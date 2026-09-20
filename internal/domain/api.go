package domain

import "time"

type RepositorySetupRequest struct {
	Mode     string `json:"mode"`
	Remote   string `json:"remote,omitempty"`
	Branch   string `json:"branch"`
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

type AuditEvent struct {
	ID          string    `json:"id"`
	ActorUserID string    `json:"actorUserId,omitempty"`
	Action      string    `json:"action"`
	TargetType  string    `json:"targetType"`
	TargetID    string    `json:"targetId,omitempty"`
	Outcome     string    `json:"outcome"`
	RequestID   string    `json:"requestId"`
	SourceIP    string    `json:"sourceIp,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}

type StackState struct {
	Runtime   RuntimeState        `json:"runtime"`
	Freshness DeploymentFreshness `json:"freshness"`
}
