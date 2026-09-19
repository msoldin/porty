package domain

import "time"

type ComposeRequest struct {
	StackDir    string
	ProjectName string
	Environment map[string]string
}

type Deployment struct {
	ID            string           `json:"id"`
	StackID       StackID          `json:"stackId"`
	OperationID   string           `json:"operationId"`
	GitCommit     string           `json:"gitCommit,omitempty"`
	Dirty         bool             `json:"dirty"`
	DiffDigest    string           `json:"diffDigest,omitempty"`
	ComposeDigest string           `json:"composeDigest,omitempty"`
	Status        DeploymentStatus `json:"status"`
	StartedAt     time.Time        `json:"startedAt"`
	CompletedAt   time.Time        `json:"completedAt"`
	Duration      time.Duration    `json:"duration"`
	ErrorCode     string           `json:"errorCode,omitempty"`
}
