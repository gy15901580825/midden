package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aism/internal/cli"
	"aism/internal/testfix"
)

const (
	sidA = "11111111-1111-4111-8111-111111111111"
	sidB = "22222222-2222-4222-8222-222222222222"
)

// run executes the CLI with a fixture root and returns stdout.
func run(t *testing.T, root string, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("AISM_CLAUDE_ROOT", root)
	t.Setenv("AISM_TRASH_ROOT", filepath.Join(t.TempDir(), "trash"))
	cmd := cli.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func fixtureRoot(t *testing.T) string {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	// sidA: 5+ records (not empty), has assistant
	sidAPath := testfix.Session(t, p, sidA,
		testfix.UserLine("hello world", "/home/u/app"),
		testfix.AssistantLine("hi"),
		testfix.UserLine("next", "/home/u/app"),
		testfix.AssistantLine("ok"),
		testfix.UserLine("one more", "/home/u/app"))
	// sidB: < 5 records (empty), no assistant
	sidBPath := testfix.Session(t, p, sidB, testfix.UserLine("second one", "/home/u/app"))
	// set old mtimes so sessions are not marked as active
	oldTime := time.Now().Add(-1 * time.Hour)
	testfix.Touch(t, sidAPath, oldTime)
	testfix.Touch(t, sidBPath, oldTime)
	return root
}

func TestProjectsTableAndJSON(t *testing.T) {
	root := fixtureRoot(t)
	out, err := run(t, root, "", "projects")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "app") || !strings.Contains(out, "2") {
		t.Fatalf("table missing data:\n%s", out)
	}
	out, err = run(t, root, "", "projects", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["name"] != "app" {
		t.Fatalf("got %+v", got)
	}
}

func TestSessionsByProjectName(t *testing.T) {
	out, err := run(t, fixtureRoot(t), "", "sessions", "app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, sidA[:8]) || !strings.Contains(out, "hello world") {
		t.Fatalf("missing session row:\n%s", out)
	}
}

func TestUnknownProject(t *testing.T) {
	if _, err := run(t, fixtureRoot(t), "", "sessions", "nope"); err == nil {
		t.Fatal("want error for unknown project")
	}
}

func TestRenameCLI(t *testing.T) {
	root := fixtureRoot(t)
	if _, err := run(t, root, "", "rename", sidA, "better title"); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, root, "", "sessions", "app")
	if !strings.Contains(out, "better title") {
		t.Fatalf("title not applied:\n%s", out)
	}
}

func TestRemoveRequiresConfirmation(t *testing.T) {
	root := fixtureRoot(t)
	// answer "n" → aborted, file stays
	out, err := run(t, root, "n\n", "remove", sidA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "aborted") {
		t.Fatalf("want aborted notice:\n%s", out)
	}
	if o, _ := run(t, root, "", "sessions", "app"); !strings.Contains(o, sidA[:8]) {
		t.Fatal("session must still exist after 'n'")
	}
	// --yes → removed
	if _, err := run(t, root, "", "remove", sidA, "--yes"); err != nil {
		t.Fatal(err)
	}
	if o, _ := run(t, root, "", "sessions", "app"); strings.Contains(o, sidA[:8]) {
		t.Fatal("session must be gone after --yes")
	}
}

func TestRemoveActiveDoubleConfirm(t *testing.T) {
	root := t.TempDir()
	p := testfix.Project(t, root, "-home-u-app")
	testfix.Session(t, p, sidA, testfix.UserLine("hello world", "/home/u/app"), testfix.AssistantLine("hi"))
	// mtime left at "now" (not aged) → session.Active is true

	out, err := run(t, root, "y\nn\n", "remove", sidA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "aborted") {
		t.Fatalf("want aborted notice:\n%s", out)
	}
	if o, _ := run(t, root, "", "sessions", "app"); !strings.Contains(o, sidA[:8]) {
		t.Fatal("session must still exist after declining the active double-confirm")
	}

	out, err = run(t, root, "y\ny\n", "remove", sidA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "moved") {
		t.Fatalf("want moved notice:\n%s", out)
	}
	if o, _ := run(t, root, "", "sessions", "app"); strings.Contains(o, sidA[:8]) {
		t.Fatal("session must be gone after confirming twice")
	}
}

func TestRemoveDryRun(t *testing.T) {
	root := fixtureRoot(t)
	out, err := run(t, root, "", "remove", sidA, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("want dry-run notice:\n%s", out)
	}
	if o, _ := run(t, root, "", "sessions", "app"); !strings.Contains(o, sidA[:8]) {
		t.Fatal("dry-run must not remove")
	}
}

func TestRemoveProject(t *testing.T) {
	root := fixtureRoot(t)
	if _, err := run(t, root, "", "remove", "--project", "app", "--yes"); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, root, "", "projects")
	if strings.Contains(out, "-home-u-app") {
		t.Fatalf("project must be gone:\n%s", out)
	}
}

