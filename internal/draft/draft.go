// Package draft tracks the file an agent handed over for editing, so the caller
// can tell an approved-as-is draft apart from an edited one.
package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Draft is a file under edit, holding the fingerprint it had when handed over.
type Draft struct {
	path        string
	fingerprint string
}

// Open reads the file and records its fingerprint. The file must exist and be
// regular: the agent writes the draft before handing it over, so an absent path
// means the caller got the handoff wrong rather than that an empty draft is wanted.
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

	text, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read draft: %w", err)
	}

	return &Draft{path: abs, fingerprint: fingerprint(text)}, nil
}

// Reopen rebuilds a Draft from a path and a fingerprint recorded by an earlier
// process, so an edit handed over in one run can be judged edited-or-not in the
// next one.
func Reopen(path, fingerprint string) *Draft {
	return &Draft{path: path, fingerprint: fingerprint}
}

// Path is the absolute path handed to the editor.
func (d *Draft) Path() string { return d.path }

// Fingerprint is the content hash recorded when the draft was handed over.
func (d *Draft) Fingerprint() string { return d.fingerprint }

// Reread returns the file's current text and whether it differs from the text
// recorded at Open. Content is compared rather than modification time because
// writing in nvim touches mtime even when nothing changed.
func (d *Draft) Reread() (text string, edited bool, err error) {
	current, err := os.ReadFile(d.path)
	if err != nil {
		return "", false, fmt.Errorf("read edited draft: %w", err)
	}
	return string(current), fingerprint(current) != d.fingerprint, nil
}

func fingerprint(text []byte) string {
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])
}
