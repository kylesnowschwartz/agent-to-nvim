package planhook

import (
	"strings"
	"testing"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/notes"
)

// submitted stands in for the tool input a request arrives with. The extra field
// is there to catch an approval that rebuilds the input instead of carrying it.
func submitted(plan string) map[string]any {
	return map[string]any{
		"plan":         plan,
		"planFilePath": "/plans/launch.md",
	}
}

func answerFor(r Review) verdict {
	return r.Answer(submitted(r.Submitted)).Output.Verdict
}

// Saving an untouched plan is approval as it stands, so the plan that comes back
// is the one that went over.
func TestAnAnsweredPlanKeptAsItIsGoesBackUnchanged(t *testing.T) {
	plan := "# Launch\n\nShip on Thursday.\n"
	got := answerFor(Review{Approved: true, Plan: plan, Submitted: plan})

	if got.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", got.Behavior)
	}
	if got.Input["plan"] != plan {
		t.Errorf("plan = %q, want %q", got.Input["plan"], plan)
	}
}

// An approval has to carry the whole tool input back: Claude Code drops one that
// arrives without it and asks the user itself instead, which is the dialog this
// exists to replace.
func TestAnApprovalCarriesTheRestOfTheInputBack(t *testing.T) {
	plan := "# Launch\n"
	got := answerFor(Review{Approved: true, Plan: plan, Submitted: plan})

	if got.Input["planFilePath"] != "/plans/launch.md" {
		t.Errorf("planFilePath = %v, want it carried back", got.Input["planFilePath"])
	}
}

// Editing the plan and saving it says to carry out the edited one. Handing back
// the submitted plan would quietly discard the change.
func TestAnEditedPlanIsTheOneToCarryOut(t *testing.T) {
	got := answerFor(Review{
		Approved:  true,
		Plan:      "# Launch\n\nShip on Friday.\n",
		Submitted: "# Launch\n\nShip on Thursday.\n",
	})

	if got.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", got.Behavior)
	}
	if want := "Ship on Friday."; !strings.Contains(got.Input["plan"].(string), want) {
		t.Errorf("plan = %q, want it to contain %q", got.Input["plan"], want)
	}
}

// A note on an approved plan is guidance for the work. An approval carries the
// tool input and nothing else, so the plan itself is the only place a note can go
// and still be read.
func TestAnApprovedPlanCarriesItsNotesInside(t *testing.T) {
	plan := "# Launch\n\nShip on Thursday.\n"
	got := answerFor(Review{
		Approved:  true,
		Plan:      plan,
		Submitted: plan,
		Notes: []notes.Note{{
			Said:  "check the runbook is current",
			Above: notes.Line{Num: 3, Text: "Ship on Thursday."},
		}},
	})

	if got.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", got.Behavior)
	}
	carried := got.Input["plan"].(string)
	for _, want := range []string{
		"Ship on Thursday.",
		"## Notes from the review",
		"check the runbook is current",
		`About plan line 3: "Ship on Thursday."`,
		"not a request",
	} {
		if !strings.Contains(carried, want) {
			t.Errorf("plan = %q, want it to contain %q", carried, want)
		}
	}
}

// Discarding a plan with notes on it is a request to revise, and the notes are
// the brief.
func TestADiscardedPlanComesBackWithTheNotesAsTheBrief(t *testing.T) {
	plan := "# Launch\n\nShip on Thursday.\n"
	got := answerFor(Review{
		Approved:  false,
		Plan:      plan,
		Submitted: plan,
		Notes: []notes.Note{{
			Said:  "no rollback section anywhere",
			About: notes.Whole,
		}},
	})

	if got.Behavior != "deny" {
		t.Errorf("behavior = %q, want deny", got.Behavior)
	}
	for _, want := range []string{
		"YOUR PLAN WAS NOT APPROVED",
		"no rollback section anywhere",
		"About the whole plan",
		"Do not resubmit the same plan unchanged",
	} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message = %q, want it to contain %q", got.Message, want)
		}
	}
}

// Discarding a plan the reviewer rewrote says the change is the point, so the
// change report is the brief even with nothing written out in words.
func TestADiscardedPlanReportsWhatTheReviewerChanged(t *testing.T) {
	got := answerFor(Review{
		Approved:  false,
		Plan:      "# Launch\n\nShip on Friday.\n",
		Submitted: "# Launch\n\nShip on Thursday.\n",
	})

	if got.Behavior != "deny" {
		t.Errorf("behavior = %q, want deny", got.Behavior)
	}
	for _, want := range []string{"What the reviewer changed", "Thursday", "Friday"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message = %q, want it to contain %q", got.Message, want)
		}
	}
}

// Discarding an untouched plan is a refusal with no reason attached. Replanning
// off nothing produces another guess, so the answer asks instead.
func TestADiscardedPlanWithNothingSaidAsksWhatTheyWant(t *testing.T) {
	plan := "# Launch\n\nShip on Thursday.\n"
	got := answerFor(Review{Approved: false, Plan: plan, Submitted: plan})

	if got.Behavior != "deny" {
		t.Errorf("behavior = %q, want deny", got.Behavior)
	}
	for _, want := range []string{"without changing it or saying why", "Ask them"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message = %q, want it to contain %q", got.Message, want)
		}
	}
	if strings.Contains(got.Message, "What the reviewer said") {
		t.Errorf("message = %q, want no empty brief section", got.Message)
	}
}

// A plan line runs to paragraph length, and the number already locates it, so the
// quote naming which line a note is about is trimmed to a label.
func TestALongPlanLineIsQuotedShort(t *testing.T) {
	long := strings.Repeat("a", quoteWidth+40)
	got := about(notes.Note{Said: "x", Above: notes.Line{Num: 1, Text: long}})

	if !strings.Contains(got, "…") {
		t.Errorf("about = %q, want the quote trimmed", got)
	}
	if len([]rune(got)) > quoteWidth+40 {
		t.Errorf("about = %q, want it shorter than the line it quotes", got)
	}
}

// A note written across several lines is one instruction, so every line of it
// arrives quoted rather than the first one only.
func TestEveryLineOfANoteReachesTheBrief(t *testing.T) {
	plan := "# Launch\n"
	got := answerFor(Review{
		Approved:  false,
		Plan:      plan,
		Submitted: plan,
		Notes:     []notes.Note{{Said: "split this\nthe table is too big", About: notes.Whole}},
	})

	for _, want := range []string{"> split this", "> the table is too big"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message = %q, want it to contain %q", got.Message, want)
		}
	}
}
