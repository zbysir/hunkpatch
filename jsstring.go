package hunkpatch

import (
	"strings"
	"unicode/utf16"
)

// JavaScript strings are sequences of UTF-16 code units: .length, s[i] and
// substring all count code units. Every place in this port that compares a
// length against a threshold, or indexes into a string, has to follow that and
// must not use Go's byte length. Concretely: "今日会员消费" has length 6 in JS
// and 18 in bytes, and directlyApplyHunk tests `beforeLinesJoined.length < 10`
// — the two ways of counting take different branches.

// jsUnits converts a string to its UTF-16 code units.
func jsUnits(s string) []uint16 {
	return utf16.Encode([]rune(s))
}

// jsString converts UTF-16 code units back to a string.
func jsString(u []uint16) string {
	return string(utf16.Decode(u))
}

// jsLength returns s.length as JavaScript computes it.
func jsLength(s string) int {
	return len(jsUnits(s))
}

// jsTrim implements JavaScript's String.prototype.trim.
//
// strings.TrimSpace is not a substitute: the two whitespace sets disagree. JS
// treats U+FEFF (BOM) as whitespace and Go does not; Go treats U+0085 (NEL) as
// whitespace and JS does not. This follows ECMAScript's WhiteSpace ∪
// LineTerminator.
func jsTrim(s string) string {
	return strings.TrimFunc(s, isJSWhitespace)
}

// jsTrimEnd implements JavaScript's String.prototype.trimEnd.
func jsTrimEnd(s string) string {
	return strings.TrimRightFunc(s, isJSWhitespace)
}

// isJSWhitespace reports whether r is in ECMAScript's WhiteSpace ∪ LineTerminator.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\v', '\f', ' ', 0x00A0, 0xFEFF, // WhiteSpace
		'\n', '\r', 0x2028, 0x2029: // LineTerminator
		return true
	}
	// The rest of Unicode category Zs.
	switch r {
	case 0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
		0x2007, 0x2008, 0x2009, 0x200A, 0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

// jsReplaceFirst implements `s.replace(search, () => replace)`.
//
// Passing a plain string (not a regexp) to String.prototype.replace replaces
// only the FIRST occurrence, not every one; the callback wrapper exists so that
// replacement patterns such as `$&` and `$1` are not interpreted. So this maps
// to Replace(..., 1), never ReplaceAll.
func jsReplaceFirst(s, search, replace string) string {
	return strings.Replace(s, search, replace, 1)
}

// jsFirstUnit returns the first UTF-16 code unit of s, or 0 when s is empty.
// Corresponds to `s[0]`, which is undefined for the empty string.
func jsFirstUnit(s string) uint16 {
	u := jsUnits(s)
	if len(u) == 0 {
		return 0
	}
	return u[0]
}
