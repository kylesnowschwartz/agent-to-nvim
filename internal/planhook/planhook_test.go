package planhook

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const request = `{
  "hook_event_name": "PermissionRequest",
  "tool_name": "ExitPlanMode",
  "cwd": "/work/project",
  "tool_input": {"plan": "# Launch\n\nShip on Thursday.\n", "planFilePath": "/plans/launch.md"}
}`

func TestReadEventFindsThePlanAndItsFile(t *testing.T) {
	event, err := ReadEvent(strings.NewReader(request))
	if err != nil {
		t.Fatalf("ReadEvent() error = %v", err)
	}

	if want := "# Launch\n\nShip on Thursday.\n"; event.Plan() != want {
		t.Errorf("Plan() = %q, want %q", event.Plan(), want)
	}
	if want := "/plans/launch.md"; event.PlanFile() != want {
		t.Errorf("PlanFile() = %q, want %q", event.PlanFile(), want)
	}
	if want := "/work/project"; event.Cwd != want {
		t.Errorf("Cwd = %q, want %q", event.Cwd, want)
	}
}

// Claude Code starts a hook and then sometimes has nothing to ask it. That is not
// a failure, so it is reported as its own outcome for the caller to stand down on.
func TestReadEventReportsNothingAskedForAnEmptyRequest(t *testing.T) {
	for _, body := range []string{"", "  \n", "{}", `{"tool_input": {}}`} {
		if _, err := ReadEvent(strings.NewReader(body)); !errors.Is(err, ErrNothingAsked) {
			t.Errorf("ReadEvent(%q) error = %v, want ErrNothingAsked", body, err)
		}
	}
}

func TestReadEventReportsAnUnreadableRequest(t *testing.T) {
	if _, err := ReadEvent(strings.NewReader("not json")); err == nil {
		t.Error("ReadEvent() error = nil, want a decode failure")
	} else if errors.Is(err, ErrNothingAsked) {
		t.Errorf("ReadEvent() error = %v, want a decode failure rather than nothing asked", err)
	}
}

// A harness that sends the plan as text only has no file to edit, and the caller
// has to be able to tell that apart from a path it failed to read.
func TestPlanFileIsEmptyWhenTheRequestHasNone(t *testing.T) {
	event, err := ReadEvent(strings.NewReader(`{"tool_input": {"plan": "# Launch\n"}}`))
	if err != nil {
		t.Fatalf("ReadEvent() error = %v", err)
	}
	if event.PlanFile() != "" {
		t.Errorf("PlanFile() = %q, want empty", event.PlanFile())
	}
}

// The answer is read off stdout as one JSON object, so it has to be exactly that
// — the shape is the contract with Claude Code, not an internal detail.
func TestSendEmitsTheAnswerAsOneJSONLine(t *testing.T) {
	var out strings.Builder
	if err := Allow("# Launch\n", map[string]any{"planFilePath": "/plans/launch.md"}).Send(&out); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	written := out.String()
	if strings.Count(written, "\n") != 1 || !strings.HasSuffix(written, "\n") {
		t.Errorf("written = %q, want one line ending in a newline", written)
	}

	var got struct {
		Output struct {
			Name    string `json:"hookEventName"`
			Verdict struct {
				Behavior string         `json:"behavior"`
				Input    map[string]any `json:"updatedInput"`
				Message  string         `json:"message"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(written), &got); err != nil {
		t.Fatalf("the answer did not decode: %v", err)
	}
	if got.Output.Name != "PermissionRequest" {
		t.Errorf("hookEventName = %q, want PermissionRequest", got.Output.Name)
	}
	if got.Output.Verdict.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", got.Output.Verdict.Behavior)
	}
	if got.Output.Verdict.Input["plan"] != "# Launch\n" {
		t.Errorf("plan = %v, want the approved plan", got.Output.Verdict.Input["plan"])
	}
	if got.Output.Verdict.Message != "" {
		t.Errorf("message = %q, want none on an approval", got.Output.Verdict.Message)
	}
}

// A refusal carries no input: the field means "carry this out instead", and
// sending one with a refusal invites the plan to be run anyway.
func TestARefusalCarriesNoInput(t *testing.T) {
	var out strings.Builder
	if err := Deny("no").Send(&out); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if strings.Contains(out.String(), "updatedInput") {
		t.Errorf("written = %q, want no updatedInput on a refusal", out.String())
	}
}

// Allow must not write through to the request's own input: the caller still reads
// the submitted plan from it to report what changed.
func TestAllowLeavesTheRequestsInputAlone(t *testing.T) {
	input := map[string]any{"plan": "# Submitted\n"}
	Allow("# Reviewed\n", input)

	if input["plan"] != "# Submitted\n" {
		t.Errorf("the request's plan = %v, want it untouched", input["plan"])
	}
}
