package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seg is a helper to build expected segment values concisely in tests.
func seg(kind segKind, name string, index int, op filterOp, pattern string) segment {
	return segment{kind: kind, name: name, index: index, filterOp: op, filterPattern: pattern}
}

func field(name string) segment             { return seg(segField, name, 0, filterOpExist, "") }
func idx(i int) segment                     { return seg(segIndex, "", i, filterOpExist, "") }
func wildcard() segment                     { return seg(segWildcard, "", 0, filterOpExist, "") }
func existFilter(f string) segment          { return seg(segFilter, f, 0, filterOpExist, "") }
func globFilter(f, p string) segment        { return seg(segFilter, f, 0, filterOpGlob, p) }
func containsFilter(f, p string) segment    { return seg(segFilter, f, 0, filterOpContains, p) }
func notExistFilter(f string) segment       { return seg(segFilter, f, 0, filterOpNotExist, "") }
func negGlobFilter(f, p string) segment     { return seg(segFilter, f, 0, filterOpNegGlob, p) }
func negContainsFilter(f, p string) segment { return seg(segFilter, f, 0, filterOpNegContains, p) }
func descent(name string) segment           { return seg(segDescend, name, 0, filterOpExist, "") }
func projection(fields ...string) segment   { return segment{kind: segProject, projectFields: fields} }

// numericFilter builds a segment for numeric comparison filter ops.
func numericFilter(f, p string, op filterOp, val float64, ok bool) segment {
	return segment{kind: segFilter, name: f, filterOp: op, filterPattern: p, filterNumVal: val, filterNumOK: ok}
}

// boolFilter builds a segment for a bool == comparison filter op.
func boolFilter(f, p string, val bool) segment {
	return segment{kind: segFilter, name: f, filterOp: filterOpNumEq, filterPattern: p, filterBoolOK: true, filterBoolVal: val}
}

// orFilter builds an OR-group segment from 2+ filter segment alternatives.
func orFilter(alts ...segment) segment {
	return segment{kind: segFilter, orAlts: alts}
}

