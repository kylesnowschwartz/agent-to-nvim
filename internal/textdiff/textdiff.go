// Package textdiff reports what a human changed in a draft, so the agent acting
// on the edit reads the change rather than inferring it by comparing the result
// against whatever it still remembers writing.
//
// Changed lines are marked word by word. Drafts are prose, and a line-level diff
// turns a one-word edit into two rewritten lines — which is the noise that sends
// an agent back to guessing.
package textdiff

import (
	"fmt"
	"strings"
	"unicode"
)

// contextLines is how many unchanged lines surround a change. One is enough to
// place it: the full edited text is already on stdout.
const contextLines = 1

// maxRenderedLines bounds the report. A wholesale rewrite has nothing useful to
// say line by line, and the edited text itself is on stdout either way.
const maxRenderedLines = 200

// pairBudget bounds the quadratic alignment. Drafts sit far below it; anything
// above is reported as one wholesale replacement rather than left to grind.
const pairBudget = 4_000_000

type kind int

const (
	equal kind = iota
	removed
	added
)

// row is one line of one version, tagged with what happened to it.
type row struct {
	kind kind
	text string
	num  int // 1-based line number in the version it came from
}

// Unified describes the change from before to after. It returns the empty string
// when the two are identical.
func Unified(before, after string) string {
	if before == after {
		return ""
	}
	rows := align(splitLines(before), splitLines(after))
	return render(rows)
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// align pairs the two versions up line by line. Matching head and tail are
// trimmed first, which is what keeps the quadratic middle small for the usual
// edit: a change to one part of a draft.
func align(was, now []string) []row {
	head := 0
	for head < len(was) && head < len(now) && was[head] == now[head] {
		head++
	}
	tail := 0
	for tail < len(was)-head && tail < len(now)-head &&
		was[len(was)-1-tail] == now[len(now)-1-tail] {
		tail++
	}

	rows := make([]row, 0, len(was)+len(now))
	for i := 0; i < head; i++ {
		rows = append(rows, row{kind: equal, text: was[i], num: i + 1})
	}

	rows = append(rows, alignMiddle(was[head:len(was)-tail], now[head:len(now)-tail], head)...)

	for i := 0; i < tail; i++ {
		rows = append(rows, row{
			kind: equal,
			text: now[len(now)-tail+i],
			num:  len(now) - tail + i + 1,
		})
	}
	return rows
}

// alignMiddle walks the longest common subsequence of the two middles. offset is
// how many matching lines were trimmed from the head, so line numbers stay true
// to the whole file.
func alignMiddle(was, now []string, offset int) []row {
	if len(was) == 0 || len(now) == 0 || len(was)*len(now) > pairBudget {
		return wholesale(was, now, offset, offset)
	}

	// common[i][j] is the length of the longest common subsequence of was[i:] and
	// now[j:], filled from the end so the walk below can follow the larger side.
	common := make([][]int, len(was)+1)
	for i := range common {
		common[i] = make([]int, len(now)+1)
	}
	for i := len(was) - 1; i >= 0; i-- {
		for j := len(now) - 1; j >= 0; j-- {
			if was[i] == now[j] {
				common[i][j] = common[i+1][j+1] + 1
				continue
			}
			common[i][j] = max(common[i+1][j], common[i][j+1])
		}
	}

	rows := make([]row, 0, len(was)+len(now))
	i, j := 0, 0
	for i < len(was) && j < len(now) {
		switch {
		case was[i] == now[j]:
			rows = append(rows, row{kind: equal, text: now[j], num: offset + j + 1})
			i, j = i+1, j+1
		case common[i+1][j] >= common[i][j+1]:
			rows = append(rows, row{kind: removed, text: was[i], num: offset + i + 1})
			i++
		default:
			rows = append(rows, row{kind: added, text: now[j], num: offset + j + 1})
			j++
		}
	}
	return append(rows, wholesale(was[i:], now[j:], offset+i, offset+j)...)
}

// wholesale reports every line as replaced. Each side carries its own offset
// because the two versions have drifted apart by the time this is reached.
func wholesale(was, now []string, wasOffset, nowOffset int) []row {
	rows := make([]row, 0, len(was)+len(now))
	for i, text := range was {
		rows = append(rows, row{kind: removed, text: text, num: wasOffset + i + 1})
	}
	for i, text := range now {
		rows = append(rows, row{kind: added, text: text, num: nowOffset + i + 1})
	}
	return rows
}

// render turns the aligned rows into hunks: each change with contextLines of
// unchanged text around it, under a header naming the line it starts at.
func render(rows []row) string {
	var out strings.Builder
	printed := 0
	truncated := false

	for at := 0; at < len(rows); {
		if rows[at].kind == equal {
			at++
			continue
		}

		start, end := hunkBounds(rows, at)
		if printed > 0 {
			out.WriteString("\n")
		}
		fmt.Fprintf(&out, "@@ line %d @@\n", headerLine(rows, start))

		for _, text := range hunkLines(rows[start:end]) {
			if printed >= maxRenderedLines {
				truncated = true
				break
			}
			out.WriteString(text)
			out.WriteString("\n")
			printed++
		}
		if truncated {
			break
		}
		at = end
	}

	if truncated {
		out.WriteString("… diff truncated; the full edited text is on stdout\n")
	}
	return out.String()
}

// hunkBounds grows a hunk from a change at index at: context before, then every
// following change that is close enough to share the same hunk.
func hunkBounds(rows []row, at int) (start, end int) {
	start = max(at-contextLines, 0)
	end = at
	for end < len(rows) {
		next := nextChange(rows, end)
		if next < 0 || next-end > 2*contextLines {
			break
		}
		end = next + 1
	}
	return start, min(end+contextLines, len(rows))
}

func nextChange(rows []row, from int) int {
	for i := from; i < len(rows); i++ {
		if rows[i].kind != equal {
			return i
		}
	}
	return -1
}

// headerLine reports where the change sits in the edited version, which is the
// version the agent is about to act on — not where the hunk's leading context
// starts. For a pure removal that is the line the removed text sat above.
func headerLine(rows []row, start int) int {
	change := nextChange(rows, start)
	if change < 0 {
		return rows[start].num
	}
	for _, r := range rows[change:] {
		if r.kind != removed {
			return r.num
		}
	}
	return rows[change].num
}

// hunkLines renders one hunk, pairing each removed run with the added run that
// replaced it so the pair can be marked word by word.
func hunkLines(rows []row) []string {
	var lines []string
	for at := 0; at < len(rows); {
		switch rows[at].kind {
		case equal:
			lines = append(lines, "  "+rows[at].text)
			at++
		default:
			gone, run := texts(rows, at, removed)
			at = run
			fresh, run := texts(rows, at, added)
			at = run

			markedLines, alike := marked(gone, fresh)
			switch {
			case len(gone) > 0 && len(fresh) > 0 && alike:
				lines = append(lines, prefixEach("~ ", markedLines)...)
			case len(gone) > 0 && len(fresh) > 0:
				lines = append(lines, prefixEach("- ", gone)...)
				lines = append(lines, prefixEach("+ ", fresh)...)
			case len(gone) > 0:
				lines = append(lines, prefixEach("- ", gone)...)
			default:
				lines = append(lines, prefixEach("+ ", fresh)...)
			}
		}
	}
	return lines
}

func texts(rows []row, from int, want kind) (found []string, next int) {
	for next = from; next < len(rows) && rows[next].kind == want; next++ {
		found = append(found, rows[next].text)
	}
	return found, next
}

func prefixEach(prefix string, lines []string) []string {
	marked := make([]string, len(lines))
	for i, line := range lines {
		marked[i] = prefix + line
	}
	return marked
}

// similarityThreshold is the share of a block's words that must survive the
// edit for word marking to help. Below it the two versions have too little in
// common: the markers interleave scraps of both texts around coincidental
// shared words, which reads worse than the plain before and after.
const similarityThreshold = 0.3

// marked renders a replaced block with the words that actually moved wrapped in
// [-removed-] and {+added+}. alike is false when the block fails the similarity
// threshold; the caller shows the two versions whole instead.
//
// Neighbouring tokens of the same kind are joined into one pair of markers. A
// word and the space after it are separate tokens, so wrapping each on its own
// turns a two-word insertion into four sets of brackets.
func marked(gone, fresh []string) (lines []string, alike bool) {
	was := tokenize(strings.Join(gone, "\n"))
	now := tokenize(strings.Join(fresh, "\n"))

	var out strings.Builder
	rows := alignMiddle(was, now, 0)
	if !similar(rows) {
		return nil, false
	}
	for at := 0; at < len(rows); {
		run := at
		for run < len(rows) && rows[run].kind == rows[at].kind {
			run++
		}

		var text strings.Builder
		for _, r := range rows[at:run] {
			text.WriteString(r.text)
		}
		switch rows[at].kind {
		case equal:
			out.WriteString(text.String())
		case removed:
			out.WriteString("[-" + text.String() + "-]")
		case added:
			out.WriteString("{+" + text.String() + "+}")
		}
		at = run
	}
	return strings.Split(out.String(), "\n"), true
}

// similar reports whether enough words survived the edit for word marking to be
// worth reading. Only words count — whitespace tokens match by coincidence.
func similar(rows []row) bool {
	kept, was, now := 0, 0, 0
	for _, r := range rows {
		if strings.TrimSpace(r.text) == "" {
			continue
		}
		switch r.kind {
		case equal:
			kept++
			was++
			now++
		case removed:
			was++
		case added:
			now++
		}
	}
	return float64(kept) >= similarityThreshold*float64(max(was, now))
}

// tokenize splits into alternating runs of whitespace and non-whitespace, so
// alignment happens on words while the draft's own spacing survives in the
// output.
func tokenize(text string) []string {
	var tokens []string
	start, inSpace := 0, false
	for i, r := range text {
		space := unicode.IsSpace(r)
		if i == 0 {
			inSpace = space
			continue
		}
		if space != inSpace {
			tokens = append(tokens, text[start:i])
			start, inSpace = i, space
		}
	}
	if start < len(text) {
		tokens = append(tokens, text[start:])
	}
	return tokens
}
