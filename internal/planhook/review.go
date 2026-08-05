package planhook

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/notes"
	"github.com/kylesnowschwartz/agent-to-nvim/internal/textdiff"
)

// Review is what a human left behind in the editor.
//
// Saving and discarding are the whole verdict: a saved plan is one to carry out,
// a discarded one is not. What they wrote shapes the answer but does not decide
// it, so a note can be guidance to work from or a change to make depending only
// on how they closed the plan.
type Review struct {
	// Approved is whether they saved the plan rather than discarding it.
	Approved bool
	// Plan is the plan as they left it, with their notes taken back out.
	Plan string
	// Submitted is the plan as the agent wrote it.
	Submitted string
	// Notes are the asides they left in it.
	Notes []notes.Note
	// Auto is whether they approved into auto mode, where the work runs start to
	// finish without stopping to have each step confirmed. It only means
	// anything alongside an approval.
	Auto bool
}

// Changed reports whether the human rewrote any of the plan itself.
func (r Review) Changed() bool { return r.Plan != r.Submitted }

// Answer turns the review into the decision Claude Code reads. submitted is the
// tool input the request arrived with, which an approval has to hand back.
func (r Review) Answer(submitted map[string]any) Decision {
	if !r.Approved {
		return Deny(r.brief())
	}

	approval := Allow(r.approvedPlan(), submitted)
	if r.Auto {
		approval = approval.InAutoMode()
	}
	return approval
}

// approvedPlan is the plan to carry out: the one the human left, with anything
// they said about it written into it.
//
// Notes go into the plan rather than alongside it because an approval has no
// other channel — Claude Code carries the tool input and drops added context. So
// the plan is where a note has to live to be read at all, and it says plainly
// that a note is guidance and not a reason to plan again.
func (r Review) approvedPlan() string {
	if len(r.Notes) == 0 {
		return r.Plan
	}

	var out strings.Builder
	out.WriteString(r.Plan)
	if !strings.HasSuffix(r.Plan, "\n") {
		out.WriteString("\n")
	}
	out.WriteString("\n## Notes from the review\n\n")
	out.WriteString("The plan is approved. These are notes to carry into the work, not a request\nto plan again.\n\n")
	out.WriteString(numbered(r.Notes))
	return out.String()
}

// brief is what a rejected plan comes back with. It leads with the verdict
// because an agent that reads the feedback first tends to start answering it
// rather than replanning.
func (r Review) brief() string {
	var out strings.Builder
	out.WriteString("YOUR PLAN WAS NOT APPROVED.\n\n")

	if len(r.Notes) == 0 && !r.Changed() {
		out.WriteString("The reviewer closed the plan without changing it or saying why. Ask them what\n" +
			"they want different before planning again. Do not guess at it, and do not\n" +
			"resubmit this plan.\n")
		return out.String()
	}

	out.WriteString("Revise the plan to address everything below, then ask to leave plan mode again.\n\n" +
		"- Do not resubmit the same plan unchanged.\n" +
		"- Do not change the plan title — the first # heading — unless asked to.\n")

	if len(r.Notes) > 0 {
		out.WriteString("\n## What the reviewer said\n\n")
		out.WriteString(numbered(r.Notes))
	}

	if change := textdiff.Unified(r.Submitted, r.Plan); change != "" {
		out.WriteString("\n## What the reviewer changed in the plan\n\n")
		out.WriteString(change)
		out.WriteString("\nThe marked words are the edit: [-removed-] and {+added+}. These changes are\n" +
			"what the reviewer wants, so keep them — do not restore wording they took out.\n")
	}
	return out.String()
}

// numbered lays the notes out as a list, each one saying what part of the plan it
// is about. A note's own line is gone from the plan by the time this is read, so
// without the part named there is an instruction and nothing to apply it to.
func numbered(left []notes.Note) string {
	var out strings.Builder
	for i, note := range left {
		fmt.Fprintf(&out, "%d. %s\n", i+1, about(note))
		for _, line := range strings.Split(note.Said, "\n") {
			fmt.Fprintf(&out, "   > %s\n", line)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// about names the part of the plan a note is about.
func about(note notes.Note) string {
	if note.About == notes.Whole {
		return "About the whole plan"
	}
	line := referent(note)
	if line.Num == 0 {
		return "About the plan"
	}
	return "About plan line " + strconv.Itoa(line.Num) + `: "` + shortened(line.Text) + `"`
}

// referent is the plan line a note most likely points at. An aside usually
// follows the text it is about, so the line above comes first; the line below is
// all a note written at the very top of a plan has.
func referent(note notes.Note) notes.Line {
	if note.Above.Num != 0 {
		return note.Above
	}
	return note.Below
}

// quoteWidth is how much of a plan line is quoted to say which one a note is
// about. Plan prose runs to paragraph-long lines, and the line number already
// locates it — the quote is only there to confirm the agent is looking at the
// right one.
const quoteWidth = 80

// shortened trims a quoted line to something that reads as a label.
func shortened(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= quoteWidth {
		return text
	}
	return string([]rune(text)[:quoteWidth]) + "…"
}
