// Package notereport lays out the notes a human left on a draft, so the agent
// acting on them and the human who wrote them can both see which part of the
// draft each one is about.
//
// A note has no line of its own in the draft the agent acts on: the human typed
// it between two lines, and it was lifted out before the draft was handed on. So
// the report quotes the draft instead. Each stretch of the draft carrying notes is
// quoted once, numbered as the text on stdout is, with every note sitting where it
// was typed.
//
// Where a note sits is shown rather than described. A note printed above the lines
// around it reads as a claim about how far it reaches, and neighbouring notes then
// quote the same line twice, which reads as a slip in the numbering. Quoting each
// stretch once answers both.
package notereport

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/kylesnowschwartz/agent-to-nvim/internal/notes"
)

// marker introduces a note. It is the marker the human typed to write one, so the
// report needs no legend for it, and it sits in the left margin with the draft
// indented past it — the margin alone tells a note from the draft.
const marker = ">>"

// Render lays out every note against the draft it was written in, and returns
// nothing at all when there are no notes.
func Render(left []notes.Note) string {
	if len(left) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("notes:\n")
	width := numberWidth(left)
	for i, run := range runs(left) {
		if i > 0 {
			b.WriteString("\n")
		}
		writeRun(&b, run, width)
	}
	return b.String()
}

// numberWidth is the width of the line-number column, taken across every note so
// the column stays put from one stretch of the draft to the next.
func numberWidth(left []notes.Note) int {
	high := 0
	for _, n := range left {
		high = max(high, n.Above.Num, n.Below.Num)
	}
	return len(strconv.Itoa(high))
}

// runs gathers notes whose surrounding lines touch or overlap. Notes written into
// the same stretch of the draft belong in one quote of it; a note written
// somewhere else starts another.
func runs(left []notes.Note) [][]notes.Note {
	var out [][]notes.Note
	reach := 0
	for _, n := range left {
		lo, hi := span(n)
		if len(out) > 0 && lo <= reach {
			out[len(out)-1] = append(out[len(out)-1], n)
			reach = max(reach, hi)
			continue
		}
		out = append(out, []notes.Note{n})
		reach = hi
	}
	return out
}

// span is the draft lines a note sits between. A note at the very top or bottom
// of a draft has no line on one side, and that side closes onto the line it does
// have, so two notes written in the same place stay in one quote of the draft.
func span(n notes.Note) (lo, hi int) {
	lo, hi = n.Above.Num, n.Below.Num
	if lo == 0 {
		lo = hi
	}
	if hi == 0 {
		hi = lo
	}
	return lo, hi
}

// writeRun quotes one stretch of the draft with its notes in place: every line
// once, in draft order, each note under the line it was written below.
func writeRun(b *strings.Builder, run []notes.Note, width int) {
	for _, n := range run {
		if n.Above.Num == 0 {
			writeNote(b, n)
		}
	}
	for _, line := range anchors(run) {
		fmt.Fprintf(b, "   %*d  %s\n", width, line.Num, line.Text)
		for _, n := range run {
			if n.Above.Num == line.Num {
				writeNote(b, n)
			}
		}
	}
}

// writeNote prints what the human said, whole. The note is the only place the
// instruction exists, so nothing about it is abbreviated.
func writeNote(b *strings.Builder, n notes.Note) {
	fmt.Fprintf(b, "%s %s\n", marker, n.Said)
}

// anchors is every draft line the run's notes sit against, in draft order and once
// each. A line two neighbouring notes were written either side of is one line of
// the draft, and reads as one line here.
func anchors(run []notes.Note) []notes.Line {
	found := map[int]notes.Line{}
	for _, n := range run {
		for _, line := range []notes.Line{n.Above, n.Below} {
			if line.Num != 0 {
				found[line.Num] = line
			}
		}
	}

	lines := make([]notes.Line, 0, len(found))
	for _, num := range slices.Sorted(maps.Keys(found)) {
		lines = append(lines, found[num])
	}
	return lines
}
