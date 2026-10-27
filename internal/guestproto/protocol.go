package guestproto

import (
	"errors"
	"path"
	"strings"
)

const Port uint32 = 1024
const MaxMessage = 5 * 1024 * 1024

// Only virtual workspace paths cross the broker. No host paths or process IDs exist in this protocol.
type File struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Deleted  bool   `json:"deleted,omitempty"`
}
type Job struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	FilePath string `json:"filePath,omitempty"`
	Files    []File `json:"files"`
}
type Result struct {
	ID            string `json:"id"`
	Output        string `json:"output"`
	Error         bool   `json:"error"`
	Files         []File `json:"files"`
	Identity      string `json:"identity"`
	FilesComplete bool   `json:"filesComplete"`
}

func ValidPath(p string) bool {
	return len(p) <= 400 && !strings.ContainsAny(p, "\x00\\") && path.Clean(p) == p && (p == "/workspace" || strings.HasPrefix(p, "/workspace/"))
}
func Validate(job Job) error {
	if job.ID == "" || len(job.ID) > 80 || len(job.Source) > 512000 || len(job.Files) > 256 {
		return errors.New("Invalid sandbox job")
	}
	if job.Kind != "python" && job.Kind != "shell" {
		return errors.New("Unsupported sandbox language")
	}
	if job.FilePath != "" && !ValidPath(job.FilePath) {
		return errors.New("Invalid script path")
	}
	total := 0
	seen := map[string]bool{}
	for _, f := range job.Files {
		if !ValidPath(f.Path) || seen[f.Path] || len(f.Content) > 512000 || (f.Kind != "file" && f.Kind != "directory") || (f.Encoding != "utf8" && f.Encoding != "base64") {
			return errors.New("Invalid sandbox file")
		}
		seen[f.Path] = true
		total += len(f.Content)
	}
	if total > 2400000 {
		return errors.New("Sandbox workspace is full")
	}
	return nil
}
