// Package trash implements a restorable trash: Put moves paths into a
// timestamped entry dir with a manifest; nothing in aism hard-deletes
// user data except Empty().
package trash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type Item struct {
	Original string `json:"original"`
	Stored   string `json:"stored"`
}

type Entry struct {
	ID    string    `json:"id"`
	Time  time.Time `json:"time"`
	Tool  string    `json:"tool"`
	Label string    `json:"label"`
	Items []Item    `json:"items"`
}

type Trash struct {
	Root string
	Now  func() time.Time
}

func New(root string) *Trash { return &Trash{Root: root, Now: time.Now} }

func DefaultRoot() (string, error) {
	if v := os.Getenv("AISM_TRASH_ROOT"); v != "" {
		return v, nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "aism", "trash"), nil
}

func (t *Trash) Put(tool, label string, paths []string) (*Entry, error) {
	if err := os.MkdirAll(t.Root, 0o700); err != nil {
		return nil, err
	}
	ts := t.Now().UTC().Format("2006-01-02T15-04-05")
	id := ts
	var dir string
	for n := 1; ; n++ {
		dir = filepath.Join(t.Root, id)
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, err
		}
		id = fmt.Sprintf("%s-%d", ts, n)
	}
	e := &Entry{ID: id, Time: t.Now(), Tool: tool, Label: label}
	for _, p := range paths {
		stored := filepath.Base(p)
		if stored == "manifest.json" || stored == "manifest.json.tmp" {
			stored = "1-" + stored
		}
		for n := 1; ; n++ { // disambiguate same basename from different projects
			_, err := os.Stat(filepath.Join(dir, stored))
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return nil, err
			}
			stored = fmt.Sprintf("%d-%s", n, filepath.Base(p))
		}
		if err := move(p, filepath.Join(dir, stored)); err != nil {
			if len(e.Items) == 0 {
				os.RemoveAll(dir)
				return nil, fmt.Errorf("moving %s: %w", p, err)
			}
			return nil, fmt.Errorf("moving %s (partial entry %s retains %d already-moved item(s)): %w", p, id, len(e.Items), err)
		}
		e.Items = append(e.Items, Item{Original: p, Stored: stored})
		if err := writeManifest(dir, e); err != nil {
			return nil, fmt.Errorf("writing manifest (partial entry %s retains %d already-moved item(s)): %w", id, len(e.Items), err)
		}
	}
	return e, nil
}

func (t *Trash) List() ([]Entry, error) {
	entries, err := os.ReadDir(t.Root)
	if os.IsNotExist(err) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(t.Root, d.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var e Entry
		if json.Unmarshal(b, &e) == nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

func writeManifest(dir string, e *Entry) error {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "manifest.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "manifest.json"))
}

// move renames src to dst; on a cross-device link error it falls back
// to copy+delete. If the copy succeeds but deleting src fails, the dst
// copy is removed again so the filesystem stays consistent (src intact).
// Windows cross-device rename surfaces its own error, which is returned
// as-is rather than triggering the copy+delete fallback.
func move(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		os.RemoveAll(dst)
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return nil
}

func isCrossDevice(err error) bool {
	var le *os.LinkError
	return errors.As(err, &le) && errors.Is(le.Err, syscall.EXDEV)
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (t *Trash) Restore(entryID string) error {
	dir := filepath.Join(t.Root, entryID)
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("trash entry %s: %w", entryID, err)
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return fmt.Errorf("trash entry %s: parsing manifest: %w", entryID, err)
	}
	var conflicts []string
	for _, it := range e.Items {
		if isConflict(dir, it) {
			conflicts = append(conflicts, it.Original)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("refusing to restore, destinations exist: %v", conflicts)
	}
	if len(e.Items) == 0 {
		return removeEntry(dir, entryID)
	}
	for len(e.Items) > 0 {
		it := e.Items[0]
		storedPath := filepath.Join(dir, it.Stored)
		_, storedErr := os.Stat(storedPath)
		storedExists := storedErr == nil
		_, origErr := os.Stat(it.Original)
		origExists := origErr == nil

		if !storedExists {
			if !origExists {
				return fmt.Errorf("item missing from both trash and original location: %s (entry %s)", it.Original, entryID)
			}
			// Original exists but the stored file doesn't: this item was
			// already moved back by a previous partially-failed run. Treat
			// it as done rather than reporting a false conflict.
			e.Items = e.Items[1:]
			if len(e.Items) == 0 {
				return removeEntry(dir, entryID)
			}
			if err := writeManifest(dir, &e); err != nil {
				return fmt.Errorf("restoring %s (entry %s, %d item(s) still in trash): updating manifest: %w", it.Original, entryID, len(e.Items), err)
			}
			continue
		}
		// The pre-check above scans every item upfront so a refusal reports
		// the complete conflict list. That leaves a gap between check and
		// move where something else could create the destination; re-check
		// immediately before this item's move. This tool is single-user and
		// local, so the remaining (tiny) race is accepted rather than adding
		// locking.
		if origExists {
			return fmt.Errorf("refusing to restore %s: destination now exists (entry %s, %d item(s) still in trash)", it.Original, entryID, len(e.Items))
		}
		if err := os.MkdirAll(filepath.Dir(it.Original), 0o755); err != nil {
			return fmt.Errorf("restoring %s (entry %s, %d item(s) still in trash): %w", it.Original, entryID, len(e.Items), err)
		}
		if err := move(storedPath, it.Original); err != nil {
			return fmt.Errorf("restoring %s (entry %s, %d item(s) still in trash): %w", it.Original, entryID, len(e.Items), err)
		}
		e.Items = e.Items[1:]
		if len(e.Items) == 0 {
			return removeEntry(dir, entryID)
		}
		if err := writeManifest(dir, &e); err != nil {
			return fmt.Errorf("restoring %s (entry %s, %d item(s) still in trash): updating manifest: %w", it.Original, entryID, len(e.Items), err)
		}
	}
	return nil
}

// isConflict reports whether it's Original path is genuinely occupied by a
// prior restore's destination. Original existing alone isn't enough: if the
// Stored file is also gone from the entry dir, a previous partially-failed
// Restore already moved this item back, and the manifest is simply stale.
func isConflict(dir string, it Item) bool {
	if _, err := os.Stat(it.Original); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, it.Stored))
	return err == nil
}

func removeEntry(dir, entryID string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing restored entry %s: %w", entryID, err)
	}
	return nil
}

func (t *Trash) Empty() error {
	entries, err := os.ReadDir(t.Root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, d := range entries {
		if err := os.RemoveAll(filepath.Join(t.Root, d.Name())); err != nil {
			return err
		}
	}
	return nil
}
