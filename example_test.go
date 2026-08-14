package hunkpatch_test

import (
	"errors"
	"fmt"

	"github.com/zbysir/hunkpatch"
)

func ExampleApply() {
	source := `func greet() string {
	return "hello"
}
`
	// What a model typically emits: no file header, and a line range that does
	// not correspond to anything.
	diff := `@@ -1,3 +1,3 @@
 func greet() string {
-	return "hello"
+	return "hello, world"
 }
`

	out, err := hunkpatch.Apply(source, diff)
	if err != nil {
		panic(err)
	}
	fmt.Print(out)
	// Output:
	// func greet() string {
	// 	return "hello, world"
	// }
}

// Apply accepts the `*** Begin Patch` envelope, prose around the diff, and
// ```diff fences without any pre-processing by the caller.
func ExampleApply_envelope() {
	source := "const port = 8080\n"
	diff := `Sure — here's the change:

*** Begin Patch
*** Update File: config.ts
@@
-const port = 8080
+const port = 3000
*** End Patch`

	out, err := hunkpatch.Apply(source, diff)
	if err != nil {
		panic(err)
	}
	fmt.Print(out)
	// Output: const port = 3000
}

// A hunk that cannot be located does not sink the whole patch: the hunks that
// matched are applied and the failure is reported.
func ExampleApply_partial() {
	source := "alpha\nbravo\ncharlie\n"
	diff := "@@\n alpha\n-bravo\n+BRAVO\n@@\n-delta\n+DELTA\n"

	out, err := hunkpatch.Apply(source, diff)

	var partial *hunkpatch.PartialError
	if errors.As(err, &partial) {
		fmt.Printf("applied %d of %d hunks\n", partial.Applied, partial.Total)
	}
	fmt.Print(out)
	// Output:
	// applied 1 of 2 hunks
	// alpha
	// BRAVO
	// charlie
}

// Options.IndentTolerant rescues the common case of a model that copied the
// code but not its indentation. The replacement is written with the file's
// indentation, not the model's.
func ExampleApplyWith_indentTolerant() {
	source := "if ok {\n        alpha := 1\n        bravo := 2\n}\n"
	// The model used 4 spaces where the file uses 8.
	diff := "@@\n-    alpha := 1\n-    bravo := 2\n+    alpha := 10\n+    bravo := 2\n"

	res, err := hunkpatch.ApplyWith(source, diff, hunkpatch.Options{IndentTolerant: true})
	if err != nil {
		panic(err)
	}
	fmt.Print(res.Text)
	// Output:
	// if ok {
	//         alpha := 10
	//         bravo := 2
	// }
}

// For a patch touching several files, split it with FindDiffs and apply each
// file's hunks yourself.
func ExampleFindDiffs() {
	patch := "```diff\n" +
		"--- a/main.go\n+++ b/main.go\n@@ @@\n-var x = 1\n+var x = 2\n" +
		"--- a/util.go\n+++ b/util.go\n@@ @@\n-var y = 1\n+var y = 2\n" +
		"```\n"

	files := map[string]string{
		"main.go": "var x = 1\n",
		"util.go": "var y = 1\n",
	}

	for _, fd := range hunkpatch.FindDiffs(patch) {
		content := files[fd.NewFileName]
		for _, hunk := range fd.Hunks {
			if next, ok := hunkpatch.ApplyHunk(content, hunk); ok {
				content = next
			}
		}
		fmt.Printf("%s: %s", fd.NewFileName, content)
	}
	// Output:
	// main.go: var x = 2
	// util.go: var y = 2
}
