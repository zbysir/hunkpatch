# hunkpatch

[![Go Reference](https://pkg.go.dev/badge/github.com/zbysir/hunkpatch.svg)](https://pkg.go.dev/github.com/zbysir/hunkpatch)
[![CI](https://github.com/zbysir/hunkpatch/actions/workflows/ci.yml/badge.svg)](https://github.com/zbysir/hunkpatch/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/zbysir/hunkpatch)](https://goreportcard.com/report/github.com/zbysir/hunkpatch)

Apply the sloppy unified diffs that language models write.

A Go port of [aider][aider]'s fuzzy hunk applier, checked function by function
against the JavaScript implementation it was ported from.

[中文文档](README.zh-CN.md)

```go
out, err := hunkpatch.Apply(source, modelProducedDiff)
```

## The problem

`git apply` and `patch` reject what models produce. Line numbers are invented,
context is copied approximately, indentation drifts, and the patch arrives
wrapped in whatever the model felt like saying. All of the following are
ordinary model output, and all of them work here:

```diff
@@ -900,3 +900,3 @@          ← line numbers that match nothing
-  const t = useTranslations()
+  const t = useTranslations('about')
```

```
*** Begin Patch                ← an envelope instead of a header
*** Update File: config.ts
@@
-const port = 8080
+const port = 3000
*** End Patch
```

```diff
@@                             ← no header, no line numbers, wrong indentation
-    <small>今日会员消费</small>
+    <small>本日の会員利用額</small>
```

Hunks are located by content, never by line number.

## Install

```bash
go get github.com/zbysir/hunkpatch
```

Requires Go 1.21+. One dependency: `github.com/sergi/go-diff`.

## Use

```go
source := "func greet() string {\n\treturn \"hello\"\n}\n"
diff := "@@\n-\treturn \"hello\"\n+\treturn \"hello, world\"\n"

out, err := hunkpatch.Apply(source, diff)
```

`err` tells you three different things, and the returned text is usable in all
three cases:

| `err`             | meaning                                              |
| ----------------- | ---------------------------------------------------- |
| `nil`             | every hunk applied                                    |
| `ErrNoHunks`      | the diff contained no hunk; source returned unchanged |
| `*PartialError`   | some hunks applied, some could not be located         |

`*PartialError` is the one to actually handle. It is what a model miscopying
context looks like, and ignoring it means writing back a file that looks fine
and is missing an edit:

```go
out, err := hunkpatch.Apply(source, diff)

var partial *hunkpatch.PartialError
if errors.As(err, &partial) {
	log.Printf("only %d of %d hunks applied", partial.Applied, partial.Total)
	for _, hunk := range partial.Skipped {
		log.Printf("could not locate:\n%s", strings.Join(hunk, ""))
	}
}
```

`ApplyWith` returns the same detail as a `Result` value, plus options.

### Wrong indentation

The single most common way a model's hunk fails is copying the code correctly
and the indentation wrongly. `Options{IndentTolerant: true}` retries the match
ignoring leading whitespace, and rewrites the inserted lines with the *file's*
indentation rather than the model's:

```go
res, err := hunkpatch.ApplyWith(source, diff, hunkpatch.Options{IndentTolerant: true})
```

It only fires after every exact strategy has failed, and it refuses rather than
guesses: the block must match in exactly one place, and every line must be off
by the same indentation prefix. Ignoring indentation makes context less
distinctive — the same fragment at two nesting levels is common — and a wrong
guess here silently corrupts a file. See [indent.go](indent.go) for the full
list of guards.

### Several files in one patch

`Apply` targets one file. For a patch spanning several, split it first:

```go
for _, fd := range hunkpatch.FindDiffs(patch) {
	content := files[fd.NewFileName]
	for _, hunk := range fd.Hunks {
		if next, ok := hunkpatch.ApplyHunk(content, hunk); ok {
			content = next
		}
	}
	files[fd.NewFileName] = content
}
```

## How the matching works

There is no similarity scoring and no line-number arithmetic. A hunk is split
into its "before" and "after" text, and the before text is searched for as an
exact substring; each attempt is tried with and without blank-line trimming.

When that fails, two things happen in order:

1. **The context is rebuilt from the real file.** `makeNewLinesExplicit` aligns
   the hunk's before-side to the actual file content with diff-match-patch, then
   regenerates a hunk with complete context. This is what repairs a model that
   dropped or invented a context line.
2. **The context is thrown away, one line at a time.** The hunk is cut into
   (preceding context, changes, following context) triples, and each triple is
   retried with less and less context, trying every split between the two sides
   until something matches.

That second step is the whole of the "fuzziness": the match itself stays exact,
and tolerance comes from asking for less. It also explains the guard rails —
when the remaining context is under 10 characters and occurs more than once in
the file, the hunk is refused rather than applied to an arbitrary occurrence.

## Where this comes from, and why it is a port

The algorithm is [aider][aider]'s (Python, Apache-2.0). It was ported to
TypeScript as the npm package [llm-diff-patcher][npm]@0.2.1, and this is a Go
port of that package's `aider_port` directory.

Using the algorithm from Go otherwise means running the JavaScript: an embedded
JS engine, a bundled copy of the library shipped alongside your binary, and
whatever that engine costs in concurrency and startup — and any change to the
matching behaviour has to be made in JavaScript and rebuilt. A native
implementation drops all of it, and puts the algorithm somewhere it can be read,
tested and extended in Go.

Reaching for an existing Go diff library does not solve it either. Those
libraries are built to *produce* diffs; putting back a hunk whose context is
only approximately right is a different problem, and it is the problem models
create. And the piece that had to be ported rather than substituted is the Myers
implementation inside jsdiff: when several shortest edit scripts exist, which
one is returned depends on the diagonal iteration order and on how add/remove
paths break ties. Different implementations group additions and removals
differently, and `unidiff.formatLines` turns that grouping straight into hunk
text. Swapping in another library changes the output.

### Porting principle: faithful first, better second

The port reproduces the original including its dead code and its unfinished
parts, each marked with a comment:

- `searchAndReplace` computes two normalised strings and never uses them.
- `allPreprocs` has four entries, but `tryStrategy` implements only the first of
  its three flags, so entries 3 and 4 are duplicates of 1 and 2 and are tried
  twice for nothing.
- The `relativeIndent` flag is destructured and dropped, which means aider's
  `RelativeIndenter` was never ported to JavaScript at all. That gap is exactly
  why wrong indentation fails, and it is why `IndentTolerant` exists here — as
  an opt-in addition, so that "we changed the behaviour" is never confused with
  "we ported it wrong".

## How this was verified

Equivalence is not claimed, it is recorded. The port was developed against the
real llm-diff-patcher bundle running in a JS engine, and every case below is
checked into `testdata/` with the JavaScript side's answer:

| file                    | what it pins                                                | cases |
| ----------------------- | ----------------------------------------------------------- | ----- |
| `parity_apply.json`     | the whole pipeline, end to end                               | 409   |
| `parity_internal.json`  | 7 internal functions, per input                              | 29×7  |
| `parity_unidiff.json`   | jsdiff's Myers + unidiff's hunk formatter                    | 56    |
| `parity_finddiffs.json` | patch splitting, file names included                         | 10    |
| `js_semantics.json`     | the emulated JavaScript string primitives                    | 187   |

**Comparing only the final output is not enough.** The fallback chain is so
forgiving that an internal function can be wrong and the end result still comes
out right. Measured, not assumed: with 15 deliberate mutations planted in the
port, end-to-end comparison caught 8. Miscounted UTF-16 lengths, treating the
empty string as success, trying fewer preproc combinations, swapping Myers'
add/remove branches and one missing `pop()` in the tokenizer were all masked by
the fallbacks. Hence the per-function goldens.

**Reading the two implementations side by side is not enough either.** The
things that actually break a port of JavaScript are invisible in the structure:

- `String.replace` with a string argument replaces only the **first** match.
- `.length` counts **UTF-16 code units**. `"今日会员消费"` is 6 in JavaScript and
  18 in bytes — and `directlyApplyHunk` branches on `length < 10`.
- The empty string is **falsy**, so a strategy that returns `""` counts as a
  failure and the search continues to the next one.
- `String.prototype.trim` treats U+FEFF as whitespace and U+0085 as not;
  Go's `strings.TrimSpace` does the exact opposite on both.

Each of those has a test that a mutation proved the other cases could not
distinguish. `js_semantics.json` is generated by running the primitives through
node (`node testdata/js_semantics.mjs`), so those expectations come from a real
JavaScript engine rather than from someone's reading of the spec — regenerating
it needs nothing but node. The `parity_*.json` files were captured from the
original bundle and are checked in as-is; reproducing them requires
llm-diff-patcher@0.2.1 and a JS runtime.

Currently 549 test cases, 96% statement coverage.

## Deliberate differences from the original

1. **`Options{IndentTolerant}`** — an addition; off by default. See above.
2. **`makeNewLinesExplicit` on malformed input.** Where JavaScript throws (two
   adjacent changes of the same type reaching `checkAndAssignTypes`), the
   exception escapes past `applyHunks` and aborts the whole call. Go has no
   exceptions here, so it falls back to the un-rewritten hunk and carries on.
   This is the one behavioural difference in the algorithm itself.
3. **Partial application is reported.** The original silently skips a hunk it
   cannot place and returns the text as if nothing was wrong. So does the
   algorithm here — but `Apply` tells you about it through `*PartialError`.
   Relatedly, a hunk with no context lines at all "succeeds" upstream without
   changing anything; `ApplyWith` counts a hunk as applied only when the text
   actually changed.
4. **`normalizeHunk` is not ported.** Nothing in the apply path reaches it, so
   it was never covered by the differential suite, and its output lines carry no
   trailing newline — the result cannot be fed back into `ApplyHunk`. Shipping
   it would have been handing callers a trap.
5. **diff-match-patch timeout.** Both sides use a 5-second timeout, after which
   the library degrades to a coarse diff. Native Go is far faster than the same
   code under a JS engine, so on a large enough input the JavaScript side can
   time out while Go has not. The parity cases stay well away from that size.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE) — the upstream chain
mixes Apache-2.0 (aider) and MIT (the npm package), and NOTICE explains why this
port picked the more restrictive of the two.

[aider]: https://github.com/Aider-AI/aider
[npm]: https://www.npmjs.com/package/llm-diff-patcher
