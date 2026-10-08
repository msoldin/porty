// Package ondemand owns activation policy and conservative group lifecycle decisions.
package ondemand

import (
	"errors"
	"time"

	"github.com/msoldin/porty/internal/stack"
	"github.com/msoldin/porty/internal/traffic"
)

const MaxGroups = 64
const MaxMembers = 8

var ErrInvalid = errors.New("invalid on-demand policy")
var ErrConflict = errors.New("on-demand policy changed")
var ErrUnavailable = errors.New("on-demand activation unavailable")

type Phase string

const (
	Running  Phase = "running"
	Sleeping Phase = "sleeping"
	Starting Phase = "starting"
	Stopping Phase = "stopping"
	Unknown  Phase = "unknown"
)

type Action string

const (
	WakeUp Action = "wake"
	Sleep  Action = "sleep"
)

type Policy struct {
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	Members           []string `json:"members"`
	WakeThreshold     uint32   `json:"wakeThreshold"`
	WakeWindowMS      int64    `json:"wakeWindowMs"`
	IdleSeconds       int64    `json:"idleSeconds"`
	MinRuntimeSeconds int64    `json:"minRuntimeSeconds"`
	StartupSeconds    int64    `json:"startupSeconds"`
	StopGraceSeconds  int64    `json:"stopGraceSeconds"`
}
type Group struct {
	ID      string        `json:"id"`
	StackID stack.StackID `json:"stackId"`
	Policy
	Revision          int64    `json:"revision"`
	Phase             Phase    `json:"phase"`
	HoldReason        string   `json:"holdReason,omitempty"`
	PausedReason      string   `json:"pausedReason,omitempty"`
	ObservationReason string   `json:"observationReason,omitempty"`
	Evidence          Evidence `json:"-"`
}
type PolicyUpdate struct {
	Policy
	ExpectedRevision int64 `json:"expectedRevision"`
}

// Evidence contains only allowlisted runtime identity; never Compose environment.
type Evidence struct {
	SourceDigest string            `json:"sourceDigest"`
	ContainerIDs []string          `json:"containerIds"`
	Bindings     []traffic.Binding `json:"bindings"`
}
type Counters struct {
	Received uint64
	Sent     uint64
}
type Sample struct {
	RuntimeEpoch string
	Phase        Phase
	ObservedAt   time.Time
	Counters     map[string]Counters
	Evidence     Evidence
}
type Decision struct {
	Action            Action
	PauseReason       string
	UnavailableReason string
}