// TestParseSuccess covers valid path expressions and their expected segments.
func TestParseSuccess(t *testing.T) {
	tests := []struct {
		name string
		expr string
		segs []segment
	}{
		{
			name: "empty path is identity",
			expr: "",
			segs: nil, // empty path has no segments
		},
		{
			name: "single field",
			expr: "Name",
			segs: []segment{field("Name")},
		},
		{
			name: "two dot-separated fields",
			expr: "Customer.Name",
			segs: []segment{field("Customer"), field("Name")},
		},
		{
			name: "three dot-separated fields",
			expr: "Orders.0.Customer.Name",
			segs: []segment{field("Orders"), idx(0), field("Customer"), field("Name")},
		},
		{
			name: "positive zero index",
			expr: "Items.0",
			segs: []segment{field("Items"), idx(0)},
		},
		{
			name: "positive non-zero index",
			expr: "Items.42",
			segs: []segment{field("Items"), idx(42)},
		},
		{
			name: "negative index -1 (last element)",
			expr: "Items.-1",
			segs: []segment{field("Items"), idx(-1)},
		},
		{
			name: "negative index -2 (second-to-last)",
			expr: "Items.-2",
			segs: []segment{field("Items"), idx(-2)},
		},
		{
			name: "large negative index",
			expr: "Items.-100",
			segs: []segment{field("Items"), idx(-100)},
		},
		{
			name: "wildcard segment",
			expr: "Orders.*",
			segs: []segment{field("Orders"), wildcard()},
		},
		{
			name: "wildcard between fields",
			expr: "Orders.*.Customer.Name",
			segs: []segment{field("Orders"), wildcard(), field("Customer"), field("Name")},
		},
		{
			name: "wildcard as sole segment",
			expr: "*",
			segs: []segment{wildcard()},
		},
		{
			name: "existence filter inline after field",
			expr: "Orders[Customer!]",
			segs: []segment{field("Orders"), existFilter("Customer")},
		},
		{
			name: "glob filter inline after field",
			expr: "Orders[Status=active]",
			segs: []segment{field("Orders"), globFilter("Status", "active")},
		},
		{
			name: "glob filter with * pattern",
			expr: "Orders[Status=*]",
			segs: []segment{field("Orders"), globFilter("Status", "*")},
		},
		{
			name: "glob filter with prefix pattern",
			expr: "Orders[Status=err*]",
			segs: []segment{field("Orders"), globFilter("Status", "err*")},
		},
		{
			name: "glob filter with suffix pattern",
			expr: "Orders[Status=*_v2]",
			segs: []segment{field("Orders"), globFilter("Status", "*_v2")},
		},
		{
			name: "glob filter with contains pattern",
			expr: "Orders[Status=*foo*]",
			segs: []segment{field("Orders"), globFilter("Status", "*foo*")},
		},
		{
			name: "glob filter with < in pattern",
			expr: "[Name=a<b]",
			segs: []segment{globFilter("Name", "a<b")},
		},
		{
			name: "glob filter with > in pattern",
			expr: "[Name=a>b]",
			segs: []segment{globFilter("Name", "a>b")},
		},
		{
			name: "glob filter with ~ in pattern",
			expr: "[Name=a~b]",
			segs: []segment{globFilter("Name", "a~b")},
		},
		{
			name: "contains filter with > in pattern",
			expr: "[Tags~prod>dev]",
			segs: []segment{containsFilter("Tags", "prod>dev")},
		},
		{
			name: "glob filter with ?* pattern (non-empty)",
			expr: "Orders[Name=?*]",
			segs: []segment{field("Orders"), globFilter("Name", "?*")},
		},
		{
			name: "glob filter with ? pattern",
			expr: "Orders[Code=ERR_?]",
			segs: []segment{field("Orders"), globFilter("Code", "ERR_?")},
		},
		{
			name: "glob filter with empty pattern",
			expr: "Orders[Status=]",
			segs: []segment{field("Orders"), globFilter("Status", "")},
		},
		{
			name: "standalone existence filter as first segment",
			expr: "[Status!]",
			segs: []segment{existFilter("Status")},
		},
		{
			name: "standalone glob filter as first segment",
			expr: "[Status=active]",
			segs: []segment{globFilter("Status", "active")},
		},
		{
			name: "filter followed by more segments",
			expr: "Orders[Status=active].*.Item",
			segs: []segment{field("Orders"), globFilter("Status", "active"), wildcard(), field("Item")},
		},
		{
			name: "existence filter followed by wildcard and field",
			expr: "Orders[Customer!].*.Price",
			segs: []segment{field("Orders"), existFilter("Customer"), wildcard(), field("Price")},
		},
		{
			name: "negative index then field",
			expr: "Orders.-1.Customer.Name",
			segs: []segment{field("Orders"), idx(-1), field("Customer"), field("Name")},
		},
		{
			name: "map key navigation",
			expr: "Meta.env",
			segs: []segment{field("Meta"), field("env")},
		},
		{
			name: "field named with digits mixed (not pure integer)",
			expr: "Field42abc",
			segs: []segment{field("Field42abc")},
		},
		{
			name: "field starting with underscore",
			expr: "_internal.Name",
			segs: []segment{field("_internal"), field("Name")},
		},
		{
			name: "deeply nested path",
			expr: "A.B.C.D.E",
			segs: []segment{field("A"), field("B"), field("C"), field("D"), field("E")},
		},
		{
			name: "index only path",
			expr: "0",
			segs: []segment{idx(0)},
		},
		{
			name: "multiple wildcards",
			expr: "A.*.B.*",
			segs: []segment{field("A"), wildcard(), field("B"), wildcard()},
		},
		{
			name: "filter on standalone wildcard",
			expr: "*[Status=active]",
			segs: []segment{wildcard(), globFilter("Status", "active")},
		},
		{
			name: "glob filter with = in pattern (value after second =)",
			expr: "Orders[Status=a=b]",
			// strings.Cut splits on first '=': field="Status", pattern="a=b"
			segs: []segment{field("Orders"), globFilter("Status", "a=b")},
		},
		{
			name: "descent at start with single name",
			expr: "..Name",
			segs: []segment{descent("Name")},
		},
		{
			name: "descent at start with different name",
			expr: "..Orders",
			segs: []segment{descent("Orders")},
		},
		{
			name: "descent followed by field",
			expr: "..Orders.Status",
			segs: []segment{descent("Orders"), field("Status")},
		},
		{
			name: "field then descent",
			expr: "A..Name",
			segs: []segment{field("A"), descent("Name")},
		},
		{
			name: "descent with integer token (treated as field name)",
			expr: "..0",
			segs: []segment{descent("0")},
		},
		// NOT filter operators
		{
			name: "not-exist filter",
			expr: "[Field!!]",
			segs: []segment{notExistFilter("Field")},
		},
		{
			name: "negated glob filter",
			expr: "[Status!=active]",
			segs: []segment{negGlobFilter("Status", "active")},
		},
		{
			name: "negated contains filter",
			expr: "[Tags!~go]",
			segs: []segment{negContainsFilter("Tags", "go")},
		},
		{
			name: "negated glob filter with wildcard pattern",
			expr: "[Status!=err*]",
			segs: []segment{negGlobFilter("Status", "err*")},
		},
		// Feature 2: quoted patterns in filter expressions
		{
			name: "quoted pattern with space",
			expr: `[Status="active user"]`,
			segs: []segment{globFilter("Status", "active user")},
		},
		{
			name: "quoted pattern with ] bracket",
			expr: `[Name="has]bracket"]`,
			segs: []segment{globFilter("Name", "has]bracket")},
		},
		{
			name: "quoted pattern with \\n escape",
			expr: `[Notes="line1\nline2"]`,
			segs: []segment{globFilter("Notes", "line1\nline2")},
		},
		{
			name: "quoted pattern with escaped quote",
			expr: `[Name="say \"hi\""]`,
			segs: []segment{globFilter("Name", `say "hi"`)},
		},
		{
			name: "quoted pattern with escaped backslash",
			expr: `[Path="C:\\Users"]`,
			segs: []segment{globFilter("Path", `C:\Users`)},
		},
		{
			name: "quoted pattern with \\* pass-through",
			expr: `[Tag="\*"]`,
			segs: []segment{globFilter("Tag", `\*`)},
		},
		{
			name: "negated glob with quoted pattern",
			expr: `[Status!="err*"]`,
			segs: []segment{negGlobFilter("Status", "err*")},
		},
		{
			name: "quoted empty pattern",
			expr: `[Status=""]`,
			segs: []segment{globFilter("Status", "")},
		},
		// Feature 4: wildcard descent (..[Filter])
		{
			name: "wildcard descent with glob filter",
			expr: "..[Status=active]",
			segs: []segment{descent(""), globFilter("Status", "active")},
		},
		{
			name: "wildcard descent with contains filter",
			expr: "..[Tags~devops]",
			segs: []segment{descent(""), containsFilter("Tags", "devops")},
		},
		{
			name: "wildcard descent with exist filter",
			expr: "..[Name!]",
			segs: []segment{descent(""), existFilter("Name")},
		},
		{
			name: "field then wildcard descent with glob filter",
			expr: "A..[Status=active]",
			segs: []segment{field("A"), descent(""), globFilter("Status", "active")},
		},
		{
			name: "wildcard descent with two chained filters",
			expr: "..[Status=active][Name!]",
			segs: []segment{descent(""), globFilter("Status", "active"), existFilter("Name")},
		},
		// Feature 3: numeric comparison operators
		{
			name: "numeric equality filter",
			expr: "[Count==5]",
			segs: []segment{numericFilter("Count", "5", filterOpNumEq, 5.0, true)},
		},
		{
			name: "numeric less-than filter",
			expr: "[Price<100]",
			segs: []segment{numericFilter("Price", "100", filterOpNumLT, 100.0, true)},
		},
		{
			name: "numeric greater-than filter",
			expr: "[Price>0]",
			segs: []segment{numericFilter("Price", "0", filterOpNumGT, 0.0, true)},
		},
		{
			name: "numeric less-than-or-equal filter",
			expr: "[Price<=99.99]",
			segs: []segment{numericFilter("Price", "99.99", filterOpNumLTE, 99.99, true)},
		},
		{
			name: "numeric greater-than-or-equal filter",
			expr: "[Count>=1]",
			segs: []segment{numericFilter("Count", "1", filterOpNumGTE, 1.0, true)},
		},
		{
			name: "bool equality with 'true' literal",
			expr: "[Enabled==true]",
			segs: []segment{boolFilter("Enabled", "true", true)},
		},
		{
			name: "bool equality with 'false' literal",
			expr: "[Enabled==false]",
			segs: []segment{boolFilter("Enabled", "false", false)},
		},
		{
			name: "bool equality with 'TRUE' (case-insensitive)",
			expr: "[Enabled==TRUE]",
			segs: []segment{boolFilter("Enabled", "TRUE", true)},
		},
		// Feature 4 (OR operator): two-way OR
		{
			name: "two-way OR filter",
			expr: "[Status=active]|[Status=pending]",
			segs: []segment{orFilter(globFilter("Status", "active"), globFilter("Status", "pending"))},
		},
		// Three-way OR
		{
			name: "three-way OR filter",
			expr: "[A=x]|[B=y]|[C=z]",
			segs: []segment{orFilter(globFilter("A", "x"), globFilter("B", "y"), globFilter("C", "z"))},
		},
		// OR after AND chain: [A!][B=x]|[C!]
		{
			name: "OR after AND chain",
			expr: "[A!][B=x]|[C!]",
			segs: []segment{existFilter("A"), orFilter(globFilter("B", "x"), existFilter("C"))},
		},
		// OR followed by field: [A=x]|[B!].Name
		{
			name: "OR followed by field",
			expr: "[A=x]|[B!].Name",
			segs: []segment{orFilter(globFilter("A", "x"), existFilter("B")), field("Name")},
		},
		// OR in wildcard descent: ..[A=x]|[B=y]
		{
			name: "OR in wildcard descent",
			expr: "..[A=x]|[B=y]",
			segs: []segment{descent(""), orFilter(globFilter("A", "x"), globFilter("B", "y"))},
		},
		// Feature: field projection (comma-separated)
		{
			name: "two-field projection",
			expr: "SKU,Price",
			segs: []segment{projection("SKU", "Price")},
		},
		{
			name: "three-field projection",
			expr: "A,B,C",
			segs: []segment{projection("A", "B", "C")},
		},
		{
			name: "projection after wildcard",
			expr: "Items.*.SKU,Price",
			segs: []segment{field("Items"), wildcard(), projection("SKU", "Price")},
		},
		{
			name: "projection after filter",
			expr: "Items[Status=active].Name,SKU",
			segs: []segment{field("Items"), globFilter("Status", "active"), projection("Name", "SKU")},
		},
		{
			name: "projection with spaces around commas",
			expr: "SKU, Price",
			segs: []segment{projection("SKU", "Price")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.expr)
			require.NoError(t, err)
			assert.Equal(t, tt.segs, p.segs)
		})
	}
}

