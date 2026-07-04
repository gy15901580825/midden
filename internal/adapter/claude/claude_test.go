package claude_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"midden/internal/adapter/claude"
	"midden/internal/testfix"
)

const (
	sidA = "11111111-1111-4111-8111-111111111111"
	sidB = "22222222-2222-4222-8222-222222222222"
)

var fixedNow = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

func newAdapter(root string) *claude.Adapter {
	a := claude.New(root)
	a.Now = func() time.Time { return fixedNow }
	return a
}

func TestProjectsScan(t *testing.T) {
	root := t.TempDir()
	p1 := testfix.Project(t, root, "-home-u-Workspace-app")
	f1 := testfix.Session(t, p1, sidA,
		testfix.UserLine("hello", "/home/u/Workspace/app"),
		testfix.AssistantLine("hi"))
	testfix.Session(t, p1, sidB, testfix.UserLine("second", "/home/u/Workspace/app"))
	testfix.Sidecar(t, p1, sidA, map[string]string{"tool-results/x.txt": "data"})
	// noise that must not count as sessions:
	testfix.Sidecar(t, p1, "memory", map[string]string{"MEMORY.md": "notes"})
	testfix.Session(t, p1, "not-a-uuid") // writes not-a-uuid.jsonl
	testfix.Touch(t, f1, fixedNow.Add(-time.Hour))

	testfix.Project(t, root, "-home-u-empty") // project with no sessions

	got, err := newAdapter(root).Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 projects, got %d", len(got))
	}
	p := got[0] // sorted by LastActive desc; empty project has zero time
	if p.ID != "-home-u-Workspace-app" {
		t.Fatalf("want app project first, got %s", p.ID)
	}
	if p.Sessions != 2 {
		t.Errorf("want 2 sessions, got %d", p.Sessions)
	}
	if p.Name != "app" || p.Dir != "/home/u/Workspace/app" {
		t.Errorf("want name=app dir from cwd, got name=%q dir=%q", p.Name, p.Dir)
	}
	if p.SizeBytes == 0 {
		t.Error("want non-zero size")
	}
	if got[1].Name != "home-u-empty" { // fallback: ID with leading '-' trimmed
		t.Errorf("fallback name wrong: %q", got[1].Name)
	}
}

func TestSessionsMetadata(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	f := testfix.Session(t, p, sidA,
		testfix.RawLine("mode"),
		testfix.UserLine("  fix the   bug\nplease  ", "/home/u/app"),
		testfix.AssistantLine("done"),
		testfix.TitleLine(sidA, "old title"),
		testfix.TitleLine(sidA, "new title"))
	testfix.Sidecar(t, p, sidA, map[string]string{"tool-results/r.txt": "0123456789"})
	testfix.Touch(t, f, fixedNow.Add(-2*time.Minute)) // active

	f2 := testfix.Session(t, p, sidB, testfix.UserStringLine("only a user message"))
	testfix.Touch(t, f2, fixedNow.Add(-48*time.Hour))

	got, err := newAdapter(root).Sessions("-home-u-app")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	s := got[0] // newest first
	if s.ID != sidA || s.Title != "new title" || !s.Active {
		t.Errorf("got %+v", s)
	}
	if s.Messages != 2 || s.Records != 5 || !s.HasAssistant {
		t.Errorf("counts wrong: %+v", s)
	}
	if s.SizeBytes <= 10 {
		t.Errorf("size must include sidecar, got %d", s.SizeBytes)
	}
	s2 := got[1]
	if s2.Title != "only a user message" || s2.Active || s2.HasAssistant {
		t.Errorf("got %+v", s2)
	}
}

