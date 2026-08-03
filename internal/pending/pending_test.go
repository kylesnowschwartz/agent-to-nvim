package pending

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
)

func TestBeginPlacesExitCodeFileBesideTheRecord(t *testing.T) {
	store, dir := openTestStore(t)

	edit, err := store.Begin("/tmp/draft.md", "abc123")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if got := filepath.Dir(edit.Window.ExitCodeFile); got != dir {
		t.Errorf("exit-code file in %q, want it in the record directory %q", got, dir)
	}
	// The exit-code file appearing is the signal that the edit finished, so it
	// must not exist yet.
	if _, err := os.Stat(edit.Window.ExitCodeFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("exit-code file already exists: %v", err)
	}
}

func TestBeginAllocatesDistinctIDs(t *testing.T) {
	store, _ := openTestStore(t)

	seen := map[string]bool{}
	for range 20 {
		edit, err := store.Begin("/tmp/draft.md", "abc123")
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if seen[edit.ID] {
			t.Fatalf("id %q handed out twice", edit.ID)
		}
		seen[edit.ID] = true
	}
}

func TestRememberAndFindRoundTrip(t *testing.T) {
	store, _ := openTestStore(t)

	edit, err := store.Begin("/tmp/draft.md", "abc123")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	edit.Window.WindowID = "@42"
	edit.Window.ReturnToWindow = "@7"
	if err := store.Remember(edit); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	found, err := store.Find(edit.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.DraftPath != edit.DraftPath || found.Fingerprint != edit.Fingerprint {
		t.Errorf("draft = %+v, want %+v", found, edit)
	}
	if found.Window != edit.Window {
		t.Errorf("window = %+v, want %+v", found.Window, edit.Window)
	}
}

func TestFindUnknownIDReportsUnknownEdit(t *testing.T) {
	store, _ := openTestStore(t)

	if _, err := store.Find("abcdef0123"); !errors.Is(err, ErrUnknownEdit) {
		t.Errorf("Find error = %v, want ErrUnknownEdit", err)
	}
}

// Ids arrive on the command line and get joined into a path, so anything that is
// not an id must be turned away rather than followed.
func TestFindRejectsPathsDisguisedAsIDs(t *testing.T) {
	store, _ := openTestStore(t)

	for _, id := range []string{"../elsewhere", "..", "/etc/passwd", "not-hex"} {
		if _, err := store.Find(id); !errors.Is(err, ErrUnknownEdit) {
			t.Errorf("Find(%q) error = %v, want ErrUnknownEdit", id, err)
		}
	}
}

func TestForgetRemovesRecordAndExitCodeFile(t *testing.T) {
	store, _ := openTestStore(t)

	edit, err := store.Begin("/tmp/draft.md", "abc123")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	edit.Window.WindowID = "@42"
	if err := store.Remember(edit); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if err := os.WriteFile(edit.Window.ExitCodeFile, []byte("0"), 0o600); err != nil {
		t.Fatalf("write exit code: %v", err)
	}

	store.Forget(edit.ID)

	if _, err := store.Find(edit.ID); !errors.Is(err, ErrUnknownEdit) {
		t.Errorf("Find after Forget error = %v, want ErrUnknownEdit", err)
	}
	if _, err := os.Stat(edit.Window.ExitCodeFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("exit-code file survived Forget: %v", err)
	}
}

func TestRememberDropsAbandonedRecordsOnly(t *testing.T) {
	store, dir := openTestStore(t)

	abandoned, err := store.Begin("/tmp/old.md", "abc123")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := store.Remember(abandoned); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	stale := time.Now().Add(-forgetAfter - time.Hour)
	recordPath := filepath.Join(dir, abandoned.ID+".json")
	if err := os.Chtimes(recordPath, stale, stale); err != nil {
		t.Fatalf("age record: %v", err)
	}

	fresh, err := store.Begin("/tmp/new.md", "def456")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := store.Remember(fresh); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	if _, err := store.Find(abandoned.ID); !errors.Is(err, ErrUnknownEdit) {
		t.Errorf("abandoned record survived: %v", err)
	}
	if _, err := store.Find(fresh.ID); err != nil {
		t.Errorf("fresh record dropped: %v", err)
	}
}

func TestHandleSurvivesTheRecord(t *testing.T) {
	store, _ := openTestStore(t)

	edit, err := store.Begin("/tmp/draft.md", "abc123")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	edit.Window = editwindow.Handle{
		WindowID:       "@42",
		ExitCodeFile:   edit.Window.ExitCodeFile,
		ReturnToWindow: "@7",
	}
	if err := store.Remember(edit); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	found, err := store.Find(edit.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if _, err := editwindow.Reattach(found.Window); err != nil && os.Getenv("TMUX") != "" {
		t.Errorf("recorded handle is not enough to reattach: %v", err)
	}
}

func openTestStore(t *testing.T) (store *Store, dir string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)

	store, err := OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store, filepath.Join(base, "agent-to-nvim")
}
