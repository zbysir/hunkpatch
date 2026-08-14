package hunkpatch

import "strings"

// This file ports jsdiff (npm `diff@5`) diffLines:
//   node_modules/unidiff/node_modules/diff/lib/diff/base.js
//   node_modules/unidiff/node_modules/diff/lib/diff/line.js
//
// Only the line-diff path is ported. unidiff calls it with no options at all
// (no ignoreWhitespace, newlineIsToken, stripTrailingCr, comparator,
// ignoreCase, maxEditLength or timeout), so those branches in base.js are
// unreachable; this specialises to "tokens are []string, equality is ==",
// otherwise line for line with the original.
//
// This is deliberately not "some Go diff library". When several shortest edit
// scripts exist, which one Myers returns depends on the diagonal iteration
// order and on how addPath/removePath are chosen; different implementations
// group added/removed differently. unidiff.formatLines turns that grouping
// straight into hunk text, so the tie-breaking has to be copied exactly.

// jsChange is jsdiff's change object: one run of the same operation.
type jsChange struct {
	Count   int
	Value   string
	Added   bool
	Removed bool
}

// jsComponent is the chained component accumulated in base.js.
type jsComponent struct {
	count             int
	added             bool
	removed           bool
	previousComponent *jsComponent
	value             string
}

// jsPath is an element of bestPath in base.js.
type jsPath struct {
	oldPos        int
	lastComponent *jsComponent
}

// jsTokenizeLines corresponds to lineDiff.tokenize in line.js (the no-options
// branch):
//
//	var retLines = [], linesAndNewlines = value.split(/(\n|\r\n)/);
//	if (!linesAndNewlines[linesAndNewlines.length - 1]) linesAndNewlines.pop();
//	for (i...) if (i % 2) retLines[last] += line; else retLines.push(line);
//
// The effect is to split the text into lines that keep their newline.
func jsTokenizeLines(value string) []string {
	// Splitting with a capturing group keeps the separators in the result, so
	// even indexes are lines and odd indexes are newlines. This builds that
	// directly instead of pulling in a regexp.
	var parts []string
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\n' {
			parts = append(parts, value[start:i]) // the line
			parts = append(parts, "\n")           // the separator
			start = i + 1
		}
	}
	parts = append(parts, value[start:])

	// if (!linesAndNewlines[last]) pop() — drop the trailing empty string
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}

	var retLines []string
	for i, part := range parts {
		if i%2 == 1 {
			// Odd index: a newline, appended back onto the previous line.
			if len(retLines) > 0 {
				retLines[len(retLines)-1] += part
			}
		} else {
			retLines = append(retLines, part)
		}
	}
	return retLines
}

// jsRemoveEmpty corresponds to removeEmpty in base.js: drop falsy (empty) tokens.
func jsRemoveEmpty(tokens []string) []string {
	ret := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t != "" {
			ret = append(ret, t)
		}
	}
	return ret
}

// jsDiffLines corresponds to jsdiff's diffLines(oldStr, newStr). The second
// return value covers the JS "returns undefined" case, which this path never
// reaches but which is kept for fidelity.
func jsDiffLines(oldStr, newStr string) ([]jsChange, bool) {
	oldString := jsRemoveEmpty(jsTokenizeLines(oldStr))
	newString := jsRemoveEmpty(jsTokenizeLines(newStr))

	newLen, oldLen := len(newString), len(oldString)
	editLength := 1
	maxEditLength := newLen + oldLen

	// In JS bestPath is a sparse array indexed by negative numbers too; a map
	// is the only equivalent way to express "undefined vs present".
	bestPath := map[int]*jsPath{0: {oldPos: -1}}

	newPos := jsExtractCommon(bestPath[0], newString, oldString, 0)
	if bestPath[0].oldPos+1 >= oldLen && newPos+1 >= newLen {
		return []jsChange{{
			Value: strings.Join(newString, ""),
			Count: len(newString),
		}}, true
	}

	// -Infinity / +Infinity stand in for a big enough range; the values used
	// always stay within ±editLength.
	const inf = int(^uint(0) >> 2)
	minDiagonalToConsider, maxDiagonalToConsider := -inf, inf

	// execEditLength corresponds to the inner function of the same name and
	// returns (result, done).
	execEditLength := func() ([]jsChange, bool) {
		lo := minDiagonalToConsider
		if -editLength > lo {
			lo = -editLength
		}
		hi := maxDiagonalToConsider
		if editLength < hi {
			hi = editLength
		}
		for diagonalPath := lo; diagonalPath <= hi; diagonalPath += 2 {
			removePath := bestPath[diagonalPath-1]
			addPath := bestPath[diagonalPath+1]
			if removePath != nil {
				delete(bestPath, diagonalPath-1)
			}

			canAdd := false
			if addPath != nil {
				addPathNewPos := addPath.oldPos - diagonalPath
				canAdd = 0 <= addPathNewPos && addPathNewPos < newLen
			}
			canRemove := removePath != nil && removePath.oldPos+1 < oldLen
			if !canAdd && !canRemove {
				delete(bestPath, diagonalPath)
				continue
			}

			var basePath *jsPath
			if !canRemove || (canAdd && removePath.oldPos+1 < addPath.oldPos) {
				basePath = jsAddToPath(addPath, true, false, 0)
			} else {
				basePath = jsAddToPath(removePath, false, true, 1)
			}

			newPos = jsExtractCommon(basePath, newString, oldString, diagonalPath)
			if basePath.oldPos+1 >= oldLen && newPos+1 >= newLen {
				return jsBuildValues(basePath.lastComponent, newString, oldString), true
			}
			bestPath[diagonalPath] = basePath
			if basePath.oldPos+1 >= oldLen && diagonalPath-1 < maxDiagonalToConsider {
				maxDiagonalToConsider = diagonalPath - 1
			}
			if newPos+1 >= newLen && diagonalPath+1 > minDiagonalToConsider {
				minDiagonalToConsider = diagonalPath + 1
			}
		}
		editLength++
		return nil, false
	}

	for editLength <= maxEditLength {
		if ret, done := execEditLength(); done {
			return ret, true
		}
	}
	return nil, false
}

