// Package editwindow runs nvim on a draft in a tmux window and blocks until the
// edit finishes.
//
// A tmux window is server-owned, so the edit survives a client disconnect: a
// dropped SSH connection leaves nvim running, and reattaching brings it back.
package editwindow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrWindowClosed reports that the edit window disappeared without nvim
// recording an exit code — the window was killed, or tmux went away.
var ErrWindowClosed = errors.New("edit window closed before the draft was saved")

// ErrDeadline reports that the wait was given up on while nvim is still running.
// The window stays open and the session stays resumable.
var ErrDeadline = errors.New("deadline reached while the draft was still open")

const pollInterval = 250 * time.Millisecond

// Request describes the edit window to open.
type Request struct {
	// Path is the draft file nvim opens.
	Path string
	// StartDir is the working directory nvim starts in.
	StartDir string
	// Name is the tmux window name.
	Name string
	// Focus opens the window in the foreground and restores the previously
	// active window once the edit finishes.
	Focus bool
}

// Session is a running edit window.
type Session struct {
	windowID    string
	sentinel    string
	returnToWin string
}

// Start opens the edit window and returns without waiting for the edit.
func Start(req Request) (*Session, error) {
	if os.Getenv("TMUX") == "" {
		return nil, errors.New("no tmux session: agent-to-nvim opens the draft in a tmux window, so it must run inside tmux")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux not found on PATH: %w", err)
	}
	editor, err := exec.LookPath(editorName())
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", editorName(), err)
	}

	sentinel, err := reserveSentinelPath()
	if err != nil {
		return nil, err
	}

	session := &Session{sentinel: sentinel}
	if req.Focus {
		// Best effort: without the previous window id the edit window simply
		// stays focused after the edit, which is not worth failing the run over.
		session.returnToWin, _ = tmuxOutput("display-message", "-p", "#{window_id}")
	}

	windowID, err := tmuxOutput(
		"new-window", "-d", "-P", "-F", "#{window_id}",
		"-c", req.StartDir, "-n", req.Name,
		"--", "sh", "-c", recordExitCode(editor, req.Path, sentinel),
	)
	if err != nil {
		os.Remove(sentinel)
		return nil, fmt.Errorf("open tmux edit window: %w", err)
	}
	session.windowID = windowID

	if req.Focus {
		_, _ = tmuxOutput("select-window", "-t", windowID)
	}
	return session, nil
}

// Wait blocks until nvim exits and returns its exit code. It returns
// ErrWindowClosed if the window disappears first and ErrDeadline if ctx ends
// while nvim is still running.
func (s *Session) Wait(ctx context.Context) (int, error) {
	for {
		if code, ok := s.recordedExitCode(); ok {
			s.finish()
			return code, nil
		}
		if !s.windowExists() {
			// The window can close in the same instant nvim writes the
			// sentinel, so read once more before calling this a kill.
			if code, ok := s.recordedExitCode(); ok {
				s.finish()
				return code, nil
			}
			s.finish()
			return 0, ErrWindowClosed
		}

		select {
		case <-ctx.Done():
			return 0, ErrDeadline
		case <-time.After(pollInterval):
		}
	}
}

// recordExitCode builds the shell command the window runs: nvim on the draft,
// then its exit code written to the sentinel. The write is a temp-file rename so
// the waiting process never reads a half-written code.
func recordExitCode(editor, path, sentinel string) string {
	return fmt.Sprintf(
		`%s %s; rc=$?; printf '%%s' "$rc" > %s.tmp && mv -f %s.tmp %s`,
		shellQuote(editor), shellQuote(path),
		shellQuote(sentinel), shellQuote(sentinel), shellQuote(sentinel),
	)
}

func (s *Session) recordedExitCode() (int, bool) {
	recorded, err := os.ReadFile(s.sentinel)
	if err != nil {
		return 0, false
	}
	var code int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(recorded)), "%d", &code); err != nil {
		return 0, false
	}
	return code, true
}

func (s *Session) windowExists() bool {
	windows, err := tmuxOutput("list-windows", "-a", "-F", "#{window_id}")
	if err != nil {
		return false
	}
	for _, id := range strings.Split(windows, "\n") {
		if strings.TrimSpace(id) == s.windowID {
			return true
		}
	}
	return false
}

func (s *Session) finish() {
	if s.returnToWin != "" {
		_, _ = tmuxOutput("select-window", "-t", s.returnToWin)
	}
	os.Remove(s.sentinel)
	os.Remove(s.sentinel + ".tmp")
}

// reserveSentinelPath returns a free path for the exit-code file. The file is
// removed so its existence is what signals the edit finished.
func reserveSentinelPath() (string, error) {
	file, err := os.CreateTemp("", "agent-to-nvim-done-*")
	if err != nil {
		return "", fmt.Errorf("reserve exit-code file: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("reserve exit-code file: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("reserve exit-code file: %w", err)
	}
	return path, nil
}

func editorName() string {
	if override := os.Getenv("AGENT_TO_NVIM_EDITOR"); override != "" {
		return override
	}
	return "nvim"
}

func tmuxOutput(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// shellQuote wraps a value in single quotes for embedding in the `sh -c` string
// tmux runs, so a path containing spaces or shell metacharacters stays literal.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
