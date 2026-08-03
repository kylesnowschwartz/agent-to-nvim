// Package pending remembers an edit that is still open, so a caller who stops
// waiting can hand the edit to a later process instead of losing it.
//
// A record and the exit-code file it points at live side by side in one
// directory, so they are created and cleaned up together. Splitting them across
// a state directory and a temp directory would let one outlive the other, and a
// resumed wait would watch a path nobody writes.
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

// Edit is an open edit: the draft handed over, the fingerprint to judge it
// against, and the window to keep waiting on.
type Edit struct {
	ID          string            `json:"id"`
	DraftPath   string            `json:"draft_path"`
	Fingerprint string            `json:"fingerprint"`
	Window      editwindow.Handle `json:"window"`
	StartedAt   time.Time         `json:"started_at"`
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

	dir := filepath.Join(base, "agent-to-nvim")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Begin allocates an id and an exit-code path for a draft about to be handed
// over. The exit-code file is not created: its appearance is what signals the
// edit finished.
func (s *Store) Begin(draftPath, fingerprint string) (Edit, error) {
	id, err := newID()
	if err != nil {
		return Edit{}, err
	}
	return Edit{
		ID:          id,
		DraftPath:   draftPath,
		Fingerprint: fingerprint,
		Window:      editwindow.Handle{ExitCodeFile: filepath.Join(s.dir, id+".rc")},
		StartedAt:   time.Now(),
	}, nil
}

// Remember writes the edit so a later process can find it, and drops records old
// enough that nobody is coming back for them.
func (s *Store) Remember(edit Edit) error {
	recorded, err := json.MarshalIndent(edit, "", "  ")
	if err != nil {
		return fmt.Errorf("encode open edit: %w", err)
	}

	path := s.recordPath(edit.ID)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, recorded, 0o600); err != nil {
		return fmt.Errorf("record open edit: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("record open edit: %w", err)
	}

	s.forgetStale()
	return nil
}

// Find returns the open edit recorded under id.
func (s *Store) Find(id string) (Edit, error) {
	if err := validateID(id); err != nil {
		return Edit{}, err
	}

	recorded, err := os.ReadFile(s.recordPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Edit{}, fmt.Errorf("%w: %s", ErrUnknownEdit, id)
	}
	if err != nil {
		return Edit{}, fmt.Errorf("read open edit: %w", err)
	}

	var edit Edit
	if err := json.Unmarshal(recorded, &edit); err != nil {
		return Edit{}, fmt.Errorf("decode open edit %s: %w", id, err)
	}
	return edit, nil
}

// Forget drops the record and its exit-code file once the edit is resolved.
func (s *Store) Forget(id string) {
	if validateID(id) != nil {
		return
	}
	_ = os.Remove(s.recordPath(id))
	_ = os.Remove(filepath.Join(s.dir, id+".rc"))
	_ = os.Remove(filepath.Join(s.dir, id+".rc.tmp"))
}

func (s *Store) recordPath(id string) string {
	return filepath.Join(s.dir, id+".json")
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
