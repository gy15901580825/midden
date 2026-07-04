// Package app is the single choke point between the UIs (CLI/TUI) and
// the filesystem: gather a Plan, show it to the user, Execute it.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aism/internal/adapter"
	"aism/internal/adapter/claude"
	"aism/internal/format"
	"aism/internal/trash"
)

type Plan struct {
	Tool, Label      string
	Details          []string
	Paths            []string
	SizeBytes        int64
	ActiveCount      int
	removeDirIfEmpty string
}

type Result struct {
	EntryID   string
	SizeBytes int64
	Count     int
}

type App struct {
	Claude *claude.Adapter
	Trash  *trash.Trash
	DryRun bool
}

func New(c *claude.Adapter, t *trash.Trash, dryRun bool) *App {
	return &App{Claude: c, Trash: t, DryRun: dryRun}
}

func (a *App) GatherSessions(pid string, sids []string) (*Plan, error) {
	sessions, err := a.Claude.Sessions(pid)
	if err != nil {
		return nil, err
	}
	byID := map[string]adapter.Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	p := &Plan{Tool: "claude"}
	for _, sid := range sids {
		s, ok := byID[sid]
		if !ok {
			return nil, fmt.Errorf("session %s not found in %s", sid, pid)
		}
		paths, err := a.Claude.SessionPaths(pid, sid)
		if err != nil {
			return nil, err
		}
		p.Paths = append(p.Paths, paths...)
		p.SizeBytes += s.SizeBytes
		detail := fmt.Sprintf("%s  %s  %s", sid[:8], s.Title, format.Size(s.SizeBytes))
		if s.Active {
			detail += " ⚡ ACTIVE"
			p.ActiveCount++
		}
		p.Details = append(p.Details, detail)
	}
	if len(sids) == 1 {
		p.Label = byID[sids[0]].Title
	} else {
		p.Label = fmt.Sprintf("%d sessions from %s", len(sids), pid)
	}
	return p, nil
}

func (a *App) GatherProject(pid string) (*Plan, error) {
	paths, err := a.Claude.ProjectPaths(pid)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("nothing to remove in %s", pid)
	}
	p := &Plan{Tool: "claude", Label: "project " + pid, Paths: paths, removeDirIfEmpty: filepath.Dir(paths[0])}
	for _, path := range paths {
		p.SizeBytes += pathSize(path)
		p.Details = append(p.Details, path)
	}
	return p, nil
}

func (a *App) GatherClear(pid string, r claude.ClearRules) (*Plan, error) {
	cands, err := a.Claude.ClearCandidates(pid, r)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("nothing to clear in %s", pid)
	}
	p := &Plan{Tool: "claude", Label: "clear " + pid}
	for _, c := range cands {
		p.Paths = append(p.Paths, c.Paths...)
		p.SizeBytes += c.SizeBytes
		p.Details = append(p.Details, fmt.Sprintf("[%s] %s  %s", c.Kind, c.Label, format.Size(c.SizeBytes)))
	}
	return p, nil
}

func (a *App) Execute(p *Plan) (*Result, error) {
	res := &Result{SizeBytes: p.SizeBytes, Count: len(p.Details)}
	if a.DryRun {
		return res, nil
	}
	e, err := a.Trash.Put(p.Tool, p.Label, p.Paths)
	if err != nil {
		return nil, err
	}
	res.EntryID = e.ID
	if p.removeDirIfEmpty != "" {
		if entries, err := os.ReadDir(p.removeDirIfEmpty); err == nil && len(entries) == 0 {
			os.Remove(p.removeDirIfEmpty)
		}
	}
	return res, nil
}

// DefaultClearRules is used for the reclaim estimate and the TUI's 'c' key.
func DefaultClearRules() claude.ClearRules {
	return claude.ClearRules{OlderThan: 30 * 24 * time.Hour, Empty: true, Orphans: true}
}

func (a *App) ReclaimEstimate() (int64, error) {
	projects, err := a.Claude.Projects()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, p := range projects {
		cands, err := a.Claude.ClearCandidates(p.ID, DefaultClearRules())
		if err != nil {
			continue
		}
		for _, c := range cands {
			total += c.SizeBytes
		}
	}
	return total, nil
}

func pathSize(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var n int64
	entries, _ := os.ReadDir(p)
	for _, e := range entries {
		n += pathSize(filepath.Join(p, e.Name()))
	}
	return n
}

// delegates for the TUI
func (a *App) Projects() ([]adapter.Project, error)           { return a.Claude.Projects() }
func (a *App) Sessions(pid string) ([]adapter.Session, error) { return a.Claude.Sessions(pid) }
func (a *App) Preview(pid, sid string, n int) ([]adapter.Message, error) {
	return a.Claude.Preview(pid, sid, n)
}
func (a *App) Rename(pid, sid, title string) error { return a.Claude.Rename(pid, sid, title) }
