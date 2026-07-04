package tui_test

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"midden/internal/adapter"
	"midden/internal/adapter/claude"
	"midden/internal/app"
	"midden/internal/tui"
)

type fakeSvc struct {
	projects []adapter.Project
	sessions map[string][]adapter.Session
	executed []*app.Plan
	renamed  [][3]string

	// emptyOnExecute, when set, makes Execute also clear this project's
	// session list — simulating a delete that empties the project.
	emptyOnExecute string

	// clearErr, when set, makes GatherClear return this error instead of a plan.
	clearErr error
}

func (f *fakeSvc) Projects() ([]adapter.Project, error) { return f.projects, nil }
func (f *fakeSvc) Sessions(pid string) ([]adapter.Session, error) {
	return f.sessions[pid], nil
}
func (f *fakeSvc) Preview(pid, sid string, n int) ([]adapter.Message, error) {
	return []adapter.Message{{Role: "user", Text: "hello"}}, nil
}
func (f *fakeSvc) Rename(pid, sid, title string) error {
	f.renamed = append(f.renamed, [3]string{pid, sid, title})
	return nil
}
func (f *fakeSvc) GatherSessions(pid string, sids []string) (*app.Plan, error) {
	return &app.Plan{Label: "sessions", Details: sids, SizeBytes: 100}, nil
}
func (f *fakeSvc) GatherProject(pid string) (*app.Plan, error) {
	return &app.Plan{Label: "project " + pid, Details: []string{pid}, SizeBytes: 200}, nil
}
func (f *fakeSvc) GatherClear(pid string, r claude.ClearRules) (*app.Plan, error) {
	if f.clearErr != nil {
		return nil, f.clearErr
	}
	return &app.Plan{Label: "clear " + pid, Details: []string{"x"}, SizeBytes: 50}, nil
}
func (f *fakeSvc) Execute(p *app.Plan) (*app.Result, error) {
	f.executed = append(f.executed, p)
	if f.emptyOnExecute != "" {
		f.sessions[f.emptyOnExecute] = nil
	}
	return &app.Result{EntryID: "e1", SizeBytes: p.SizeBytes, Count: len(p.Details)}, nil
}
func (f *fakeSvc) ReclaimEstimate() (int64, error) { return 1 << 20, nil }

func newFake() *fakeSvc {
	return &fakeSvc{
		projects: []adapter.Project{
			{ID: "-home-u-app", Name: "app", Sessions: 2, SizeBytes: 1000},
			{ID: "-home-u-web", Name: "web", Sessions: 1, SizeBytes: 500},
		},
		sessions: map[string][]adapter.Session{
			"-home-u-app": {
				{ID: "11111111-1111-4111-8111-111111111111", Title: "first", SizeBytes: 10},
				{ID: "22222222-2222-4222-8222-222222222222", Title: "second", SizeBytes: 20},
			},
		},
	}
}

// drive pumps messages through Update, executing returned tea.Cmds.
//
// Adaptation: the top-level loop also unwraps a tea.BatchMsg passed in
// directly (e.g. from mod.Init()()), not just ones produced by a cmd()
// call inside the loop below — bubbletea's own runtime does this
// unwrapping in the program's event loop rather than in Update, so a
// bare BatchMsg fed straight into Update would otherwise match no case
// and be silently dropped.
func drive(m tea.Model, msgs ...tea.Msg) tea.Model {
	for _, msg := range msgs {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					if r := c(); r != nil {
						m, _ = m.Update(r)
					}
				}
			}
			continue
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		for cmd != nil {
			out := cmd()
			cmd = nil
			if out != nil {
				if batch, ok := out.(tea.BatchMsg); ok {
					for _, c := range batch {
						if c != nil {
							if r := c(); r != nil {
								m, _ = m.Update(r)
							}
						}
					}
				} else {
					m, cmd = m.Update(out)
				}
			}
		}
	}
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestProjectsViewLoadsAndNavigates(t *testing.T) {
	m := tui.New(newFake())
	var mod tea.Model = m
	mod = drive(mod, mod.Init()()) // pump Init's first cmd result through Update
	view := mod.View()
	if !contains(view, "app") || !contains(view, "web") {
		t.Fatalf("projects missing:\n%s", view)
	}
	mod = drive(mod, key("down"))
	if !contains(mod.View(), "> ") {
		t.Fatalf("cursor missing:\n%s", mod.View())
	}
}

