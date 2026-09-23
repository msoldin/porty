package filesystem

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
