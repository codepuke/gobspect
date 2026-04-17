package query

import (
	"reflect"
	"testing"
)

// TestRoundTripCorpus is a property-style test that verifies every path
// produced by Parse round-trips through String() → Parse to an identical Path.
//
// For each input we check:
//  1. Parse(input) succeeds.
//  2. p1.String() → Parse → p2: p1 and p2 are deeply equal (segment-level).
//  3. p2.String() == p1.String() (the canonical form is stable).
func TestRoundTripCorpus(t *testing.T) {
	corpus := []struct {
		name  string
		input string
	}{
		// --- Basic segment kinds ---
		{"single field", "Name"},
		{"dotted fields", "Customer.Name"},
		{"integer index", "Items.0"},
		{"negative index", "Items.-1"},
		{"wildcard", "Orders.*"},
		{"sole wildcard", "*"},

		// --- Filter operators ---
		{"existence filter", "[Field!]"},
		{"not-exist filter", "[Field!!]"},
		{"glob filter", "[Status=active]"},
		{"contains filter", "[Tags~devops]"},
		{"neg glob filter", "[Status!=active]"},
		{"neg contains filter", "[Tags!~go]"},
		{"numeric eq", "[Count==5]"},
		{"numeric lt", "[Price<100]"},
		{"numeric gt", "[Price>0]"},
		{"numeric lte", "[Price<=99.99]"},
		{"numeric gte", "[Count>=1]"},
		{"bool eq true", "[Enabled==true]"},
		{"bool eq false", "[Enabled==false]"},

		// --- Filter after field ---
		{"filter inline", "Orders[Status=active]"},
		{"exist filter inline", "Orders[Customer!]"},
		{"two chained filters", "Orders[Status=active][Name!]"},
		{"filter then wildcard then field", "Orders[Status=active].*.Item"},

		// --- Filter as first segment ---
		{"filter at start", "[Status=active]"},
		{"exist filter at start", "[Name!]"},

		// --- Named descent ---
		{"named descent at start", "..Name"},
		{"named descent after field", "A..Name"},
		{"named descent + field", "..Orders.Status"},
		{"descent + wildcard", "..Orders.*"},

		// --- Wildcard descent + filter ---
		{"wildcard descent glob", "..[Status=active]"},
		{"wildcard descent contains", "..[Tags~devops]"},
		{"wildcard descent exist", "..[Name!]"},
		{"field then wildcard descent", "A..[Status=active]"},
		{"wildcard descent chained", "..[Status=active][Name!]"},

		// --- OR groups ---
		{"two-way OR", "[Status=active]|[Status=pending]"},
		{"three-way OR", "[A=x]|[B=y]|[C=z]"},
		{"OR after AND", "[A!][B=x]|[C!]"},
		{"OR followed by field", "[A=x]|[B!].Name"},
		{"OR in wildcard descent", "..[A=x]|[B=y]"},
		{"OR at start", "[X=1]|[Y=2]"},

		// --- Projection ---
		{"two-field projection", "SKU,Price"},
		{"three-field projection", "A,B,C"},
		{"projection after wildcard", "Items.*.SKU,Price"},
		{"projection after filter", "Items[Status=active].Name,SKU"},

		// --- Quoted patterns: special characters ---
		{"pattern with ] bracket", `[Name="has]bracket"]`},
		{"pattern with backslash", `[Path="C:\\Users"]`},
		{"pattern with double quote", `[Name="say \"hi\""]`},
		{"pattern with newline", `[Notes="line1\nline2"]`},
		{"pattern with tab", `[Notes="col1\tcol2"]`},
		{"pattern with trailing !", `[Tag="alert!"]`},
		{"pattern starting with quote", `[Tag="\"hello\""]`},
		{"neg glob quoted", `[Status!="err*"]`},
		{"neg contains quoted bracket", `[Tags!~"has]it"]`},

		// --- Edge cases: pipe in pattern (requires quoting) ---
		{"pattern with pipe", `[Tag="a|b"]`},

		// --- Edge cases: unicode ---
		{"unicode field name", "Ünïcödé"},
		{"unicode pattern", "[Name=日本語]"},

		// --- Complex composed ---
		{"complex descent", "..Resources[Tags~prod][Status=active].Name"},
		{"wildcard + filter + field", "*[Status=active].Name"},
		{"deep path", "A.B.C.D.E"},
		{"index + filter", "Items.0[Name!]"},
	}

	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			// Step 1: Parse the input.
			p1, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", tc.input, err)
			}

			// Step 2: String() → re-parse.
			s1 := p1.String()
			p2, err := Parse(s1)
			if err != nil {
				t.Fatalf("Parse(String(%q)) = Parse(%q) failed: %v", tc.input, s1, err)
			}

			// Step 3: Deep equality of the two parsed paths.
			if !reflect.DeepEqual(p1.segs, p2.segs) {
				t.Errorf("segments not equal after round-trip of %q\n  String()=%q\n  p1.segs=%+v\n  p2.segs=%+v",
					tc.input, s1, p1.segs, p2.segs)
			}

			// Step 4: String() stability — second String() must equal first.
			s2 := p2.String()
			if s1 != s2 {
				t.Errorf("String() not stable for %q\n  first =%q\n  second=%q",
					tc.input, s1, s2)
			}
		})
	}
}