// TestParseErrors covers all syntactically invalid path expressions.
func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr string // substring of the expected error message
	}{
		{
			name:    "leading dot",
			expr:    ".Orders",
			wantErr: "may not start with '.'",
		},
		{
			name:    "double dot followed by star is reserved",
			expr:    "..*",
			wantErr: "reserved",
		},
		{
			name:    "double dot mid-path followed by star is reserved",
			expr:    "A..*",
			wantErr: "reserved",
		},
		{
			name:    "trailing dot",
			expr:    "Orders.",
			wantErr: "trailing '.'",
		},
		{
			name:    "unclosed bracket",
			expr:    "Orders[Status",
			wantErr: "unclosed '['",
		},
		{
			name:    "unclosed bracket at start",
			expr:    "[Status",
			wantErr: "unclosed '['",
		},
		{
			name:    "empty filter brackets",
			expr:    "Orders[]",
			wantErr: "empty filter",
		},
		{
			name:    "filter with no ! or = (bare field name only)",
			expr:    "Orders[Status]",
			wantErr: "expected '!' suffix or '=' separator",
		},
		{
			name:    "filter with empty field name before !",
			expr:    "Orders[!]",
			wantErr: "filter field name is empty",
		},
		{
			name:    "filter with empty field name before =",
			expr:    "Orders[=pattern]",
			wantErr: "filter field name is empty",
		},
		{
			name:    "filter field name contains dot",
			expr:    "Orders[Foo.Bar!]",
			wantErr: "invalid filter field name",
		},
		{
			name:    "filter field name contains space",
			expr:    "Orders[Foo Bar!]",
			wantErr: "invalid filter field name",
		},
		{
			name:    "operator interpreted incorrectly fails on value validation",
			expr:    "[Foo<bar>baz]",
			wantErr: "invalid filter value \"bar>baz\": expected a number or bool literal",
		},
		{
			name:    "not-exist filter with empty field name",
			expr:    "[!!]",
			wantErr: "filter field name is empty",
		},
		{
			name:    "not-exist filter with invalid field name",
			expr:    "[.Bad!!]",
			wantErr: "invalid filter field name",
		},
		{
			name:    "double dot standalone (trailing)",
			expr:    "..",
			wantErr: "trailing",
		},
		{
			name:    "trailing double dot after field",
			expr:    "A..",
			wantErr: "trailing",
		},
		{
			name:    "unclosed quoted pattern (missing closing quote)",
			expr:    `[Status="active]`,
			wantErr: "unclosed quoted pattern",
		},
		// Feature 4 (OR operator): error cases
		{
			name:    "trailing pipe after filter",
			expr:    "[A!]|",
			wantErr: "'|' must be followed by",
		},
		{
			name:    "pipe followed by non-bracket",
			expr:    "[A!]|Field",
			wantErr: "'|' must be followed by",
		},
		// Feature: projection error cases
		{
			name:    "projection with empty field (trailing comma)",
			expr:    "SKU,",
			wantErr: "invalid segment",
		},
		{
			name:    "projection with empty field (leading comma)",
			expr:    ",SKU",
			wantErr: "invalid segment",
		},
		{
			name:    "projection with double comma",
			expr:    "SKU,,Price",
			wantErr: "invalid segment",
		},
		// Bool/numeric filter error cases
		{
			name:    "non-numeric non-bool pattern on == operator",
			expr:    "[Count==abc]",
			wantErr: "abc",
		},
		{
			name:    "banana is not a valid bool or number",
			expr:    "[Enabled==banana]",
			wantErr: "banana",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.expr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr,
				"error %q should contain %q", err.Error(), tt.wantErr)
		})
	}
}

