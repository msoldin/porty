package operation

import (
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/compose"
	"time"
)

type OperationStatus string

const (
	OperationQueued    OperationStatus = "queued"
	OperationRunning   OperationStatus = "running"
	OperationSucceeded OperationStatus = "succeeded"
	OperationFailed    OperationStatus = "failed"
	OperationCancelled OperationStatus = "cancelled"
)

type Operation struct {
	ServiceUpdates  []compose.ServiceUpdateResult `json:"serviceUpdates,omitempty"`
	ID              string                        `json:"id"`
	Kind            string                        `json:"kind"`
	ScopeType       string                        `json:"scopeType"`
	ScopeID         string                        `json:"scopeId,omitempty"`
	RequestKey      string                        `json:"requestKey,omitempty"`
	Status          OperationStatus               `json:"status"`
	StartedAt       time.Time                     `json:"startedAt,omitempty"`
	CompletedAt     time.Time                     `json:"completedAt,omitempty"`
	ExitCode        int                           `json:"exitCode,omitempty"`
	ErrorCode       string                        `json:"errorCode,omitempty"`
	Output          string                        `json:"output,omitempty"`
	OutputTruncated bool                          `json:"outputTruncated"`
	InitiatedBy     string                        `json:"initiatedBy,omitempty"`
	Trigger         string                        `json:"trigger,omitempty"`
	AlertTargets    []alert.Key                   `json:"-"`
	StackName       string                        `json:"-"`
}
