package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"midden/internal/testfix"
)

const sid = "11111111-1111-4111-8111-111111111111"

func TestEndToEnd(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "midden")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	croot := t.TempDir()
	troot := filepath.Join(t.TempDir(), "trash")
	p := testfix.Project(t, croot, "-home-u-demo")
	testfix.Session(t, p, sid,
		testfix.UserLine("integration hello", "/home/u/demo"),
		testfix.AssistantLine("world"))

	env := append(os.Environ(), "MIDDEN_CLAUDE_ROOT="+croot, "MIDDEN_TRASH_ROOT="+troot)
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	if out := run("projects", "--json"); !strings.Contains(out, "demo") {
		t.Fatalf("projects: %s", out)
	}
	run("rename", sid, "e2e title")
	if out := run("sessions", "demo"); !strings.Contains(out, "e2e title") {
		t.Fatalf("rename not visible: %s", out)
	}
	run("remove", sid, "--yes")
	if out := run("sessions", "--", "-home-u-demo"); strings.Contains(out, sid[:8]) { // project is empty here — friendly name unavailable, resolve by id
		t.Fatalf("remove failed: %s", out)
	}
	var entries []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(run("trash", "list", "--json")), &entries)
	if len(entries) != 1 {
		t.Fatal("want one trash entry")
	}
	run("trash", "restore", entries[0].ID)
	if out := run("sessions", "demo"); !strings.Contains(out, "e2e title") {
		t.Fatalf("restore failed: %s", out)
	}
}