// TestParseEmptyPath verifies that the empty string produces a valid zero-segment Path.
func TestParseEmptyPath(t *testing.T) {
	p, err := Parse("")
	require.NoError(t, err)
	assert.Nil(t, p.segs, "empty path should have nil segment slice")
}

// TestParseSegmentKinds verifies each segKind is parsed to the correct kind constant.
func TestParseSegmentKinds(t *testing.T) {
	tests := []struct {
		expr     string
		wantKind segKind
	}{
		{"Name", segField},
		{"0", segIndex},
		{"-1", segIndex},
		{"*", segWildcard},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			p, err := Parse(tt.expr)
			require.NoError(t, err)
			require.Len(t, p.segs, 1)
			assert.Equal(t, tt.wantKind, p.segs[0].kind)
		})
	}
}

// TestParseFilterDetails verifies segment fields for both filter forms.
func TestParseFilterDetails(t *testing.T) {
	t.Run("existence filter sets filterOp=filterOpExist and empty pattern", func(t *testing.T) {
		p, err := Parse("[Field!]")
		require.NoError(t, err)
		require.Len(t, p.segs, 1)
		s := p.segs[0]
		assert.Equal(t, segFilter, s.kind)
		assert.Equal(t, "Field", s.name)
		assert.Equal(t, filterOpExist, s.filterOp)
		assert.Empty(t, s.filterPattern)
	})

	t.Run("glob filter sets filterOp=filterOpGlob and filterPattern", func(t *testing.T) {
		p, err := Parse("[Name=J*]")
		require.NoError(t, err)
		require.Len(t, p.segs, 1)
		s := p.segs[0]
		assert.Equal(t, segFilter, s.kind)
		assert.Equal(t, "Name", s.name)
		assert.Equal(t, filterOpGlob, s.filterOp)
		assert.Equal(t, "J*", s.filterPattern)
	})
}