// jsAddToPath corresponds to addToPath in base.js. JS passes true / undefined
// for added and removed; here that maps to true / false.
func jsAddToPath(path *jsPath, added, removed bool, oldPosInc int) *jsPath {
	last := path.lastComponent
	if last != nil && last.added == added && last.removed == removed {
		return &jsPath{
			oldPos: path.oldPos + oldPosInc,
			lastComponent: &jsComponent{
				count:             last.count + 1,
				added:             added,
				removed:           removed,
				previousComponent: last.previousComponent,
			},
		}
	}
	return &jsPath{
		oldPos: path.oldPos + oldPosInc,
		lastComponent: &jsComponent{
			count:             1,
			added:             added,
			removed:           removed,
			previousComponent: last,
		},
	}
}

// jsExtractCommon corresponds to extractCommon in base.js. It mutates basePath
// in place, as the original does.
func jsExtractCommon(basePath *jsPath, newString, oldString []string, diagonalPath int) int {
	newLen, oldLen := len(newString), len(oldString)
	oldPos := basePath.oldPos
	newPos := oldPos - diagonalPath
	commonCount := 0
	for newPos+1 < newLen && oldPos+1 < oldLen && newString[newPos+1] == oldString[oldPos+1] {
		newPos++
		oldPos++
		commonCount++
	}
	if commonCount > 0 {
		basePath.lastComponent = &jsComponent{
			count:             commonCount,
			previousComponent: basePath.lastComponent,
		}
	}
	basePath.oldPos = oldPos
	return newPos
}

// jsBuildValues corresponds to buildValues in base.js (the path where
// useLongestToken is falsy).
func jsBuildValues(lastComponent *jsComponent, newString, oldString []string) []jsChange {
	var components []*jsComponent
	for lastComponent != nil {
		components = append(components, lastComponent)
		lastComponent = lastComponent.previousComponent
	}
	// components.reverse()
	for i, j := 0, len(components)-1; i < j; i, j = i+1, j-1 {
		components[i], components[j] = components[j], components[i]
	}

	componentLen := len(components)
	newPos, oldPos := 0, 0
	for componentPos := 0; componentPos < componentLen; componentPos++ {
		component := components[componentPos]
		if !component.removed {
			component.value = strings.Join(newString[newPos:newPos+component.count], "")
			newPos += component.count
			if !component.added {
				oldPos += component.count
			}
		} else {
			component.value = strings.Join(oldString[oldPos:oldPos+component.count], "")
			oldPos += component.count
			// A removal directly after an addition is swapped so the removal
			// comes first.
			if componentPos > 0 && components[componentPos-1].added {
				components[componentPos-1], components[componentPos] = components[componentPos], components[componentPos-1]
			}
		}
	}

	// Merge a trailing component into the previous one when its value is empty.
	if componentLen > 1 {
		final := components[componentLen-1]
		if (final.added || final.removed) && final.value == "" {
			components[componentLen-2].value += final.value
			components = components[:componentLen-1]
		}
	}

	out := make([]jsChange, 0, len(components))
	for _, c := range components {
		out = append(out, jsChange{
			Count:   c.count,
			Value:   c.value,
			Added:   c.added,
			Removed: c.removed,
		})
	}
	return out
}
