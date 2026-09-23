package domain

type GitStatus struct {
	Configured bool     `json:"configured"`
	Branch     string   `json:"branch"`
	Dirty      bool     `json:"dirty"`
	Ahead      int      `json:"ahead"`
	Behind     int      `json:"behind"`
	Paths      []string `json:"paths"`
}

type GitCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    string `json:"time"`
}