// TestParseIndexDetails verifies the index field for positive and negative indices.
func TestParseIndexDetails(t *testing.T) {
	tests := []struct {
		expr      string
		wantIndex int
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"-1", -1},
		{"-2", -2},
		{"-100", -100},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			p, err := Parse(tt.expr)
			require.NoError(t, err)
			require.Len(t, p.segs, 1)
			assert.Equal(t, segIndex, p.segs[0].kind)
			assert.Equal(t, tt.wantIndex, p.segs[0].index)
		})
	}
}

// TestParseInlineFilterPosition verifies that an inline filter is placed
// immediately after its preceding segment in the segment slice.
func TestParseInlineFilterPosition(t *testing.T) {
	p, err := Parse("Orders[Status=active].*.Item")
	require.NoError(t, err)
	require.Len(t, p.segs, 4)
	assert.Equal(t, segField, p.segs[0].kind)
	assert.Equal(t, "Orders", p.segs[0].name)
	assert.Equal(t, segFilter, p.segs[1].kind)
	assert.Equal(t, "Status", p.segs[1].name)
	assert.Equal(t, "active", p.segs[1].filterPattern)
	assert.Equal(t, segWildcard, p.segs[2].kind)
	assert.Equal(t, segField, p.segs[3].kind)
	assert.Equal(t, "Item", p.segs[3].name)
}

// TestParseRecursiveDescentSuccess verifies that '..Name' and 'A..Name' now parse
// successfully, and that bare '..' still errors with "trailing".
func TestParseRecursiveDescentSuccess(t *testing.T) {
	successCases := []struct {
		expr     string
		wantSegs []segment
	}{
		{"..Name", []segment{descent("Name")}},
		{"Orders..Name", []segment{field("Orders"), descent("Name")}},
	}
	for _, tc := range successCases {
		t.Run(tc.expr, func(t *testing.T) {
			p, err := Parse(tc.expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantSegs, p.segs)
		})
	}

	t.Run("..", func(t *testing.T) {
		_, err := Parse("..")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "trailing")
	})
}

