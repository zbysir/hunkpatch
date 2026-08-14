package hunkpatch

import "strings"

// The indentation-tolerant strategy. This is an ADDITION, not part of the port:
// the JS implementation has no counterpart.
//
// Why it is needed. The original matcher is a plain substring match, which
// makes wrong indentation behave strangely:
//
//	If the file indents with 14 spaces and the model wrote 12, a single-line
//	hunk matches by accident ("12 spaces + <small>" is a substring of
//	"14 spaces + <small>", starting at the third space), while a multi-line hunk
//	always fails, because the second line's indentation has to line up on a line
//	boundary. When the model writes MORE indentation than the file has, even the
//	single-line case fails.
//
// Real aider covers both cases with the strip_blank_lines × relative_indent
// combinations in all_preprocs, but the npm port only ever implemented strip:
// the relative_indent flag is destructured and thrown away.
//
// Rather than copy aider's RelativeIndenter (which picks a marker character
// that does not occur in the source, encodes relative indentation with it, then
// decodes back — both the failure modes and the decode step are hard to reason
// about), this takes a more direct and more conservative route, with guards
// aider does not have:
//
//  1. THE MATCH MUST BE UNIQUE. Ignoring indentation makes context less
//     distinctive, and the same fragment appearing at two nesting levels is
//     common (two `<strong>¥ 8,420</strong>`, say). More than one candidate is
//     an immediate refusal, never a guess. This is the most important guard
//     here: guessing wrong silently corrupts the file.
//  2. THE SHIFT MUST BE UNIFORM. Every line's indentation must differ from the
//     file's by the same prefix string, otherwise refuse. Comparing prefixes
//     rather than lengths keeps tabs and spaces from being conflated.
//  3. '+' LINES ARE REWRITTEN WITH THE FILE'S INDENTATION, so the model's wrong
//     indentation never reaches the file.
//  4. It runs last, after every exact strategy has failed, so nothing that
//     succeeds today can change.

// lineSpan records where a line sits in the original text. end points at the
// end of the line, excluding the newline.
type lineSpan struct {
	text  string
	start int
	end   int
}

// splitLineSpans splits text into lines and records each line's byte range.
func splitLineSpans(s string) []lineSpan {
	var out []lineSpan
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, lineSpan{text: s[start:i], start: start, end: i})
			start = i + 1
		}
	}
	if start <= len(s) {
		out = append(out, lineSpan{text: s[start:], start: start, end: len(s)})
	}
	return out
}

// contentLines splits text into content lines: the empty string produced by a
// trailing newline does not count as a line.
func contentLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// splitIndent splits a line into its leading whitespace and the rest.
func splitIndent(line string) (indent, rest string) {
	rest = strings.TrimLeft(line, " \t")
	return line[:len(line)-len(rest)], rest
}

// trimBlankEdges drops blank lines at both ends, keeping blank lines in the
// middle — those are real content.
//
// This step is required. Apply wraps the diff in a fence as
// "```diff\n" + diff + "\n```", and the diff itself usually already ends in a
// newline, so the hunk reliably picks up one extra blank line at the end.
// hunkToBeforeAfter counts it as a context line and the search block gains a
// line that does not exist. The exact strategies get away with it because
// stripBlankLines trims it off; matching that tolerance here is what makes the
// strategy fire on real input at all.
func trimBlankEdges(lines []string) []string {
	i, j := 0, len(lines)
	for i < j && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	for j > i && strings.TrimSpace(lines[j-1]) == "" {
		j--
	}
	return lines[i:j]
}

// indentShift describes how the hunk's indentation differs from the file's.
type indentShift struct {
	prefix string // the leading whitespace they differ by
	add    bool   // true: file = prefix + hunk; false: hunk = prefix + file
}

