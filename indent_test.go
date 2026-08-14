package hunkpatch

import (
	"strings"
	"testing"
)

// Tests for the indentation-tolerant strategy. This is the only addition that
// changes what gets WRITTEN, so the guard tests matter more than the happy
// path: guessing the location wrong corrupts the file silently instead of
// reporting a failure.

func TestIndentTolerantAppliesUniformShift(t *testing.T) {
	// The shape this strategy exists for: the file indents with 14 spaces, the
	// model wrote 12, and the change spans two lines. The base strategy is bound to
	// fail (the second line's indentation cannot line up on a line boundary),
	// the indentation strategy should rescue it — and what lands in the file
	// must be 14 spaces, not the model's 12.
	content := "<div>\n" +
		"            <span>outer</span>\n" +
		"  <section>\n" +
		"              <small>今日会员消费</small>\n" +
		"              <strong>¥ 8,420</strong>\n" +
		"  </section>\n" +
		"</div>\n"
	hunk := []string{
		"-            <small>今日会员消费</small>\n",
		"-            <strong>¥ 8,420</strong>\n",
		"+            <small>本日の会員利用額</small>\n",
		"+            <strong>¥ 8,420</strong>\n",
	}

	if _, ok := ApplyHunk(content, hunk); ok {
		if out, _ := ApplyHunk(content, hunk); out != content {
			t.Fatalf("the base strategy was supposed to fail here; the case is pointless otherwise:\n%s", out)
		}
	}

	out, ok := ApplyHunkWith(content, hunk, Options{IndentTolerant: true})
	if !ok || out == content {
		t.Fatalf("the indentation strategy should have applied this, got ok=%v changed=%v", ok, out != content)
	}
	if !strings.Contains(out, "              <small>本日の会員利用額</small>\n") {
		t.Errorf("the written indentation must be the file's 14 spaces, not the model's 12:\n%s", out)
	}
	// Contains cannot prove the indentation is right: 14 spaces contains 12
	// spaces as a substring. Compare whole lines.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "本日の会員利用額") {
			indent, _ := splitIndent(line)
			if indent != "              " {
				t.Errorf("that line should be indented by 14 spaces, got %d: %q", len(indent), line)
			}
		}
	}
	// The edit has to land inside <section>, not somewhere else.
	if strings.Index(out, "本日の会員利用額") < strings.Index(out, "<section>") {
		t.Errorf("wrong location:\n%s", out)
	}
}

func TestIndentTolerantRefusesAmbiguous(t *testing.T) {
	// The same content appears once at each of two nesting levels, so ignoring
	// indentation leaves two candidates. It must refuse rather than pick one.
	content := "<a>\n" +
		"    <small>金额</small>\n" +
		"    <strong>¥ 1</strong>\n" +
		"</a>\n" +
		"<b>\n" +
		"        <small>金额</small>\n" +
		"        <strong>¥ 1</strong>\n" +
		"</b>\n"
	hunk := []string{
		"-      <small>金额</small>\n",
		"-      <strong>¥ 1</strong>\n",
		"+      <small>AMOUNT</small>\n",
		"+      <strong>¥ 1</strong>\n",
	}

	out, ok := ApplyHunkWith(content, hunk, Options{IndentTolerant: true})
	if ok && out != content {
		t.Errorf("two candidates must be refused, but it edited:\n%s", out)
	}
}

func TestIndentTolerantRefusesNonUniformShift(t *testing.T) {
	// The two lines are off by different shifts (+2 and +4), which means the
	// model misunderstood the structure. Applying either shift could break the
	// nesting, so refuse.
	content := "root\n" +
		"    alpha()\n" +
		"          beta()\n" + // model wrote 6 spaces -> 10 here, shift +4; previous line +2
		"done\n"
	hunk := []string{
		"-  alpha()\n",
		"-      beta()\n",
		"+  alpha2()\n",
		"+      beta()\n",
	}
	out, ok := ApplyHunkWith(content, hunk, Options{IndentTolerant: true})
	if ok && out != content {
		t.Errorf("a non-uniform shift must be refused, but it edited:\n%s", out)
	}
}

func TestIndentTolerantRefusesTabSpaceMix(t *testing.T) {
	// File uses tabs, model used spaces: neither adding nor removing a prefix. Refuse.
	content := "func x() {\n\tif a {\n\t\treturn 1\n\t}\n}\n"
	hunk := []string{
		"-    if a {\n",
		"-        return 1\n",
		"+    if b {\n",
		"+        return 2\n",
	}
	out, ok := ApplyHunkWith(content, hunk, Options{IndentTolerant: true})
	if ok && out != content {
		t.Errorf("mixing tabs and spaces must be refused, but it edited:\n%s", out)
	}
}

