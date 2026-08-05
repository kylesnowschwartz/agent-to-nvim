// Package editwindow runs nvim on a draft in a tmux window and blocks until the
// edit finishes.
//
// A tmux window is server-owned, so the edit survives a client disconnect: a
// dropped SSH connection leaves nvim running, and reattaching brings it back.
// Waiting is separable from starting — a caller that gives up can hand its
// Handle to a later process, which reattaches and keeps waiting.
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
// The window stays open and the Handle stays valid for a later Reattach.
var ErrDeadline = errors.New("deadline reached while the draft was still open")

const pollInterval = 250 * time.Millisecond

// Request describes the edit window to open.
type Request struct {
	// Path is the draft file nvim opens.
	Path string
	// EditorArgs go to the editor ahead of the file, for a caller that needs the
	// session set up a particular way.
	EditorArgs []string
	// StartDir is the working directory nvim starts in.
	StartDir string
	// Name is the tmux window name.
	Name string
	// ExitCodeFile is where nvim's exit code gets recorded. It must not exist
	// yet: its appearance is what signals the edit finished. The caller owns the
	// path so it can outlive this process and be picked up by a Reattach.
	ExitCodeFile string
	// Focus opens the window in the foreground and restores the previously
	// active window once the edit finishes.
	Focus bool
}

// Handle identifies a running edit window well enough for a different process to
// wait on it. Every field is a plain value so a caller can persist it.
type Handle struct {
	WindowID       string `json:"window_id"`
	ExitCodeFile   string `json:"exit_code_file"`
	ReturnToWindow string `json:"return_to_window,omitempty"`
}

// Session is a running edit window.
type Session struct {
	handle Handle
}

// Start opens the edit window and returns without waiting for the edit.
func Start(req Request) (*Session, error) {
	if req.ExitCodeFile == "" {
		return nil, errors.New("no exit-code file given for the edit window")
	}
	if err := requireTmux(); err != nil {
		return nil, err
	}
	editor, err := exec.LookPath(editorName())
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", editorName(), err)
	}

	handle := Handle{ExitCodeFile: req.ExitCodeFile}
	if req.Focus {
		// Best effort: without the previous window id the edit window simply
		// stays focused after the edit, which is not worth failing the run over.
		handle.ReturnToWindow, _ = tmuxOutput("display-message", "-p", "#{window_id}")
	}

	windowID, err := tmuxOutput(
		"new-window", "-d", "-P", "-F", "#{window_id}",
		"-c", req.StartDir, "-n", req.Name,
		"--", "sh", "-c", recordExitCode(editor, req.EditorArgs, req.Path, req.ExitCodeFile),
	)
	if err != nil {
		return nil, fmt.Errorf("open tmux edit window: %w", err)
	}
	handle.WindowID = windowID

	if req.Focus {
		_, _ = tmuxOutput("select-window", "-t", windowID)
	}
	return &Session{handle: handle}, nil
}

// Reattach returns a Session for an edit window started by an earlier process.
func Reattach(handle Handle) (*Session, error) {
	if handle.WindowID == "" || handle.ExitCodeFile == "" {
		return nil, errors.New("incomplete edit-window handle")
	}
	if err := requireTmux(); err != nil {
		return nil, err
	}
	return &Session{handle: handle}, nil
}

// Handle returns what a later process needs to wait on this edit window.
func (s *Session) Handle() Handle { return s.handle }

// Wait blocks until nvim exits and returns its exit code. It returns
// ErrWindowClosed if the window disappears first and ErrDeadline if ctx ends
// while nvim is still running.
//
// Only the two returning-an-exit-code paths clean up. ErrDeadline deliberately
// leaves the exit-code file in place: nvim is still going to write it, and a
// later Reattach is what reads it.
func (s *Session) Wait(ctx context.Context) (int, error) {
	for {
		if code, ok := s.recordedExitCode(); ok {
			s.finish()
			return code, nil
		}
		if !s.windowExists() {
			// The window can close in the same instant nvim writes the exit
			// code, so read once more before calling this a kill.
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
// then its exit code written to the exit-code file. The write is a temp-file
// rename so the waiting process never reads a half-written code.
func recordExitCode(editor string, args []string, path, exitCodeFile string) string {
	open := shellQuote(editor)
	for _, arg := range args {
		open += " " + shellQuote(arg)
	}
	open += " " + shellQuote(path)

	return fmt.Sprintf(
		`%s; rc=$?; printf '%%s' "$rc" > %s.tmp && mv -f %s.tmp %s`,
		open,
		shellQuote(exitCodeFile), shellQuote(exitCodeFile), shellQuote(exitCodeFile),
	)
}

func (s *Session) recordedExitCode() (int, bool) {
	recorded, err := os.ReadFile(s.handle.ExitCodeFile)
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
		if strings.TrimSpace(id) == s.handle.WindowID {
			return true
		}
	}
	return false
}

func (s *Session) finish() {
	if s.handle.ReturnToWindow != "" {
		_, _ = tmuxOutput("select-window", "-t", s.handle.ReturnToWindow)
	}
	_ = os.Remove(s.handle.ExitCodeFile)
	_ = os.Remove(s.handle.ExitCodeFile + ".tmp")
}

func requireTmux() error {
	if os.Getenv("TMUX") == "" {
		return errors.New("no tmux session: agent-to-nvim opens the draft in a tmux window, so it must run inside tmux")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found on PATH: %w", err)
	}
	return nil
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
