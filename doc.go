// Package hunkpatch applies sloppy, LLM-generated unified diffs to text.
//
// Language models do not produce diffs that `patch` or `git apply` will accept.
// They get line numbers wrong, copy the surrounding context imperfectly, shift
// the indentation, wrap the patch in an `*** Begin Patch` envelope, or emit a
// hunk that is nothing but an anchor line. A strict patcher rejects all of that.
// This package accepts it, using the fuzzy hunk-application algorithm from
// aider: match the change block exactly, and when that fails, retry with less
// and less surrounding context until something lines up.
//
// # Where this code comes from
//
// The algorithm is aider's (https://github.com/Aider-AI/aider, Apache-2.0),
// which was ported to TypeScript as the npm package llm-diff-patcher@0.2.1
// (MIT). This package is a line-by-line Go port of that npm package's
// `aider_port` directory. The JS files it corresponds to:
//
//	dist/aider_port/normalize_utils.js
//	dist/aider_port/search_replace.js
//	dist/aider_port/aider_udiff.js
//	dist/aider_port/diff_lines.js
//	dist/aider_port/make_new_lines_explicit.js
//	dist/aider_port/apply_hunk.js
//
// plus the parts of its two third-party dependencies that are actually reached:
// unidiff's diffLines/formatLines (which embeds jsdiff's Myers implementation)
// and diff-match-patch (here: github.com/sergi/go-diff).
//
// # Porting principle: faithful first, better second
//
// The port reproduces the original byte for byte, including its dead code and
// its half-finished pieces. For example search_replace.js destructures a
// `relativeIndent` flag and then never uses it, which means aider's
// RelativeIndenter was never actually ported to JS; that gap is preserved here
// and marked with a comment. Improvements are opt-in and live behind Options,
// so that "we changed the behaviour" is never confused with "we ported it
// wrong".
//
// Equivalence is not a claim, it is a test: every function below was compared
// against the JS original running in a JS engine, over several hundred inputs,
// and the recorded results are checked in under testdata/. See the README for
// how that suite is built and why comparing only the final output is not
// enough.
//
// # Typical use
//
//	out, err := hunkpatch.Apply(source, modelProducedDiff)
//
// Apply targets a single file. For a patch that spans several files, use
// FindDiffs to split it and ApplyHunk to apply each hunk to the right file.
package hunkpatch
