package hunkpatch

import (
	"errors"
	"strings"
	"testing"
)

// Tests for the public surface: what Apply promises about its return values,
// as opposed to the parity tests, which only ask whether the algorithm matches
// the JS original.

const apiSource = `package main

import "fmt"

func main() {
	name := "world"
	fmt.Println("hello", name)
}
`

func TestApplySimpleReplace(t *testing.T) {
	out, err := Apply(apiSource, "@@\n-\tname := \"world\"\n+\tname := \"gopher\"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `name := "gopher"`) {
		t.Errorf("replacement missing:\n%s", out)
	}
	if strings.Contains(out, `name := "world"`) {
		t.Errorf("old line still present:\n%s", out)
	}
	// Nothing else may move.
	if !strings.HasPrefix(out, "package main\n") || !strings.HasSuffix(out, "}\n") {
		t.Errorf("the rest of the file changed:\n%q", out)
	}
}

func TestApplyIgnoresWrongLineNumbers(t *testing.T) {
	// A header claiming lines that do not exist must not matter: hunks are
	// located by content.
	out, err := Apply(apiSource, "@@ -900,3 +900,3 @@\n-\tname := \"world\"\n+\tname := \"gopher\"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `name := "gopher"`) {
		t.Errorf("replacement missing:\n%s", out)
	}
}

func TestApplyAcceptsEnvelopeAndFence(t *testing.T) {
	body := "@@\n-\tname := \"world\"\n+\tname := \"gopher\"\n"
	for _, diff := range []string{
		body,
		"*** Begin Patch\n*** Update File: main.go\n" + body + "*** End Patch",
		"Here is the change you asked for:\n\n" + body,
		"--- a/main.go\n+++ b/main.go\n" + body,
		"```diff\n--- a/main.go\n+++ b/main.go\n" + body + "```\n",
	} {
		out, err := Apply(apiSource, diff)
		if err != nil {
			t.Errorf("unexpected error for %q: %v", diff, err)
			continue
		}
		if !strings.Contains(out, `name := "gopher"`) {
			t.Errorf("not applied for input %q:\n%s", diff, out)
		}
	}
}

// A diff carrying file headers but no @@ line anywhere must not be read as two
// files (its own header plus the placeholder one), which would report a
// phantom skipped hunk.
func TestApplyFileHeadersWithoutHunkHeader(t *testing.T) {
	diff := "--- a/main.go\n+++ b/main.go\n-\tname := \"world\"\n+\tname := \"gopher\"\n"
	res, err := ApplyWith(apiSource, diff, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 1 || res.Applied != 1 {
		t.Errorf("expected 1 of 1 hunk applied, got %d of %d", res.Applied, res.Total)
	}
	if !strings.Contains(res.Text, `name := "gopher"`) {
		t.Errorf("not applied:\n%s", res.Text)
	}
}

func TestApplyMultipleHunks(t *testing.T) {
	diff := "@@\n-import \"fmt\"\n+import (\n+\t\"fmt\"\n+\t\"os\"\n+)\n" +
		"@@\n-\tfmt.Println(\"hello\", name)\n+\tfmt.Fprintln(os.Stdout, \"hello\", name)\n"
	res, err := ApplyWith(apiSource, diff, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 2 || res.Applied != 2 {
		t.Fatalf("expected 2/2 hunks applied, got %d/%d", res.Applied, res.Total)
	}
	if !strings.Contains(res.Text, `"os"`) || !strings.Contains(res.Text, "fmt.Fprintln") {
		t.Errorf("both hunks should be visible:\n%s", res.Text)
	}
}

func TestApplyNoHunks(t *testing.T) {
	for _, diff := range []string{
		"",
		"I could not find anything to change.",
		"@@\n",
	} {
		out, err := Apply(apiSource, diff)
		if !errors.Is(err, ErrNoHunks) {
			t.Errorf("expected ErrNoHunks for %q, got %v", diff, err)
		}
		if out != apiSource {
			t.Errorf("source must come back unchanged for %q", diff)
		}
	}
}

func TestApplyPartial(t *testing.T) {
	// First hunk matches, second one references a line that does not exist.
	diff := "@@\n-\tname := \"world\"\n+\tname := \"gopher\"\n" +
		"@@\n-\tthis line is not in the file at all\n+\treplacement\n"

	out, err := Apply(apiSource, diff)

	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("expected *PartialError, got %v", err)
	}
	if partial.Total != 2 || partial.Applied != 1 {
		t.Errorf("expected 1 of 2 applied, got %d of %d", partial.Applied, partial.Total)
	}
	if len(partial.Skipped) != 1 {
		t.Fatalf("expected 1 skipped hunk, got %d", len(partial.Skipped))
	}
	if !strings.Contains(strings.Join(partial.Skipped[0], ""), "not in the file") {
		t.Errorf("the skipped hunk should be the failing one, got %q", partial.Skipped[0])
	}
	// The successful hunk is still applied — that is what makes the text usable.
	if !strings.Contains(out, `name := "gopher"`) {
		t.Errorf("the hunk that matched should still be applied:\n%s", out)
	}
}

func TestApplyResultCarriesSameDetailAsError(t *testing.T) {
	diff := "@@\n-\tname := \"world\"\n+\tname := \"gopher\"\n" +
		"@@\n-\tnowhere to be found\n+\tx\n"
	res, err := ApplyWith(apiSource, diff, Options{})
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("expected *PartialError, got %v", err)
	}
	if res.Total != partial.Total || res.Applied != partial.Applied || len(res.Skipped) != len(partial.Skipped) {
		t.Errorf("Result and PartialError disagree: %+v vs %+v", res, partial)
	}
}

