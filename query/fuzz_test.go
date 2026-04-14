package query

import (
	"testing"
)

// FuzzParse verifies three invariants for all inputs:
// 1. Parse never panics.
// 2. On error, the result is a *ParseError with Expr equal to the input string.
// 3. On success, p.String() round-trips: Parse(p.String()) succeeds and its String() is stable.
func FuzzParse(f *testing.F) {
	// Seed corpus: one representative per documented path feature.
	seeds := []string{
		"Name",
		"Customer.Name",
		"Items.0",
		"Items.-1",
		"Orders.*",
		"[Field!]",
		"[Status=active]",
		"[Tags~devops]",
		"..Name",
		"..[Status=active]",
		"[Field!!]",
		"[Status!=active]",
		"[Tags!~go]",
		`[Status="active user"]`,
		"[Count==5]",
		"[Price<100]",
		"[Price>0]",
		"[Price<=99.99]",
		"[Count>=1]",
		"[Status=active]|[Status=pending]",
		"[A=x]|[B=y]|[C=z]",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		p, err := Parse(input)
		if err != nil {
			// Invariant 2: error must be *ParseError with correct Expr.
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("Parse(%q) returned non-*ParseError: %T", input, err)
			}
			if pe.Expr != input {
				t.Fatalf("ParseError.Expr = %q, want %q", pe.Expr, input)
			}
			return
		}
		// Invariant 3: round-trip.
		s := p.String()
		p2, err2 := Parse(s)
		if err2 != nil {
			t.Fatalf("Parse(p.String()) failed for input %q (String()=%q): %v", input, s, err2)
		}
		if p2.String() != s {
			t.Fatalf("p.String() not stable for input %q: first=%q second=%q", input, s, p2.String())
		}
	})
}
