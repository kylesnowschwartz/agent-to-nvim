// Package notes separates what a human wrote for the agent from what they wrote
// for the draft's audience.
//
// A human editing a draft answers in two registers at once: they fix the text,
// and they leave asides about it — "make this shorter", "check the date". Both
// arrive in the same file. Left mixed together, an aside rides out to Slack with
// the message it was about, so the two are told apart here and only the draft
// text is handed back as the draft.
//
// Most asides are about a particular part of the draft, and the line one was
// written on is gone from the draft by the time the agent reads it. So each of
// those carries the draft lines either side of it, numbered as the draft it came
// out of. Some asides are about the draft as a whole — "too formal", "no
// rollback section anywhere" — and have no referent to carry; those are written
// with a wider marker and reported without any lines.
package notes

import "strings"

// Markers introduce a line meant for the agent rather than for the draft's
// audience. Either has to start the line: an indented marker is content — a
// quoted reply inside a code block, say — and a human who indents an aside gets
// it sent rather than acted on, which is the safer way round to be wrong.
const (
	partMarker  = ">>"
	wholeMarker = ">>>"
)

// Scope says how much of the draft a note is about.
type Scope int

const (
	// Part is a note about the lines it was written between. It is the zero
	// Scope because it is what an aside written against the text is.
	Part Scope = iota
	// Whole is a note about the draft as a whole, written wherever there was
	// room for it rather than against any particular line.
	Whole
)

// Note is one aside to the agent. A Part note carries the draft either side of
// it: an aside is usually written under the text it is about, so Above is the
// likelier referent and Below is there to bound it. A Whole note carries
// neither, because where it happened to be typed says nothing about what it is
// for.
type Note struct {
	Said  string
	About Scope
	Above Line
	Below Line
}

// Line is one line of the draft Split returns — the text the agent is handed —
// numbered from 1. The zero Line means there is no such line, which is what a
// note at the very start or very end of a draft has on one side.
type Line struct {
	Num  int
	Text string
}

// placed is a note and how many draft lines came before it. Where it sits can
// only be turned into surrounding lines once the whole draft is known. What it
// says is kept line by line so a run of marker lines can be joined without
// losing the blank line a human left between two paragraphs of it.
type placed struct {
	said  []string
	about Scope
	after int
}

// Split returns the draft with its note lines removed, and what those lines say.
// A dropped line takes its newline with it, so the surrounding text closes up as
// if the note had never been typed.
func Split(text string) (draft string, left []Note) {
	if !strings.Contains(text, partMarker) {
		return text, nil
	}

	var kept []string
	var found []placed
	for rest := text; rest != ""; {
		line := rest
		if end := strings.IndexByte(rest, '\n'); end >= 0 {
			line, rest = rest[:end+1], rest[end+1:]
		} else {
			rest = ""
		}

		if said, about, ok := read(line); ok {
			found = add(found, said, about, len(kept))
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, ""), place(found, kept)
}

// read reports whether the line is an aside to the agent, what it says, and how
// much of the draft it is about. The wider marker is looked for first, since
// ">>>" also starts with ">>".
func read(line string) (said string, about Scope, ok bool) {
	bare := strings.TrimRight(line, "\r\n")
	if rest, found := strings.CutPrefix(bare, wholeMarker); found {
		return strings.TrimSpace(rest), Whole, true
	}
	if rest, found := strings.CutPrefix(bare, partMarker); found {
		return strings.TrimSpace(rest), Part, true
	}
	return "", Part, false
}

// add takes in what one marker line said, joining it to the note before it when
// that note is the line immediately above and carries the same marker. A thought
// too long for one line is one note about one thing, and reporting it as a note
// per line would have the agent answer the same point several times over.
//
// Two runs are adjacent only if no draft line came between them, which is what
// comparing the count of kept lines says. A blank line between two markers is a
// draft line, so it separates them: a human who wants two notes has a way to ask
// for two.
func add(found []placed, said string, about Scope, after int) []placed {
	if last := len(found) - 1; last >= 0 && found[last].after == after && found[last].about == about {
		found[last].said = append(found[last].said, said)
		return found
	}
	return append(found, placed{said: []string{said}, about: about, after: after})
}

// place attaches each note to the draft lines around it. A note about the whole
// draft gets none: it was written wherever there was room for it, so the text it
// happens to sit between says nothing about what it is for.
func place(found []placed, lines []string) []Note {
	var notes []Note
	for _, f := range found {
		note := Note{
			Said:  strings.Trim(strings.Join(f.said, "\n"), "\n"),
			About: f.about,
		}
		if f.about == Part {
			note.Above = nearest(lines, f.after-1, -1)
			note.Below = nearest(lines, f.after, 1)
		}
		notes = append(notes, note)
	}
	return notes
}

// nearest walks from at in the given direction for the first line with text on
// it. Blank lines are stepped over: a note written under a paragraph usually has
// an empty line above it, and an empty line says nothing about what the note is
// for.
func nearest(lines []string, at, step int) Line {
	for ; at >= 0 && at < len(lines); at += step {
		text := strings.TrimRight(lines[at], "\r\n")
		if strings.TrimSpace(text) != "" {
			return Line{Num: at + 1, Text: text}
		}
	}
	return Line{}
}
