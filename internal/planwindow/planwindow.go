// Package planwindow sets the editor up for reading a plan: what the keys do, and
// what the window says they do.
//
// A plan review needs a verdict as well as an edit, and "save to approve, quit
// with a failure to send it back" is not something an editor says on its own. So
// the session is opened with the two answers bound to keys and spelled out along
// the top of the window, and a reader never has to be told.
package planwindow

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed review.lua
var setup string

// HandedToTheDialog is the exit code the reader leaves when they want Claude
// Code's own approval dialog to answer instead of this window.
//
// Claude Code asks the same question in two places at once and takes whichever
// answer arrives first. Its dialog can offer one this window cannot: approving
// with the conversation cleared. So the reader needs a way to say which of the
// two is answering, and a distinct exit code is it — 0 already means approve and
// 1 means revise.
//
// review.lua declares the same number, and a test holds the two together.
const HandedToTheDialog = 7

// Args returns the editor arguments that load the review setup, writing the setup
// out under dir first.
//
// It is written on every review rather than installed once, so the keys can never
// be a version behind the tool that reads what they produce.
func Args(dir string) ([]string, error) {
	path := filepath.Join(dir, "review.lua")
	if err := os.WriteFile(path, []byte(setup), 0o600); err != nil {
		return nil, fmt.Errorf("write the plan review keys out: %w", err)
	}
	return []string{"-S", path}, nil
}
