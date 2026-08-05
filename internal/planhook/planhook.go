// Package planhook speaks Claude Code's plan-approval protocol: it reads the
// request to leave plan mode and writes back the answer a human gave.
//
// Claude Code asks before it acts on a plan. That question arrives as a
// PermissionRequest hook event on stdin and is answered with one JSON object on
// stdout. The plan itself comes both as text and as a file Claude Code has
// already written, which is what makes it editable in place.
package planhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Event is the request to leave plan mode.
//
// The plan's own fields are read through accessors rather than decoded into a
// struct because an approval has to hand the whole input back (see Allow), and
// fields this package does not know about have to survive that round trip.
type Event struct {
	Name      string         `json:"hook_event_name"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	Cwd       string         `json:"cwd"`
}

// planKey and planFileKey are ExitPlanMode's input fields: the plan as text, and
// the file Claude Code wrote it to before asking.
const (
	planKey     = "plan"
	planFileKey = "planFilePath"
)

// ReadEvent decodes the request. An empty body is not an error — Claude Code
// starts hooks it then has nothing to ask — so the caller is told to stand down
// via ErrNothingAsked rather than to report a failure.
func ReadEvent(r io.Reader) (Event, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return Event{}, fmt.Errorf("read the plan request: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return Event{}, ErrNothingAsked
	}

	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		return Event{}, fmt.Errorf("decode the plan request: %w", err)
	}
	if event.Plan() == "" {
		return Event{}, ErrNothingAsked
	}
	return event, nil
}

// ErrNothingAsked reports that the request carried no plan, so there is nothing
// to review and no answer to give.
var ErrNothingAsked = errors.New("the plan request carried no plan")

// Plan is the plan as the agent wrote it.
func (e Event) Plan() string {
	text, _ := e.ToolInput[planKey].(string)
	return text
}

// PlanFile is where Claude Code already wrote the plan, which is the file a
// human edits. It is empty for a harness that only sends the text.
func (e Event) PlanFile() string {
	path, _ := e.ToolInput[planFileKey].(string)
	return path
}

// Decision is the answer Claude Code reads on stdout.
type Decision struct {
	Output output `json:"hookSpecificOutput"`
}

type output struct {
	Name    string  `json:"hookEventName"`
	Verdict verdict `json:"decision"`
}

type verdict struct {
	Behavior    string         `json:"behavior"`
	Input       map[string]any `json:"updatedInput,omitempty"`
	Permissions []permission   `json:"updatedPermissions,omitempty"`
	Message     string         `json:"message,omitempty"`
}

// permission is a change to what the session may do without asking again.
type permission struct {
	Type        string `json:"type"`
	Mode        string `json:"mode"`
	Destination string `json:"destination"`
}

// Allow approves the plan and hands back the one to carry out, which is the
// reviewed plan rather than the submitted one whenever the human changed it.
//
// The whole tool input goes back with the answer. Claude Code drops an approval
// for a tool that would otherwise have asked the user when the input is missing,
// falling back to its own dialog — which is the browser-shaped hole this exists
// to avoid.
func Allow(plan string, submitted map[string]any) Decision {
	input := make(map[string]any, len(submitted)+1)
	for key, value := range submitted {
		input[key] = value
	}
	input[planKey] = plan

	return Decision{Output: output{
		Name:    "PermissionRequest",
		Verdict: verdict{Behavior: "allow", Input: input},
	}}
}

// AcceptingEdits returns the approval with the session switched to accepting
// edits, so a reviewer who approved and walked away is not asked about each one.
//
// Approving a plan does not otherwise widen anything: the session lands in the
// mode it was already in. Claude Code's own approval dialog offers this as its
// second answer, and without it a plan approved here is followed by a prompt per
// edit.
func (d Decision) AcceptingEdits() Decision {
	d.Output.Verdict.Permissions = append(d.Output.Verdict.Permissions, permission{
		Type:        "setMode",
		Mode:        "acceptEdits",
		Destination: "session",
	})
	return d
}

// Deny sends the plan back with what the human said about it.
func Deny(message string) Decision {
	return Decision{Output: output{
		Name:    "PermissionRequest",
		Verdict: verdict{Behavior: "deny", Message: message},
	}}
}

// Send emits the answer. Claude Code reads one JSON object from stdout, so
// nothing else may be written there.
func (d Decision) Send(w io.Writer) error {
	answer, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("encode the plan answer: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(answer)); err != nil {
		return fmt.Errorf("write the plan answer: %w", err)
	}
	return nil
}
