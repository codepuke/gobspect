package query

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — [Field!] existence filter ————————————————————————————————————————————————

// TestFilterExistStructField verifies [Field!] keeps StructValue elements where
// the named field is present in Fields.
func TestFilterExistStructField(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Total", makeInt(10))),                                            // no Status
		makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(20))), // has Status
		makeStruct("Order", makeField("Status", makeString("done")), makeField("Total", makeInt(30))),   // has Status
	)

	// All: keeps elements where Status is present.
	got := All(orders, "[Status!]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(20), MustGet(got[0], "Total"))
	assert.Equal(t, makeInt(30), MustGet(got[1], "Total"))

	// Get: returns first surviving element.
	v, ok := Get(orders, "[Status!].Total")
	require.True(t, ok)
	assert.Equal(t, makeInt(20), v)
}

// TestFilterExistOnArray verifies [Field!] works on ArrayValue containers.
func TestFilterExistOnArray(t *testing.T) {
	items := makeArray(
		makeStruct("Item", makeField("Price", makeInt(5))),
		makeStruct("Item", makeField("Name", makeString("widget")), makeField("Price", makeInt(10))),
	)

	got := All(items, "[Name!]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(10), MustGet(got[0], "Price"))
}

// TestFilterExistOnMapValues verifies [Field!] on a MapValue: keeps entries whose
// value-struct has the named field.
func TestFilterExistOnMapValues(t *testing.T) {
	m := makeMap(
		entry(makeString("a"), makeStruct("V", makeField("X", makeInt(1)))),
		entry(makeString("b"), makeStruct("V", makeField("Y", makeInt(2)))), // no X
		entry(makeString("c"), makeStruct("V", makeField("X", makeInt(3)), makeField("Y", makeInt(4)))),
	)

	got := All(m, "[X!]")
	require.Len(t, got, 2)
}

// TestFilterExistMapElementIsMap verifies [Field!] on an element that is itself a
// MapValue: a key equal to the filter field name must be present.
func TestFilterExistMapElementIsMap(t *testing.T) {
	items := makeSlice(
		makeMap(entry(makeString("foo"), makeInt(1)), entry(makeString("bar"), makeInt(2))),
		makeMap(entry(makeString("bar"), makeInt(3))), // no "foo" key
		makeMap(entry(makeString("foo"), makeInt(4))),
	)

	got := All(items, "[foo!]")
	require.Len(t, got, 2)
}

// TestFilterExistInterfaceValueUnwrap verifies [Field!] unwraps InterfaceValue
// before checking field presence.
func TestFilterExistInterfaceValueUnwrap(t *testing.T) {
	items := makeSlice(
		wrapped("Item", makeStruct("Item", makeField("Name", makeString("A")))),
		wrapped("Item", makeStruct("Item", makeField("Price", makeInt(5)))), // no Name
		wrapped("Item", makeStruct("Item", makeField("Name", makeString("B")))),
	)

	got := All(items, "[Name!]")
	require.Len(t, got, 2)
	// Result values are unwrapped from their InterfaceValue wrappers.
	assert.Equal(t, makeString("A"), MustGet(got[0], "Name"))
	assert.Equal(t, makeString("B"), MustGet(got[1], "Name"))
}

// TestFilterExistNonStructExcluded verifies that scalar and other non-struct,
// non-map elements never match [Field!] and are excluded.
func TestFilterExistNonStructExcluded(t *testing.T) {
	items := makeSlice(
		makeInt(1),
		makeString("hello"),
		makeBool(true),
		gobspect.NilValue{},
		gobspect.OpaqueValue{TypeName: "T"},
	)

	got := All(items, "[Field!]")
	assert.Nil(t, got)
}

// TestFilterExistNoMatchReturnsNilAndFalse verifies that [Field!] returns nil / false
// when no element has the field.
func TestFilterExistNoMatchReturnsNilAndFalse(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Total", makeInt(10))),
		makeStruct("Order", makeField("Amount", makeInt(20))),
	)

	got := All(orders, "[Status!]")
	assert.Nil(t, got)

	_, ok := Get(orders, "[Status!].Total")
	assert.False(t, ok)
}

// — [Field=pattern] glob filter ——————————————————————————————————————————————

// TestFilterGlobExactMatch verifies [Field=literal] performs an exact string match.
func TestFilterGlobExactMatch(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(10))),
		makeStruct("Order", makeField("Status", makeString("inactive")), makeField("Total", makeInt(20))),
		makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(30))),
	)

	got := All(orders, "[Status=active]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(10), MustGet(got[0], "Total"))
	assert.Equal(t, makeInt(30), MustGet(got[1], "Total"))
}

// TestFilterGlobPrefixMatch verifies [Field=prefix*] prefix-matching.
func TestFilterGlobPrefixMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Code", makeString("ERR_404"))),
		makeStruct("Item", makeField("Code", makeString("OK_200"))),
		makeStruct("Item", makeField("Code", makeString("ERR_500"))),
	)

	got := All(items, "[Code=ERR_*]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("ERR_404"), MustGet(got[0], "Code"))
	assert.Equal(t, makeString("ERR_500"), MustGet(got[1], "Code"))
}

// TestFilterGlobSuffixMatch verifies [Field=*suffix] suffix-matching.
func TestFilterGlobSuffixMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString("jackson"))), // ends with "son" — matches
		makeStruct("Item", makeField("Name", makeString("alice"))),   // does not end with "son" — excluded
		makeStruct("Item", makeField("Name", makeString("johnson"))), // ends with "son" — matches
	)

	got := All(items, "[Name=*son]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("jackson"), MustGet(got[0], "Name"))
	assert.Equal(t, makeString("johnson"), MustGet(got[1], "Name"))
}