// TestPathString verifies Path.String() for all documented segment combinations.
func TestPathString(t *testing.T) {
	mustParse := func(expr string) Path {
		p, err := Parse(expr)
		require.NoError(t, err)
		return p
	}

	tests := []struct {
		name string
		path Path
		want string
	}{
		{
			name: "zero path returns empty string",
			path: Path{},
			want: "",
		},
		{
			name: "single field",
			path: mustParse("Name"),
			want: "Name",
		},
		{
			name: "two fields",
			path: mustParse("Customer.Name"),
			want: "Customer.Name",
		},
		{
			name: "field + index",
			path: mustParse("Items.0"),
			want: "Items.0",
		},
		{
			name: "negative index",
			path: mustParse("Items.-1"),
			want: "Items.-1",
		},
		{
			name: "wildcard",
			path: mustParse("Orders.*"),
			want: "Orders.*",
		},
		{
			name: "wildcard as sole segment",
			path: mustParse("*"),
			want: "*",
		},
		{
			name: "existence filter",
			path: mustParse("[Field!]"),
			want: "[Field!]",
		},
		{
			name: "glob filter",
			path: mustParse("[Status=active]"),
			want: "[Status=active]",
		},
		{
			name: "contains filter",
			path: mustParse("[Tags~devops]"),
			want: "[Tags~devops]",
		},
		{
			name: "filter inline after field",
			path: mustParse("Orders[Status=active]"),
			want: "Orders[Status=active]",
		},
		{
			name: "named descent",
			path: mustParse("..Name"),
			want: "..Name",
		},
		{
			name: "named descent followed by field",
			path: mustParse("..Orders.Status"),
			want: "..Orders.Status",
		},
		{
			name: "wildcard descent with filter",
			path: mustParse("..[Status=active]"),
			want: "..[Status=active]",
		},
		{
			name: "complex composed expression",
			path: mustParse("..Resources[Tags~prod][Status=active].Name"),
			want: "..Resources[Tags~prod][Status=active].Name",
		},
		{
			name: "single-segment path has no leading dot",
			path: mustParse("Price"),
			want: "Price",
		},
		{
			name: "filter immediately after field name has no dot before bracket",
			path: mustParse("Orders[Status=active]"),
			want: "Orders[Status=active]",
		},
		{
			name: "round-trip ..Orders.*",
			path: mustParse("..Orders.*"),
			want: "..Orders.*",
		},
		{
			name: "round-trip A..Name",
			path: mustParse("A..Name"),
			want: "A..Name",
		},
		{
			name: "round-trip ..[Tags~devops][Status=active]",
			path: mustParse("..[Tags~devops][Status=active]"),
			want: "..[Tags~devops][Status=active]",
		},
		{
			name: "projection two fields",
			path: mustParse("SKU,Price"),
			want: "SKU,Price",
		},
		{
			name: "projection after dot-separated path",
			path: mustParse("Items.*.SKU,Price"),
			want: "Items.*.SKU,Price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.path.String())
		})
	}
}

// TestPathStringRoundTrip verifies that Parse(expr).String() == expr for all
// canonical path expressions (idempotency).
func TestPathStringRoundTrip(t *testing.T) {
	exprs := []string{
		"Name",
		"Customer.Name",
		"Items.0",
		"Items.-1",
		"Orders.*",
		"*",
		"[Field!]",
		"[Status=active]",
		"[Tags~devops]",
		"Orders[Status=active]",
		"..Name",
		"..Orders.Status",
		"..[Status=active]",
		"..Resources[Tags~prod][Status=active].Name",
		"..Orders.*",
		"A..Name",
		"..[Tags~devops][Status=active]",
		"A.B.C.D.E",
		"Orders[Status=active].*.Item",
		"*[Status=active]",
		"..[Status=active][Name!]",
		"[Field!!]",
		"[Status!=active]",
		"[Tags!~go]",
		// Feature 3: numeric comparison operators
		"[Count==5]",
		"[Price<100]",
		"[Price>0]",
		"[Price<=99.99]",
		"[Count>=1]",
		// Feature 4: OR operator
		"[Status=active]|[Status=pending]",
		"[A=x]|[B=y]|[C=z]",
		"[A!][B=x]|[C!]",
		// Feature: projection
		"SKU,Price",
		"A,B,C",
		"Items.*.SKU,Price",
	}

	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			p, err := Parse(expr)
			require.NoError(t, err)
			assert.Equal(t, expr, p.String())
		})
	}
}