// TestRoundTripDirectConstruction verifies that Path values built by direct
// segment construction (not via Parse) also round-trip through String() → Parse.
// This catches issues where String() emits forms the parser can't handle.
func TestRoundTripDirectConstruction(t *testing.T) {
	tests := []struct {
		name string
		segs []segment
	}{
		{
			name: "pattern with literal newline",
			segs: []segment{
				{kind: segFilter, name: "Notes", filterOp: filterOpGlob, filterPattern: "line1\nline2"},
			},
		},
		{
			name: "pattern with literal tab",
			segs: []segment{
				{kind: segFilter, name: "Notes", filterOp: filterOpGlob, filterPattern: "col1\tcol2"},
			},
		},
		{
			name: "pattern with literal carriage return",
			segs: []segment{
				{kind: segFilter, name: "Notes", filterOp: filterOpGlob, filterPattern: "a\rb"},
			},
		},
		{
			name: "pattern with pipe char",
			segs: []segment{
				{kind: segFilter, name: "Tag", filterOp: filterOpGlob, filterPattern: "a|b"},
			},
		},
		{
			name: "OR group with bracket in pattern",
			segs: []segment{
				{kind: segFilter, orAlts: []segment{
					{kind: segFilter, name: "A", filterOp: filterOpGlob, filterPattern: "x]y"},
					{kind: segFilter, name: "B", filterOp: filterOpExist},
				}},
			},
		},
		{
			name: "OR group with pipe in pattern",
			segs: []segment{
				{kind: segFilter, orAlts: []segment{
					{kind: segFilter, name: "A", filterOp: filterOpGlob, filterPattern: "x|y"},
					{kind: segFilter, name: "B", filterOp: filterOpExist},
				}},
			},
		},
		{
			name: "pattern with only backslash",
			segs: []segment{
				{kind: segFilter, name: "F", filterOp: filterOpGlob, filterPattern: `\`},
			},
		},
		{
			name: "pattern with only double quote",
			segs: []segment{
				{kind: segFilter, name: "F", filterOp: filterOpGlob, filterPattern: `"`},
			},
		},
		{
			name: "pattern with only bracket",
			segs: []segment{
				{kind: segFilter, name: "F", filterOp: filterOpGlob, filterPattern: "]"},
			},
		},
		{
			name: "empty pattern glob",
			segs: []segment{
				{kind: segFilter, name: "F", filterOp: filterOpGlob, filterPattern: ""},
			},
		},
		{
			name: "pattern with all special chars",
			segs: []segment{
				{kind: segFilter, name: "F", filterOp: filterOpGlob, filterPattern: "a\"b\\c]d\ne|f"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p1 := Path{segs: tc.segs}
			s1 := p1.String()

			p2, err := Parse(s1)
			if err != nil {
				t.Fatalf("Parse(String()) = Parse(%q) failed: %v", s1, err)
			}

			// Compare only the structurally significant fields (ignore numeric
			// caching fields which are populated lazily by Parse).
			if !segmentsEqualStructural(p1.segs, p2.segs) {
				t.Errorf("segments not equal after round-trip\n  String()=%q\n  p1.segs=%+v\n  p2.segs=%+v",
					s1, p1.segs, p2.segs)
			}

			s2 := p2.String()
			if s1 != s2 {
				t.Errorf("String() not stable\n  first =%q\n  second=%q",
					s1, s2)
			}
		})
	}
}

// segmentsEqualStructural compares segments by their structural fields,
// ignoring computed/cached numeric fields (filterNumVal, filterIntVal, etc.)
// that are populated by Parse but not by direct construction.
func segmentsEqualStructural(a, b []segment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !segEqualStructural(a[i], b[i]) {
			return false
		}
	}
	return true
}

func segEqualStructural(a, b segment) bool {
	if a.kind != b.kind || a.name != b.name || a.index != b.index ||
		a.filterOp != b.filterOp || a.filterPattern != b.filterPattern {
		return false
	}
	if !reflect.DeepEqual(a.projectFields, b.projectFields) {
		return false
	}
	if len(a.orAlts) != len(b.orAlts) {
		return false
	}
	for i := range a.orAlts {
		if !segEqualStructural(a.orAlts[i], b.orAlts[i]) {
			return false
		}
	}
	return true
}