// TestFilterGlobContainsMatch verifies [Field=*sub*] substring-matching.
func TestFilterGlobContainsMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tag", makeString("foobar"))),
		makeStruct("Item", makeField("Tag", makeString("notbar"))),
		makeStruct("Item", makeField("Tag", makeString("foobaz"))),
	)

	got := All(items, "[Tag=*foo*]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("foobar"), MustGet(got[0], "Tag"))
	assert.Equal(t, makeString("foobaz"), MustGet(got[1], "Tag"))
}

// TestFilterGlobStarMatchesAnyStringIncludingEmpty verifies [Field=*] matches any
// string, including the empty string.
func TestFilterGlobStarMatchesAnyStringIncludingEmpty(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString(""))),      // empty string — matches
		makeStruct("Item", makeField("Name", makeString("hello"))), // non-empty — matches
		makeStruct("Item", makeField("Price", makeInt(5))),         // no Name field — excluded
	)

	got := All(items, "[Name=*]")
	require.Len(t, got, 2, "[Name=*] should match empty and non-empty strings but not missing fields")
}

// TestFilterGlobQuestionStarMatchesNonEmpty verifies [Field=?*] matches only
// non-empty strings (? requires exactly one char; * matches the rest).
func TestFilterGlobQuestionStarMatchesNonEmpty(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString(""))),      // empty — excluded
		makeStruct("Item", makeField("Name", makeString("x"))),     // single char — included
		makeStruct("Item", makeField("Name", makeString("hello"))), // multi-char — included
	)

	got := All(items, "[Name=?*]")
	require.Len(t, got, 2, "[Name=?*] should match only non-empty strings")
	assert.Equal(t, makeString("x"), MustGet(got[0], "Name"))
	assert.Equal(t, makeString("hello"), MustGet(got[1], "Name"))
}

// TestFilterGlobSingleTrailingQuestionMark verifies [Field=prefix?] exact-one-char suffix.
func TestFilterGlobSingleTrailingQuestionMark(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Code", makeString("ERR_1"))),  // matches
		makeStruct("Item", makeField("Code", makeString("ERR_12"))), // does not match (two extra chars)
		makeStruct("Item", makeField("Code", makeString("ERR_X"))),  // matches
		makeStruct("Item", makeField("Code", makeString("ERR_"))),   // does not match (zero extra chars)
	)

	got := All(items, "[Code=ERR_?]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("ERR_1"), MustGet(got[0], "Code"))
	assert.Equal(t, makeString("ERR_X"), MustGet(got[1], "Code"))
}

// TestFilterGlobNonStringFieldExcluded verifies that non-StringValue fields never
// match a glob filter; all other types are excluded.
func TestFilterGlobNonStringFieldExcluded(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeInt(1))),       // int — excluded
		makeStruct("Item", makeField("Status", makeBool(true))),   // bool — excluded
		makeStruct("Item", makeField("Status", makeString("ok"))), // string — matches
	)

	got := All(items, "[Status=ok]")
	require.Len(t, got, 1)
	assert.Equal(t, makeString("ok"), MustGet(got[0], "Status"))

	// Wildcard pattern also only matches the string element.
	got = All(items, "[Status=*]")
	require.Len(t, got, 1)
}

// TestFilterGlobInterfaceValueUnwrap verifies [Field=pattern] unwraps InterfaceValue
// before evaluating the glob match.
func TestFilterGlobInterfaceValueUnwrap(t *testing.T) {
	items := makeSlice(
		wrapped("Item", makeStruct("Item", makeField("Status", makeString("active")))),
		wrapped("Item", makeStruct("Item", makeField("Status", makeString("inactive")))),
		wrapped("Item", makeStruct("Item", makeField("Status", makeString("active")))),
	)

	got := All(items, "[Status=active]")
	require.Len(t, got, 2)
}

// TestFilterGlobMissingFieldExcluded verifies that elements without the filter field
// are excluded even when the pattern is a catch-all.
func TestFilterGlobMissingFieldExcluded(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Other", makeString("anything"))), // no Status
		makeStruct("Item", makeField("Status", makeString("active"))),  // has Status
	)

	got := All(items, "[Status=*]")
	require.Len(t, got, 1)
	assert.Equal(t, makeString("active"), MustGet(got[0], "Status"))
}

// TestFilterGlobNoMatchReturnsNilAndFalse verifies nil / false when no element matches.
func TestFilterGlobNoMatchReturnsNilAndFalse(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Status", makeString("inactive"))),
		makeStruct("Order", makeField("Status", makeString("pending"))),
	)

	got := All(orders, "[Status=active]")
	assert.Nil(t, got)

	_, ok := Get(orders, "[Status=active].Total")
	assert.False(t, ok)
}

// — Filter integration with GetPath ——————————————————————————————————————————

// TestFilterInGetPathFirstMatch verifies GetPath returns only the first surviving
// element when multiple elements match the filter.
func TestFilterInGetPathFirstMatch(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Total", makeInt(10))), // no Status
		makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(20))),
		makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(30))),
	)

	v, ok := Get(orders, "[Status=active].Total")
	require.True(t, ok)
	assert.Equal(t, makeInt(20), v, "Get should return the Total from the first matching element")
}

