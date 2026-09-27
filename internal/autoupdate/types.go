package autoupdate

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/stack"
	"time"
)

var ErrConflict = errors.New("auto-update policy changed")
var ErrInvalid = errors.New("invalid UTC cron expression")
var ErrUnavailable = errors.New("automatic updates unavailable")

const DefaultExpression = "0 0 * * *"

type Policy struct {
	StackID      stack.StackID `json:"stackId"`
	Enabled      bool          `json:"enabled"`
	Expression   string        `json:"expression"`
	Revision     int64         `json:"revision"`
	NextRunAt    time.Time     `json:"nextRunAt"`
	PausedReason string        `json:"pausedReason,omitempty"`
}
type Run struct {
	ID             string        `json:"id"`
	StackID        stack.StackID `json:"stackId"`
	StackName      string        `json:"stackName"`
	PolicyRevision int64         `json:"policyRevision"`
	ScheduledAt    time.Time     `json:"scheduledAt"`
	Phase          string        `json:"phase"`
	Outcome        string        `json:"outcome,omitempty"`
	Reason         string        `json:"reason,omitempty"`
	OperationID    string        `json:"operationId,omitempty"`
}
type PolicyUpdate struct {
	Enabled          bool   `json:"enabled"`
	Expression       string `json:"expression"`
	ExpectedRevision int64  `json:"expectedRevision"`
}
type Store interface {
	GetPolicy(context.Context, stack.StackID) (Policy, error)
	SavePolicy(context.Context, stack.StackID, PolicyUpdate, time.Time) (Policy, error)
	ListDue(context.Context, time.Time, int) ([]Policy, error)
	Admit(context.Context, Policy, time.Time) (Run, bool, error)
	ResetAfterStartup(context.Context, time.Time) error
	PendingRuns(context.Context) ([]Run, error)
}
type Executor interface {
	CheckAndUpdate(context.Context, Run) error
}
