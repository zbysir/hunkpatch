package hunkpatch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Differential tests against the JavaScript original.
//
// Reading the two implementations side by side is not enough. The things that
// actually break a port of JS — String.replace replacing only the first match,
// .length counting UTF-16 code units, the empty string being falsy so an empty
// result counts as "no match" and the search continues — are invisible in the
// structure and only show up when you run it.
//
// So the port was developed against the real llm-diff-patcher bundle running in
// a JS engine, function by function, and the JS side's answers for every input
// below are recorded in testdata/. Regenerating them requires the JS bundle and
// a JS runtime — an engine embedded in the test binary works well for this; see
// README.md ("How this was verified").
//
// Comparing only the final output of Apply is also not enough. ApplyHunk's
// fallback chain (makeNewLinesExplicit, then applyPartialHunk shrinking the
// context step by step) is so forgiving that an internal function can be wrong
// and the end result still comes out right. Measured: with 15 deliberate
// mutations planted in the port, end-to-end comparison caught 8. Miscounted
// UTF-16 lengths, treating the empty string as success, trying fewer preproc
// combinations, swapping Myers' add/remove branches and one missing pop() in
// the tokenizer were all masked. Hence the per-function golden files below.

func loadGolden(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse golden %s: %v", name, err)
	}
}

// orEmpty turns a nil slice into an empty one, so that "no lines" compares
// equal to the JS side's [].
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// jsonEq compares by JSON, after callers have normalised nil to empty.
func jsonEq(t *testing.T, a, b any) (string, string, bool) {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(ja), string(jb), string(ja) == string(jb)
}

func firstDiffAt(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

type goldenApplyCase struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Diff   string `json:"diff"`
	OK     bool   `json:"ok"`
	Out    string `json:"out"`
}

// TestParityApply covers the whole pipeline. The cases are grouped by what they
// are trying to reach:
//
//	shape/         one of each diff shape a model produces
//	generated/     360 combinations of odd sources and malformed hunk bodies
//	fallback/      context that is partly wrong, to force makeNewLinesExplicit
//	unidiff-layer/ shapes verified to reach jsdiff's Myers and unidiff's hunk
//	               formatter, the riskiest ~500 lines of the port
//	semantics/     one case per JS semantic that a mutation test proved the
//	               other groups could not distinguish
func TestParityApply(t *testing.T) {
	var cases []goldenApplyCase
	loadGolden(t, "parity_apply.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			out, err := Apply(c.Source, c.Diff)
			ok := !errors.Is(err, ErrNoHunks)
			if ok != c.OK {
				t.Fatalf("outcome differs: js ok=%v, go ok=%v (err=%v)\ngo out=%q", c.OK, ok, err, out)
			}
			if !c.OK {
				return
			}
			if out != c.Out {
				t.Errorf("output differs at byte %d:\n--- js ---\n%s\n--- go ---\n%s",
					firstDiffAt(c.Out, out), c.Out, out)
			}
		})
	}
	t.Logf("%d end-to-end cases match the JS implementation", len(cases))
}

type goldenInternalCase struct {
	Name        string   `json:"name"`
	Content     string   `json:"content"`
	Hunk        []string `json:"hunk"`
	Before      string   `json:"before"`
	After       string   `json:"after"`
	BeforeLines []string `json:"before_lines"`
	AfterLines  []string `json:"after_lines"`

	DirectlyApplyOK bool   `json:"directly_apply_ok"`
	DirectlyApply   string `json:"directly_apply"`

	FlexiOK bool   `json:"flexi_ok"`
	Flexi   string `json:"flexi"`

	DmpDiffLines []string `json:"dmp_diff_lines"`
	MadeExplicit []string `json:"made_explicit"`

	ApplyHunkOK bool   `json:"apply_hunks_ok"`
	ApplyHunk   string `json:"apply_hunks"`
}