// TestFilterInGetPathNoMatchReturnsFalse verifies GetPath returns (nil, false) when
// the filter has no matches.
func TestFilterInGetPathNoMatchReturnsFalse(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Status", makeString("inactive"))),
	)

	_, ok := Get(orders, "[Status=active]")
	assert.False(t, ok)
}

// — Filter composed with other segments ——————————————————————————————————————

// TestFilterThenFieldNavigation verifies a filter followed by field navigation
// in AllPath: each surviving element's sub-field is returned.
func TestFilterThenFieldNavigation(t *testing.T) {
	root := makeStruct("Root",
		makeField("Orders", makeSlice(
			makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(100))),
			makeStruct("Order", makeField("Status", makeString("inactive")), makeField("Total", makeInt(200))),
			makeStruct("Order", makeField("Status", makeString("active")), makeField("Total", makeInt(300))),
		)),
	)

	got := All(root, "Orders[Status=active].Total")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(100), got[0])
	assert.Equal(t, makeInt(300), got[1])
}

// TestFilterThenSliceNavigation verifies a filter followed by slice/field navigation.
// After filtering to active orders, we navigate into each order's nested Items slice.
func TestFilterThenSliceNavigation(t *testing.T) {
	activeOrder := makeStruct("Order",
		makeField("Status", makeString("active")),
		makeField("Items", makeSlice(makeString("apple"), makeString("banana"))),
	)
	inactiveOrder := makeStruct("Order",
		makeField("Status", makeString("inactive")),
		makeField("Items", makeSlice(makeString("cherry"))),
	)
	root := makeStruct("Root", makeField("Orders", makeSlice(activeOrder, inactiveOrder)))

	// Navigate into Items of each active order, then expand Items elements.
	got := All(root, "Orders[Status=active].Items.*")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("apple"), got[0])
	assert.Equal(t, makeString("banana"), got[1])
}

// TestFilterAfterWildcard verifies a filter applied after wildcard expansion.
// "Orders.*.Tags[Name=foo]" expands all orders, then filters each order's Tags.
func TestFilterAfterWildcard(t *testing.T) {
	order0 := makeStruct("Order", makeField("Tags", makeSlice(
		makeStruct("Tag", makeField("Name", makeString("foo"))),
		makeStruct("Tag", makeField("Name", makeString("bar"))),
	)))
	order1 := makeStruct("Order", makeField("Tags", makeSlice(
		makeStruct("Tag", makeField("Name", makeString("baz"))),
	)))
	root := makeStruct("Root", makeField("Orders", makeSlice(order0, order1)))

	got := All(root, "Orders.*.Tags[Name=foo]")
	require.Len(t, got, 1)
	assert.Equal(t, makeString("foo"), MustGet(got[0], "Name"))
}

// TestFilterExistAfterWildcard verifies [Field!] applied after wildcard expansion.
func TestFilterExistAfterWildcard(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(10))),                                          // no Name
		makeStruct("Item", makeField("Name", makeString("widget")), makeField("Price", makeInt(20))), // has Name
		makeStruct("Item", makeField("Name", makeString("gadget")), makeField("Price", makeInt(30))), // has Name
	)
	root := makeStruct("Root", makeField("Items", items))

	got := All(root, "Items[Name!].Price")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(20), got[0])
	assert.Equal(t, makeInt(30), got[1])
}

// TestFilterExistFieldThenGlob covers a field segment into a filter expression
// matching a PRD-style path: "Orders[Customer!].*.Price".
// Orders is a slice; [Customer!] keeps orders with a Customer field;
// the remaining *.Price navigates the Orders field (a slice) inside each matching order.
// Here we test the actual working composition: [Customer!].Price.
func TestFilterExistFieldThenGlob(t *testing.T) {
	root := makeStruct("Root",
		makeField("Orders", makeSlice(
			makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Alice")))), makeField("Price", makeInt(50))),
			makeStruct("Order", makeField("Price", makeInt(75))), // no Customer
			makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Bob")))), makeField("Price", makeInt(100))),
		)),
	)

	got := All(root, "Orders[Customer!].Price")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(50), got[0])
	assert.Equal(t, makeInt(100), got[1])
}

// TestFilterErrPrefixGlob covers the PRD example "[Status=err*]" — orders whose
// status starts with "err".
func TestFilterErrPrefixGlob(t *testing.T) {
	root := makeStruct("Root",
		makeField("Orders", makeSlice(
			makeStruct("Order", makeField("Status", makeString("err_timeout")), makeField("ID", makeInt(1))),
			makeStruct("Order", makeField("Status", makeString("ok")), makeField("ID", makeInt(2))),
			makeStruct("Order", makeField("Status", makeString("err_cancelled")), makeField("ID", makeInt(3))),
		)),
	)

	got := All(root, "Orders[Status=err*].ID")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(3), got[1])
}

// — [Field~pattern] contains filter ——————————————————————————————————————————

// TestFilterContainsExactMatchOnSlice verifies [Field~exact] keeps structs whose
// named field is a slice containing at least one element that exactly matches.
func TestFilterContainsExactMatchOnSlice(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go"), makeString("cloud")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("rust")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go"), makeString("devops")))),
	)

	got := All(items, "[Tags~go]")
	require.Len(t, got, 2)
	assert.Equal(t, makeSlice(makeString("go"), makeString("cloud")), MustGet(got[0], "Tags"))
	assert.Equal(t, makeSlice(makeString("go"), makeString("devops")), MustGet(got[1], "Tags"))
}

