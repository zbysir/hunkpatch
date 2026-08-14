package hunkpatch

import "testing"

// The JavaScript primitives this package emulates, checked against values
// produced by a real JS engine (testdata/js_semantics.mjs, run under node).
//
// These functions are the foundation the rest of the port stands on. When
// jsTrim's whitespace set is off by one character, or String.replace replaces
// every occurrence instead of the first, the failure does not show up here — it
// shows up as a patch that lands in the wrong place, several layers away. So
// they get pinned directly, with expectations that nobody had to remember from
// the spec.

type jsSemantics struct {
	Trim []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"trim"`
	TrimEnd []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"trimEnd"`
	Length []struct {
		In  string `json:"in"`
		Out int    `json:"out"`
	} `json:"length"`
	FirstUnit []struct {
		In  string `json:"in"`
		Out uint16 `json:"out"`
	} `json:"firstUnit"`
	ReplaceFirst []struct {
		S       string `json:"s"`
		Search  string `json:"search"`
		Replace string `json:"replace"`
		Out     string `json:"out"`
	} `json:"replaceFirst"`
	IndexOf []struct {
		S    string `json:"s"`
		V    string `json:"v"`
		From int    `json:"from"`
		Out  int    `json:"out"`
	} `json:"indexOf"`
	LastIndexOf []struct {
		S    string `json:"s"`
		V    string `json:"v"`
		From int    `json:"from"`
		Out  int    `json:"out"`
	} `json:"lastIndexOf"`
	Substring []struct {
		S     string `json:"s"`
		Start int    `json:"start"`
		End   int    `json:"end"`
		Out   string `json:"out"`
	} `json:"substring"`
	TokenizeLines []struct {
		In  string   `json:"in"`
		Out []string `json:"out"`
	} `json:"tokenizeLines"`
}

func TestJSStringSemantics(t *testing.T) {
	var g jsSemantics
	loadGolden(t, "js_semantics.json", &g)

	t.Run("trim", func(t *testing.T) {
		if len(g.Trim) == 0 {
			t.Fatal("no cases")
		}
		for _, c := range g.Trim {
			if got := jsTrim(c.In); got != c.Out {
				t.Errorf("jsTrim(%q) = %q, JS says %q", c.In, got, c.Out)
			}
		}
	})

	t.Run("trimEnd", func(t *testing.T) {
		for _, c := range g.TrimEnd {
			if got := jsTrimEnd(c.In); got != c.Out {
				t.Errorf("jsTrimEnd(%q) = %q, JS says %q", c.In, got, c.Out)
			}
		}
	})

	t.Run("length", func(t *testing.T) {
		for _, c := range g.Length {
			if got := jsLength(c.In); got != c.Out {
				t.Errorf("jsLength(%q) = %d, JS says %d", c.In, got, c.Out)
			}
		}
	})

	t.Run("firstUnit", func(t *testing.T) {
		for _, c := range g.FirstUnit {
			if got := jsFirstUnit(c.In); got != c.Out {
				t.Errorf("jsFirstUnit(%q) = %d, JS says %d", c.In, got, c.Out)
			}
		}
	})

	t.Run("replaceFirst", func(t *testing.T) {
		for _, c := range g.ReplaceFirst {
			if got := jsReplaceFirst(c.S, c.Search, c.Replace); got != c.Out {
				t.Errorf("jsReplaceFirst(%q, %q, %q) = %q, JS says %q", c.S, c.Search, c.Replace, got, c.Out)
			}
		}
	})

	// The index helpers use byte offsets where JS uses UTF-16 code unit
	// offsets. That is sound in this port because they only ever locate "\n"
	// and consume offsets computed here, and the generator keeps these cases
	// ASCII-only so the two coordinate systems coincide.
	t.Run("indexOf", func(t *testing.T) {
		for _, c := range g.IndexOf {
			if got := jsIndexOf(c.S, c.V, c.From); got != c.Out {
				t.Errorf("jsIndexOf(%q, %q, %d) = %d, JS says %d", c.S, c.V, c.From, got, c.Out)
			}
		}
	})

	t.Run("lastIndexOf", func(t *testing.T) {
		for _, c := range g.LastIndexOf {
			if got := jsLastIndexOf(c.S, c.V, c.From); got != c.Out {
				t.Errorf("jsLastIndexOf(%q, %q, %d) = %d, JS says %d", c.S, c.V, c.From, got, c.Out)
			}
		}
	})

	t.Run("substring", func(t *testing.T) {
		for _, c := range g.Substring {
			if got := jsSubstring(c.S, c.Start, c.End); got != c.Out {
				t.Errorf("jsSubstring(%q, %d, %d) = %q, JS says %q", c.S, c.Start, c.End, got, c.Out)
			}
		}
	})

	// jsTokenizeLines replaces a split on /(\n|\r\n)/ with a scan for '\n'.
	// Those differ in where a lone '\r' lands — as part of the separator in JS,
	// as part of the line in Go — but the tokenizer glues separators back onto
	// their line, so the output is identical. This is where that claim is checked.
	t.Run("tokenizeLines", func(t *testing.T) {
		for _, c := range g.TokenizeLines {
			got := jsTokenizeLines(c.In)
			if ja, jb, ok := jsonEq(t, c.Out, orEmpty(got)); !ok {
				t.Errorf("jsTokenizeLines(%q)\nJS = %s\nGo = %s", c.In, ja, jb)
			}
		}
	})
}
