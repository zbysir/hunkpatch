package hunkpatch

// Options turns on behaviour that goes beyond the port. The zero value is
// exactly the upstream JavaScript implementation.
//
// They are per-call rather than a package-level switch so that two policies can
// coexist in one process: strict equivalence with the JavaScript implementation
// where that is what you need, and the extra strategies where rescuing a hunk
// is worth more than matching upstream exactly.
type Options struct {
	// IndentTolerant enables the indentation-tolerant strategy: after every
	// exact strategy has failed, retry the match ignoring leading whitespace.
	// The block must match in exactly one place AND every line must be off by
	// the same indentation prefix, otherwise it still fails. See indent.go.
	IndentTolerant bool
}