// TestParityInternals compares each internal function against the JS module of
// the same name, so a bug cannot hide behind the fallback chain.
func TestParityInternals(t *testing.T) {
	var cases []goldenInternalCase
	loadGolden(t, "parity_internal.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			// aider_udiff.hunkToBeforeAfter
			before, after := hunkToBeforeAfter(c.Hunk)
			if before != c.Before || after != c.After {
				t.Errorf("hunkToBeforeAfter differs\njs=(%q, %q)\ngo=(%q, %q)", c.Before, c.After, before, after)
			}

			// aider_udiff.hunkToBeforeAfter(hunk, true)
			beforeLines, afterLines := hunkToBeforeAfterLines(c.Hunk)
			if ja, jb, ok := jsonEq(t,
				[][]string{orEmpty(c.BeforeLines), orEmpty(c.AfterLines)},
				[][]string{orEmpty(beforeLines), orEmpty(afterLines)}); !ok {
				t.Errorf("hunkToBeforeAfter(lines) differs\njs=%s\ngo=%s", ja, jb)
			}

			// aider_udiff.directlyApplyHunk
			got, gotOK := directlyApplyHunk(c.Content, c.Hunk, Options{})
			if gotOK != c.DirectlyApplyOK || (gotOK && got != c.DirectlyApply) {
				t.Errorf("directlyApplyHunk differs\njs=(%v, %q)\ngo=(%v, %q)", c.DirectlyApplyOK, c.DirectlyApply, gotOK, got)
			}

			// aider_udiff.flexiJustSearchAndReplace
			got, gotOK = flexiJustSearchAndReplace([3]string{c.Before, c.After, c.Content}, Options{})
			if gotOK != c.FlexiOK || (gotOK && got != c.Flexi) {
				t.Errorf("flexiJustSearchAndReplace differs\njs=(%v, %q)\ngo=(%v, %q)", c.FlexiOK, c.Flexi, gotOK, got)
			}

			// diff_lines.diffLines — this one also pins sergi/go-diff against
			// the JS diff-match-patch it replaces.
			if ja, jb, ok := jsonEq(t, c.DmpDiffLines, dmpDiffLines(c.Before, c.Content)); !ok {
				t.Errorf("diff_lines.diffLines differs\njs=%s\ngo=%s", ja, jb)
			}

			// make_new_lines_explicit.makeNewLinesExplicit
			if ja, jb, ok := jsonEq(t, c.MadeExplicit, makeNewLinesExplicit(c.Content, c.Hunk)); !ok {
				t.Errorf("makeNewLinesExplicit differs\njs=%s\ngo=%s", ja, jb)
			}

			// apply_hunk.applyHunks
			got, gotOK = ApplyHunk(c.Content, c.Hunk)
			if gotOK != c.ApplyHunkOK || (gotOK && got != c.ApplyHunk) {
				t.Errorf("applyHunks differs\njs=(%v, %q)\ngo=(%v, %q)", c.ApplyHunkOK, c.ApplyHunk, gotOK, got)
			}
		})
	}
}

type goldenUnidiffCase struct {
	Name    string   `json:"name"`
	A       []string `json:"a"`
	B       []string `json:"b"`
	Context int      `json:"context"`
	Err     bool     `json:"err"`
	Out     string   `json:"out"`
}

// TestParityUnidiff pins the whole unidiff + jsdiff chain in one go: the Myers
// implementation, makeHunks, lineChanges and unifiedHeader. Which of several
// equally short edit scripts Myers returns is an implementation detail that
// leaks straight into the hunk text, so this has to match exactly and cannot be
// delegated to another Go diff library.
func TestParityUnidiff(t *testing.T) {
	var cases []goldenUnidiffCase
	loadGolden(t, "parity_unidiff.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}

	for _, c := range cases {
		t.Run(c.Name+"/ctx"+itoa(c.Context), func(t *testing.T) {
			out, err := unidiffFormatLines(unidiffDiffLinesFromArrays(c.A, c.B), c.Context)
			if (err != nil) != c.Err {
				t.Fatalf("error state differs: js err=%v, go err=%v", c.Err, err)
			}
			if c.Err {
				return
			}
			if out != c.Out {
				t.Errorf("formatLines differs\njs=%q\ngo=%q", c.Out, out)
			}
		})
	}
}

type goldenFileDiff struct {
	Old   string     `json:"old"`
	New   string     `json:"new"`
	Hunks [][]string `json:"hunks"`
}

type goldenFindDiffsCase struct {
	Name    string           `json:"name"`
	Content string           `json:"content"`
	Result  []goldenFileDiff `json:"result"`
}

// TestParityFindDiffs compares the full return value, file names included.
//
// Apply only reads Hunks, so a bug in OldFileName/NewFileName (forgetting to
// strip the a//b/ prefix, say) is invisible end to end — that is exactly the
// case the mutation testing kept missing.
func TestParityFindDiffs(t *testing.T) {
	var cases []goldenFindDiffsCase
	loadGolden(t, "parity_finddiffs.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no cases")
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var got []goldenFileDiff
			for _, g := range FindDiffs(c.Content) {
				h := g.Hunks
				if h == nil {
					h = [][]string{}
				}
				got = append(got, goldenFileDiff{Old: g.OldFileName, New: g.NewFileName, Hunks: h})
			}
			if got == nil {
				got = []goldenFileDiff{}
			}
			if ja, jb, ok := jsonEq(t, c.Result, got); !ok {
				t.Errorf("findDiffs differs\njs=%s\ngo=%s", ja, jb)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
