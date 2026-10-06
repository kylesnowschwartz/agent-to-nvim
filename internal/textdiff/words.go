package textdiff

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Comparison decides what counts as a change and describes it. The zero value
// compares words and ignores whitespace, which is what a draft passed through
// an editor needs: formatters re-indent, trim, and re-wrap on save, and a
// line-by-line report of that buries the one edit the human actually made.
type Comparison struct {
	// WhitespaceCounts compares the texts byte for byte and reports the change
	// line by line, so indentation, trailing spaces, blank lines, and line
	// breaks are changes like any other.
	WhitespaceCounts bool
}

// Same reports whether after holds nothing the comparison counts as a change
// from before.
func (c Comparison) Same(before, after string) bool {
	if c.WhitespaceCounts {
		return before == after
	}
	return sameWords(before, after)
}

// Report describes the change from before to after, or returns the empty
// string when Same holds.
func (c Comparison) Report(before, after string) string {
	if c.WhitespaceCounts {
		return Unified(before, after)
	}
	return wordDiff(before, after)
}

func sameWords(before, after string) bool {
	return slices.Equal(wordTexts(splitWords(splitLines(before))), wordTexts(splitWords(splitLines(after))))
}

// word is one token of a version: a run of letters and digits, or a single
// other character. Splitting punctuation off on its own is what lets markup
// formatters through: they break lines between "><" where there was no
// whitespace, and a whitespace-delimited word would change with them.
type word struct {
	text string
	line int // 1-based
	// spaced is whether whitespace or a line start came before the word in its
	// own version, so the report can print words the way that version spaced
	// them.
	spaced bool
}

func splitWords(lines []string) []word {
	var words []word
	for i, line := range lines {
		spaced := true
		start := -1
		emit := func(end int) {
			words = append(words, word{text: line[start:end], line: i + 1, spaced: spaced})
			spaced, start = false, -1
		}
		for at, r := range line {
			if wordRune(r) {
				if start < 0 {
					start = at
				}
				continue
			}
			if start >= 0 {
				emit(at)
			}
			if unicode.IsSpace(r) {
				spaced = true
				continue
			}
			start = at
			emit(at + utf8.RuneLen(r))
		}
		if start >= 0 {
			emit(len(line))
		}
	}
	return words
}

// wordRune is a character that belongs to a word rather than standing alone.
// "on line" and "online" stay different edits; "<b>" and "< b >" do not.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
}

// wordLike reports whether a token carries a word rather than punctuation,
// which is what the similarity rule counts: shared tags and commas survive any
// rewrite by coincidence.
func wordLike(text string) bool {
	return strings.IndexFunc(text, wordRune) >= 0
}

func wordTexts(words []word) []string {
	texts := make([]string, len(words))
	for i, w := range words {
		texts[i] = w.text
	}
	return texts
}

// step is one move in the alignment of the two word sequences. was and now
// index each version's words; the side a word is absent from holds -1.
type step struct {
	kind     kind
	was, now int
}

// wordDiff reports the change as the edited version's lines, each marked with
// the words that moved. Comparing words rather than lines is what lets a
// re-wrapped paragraph with one changed word come back as that one word.
func wordDiff(before, after string) string {
	if sameWords(before, after) {
		return ""
	}
	afterLines := splitLines(after)
	was, now := splitWords(splitLines(before)), splitWords(afterLines)
	steps, ok := alignWords(was, now)
	if !ok {
		return Unified(before, after)
	}
	return render(project(steps, was, now, afterLines), printed)
}

// alignWords pairs the two word sequences up the way align pairs lines. ok is
// false when the differing middle is too large to align, so the caller can
// fall back to the cheaper line comparison.
func alignWords(was, now []word) (steps []step, ok bool) {
	head := 0
	for head < len(was) && head < len(now) && was[head].text == now[head].text {
		head++
	}
	tail := 0
	for tail < len(was)-head && tail < len(now)-head &&
		was[len(was)-1-tail].text == now[len(now)-1-tail].text {
		tail++
	}
	wasMiddle, nowMiddle := was[head:len(was)-tail], now[head:len(now)-tail]
	if len(wasMiddle)*len(nowMiddle) > pairBudget {
		return nil, false
	}

	steps = make([]step, 0, len(was)+len(now))
	for i := 0; i < head; i++ {
		steps = append(steps, step{kind: equal, was: i, now: i})
	}
	i, j := head, head
	for _, r := range alignMiddle(wordTexts(wasMiddle), wordTexts(nowMiddle), 0) {
		switch r.kind {
		case equal:
			steps = append(steps, step{kind: equal, was: i, now: j})
			i, j = i+1, j+1
		case removed:
			steps = append(steps, step{kind: removed, was: i, now: -1})
			i++
		case added:
			steps = append(steps, step{kind: added, was: -1, now: j})
			j++
		}
	}
	for k := 0; k < tail; k++ {
		steps = append(steps, step{kind: equal, was: i + k, now: j + k})
	}
	return steps, true
}

