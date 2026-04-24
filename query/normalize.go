package query

import "strings"

// NormalizeQuery strips a leading "." from a query expression so that callers
// can accept both "Foo.Bar" and ".Foo.Bar" as equivalent inputs.
// Special cases: "." becomes "" (identity path); ".." is preserved (recursive
// descent syntax).
func NormalizeQuery(expr string) string {
	if expr == "." {
		return ""
	}
	if strings.HasPrefix(expr, ".") && !strings.HasPrefix(expr, "..") {
		return expr[1:]
	}
	return expr
}
