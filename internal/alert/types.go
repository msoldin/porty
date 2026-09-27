package alert

import (
	"context"
	"errors"
	"time"
)

var (
	ErrConflict                    = errors.New("alert changed; reload and try again")
	ErrNotFound                    = errors.New("alert not found")
	ErrInvalid                     = errors.New("invalid alert request")
	ErrManualResolutionUnavailable = errors.New("manual resolution unavailable")
)

type Key struct {
	StackID string `json:"stackId"`
	Problem string `json:"problem"`
	Target  string `json:"target"`
}

type Alert struct {
	ID                 string     `json:"id"`
	Key                Key        `json:"key"`
	StackName          string     `json:"stackName"`
	Revision           int64      `json:"revision"`
	Episode            int        `json:"episode"`
	Count              int        `json:"count"`
	Summary            string     `json:"summary"`
	OperationID        string     `json:"operationId,omitempty"`
	FirstAt            time.Time  `json:"firstAt"`
	LatestAt           time.Time  `json:"latestAt"`
	AcknowledgedAt     *time.Time `json:"acknowledgedAt,omitempty"`
	AcknowledgedBy     string     `json:"acknowledgedBy,omitempty"`
	ResolvedAt         *time.Time `json:"resolvedAt,omitempty"`
	ResolvedBy         string     `json:"resolvedBy,omitempty"`
	Resolution         string     `json:"resolution,omitempty"`
	CanResolveManually bool       `json:"canResolveManually"`
}

type Change struct {
	Kind                                          string
	Key                                           Key
	StackName, OccurrenceID, OperationID, Summary string
	ExpectedRevision                              int64
	ObservedAt                                    time.Time
	CanResolveManually                            bool
}

type Mutation struct {
	ID               string
	ExpectedRevision int64
	ActorID, Note    string
}
type Filter struct {
	StackID, View string
	Limit, Offset int
}
type Page struct {
	Items               []Alert `json:"items"`
	UnacknowledgedCount int     `json:"unacknowledgedCount"`
	Total               int     `json:"total"`
}
type Event struct {
	ID          string    `json:"id"`
	AlertID     string    `json:"alertId"`
	Episode     int       `json:"episode"`
	Kind        string    `json:"kind"`
	ActorID     string    `json:"actorId,omitempty"`
	At          time.Time `json:"at"`
	Note        string    `json:"note,omitempty"`
	OperationID string    `json:"operationId,omitempty"`
}

type Store interface {
	Apply(context.Context, []Change) ([]Alert, error)
	List(context.Context, Filter) (Page, error)
	Get(context.Context, string) (Alert, error)
	History(context.Context, string, int, int) ([]Event, error)
	Acknowledge(context.Context, Mutation) (Alert, error)
	Resolve(context.Context, Mutation) (Alert, error)
}

type Publisher interface{ PublishAlert(Alert) }
