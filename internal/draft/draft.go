// Package draft tracks the file an agent handed over for editing, so the caller
// can tell an approved-as-is draft apart from an edited one and can say what
// changed.
package draft

import (
	"fmt"
	"os"
	"path/filepath"
)

// Draft is a file under edit, holding the text it had when handed over.
type Draft struct {
	path     string
	original []byte
}

// Open reads the file and keeps its text. The file must exist and be regular:
// the agent writes the draft before handing it over, so an absent path means the
// caller got the handoff wrong rather than that an empty draft is wanted.
func Open(path string) (*Draft, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("read draft: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("draft %q is not a regular file", abs)
	}

	original, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read draft: %w", err)
	}

	return &Draft{path: abs, original: original}, nil
}

// Reopen rebuilds a Draft from a path and the original text recorded by an
// earlier process, so an edit handed over in one run can be judged in the next
// one.
func Reopen(path string, original []byte) *Draft {
	return &Draft{path: path, original: original}
}

// Path is the absolute path handed to the editor.
func (d *Draft) Path() string { return d.path }

// Original is the text as handed over, which is what the edited version is
// compared against.
func (d *Draft) Original() []byte { return d.original }

// Reread returns the file's current text. Whether that counts as an edit is the
// caller's call against Original, and it is settled by comparing content rather
// than modification time: writing in nvim touches mtime even when nothing
// changed.
func (d *Draft) Reread() (string, error) {
	current, err := os.ReadFile(d.path)
	if err != nil {
		return "", fmt.Errorf("read edited draft: %w", err)
	}
	return string(current), nil
}
