package ondemand

import (
	"testing"
	"time"
)

func TestPolicyDefaultsMatchConservativeIdleBehavior(t *testing.T) {
	p := DefaultPolicy()
	p.Name = "Minecraft"
	p.Members = []string{"server"}
	if err := ValidatePolicy(p); err != nil {
		t.Fatal(err)
	}
	if p.WakeThreshold != 1 || p.WakeWindowMS != 1000 || p.IdleSeconds != 600 || p.MinRuntimeSeconds != 120 || p.StartupSeconds != 300 || p.StopGraceSeconds != 120 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}
func TestPolicyRejectsInvalidLimitsAndMembership(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Policy)
	}{
		{"empty name", func(p *Policy) { p.Name = "" }},
		{"empty members", func(p *Policy) { p.Members = nil }},
		{"duplicate member", func(p *Policy) { p.Members = []string{"server", "server"} }},
		{"too many members", func(p *Policy) { p.Members = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} }},
		{"invalid service name", func(p *Policy) { p.Members = []string{"../server"} }},
		{"short idle", func(p *Policy) { p.IdleSeconds = 59 }},
		{"long idle", func(p *Policy) { p.IdleSeconds = 86401 }},
		{"negative minimum", func(p *Policy) { p.MinRuntimeSeconds = -1 }},
		{"short startup", func(p *Policy) { p.StartupSeconds = 29 }},
		{"long startup", func(p *Policy) { p.StartupSeconds = 901 }},
		{"short stop grace", func(p *Policy) { p.StopGraceSeconds = 9 }},
		{"long stop grace", func(p *Policy) { p.StopGraceSeconds = 121 }},
		{"zero threshold", func(p *Policy) { p.WakeThreshold = 0 }},
		{"long wake window", func(p *Policy) { p.WakeWindowMS = 60001 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultPolicy()
			p.Name = "Game"
			p.Members = []string{"server"}
			tc.edit(&p)
			if err := ValidatePolicy(p); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	p := DefaultPolicy()
	p.Name = "Game"
	p.Members = []string{"server"}
	p.MinRuntimeSeconds = 0
	if err := ValidatePolicy(p); err != nil {
		t.Fatalf("zero minimum runtime rejected: %v", err)
	}
}
func TestTrackerRequiresFreshIdleEvidenceAndMinimumRuntime(t *testing.T) {
	now := time.Unix(1000, 0)
	p := DefaultPolicy()
	p.Name = "Game"
	p.Members = []string{"server"}
	p.Enabled = true
	p.IdleSeconds = 60
	p.MinRuntimeSeconds = 120
	g := Group{ID: "group", Policy: p, Revision: 1, Phase: Running}
	sample := Sample{Phase: Running, ObservedAt: now, Counters: map[string]Counters{"container": {Received: 10, Sent: 20}}}
	tracker := Tracker{}
	if d := tracker.Evaluate(g, sample, false, now); d.Action != "" {
		t.Fatalf("fresh process slept: %+v", d)
	}
	sample.ObservedAt = now.Add(61 * time.Second)
	if d := tracker.Evaluate(g, sample, false, sample.ObservedAt); d.Action != "" {
		t.Fatalf("minimum runtime ignored: %+v", d)
	}
	sample.ObservedAt = now.Add(120 * time.Second)
	if d := tracker.Evaluate(g, sample, false, sample.ObservedAt); d.Action != Sleep {
		t.Fatalf("idle group not eligible: %+v", d)
	}
}
func TestTrackerRefreshesIdleOnEitherDirectionAndCounterReset(t *testing.T) {
	now := time.Unix(1000, 0)
	p := DefaultPolicy()
	p.Enabled = true
	p.IdleSeconds = 60
	p.MinRuntimeSeconds = 0
	for _, direction := range []string{"incoming", "outgoing", "reset"} {
		t.Run(direction, func(t *testing.T) {
			g := Group{ID: "group", Policy: p, Revision: 1, Phase: Running}
			tracker := Tracker{}
			s := Sample{Phase: Running, ObservedAt: now, Counters: map[string]Counters{"id": {Received: 10, Sent: 20}}}
			tracker.Evaluate(g, s, false, now)
			counters := s.Counters["id"]
			switch direction {
			case "incoming":
				counters.Received++
			case "outgoing":
				counters.Sent++
			case "reset":
				counters.Received = 0
			}
			s.Counters["id"] = counters
			s.ObservedAt = now.Add(59 * time.Second)
			if d := tracker.Evaluate(g, s, false, s.ObservedAt); d.Action != "" {
				t.Fatalf("activity allowed stop: %+v", d)
			}
			s.ObservedAt = now.Add(61 * time.Second)
			if d := tracker.Evaluate(g, s, false, s.ObservedAt); d.Action != "" {
				t.Fatalf("activity did not refresh idle: %+v", d)
			}
		})
	}
}
func TestTrackerNeverStopsWithoutCompleteFreshCounters(t *testing.T) {
	now := time.Unix(1000, 0)
	p := DefaultPolicy()
	p.Enabled = true
	p.IdleSeconds = 60
	p.MinRuntimeSeconds = 0
	g := Group{Policy: p, Revision: 1, Phase: Running}
	for _, missing := range []bool{true, false} {
		tracker := Tracker{}
		s := Sample{Phase: Running, ObservedAt: now, Counters: map[string]Counters{"id": {}}}
		tracker.Evaluate(g, s, false, now)
		if missing {
			s.Counters = nil
			s.ObservedAt = now.Add(time.Minute)
		}
		d := tracker.Evaluate(g, s, false, now.Add(2*time.Minute))
		if d.Action == Sleep {
			t.Fatalf("missing or stale counters authorized stop: %+v", d)
		}
	}
}
func TestTrackerIgnoresWakeWhenHeldDisabledOrPaused(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, mode := range []string{"held", "disabled", "paused"} {
		p := DefaultPolicy()
		p.Enabled = true
		g := Group{Policy: p, Revision: 1, Phase: Sleeping}
		switch mode {
		case "held":
			g.HoldReason = "manual"
		case "disabled":
			g.Enabled = false
		case "paused":
			g.PausedReason = "recovery"
		}
		tracker := Tracker{}
		if d := tracker.Evaluate(g, Sample{Phase: Sleeping, ObservedAt: now}, true, now); d.Action != "" {
			t.Fatalf("%s group woke: %+v", mode, d)
		}
	}
}
func TestTrackerWakesOnlyOwnedSleepAndPausesUnexpectedRuntime(t *testing.T) {
	now := time.Unix(1000, 0)
	p := DefaultPolicy()
	p.Enabled = true
	tracker := Tracker{}
	g := Group{Policy: p, Revision: 1, Phase: Sleeping}
	if d := tracker.Evaluate(g, Sample{Phase: Sleeping, ObservedAt: now}, true, now); d.Action != WakeUp {
		t.Fatalf("owned sleep did not wake: %+v", d)
	}
	g.Phase = Running
	if d := tracker.Evaluate(g, Sample{Phase: Sleeping, ObservedAt: now}, true, now); d.PauseReason == "" || d.Action != "" {
		t.Fatalf("external stop treated as owned sleep: %+v", d)
	}
}

func TestTrackerPausesAfterExternalRestartWithSameCounters(t *testing.T) {
	now := time.Unix(1000, 0)
	p := DefaultPolicy()
	p.Enabled = true
	p.IdleSeconds = 60
	p.MinRuntimeSeconds = 0
	g := Group{Policy: p, Revision: 1, Phase: Running}
	s := Sample{Phase: Running, ObservedAt: now, RuntimeEpoch: "first", Counters: map[string]Counters{"id": {}}}
	tracker := Tracker{}
	tracker.Evaluate(g, s, false, now)
	s.RuntimeEpoch = "restarted"
	s.ObservedAt = now.Add(time.Minute)
	d := tracker.Evaluate(g, s, false, s.ObservedAt)
	if d.Action != "" || d.PauseReason == "" {
		t.Fatalf("external restart inherited old idle window: %+v", d)
	}
}
