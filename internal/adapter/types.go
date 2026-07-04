package adapter

import "time"

type Project struct {
	ID         string    `json:"id"`   // flattened dir name under ~/.claude/projects
	Path       string    `json:"path"` // absolute path of the project dir
	Dir        string    `json:"dir"`  // original working directory (from jsonl cwd), "" if unknown
	Name       string    `json:"name"` // display name: base of Dir, else ID with leading '-' trimmed
	Sessions   int       `json:"sessions"`
	SizeBytes  int64     `json:"sizeBytes"`
	LastActive time.Time `json:"lastActive"`
}

type Session struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	Title        string    `json:"title"`
	SizeBytes    int64     `json:"sizeBytes"` // jsonl + sidecar dir
	Messages     int       `json:"messages"`  // user+assistant records
	Records      int       `json:"records"`   // all records
	HasAssistant bool      `json:"hasAssistant"`
	Modified     time.Time `json:"modified"`
	Active       bool      `json:"active"` // modified within claude.ActiveWindow
}

type Orphan struct {
	ID        string `json:"id"` // uuid of sidecar dir without a jsonl
	ProjectID string `json:"projectId"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

type Message struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}
