package trash_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aism/internal/trash"
)

var fixedNow = time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

func newTrash(t *testing.T) *trash.Trash {
	tr := trash.New(filepath.Join(t.TempDir(), "trash"))
	tr.Now = func() time.Time { return fixedNow }
	return tr
}

func TestPutAndList(t *testing.T) {
	src := t.TempDir()
	f := filepath.Join(src, "s.jsonl")
	os.WriteFile(f, []byte("data"), 0o600)
	d := filepath.Join(src, "s")
	os.MkdirAll(filepath.Join(d, "sub"), 0o755)
	os.WriteFile(filepath.Join(d, "sub", "x.txt"), []byte("x"), 0o600)

	tr := newTrash(t)
	e, err := tr.Put("claude", "my session", []string{f, d})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("original file must be gone")
	}
	if _, err := os.Stat(filepath.Join(tr.Root, e.ID, "s.jsonl")); err != nil {
		t.Fatal("file must be in trash entry dir")
	}
	if _, err := os.Stat(filepath.Join(tr.Root, e.ID, "s", "sub", "x.txt")); err != nil {
		t.Fatal("dir tree must be preserved in trash")
	}

	// second Put same second → collision suffix
	f2 := filepath.Join(src, "t.jsonl")
	os.WriteFile(f2, []byte("d2"), 0o600)
	e2, err := tr.Put("claude", "another", []string{f2})
	if err != nil {
		t.Fatal(err)
	}
	if e2.ID == e.ID {
		t.Fatal("entry IDs must be unique")
	}

	list, err := tr.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("got %d entries, err %v", len(list), err)
	}
	if list[0].Label == "" || len(list[0].Items) == 0 {
		t.Fatalf("manifest not round-tripped: %+v", list[0])
	}
}

func TestPutBasenameCollision(t *testing.T) {
	src1 := t.TempDir()
	src2 := t.TempDir()
	f1 := filepath.Join(src1, "same.txt")
	f2 := filepath.Join(src2, "same.txt")
	os.WriteFile(f1, []byte("one"), 0o600)
	os.WriteFile(f2, []byte("two"), 0o600)

	tr := newTrash(t)
	e, err := tr.Put("claude", "collision", []string{f1, f2})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Items) != 2 {
		t.Fatalf("want 2 items, got %d: %+v", len(e.Items), e.Items)
	}
	if e.Items[0].Stored == e.Items[1].Stored {
		t.Fatalf("stored names must differ for same-basename items: %+v", e.Items)
	}
	for _, it := range e.Items {
		if _, err := os.Stat(filepath.Join(tr.Root, e.ID, it.Stored)); err != nil {
			t.Fatalf("stored file %q missing: %v", it.Stored, err)
		}
	}
	if e.Items[0].Original != f1 || e.Items[1].Original != f2 {
		t.Fatalf("original paths mismatch: %+v", e.Items)
	}

	list, err := tr.List()
	if err != nil || len(list) != 1 || len(list[0].Items) != 2 {
		t.Fatalf("manifest round-trip failed: list=%+v err=%v", list, err)
	}
}

