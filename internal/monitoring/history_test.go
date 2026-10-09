package monitoring

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func historyService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	now := time.Now()
	service, err := NewService(ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, &now
}

func TestHistoryExpiresSamplesAfterFiveMinutes(t *testing.T) {
	service, now := historyService(t)
	start := *now
	for i := 0; i < 152; i++ {
		*now = start.Add(time.Duration(i) * 2 * time.Second)
		service.record(*now)
	}
	got, err := service.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Samples) != 151 || got.Samples[0].Sequence != "2" {
		t.Fatalf("retention: %d samples, first %s", len(got.Samples), got.Samples[0].Sequence)
	}
	*now = now.Add(6 * time.Minute)
	got, err = service.Snapshot("")
	if err != nil || len(got.Samples) != 0 {
		t.Fatalf("expired history: %d, %v", len(got.Samples), err)
	}
}

func TestHistoryResetsForOldGeneration(t *testing.T) {
	first, now := historyService(t)
	first.record(*now)
	initial, _ := first.Snapshot("")
	next, _ := historyService(t)
	got, err := next.Snapshot(initial.Cursor)
	if err != nil || !got.Reset || got.Inventory == nil {
		t.Fatalf("restart did not reset: %#v %v", got, err)
	}
}

func TestHistoryRejectsMalformedCursor(t *testing.T) {
	service, _ := historyService(t)
	for _, cursor := range []string{"!", strings.Repeat("x", 257), base64.RawURLEncoding.EncodeToString([]byte(`{"generation":"g","sequence":"-1","inventory":"0"}`))} {
		if _, err := service.Snapshot(cursor); err != ErrInvalidCursor {
			t.Errorf("cursor %q: %v", cursor, err)
		}
	}
}

func TestHistoryPreservesSequencePrecisionAndOmitsUnchangedInventory(t *testing.T) {
	service, now := historyService(t)
	service.sequence = 9007199254740992
	service.record(*now)
	initial, err := service.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Samples[0].Sequence != "9007199254740993" {
		t.Fatal("sequence lost precision")
	}
	*now = now.Add(2 * time.Second)
	service.record(*now)
	got, err := service.Snapshot(initial.Cursor)
	if err != nil || got.Reset || got.Inventory != nil || len(got.Samples) != 1 {
		t.Fatalf("incremental response: %#v, %v", got, err)
	}
	if got.Samples[0].Sequence != "9007199254740994" {
		t.Fatal("incorrect incremental sequence")
	}
}

func TestHistoryReportsReducedWindowUnderByteLimit(t *testing.T) {
	service, now := historyService(t)
	service.historyLimit = 700
	for i := 0; i < 12; i++ {
		*now = now.Add(2 * time.Second)
		service.record(*now)
	}
	got, err := service.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Samples) == 0 || len(got.Samples) >= 12 || got.WindowStart != got.Samples[0].CapturedAt {
		t.Fatalf("window not reduced: %#v", got)
	}
}

func TestHistorySnapshotIsIndependentOfCallerMutation(t *testing.T) {
	service, now := historyService(t)
	service.acceptBatch("cpu", fixtureBatch(*now, 24), nil, *now)
	service.record(*now)
	got, _ := service.Snapshot("")
	got.Current.Readings["cpu.busy"] = Reading{}
	got.Inventory.Devices[0].Name = "changed"
	next, _ := service.Snapshot("")
	if next.Current.Readings["cpu.busy"].Value == nil || next.Inventory.Devices[0].Name != "Host CPU" {
		t.Fatal("snapshot shares mutable collector state")
	}
}

func TestHistoryResponseIsBoundedJSON(t *testing.T) {
	service, now := historyService(t)
	service.responseLimit = 1800
	for i := 0; i < 30; i++ {
		*now = now.Add(2 * time.Second)
		service.acceptBatch("cpu", fixtureBatch(*now, 24), nil, *now)
		service.record(*now)
	}
	got, err := service.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil || len(encoded) > 1800 || len(got.Samples) >= 30 {
		t.Fatalf("unbounded response: %d %v", len(encoded), err)
	}
	if got.WindowStart != got.Samples[0].CapturedAt {
		t.Fatal("window does not describe returned samples")
	}
}

func TestHistoryUsesObservationAgeWhenWallTimeJumps(t *testing.T) {
	for _, jump := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		service, now := historyService(t)
		service.record(*now)
		// Simulate a wall-clock discontinuity in the displayed sample while
		// retaining the collector's monotonic observation time.
		var sample Sample
		if err := json.Unmarshal(service.history[0].data, &sample); err != nil {
			t.Fatal(err)
		}
		sample.CapturedAt = now.Add(jump).UTC()
		data, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		service.history[0].data = data
		*now = now.Add(2 * time.Second)
		got, err := service.Snapshot("")
		if err != nil || len(got.Samples) != 1 {
			t.Fatalf("wall-clock jump expired live history: %v", err)
		}
		*now = now.Add(5 * time.Minute)
		got, err = service.Snapshot("")
		if err != nil || len(got.Samples) != 0 {
			t.Fatalf("wall-clock jump retained old history: %v", err)
		}
	}
}
