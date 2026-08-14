package hunkpatch

// Port of dist/aider_port/apply_hunk.js.

// applyPartialHunk corresponds to
// applyPartialHunk(content, precedingContext, changes, followingContext).
//
// It retries with less and less context: full context first, then dropping one
// line at a time, trying every split between the leading and trailing side.
// This is the only "fuzzy" part of the applier — the matching itself is an
// exact substring match, and the tolerance comes from shrinking the context.
func applyPartialHunk(content string, precedingContext, changes, followingContext []string, opts Options) (string, bool) {
	lenPrec := len(precedingContext)
	lenFoll := len(followingContext)
	useAll := lenPrec + lenFoll

	for drop := 0; drop <= useAll; drop++ {
		use := useAll - drop
		for usePrec := lenPrec; usePrec >= 0; usePrec-- {
			if usePrec > use {
				continue
			}
			useFoll := use - usePrec
			if useFoll > lenFoll {
				continue
			}

			var thisPrec []string
			if usePrec > 0 {
				thisPrec = precedingContext[lenPrec-usePrec:]
			}
			thisFoll := followingContext[:useFoll]

			attempt := make([]string, 0, len(thisPrec)+len(changes)+len(thisFoll))
			attempt = append(attempt, thisPrec...)
			attempt = append(attempt, changes...)
			attempt = append(attempt, thisFoll...)

			if result, ok := directlyApplyHunk(content, attempt, opts); ok {
				return result, true
			}
		}
	}
	return "", false
}

// ApplyHunk applies a single hunk to content and reports whether it matched.
//
// A hunk is a slice of lines, each keeping its trailing newline and its leading
// operator: ' ' for context, '-' for removal, '+' for addition. A line shorter
// than two UTF-16 code units is treated as context.
//
// ok == false means the hunk could not be located, and the first return value
// is then "" rather than content.
//
// Beware that ok == true does NOT guarantee the text changed. A hunk with no
// context lines at all — only '-' and '+' — has no context/change/context
// triple to work with, so the matching loop never runs and the content comes
// back untouched with ok == true. That is upstream's behaviour and it is
// preserved here; ApplyWith compensates by treating an unchanged result as a
// skipped hunk.
//
// Corresponds to applyHunks(content, hunk) upstream — despite the plural, that
// function takes a single hunk.
func ApplyHunk(content string, hunk []string) (string, bool) {
	return ApplyHunkWith(content, hunk, Options{})
}

// ApplyHunkWith is ApplyHunk with the opt-in strategies in Options enabled.
func ApplyHunkWith(content string, hunk []string, opts Options) (string, bool) {
	content = normalizeLineEndings(content)
	hunk = normalizeAll(hunk)

	// Try a direct application first; the vast majority of hunks stop here.
	if res, ok := directlyApplyHunk(content, hunk, opts); ok {
		return res, true
	}

	enhancedHunk := makeNewLinesExplicit(content, hunk)

	// Collapse each line's operator into a single character: '-' and '+' both
	// become 'x', and '\n' (a line that is nothing but a newline) becomes ' '.
	// A shape like " xx  x " can then be cut into alternating context/change
	// sections.
	ops := make([]uint16, len(enhancedHunk))
	for i, line := range enhancedHunk {
		u := jsFirstUnit(normalizeLineEndings(line))
		if u == 0 {
			u = ' ' // JS: `|| " "`
		}
		switch u {
		case '-', '+':
			u = 'x'
		case '\n':
			u = ' '
		}
		ops[i] = u
	}

	var sections [][]string
	currentOp := uint16(' ')
	var currentSection []string
	for i, op := range ops {
		if op != currentOp {
			if len(currentSection) > 0 {
				sections = append(sections, currentSection)
			}
			currentSection = nil
			currentOp = op
		}
		currentSection = append(currentSection, enhancedHunk[i])
	}
	if len(currentSection) > 0 {
		sections = append(sections, currentSection)
	}
	if currentOp != ' ' {
		sections = append(sections, []string{})
	}

	modifiedContent := normalizeLineEndings(content)
	allDone := true
	// sections alternates [context, changes, context, changes, context...], so
	// each step takes one (preceding context, changes, following context) triple.
	for i := 2; i < len(sections); i += 2 {
		precedingContext := sections[i-2]
		changes := sections[i-1]
		followingContext := sections[i]
		res, ok := applyPartialHunk(modifiedContent, precedingContext, changes, followingContext, opts)
		if !ok {
			allDone = false
			break
		}
		modifiedContent = res
	}
	if !allDone {
		return "", false
	}
	return modifiedContent, true
}
