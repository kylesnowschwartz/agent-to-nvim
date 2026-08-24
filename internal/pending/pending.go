// Package pending remembers an edit that is still open, so a caller who stops
// waiting can hand the edit to a later process instead of losing it.
//
// A record, the exit-code file it points at, and a copy of the draft as handed
// over all live side by side in one directory, so they are created and cleaned up
// together. Splitting them across a state directory and a temp directory would
// let one outlive the other, and a resumed wait would watch a path nobody writes.
//
// The copy of the draft is a file rather than a field in the record because it
// round-trips arbitrary bytes; JSON would rewrite anything that is not valid
// UTF-8 and the resumed run would report changes the human never made.
package pending

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/editwindow"
)

// ErrUnknownEdit reports that no open edit is recorded under the given id.
var ErrUnknownEdit = errors.New("no open edit under that id")

// forgetAfter bounds how long an abandoned record is kept. An edit window left
// open for this long is not coming back, and its record would otherwise sit in
// the state directory forever.
const forgetAfter = 7 * 24 * time.Hour

// Edit is an open edit: the draft handed over and the window to keep waiting on.
// The draft's original text is stored alongside rather than in here — see Find.
type Edit struct {
	ID        string            `json:"id"`
	DraftPath string            `json:"draft_path"`
	Window    editwindow.Handle `json:"window"`
	StartedAt time.Time         `json:"started_at"`
}

// Store is the directory of open edits.
type Store struct {
	dir string
}

// OpenStore creates the state directory if needed and returns the store.
func OpenStore() (*Store, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}

	dir, err := filepath.Abs(filepath.Join(base, "agent-to-nvim"))
	if err != nil {
		return nil, fmt.Errorf("resolve state directory: %w", err)
	}
	store := &Store{dir: dir}
	for _, sub := range []string{store.DraftsDir(), store.PlansDir(), store.NotesDir()} {
		if err := os.MkdirAll(sub, 0o700); err != nil {
			return nil, fmt.Errorf("create state directory: %w", err)
		}
	}
	return store, nil
}

// DraftsDir is where an agent writes a draft that is not already a file of the
// user's own. It is a fixed path so the agent can write straight to it, rather
// than spending a command on minting a temp directory first.
func (s *Store) DraftsDir() string { return filepath.Join(s.dir, "drafts") }

// PlansDir is where a plan under review is held while a human reads it.
func (s *Store) PlansDir() string { return filepath.Join(s.dir, "plans") }

// NotesDir is where the notes from each settled edit are kept, so an agent
// whose process lost the stderr report can read them back.
func (s *Store) NotesDir() string { return filepath.Join(s.dir, "notes") }

// KeepNotes writes the rendered notes report for an edit to NotesDir, named by
// the edit's id. The copy stays after the edit settles: the report on stderr is
// gone the moment it scrolls away, and this file is the only other place the
// user's instructions exist.
func (s *Store) KeepNotes(id, rendered string) (string, error) {
	path := filepath.Join(s.NotesDir(), id+".txt")
	if err := writeFileAtomic(path, []byte(rendered)); err != nil {
		return "", fmt.Errorf("keep notes: %w", err)
	}
	return path, nil
}

// HoldPlan writes a plan out for review and returns the file to open.
//
// The copy is this tool's own. The harness wrote the plan to a file of its own
// before asking about it, but an approved plan travels back in the answer rather
// than on disk, so there is nothing to gain by editing the harness's copy and a
// directory belonging to another tool to keep out of. name is what the plan is
// called, so the window title and the editor's filetype both say what is being
// read.
func (s *Store) HoldPlan(name, plan string) (string, error) {
	path := filepath.Join(s.PlansDir(), safeName(name)+".md")
	if err := os.WriteFile(path, []byte(plan), 0o600); err != nil {
		return "", fmt.Errorf("write the plan out for review: %w", err)
	}
	return path, nil
}

// safeName reduces a name from the harness to something that can only ever be one
// file inside PlansDir.
func safeName(name string) string {
	name = filepath.Base(strings.TrimSuffix(name, ".md"))
	kept := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	kept = strings.Trim(kept, "-")

	if kept == "" {
		return "plan-under-review"
	}
	if len(kept) > 60 {
		kept = strings.Trim(kept[:60], "-")
	}
	return kept
}

