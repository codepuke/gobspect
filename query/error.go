package query

import "fmt"

// ParseError is returned by [Parse] when the path expression is syntactically
// invalid. It exposes the full expression, the byte offset of the problem, and
// a human-readable description so callers can build targeted diagnostic messages.
type ParseError struct {
	Expr string // the full expression that failed to parse
	Pos  int    // byte offset of the first invalid character; -1 if unknown
	Msg  string // human-readable description (lowercase, no trailing punctuation)
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.Pos >= 0 {
		return fmt.Sprintf("at position %d in %q: %s", e.Pos, e.Expr, e.Msg)
	}
	return fmt.Sprintf("in %q: %s", e.Expr, e.Msg)
}

// parseErr constructs a ParseError with the given position and a static message.
// Expr is intentionally left empty; [Parse] fills it in before returning.
func parseErr(pos int, msg string) *ParseError {
	return &ParseError{Pos: pos, Msg: msg}
}

// parseErrf constructs a ParseError with a formatted message.
// Expr is intentionally left empty; [Parse] fills it in before returning.
func parseErrf(pos int, format string, args ...any) *ParseError {
	return &ParseError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}
