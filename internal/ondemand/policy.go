package ondemand

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var memberName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

func DefaultPolicy() Policy {
	return Policy{WakeThreshold: 1, WakeWindowMS: 1000, IdleSeconds: 600, MinRuntimeSeconds: 120, StartupSeconds: 300, StopGraceSeconds: 120}
}
func ValidatePolicy(p Policy) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalid, reason) }
	if p.Name != strings.TrimSpace(p.Name) || p.Name == "" || !utf8.ValidString(p.Name) || utf8.RuneCountInString(p.Name) > 64 {
		return invalid("name must contain 1–64 characters without surrounding spaces")
	}
	if len(p.Members) == 0 || len(p.Members) > MaxMembers {
		return invalid("select 1–8 services")
	}
	seen := map[string]bool{}
	for _, member := range p.Members {
		if !memberName.MatchString(member) || seen[member] {
			return invalid("service names must be valid and unique")
		}
		seen[member] = true
	}
	if p.WakeThreshold < 1 || p.WakeThreshold > 1000 {
		return invalid("wake threshold must be 1–1000 attempts")
	}
	if p.WakeWindowMS < 10 || p.WakeWindowMS > 60000 {
		return invalid("wake window must be 10–60000 milliseconds")
	}
	if p.IdleSeconds < 60 || p.IdleSeconds > 86400 {
		return invalid("idle timeout must be 1 minute–24 hours")
	}
	if p.MinRuntimeSeconds < 0 || p.MinRuntimeSeconds > 3600 {
		return invalid("minimum runtime must be 0–60 minutes")
	}
	if p.StartupSeconds < 30 || p.StartupSeconds > 900 {
		return invalid("startup timeout must be 30 seconds–15 minutes")
	}
	if p.StopGraceSeconds < 10 || p.StopGraceSeconds > 120 {
		return invalid("stop grace must be 10–120 seconds per service")
	}
	return nil
}

// Tracker uses monotonic times and fresh container-wide counters. It is owned by
// one controller worker at a time and never grants authority to mutate Docker;
// the executor must revalidate durable policy and identity under the stack lock.
type Tracker struct {
	runtimeEpoch string
	revision     int64
	phase        Phase
	runningSince time.Time
	lastActivity time.Time
	counters     map[string]Counters
}

func (t *Tracker) Invalidate(now time.Time) { t.lastActivity = now; t.counters = nil }
func (t *Tracker) Evaluate(g Group, s Sample, pending bool, now time.Time) Decision {
	if t.revision != g.Revision || t.phase != g.Phase || t.runningSince.IsZero() {
		t.revision = g.Revision
		t.phase = g.Phase
		t.runningSince = now
		t.runtimeEpoch = s.RuntimeEpoch
		t.Invalidate(now)
	}
	if !g.Enabled || g.HoldReason != "" || g.PausedReason != "" {
		t.Invalidate(now)
		return Decision{}
	}
	if s.ObservedAt.IsZero() || now.Sub(s.ObservedAt) > 10*time.Second || s.ObservedAt.After(now) {
		t.Invalidate(now)
		return Decision{UnavailableReason: "Runtime observation is stale."}
	}
	if g.Phase == Starting || g.Phase == Stopping {
		return Decision{}
	}
	if s.Phase != g.Phase || g.Phase == Unknown {
		return Decision{PauseReason: "Container state changed outside the owned transition. Review and resume this group."}
	}
	if g.Phase == Sleeping {
		if pending {
			return Decision{Action: WakeUp}
		}
		return Decision{}
	}
	if len(s.Counters) == 0 {
		t.Invalidate(now)
		return Decision{UnavailableReason: "Network activity could not be observed."}
	}
	if t.runtimeEpoch != s.RuntimeEpoch {
		return Decision{PauseReason: "Container restarted outside the owned transition. Review and resume this group."}
	}
	if t.counters == nil {
		t.lastActivity = now
		t.counters = maps.Clone(s.Counters)
		return Decision{}
	}
	if len(s.Counters) != len(t.counters) {
		return Decision{PauseReason: "Container membership changed. Review and resume this group."}
	}
	changed := false
	for id, counts := range s.Counters {
		previous, ok := t.counters[id]
		if !ok {
			return Decision{PauseReason: "Container identity changed. Review and resume this group."}
		}
		if previous != counts {
			changed = true
		}
	}
	if changed {
		t.lastActivity = now
	}
	t.counters = maps.Clone(s.Counters)
	if now.Sub(t.runningSince) >= time.Duration(g.MinRuntimeSeconds)*time.Second && now.Sub(t.lastActivity) >= time.Duration(g.IdleSeconds)*time.Second {
		return Decision{Action: Sleep}
	}
	return Decision{}
}
