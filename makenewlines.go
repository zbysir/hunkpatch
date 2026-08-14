package hunkpatch

import (
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Port of dist/aider_port/diff_lines.js and make_new_lines_explicit.js.

// dmpDiffTimeout corresponds to `dmp.Diff_Timeout = 5` (seconds).
//
// This is an inherent source of divergence from the JS: once diff-match-patch
// times out it degrades to a coarse diff, and native Go is far faster than the
// same code under a JS engine — for a large enough input the JS side can time
// out while Go has not, and the two produce different results for reasons that
// have nothing to do with the port being correct. The parity suite deliberately
// stays away from inputs big enough to hit it.
const dmpDiffTimeout = 5 * time.Second

// dmpDiffLines corresponds to diff_lines.js diffLines(searchText, replaceText):
// a line-level diff via diff-match-patch, returned as lines prefixed with
// ' ', '-' or '+' and each carrying its own newline.
func dmpDiffLines(searchText, replaceText string) []string {
	searchText = normalizeLineEndings(searchText)
	replaceText = normalizeLineEndings(replaceText)

	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = dmpDiffTimeout

	searchLines, replaceLines, mapping := dmp.DiffLinesToChars(searchText, replaceText)
	diffs := dmp.DiffMain(searchLines, replaceLines, false)
	diffs = dmp.DiffCleanupSemantic(diffs)
	diffs = dmp.DiffCleanupEfficiency(diffs)
	diffs = dmp.DiffCharsToLines(diffs, mapping)

	var udiff []string
	for _, d := range diffs {
		var prefix string
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			prefix = "-"
		case diffmatchpatch.DiffInsert:
			prefix = "+"
		default:
			prefix = " "
		}
		splitLines := strings.Split(d.Text, "\n")
		for i, line := range splitLines {
			// Skip the last segment when it is empty — that is just the tail
			// left behind by a trailing newline.
			if i < len(splitLines)-1 || line != "" {
				udiff = append(udiff, prefix+line+"\n")
			}
		}
	}
	return udiff
}

// makeNewLinesExplicit corresponds to make_new_lines_explicit.js
// makeNewLinesExplicit.
//
// It uses the real file content to complete the hunk's context: models
// routinely give partial or slightly wrong context, so this first aligns the
// hunk's "before" side to the file with diff-match-patch, then regenerates a
// hunk with full context via unidiff. If any step looks unreliable it returns
// the hunk it was given, unchanged.
func makeNewLinesExplicit(content string, hunk []string) []string {
	content = normalizeLineEndings(content)
	hunk = normalizeAll(hunk)

	before, after := hunkToBeforeAfter(hunk)

	diff := dmpDiffLines(before, content)
	backDiff := make([]string, 0, len(diff))
	for _, line := range diff {
		if strings.HasPrefix(line, "+") {
			continue
		}
		backDiff = append(backDiff, line)
	}

	directResult, ok := directlyApplyBackDiff(before, backDiff)
	if !ok || jsLength(jsTrim(directResult)) < 10 {
		return hunk
	}

	beforeLines := strings.Split(before, "\n")
	newBeforeLines := strings.Split(directResult, "\n")
	afterLines := strings.Split(after, "\n")

	// Losing too many lines during alignment means the alignment is not
	// trustworthy; give up.
	if float64(len(newBeforeLines)) < float64(len(beforeLines))*0.66 {
		return hunk
	}

	withNewline := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = l + "\n"
		}
		return out
	}
	patch := unidiffDiffLinesFromArrays(withNewline(newBeforeLines), withNewline(afterLines))

	maxContextLines := len(newBeforeLines)
	if len(afterLines) > maxContextLines {
		maxContextLines = len(afterLines)
	}
	formatted, err := unidiffFormatLines(patch, maxContextLines)
	if err != nil {
		// In JS, checkAndAssignTypes throws and the exception propagates out
		// past applyHunks. Go has no exceptions, so this falls back to the
		// original hunk — the one behavioural difference in the port, and it
		// only shows up on inputs where the JS would have aborted the whole
		// call.
		return hunk
	}

	newHunk := linesAfterHunkHeader(formatted)
	out := make([]string, len(newHunk))
	for i, l := range newHunk {
		out[i] = l + "\n"
	}
	return out
}

// directlyApplyBackDiff corresponds to make_new_lines_explicit.js
// directlyApplyBackDiff.
func directlyApplyBackDiff(text string, diff []string) (string, bool) {
	var result strings.Builder
	lines := strings.Split(text, "\n")
	lineIndex := 0
	for _, diffLine := range diff {
		units := jsUnits(diffLine)
		if len(units) == 0 {
			continue
		}
		op := rune(units[0])
		content := jsString(units[1:])
		switch op {
		case ' ':
			result.WriteString(content)
			lineIndex++
		case '-':
			lineIndex++
		case '+':
			result.WriteString(content)
		}
		if lineIndex > len(lines) {
			return "", false
		}
	}
	return result.String(), true
}