func TestOrphansAndFindSession(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	testfix.Session(t, p, sidA, testfix.UserLine("hi", "/home/u/app"))
	testfix.Sidecar(t, p, sidA, map[string]string{"a.txt": "x"})  // has jsonl → not orphan
	testfix.Sidecar(t, p, sidB, map[string]string{"b.txt": "yy"}) // no jsonl → orphan
	testfix.Sidecar(t, p, "memory", map[string]string{"MEMORY.md": "m"})

	a := newAdapter(root)
	orphans, err := a.Orphans("-home-u-app")
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].ID != sidB || orphans[0].SizeBytes != 2 {
		t.Fatalf("got %+v", orphans)
	}

	pid, err := a.FindSession(sidA)
	if err != nil || pid != "-home-u-app" {
		t.Fatalf("got %q, %v", pid, err)
	}
	if _, err := a.FindSession(sidB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestPathsAndFence(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	testfix.Session(t, p, sidA, testfix.UserLine("hi", "/home/u/app"))
	testfix.Sidecar(t, p, sidA, map[string]string{"a.txt": "x"})
	testfix.Sidecar(t, p, "memory", map[string]string{"MEMORY.md": "m"})
	testfix.Session(t, p, sidB, testfix.UserLine("no sidecar", "/home/u/app"))

	a := newAdapter(root)

	paths, err := a.SessionPaths("-home-u-app", sidA)
	if err != nil || len(paths) != 2 {
		t.Fatalf("got %v, %v", paths, err)
	}
	paths, err = a.SessionPaths("-home-u-app", sidB)
	if err != nil || len(paths) != 1 {
		t.Fatalf("got %v, %v", paths, err)
	}
	if _, err := a.SessionPaths("-home-u-app", "33333333-3333-4333-8333-333333333333"); err == nil {
		t.Fatal("want error for missing session")
	}

	pp, err := a.ProjectPaths("-home-u-app")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pp {
		if filepath.Base(p) == "memory" {
			t.Fatal("memory/ must never be listed")
		}
	}
	if len(pp) != 3 { // sidA.jsonl, sidA/, sidB.jsonl
		t.Fatalf("want 3 paths, got %v", pp)
	}

	// fence: symlinked project dir escaping root is rejected
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "-evil")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := a.ProjectPaths("-evil"); err == nil {
		t.Fatal("want fence error for symlink escape")
	}
	// fence: traversal in pid rejected
	if _, err := a.ProjectPaths("../outside"); err == nil {
		t.Fatal("want error for path traversal")
	}
	// fence: symlink resolving exactly to root is rejected
	if err := os.Symlink(root, filepath.Join(root, "-self")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := a.ProjectPaths("-self"); err == nil {
		t.Fatal("want fence error for symlink resolving to root")
	}
}

func TestRenameAppendsTitle(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	f := testfix.Session(t, p, sidA, testfix.UserLine("hi", "/home/u/app"))
	before, _ := os.ReadFile(f)

	a := newAdapter(root)
	if err := a.Rename("-home-u-app", sidA, "my new title"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(f)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatal("rename must only append, not rewrite")
	}
	ss, _ := a.Sessions("-home-u-app")
	if ss[0].Title != "my new title" {
		t.Fatalf("got title %q", ss[0].Title)
	}
	if err := a.Rename("-home-u-app", sidB, "x"); err == nil {
		t.Fatal("want error for missing session")
	}
}

func TestPreview(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	lines := []string{testfix.RawLine("mode")}
	for i := 1; i <= 5; i++ {
		lines = append(lines,
			testfix.UserLine(fmt.Sprintf("q%d", i), "/home/u/app"),
			testfix.AssistantLine(fmt.Sprintf("a%d", i)))
	}
	testfix.Session(t, p, sidA, lines...)

	got, err := newAdapter(root).Preview("-home-u-app", sidA, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 messages (2 head + 2 tail), got %d", len(got))
	}
	if got[0].Text != "q1" || got[0].Role != "user" || got[3].Text != "a5" {
		t.Fatalf("got %+v", got)
	}
}

func TestClearCandidates(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	old := testfix.Session(t, p, sidA,
		testfix.UserLine("old full", "/home/u/app"), testfix.AssistantLine("x"),
		testfix.RawLine("mode"), testfix.RawLine("mode"), testfix.RawLine("mode"))
	testfix.Touch(t, old, fixedNow.Add(-40*24*time.Hour))
	empty := testfix.Session(t, p, sidB, testfix.UserLine("no reply", "/home/u/app"))
	testfix.Touch(t, empty, fixedNow.Add(-time.Hour))
	const sidC = "33333333-3333-4333-8333-333333333333"
	active := testfix.Session(t, p, sidC, testfix.UserLine("live", "/home/u/app"))
	testfix.Touch(t, active, fixedNow.Add(-time.Minute))
	testfix.Sidecar(t, p, "44444444-4444-4444-8444-444444444444", map[string]string{"x": "1"})

	a := newAdapter(root)

	got, _ := a.ClearCandidates("-home-u-app", claude.ClearRules{OlderThan: 30 * 24 * time.Hour})
	if len(got) != 1 || got[0].ID != sidA {
		t.Fatalf("older-than: got %+v", got)
	}
	got, _ = a.ClearCandidates("-home-u-app", claude.ClearRules{Empty: true})
	if len(got) != 1 || got[0].ID != sidB {
		t.Fatalf("empty: got %+v", got)
	}
	got, _ = a.ClearCandidates("-home-u-app", claude.ClearRules{Orphans: true})
	if len(got) != 1 || got[0].Kind != "orphan" {
		t.Fatalf("orphans: got %+v", got)
	}
	got, _ = a.ClearCandidates("-home-u-app", claude.ClearRules{All: true})
	if len(got) != 3 { // sidA + sidB + orphan; active sidC excluded
		t.Fatalf("all: want 3, got %+v", got)
	}
	for _, c := range got {
		if c.ID == sidC {
			t.Fatal("active session must never be a candidate")
		}
	}
}