// TestFilterContainsPrefixGlobOnSlice verifies [Field~pre*] prefix-glob on slice elements.
func TestFilterContainsPrefixGlobOnSlice(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("devops"), makeString("cloud")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("golang")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("dev-tools"), makeString("ci")))),
	)

	got := All(items, "[Tags~dev*]")
	require.Len(t, got, 2)
}

// TestFilterContainsSuffixGlobOnSlice verifies [Field~*suf] suffix-glob on slice elements.
func TestFilterContainsSuffixGlobOnSlice(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("devops")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("secops"), makeString("cloud")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("golang")))),
	)

	got := All(items, "[Tags~*ops]")
	require.Len(t, got, 2)
}

// TestFilterContainsSubstringGlobOnSlice verifies [Field~*mid*] substring-glob on slice elements.
func TestFilterContainsSubstringGlobOnSlice(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go-testing")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("rust-testing")))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("devops")))),
	)

	got := All(items, "[Tags~*test*]")
	require.Len(t, got, 2)
}

// TestFilterContainsStarMatchesAnyElemIncludingEmpty verifies [Field~*] matches a
// slice containing an empty string element.
func TestFilterContainsStarMatchesAnyElemIncludingEmpty(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("")))),          // empty string element — matches
		makeStruct("Item", makeField("Tags", makeSlice(makeString("something")))), // non-empty — matches
		makeStruct("Item", makeField("Price", makeInt(5))),                        // no Tags field — excluded
	)

	got := All(items, "[Tags~*]")
	require.Len(t, got, 2, "[Tags~*] should match any slice with at least one string element")
}

// TestFilterContainsQuestionStarMatchesOnlyNonEmptyElems verifies [Field~?*] keeps
// only structs whose slice contains at least one non-empty string element.
func TestFilterContainsQuestionStarMatchesOnlyNonEmptyElems(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("")))),                       // only empty — excluded
		makeStruct("Item", makeField("Tags", makeSlice(makeString(""), makeString("devops")))), // has non-empty — included
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go")))),                     // non-empty — included
	)

	got := All(items, "[Tags~?*]")
	require.Len(t, got, 2, "[Tags~?*] should match only structs with a non-empty element in the slice")
}

// TestFilterContainsSingleTrailingCharWildcard verifies [Field~ERR_?] matches slice
// elements with exactly one trailing character after "ERR_".
func TestFilterContainsSingleTrailingCharWildcard(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Codes", makeSlice(makeString("ERR_1"), makeString("OK")))),   // ERR_1 matches
		makeStruct("Item", makeField("Codes", makeSlice(makeString("ERR_12")))),                    // two trailing chars — no match
		makeStruct("Item", makeField("Codes", makeSlice(makeString("ERR_X"), makeString("ERR_")))), // ERR_X matches
	)

	got := All(items, "[Codes~ERR_?]")
	require.Len(t, got, 2)
}

// TestFilterContainsExactMatchOnArray verifies [Field~exact] works on ArrayValue.
func TestFilterContainsExactMatchOnArray(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeArray(makeString("go"), makeString("cloud")))),
		makeStruct("Item", makeField("Tags", makeArray(makeString("rust")))),
	)

	got := All(items, "[Tags~go]")
	require.Len(t, got, 1)
}

// TestFilterContainsExactMatchOnMapKeys verifies [Field~exact] on a MapValue field:
// matches if any StringValue key in the map equals the pattern.
func TestFilterContainsExactMatchOnMapKeys(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Meta", makeMap(entry(makeString("devops"), makeInt(1)), entry(makeString("cloud"), makeInt(2))))),
		makeStruct("Item", makeField("Meta", makeMap(entry(makeString("security"), makeInt(3))))),
		makeStruct("Item", makeField("Meta", makeMap(entry(makeString("devops"), makeInt(4))))),
	)

	got := All(items, "[Meta~devops]")
	require.Len(t, got, 2)
}

// TestFilterContainsMapNonStringKeysNoMatch verifies [Field~exact] on a MapValue
// with non-string keys produces no matches.
func TestFilterContainsMapNonStringKeysNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Meta", makeMap(entry(makeInt(1), makeString("one"))))),
		makeStruct("Item", makeField("Meta", makeMap(entry(makeInt(2), makeString("two"))))),
	)

	got := All(items, "[Meta~1]")
	assert.Nil(t, got)
}

// TestFilterContainsScalarFieldNoMatch verifies [Field~exact] on a scalar StringValue
// field returns no match — use [Field=pattern] for scalars.
func TestFilterContainsScalarFieldNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString("exact"))),
	)

	got := All(items, "[Name~exact]")
	assert.Nil(t, got)
}

// TestFilterContainsAbsentFieldNoMatch verifies [Field~exact] when the field is
// absent from the struct returns no match.
func TestFilterContainsAbsentFieldNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Other", makeString("exact"))),
	)

	got := All(items, "[Tags~exact]")
	assert.Nil(t, got)
}

// TestFilterContainsNonStringSliceNoMatch verifies [Field~exact] on a slice of
// non-string values (e.g., ints) returns no match.
func TestFilterContainsNonStringSliceNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Nums", makeSlice(makeInt(1), makeInt(2), makeInt(3)))),
	)

	got := All(items, "[Nums~1]")
	assert.Nil(t, got)
}

