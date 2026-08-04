// Package notes separates what a human wrote for the agent from what they wrote
// for the draft's audience.
//
// A human editing a draft answers in two registers at once: they fix the text,
// and they leave asides about it — "make this shorter", "check the date". Both
// arrive in the same file. Left mixed together, an aside rides out to Slack with
// the message it was about, so the two are told apart here and only the draft
// text is handed back as the draft.
package notes

import "strings"

// marker introduces a line meant for the agent. It has to start the line: an
// indented ">>" is content — a quoted reply inside a code block, say — and a
// human who indents an aside gets it sent rather than acted on, which is the
// safer way round to be wrong.
const marker = ">>"

// Split returns the draft with its note lines removed, and what those lines say.
// A dropped line takes its newline with it, so the surrounding text closes up as
// if the note had never been typed.
func Split(text string) (draft string, left []string) {
	if !strings.Contains(text, marker) {
		return text, nil
	}

	var kept strings.Builder
	kept.Grow(len(text))
	for rest := text; rest != ""; {
		line := rest
		if end := strings.IndexByte(rest, '\n'); end >= 0 {
			line, rest = rest[:end+1], rest[end+1:]
		} else {
			rest = ""
		}

		if said, ok := read(line); ok {
			left = append(left, said)
			continue
		}
		kept.WriteString(line)
	}
	return kept.String(), left
}

// read reports whether the line is an aside to the agent, and what it says.
func read(line string) (said string, ok bool) {
	if !strings.HasPrefix(line, marker) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimRight(line, "\r\n"), marker)), true
}