func TestPutReservedManifestName(t *testing.T) {
	src := t.TempDir()
	f := filepath.Join(src, "manifest.json")
	if err := os.WriteFile(f, []byte("not a trash manifest"), 0o600); err != nil {
		t.Fatal(err)
	}

	tr := newTrash(t)
	e, err := tr.Put("claude", "reserved name", []string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Items) != 1 {
		t.Fatalf("want 1 item, got %d: %+v", len(e.Items), e.Items)
	}
	if e.Items[0].Stored == "manifest.json" {
		t.Fatalf("stored name must not collide with the entry's own manifest: %+v", e.Items[0])
	}

	b, err := os.ReadFile(filepath.Join(tr.Root, e.ID, "manifest.json"))
	if err != nil {
		t.Fatalf("real manifest.json must exist: %v", err)
	}
	var got trash.Entry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("real manifest.json must be valid JSON: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("manifest should list exactly 1 item, got %d: %+v", len(got.Items), got.Items)
	}

	if err := tr.Restore(e.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(f); err != nil || string(b) != "not a trash manifest" {
		t.Fatalf("restore must round-trip the original manifest.json file: %v, %q", err, b)
	}
}

func TestDefaultRoot(t *testing.T) {
	t.Run("explicit root wins", func(t *testing.T) {
		t.Setenv("AISM_TRASH_ROOT", "/custom/root")
		got, err := trash.DefaultRoot()
		if err != nil {
			t.Fatal(err)
		}
		if got != "/custom/root" {
			t.Fatalf("got %q, want /custom/root", got)
		}
	})

	t.Run("falls back to XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("AISM_TRASH_ROOT", "")
		t.Setenv("XDG_DATA_HOME", "/x")
		got, err := trash.DefaultRoot()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join("/x", "aism", "trash")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("AISM_TRASH_ROOT wins over XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("AISM_TRASH_ROOT", "/custom")
		t.Setenv("XDG_DATA_HOME", "/xdg")
		got, err := trash.DefaultRoot()
		if err != nil {
			t.Fatal(err)
		}
		if got != "/custom" {
			t.Fatalf("got %q, want /custom", got)
		}
	})

	t.Run("uses .local/share when both env vars empty", func(t *testing.T) {
		t.Setenv("AISM_TRASH_ROOT", "")
		t.Setenv("XDG_DATA_HOME", "")
		got, err := trash.DefaultRoot()
		if err != nil {
			t.Fatal(err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".local", "share", "aism", "trash")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestPutPartialFailureKeepsManifest(t *testing.T) {
	src := t.TempDir()
	f := filepath.Join(src, "keep.txt")
	os.WriteFile(f, []byte("data"), 0o600)

	tr := newTrash(t)
	_, err := tr.Put("claude", "partial", []string{f, "/nonexistent/path/x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "partial entry") {
		t.Fatalf("error should mention the partial entry: %v", err)
	}

	id := fixedNow.UTC().Format("2006-01-02T15-04-05")
	dir := filepath.Join(tr.Root, id)
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("entry dir should still exist: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "keep.txt")); statErr != nil {
		t.Fatalf("already-moved file should remain in entry dir: %v", statErr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json should exist: %v", err)
	}
	var got trash.Entry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("manifest should list exactly 1 item, got %d: %+v", len(got.Items), got.Items)
	}
}

func TestRestoreAndEmpty(t *testing.T) {
	src := t.TempDir()
	f := filepath.Join(src, "s.jsonl")
	os.WriteFile(f, []byte("data"), 0o600)

	tr := newTrash(t)
	e, _ := tr.Put("claude", "sess", []string{f})

	if err := tr.Restore(e.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(f); err != nil || string(b) != "data" {
		t.Fatal("restore must recreate original file")
	}
	if list, _ := tr.List(); len(list) != 0 {
		t.Fatal("restored entry must be removed from trash")
	}

	// conflict: original exists → refuse, trash intact
	e2, _ := tr.Put("claude", "sess", []string{f})
	os.WriteFile(f, []byte("newer"), 0o600)
	if err := tr.Restore(e2.ID); err == nil {
		t.Fatal("want conflict error")
	}
	if list, _ := tr.List(); len(list) != 1 {
		t.Fatal("failed restore must not consume the entry")
	}

	if err := tr.Empty(); err != nil {
		t.Fatal(err)
	}
	if list, _ := tr.List(); len(list) != 0 {
		t.Fatal("empty must clear all entries")
	}

	if err := tr.Restore("nope"); err == nil {
		t.Fatal("want error for unknown entry")
	}
}

func TestRestorePartialFailureIsRetryable(t *testing.T) {
	srcA := t.TempDir()
	srcB := t.TempDir()
	a := filepath.Join(srcA, "a.txt")
	os.WriteFile(a, []byte("a-data"), 0o600)
	if err := os.MkdirAll(filepath.Join(srcB, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(srcB, "sub", "b.txt")
	os.WriteFile(b, []byte("b-data"), 0o600)

	tr := newTrash(t)
	// Put preserves argument order: a restores before b.
	e, err := tr.Put("claude", "two items", []string{a, b})
	if err != nil {
		t.Fatal(err)
	}

	// Blow away srcB and put a regular file where b's parent dir needs
	// to be recreated, so MkdirAll for item b fails with ENOTDIR.
	if err := os.RemoveAll(srcB); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcB, 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(srcB, "sub")
	if err := os.WriteFile(blocker, []byte("blocking file"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = tr.Restore(e.ID)
	if err == nil {
		t.Fatal("expected error from partial restore failure")
	}
	if !strings.Contains(err.Error(), e.ID) {
		t.Fatalf("error should mention the entry id %q: %v", e.ID, err)
	}
	if got, err := os.ReadFile(a); err != nil || string(got) != "a-data" {
		t.Fatalf("item a should be restored on disk: %v, %q", err, got)
	}

	list, err := tr.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("entry should still be listed, got %d entries, err %v", len(list), err)
	}
	if len(list[0].Items) != 1 || list[0].Items[0].Original != b {
		t.Fatalf("manifest should list exactly 1 remaining item (b): %+v", list[0].Items)
	}

	// Unblock and retry: full success this time.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := tr.Restore(e.ID); err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if list, _ := tr.List(); len(list) != 0 {
		t.Fatal("entry should be gone from trash after full restore")
	}
	if got, err := os.ReadFile(b); err != nil || string(got) != "b-data" {
		t.Fatalf("item b should be restored on disk: %v, %q", err, got)
	}
}

func TestRestoreRecreatesMissingParent(t *testing.T) {
	src := t.TempDir()
	sub := filepath.Join(src, "sub")
	os.MkdirAll(sub, 0o755)
	f := filepath.Join(sub, "f.txt")
	os.WriteFile(f, []byte("data"), 0o600)

	tr := newTrash(t)
	e, err := tr.Put("claude", "one", []string{f})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}

	if err := tr.Restore(e.ID); err != nil {
		t.Fatalf("restore should recreate missing parent dir: %v", err)
	}
	if got, err := os.ReadFile(f); err != nil || string(got) != "data" {
		t.Fatalf("file should be restored: %v, %q", err, got)
	}
}

func TestRestoreSelfHealsStaleManifest(t *testing.T) {
	srcA := t.TempDir()
	srcB := t.TempDir()
	a := filepath.Join(srcA, "a.txt")
	b := filepath.Join(srcB, "b.txt")
	os.WriteFile(a, []byte("a-data"), 0o600)
	os.WriteFile(b, []byte("b-data"), 0o600)

	tr := newTrash(t)
	if _, err := tr.Put("claude", "two items", []string{a, b}); err != nil {
		t.Fatal(err)
	}

	list, err := tr.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d, err %v", len(list), err)
	}
	entry := list[0]
	if len(entry.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(entry.Items), entry.Items)
	}
	var storedA string
	for _, it := range entry.Items {
		if it.Original == a {
			storedA = it.Stored
		}
	}
	if storedA == "" {
		t.Fatalf("could not find stored name for %q: %+v", a, entry.Items)
	}

	// Poison the manifest: move a's stored file back to its original
	// location directly, without going through Restore, so the manifest
	// still lists it as "in trash" even though it physically isn't.
	if err := os.MkdirAll(filepath.Dir(a), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(tr.Root, entry.ID, storedA), a); err != nil {
		t.Fatal(err)
	}

	if err := tr.Restore(entry.ID); err != nil {
		t.Fatalf("restore should self-heal the stale manifest, not report a false conflict: %v", err)
	}
	if got, err := os.ReadFile(a); err != nil || string(got) != "a-data" {
		t.Fatalf("a should remain restored: %v, %q", err, got)
	}
	if got, err := os.ReadFile(b); err != nil || string(got) != "b-data" {
		t.Fatalf("b should be restored: %v, %q", err, got)
	}
	if list, _ := tr.List(); len(list) != 0 {
		t.Fatal("entry should be gone from trash after self-healing restore")
	}
}
