package stack

import "time"

type StackID string

type Stack struct {
	ID                 StackID    `json:"id"`
	DirectoryName      string     `json:"directoryName"`
	ComposeProjectName string     `json:"composeProjectName"`
	ArchivedAt         *time.Time `json:"archivedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}