func TestProjectsFilter(t *testing.T) {
	m := tui.New(newFake())
	var mod tea.Model = m
	mod = drive(mod, mod.Init()())
	mod = drive(mod, key("/"), key("w"), key("e"), key("enter"))
	view := mod.View()
	if contains(view, " app ") || !contains(view, "web") {
		t.Fatalf("filter failed:\n%s", view)
	}
}

func contains(s, sub string) bool { return len(s) > 0 && strings.Contains(s, sub) }

func TestDeleteSessionFlow(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter"))              // open first project
	mod = drive(mod, key("space"), key("down"), key("space")) // select both
	mod = drive(mod, key("d"))
	if !contains(mod.View(), "trash") { // confirm box shows
		t.Fatalf("confirm box missing:\n%s", mod.View())
	}
	mod = drive(mod, key("y"))
	if len(f.executed) != 1 || len(f.executed[0].Details) != 2 {
		t.Fatalf("executed %+v", f.executed)
	}
}

func TestConfirmCancel(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter"), key("d"), key("n"))
	if len(f.executed) != 0 {
		t.Fatal("cancel must not execute")
	}
}

func TestRenameFlow(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter"), key("r"))
	mod = drive(mod, key("h"), key("i"), key("enter"))
	if len(f.renamed) != 1 || f.renamed[0][2] != "hi" {
		t.Fatalf("renamed %+v", f.renamed)
	}
}

func TestProjectDeleteFlow(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("d"), key("y"))
	if len(f.executed) != 1 || f.executed[0].Label != "project -home-u-app" {
		t.Fatalf("executed %+v", f.executed)
	}
}

func TestPreviewFlow(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter")) // open first project
	mod = drive(mod, key("p"))
	view := mod.View()
	if !contains(view, "user: hello") {
		t.Fatalf("preview missing:\n%s", view)
	}
	mod = drive(mod, key("x")) // any key closes preview
	view = mod.View()
	if !contains(view, "first") || !contains(view, "space select") {
		t.Fatalf("expected sessions list after closing preview:\n%s", view)
	}
}

func TestRenameEmptyInputCancels(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter"), key("r"))
	mod = drive(mod, key("enter")) // nothing typed
	if len(f.renamed) != 0 {
		t.Fatalf("renamed %+v", f.renamed)
	}
	if !contains(mod.View(), "first") || !contains(mod.View(), "space select") {
		t.Fatalf("expected sessions list after cancel:\n%s", mod.View())
	}
}

func TestErrorRecoverable(t *testing.T) {
	f := newFake()
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter")) // open first project
	f.clearErr = errors.New("nothing to clear")
	mod = drive(mod, key("c"))
	view := mod.View()
	if !contains(view, "error:") {
		t.Fatalf("expected error screen:\n%s", view)
	}
	mod = drive(mod, key("x")) // any key goes back
	view = mod.View()
	if !contains(view, "first") || !contains(view, "space select") {
		t.Fatalf("expected sessions list after dismissing error:\n%s", view)
	}
	// subsequent delete flow still works
	mod = drive(mod, key("d"), key("y"))
	if len(f.executed) != 1 {
		t.Fatalf("expected execute after recovering from error: %+v", f.executed)
	}
}

func TestShrunkReloadNoPanic(t *testing.T) {
	f := newFake()
	f.emptyOnExecute = "-home-u-app"
	var mod tea.Model = tui.New(f)
	mod = drive(mod, mod.Init()(), key("enter")) // open first project
	mod = drive(mod, key("down"))                // cursor to index 1
	mod = drive(mod, key("d"))
	mod = drive(mod, key("y")) // executes; reload now sees an empty session list

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic after shrunk reload: %v", r)
		}
	}()
	mod = drive(mod, key("r"), key("p"), key("d"), key("space"))
	if len(f.renamed) != 0 {
		t.Fatalf("rename recorded on empty session list: %+v", f.renamed)
	}
	if len(f.executed) != 1 {
		t.Fatalf("unexpected extra execute: %+v", f.executed)
	}
}