// DropScratch removes a draft that lives in DraftsDir, so a scratch draft does
// not outlive the handover it was written for and the next handover under the
// same name starts clean. A draft anywhere else is the user's own file — editing
// it in place is the point, so it stays.
//
// Call this only once the text has been handed back. A draft whose edit was
// discarded or abandoned holds words that went nowhere else, so it is left on
// disk to be recovered.
func (s *Store) DropScratch(path string) {
	if !s.IsScratch(path) {
		return
	}
	_ = os.Remove(path)
}

// IsScratch reports whether a draft lives in DraftsDir — written only for the
// handover — as opposed to being a file of the user's own edited in place.
func (s *Store) IsScratch(path string) bool {
	return filepath.Dir(path) == s.DraftsDir()
}

// Begin allocates an id and an exit-code path for a draft about to be handed
// over. The exit-code file is not created: its appearance is what signals the
// edit finished.
func (s *Store) Begin(draftPath string) (Edit, error) {
	id, err := newID()
	if err != nil {
		return Edit{}, err
	}
	return Edit{
		ID:        id,
		DraftPath: draftPath,
		Window:    editwindow.Handle{ExitCodeFile: filepath.Join(s.dir, id+".rc")},
		StartedAt: time.Now(),
	}, nil
}

// Remember writes the edit and the draft as handed over so a later process can
// find both, and drops records old enough that nobody is coming back for them.
//
// The original is written first: once the record exists, whoever reads it is
// entitled to assume the original is there too.
func (s *Store) Remember(edit Edit, original []byte) error {
	if err := writeFileAtomic(s.originalPath(edit.ID), original); err != nil {
		return fmt.Errorf("record handed-over draft: %w", err)
	}

	recorded, err := json.MarshalIndent(edit, "", "  ")
	if err != nil {
		return fmt.Errorf("encode open edit: %w", err)
	}
	if err := writeFileAtomic(s.recordPath(edit.ID), recorded); err != nil {
		return fmt.Errorf("record open edit: %w", err)
	}

	s.forgetStale()
	return nil
}

// Find returns the open edit recorded under id along with the draft as it was
// handed over, which is what the edited version gets compared against.
func (s *Store) Find(id string) (Edit, []byte, error) {
	if err := validateID(id); err != nil {
		return Edit{}, nil, err
	}

	recorded, err := os.ReadFile(s.recordPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Edit{}, nil, fmt.Errorf("%w: %s", ErrUnknownEdit, id)
	}
	if err != nil {
		return Edit{}, nil, fmt.Errorf("read open edit: %w", err)
	}

	var edit Edit
	if err := json.Unmarshal(recorded, &edit); err != nil {
		return Edit{}, nil, fmt.Errorf("decode open edit %s: %w", id, err)
	}

	original, err := os.ReadFile(s.originalPath(id))
	if err != nil {
		return Edit{}, nil, fmt.Errorf("read handed-over draft for %s: %w", id, err)
	}
	return edit, original, nil
}

// Forget drops the record and the files beside it once the edit is resolved.
func (s *Store) Forget(id string) {
	if validateID(id) != nil {
		return
	}
	for _, name := range []string{id + ".json", id + ".orig", id + ".rc"} {
		_ = os.Remove(filepath.Join(s.dir, name))
		_ = os.Remove(filepath.Join(s.dir, name+".tmp"))
	}
}

func (s *Store) recordPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) originalPath(id string) string {
	return filepath.Join(s.dir, id+".orig")
}

func (s *Store) forgetStale() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < forgetAfter {
			continue
		}
		s.Forget(strings.TrimSuffix(entry.Name(), ".json"))
	}
}

// writeFileAtomic lands the whole file in one rename, so a resuming process
// never reads a record or a draft copy that is still being written.
func writeFileAtomic(path string, content []byte) error {
	temp := path + ".tmp"
	if err := os.WriteFile(temp, content, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func newID() (string, error) {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate edit id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// validateID keeps an id from reaching into the filesystem through the path it
// gets joined into — ids come from the command line.
func validateID(id string) error {
	if id == "" {
		return errors.New("empty edit id")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("%w: %q is not an edit id", ErrUnknownEdit, id)
	}
	return nil
}
