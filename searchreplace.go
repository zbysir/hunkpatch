package hunkpatch

import "strings"

// Port of dist/aider_port/search_replace.js.
//
// Two things here are preserved exactly as they are, and both are worth
// recording:
//
//  1. searchAndReplace computes normalizedSearchText / normalizedOriginalText
//     and then never uses them; the comparison runs on the un-normalized
//     searchText / originalText. Dead code.
//
//  2. tryStrategy destructures a preproc triple into three flags but implements
//     only the first, stripBlankLines; relativeIndent and reverseLines are
//     destructured and dropped. That means aider's RelativeIndenter (relative
//     indentation matching) was never ported to JS at all — which is precisely
//     why a hunk indented with 12 spaces fails against a file indented with 14.
//     See indent.go for the opt-in strategy that fills the gap.

// searchReplaceFunc is one match-and-replace strategy. ok == false corresponds
// to returning undefined in JS.
type searchReplaceFunc func(texts [3]string) (result string, ok bool)

// allPreprocs corresponds to exports.allPreprocs.
//
// Because flags 2 and 3 are not implemented by tryStrategy, entries 3 and 4 are
// duplicates of entries 1 and 2 and are tried a second time for nothing. That
// is what the original does, so that is what this does.
var allPreprocs = [][3]bool{
	{false, false, false},
	{true, false, false},
	{false, true, false},
	{true, true, false},
}

// searchAndReplace corresponds to searchAndReplace(texts).
func searchAndReplace(texts [3]string) (string, bool) {
	searchText, replaceText, originalText := texts[0], texts[1], texts[2]
	if !strings.Contains(originalText, searchText) {
		return "", false
	}
	return jsReplaceFirst(originalText, searchText, replaceText), true
}

// stripBlankLines corresponds to stripBlankLines(texts): trim each of the three
// texts and put a single newline back.
func stripBlankLines(texts [3]string) [3]string {
	var out [3]string
	for i, t := range texts {
		out[i] = jsTrim(t) + "\n"
	}
	return out
}

// tryStrategy corresponds to tryStrategy(texts, strategy, preproc).
func tryStrategy(texts [3]string, strategy searchReplaceFunc, preproc [3]bool) (string, bool) {
	processed := texts
	if preproc[0] {
		processed = stripBlankLines(processed)
	}
	// preproc[1] (relativeIndent) and preproc[2] (reverseLines) have no
	// counterpart in the original implementation.
	return strategy(processed)
}

// strategyWithPreprocs pairs a strategy with the preproc combinations to try.
type strategyWithPreprocs struct {
	strategy searchReplaceFunc
	preprocs [][3]bool
}

// flexibleSearchAndReplace corresponds to flexibleSearchAndReplace(texts, strategies).
//
// Note that JS decides success with `if (result)`, and the empty string is
// falsy — an empty result counts as failure and the loop keeps going. The
// `result != ""` below is that semantic, not an accident.
func flexibleSearchAndReplace(texts [3]string, strategies []strategyWithPreprocs) (string, bool) {
	for _, s := range strategies {
		for _, preproc := range s.preprocs {
			if result, ok := tryStrategy(texts, s.strategy, preproc); ok && result != "" {
				return result, true
			}
		}
	}
	return "", false
}