// piece is one word as an edited line shows it, spaced as the version it came
// from spaced it.
type piece struct {
	kind   kind
	text   string
	spaced bool
}

// editedLine is what one line of the edited version shows: its words in order
// with what happened to each, and the original lines deleted outright just
// after it.
type editedLine struct {
	pieces  []piece
	deleted [][]piece
}

// projection places the aligned words onto the edited version's lines, which
// are the lines the agent is about to act on.
type projection struct {
	steps []step
	was   []word
	now   []word
	// slots[n] is edited line n; slots[0] holds what was deleted before the
	// first line.
	slots []editedLine
	// gone marks the original lines that lost every word, which read better
	// shown whole than scattered as inline removals.
	gone map[int]bool
}

func project(steps []step, was, now []word, afterLines []string) []row {
	p := projection{
		steps: steps,
		was:   was,
		now:   now,
		slots: make([]editedLine, len(afterLines)+1),
		gone:  whollyRemovedLines(steps, was),
	}
	p.place()
	return p.rows(afterLines)
}

func whollyRemovedLines(steps []step, was []word) map[int]bool {
	total, lost := map[int]int{}, map[int]int{}
	for _, s := range steps {
		if s.was < 0 {
			continue
		}
		line := was[s.was].line
		total[line]++
		if s.kind == removed {
			lost[line]++
		}
	}
	gone := map[int]bool{}
	for line, n := range total {
		if lost[line] == n {
			gone[line] = true
		}
	}
	return gone
}

// place walks the alignment once. A removed word goes inline next to the edited
// word it sits beside; a wholly deleted original line goes after the edited
// line it followed.
func (p *projection) place() {
	nextWord := p.nextWordSteps()
	lastLine := 0     // edited line of the last kept or added word
	lastKeptWas := -1 // original line of the last kept word
	var waiting []piece
	open := deletedBlock{wasLine: -1}

	for k, s := range p.steps {
		if s.kind != removed {
			line := p.now[s.now].line
			p.slots[line].pieces = append(p.slots[line].pieces, waiting...)
			w := p.now[s.now]
			p.slots[line].pieces = append(p.slots[line].pieces, piece{kind: s.kind, text: w.text, spaced: w.spaced})
			waiting = nil
			lastLine = line
			if s.kind == equal {
				lastKeptWas = p.was[s.was].line
			}
			continue
		}

		w := p.was[s.was]
		if p.gone[w.line] {
			if open.wasLine != w.line {
				slot := &p.slots[lastLine]
				slot.deleted = append(slot.deleted, nil)
				open = deletedBlock{wasLine: w.line, slot: lastLine, index: len(slot.deleted) - 1}
			}
			block := &p.slots[open.slot].deleted[open.index]
			*block = append(*block, piece{kind: removed, text: w.text, spaced: w.spaced})
			continue
		}

		removal := piece{kind: removed, text: w.text, spaced: w.spaced}
		if p.attachesForward(w.line, lastLine, lastKeptWas, nextWord[k]) {
			waiting = append(waiting, removal)
			continue
		}
		p.slots[lastLine].pieces = append(p.slots[lastLine].pieces, removal)
	}
	if len(waiting) > 0 {
		p.slots[lastLine].pieces = append(p.slots[lastLine].pieces, waiting...)
	}
}

// deletedBlock locates the deleted original line currently being collected.
type deletedBlock struct {
	wasLine     int
	slot, index int
}

