package domain

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

type FileContent struct {
	Path    string `json:"path"`
	Content []byte `json:"-"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
}

type FileEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"isDirectory"`
	Editable bool   `json:"editable"`
}