func TestRemoveAcrossProjects(t *testing.T) {
	root := t.TempDir()
	p1 := testfix.Project(t, root, "-home-u-app")
	testfix.Session(t, p1, sidA, testfix.UserLine("one", "/home/u/app"))
	p2 := testfix.Project(t, root, "-home-u-web")
	testfix.Session(t, p2, sidB, testfix.UserLine("two", "/home/u/web"))

	out, err := run(t, root, "", "remove", sidA, sidB, "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "moved 2 item(s)") {
		t.Fatalf("want 2 items moved:\n%s", out)
	}
	for _, proj := range []string{"app", "web"} {
		if o, _ := run(t, root, "", "sessions", proj); strings.Contains(o, sidA[:8]) || strings.Contains(o, sidB[:8]) {
			t.Fatalf("sessions must be gone from %s:\n%s", proj, o)
		}
	}
}

func TestClearEmptyRule(t *testing.T) {
	root := fixtureRoot(t) // sidB has no assistant reply → "empty"
	out, err := run(t, root, "", "clear", "app", "--empty", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "moved") {
		t.Fatalf("want moved notice:\n%s", out)
	}
	o, _ := run(t, root, "", "sessions", "app")
	if strings.Contains(o, sidB[:8]) || !strings.Contains(o, sidA[:8]) {
		t.Fatalf("wrong session cleared:\n%s", o)
	}
}

func TestClearNoFlagsClearsAllWithConfirm(t *testing.T) {
	root := fixtureRoot(t)
	if _, err := run(t, root, "y\n", "clear", "app"); err != nil {
		t.Fatal(err)
	}
	if o, _ := run(t, root, "", "sessions", "app"); strings.Contains(o, sidA[:8]) {
		t.Fatal("all sessions should be cleared")
	}
}

func TestTrashRoundTrip(t *testing.T) {
	root := fixtureRoot(t)
	trashRoot := filepath.Join(t.TempDir(), "trash")
	t.Setenv("AISM_TRASH_ROOT", trashRoot) // shared across runs below

	runShared := func(stdin string, args ...string) (string, error) {
		t.Helper()
		t.Setenv("AISM_CLAUDE_ROOT", root)
		cmd := cli.New()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}

	if _, err := runShared("", "remove", sidA, "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := runShared("", "trash", "list")
	if err != nil || !strings.Contains(out, "hello world") {
		t.Fatalf("trash list: %v\n%s", err, out)
	}
	// extract entry id from list --json
	out, _ = runShared("", "trash", "list", "--json")
	var entries []map[string]any
	json.Unmarshal([]byte(out), &entries)
	id := entries[0]["id"].(string)

	if _, err := runShared("", "trash", "restore", id); err != nil {
		t.Fatal(err)
	}
	if o, _ := runShared("", "sessions", "app"); !strings.Contains(o, sidA[:8]) {
		t.Fatal("session must be back after restore")
	}
	if _, err := runShared("", "trash", "empty", "--yes"); err != nil {
		t.Fatal(err)
	}
}

func TestTrashDryRun(t *testing.T) {
	root := fixtureRoot(t)
	trashRoot := filepath.Join(t.TempDir(), "trash")
	t.Setenv("AISM_TRASH_ROOT", trashRoot) // shared across runs below

	runShared := func(stdin string, args ...string) (string, error) {
		t.Helper()
		t.Setenv("AISM_CLAUDE_ROOT", root)
		cmd := cli.New()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}

	if _, err := runShared("", "remove", sidA, "--yes"); err != nil {
		t.Fatal(err)
	}
	out, _ := runShared("", "trash", "list", "--json")
	var entries []map[string]any
	json.Unmarshal([]byte(out), &entries)
	if len(entries) != 1 {
		t.Fatalf("want 1 trash entry, got %+v", entries)
	}
	id := entries[0]["id"].(string)

	// restore --dry-run: session must stay absent, entry must stay listed
	out, err := runShared("", "trash", "restore", id, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("want dry-run notice:\n%s", out)
	}
	if o, _ := runShared("", "sessions", "app"); strings.Contains(o, sidA[:8]) {
		t.Fatal("dry-run restore must not bring session back")
	}
	if o, _ := runShared("", "trash", "list"); !strings.Contains(o, id) {
		t.Fatalf("dry-run restore must leave entry listed:\n%s", o)
	}

	// empty --dry-run --yes: entry must stay intact
	out, err = runShared("", "trash", "empty", "--dry-run", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("want dry-run notice:\n%s", out)
	}
	if o, _ := runShared("", "trash", "list"); !strings.Contains(o, id) {
		t.Fatalf("dry-run empty must leave entry intact:\n%s", o)
	}
}