// TestQuotedPatternRoundTrip verifies that patterns requiring quoting round-trip
// correctly through Path.String() → Parse().
func TestQuotedPatternRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string // the decoded pattern value
		op          filterOp
		wantContain string // substring expected in the String() output
		wantQuoted  bool   // whether String() should emit a quoted form
	}{
		{
			name:        "pattern with space (no quoting needed)",
			pattern:     "active user",
			op:          filterOpGlob,
			wantContain: "active user",
			wantQuoted:  false,
		},
		{
			name:        "pattern with ] bracket",
			pattern:     "has]bracket",
			op:          filterOpGlob,
			wantContain: `"has]bracket"`,
			wantQuoted:  true,
		},
		{
			name:        "pattern with backslash",
			pattern:     `C:\Users`,
			op:          filterOpGlob,
			wantContain: `"C:\\Users"`,
			wantQuoted:  true,
		},
		{
			name:        "pattern with double quote",
			pattern:     `say "hi"`,
			op:          filterOpGlob,
			wantContain: `"say \"hi\""`,
			wantQuoted:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Construct a path directly with the pattern.
			p := Path{segs: []segment{
				{kind: segFilter, name: "Field", filterOp: tt.op, filterPattern: tt.pattern},
			}}
			str := p.String()
			assert.Contains(t, str, tt.wantContain, "String() should contain expected pattern text")

			// Parse the string back and check the pattern is preserved.
			p2, err := Parse(str)
			require.NoError(t, err, "Parse(String()) should succeed")
			require.Len(t, p2.segs, 1)
			assert.Equal(t, tt.pattern, p2.segs[0].filterPattern, "pattern should round-trip")
		})
	}
}

// TestParseZeroSegmentFromNonEmptyExpr ensures parseSegments never silently
// succeeds with zero segments for a non-empty expression.
func TestParseZeroSegmentFromNonEmptyExpr(t *testing.T) {
	// The only valid zero-segment path is the empty string.
	_, err := Parse("")
	require.NoError(t, err)

	// A non-empty string that somehow yields no segments should error.
	// (This is a belt-and-suspenders test for the internal guard.)
	// The filter bracket alone with no content is already caught by parseFilter,
	// so we test other odd inputs here.
	_, err = Parse(".")
	require.Error(t, err)
}

// — ParseError type tests ————————————————————————————————————————————————————

// TestParseReturnsParseError verifies that Parse always returns a *ParseError
// for invalid expressions, allowing callers to type-assert and extract fields.
func TestParseReturnsParseError(t *testing.T) {
	_, err := Parse(".bad")
	require.Error(t, err)

	var pe *ParseError
	require.ErrorAs(t, err, &pe, "Parse must return *ParseError for invalid expressions")
	assert.Equal(t, ".bad", pe.Expr, "Expr must be the full expression")
	assert.NotEmpty(t, pe.Msg, "Msg must be non-empty")
}

// TestParseErrorPosPopulated verifies that Pos is set to a non-negative byte
// offset for errors where position is known.
func TestParseErrorPosPopulated(t *testing.T) {
	cases := []struct {
		expr    string
		wantPos int
	}{
		{".Orders", 0}, // leading dot at offset 0
		{"[", 0},       // unclosed '[' at offset 0
		{"Orders[", 6}, // unclosed '[' at offset 6
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := Parse(tc.expr)
			require.Error(t, err)

			var pe *ParseError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, tc.expr, pe.Expr)
			assert.Equal(t, tc.wantPos, pe.Pos)
		})
	}
}

// TestParseErrorErrorMethod verifies that ParseError.Error() includes both the
// expression and the position in its output.
func TestParseErrorErrorMethod(t *testing.T) {
	_, err := Parse("[bad")
	require.Error(t, err)

	msg := err.Error()
	assert.Contains(t, msg, "[bad", "Error() should include the expression")
	assert.Contains(t, msg, "0", "Error() should include the position")
}

// TestParseErrorNegativePos verifies the Pos==-1 branch of ParseError.Error(),
// which omits "at position" from the message.
func TestParseErrorNegativePos(t *testing.T) {
	pe := &ParseError{Expr: "foo", Pos: -1, Msg: "path produced no segments"}
	msg := pe.Error()
	assert.NotContains(t, msg, "at position", "Pos==-1 should omit position from message")
	assert.Contains(t, msg, "foo", "Error() should include the expression")
	assert.Contains(t, msg, "path produced no segments", "Error() should include the message")
}

// TestParseFilterInvalidFieldNameGlobAndContains verifies that [Foo.Bar=active] and
// [Foo.Bar~tag] return "invalid filter field name" errors.
func TestParseFilterInvalidFieldNameGlobAndContains(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"dot in field name before =", "Orders[Foo.Bar=active]"},
		{"dot in field name before ~", "Orders[Foo.Bar~tag]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.expr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid filter field name")
		})
	}
}

