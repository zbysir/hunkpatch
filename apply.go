package hunkpatch

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoHunks is returned when the diff contains nothing that can be read as a
// hunk — an empty string, prose with no `@@`/`-`/`+` lines, and so on.
var ErrNoHunks = errors.New("hunkpatch: no hunk found in diff")

// PartialError reports that the patch applied, but not completely. Apply and
// ApplyWith still return the best-effort text alongside it: the hunks that did
// match have been applied, and the ones in Skipped have not.
//
// This is worth handling rather than ignoring. A model that miscopied context
// produces a hunk that cannot be located, and without this you would write back
// a file that looks fine and is missing an edit.
//
// A hunk that leaves the text exactly as it was counts as skipped, since
// nothing would be written either way. That covers both the hunk whose context
// could not be found and the hunk that asks for no change at all (an
// anchor-only hunk, or one whose '-' and '+' lines are identical).
type PartialError struct {
	// Total is how many hunks the diff contained.
	Total int
	// Applied is how many of them changed the text.
	Applied int
	// Skipped holds the hunks that left the text unchanged, in the order they
	// appeared.
	Skipped [][]string
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("hunkpatch: %d of %d hunks could not be applied", e.Total-e.Applied, e.Total)
}

// Result is the full outcome of ApplyWith.
type Result struct {
	// Text is the patched text. When some hunks did not apply it still holds
	// the ones that did.
	Text string
	// Total is how many hunks the diff contained.
	Total int
	// Applied is how many of them changed the text.
	Applied int
	// Skipped holds the hunks that left the text unchanged; see PartialError.
	Skipped [][]string
}

// Apply applies diff to source and returns the patched text.
//
// diff is whatever the model produced: a unified diff, a hunk with no header, a
// patch wrapped in an `*** Begin Patch` envelope, a fenced ```diff block. Line
// numbers in `@@` headers are ignored — hunks are located by content.
//
// Apply targets a SINGLE file: any file headers in the diff are replaced with a
// placeholder before matching, so every hunk is applied to source. For a patch
// spanning several files, use FindDiffs to split it first and ApplyHunk to
// apply each hunk to its own file.
//
// The returned text is always usable. err is ErrNoHunks when the diff held no
// hunk at all (source is returned unchanged), or a *PartialError when some
// hunks could not be located (the rest have been applied).
func Apply(source, diff string) (string, error) {
	res, err := ApplyWith(source, diff, Options{})
	return res.Text, err
}

// ApplyWith is Apply with the opt-in strategies in Options enabled, and reports
// per-hunk detail in Result. The error follows the same contract as Apply, so a
// caller that already inspects Result can ignore it.
func ApplyWith(source, diff string, opts Options) (Result, error) {
	res := Result{Text: source}

	groups := FindDiffs(normalizeDiff(diff))
	if len(groups) == 0 {
		return res, ErrNoHunks
	}

	for _, group := range groups {
		for _, hunk := range group.Hunks {
			res.Total++
			// Matching the upstream behaviour: a hunk that cannot be located is
			// skipped, and the remaining hunks are still applied.
			//
			// "Applied" means the text actually changed, which is a stricter
			// test than ApplyHunk's ok. A hunk made only of '-' and '+' lines
			// with no context around it reports success even when its '-' line
			// is nowhere in the file (see ApplyHunk); counting that as applied
			// would hide the single most common way a model's patch fails.
			if next, ok := ApplyHunkWith(res.Text, hunk, opts); ok && next != res.Text {
				res.Text = next
				res.Applied++
			} else {
				res.Skipped = append(res.Skipped, hunk)
			}
		}
	}

	if res.Applied != res.Total {
		return res, &PartialError{Total: res.Total, Applied: res.Applied, Skipped: res.Skipped}
	}
	return res, nil
}

// normalizeDiff rewrites whatever the model produced into the one shape
// FindDiffs reads: a ```diff fence whose first two lines are a `--- `/`+++ `
// pair.
//
// Everything before the first `@@` is dropped, which takes with it envelopes
// such as `*** Begin Patch` / `*** Update File: x.tsx`, any prose the model put
// in front of the diff, an opening ```diff fence, and any real file headers —
// none of which matter when the target is a single known file. What remains
// gets a placeholder header, because FindDiffs discards hunks with no file
// header at all and models very often omit one.
//
// A diff with file headers but no `@@` anywhere keeps its own headers rather
// than being given a second pair, which would otherwise be read as two files.
func normalizeDiff(diff string) string {
	if i := strings.Index(diff, "@@"); i > 0 {
		diff = diff[i:]
	}
	if !startsWithFileHeaders(diff) {
		diff = "--- source\n+++ source\n" + diff
	}
	// A trailing ``` from an already-fenced input is harmless: processFencedBlock
	// stops at the first fence line it sees.
	return "```diff\n" + diff + "\n```"
}

// startsWithFileHeaders reports whether the first two lines are a `--- `/`+++ `
// pair, the shape processFencedBlock reads a file name from.
func startsWithFileHeaders(diff string) bool {
	first, rest, ok := strings.Cut(diff, "\n")
	if !ok {
		return false
	}
	second, _, _ := strings.Cut(rest, "\n")
	return strings.HasPrefix(first, "--- ") && strings.HasPrefix(second, "+++ ")
}
