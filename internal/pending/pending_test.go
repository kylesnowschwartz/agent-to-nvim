package pending

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
)

func TestBeginPlacesExitCodeFileBesideTheRecord(t *testing.T) {
	store, dir := openTestStore(t)

	edit, err := store.Begin("/tmp/draft.md")
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
		edit, err := store.Begin("/tmp/draft.md")
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

	edit := remember(t, store, "/tmp/draft.md", "hey team\n")

	found, original, err := store.Find(edit.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.DraftPath != edit.DraftPath {
		t.Errorf("draft = %+v, want %+v", found, edit)
	}
	if found.Window != edit.Window {
		t.Errorf("window = %+v, want %+v", found.Window, edit.Window)
	}
	if string(original) != "hey team\n" {
		t.Errorf("original = %q, want the text handed over", original)
	}
}

// The draft copy is a file rather than a record field so that bytes JSON would
// rewrite survive: a mangled original reads as an edit the human never made.
func TestFindReturnsBytesJSONWouldRewrite(t *testing.T) {
	store, _ := openTestStore(t)

	handedOver := []byte{'h', 'i', ' ', 0xff, 0xfe, '\n'}
	edit, err := store.Begin("/tmp/draft.md")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := store.Remember(edit, handedOver); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	_, original, err := store.Find(edit.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !bytes.Equal(original, handedOver) {
		t.Errorf("original = %v, want the exact bytes %v", original, handedOver)
	}
}

func TestFindReportsAMissingDraftCopy(t *testing.T) {
	store, dir := openTestStore(t)

	edit := remember(t, store, "/tmp/draft.md", "hey team\n")
	if err := os.Remove(filepath.Join(dir, edit.ID+".orig")); err != nil {
		t.Fatalf("remove draft copy: %v", err)
	}

	if _, _, err := store.Find(edit.ID); err == nil {
		t.Error("expected an error when the draft copy is gone")
	}
}

func TestFindUnknownIDReportsUnknownEdit(t *testing.T) {
	store, _ := openTestStore(t)

	if _, _, err := store.Find("abcdef0123"); !errors.Is(err, ErrUnknownEdit) {
		t.Errorf("Find error = %v, want ErrUnknownEdit", err)
	}
}

// Ids arrive on the command line and get joined into a path, so anything that is
// not an id must be turned away rather than followed.
func TestFindRejectsPathsDisguisedAsIDs(t *testing.T) {
	store, _ := openTestStore(t)

	for _, id := range []string{"../elsewhere", "..", "/etc/passwd", "not-hex"} {
		if _, _, err := store.Find(id); !errors.Is(err, ErrUnknownEdit) {
			t.Errorf("Find(%q) error = %v, want ErrUnknownEdit", id, err)
		}
	}
}

func TestForgetRemovesEverythingBesideTheRecord(t *testing.T) {
	store, dir := openTestStore(t)

	edit := remember(t, store, "/tmp/draft.md", "hey team\n")
	if err := os.WriteFile(edit.Window.ExitCodeFile, []byte("0"), 0o600); err != nil {
		t.Fatalf("write exit code: %v", err)
	}

	store.Forget(edit.ID)

	if _, _, err := store.Find(edit.ID); !errors.Is(err, ErrUnknownEdit) {
		t.Errorf("Find after Forget error = %v, want ErrUnknownEdit", err)
	}
	for _, name := range []string{edit.ID + ".orig", edit.ID + ".rc"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived Forget: %v", name, err)
		}
	}
}

func TestRememberDropsAbandonedRecordsOnly(t *testing.T) {
	store, dir := openTestStore(t)

	abandoned := remember(t, store, "/tmp/old.md", "old draft\n")
	stale := time.Now().Add(-forgetAfter - time.Hour)
	recordPath := filepath.Join(dir, abandoned.ID+".json")
	if err := os.Chtimes(recordPath, stale, stale); err != nil {
		t.Fatalf("age record: %v", err)
	}

	fresh := remember(t, store, "/tmp/new.md", "new draft\n")

	if _, _, err := store.Find(abandoned.ID); !errors.Is(err, ErrUnknownEdit) {
		t.Errorf("abandoned record survived: %v", err)
	}
	if _, _, err := store.Find(fresh.ID); err != nil {
		t.Errorf("fresh record dropped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, abandoned.ID+".orig")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("abandoned draft copy survived: %v", err)
	}
}

func TestHandleSurvivesTheRecord(t *testing.T) {
	store, _ := openTestStore(t)

	edit, err := store.Begin("/tmp/draft.md")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	edit.Window = editwindow.Handle{
		WindowID:       "@42",
		ExitCodeFile:   edit.Window.ExitCodeFile,
		ReturnToWindow: "@7",
	}
	if err := store.Remember(edit, []byte("hey team\n")); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	found, _, err := store.Find(edit.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if _, err := editwindow.Reattach(found.Window); err != nil && os.Getenv("TMUX") != "" {
		t.Errorf("recorded handle is not enough to reattach: %v", err)
	}
}

func remember(t *testing.T, store *Store, draftPath, original string) Edit {
	t.Helper()
	edit, err := store.Begin(draftPath)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	edit.Window.WindowID = "@42"
	edit.Window.ReturnToWindow = "@7"
	if err := store.Remember(edit, []byte(original)); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	return edit
}

func TestDropScratchRemovesADraftFromTheDraftsDirectory(t *testing.T) {
	store, _ := openTestStore(t)
	path := filepath.Join(store.DraftsDir(), "slack-launch-announcement.md")
	if err := os.WriteFile(path, []byte("hey team\n"), 0o600); err != nil {
		t.Fatalf("write scratch draft: %v", err)
	}

	store.DropScratch(path)

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("scratch draft survived the handover: %v", err)
	}
}

// Handing over a file of the user's own is an edit to that file, so it stays put
// however the edit ended.
func TestDropScratchKeepsADraftFromAnywhereElse(t *testing.T) {
	store, _ := openTestStore(t)
	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(path, []byte("hey team\n"), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	store.DropScratch(path)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("draft outside the drafts directory was removed: %v", err)
	}
}

func TestHoldPlanWritesThePlanUnderTheStoresOwnDirectory(t *testing.T) {
	store, dir := openTestStore(t)
	plan := "# Launch\n\nShip on Thursday.\n"

	path, err := store.HoldPlan("/Users/someone/.claude/plans/tidy-launch.md", plan)
	if err != nil {
		t.Fatalf("HoldPlan: %v", err)
	}

	if want := filepath.Join(dir, "plans"); filepath.Dir(path) != want {
		t.Errorf("plan held at %q, want it under %q", path, want)
	}
	if want := "tidy-launch.md"; filepath.Base(path) != want {
		t.Errorf("plan named %q, want %q so the window title says what it is", filepath.Base(path), want)
	}
	held, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the held plan: %v", err)
	}
	if string(held) != plan {
		t.Errorf("held plan = %q, want %q", held, plan)
	}
}

// The name comes from the harness, so it can only ever land on one file inside
// PlansDir however it is spelled.
func TestHoldPlanCannotBeAimedOutOfThePlansDirectory(t *testing.T) {
	store, dir := openTestStore(t)
	plans := filepath.Join(dir, "plans")

	for _, name := range []string{
		"../../../etc/passwd",
		"/absolute/elsewhere.md",
		"..",
		"",
		"  ",
		"plan/../../escape.md",
	} {
		path, err := store.HoldPlan(name, "# Launch\n")
		if err != nil {
			t.Fatalf("HoldPlan(%q): %v", name, err)
		}
		if filepath.Dir(path) != plans {
			t.Errorf("HoldPlan(%q) landed at %q, want it inside %q", name, path, plans)
		}
	}
}

// A plan reviewed twice is the same plan being worked on, so its copy is replaced
// rather than piling up one file per revision.
func TestHoldPlanReplacesAnEarlierCopyOfTheSamePlan(t *testing.T) {
	store, _ := openTestStore(t)

	first, err := store.HoldPlan("launch.md", "# Launch\n\nThursday.\n")
	if err != nil {
		t.Fatalf("HoldPlan: %v", err)
	}
	second, err := store.HoldPlan("launch.md", "# Launch\n\nFriday.\n")
	if err != nil {
		t.Fatalf("HoldPlan: %v", err)
	}
	if first != second {
		t.Errorf("second review held at %q, want the same file as %q", second, first)
	}

	held, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "# Launch\n\nFriday.\n"; string(held) != want {
		t.Errorf("held plan = %q, want the revision %q", held, want)
	}
}

func TestSafeNameKeepsAReadableNameReadable(t *testing.T) {
	for name, want := range map[string]string{
		"tidy-launch-plan.md": "tidy-launch-plan",
		"peppy_cooking_wave":  "peppy_cooking_wave",
		"plan with spaces.md": "plan-with-spaces",
		"../weird/../name.md": "name",
		"":                    "plan-under-review",
		"!!!":                 "plan-under-review",
		"café-launch":         "caf--launch",
	} {
		if got := safeName(name); got != want {
			t.Errorf("safeName(%q) = %q, want %q", name, got, want)
		}
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

func TestKeepNotesWritesAReadableCopy(t *testing.T) {
	store, _ := openTestStore(t)

	path, err := store.KeepNotes("5ce4bf832a", "notes:\n>> shorten this\n")
	if err != nil {
		t.Fatalf("KeepNotes: %v", err)
	}
	if filepath.Dir(path) != store.NotesDir() {
		t.Errorf("notes kept in %q, want %q", filepath.Dir(path), store.NotesDir())
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read kept notes: %v", err)
	}
	if want := "notes:\n>> shorten this\n"; string(kept) != want {
		t.Errorf("kept notes = %q, want %q", kept, want)
	}
}