// nextWordSteps maps each step to the first step at or after it that places a
// word in the edited version, or -1 when none follows.
func (p *projection) nextWordSteps() []int {
	next := make([]int, len(p.steps))
	following := -1
	for k := len(p.steps) - 1; k >= 0; k-- {
		if p.steps[k].kind != removed {
			following = k
		}
		next[k] = following
	}
	return next
}

// attachesForward decides whether a removed word belongs with the edited word
// after it rather than the one before. A replacement stays with what replaced
// it; otherwise the word stays with the neighbour that shared its original line.
func (p *projection) attachesForward(wasLine, lastLine, lastKeptWas, next int) bool {
	if next < 0 {
		return false
	}
	if lastLine == 0 || p.steps[next].kind == added {
		return true
	}
	if lastKeptWas == wasLine {
		return false
	}
	return p.was[p.steps[next].was].line == wasLine
}

// rows lays the projection out as printable rows in edited-line order.
func (p *projection) rows(afterLines []string) []row {
	var rows []row
	for n, slot := range p.slots {
		if n > 0 {
			rows = append(rows, lineRows(afterLines[n-1], slot.pieces, n)...)
		}
		for _, words := range slot.deleted {
			rows = append(rows, row{kind: removed, text: "- " + joinKinds(words, removed), num: n + 1})
		}
	}
	return rows
}

// lineRows renders one edited line: as context when none of its words moved,
// as an addition when all of them are new, and otherwise word by word unless
// too little survived for markers to read well.
func lineRows(raw string, pieces []piece, num int) []row {
	kept, lost, fresh := 0, 0, 0
	moved := false
	for _, pc := range pieces {
		if pc.kind != equal {
			moved = true
		}
		if !wordLike(pc.text) {
			continue
		}
		switch pc.kind {
		case equal:
			kept++
		case removed:
			lost++
		case added:
			fresh++
		}
	}
	if !moved {
		return []row{{kind: equal, text: "  " + raw, num: num}}
	}

	indent := raw[:len(raw)-len(strings.TrimLeftFunc(raw, unicode.IsSpace))]
	if allKind(pieces, added) {
		return []row{{kind: added, text: "+ " + indent + joinKinds(pieces, equal, added), num: num}}
	}
	if float64(kept) < similarityThreshold*float64(max(kept+lost, kept+fresh)) {
		return []row{
			{kind: removed, text: "- " + indent + joinKinds(pieces, equal, removed), num: num},
			{kind: added, text: "+ " + indent + joinKinds(pieces, equal, added), num: num},
		}
	}
	return []row{{kind: added, text: "~ " + indent + markWords(pieces), num: num}}
}

func allKind(pieces []piece, want kind) bool {
	for _, pc := range pieces {
		if pc.kind != want {
			return false
		}
	}
	return true
}

// joinKinds joins the words of the given kinds, which is one side of the line:
// kept and removed words are the original, kept and added the edit.
func joinKinds(pieces []piece, kinds ...kind) string {
	var picked []piece
	for _, pc := range pieces {
		if slices.Contains(kinds, pc.kind) {
			picked = append(picked, pc)
		}
	}
	return spaced(picked)
}

// spaced joins pieces with a space wherever their own version had whitespace.
// The first piece sits against whatever precedes the joined text.
func spaced(pieces []piece) string {
	var out strings.Builder
	for i, pc := range pieces {
		if i > 0 && pc.spaced {
			out.WriteString(" ")
		}
		out.WriteString(pc.text)
	}
	return out.String()
}

// markWords wraps runs of moved words in [-removed-] and {+added+}, one pair of
// markers per run. A removal directly followed by its replacement is written
// without a space between, as the line diff writes it.
func markWords(pieces []piece) string {
	var out strings.Builder
	for at := 0; at < len(pieces); {
		run := at
		for run < len(pieces) && pieces[run].kind == pieces[at].kind {
			run++
		}
		text := spaced(pieces[at:run])

		replacement := at > 0 && pieces[at-1].kind == removed && pieces[at].kind == added
		if at > 0 && !replacement && pieces[at].spaced {
			out.WriteString(" ")
		}
		switch pieces[at].kind {
		case equal:
			out.WriteString(text)
		case removed:
			out.WriteString("[-" + text + "-]")
		case added:
			out.WriteString("{+" + text + "+}")
		}
		at = run
	}
	return out.String()
}

// printed formats rows that already hold their printed text.
func printed(rows []row) []string {
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.text
	}
	return lines
}