// TestFilterContainsEmptySliceNoMatch verifies [Field~exact] on an empty slice
// produces no match.
func TestFilterContainsEmptySliceNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice())),
	)

	got := All(items, "[Tags~anything]")
	assert.Nil(t, got)
}

// TestFilterContainsInterfaceWrappedSliceElems verifies [Field~exact] unwraps
// InterfaceValue-wrapped StringValues inside the slice.
func TestFilterContainsInterfaceWrappedSliceElems(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(
			wrapped("string", makeString("devops")),
			wrapped("string", makeString("cloud")),
		))),
		makeStruct("Item", makeField("Tags", makeSlice(
			wrapped("string", makeString("security")),
		))),
	)

	got := All(items, "[Tags~devops]")
	require.Len(t, got, 1)
}

// TestFilterContainsMultipleElemsPartialMatch verifies that when a slice has multiple
// elements and only some match the pattern, only the owning structs with at least one
// match are kept, and the correct count is returned.
func TestFilterContainsMultipleElemsPartialMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go"), makeString("devops"), makeString("cloud")))), // "go" matches
		makeStruct("Item", makeField("Tags", makeSlice(makeString("rust"), makeString("wasm")))),                      // no "go" — excluded
		makeStruct("Item", makeField("Tags", makeSlice(makeString("python"), makeString("go"), makeString("ml")))),    // "go" matches
		makeStruct("Item", makeField("Tags", makeSlice(makeString("java")))),                                          // no "go" — excluded
	)

	got := All(items, "[Tags~go]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("go"), MustGet(got[0], "Tags.0"))
	assert.Equal(t, makeString("go"), MustGet(got[1], "Tags.1"))
}

// TestFilterContainsChainedWithExist verifies [Field!] and [Field~pattern] chained
// on the same collection: first keep structs where Tags is present, then keep those
// whose Tags slice contains "devops".
func TestFilterContainsChainedWithExist(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("devops"), makeString("cloud"))), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("ID", makeInt(2))),                                                       // no Tags — excluded by [Tags!]
		makeStruct("Item", makeField("Tags", makeSlice(makeString("security"))), makeField("ID", makeInt(3))), // Tags present, no "devops" — excluded by [Tags~devops]
		makeStruct("Item", makeField("Tags", makeSlice(makeString("devops"))), makeField("ID", makeInt(4))),
	)

	// Filter: Tags present, then within those, Tags contains "devops".
	existMatches := All(items, "[Tags!]")
	require.Len(t, existMatches, 3, "three items have Tags")

	containsMatches := All(items, "[Tags~devops]")
	require.Len(t, containsMatches, 2)
	assert.Equal(t, makeInt(1), MustGet(containsMatches[0], "ID"))
	assert.Equal(t, makeInt(4), MustGet(containsMatches[1], "ID"))
}

// — Filter on MapValue elements (fieldValue and fieldAsString map cases) ———————

// TestFilterGlobOnMapElements verifies [Field=pattern] works when the elements
// being filtered are MapValues (e.g. a slice of maps). fieldAsString looks up
// the value for the named key in each map.
func TestFilterGlobOnMapElements(t *testing.T) {
	items := makeSlice(
		makeMap(entry(makeString("Status"), makeString("active")), entry(makeString("ID"), makeString("1"))),
		makeMap(entry(makeString("Status"), makeString("inactive")), entry(makeString("ID"), makeString("2"))),
		makeMap(entry(makeString("Status"), makeString("active")), entry(makeString("ID"), makeString("3"))),
	)

	// Each element is a MapValue; [Status=active] should keep maps with Status key = "active".
	got := All(items, "[Status=active]")
	require.Len(t, got, 2)
	v, ok := Get(got[0], "ID")
	require.True(t, ok)
	assert.Equal(t, makeString("1"), v)
}

// TestFilterGlobOnMapElements_NonStringValue verifies [Field=pattern] on a
// MapValue element where the matching key's value is not a StringValue:
// the filter must exclude those elements.
func TestFilterGlobOnMapElements_NonStringValue(t *testing.T) {
	items := makeSlice(
		makeMap(entry(makeString("Count"), makeInt(42))),      // Count is int, not string
		makeMap(entry(makeString("Count"), makeString("42"))), // Count is string — matches
	)

	got := All(items, "[Count=42]")
	require.Len(t, got, 1)
	assert.Equal(t, makeString("42"), MustGet(got[0], "Count"))
}

// TestFilterContainsOnMapElements verifies [Field~pattern] works when elements
// are MapValues. fieldValue looks up the collection-typed value by key.
func TestFilterContainsOnMapElements(t *testing.T) {
	items := makeSlice(
		makeMap(entry(makeString("Tags"), makeSlice(makeString("go"), makeString("cloud")))),
		makeMap(entry(makeString("Tags"), makeSlice(makeString("rust")))),
		makeMap(entry(makeString("Tags"), makeSlice(makeString("go"), makeString("devops")))),
	)

	// Each element is a MapValue; [Tags~go] should keep maps whose "Tags" slice contains "go".
	got := All(items, "[Tags~go]")
	require.Len(t, got, 2)
}

// TestFilterContainsOnMapElements_AbsentKey verifies [Field~pattern] on a
// MapValue element where the key is absent returns no match.
func TestFilterContainsOnMapElements_AbsentKey(t *testing.T) {
	items := makeSlice(
		makeMap(entry(makeString("Other"), makeSlice(makeString("go")))),
	)

	got := All(items, "[Tags~go]")
	assert.Nil(t, got)
}

// — [Field!!] not-exist filter ————————————————————————————————————————————————

