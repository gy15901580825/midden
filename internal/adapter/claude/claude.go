// Package claude scans and manipulates Claude Code session data
// stored under ~/.claude/projects. The adapter is read-only except
// for Rename (which appends one line); all deletions happen in
// internal/trash.
package claude

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"midden/internal/adapter"
)

const ActiveWindow = 10 * time.Minute

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Adapter struct {
	Root string
	Now  func() time.Time
}

func New(root string) *Adapter { return &Adapter{Root: root, Now: time.Now} }

func DefaultRoot() (string, error) {
	if v := os.Getenv("MIDDEN_CLAUDE_ROOT"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func (a *Adapter) Projects() ([]adapter.Project, error) {
	entries, err := os.ReadDir(a.Root)
	if err != nil {
		return nil, err
	}
	out := []adapter.Project{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := adapter.Project{ID: e.Name(), Path: filepath.Join(a.Root, e.Name())}
		var newestPath string
		filepath.WalkDir(p.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			p.SizeBytes += info.Size()
			if filepath.Dir(path) == p.Path && strings.HasSuffix(d.Name(), ".jsonl") &&
				uuidRe.MatchString(strings.TrimSuffix(d.Name(), ".jsonl")) {
				p.Sessions++
				if info.ModTime().After(p.LastActive) {
					p.LastActive, newestPath = info.ModTime(), path
				}
			}
			return nil
		})
		if newestPath != "" {
			p.Dir = readCwd(newestPath)
		}
		p.Name = displayName(p.ID, p.Dir)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActive.After(out[j].LastActive) })
	return out, nil
}

func displayName(id, dir string) string {
	if dir != "" {
		return filepath.Base(dir)
	}
	return strings.TrimPrefix(id, "-")
}

// projectDir validates pid and returns the project path.
// Rejects symlinks that resolve outside Root (fence against escape).
func (a *Adapter) projectDir(pid string) (string, error) {
	if pid == "" || strings.ContainsAny(pid, `/\`) || pid == "." || pid == ".." {
		return "", fmt.Errorf("invalid project id %q", pid)
	}
	dir := filepath.Join(a.Root, pid)
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	rootResolved, err := filepath.EvalSymlinks(a.Root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing: %s resolves outside %s", dir, a.Root)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a project dir: %s", dir)
	}
	return resolved, nil
}

func (a *Adapter) SessionPaths(pid, sid string) ([]string, error) {
	dir, err := a.projectDir(pid)
	if err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(sid) {
		return nil, fmt.Errorf("invalid session id %q", sid)
	}
	jsonl := filepath.Join(dir, sid+".jsonl")
	if _, err := os.Stat(jsonl); err != nil {
		return nil, err
	}
	paths := []string{jsonl}
	if info, err := os.Stat(filepath.Join(dir, sid)); err == nil && info.IsDir() {
		paths = append(paths, filepath.Join(dir, sid))
	}
	return paths, nil
}

func (a *Adapter) ProjectPaths(pid string) ([]string, error) {
	dir, err := a.projectDir(pid)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, e := range entries {
		if e.Name() == "memory" {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	return paths, nil
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func (a *Adapter) Sessions(pid string) ([]adapter.Session, error) {
	dir, err := a.projectDir(pid)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []adapter.Session{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".jsonl")
		if !uuidRe.MatchString(sid) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		meta := parseMeta(filepath.Join(dir, e.Name()))
		s := adapter.Session{
			ID: sid, ProjectID: pid,
			Title:        meta.Title,
			Messages:     meta.Messages,
			Records:      meta.Records,
			HasAssistant: meta.HasAssistant,
			Modified:     info.ModTime(),
			SizeBytes:    info.Size() + dirSize(filepath.Join(dir, sid)),
		}
		if s.Title == "" {
			s.Title = "(untitled)"
		}
		s.Active = a.Now().Sub(s.Modified) < ActiveWindow
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

func (a *Adapter) Orphans(pid string) ([]adapter.Orphan, error) {
	dir, err := a.projectDir(pid)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []adapter.Orphan{}
	for _, e := range entries {
		if !e.IsDir() || !uuidRe.MatchString(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name()+".jsonl")); err == nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		out = append(out, adapter.Orphan{ID: e.Name(), ProjectID: pid, Path: p, SizeBytes: dirSize(p)})
	}
	return out, nil
}

func (a *Adapter) FindSession(sid string) (string, error) {
	if !uuidRe.MatchString(sid) {
		return "", fmt.Errorf("invalid session id %q", sid)
	}
	entries, err := os.ReadDir(a.Root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(a.Root, e.Name(), sid+".jsonl")); err == nil {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("session %s: %w", sid, os.ErrNotExist)
}

func (a *Adapter) Rename(pid, sid, title string) error {
	paths, err := a.SessionPaths(pid, sid)
	if err != nil {
		return err
	}
	line, err := json.Marshal(map[string]string{
		"type": "ai-title", "aiTitle": title, "sessionId": sid,
	})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(paths[0], os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func (a *Adapter) Preview(pid, sid string, n int) ([]adapter.Message, error) {
	paths, err := a.SessionPaths(pid, sid)
	if err != nil {
		return nil, err
	}
	msgs := readMessages(paths[0])
	if len(msgs) <= 2*n {
		return msgs, nil
	}
	return append(append([]adapter.Message{}, msgs[:n]...), msgs[len(msgs)-n:]...), nil
}

type ClearRules struct {
	All       bool
	OlderThan time.Duration
	Empty     bool
	Orphans   bool
}

type Candidate struct {
	Kind      string
	ID        string
	Label     string
	Paths     []string
	SizeBytes int64
}

func (a *Adapter) ClearCandidates(pid string, r ClearRules) ([]Candidate, error) {
	sessions, err := a.Sessions(pid)
	if err != nil {
		return nil, err
	}
	out := []Candidate{}
	for _, s := range sessions {
		if s.Active {
			continue
		}
		match := r.All ||
			(r.OlderThan > 0 && a.Now().Sub(s.Modified) > r.OlderThan) ||
			(r.Empty && (!s.HasAssistant || s.Records < 5))
		if !match {
			continue
		}
		paths, err := a.SessionPaths(pid, s.ID)
		if err != nil {
			continue
		}
		out = append(out, Candidate{
			Kind: "session", ID: s.ID, Label: s.Title,
			Paths: paths, SizeBytes: s.SizeBytes,
		})
	}
	if r.All || r.Orphans {
		orphans, err := a.Orphans(pid)
		if err != nil {
			return nil, err
		}
		for _, o := range orphans {
			out = append(out, Candidate{
				Kind: "orphan", ID: o.ID, Label: "orphan sidecar " + o.ID[:8],
				Paths: []string{o.Path}, SizeBytes: o.SizeBytes,
			})
		}
	}
	return out, nil
}
