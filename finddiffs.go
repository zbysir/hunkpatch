package hunkpatch

import "strings"

// Port of findDiffs / processFencedBlock from aider_udiff.js.
//
// findDiffs only looks inside ```diff fences — callers that hold a bare diff
// (Apply, for one) wrap it in a fence first.

// FileDiff is one file's worth of hunks, as returned by FindDiffs.
type FileDiff struct {
	// OldFileName is the path from the `--- ` header with any a/ or b/ prefix
	// removed, or "unknown" when the diff carried no matching header.
	OldFileName string
	// NewFileName is the path from the `+++ ` header, prefix removed.
	NewFileName string
	// Hunks holds each hunk as a slice of lines, newline included, with the
	// leading ' ', '-' or '+' operator still attached.
	Hunks [][]string
}

// rawEdit is the [fname, hunk] pair that processFencedBlock yields. fname is
// null in JS, which is represented here by hasName == false.
type rawEdit struct {
	fname   string
	hasName bool
	hunk    []string
}

// processFencedBlock corresponds to processFencedBlock(lines, startLineNum).
// Every element of lines is one line WITH its trailing newline.
func processFencedBlock(lines []string, startLineNum int) (int, []rawEdit) {
	lines = normalizeAll(lines)

	lineNum := startLineNum
	for ; lineNum < len(lines); lineNum++ {
		if strings.HasPrefix(lines[lineNum], "```") {
			break
		}
	}

	end := lineNum
	if end > len(lines) {
		end = len(lines)
	}
	start := startLineNum
	if start > end {
		start = end
	}
	block := make([]string, 0, end-start+1)
	block = append(block, lines[start:end]...)
	block = append(block, "@@ @@\n")

	fname := ""
	hasName := false
	if len(block) >= 2 && strings.HasPrefix(block[0], "--- ") && strings.HasPrefix(block[1], "+++ ") {
		fname = jsTrim(block[1][4:])
		hasName = true
		block = block[2:]
	}

	var edits []rawEdit
	keeper := false
	var hunk []string

	for _, line := range block {
		hunk = append(hunk, line)
		if jsLength(line) < 2 {
			continue
		}

		// A `--- ` immediately followed by a `+++ ` means the next file's diff
		// has started.
		if strings.HasPrefix(line, "+++ ") && len(hunk) >= 2 && strings.HasPrefix(hunk[len(hunk)-2], "--- ") {
			if len(hunk) >= 3 && hunk[len(hunk)-3] == "\n" {
				hunk = hunk[:len(hunk)-3]
			} else {
				hunk = hunk[:len(hunk)-2]
			}
			edits = append(edits, rawEdit{fname: fname, hasName: hasName, hunk: hunk})
			hunk = nil
			keeper = false
			fname = jsTrim(line[4:])
			hasName = true
			continue
		}

		op := rune(jsUnits(line)[0])
		if op == '-' || op == '+' {
			keeper = true
			continue
		}
		if op != '@' {
			continue
		}
		if !keeper {
			hunk = nil
			continue
		}
		hunk = hunk[:len(hunk)-1] // hunk.pop() — drop this @@ line
		edits = append(edits, rawEdit{fname: fname, hasName: hasName, hunk: hunk})
		hunk = nil
		keeper = false
	}

	return lineNum + 1, edits
}

// FindDiffs splits a patch into per-file hunks. It corresponds to
// findDiffs(content) and only reads what is inside ```diff fences; a bare diff
// has to be fenced first (Apply does that for you).
//
// Hunks with no `--- `/`+++ ` header pair are dropped, matching the original.
func FindDiffs(content string) []FileDiff {
	content = normalizeLineEndings(content)

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	// First pass: record new file name -> old file name for every ---/+++ pair.
	contentLines := strings.Split(content, "\n")
	fileHeaders := map[string]string{}
	for i := 0; i < len(contentLines)-1; i++ {
		if strings.HasPrefix(contentLines[i], "--- ") && strings.HasPrefix(contentLines[i+1], "+++ ") {
			oldFileName := jsTrim(contentLines[i][4:])
			newFileName := jsTrim(contentLines[i+1][4:])
			fileHeaders[newFileName] = oldFileName
		}
	}

	// Put the newline back on every line, matching `.map(line => line + "\n")`.
	lines := make([]string, len(contentLines))
	for i, l := range contentLines {
		lines[i] = l + "\n"
	}

	var rawEdits []rawEdit
	lineNum := 0
	for lineNum < len(lines) {
		for lineNum < len(lines) {
			if strings.HasPrefix(lines[lineNum], "```diff") {
				next, edits := processFencedBlock(lines, lineNum+1)
				lineNum = next
				rawEdits = append(rawEdits, edits...)
				break
			}
			lineNum++
		}
	}

	// Group by file, keeping first-seen order (JS uses a Map).
	type fileData struct {
		oldFileName string
		hunks       [][]string
	}
	order := make([]string, 0, len(rawEdits))
	fileMap := map[string]*fileData{}
	for _, e := range rawEdits {
		if !e.hasName || e.fname == "" {
			continue
		}
		d, ok := fileMap[e.fname]
		if !ok {
			oldName := fileHeaders[e.fname]
			if oldName == "" {
				oldName = "unknown"
			}
			d = &fileData{oldFileName: oldName}
			fileMap[e.fname] = d
			order = append(order, e.fname)
		}
		d.hunks = append(d.hunks, append([]string(nil), e.hunk...))
	}

	out := make([]FileDiff, 0, len(order))
	for _, name := range order {
		d := fileMap[name]
		out = append(out, FileDiff{
			OldFileName: cleanFilePath(d.oldFileName),
			NewFileName: cleanFilePath(name),
			Hunks:       d.hunks,
		})
	}
	return out
}

// cleanFilePath strips a leading a/ or b/, as findDiffs does.
func cleanFilePath(path string) string {
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		return path[2:]
	}
	return path
}