// TestFilterNotExistAbsentField verifies [Field!!] keeps elements where the
// named field is absent.
func TestFilterNotExistAbsentField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(10))),                                            // no Status — kept
		makeStruct("Item", makeField("Status", makeString("active")), makeField("Price", makeInt(20))), // has Status — excluded
		makeStruct("Item", makeField("Price", makeInt(30))),                                            // no Status — kept
	)

	got := All(items, "[Status!!]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(10), MustGet(got[0], "Price"))
	assert.Equal(t, makeInt(30), MustGet(got[1], "Price"))
}

// TestFilterNotExistPresentField verifies [Field!!] excludes elements where the
// named field is present.
func TestFilterNotExistPresentField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("active"))),
		makeStruct("Item", makeField("Status", makeString("inactive"))),
	)

	got := All(items, "[Status!!]")
	assert.Nil(t, got)
}

// TestFilterNotExistNonStructPassThrough verifies [Field!!] returns true for
// scalar and other non-struct, non-map elements because fieldPresent returns
// false for them (the field is indeed not present), so the negation is true.
func TestFilterNotExistNonStructPassThrough(t *testing.T) {
	items := makeSlice(
		makeInt(1),
		makeString("hello"),
		makeBool(true),
	)

	got := All(items, "[Field!!]")
	require.Len(t, got, 3)
}

// — [Field!=pattern] negated glob filter ——————————————————————————————————————

// TestFilterNegGlobAbsentField verifies [Field!=pattern] returns false for
// elements where the field is absent.
func TestFilterNegGlobAbsentField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Other", makeString("active"))), // no Status
	)

	got := All(items, "[Status!=active]")
	assert.Nil(t, got)
}

// TestFilterNegGlobNonStringField verifies [Field!=pattern] returns false for
// elements where the field is not a string.
func TestFilterNegGlobNonStringField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Count", makeInt(42))),
	)

	got := All(items, "[Count!=42]")
	assert.Nil(t, got)
}

// TestFilterNegGlobMatchingExcluded verifies [Field!=pattern] excludes elements
// whose field value matches the pattern.
func TestFilterNegGlobMatchingExcluded(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Status", makeString("inactive")), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(3))),
	)

	got := All(items, "[Status!=active]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
}

// TestFilterNegGlobNonMatchingKept verifies [Field!=pattern] keeps elements
// whose field value does NOT match the pattern.
func TestFilterNegGlobNonMatchingKept(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Code", makeString("ERR_404")), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Code", makeString("OK_200")), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Code", makeString("ERR_500")), makeField("ID", makeInt(3))),
	)

	// [Code!=ERR_*] keeps only those where Code does NOT start with ERR_
	got := All(items, "[Code!=ERR_*]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
}

// — [Field!~pattern] negated contains filter ——————————————————————————————————

// TestFilterNegContainsAbsentField verifies [Field!~pattern] returns false for
// elements where the field is absent.
func TestFilterNegContainsAbsentField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Other", makeSlice(makeString("go")))), // no Tags
	)

	got := All(items, "[Tags!~go]")
	assert.Nil(t, got)
}

// TestFilterNegContainsNonCollection verifies [Field!~pattern] returns false for
// elements where the field is not a collection.
func TestFilterNegContainsNonCollection(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString("go"))),
	)

	got := All(items, "[Name!~go]")
	assert.Nil(t, got)
}

// TestFilterNegContainsCollectionWithMatch verifies [Field!~pattern] excludes
// elements whose collection field contains an element matching the pattern.
func TestFilterNegContainsCollectionWithMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go"), makeString("cloud"))), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("rust"))), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("go"), makeString("devops"))), makeField("ID", makeInt(3))),
	)

	// [Tags!~go] keeps only those that do NOT contain "go"
	got := All(items, "[Tags!~go]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
}

// TestFilterNegContainsCollectionWithNoMatch verifies [Field!~pattern] keeps
// elements whose collection field contains no element matching the pattern.
func TestFilterNegContainsCollectionWithNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tags", makeSlice(makeString("rust"), makeString("wasm"))), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Tags", makeSlice(makeString("python"))), makeField("ID", makeInt(2))),
	)

	got := All(items, "[Tags!~go]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(2), MustGet(got[1], "ID"))
}

// TestFilterNegContainsMapWithNoMatch verifies [Field!~pattern] keeps elements
// whose map field has no key matching the pattern.
func TestFilterNegContainsMapWithNoMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Meta", makeMap(entry(makeString("devops"), makeInt(1)), entry(makeString("cloud"), makeInt(2)))), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Meta", makeMap(entry(makeString("security"), makeInt(3)))), makeField("ID", makeInt(2))),
	)

	// [Meta!~devops] keeps those whose map does NOT contain a "devops" key
	got := All(items, "[Meta!~devops]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
}

// — Feature 2: quoted pattern filter behavior ————————————————————————————————

// TestFilterGlobQuotedPatternWithSpace verifies that a quoted pattern containing
// a space matches a string field value that contains that space.
func TestFilterGlobQuotedPatternWithSpace(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("active user")), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Status", makeString("inactive user")), makeField("ID", makeInt(3))),
	)

	got := All(items, `[Status="active user"]`)
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestFilterGlobQuotedPatternLiteralStar verifies that \* in a quoted pattern is
// passed through to path.Match as a literal backslash-star, matching a literal '*'
// in a value rather than acting as a glob wildcard.
func TestFilterGlobQuotedPatternLiteralStar(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Tag", makeString("*")), makeField("ID", makeInt(1))),        // literal *
		makeStruct("Item", makeField("Tag", makeString("anything")), makeField("ID", makeInt(2))), // not *
	)

	// \* in quoted pattern should match a literal '*' string.
	got := All(items, `[Tag="\*"]`)
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// — Feature 3: numeric and bool comparison operators ————————————————————————

