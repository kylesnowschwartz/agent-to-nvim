// Package notes separates what a human wrote for the agent from what they wrote
// for the draft's audience.
//
// A human editing a draft answers in two registers at once: they fix the text,
// and they leave asides about it — "make this shorter", "check the date". Both
// arrive in the same file. Left mixed together, an aside rides out to Slack with
// the message it was about, so the two are told apart here and only the draft
// text is handed back as the draft.
//
// An aside is about a particular part of the draft, and the line it was written
// on is gone from the draft by the time the agent reads it. So each aside carries
// the draft lines either side of it, numbered as the draft it came out of.
package notes

import "strings"

// marker introduces a line meant for the agent. It has to start the line: an
// indented ">>" is content — a quoted reply inside a code block, say — and a
// human who indents an aside gets it sent rather than acted on, which is the
// safer way round to be wrong.
const marker = ">>"

// Note is one aside to the agent, with the draft either side of it. An aside is
// usually written under the text it is about, so Above is the likelier referent
// and Below is there to bound it.
type Note struct {
	Said  string
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
// only be turned into surrounding lines once the whole draft is known.
type placed struct {
	said  string
	after int
}

// Split returns the draft with its note lines removed, and what those lines say.
// A dropped line takes its newline with it, so the surrounding text closes up as
// if the note had never been typed.
func Split(text string) (draft string, left []Note) {
	if !strings.Contains(text, marker) {
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

		if said, ok := read(line); ok {
			found = append(found, placed{said: said, after: len(kept)})
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, ""), place(found, kept)
}

// read reports whether the line is an aside to the agent, and what it says.
func read(line string) (said string, ok bool) {
	if !strings.HasPrefix(line, marker) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimRight(line, "\r\n"), marker)), true
}

// place attaches each note to the draft lines around it.
func place(found []placed, lines []string) []Note {
	var notes []Note
	for _, f := range found {
		notes = append(notes, Note{
			Said:  f.said,
			Above: nearest(lines, f.after-1, -1),
			Below: nearest(lines, f.after, 1),
		})
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
