package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"midden/internal/adapter/claude"
	"midden/internal/app"
	"midden/internal/testfix"
	"midden/internal/trash"
)

const (
	sidA = "11111111-1111-4111-8111-111111111111"
	sidB = "22222222-2222-4222-8222-222222222222"
)

var fixedNow = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

func setup(t *testing.T, dryRun bool) (*app.App, string) {
	root := t.TempDir()
	c := claude.New(root)
	c.Now = func() time.Time { return fixedNow }
	tr := trash.New(filepath.Join(t.TempDir(), "trash"))
	tr.Now = c.Now
	p := testfix.Project(t, root, "-home-u-app")
	f := testfix.Session(t, p, sidA,
		testfix.UserLine("hello", "/home/u/app"), testfix.AssistantLine("hi"))
	testfix.Touch(t, f, fixedNow.Add(-time.Hour))
	testfix.Sidecar(t, p, sidA, map[string]string{"tool-results/x.txt": "abc"})
	testfix.Sidecar(t, p, "memory", map[string]string{"MEMORY.md": "keep me"})
	return app.New(c, tr, dryRun), p
}

func TestGatherAndExecuteSessions(t *testing.T) {
	a, projDir := setup(t, false)
	plan, err := a.GatherSessions("-home-u-app", []string{sidA})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) != 2 || plan.SizeBytes == 0 || len(plan.Details) != 1 {
		t.Fatalf("plan wrong: %+v", plan)
	}
	res, err := a.Execute(plan)
	if err != nil || res.EntryID == "" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(projDir, sidA+".jsonl")); !os.IsNotExist(err) {
		t.Fatal("session must be moved away")
	}
	if _, err := os.Stat(filepath.Join(projDir, "memory", "MEMORY.md")); err != nil {
		t.Fatal("memory/ must survive")
	}
}

func TestDryRunMovesNothing(t *testing.T) {
	a, projDir := setup(t, true)
	plan, _ := a.GatherSessions("-home-u-app", []string{sidA})
	res, err := a.Execute(plan)
	if err != nil || res.EntryID != "" {
		t.Fatalf("dry-run must not create entry: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(projDir, sidA+".jsonl")); err != nil {
		t.Fatal("dry-run must not move files")
	}
}

func TestGatherProjectKeepsMemoryAndRemovesEmptyDir(t *testing.T) {
	a, projDir := setup(t, false)
	plan, err := a.GatherProject("-home-u-app")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Paths {
		if strings.Contains(p, "memory") {
			t.Fatal("memory/ in project plan")
		}
	}
	if _, err := a.Execute(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(projDir); err != nil {
		t.Fatal("project dir with memory/ must remain")
	}

	// a project without memory/ disappears entirely
	root := a.Claude.Root
	p2 := testfix.Project(t, root, "-home-u-tmp")
	testfix.Session(t, p2, sidB, testfix.UserLine("x", "/home/u/tmp"))
	plan2, _ := a.GatherProject("-home-u-tmp")
	if _, err := a.Execute(plan2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Fatal("empty project dir must be removed")
	}
}

func TestGatherSessionsMarksActive(t *testing.T) {
	a, p := setup(t, false)
	f := testfix.Session(t, p, sidB, testfix.UserLine("live", "/home/u/app"), testfix.AssistantLine("hi"))
	testfix.Touch(t, f, fixedNow) // modified "now" → within claude.ActiveWindow

	plan, err := a.GatherSessions("-home-u-app", []string{sidB})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ActiveCount != 1 {
		t.Fatalf("want ActiveCount 1, got %d (details %+v)", plan.ActiveCount, plan.Details)
	}
	if !strings.Contains(plan.Details[0], "⚡ ACTIVE") {
		t.Fatalf("details missing ACTIVE marker: %+v", plan.Details)
	}
}

func TestGatherClearNothing(t *testing.T) {
	a, _ := setup(t, false)
	// sidA is 1h old with assistant → no rule matches
	_, err := a.GatherClear("-home-u-app", claude.ClearRules{OlderThan: 30 * 24 * time.Hour})
	if err == nil || !strings.Contains(err.Error(), "nothing to clear") {
		t.Fatalf("want nothing-to-clear error, got %v", err)
	}
}