// TestParseTokenAtoiOverflow verifies that a numeric token that overflows int64
// is treated as a field name segment rather than an error.
func TestParseTokenAtoiOverflow(t *testing.T) {
	p, err := Parse("99999999999999999999")
	require.NoError(t, err)
	require.Len(t, p.segs, 1)
	assert.Equal(t, segField, p.segs[0].kind)
	assert.Equal(t, "99999999999999999999", p.segs[0].name)
}

// TestParseDescentAtTripleDot verifies that "A...B" (triple dot) is an error.
// The second ".." is parsed as a descent segment, which then finds an empty
// name token "." that begins with a dot, producing a trailing '..' error.
func TestParseDescentAtTripleDot(t *testing.T) {
	_, err := Parse("A...B")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trailing '..'")
}

// TestParseSegmentsExpectedDotBetweenSegments verifies that "*A" (wildcard not
// separated from the next token by a '.') produces an error.
func TestParseSegmentsExpectedDotBetweenSegments(t *testing.T) {
	_, err := Parse("*A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected '.' between segments")
}

// TestIsIntegerTokenBareNegativeSign verifies that "-" alone is not an integer token.
// This exercises the len(s)==1 guard inside isIntegerToken.
func TestIsIntegerTokenBareNegativeSign(t *testing.T) {
	// A bare "-" should parse as a field name, not an index.
	p, err := Parse("-")
	require.NoError(t, err)
	require.Len(t, p.segs, 1)
	assert.Equal(t, segField, p.segs[0].kind)
	assert.Equal(t, "-", p.segs[0].name)
}

// TestUnquotePatternEscapeSequences verifies that \n, \t, \r, and unknown escapes
// are handled correctly inside unquotePattern.
func TestUnquotePatternEscapeSequences(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantPat string
	}{
		{"tab escape", `[Field="\t"]`, "\t"},
		{"cr escape", `[Field="\r"]`, "\r"},
		{"unknown escape passes through", `[Field="\z"]`, `\z`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.expr)
			require.NoError(t, err)
			require.Len(t, p.segs, 1)
			assert.Equal(t, tt.wantPat, p.segs[0].filterPattern)
		})
	}
}

// TestParseSegmentsNoSegmentsFromNonEmpty verifies the internal guard that detects
// a non-empty expression that produced zero segments. This fires for a lone '['
// that parseFilter rejects before appending — tested via the OR-error path where
// '|' appears without a preceding '['. Another internal path: if we somehow reach
// the post-loop len(segs)==0 check, that returns parseErr(-1, ...). The only way to
// trigger it from the public API is an expression that starts with '[' and then has
// the OR alternative fail, but that actually errors earlier. The Pos==-1 guard in
// parseErr is tested via TestParseErrorNegativePos. We verify here that an expression
// that consists solely of OR-pipe content errors correctly.
func TestParseSegmentsOrAltError(t *testing.T) {
	// A '[' that starts an OR chain but the OR alternative itself has an error.
	_, err := Parse("[A!]|[unclosed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed '['")
}

// TestParseFilterAfterQuoteNotClosedByBracket verifies the "unclosed quoted pattern"
// error when the closing '"' is not immediately followed by ']'.
func TestParseFilterAfterQuoteNotClosedByBracket(t *testing.T) {
	// Quoted pattern closes but the next char is not ']'.
	_, err := Parse(`[Status="active"x]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed quoted pattern")
}

// TestParseFilterQuotedPatternNoClosingQuote verifies the "unclosed quoted pattern"
// error when the opening '"' in a quoted pattern has no matching closing '"'.
// This triggers the scanQuotedPattern !ok branch inside parseFilter.
func TestParseFilterQuotedPatternNoClosingQuote(t *testing.T) {
	// The ']' ends basicInner, then scanQuotedPattern scans without finding '"'.
	_, err := Parse(`[Status="active]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed quoted pattern")
}

// TestNeedsQuotingStartsWithDoubleQuote verifies that a pattern beginning with '"'
// requires quoting (the p[0]=='"' branch of needsQuoting).
func TestNeedsQuotingStartsWithDoubleQuote(t *testing.T) {
	// Construct a path directly with a filterPattern that starts with '"'.
	p := Path{segs: []segment{
		{kind: segFilter, name: "Field", filterOp: filterOpGlob, filterPattern: `"hello"`},
	}}
	str := p.String()
	// The emitted form should be quoted because the pattern starts with '"'.
	assert.Contains(t, str, `"\"hello\""`, "pattern starting with double-quote should be quoted")

	// It must also round-trip through Parse.
	p2, err := Parse(str)
	require.NoError(t, err)
	require.Len(t, p2.segs, 1)
	assert.Equal(t, `"hello"`, p2.segs[0].filterPattern)
}
