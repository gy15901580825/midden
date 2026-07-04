// Package testfix builds fake ~/.claude/projects trees for tests.
package testfix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func Project(t *testing.T, root, id string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Session writes <sid>.jsonl from lines and returns its path.
func Session(t *testing.T, projDir, sid string, lines ...string) string {
	t.Helper()
	p := filepath.Join(projDir, sid+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Sidecar creates <sid>/ with the given relative files and returns its path.
func Sidecar(t *testing.T, projDir, sid string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(projDir, sid)
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Touch sets mtime (and atime) of path.
func Touch(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func jstr(s string) string { b, _ := json.Marshal(s); return string(b) }

func UserLine(text, cwd string) string {
	return fmt.Sprintf(`{"type":"user","cwd":%s,"message":{"role":"user","content":[{"type":"text","text":%s}]}}`,
		jstr(cwd), jstr(text))
}

// UserStringLine uses the string-content variant of a user record.
func UserStringLine(text string) string {
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%s}}`, jstr(text))
}

func AssistantLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]}}`, jstr(text))
}

func TitleLine(sid, title string) string {
	return fmt.Sprintf(`{"type":"ai-title","aiTitle":%s,"sessionId":%s}`, jstr(title), jstr(sid))
}

func RawLine(typ string) string { return fmt.Sprintf(`{"type":%s}`, jstr(typ)) }
