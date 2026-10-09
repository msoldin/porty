package monitoring

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
)

var ErrInvalidCursor = errors.New("invalid monitoring cursor")
var errSnapshotTooLarge = errors.New("monitoring snapshot exceeds limit")

type cursor struct {
	Generation string `json:"g"`
	Sequence   string `json:"s"`
	Inventory  string `json:"i"`
}
type storedSample struct {
	at       time.Time
	sequence uint64
	data     []byte
}

func decodeCursor(raw string) (cursor, uint64, error) {
	if len(raw) > 256 {
		return cursor{}, 0, ErrInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursor{}, 0, ErrInvalidCursor
	}
	var value cursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.Generation == "" || value.Inventory == "" {
		return cursor{}, 0, ErrInvalidCursor
	}
	if decoder.Decode(new(any)) != io.EOF {
		return cursor{}, 0, ErrInvalidCursor
	}
	seq, err := strconv.ParseUint(value.Sequence, 10, 64)
	if err != nil || strconv.FormatUint(seq, 10) != value.Sequence {
		return cursor{}, 0, ErrInvalidCursor
	}
	return value, seq, nil
}

func (s *Service) pruneLocked(now time.Time) {
	for len(s.history) > 0 && (len(s.history) > maxSamples || now.Sub(s.history[0].at) > HistoryWindow || s.historyBytes > s.historyLimit) {
		s.historyBytes -= len(s.history[0].data) + 64
		s.history[0] = storedSample{}
		s.history = s.history[1:]
	}
}
func (s *Service) record(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	current := s.currentLocked(now)
	current.Sequence = strconv.FormatUint(s.sequence, 10)
	current.CapturedAt = now.UTC()
	data, err := json.Marshal(current)
	if err != nil {
		return
	}
	s.history = append(s.history, storedSample{at: now, sequence: s.sequence, data: data})
	s.historyBytes += len(data) + 64
	s.pruneLocked(now)
}

func (s *Service) Snapshot(rawCursor string) (Snapshot, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	reset := rawCursor == ""
	var previous cursor
	var sequence uint64
	if !reset {
		var err error
		previous, sequence, err = decodeCursor(rawCursor)
		if err != nil {
			return Snapshot{}, err
		}
		reset = previous.Generation != s.generation || sequence > s.sequence
		if len(s.history) > 0 && sequence < s.history[0].sequence-1 {
			reset = true
		}
		if len(s.history) == 0 && sequence < s.sequence {
			reset = true
		}
	}
	result := Snapshot{Generation: s.generation, Reset: reset, ServerTime: now.UTC(), WindowStart: now.UTC(), Host: s.host, Current: s.currentLocked(now), Samples: []Sample{}, Coverage: append([]Coverage{}, s.coverage...)}
	if reset || previous.Inventory != s.inventory.Revision {
		data, _ := json.Marshal(s.inventory)
		if err := json.Unmarshal(data, &result.Inventory); err != nil {
			return Snapshot{}, err
		}
	}

	encodedCursor, _ := json.Marshal(cursor{s.generation, strconv.FormatUint(s.sequence, 10), s.inventory.Revision})
	result.Cursor = base64.RawURLEncoding.EncodeToString(encodedCursor)
	if len(s.history) > 0 {
		result.WindowStart = s.history[0].at.UTC()
	}
	// Select the newest encoded samples that fit before allocating decoded maps.
	// Re-encoding and trimming an entire large history creates quadratic work.
	metadata, err := json.Marshal(result)
	if err != nil {
		return Snapshot{}, err
	}
	budget := s.responseLimit - len(metadata) - 16 // timestamp precision may change
	first := len(s.history)
	trimmed := false
	for i := len(s.history) - 1; i >= 0; i-- {
		entry := s.history[i]
		if !reset && entry.sequence <= sequence {
			break
		}
		size := len(entry.data) + 1
		if size > budget {
			trimmed = true
			break
		}
		budget -= size
		first = i
	}
	if trimmed {
		result.WindowStart = now.UTC()
		if first < len(s.history) {
			result.WindowStart = s.history[first].at.UTC()
		}
	}
	for _, entry := range s.history[first:] {
		var sample Sample
		if err := json.Unmarshal(entry.data, &sample); err != nil {
			return Snapshot{}, err
		}
		result.Samples = append(result.Samples, sample)
	}
	for {
		data, err := json.Marshal(result)
		if err != nil {
			return Snapshot{}, err
		}
		if len(data) <= s.responseLimit {
			return result, nil
		}
		if len(result.Samples) == 0 {
			return Snapshot{}, errSnapshotTooLarge
		}
		result.Samples[0] = Sample{}
		result.Samples = result.Samples[1:]
		result.WindowStart = now.UTC()
		if len(result.Samples) > 0 {
			result.WindowStart = result.Samples[0].CapturedAt
		}
	}
}

func (s *Service) currentLocked(now time.Time) Sample {
	readings := make(map[string]Reading, len(s.readings))
	for id, reading := range s.readings {
		if reading.State == StateAvailable && now.Sub(reading.SampledAt) >= StaleAfter {
			reading.State = StateStale
			reading.Reason = "collection_delayed"
		}
		if reading.Value != nil {
			value := *reading.Value
			reading.Value = &value
		}
		if reading.LastSuccessAt != nil {
			at := *reading.LastSuccessAt
			reading.LastSuccessAt = &at
		}
		readings[id] = reading
	}
	return Sample{Sequence: strconv.FormatUint(s.sequence, 10), CapturedAt: now.UTC(), Readings: readings}
}
