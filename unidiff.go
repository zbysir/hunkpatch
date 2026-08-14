package hunkpatch

import (
	"fmt"
	"strconv"
	"strings"
)

// This file ports the parts of npm `unidiff@1.0.4` that are actually reached:
//   node_modules/unidiff/index.js  — diffLines / formatLines
//   node_modules/unidiff/hunk.js   — makeHunks / Hunk / LineChange / lineChanges
//
// The index arithmetic here (nthIndexOf, substring) uses UTF-16 code unit
// offsets in JS, but every offset is derived from locating the ASCII character
// "\n" and is only ever passed between functions in this file, so Go byte
// offsets give identical results and no UTF-16 conversion is needed.

// Change types, corresponding to ADDED / REMOVED / UNMODIFIED in hunk.js.
const (
	typeAdded      = "+"
	typeRemoved    = "-"
	typeUnmodified = "s"
)

// lineChange corresponds to LineChange in hunk.js.
type lineChange struct {
	typ  string
	text string
}

func (l lineChange) unified() string {
	return type2unified(l.typ) + l.text
}

func type2unified(typ string) string {
	if typ == typeUnmodified {
		return " "
	}
	return typ
}

// unidiffHunk corresponds to Hunk in hunk.js.
type unidiffHunk struct {
	aoff    int
	boff    int
	changes []lineChange
}

// calcLen corresponds to calcLen(linechanges, ab) in hunk.js.
func calcLen(changes []lineChange, aWeight, bWeight int) (int, error) {
	length := 0
	for _, c := range changes {
		switch c.typ {
		case typeRemoved:
			length += aWeight
		case typeAdded:
			length += bWeight
		case typeUnmodified:
			length++
		default:
			return 0, fmt.Errorf("unknown change type: %s", c.typ)
		}
	}
	return length, nil
}

func (h unidiffHunk) alen() int {
	n, _ := calcLen(h.changes, 1, 0)
	return n
}

func (h unidiffHunk) blen() int {
	n, _ := calcLen(h.changes, 0, 1)
	return n
}

// unifiedHeader corresponds to Hunk.prototype.unifiedHeader.
func (h unidiffHunk) unifiedHeader() string {
	alen, blen := h.alen(), h.blen()
	aStr, bStr := "", ""
	if alen != 1 {
		aStr = "," + strconv.Itoa(alen)
	}
	if blen != 1 {
		bStr = "," + strconv.Itoa(blen)
	}
	afudg, bfudg := 1, 1
	if alen == 0 {
		afudg = 0
	}
	if blen == 0 {
		bfudg = 0
	}
	return "@@ -" + strconv.Itoa(h.aoff+afudg) + aStr + " +" + strconv.Itoa(h.boff+bfudg) + bStr + " @@"
}

// unified corresponds to Hunk.prototype.unified.
func (h unidiffHunk) unified() string {
	parts := []string{h.unifiedHeader()}
	for _, c := range h.changes {
		parts = append(parts, c.unified())
	}
	return strings.Join(parts, "\n")
}

// nthIndexOf corresponds to nthIndexOf in hunk.js.
func nthIndexOf(s, v string, from, n int, reverse bool) int {
	d := 1
	if reverse {
		d = -1
	}
	from -= d
	for c := 0; c < n; c++ {
		if reverse {
			from = jsLastIndexOf(s, v, from+d)
		} else {
			from = jsIndexOf(s, v, from+d)
		}
	}
	return from
}

// jsIndexOf corresponds to String.prototype.indexOf(v, from).
func jsIndexOf(s, v string, from int) int {
	if from < 0 {
		from = 0
	}
	if from > len(s) {
		if v == "" {
			return len(s)
		}
		return -1
	}
	idx := strings.Index(s[from:], v)
	if idx < 0 {
		return -1
	}
	return from + idx
}

// jsLastIndexOf corresponds to String.prototype.lastIndexOf(v, from): search
// backwards from `from`, where a match may start at `from` itself.
func jsLastIndexOf(s, v string, from int) int {
	if from < 0 {
		if v == "" {
			return 0
		}
		return -1
	}
	end := from + len(v)
	if end > len(s) {
		end = len(s)
	}
	return strings.LastIndex(s[:end], v)
}