// makeFloat returns a FloatValue for use in tests.
func makeFloat(f float64) gobspect.Value { return gobspect.FloatValue{V: f} }

// makeUint returns a UintValue for use in tests.
func makeUint(u uint64) gobspect.Value { return gobspect.UintValue{V: u} }

// TestFilterNumEqInt verifies [Field==N] equality on IntValue.
func TestFilterNumEqInt(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Count", makeInt(5)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Count", makeInt(3)), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Count", makeInt(5)), makeField("ID", makeInt(3))),
	)

	got := All(items, "[Count==5]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestFilterNumEqUint verifies [Field==N] equality on UintValue.
func TestFilterNumEqUint(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Count", makeUint(5)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Count", makeUint(10)), makeField("ID", makeInt(2))),
	)

	got := All(items, "[Count==5]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestFilterNumLTInt verifies [Field<N] on IntValue.
func TestFilterNumLTInt(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(50)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Price", makeInt(100)), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Price", makeInt(99)), makeField("ID", makeInt(3))),
	)

	got := All(items, "[Price<100]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestFilterNumGTInt verifies [Field>N] on IntValue.
func TestFilterNumGTInt(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(0)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Price", makeInt(1)), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Price", makeInt(-1)), makeField("ID", makeInt(3))),
	)

	got := All(items, "[Price>0]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
}

// TestFilterNumLTEFloat verifies [Field<=N] on FloatValue.
func TestFilterNumLTEFloat(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeFloat(99.99)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Price", makeFloat(100.00)), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Price", makeFloat(50.00)), makeField("ID", makeInt(3))),
	)

	got := All(items, "[Price<=99.99]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestFilterNumGTEInt verifies [Field>=N] on IntValue.
func TestFilterNumGTEInt(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Count", makeInt(0)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Count", makeInt(1)), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Count", makeInt(5)), makeField("ID", makeInt(3))),
	)

	got := All(items, "[Count>=1]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestFilterNumBoolEqTrue verifies [Field==true] matches BoolValue true.
func TestFilterNumBoolEqTrue(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Enabled", makeBool(true)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Enabled", makeBool(false)), makeField("ID", makeInt(2))),
	)

	got := All(items, "[Enabled==true]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestFilterNumBoolEqFalse verifies [Field==false] matches BoolValue false.
func TestFilterNumBoolEqFalse(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Enabled", makeBool(true)), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Enabled", makeBool(false)), makeField("ID", makeInt(2))),
	)

	got := All(items, "[Enabled==false]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(2), MustGet(got[0], "ID"))
}

// TestFilterNumBoolLTRejected verifies [Field<N] on BoolValue returns false.
func TestFilterNumBoolLTRejected(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Enabled", makeBool(true)), makeField("ID", makeInt(1))),
	)

	got := All(items, "[Enabled<1]")
	assert.Nil(t, got)
}

// TestFilterNumAbsentField verifies that a missing field returns false.
func TestFilterNumAbsentField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Other", makeInt(5)), makeField("ID", makeInt(1))),
	)

	got := All(items, "[Count==5]")
	assert.Nil(t, got)
}

// TestFilterNumBadPattern verifies that a non-numeric, non-bool pattern is a parse-time error.
func TestFilterNumBadPattern(t *testing.T) {
	_, err := Parse("[Count==abc]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "abc")
}

// TestFilterNumStringFieldRejected verifies that StringValue is not matched by numeric ops.
func TestFilterNumStringFieldRejected(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString("5")), makeField("ID", makeInt(1))),
	)

	got := All(items, "[Name==5]")
	assert.Nil(t, got)
}

// TestFilterNumNilValueRejected verifies that NilValue is not matched by numeric ops.
func TestFilterNumNilValueRejected(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Count", gobspect.NilValue{}), makeField("ID", makeInt(1))),
	)

	got := All(items, "[Count==0]")
	assert.Nil(t, got)
}

// — Feature 4: OR filter operator (|) ————————————————————————————————————————

// TestFilterORBothMatch verifies two-way OR returns true when both alternatives match.
func TestFilterORBothMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(1))),
	)

	got := All(items, "[Status=active]|[Status=pending]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestFilterOROneMatch verifies two-way OR returns true when only one alternative matches.
func TestFilterOROneMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("pending")), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Status", makeString("done")), makeField("ID", makeInt(2))),
	)

	got := All(items, "[Status=active]|[Status=pending]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestFilterORNeitherMatch verifies two-way OR returns false when neither alternative matches.
func TestFilterORNeitherMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("done")), makeField("ID", makeInt(1))),
	)

	got := All(items, "[Status=active]|[Status=pending]")
	assert.Nil(t, got)
}