func TestApplyIndentTolerantOption(t *testing.T) {
	// The file indents with 6 spaces, the model wrote 2, and the change spans
	// two lines — the shape the base algorithm cannot handle.
	source := "root:\n  block:\n      alpha: 1\n      bravo: 2\n  other:\n"
	hunk := "@@\n-  alpha: 1\n-  bravo: 2\n+  alpha: 10\n+  bravo: 2\n"

	if _, err := Apply(source, hunk); err == nil {
		t.Fatal("the base algorithm was expected to fail here; this case no longer tests anything")
	}

	res, err := ApplyWith(source, hunk, Options{IndentTolerant: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "      alpha: 10\n") {
		t.Errorf("should be rewritten with the file's 6 spaces:\n%q", res.Text)
	}
}

func TestApplyDoesNotMutateSourceOnFailure(t *testing.T) {
	source := apiSource
	if _, err := Apply(source, "@@\n-nothing like this exists\n+x\n"); err == nil {
		t.Fatal("expected an error")
	}
	if source != apiSource {
		t.Error("Apply must not modify its input")
	}
}

func TestFindDiffsSplitsFiles(t *testing.T) {
	// The multi-file route: FindDiffs to split, ApplyHunk per file.
	patch := "```diff\n" +
		"--- a/one.go\n+++ b/one.go\n@@ @@\n-alpha\n+ALPHA\n" +
		"--- a/two.go\n+++ b/two.go\n@@ @@\n-bravo\n+BRAVO\n" +
		"```\n"

	groups := FindDiffs(patch)
	if len(groups) != 2 {
		t.Fatalf("expected 2 files, got %d", len(groups))
	}
	if groups[0].NewFileName != "one.go" || groups[1].NewFileName != "two.go" {
		t.Fatalf("wrong file names: %q, %q", groups[0].NewFileName, groups[1].NewFileName)
	}
	if groups[0].OldFileName != "one.go" {
		t.Errorf("the a/ prefix should be stripped, got %q", groups[0].OldFileName)
	}

	out, ok := ApplyHunk("alpha\n", groups[0].Hunks[0])
	if !ok || out != "ALPHA\n" {
		t.Errorf("applying the first file's hunk gave ok=%v out=%q", ok, out)
	}
}

// A hunk with no context lines cannot fail in ApplyHunk's own terms — see the
// note on ApplyHunk — so ApplyWith has to notice that nothing changed. This
// pins that down, because it is the difference between a caller writing a
// half-patched file and a caller seeing an error.
func TestApplyReportsNoOpHunkAsSkipped(t *testing.T) {
	hunk := []string{"-nowhere to be found\n", "+replacement\n"}
	if out, ok := ApplyHunk(apiSource, hunk); !ok || out != apiSource {
		t.Fatalf("precondition changed: ApplyHunk now reports ok=%v out==source:%v", ok, out == apiSource)
	}

	res, err := ApplyWith(apiSource, "@@\n"+strings.Join(hunk, ""), Options{})
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("a hunk that changes nothing must be reported, got %v", err)
	}
	if res.Applied != 0 || res.Total != 1 {
		t.Errorf("expected 0 of 1 applied, got %d of %d", res.Applied, res.Total)
	}
	if res.Text != apiSource {
		t.Error("the text must come back unchanged")
	}
}