// lineChangesOf corresponds to lineChanges(change, select) in hunk.js.
// selectSet == false means JS passed undefined for select.
func lineChangesOf(change jsChange, sel int, selectSet bool) []lineChange {
	if selectSet && sel == 0 {
		return nil
	}
	v := change.Value
	var lines []string
	switch {
	case !selectSet:
		lines = strings.Split(v, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	case sel > 0:
		i := nthIndexOf(v, "\n", 0, sel, false)
		lines = strings.Split(jsSubstring(v, 0, i), "\n")
	default:
		length := len(v)
		if length > 0 && v[length-1] == '\n' {
			length--
		}
		i := nthIndexOf(v, "\n", length-1, -sel, true)
		lines = strings.Split(jsSubstring(v, i+1, length), "\n")
	}

	typ := changeType(change)
	out := make([]lineChange, 0, len(lines))
	for _, l := range lines {
		out = append(out, lineChange{typ: typ, text: l})
	}
	return out
}

// jsSubstring corresponds to String.prototype.substring(start, end): indexes
// are clamped to the string, and start > end swaps the two.
func jsSubstring(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > len(s) {
		start = len(s)
	}
	if end > len(s) {
		end = len(s)
	}
	if start > end {
		start, end = end, start
	}
	return s[start:end]
}

// changeType corresponds to changeType in index.js.
func changeType(c jsChange) string {
	switch {
	case c.Added:
		return typeAdded
	case c.Removed:
		return typeRemoved
	default:
		return typeUnmodified
	}
}

// checkAndAssignTypes corresponds to checkAndAssignTypes in index.js. JS throws
// when two adjacent runs have the same type; this returns an error instead.
func checkAndAssignTypes(changes []jsChange) error {
	if len(changes) == 0 {
		return nil
	}
	for i := 1; i < len(changes); i++ {
		if changeType(changes[i-1]) == changeType(changes[i]) {
			return fmt.Errorf("repeating change types are not handled: %s (at %d and %d)",
				changeType(changes[i-1]), i-1, i)
		}
	}
	return nil
}

// makeHunks corresponds to makeHunks in hunk.js.
func makeHunks(changes []jsChange, precontext, postcontext int) []unidiffHunk {
	var ret []unidiffHunk
	var lchanges []lineChange
	lskipped := 0

	finishHunk := func() {
		if len(lchanges) == 0 {
			return
		}
		aoff, boff := lskipped, lskipped
		if len(ret) > 0 {
			prev := ret[len(ret)-1]
			aoff += prev.aoff + prev.alen()
			boff += prev.boff + prev.blen()
		}
		ret = append(ret, unidiffHunk{aoff: aoff, boff: boff, changes: lchanges})
		lchanges = nil
		lskipped = 0
	}

	for ci, change := range changes {
		if changeType(change) == typeUnmodified {
			ctxAfter := 0
			if ci > 0 {
				ctxAfter = postcontext
			}
			ctxBefore := 0
			if ci < len(changes)-1 {
				ctxBefore = precontext
			}
			skip := change.Count - (ctxAfter + ctxBefore)
			if skip < 0 {
				skip = 0
			}
			if skip > 0 {
				lchanges = append(lchanges, lineChangesOf(change, ctxAfter, true)...)
				finishHunk()
				lchanges = append(lchanges, lineChangesOf(change, -ctxBefore, true)...)
				lskipped = skip
			} else {
				lchanges = append(lchanges, lineChangesOf(change, 0, false)...)
			}
		} else {
			lchanges = append(lchanges, lineChangesOf(change, 0, false)...)
		}
	}
	finishHunk()
	return ret
}

// unidiffDiffLinesFromArrays corresponds to diffLines(a, b) in index.js for the
// array-of-lines call shape.
//
// Note `a.join("\n") + "\n"`: callers pass elements that ALREADY end in "\n"
// (makeNewLinesExplicit builds them with `lines.map(line => line + "\n")`), and
// joining with "\n" again puts a blank line between them. That is the
// original's behaviour, reproduced here.
func unidiffDiffLinesFromArrays(a, b []string) []jsChange {
	as := strings.Join(a, "\n") + "\n"
	bs := strings.Join(b, "\n") + "\n"
	ret, ok := jsDiffLines(as, bs)
	if !ok {
		return nil
	}
	if len(ret) == 1 && !ret[0].Added && !ret[0].Removed {
		return nil
	}
	return ret
}

// unidiffFormatLines corresponds to formatLines(changes, {context}) in index.js.
func unidiffFormatLines(changes []jsChange, context int) (string, error) {
	if err := checkAndAssignTypes(changes); err != nil {
		return "", err
	}
	hunks := makeHunks(changes, context, context)
	if len(hunks) == 0 {
		return "", nil
	}
	parts := []string{"--- a", "+++ b"}
	for _, h := range hunks {
		parts = append(parts, h.unified())
	}
	return strings.Join(parts, "\n"), nil
}
