package editwindow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellQuoteKeepsMetacharactersLiteral(t *testing.T) {
	tests := map[string]string{
		"/tmp/plain.md":          `'/tmp/plain.md'`,
		"/tmp/with space.md":     `'/tmp/with space.md'`,
		"/tmp/it's a draft.md":   `'/tmp/it'\''s a draft.md'`,
		"/tmp/$(whoami).md":      `'/tmp/$(whoami).md'`,
		"/tmp/a;rm -rf slash.md": `'/tmp/a;rm -rf slash.md'`,
	}
	for value, want := range tests {
		if got := shellQuote(value); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestRecordExitCodeQuotesEveryPath(t *testing.T) {
	command := recordExitCode("/usr/bin/nvim", nil, "/tmp/it's a draft.md", "/tmp/done-1")
	for _, want := range []string{`'/usr/bin/nvim'`, `'/tmp/it'\''s a draft.md'`, `'/tmp/done-1'`} {
		if !strings.Contains(command, want) {
			t.Errorf("command %q is missing quoted %s", command, want)
		}
	}
	if !strings.Contains(command, `mv -f '/tmp/done-1'.tmp '/tmp/done-1'`) {
		t.Errorf("command %q does not write the exit code through a rename", command)
	}
}

// Editor arguments go ahead of the file and are quoted like everything else: they
// carry paths of their own, and nvim reads the file it is given last.
func TestRecordExitCodePutsEditorArgumentsBeforeTheFile(t *testing.T) {
	command := recordExitCode("/usr/bin/nvim",
		[]string{"-S", "/tmp/setup dir/review.lua"}, "/tmp/plan.md", "/tmp/done-1")

	want := `'/usr/bin/nvim' '-S' '/tmp/setup dir/review.lua' '/tmp/plan.md'`
	if !strings.Contains(command, want) {
		t.Errorf("command %q does not open the editor as %q", command, want)
	}
}

func TestStartWithoutTmuxServerFails(t *testing.T) {
	stubTmuxProbe(t, errors.New("no server running on /tmp/tmux-502/default"))
	request := Request{Path: "/tmp/draft.md", ExitCodeFile: "/tmp/done-1"}
	_, err := Start(request)
	if err == nil {
		t.Fatal("expected an error when no tmux server is reachable")
	}
	if !strings.Contains(err.Error(), "no server running") {
		t.Errorf("error %q does not carry tmux's own message", err)
	}
}

// A process started by a daemon or background job runner has no TMUX variable
// even though the tmux server is reachable; the check must not depend on it.
func TestRequireTmuxIgnoresMissingTmuxVariable(t *testing.T) {
	t.Setenv("TMUX", "")
	stubTmuxProbe(t, nil)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	if err := requireTmux(); err != nil {
		t.Fatalf("requireTmux refused a reachable tmux server: %v", err)
	}
}

func stubTmuxProbe(t *testing.T, result error) {
	t.Helper()
	previous := probeTmux
	probeTmux = func() error { return result }
	t.Cleanup(func() { probeTmux = previous })
}

func TestStartWithoutExitCodeFileFails(t *testing.T) {
	if _, err := Start(Request{Path: "/tmp/draft.md"}); err == nil {
		t.Fatal("expected an error when no exit-code file is given")
	}
}

func TestReattachRejectsIncompleteHandle(t *testing.T) {
	for name, handle := range map[string]Handle{
		"no window":         {ExitCodeFile: "/tmp/done-1"},
		"no exit-code file": {WindowID: "@1"},
	} {
		if _, err := Reattach(handle); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestWaitReturnsEditorExitCodeOnSave(t *testing.T) {
	path, session := startEdit(t)
	sendKeys(t, session.handle.WindowID, "ggA", " (edited)", "Escape")
	sendKeys(t, session.handle.WindowID, ":wq", "Enter")

	code, err := session.Wait(waitContext(t))
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for a saved draft", code)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read draft: %v", err)
	}
	if want := "hey team (edited)\n"; string(saved) != want {
		t.Errorf("draft = %q, want %q", saved, want)
	}
}

func TestWaitReportsDiscardAsNonZeroExitCode(t *testing.T) {
	_, session := startEdit(t)
	sendKeys(t, session.handle.WindowID, ":cq", "Enter")

	code, err := session.Wait(waitContext(t))
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code == 0 {
		t.Error("exit code = 0, want nonzero after :cq")
	}
}

func TestWaitReportsKilledWindow(t *testing.T) {
	_, session := startEdit(t)
	if err := exec.Command("tmux", "kill-window", "-t", session.handle.WindowID).Run(); err != nil {
		t.Fatalf("kill window: %v", err)
	}

	if _, err := session.Wait(waitContext(t)); !errors.Is(err, ErrWindowClosed) {
		t.Errorf("Wait error = %v, want ErrWindowClosed", err)
	}
}

func TestWaitReportsDeadlineWhileEditorRuns(t *testing.T) {
	_, session := startEdit(t)
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-window", "-t", session.handle.WindowID).Run() })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := session.Wait(ctx); !errors.Is(err, ErrDeadline) {
		t.Errorf("Wait error = %v, want ErrDeadline", err)
	}
}

// A caller that gives up must leave the edit collectable: the exit-code file has
// to survive the abandoned wait, and a Handle carried through JSON — the shape a
// later process reads it back in — has to be enough to finish the edit.
func TestReattachFinishesAnAbandonedWait(t *testing.T) {
	path, session := startEdit(t)

	givenUp, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := session.Wait(givenUp); !errors.Is(err, ErrDeadline) {
		t.Fatalf("first Wait error = %v, want ErrDeadline", err)
	}

	carried, err := json.Marshal(session.Handle())
	if err != nil {
		t.Fatalf("encode handle: %v", err)
	}
	var handle Handle
	if err := json.Unmarshal(carried, &handle); err != nil {
		t.Fatalf("decode handle: %v", err)
	}

	resumed, err := Reattach(handle)
	if err != nil {
		t.Fatalf("Reattach: %v", err)
	}

	sendKeys(t, handle.WindowID, "ggA", " (edited later)", "Escape")
	sendKeys(t, handle.WindowID, ":wq", "Enter")

	code, err := resumed.Wait(waitContext(t))
	if err != nil {
		t.Fatalf("resumed Wait: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for a saved draft", code)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read draft: %v", err)
	}
	if want := "hey team (edited later)\n"; string(saved) != want {
		t.Errorf("draft = %q, want %q", saved, want)
	}
}

// startEdit opens a background edit window on a fresh draft and waits until the
// draft text is on screen, so keys sent by the test land in nvim rather than in
// the shell that launches it. Readiness is read off the rendered pane because
// tmux reports the launching shell as #{pane_current_command} even once nvim is
// running as its child.
func startEdit(t *testing.T) (path string, session *Session) {
	t.Helper()
	requireTmuxAndEditor(t)

	dir := t.TempDir()
	path = filepath.Join(dir, "draft.md")
	if err := os.WriteFile(path, []byte("hey team\n"), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	session, err := Start(Request{
		Path:         path,
		StartDir:     dir,
		Name:         "a2n-test",
		ExitCodeFile: filepath.Join(dir, "exit-code"),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		screen, _ := tmuxOutput("capture-pane", "-p", "-t", session.handle.WindowID)
		if strings.Contains(screen, "hey team") {
			return path, session
		}
		if time.Now().After(deadline) {
			_ = exec.Command("tmux", "kill-window", "-t", session.handle.WindowID).Run()
			t.Fatalf("nvim did not draw the draft in the edit window (screen: %q)", screen)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func requireTmuxAndEditor(t *testing.T) {
	t.Helper()
	if err := requireTmux(); err != nil {
		t.Skipf("no usable tmux: %v", err)
	}
	if _, err := exec.LookPath(editorName()); err != nil {
		t.Skipf("%s not on PATH", editorName())
	}
}

func sendKeys(t *testing.T, windowID string, keys ...string) {
	t.Helper()
	args := append([]string{"send-keys", "-t", windowID}, keys...)
	if err := exec.Command("tmux", args...).Run(); err != nil {
		t.Fatalf("send-keys %v: %v", keys, err)
	}
}

func waitContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
