package hunkpatch

import "strings"

// Port of dist/aider_port/aider_udiff.js and normalize_utils.js.

// normalizeLineEndings corresponds to normalize_utils.js normalizeLineEndings.
// Note that JS writes `if (!text) return ""`, so the empty string takes that
// branch too.
func normalizeLineEndings(text string) string {
	if text == "" {
		return ""
	}
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func normalizeAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = normalizeLineEndings(l)
	}
	return out
}

// hunkToBeforeAfter corresponds to hunkToBeforeAfter(hunk, returnLines=false):
// split a hunk into its "before" and "after" text.
//
// The `line.length < 2` test must use the JS UTF-16 length, not the byte
// length: one CJK character has length 1 in JS (< 2, so the whole line counts
// as context) and 3 in bytes (>= 2, so its first byte would be taken as the
// operator and the remaining two would become mojibake).
//
// before/after are joined with "" because each hunk line already carries its
// own newline — no separator may be added here.
func hunkToBeforeAfter(hunk []string) (string, string) {
	before, after := hunkToBeforeAfterLines(hunk)
	return strings.Join(before, ""), strings.Join(after, "")
}

// hunkToBeforeAfterLines corresponds to hunkToBeforeAfter(hunk, returnLines=true).
func hunkToBeforeAfterLines(hunk []string) ([]string, []string) {
	hunk = normalizeAll(hunk)
	var before, after []string
	for _, line := range hunk {
		var op rune
		var lineContent string
		units := jsUnits(line)
		if len(units) < 2 {
			op = ' '
			lineContent = line
		} else {
			op = rune(units[0])
			lineContent = jsString(units[1:])
		}
		switch op {
		case ' ':
			before = append(before, lineContent)
			after = append(after, lineContent)
		case '-':
			before = append(before, lineContent)
		case '+':
			after = append(after, lineContent)
		}
		// Any other first character (a CJK character, a '\\', ...) makes the
		// original drop the line entirely: it goes into neither before nor after.
	}
	return before, after
}

// flexiJustSearchAndReplace corresponds to flexiJustSearchAndReplace(texts).
//
// When opts.IndentTolerant is set, an indentation-tolerant strategy (see
// indent.go) is appended after the original ones. That is an addition, not part
// of the port; with the option off the behaviour is identical to the JS.
func flexiJustSearchAndReplace(texts [3]string, opts Options) (string, bool) {
	for i := range texts {
		texts[i] = normalizeLineEndings(texts[i])
	}
	strategies := []strategyWithPreprocs{
		{strategy: searchAndReplace, preprocs: allPreprocs},
	}
	if opts.IndentTolerant {
		// Last, so it only runs once every exact strategy has failed. Nothing
		// that succeeds today can change its result.
		strategies = append(strategies, strategyWithPreprocs{
			strategy: indentTolerantSearchAndReplace,
			preprocs: [][3]bool{{false, false, false}},
		})
	}
	return flexibleSearchAndReplace(texts, strategies)
}

// directlyApplyHunk corresponds to directlyApplyHunk(content, hunk).
func directlyApplyHunk(content string, hunk []string, opts Options) (string, bool) {
	content = normalizeLineEndings(content)
	hunk = normalizeAll(hunk)

	before, after := hunkToBeforeAfter(hunk)
	if before == "" {
		return "", false
	}

	beforeLines, _ := hunkToBeforeAfterLines(hunk)
	trimmed := make([]string, len(beforeLines))
	for i, l := range beforeLines {
		trimmed[i] = jsTrim(l)
	}
	beforeLinesJoined := strings.Join(trimmed, "\n")

	// If the context is shorter than 10 characters once whitespace is removed
	// and it occurs more than once in the file, refuse: there is not enough
	// context to tell which occurrence was meant. `content.split(before).length > 2`
	// means "appears at least twice". The threshold is a UTF-16 length as well:
	// five CJK characters count as 5 in JS and 15 in bytes.
	if jsLength(beforeLinesJoined) < 10 &&
		strings.Contains(content, before) &&
		strings.Count(content, before)+1 > 2 {
		return "", false
	}

	// The original wraps this in a try/catch for SearchTextNotUnique, but this
	// port's searchAndReplace never throws it. Dead code.
	return flexiJustSearchAndReplace([3]string{before, after, content}, opts)
}

// aider_udiff.js also exports normalizeHunk (and its helper
// cleanupPureWhitespaceLines), which rebuilds a hunk from a fresh line diff of
// its before/after sides. It is deliberately NOT ported:
//
//   - nothing in the apply path reaches it, so it was never covered by the
//     differential suite that every other function here passed;
//   - its output lines carry no trailing newline and inherit the double-newline
//     artefact described in unidiff.go, so the result cannot be fed back into
//     ApplyHunk. Exporting it would be handing callers a trap.
//
// If you need it, it is a dozen lines against hunkToBeforeAfterLines and
// unidiffFormatLines — but verify it against the JS first.

// linesAfterHunkHeader returns every non-empty line after the @@ header,
// matching the JS block that finds the first line starting with "@@", starts
// from the line after it, and filters out empty strings.
func linesAfterHunkHeader(formatted string) []string {
	lines := strings.Split(formatted, "\n")
	startIndex := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			startIndex = i + 1
			break
		}
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines[startIndex:] {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