// resolveIndentShift checks that every line differs by the same prefix string.
// Each pair is (indentation in the file, indentation in the hunk).
func resolveIndentShift(pairs [][2]string) (indentShift, bool) {
	// Find the first pair with real content and differing indentation to
	// establish the prefix.
	var shift indentShift
	found := false
	for _, p := range pairs {
		fileIndent, hunkIndent := p[0], p[1]
		if fileIndent == hunkIndent {
			continue
		}
		switch {
		case strings.HasSuffix(fileIndent, hunkIndent):
			shift = indentShift{prefix: fileIndent[:len(fileIndent)-len(hunkIndent)], add: true}
		case strings.HasSuffix(hunkIndent, fileIndent):
			shift = indentShift{prefix: hunkIndent[:len(hunkIndent)-len(fileIndent)], add: false}
		default:
			// Neither adding nor removing a prefix (tabs swapped for spaces,
			// for instance). Leave it alone.
			return indentShift{}, false
		}
		found = true
		break
	}
	if !found {
		// Every line already has the same indentation, so this strategy has
		// nothing to add; hand it back to exact matching.
		return indentShift{}, false
	}

	// Then verify every line agrees with that one shift.
	for _, p := range pairs {
		fileIndent, hunkIndent := p[0], p[1]
		if shift.add {
			if fileIndent != shift.prefix+hunkIndent {
				return indentShift{}, false
			}
		} else {
			if hunkIndent != shift.prefix+fileIndent {
				return indentShift{}, false
			}
		}
	}
	return shift, true
}

// applyShift converts one line's indentation to the file's convention.
func applyShift(line string, shift indentShift) (string, bool) {
	indent, rest := splitIndent(line)
	if rest == "" {
		// Leave blank and whitespace-only lines alone, so we never manufacture
		// a line of trailing spaces.
		return line, true
	}
	if shift.add {
		return shift.prefix + indent + rest, true
	}
	if !strings.HasPrefix(indent, shift.prefix) {
		// The prefix that should be removed is not there, so this line does not
		// follow the same convention as the others. Refuse.
		return "", false
	}
	return strings.TrimPrefix(indent, shift.prefix) + rest, true
}

// indentTolerantSearchAndReplace is the indentation-tolerant counterpart of
// searchAndReplace. It only acts when the whole block matches in exactly one
// place and the indentation is off by a uniform shift; otherwise it fails.
func indentTolerantSearchAndReplace(texts [3]string) (string, bool) {
	searchText, replaceText, originalText := texts[0], texts[1], texts[2]

	// An all-whitespace search block carries no information. Do not touch it.
	if strings.TrimSpace(searchText) == "" {
		return "", false
	}
	searchLines := trimBlankEdges(contentLines(searchText))
	if len(searchLines) == 0 {
		return "", false
	}

	origSpans := splitLineSpans(originalText)
	if len(searchLines) > len(origSpans) {
		return "", false
	}

	// Compare line by line ignoring indentation, collecting every hit.
	var matches []int
	for i := 0; i+len(searchLines) <= len(origSpans); i++ {
		ok := true
		for j, sl := range searchLines {
			_, want := splitIndent(sl)
			_, got := splitIndent(origSpans[i+j].text)
			if want != got {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, i)
		}
	}
	// Only a unique match may proceed — see the guard list at the top of the file.
	if len(matches) != 1 {
		return "", false
	}
	at := matches[0]

	// The indentation must be off by one uniform prefix. Blank lines abstain.
	pairs := make([][2]string, 0, len(searchLines))
	for j, sl := range searchLines {
		hunkIndent, rest := splitIndent(sl)
		if rest == "" {
			continue
		}
		fileIndent, _ := splitIndent(origSpans[at+j].text)
		pairs = append(pairs, [2]string{fileIndent, hunkIndent})
	}
	shift, ok := resolveIndentShift(pairs)
	if !ok {
		return "", false
	}

	// Rewrite the replacement with the file's indentation. Blank lines at the
	// edges come from the fence as well, so they go too.
	replaceLines := trimBlankEdges(contentLines(replaceText))
	shifted := make([]string, 0, len(replaceLines))
	for _, rl := range replaceLines {
		s, ok := applyShift(rl, shift)
		if !ok {
			return "", false
		}
		shifted = append(shifted, s)
	}

	// Splice it back. The replaced range covers the matched lines plus the last
	// line's newline — whole lines including the newline, which matches the
	// substring semantics of the exact strategies.
	spanStart := origSpans[at].start
	spanEnd := origSpans[at+len(searchLines)-1].end
	hasTrailingNewline := spanEnd < len(originalText)
	if hasTrailingNewline {
		spanEnd++ // include the newline
	}

	var block strings.Builder
	for i, s := range shifted {
		if i > 0 {
			block.WriteString("\n")
		}
		block.WriteString(s)
	}
	if len(shifted) > 0 && hasTrailingNewline {
		block.WriteString("\n")
	}

	return originalText[:spanStart] + block.String() + originalText[spanEnd:], true
}