func TestIndentTolerantHandlesOverIndent(t *testing.T) {
	// The model over-indents (16 spaces against the file's 8). The base
	// strategy cannot even rescue a single line of this.
	content := "wrapper\n        alpha = 1\n        bravo = 2\n end\n"
	hunk := []string{
		"-                alpha = 1\n",
		"-                bravo = 2\n",
		"+                alpha = 10\n",
		"+                bravo = 2\n",
	}
	out, ok := ApplyHunkWith(content, hunk, Options{IndentTolerant: true})
	if !ok || out == content {
		t.Fatalf("over-indentation should be rescued too, got ok=%v", ok)
	}
	if !strings.Contains(out, "        alpha = 10\n") {
		t.Errorf("should be written with the file's 8 spaces:\n%q", out)
	}
}

func TestIndentTolerantOffByDefault(t *testing.T) {
	// Off by default — matching the JS is the baseline, additions are opt-in.
	content := "<div>\n              <small>a</small>\n              <strong>b</strong>\n</div>\n"
	hunk := []string{"-  <small>a</small>\n", "-  <strong>b</strong>\n", "+  <small>A</small>\n", "+  <strong>b</strong>\n"}

	if out, ok := ApplyHunk(content, hunk); ok && out != content {
		t.Errorf("indentation tolerance must not be on by default:\n%s", out)
	}
	if out, ok := ApplyHunkWith(content, hunk, Options{}); ok && out != content {
		t.Errorf("the zero Options value must not enable indentation tolerance:\n%s", out)
	}
}

func TestIndentTolerantNeverBreaksExactMatches(t *testing.T) {
	// With the addition enabled, anything that matched exactly before must come
	// out byte for byte the same.
	content := "alpha\n  bravo\n    charlie\n  delta\n"
	hunks := [][]string{
		{"-  bravo\n", "+  BRAVO\n"},
		{" alpha\n", "-  bravo\n", "+  BRAVO\n"},
		{"-    charlie\n"},
		{" alpha\n", "+  inserted\n"},
		{"-alpha\n", "+ALPHA\n"},
	}
	for i, h := range hunks {
		base, baseOK := ApplyHunk(content, h)
		with, withOK := ApplyHunkWith(content, h, Options{IndentTolerant: true})
		if baseOK != withOK || base != with {
			t.Errorf("hunk %d: the addition changed a result that already succeeded\nbase(ok=%v)=%q\nwith(ok=%v)=%q",
				i, baseOK, base, withOK, with)
		}
	}
}

func TestIndentTolerantRefusesWhitespaceOnlySearch(t *testing.T) {
	// A search block of pure whitespace carries no information, so the addition
	// must stay out of it.
	//
	// Note this cannot assert "the result is unchanged": the base strategy
	// already acts on input like this (stripBlankLines trims " \n" down to
	// "\n", which then matches the first blank line in the file). What is being
	// checked is that the ADDITION changes nothing, so compare with it on and off.
	content := "a\n\n\nb\n"
	for _, h := range [][]string{
		{"- \n", "+X\n"},
		{"-\n", "-\n", "+X\n"},
		{" \n", "+X\n"},
	} {
		base, baseOK := ApplyHunk(content, h)
		with, withOK := ApplyHunkWith(content, h, Options{IndentTolerant: true})
		if baseOK != withOK || base != with {
			t.Errorf("a whitespace-only search must not be touched by the addition\nbase(ok=%v)=%q\nwith(ok=%v)=%q", baseOK, base, withOK, with)
		}
	}
}

// Unit-test indentTolerantSearchAndReplace directly, so the base strategy's
// behaviour cannot muddy the result.
func TestIndentTolerantStrategyDirectly(t *testing.T) {
	cases := []struct {
		name           string
		search, repl   string
		original       string
		wantOK         bool
		wantContainsLn string // this exact line must appear in the output
	}{
		{
			name: "uniform-shift-unique-match", search: "  a = 1\n  b = 2\n", repl: "  a = 9\n  b = 2\n",
			original: "x\n    a = 1\n    b = 2\ny\n", wantOK: true, wantContainsLn: "    a = 9",
		},
		{
			name: "two-candidates-must-refuse", search: "  a = 1\n", repl: "  a = 9\n",
			original: "  a = 1\nmid\n      a = 1\n", wantOK: false,
		},
		{
			name: "identical-indent-defers-to-exact-match", search: "  a = 1\n", repl: "  a = 9\n",
			original: "x\n  a = 1\ny\n", wantOK: false,
		},
		{
			name: "whitespace-only-search", search: " \n", repl: "X\n",
			original: "a\n\nb\n", wantOK: false,
		},
		{
			name: "search-longer-than-original", search: "  a\n  b\n  c\n", repl: "  z\n",
			original: "    a\n", wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, ok := indentTolerantSearchAndReplace([3]string{c.search, c.repl, c.original})
			if ok != c.wantOK {
				t.Fatalf("ok=%v want %v (out=%q)", ok, c.wantOK, out)
			}
			if !ok {
				return
			}
			found := false
			for _, line := range strings.Split(out, "\n") {
				if line == c.wantContainsLn {
					found = true
				}
			}
			if !found {
				t.Errorf("expected line %q missing from output:\n%q", c.wantContainsLn, out)
			}
		})
	}
}