// TestFilterORThreeWayMiddleMatch verifies three-way OR returns true when the middle alternative matches.
func TestFilterORThreeWayMiddleMatch(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("B")), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Status", makeString("none")), makeField("ID", makeInt(2))),
	)

	got := All(items, "[Status=A]|[Status=B]|[Status=C]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestFilterORInAllSlice verifies OR inside All filters a slice where multiple
// elements match different alternatives.
func TestFilterORInAllSlice(t *testing.T) {
	items := makeSlice(
		makeStruct("Order", makeField("Status", makeString("active")), makeField("ID", makeInt(1))),
		makeStruct("Order", makeField("Status", makeString("pending")), makeField("ID", makeInt(2))),
		makeStruct("Order", makeField("Status", makeString("done")), makeField("ID", makeInt(3))),
		makeStruct("Order", makeField("Status", makeString("active")), makeField("ID", makeInt(4))),
	)

	got := All(items, "[Status=active]|[Status=pending]")
	require.Len(t, got, 3)
}

// TestFilterORInGet verifies OR inside Get returns first matching element.
func TestFilterORInGet(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Status", makeString("done")), makeField("ID", makeInt(1))),
		makeStruct("Item", makeField("Status", makeString("pending")), makeField("ID", makeInt(2))),
		makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(3))),
	)

	v, ok := Get(items, "[Status=active]|[Status=pending].ID")
	require.True(t, ok)
	assert.Equal(t, makeInt(2), v)
}

// TestFilterORPrecededByAND verifies that a filter preceding an OR group works as
// sequential segments: the first segment filters the collection, and the second
// OR segment filters within a nested collection.
func TestFilterORPrecededByAND(t *testing.T) {
	// Path: [Status=active][Category=A]|[Category=B]
	// This is two segments: [Status=active] then [Category=A]|[Category=B].
	// Each segment is a filter on a SliceValue: [Status=active] narrows the outer
	// items, then for each surviving item [Category=A]|[Category=B] applies to its
	// Sub field (which is a nested slice).
	items := makeSlice(
		// Status=active, Sub has Category=A: second filter matches
		makeStruct("Item", makeField("Status", makeString("active")), makeField("Sub", makeSlice(
			makeStruct("Sub", makeField("Category", makeString("A")), makeField("ID", makeInt(1))),
		))),
		// Status=active, Sub has Category=B: second filter matches
		makeStruct("Item", makeField("Status", makeString("active")), makeField("Sub", makeSlice(
			makeStruct("Sub", makeField("Category", makeString("B")), makeField("ID", makeInt(2))),
		))),
		// Status=inactive: first filter excludes this
		makeStruct("Item", makeField("Status", makeString("inactive")), makeField("Sub", makeSlice(
			makeStruct("Sub", makeField("Category", makeString("A")), makeField("ID", makeInt(3))),
		))),
		// Status=active, Sub has Category=C: second filter excludes
		makeStruct("Item", makeField("Status", makeString("active")), makeField("Sub", makeSlice(
			makeStruct("Sub", makeField("Category", makeString("C")), makeField("ID", makeInt(4))),
		))),
	)

	got := All(items, "[Status=active].Sub[Category=A]|[Category=B].ID")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
}

// TestFilterORInWildcardDescent verifies OR filter works inside wildcard descent.
func TestFilterORInWildcardDescent(t *testing.T) {
	root := makeStruct("Root",
		makeField("Items", makeSlice(
			makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(1))),
			makeStruct("Item", makeField("Status", makeString("pending")), makeField("ID", makeInt(2))),
			makeStruct("Item", makeField("Status", makeString("done")), makeField("ID", makeInt(3))),
		)),
	)

	got := All(root, "..[Status=active]|[Status=pending]")
	require.Len(t, got, 2)
}

// — [Field==true/false] bool filter edge cases ———————————————————————————————

// TestFilterBoolEqEdgeCases is a table-driven test covering bool filter matching,
// case-insensitive literal parsing, and parse-time rejection of invalid literals.
func TestFilterBoolEqEdgeCases(t *testing.T) {
	trueItem := makeStruct("Item", makeField("Enabled", makeBool(true)), makeField("ID", makeInt(1)))
	falseItem := makeStruct("Item", makeField("Enabled", makeBool(false)), makeField("ID", makeInt(2)))
	both := makeSlice(trueItem, falseItem)

	matchCases := []struct {
		name    string
		filter  string
		input   gobspect.Value
		wantLen int
		wantID  int64
	}{
		{"[Enabled==true] matches BoolValue{true}", "[Enabled==true]", both, 1, 1},
		{"[Enabled==true] no match BoolValue{false}", "[Enabled==true]", falseItem, 0, 0},
		{"[Enabled==false] matches BoolValue{false}", "[Enabled==false]", both, 1, 2},
		{"[Enabled==false] no match BoolValue{true}", "[Enabled==false]", trueItem, 0, 0},
		{"[Enabled==TRUE] matches BoolValue{true} case-ins", "[Enabled==TRUE]", both, 1, 1},
		{"[Enabled==FALSE] matches BoolValue{false} case-ins", "[Enabled==FALSE]", both, 1, 2},
	}
	for _, tc := range matchCases {
		t.Run(tc.name, func(t *testing.T) {
			got := All(tc.input, tc.filter)
			if tc.wantLen == 0 {
				assert.Nil(t, got)
			} else {
				require.Len(t, got, tc.wantLen)
				assert.Equal(t, makeInt(tc.wantID), MustGet(got[0], "ID"))
			}
		})
	}

	// Non-bool, non-numeric patterns on == must be rejected at parse time.
	errCases := []struct {
		name   string
		filter string
		wantIn string
	}{
		{"banana is not a bool or number", "[Enabled==banana]", "banana"},
		{"bare word is not a bool or number", "[Enabled==notabool]", "notabool"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.filter)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantIn)
		})
	}
}
