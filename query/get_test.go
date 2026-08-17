package query

import (
	"fmt"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — test fixtures ——————————————————————————————————————————————————————————

func makeString(s string) gobspect.Value { return gobspect.StringValue{V: s} }
func makeInt(i int64) gobspect.Value     { return gobspect.IntValue{V: i} }
func makeBool(b bool) gobspect.Value     { return gobspect.BoolValue{V: b} }

func makeStruct(name string, fields ...gobspect.Field) gobspect.StructValue {
	return gobspect.StructValue{TypeName: name, Fields: fields}
}

func makeField(name string, v gobspect.Value) gobspect.Field {
	return gobspect.Field{Name: name, Value: v}
}

func makeSlice(elems ...gobspect.Value) gobspect.SliceValue {
	return gobspect.SliceValue{Elems: elems}
}

func makeArray(elems ...gobspect.Value) gobspect.ArrayValue {
	return gobspect.ArrayValue{Len: len(elems), Elems: elems}
}

func makeMap(entries ...gobspect.MapEntry) gobspect.MapValue {
	return gobspect.MapValue{Entries: entries}
}

func entry(k, v gobspect.Value) gobspect.MapEntry {
	return gobspect.MapEntry{Key: k, Value: v}
}

func wrapped(typeName string, v gobspect.Value) gobspect.InterfaceValue {
	return gobspect.InterfaceValue{TypeName: typeName, Value: v}
}

// — TestGetPath ——————————————————————————————————————————————————————————————

// TestGetPathEmptyPath verifies that the empty path is the identity.
func TestGetPathEmptyPath(t *testing.T) {
	root := makeString("hello")
	p, err := Parse("")
	require.NoError(t, err)
	v, ok := GetPath(root, p)
	require.True(t, ok)
	assert.Equal(t, root, v)
}

// TestGetPathStructField covers StructValue field navigation.
func TestGetPathStructField(t *testing.T) {
	root := makeStruct("Person",
		makeField("Name", makeString("Alice")),
		makeField("Age", makeInt(30)),
	)

	tests := []struct {
		name      string
		expr      string
		wantValue gobspect.Value
		wantOK    bool
	}{
		{
			name:      "top-level field present",
			expr:      "Name",
			wantValue: makeString("Alice"),
			wantOK:    true,
		},
		{
			name:      "second field",
			expr:      "Age",
			wantValue: makeInt(30),
			wantOK:    true,
		},
		{
			name:   "absent field",
			expr:   "Missing",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.expr)
			require.NoError(t, err)
			v, ok := GetPath(root, p)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantValue, v)
			}
		})
	}
}

// TestGetPathNestedStruct covers deeply nested struct field navigation.
func TestGetPathNestedStruct(t *testing.T) {
	customer := makeStruct("Customer",
		makeField("Name", makeString("Bob")),
	)
	order := makeStruct("Order",
		makeField("Customer", customer),
		makeField("Total", makeInt(100)),
	)
	root := makeStruct("Root",
		makeField("Order", order),
	)

	v, ok := GetPath(root, mustParse("Order.Customer.Name"))
	require.True(t, ok)
	assert.Equal(t, makeString("Bob"), v)

	_, ok = GetPath(root, mustParse("Order.Customer.Missing"))
	assert.False(t, ok)
}

// TestGetPathSliceIndex covers positive integer index navigation.
func TestGetPathSliceIndex(t *testing.T) {
	slice := makeSlice(makeString("a"), makeString("b"), makeString("c"))
	root := makeStruct("Root", makeField("Items", slice))

	tests := []struct {
		name      string
		expr      string
		wantValue gobspect.Value
		wantOK    bool
	}{
		{"index 0", "Items.0", makeString("a"), true},
		{"index 1", "Items.1", makeString("b"), true},
		{"index 2", "Items.2", makeString("c"), true},
		{"out of bounds high", "Items.3", nil, false},
		{"out of bounds very high", "Items.99", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := Get(root, tt.expr)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantValue, v)
			}
		})
	}
}

// TestGetPathNegativeIndex covers negative integer index navigation.
func TestGetPathNegativeIndex(t *testing.T) {
	slice := makeSlice(makeString("a"), makeString("b"), makeString("c"))
	root := makeStruct("Root", makeField("Items", slice))

	tests := []struct {
		name      string
		expr      string
		wantValue gobspect.Value
		wantOK    bool
	}{
		{"index -1 (last)", "Items.-1", makeString("c"), true},
		{"index -2 (second-to-last)", "Items.-2", makeString("b"), true},
		{"index -3 (first)", "Items.-3", makeString("a"), true},
		{"index -4 (out of bounds)", "Items.-4", nil, false},
		{"large negative", "Items.-100", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := Get(root, tt.expr)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantValue, v)
			}
		})
	}
}

// TestGetPathArrayIndex covers ArrayValue index navigation.
func TestGetPathArrayIndex(t *testing.T) {
	arr := makeArray(makeInt(10), makeInt(20), makeInt(30))
	root := makeStruct("Root", makeField("Arr", arr))

	v, ok := Get(root, "Arr.0")
	require.True(t, ok)
	assert.Equal(t, makeInt(10), v)

	v, ok = Get(root, "Arr.-1")
	require.True(t, ok)
	assert.Equal(t, makeInt(30), v)

	_, ok = Get(root, "Arr.5")
	assert.False(t, ok)
}

// TestGetPathMapStringKey covers MapValue string-key navigation.
func TestGetPathMapStringKey(t *testing.T) {
	m := makeMap(
		entry(makeString("foo"), makeInt(1)),
		entry(makeString("bar"), makeInt(2)),
	)
	root := makeStruct("Root", makeField("Meta", m))

	tests := []struct {
		name      string
		expr      string
		wantValue gobspect.Value
		wantOK    bool
	}{
		{"existing key foo", "Meta.foo", makeInt(1), true},
		{"existing key bar", "Meta.bar", makeInt(2), true},
		{"missing key", "Meta.baz", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := Get(root, tt.expr)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantValue, v)
			}
		})
	}
}

// TestGetPathNumericMapKey verifies that numeric-looking segments correctly
// navigate map[string]T by falling back to string-key lookup.
func TestGetPathNumericMapKey(t *testing.T) {
	m := makeMap(
		entry(makeString("42"), makeString("The Answer")),
		entry(makeString("100"), makeInt(100)),
	)
	root := makeStruct("Root", makeField("m", m))

	v, ok := Get(root, "m.42")
	require.True(t, ok)
	assert.Equal(t, makeString("The Answer"), v)

	v, ok = Get(root, "m.100")
	require.True(t, ok)
	assert.Equal(t, makeInt(100), v)

	// Negative index has no map fallback.
	_, ok = Get(root, "m.-1")
	assert.False(t, ok)
}

// TestGetPathMapNonStringKey verifies that map entries with non-string keys
// are silently skipped during string-based navigation.
func TestGetPathMapNonStringKey(t *testing.T) {
	m := makeMap(
		entry(makeInt(1), makeString("one")), // non-string key
		entry(makeString("two"), makeString("2")),
	)
	// Navigating by "1" should not find the int key.
	v, ok := Get(m, "1")
	// "1" would be treated as an index on a non-slice; not a string map lookup.
	// The map has no StringValue key "1", so not found.
	assert.False(t, ok)
	_ = v

	// "two" navigates the string key successfully.
	v, ok = Get(m, "two")
	// Get on a MapValue directly uses stepField if it's a segField.
	// But "two" parses as a field name, and the map has key StringValue "two".
	require.True(t, ok)
	assert.Equal(t, makeString("2"), v)
}

// TestGetPathInterfaceValueTransparency verifies that InterfaceValue is
// silently unwrapped before each segment is matched.
func TestGetPathInterfaceValueTransparency(t *testing.T) {
	inner := makeStruct("Inner", makeField("Value", makeString("found")))
	root := makeStruct("Root",
		makeField("Child", wrapped("Inner", inner)),
	)

	// Child is wrapped in InterfaceValue; GetPath should unwrap it.
	v, ok := Get(root, "Child.Value")
	require.True(t, ok)
	assert.Equal(t, makeString("found"), v)
}

// TestGetPathInterfaceValueAtRoot verifies unwrapping when root itself is
// wrapped in an InterfaceValue.
func TestGetPathInterfaceValueAtRoot(t *testing.T) {
	inner := makeStruct("Inner", makeField("X", makeInt(42)))
	root := wrapped("Inner", inner)

	v, ok := Get(root, "X")
	require.True(t, ok)
	assert.Equal(t, makeInt(42), v)
}

// TestGetPathInterfaceValueChain verifies multiple layers of interface wrapping
// are each unwrapped at the appropriate segment boundary.
func TestGetPathInterfaceValueChain(t *testing.T) {
	// Slice of interface-wrapped structs.
	elem := wrapped("Item", makeStruct("Item", makeField("Name", makeString("hello"))))
	slice := makeSlice(elem)
	root := makeStruct("Root", makeField("Items", slice))

	v, ok := Get(root, "Items.0.Name")
	require.True(t, ok)
	assert.Equal(t, makeString("hello"), v)
}

// TestGetPathScalarNode verifies that navigating into a scalar returns (nil, false).
func TestGetPathScalarNode(t *testing.T) {
	root := makeStruct("Root", makeField("Num", makeInt(5)))

	// Trying to navigate further into an IntValue.
	_, ok := Get(root, "Num.Something")
	assert.False(t, ok)

	_, ok = Get(root, "Num.0")
	assert.False(t, ok)
}

// TestGetPathOpaqueNode verifies that OpaqueValue nodes stop navigation.
func TestGetPathOpaqueNode(t *testing.T) {
	opaque := gobspect.OpaqueValue{TypeName: "time.Time", Encoding: "binary", Raw: []byte{1, 2, 3}}
	root := makeStruct("Root", makeField("CreatedAt", opaque))

	_, ok := Get(root, "CreatedAt.anything")
	assert.False(t, ok)
}

// TestGetPathNilValue verifies NilValue stops navigation.
func TestGetPathNilValue(t *testing.T) {
	root := makeStruct("Root", makeField("Ptr", gobspect.NilValue{}))

	_, ok := Get(root, "Ptr.Field")
	assert.False(t, ok)
}

// TestGetPathEmptySlice covers out-of-bounds on an empty slice.
func TestGetPathEmptySlice(t *testing.T) {
	slice := makeSlice() // zero elements
	root := makeStruct("Root", makeField("Items", slice))

	_, ok := Get(root, "Items.0")
	assert.False(t, ok)

	_, ok = Get(root, "Items.-1")
	assert.False(t, ok)
}

// TestGetPathComplexNested covers the PRD example: Orders.0.Customer.Name
func TestGetPathComplexNested(t *testing.T) {
	customer := makeStruct("Customer",
		makeField("Name", makeString("Alice")),
	)
	order0 := makeStruct("Order",
		makeField("Customer", customer),
		makeField("Total", makeInt(50)),
	)
	order1 := makeStruct("Order",
		makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Bob")))),
		makeField("Total", makeInt(75)),
	)
	orders := makeSlice(order0, order1)
	root := makeStruct("Root", makeField("Orders", orders))

	v, ok := Get(root, "Orders.0.Customer.Name")
	require.True(t, ok)
	assert.Equal(t, makeString("Alice"), v)

	v, ok = Get(root, "Orders.1.Customer.Name")
	require.True(t, ok)
	assert.Equal(t, makeString("Bob"), v)

	v, ok = Get(root, "Orders.-1.Customer.Name")
	require.True(t, ok)
	assert.Equal(t, makeString("Bob"), v)

	v, ok = Get(root, "Orders.-2.Customer.Name")
	require.True(t, ok)
	assert.Equal(t, makeString("Alice"), v)
}

// — TestGet (convenience wrapper) —————————————————————————————————————————

// TestGetPanicsOnInvalidSyntax verifies that Get panics for invalid expressions.
func TestGetPanicsOnInvalidSyntax(t *testing.T) {
	root := makeString("x")
	assert.Panics(t, func() {
		Get(root, ".bad")
	})
	assert.Panics(t, func() {
		Get(root, "Orders.")
	})
}

// TestGetReturnsFalseOnMissingPath verifies that Get returns (nil, false) for
// valid syntax that does not resolve, without panicking.
func TestGetReturnsFalseOnMissingPath(t *testing.T) {
	root := makeStruct("Root", makeField("Name", makeString("hi")))
	assert.NotPanics(t, func() {
		v, ok := Get(root, "Missing")
		assert.False(t, ok)
		assert.Nil(t, v)
	})
}

// — TestMustGet ——————————————————————————————————————————————————————————————

// TestMustGetSuccess verifies the happy path.
func TestMustGetSuccess(t *testing.T) {
	root := makeStruct("Root", makeField("Name", makeString("hello")))
	v := MustGet(root, "Name")
	assert.Equal(t, makeString("hello"), v)
}

// TestMustGetPanicsOnInvalidSyntax verifies panic on bad syntax.
func TestMustGetPanicsOnInvalidSyntax(t *testing.T) {
	root := makeString("x")
	assert.Panics(t, func() {
		MustGet(root, ".bad")
	})
}

// TestMustGetPanicsOnUnresolvedPath verifies panic when path does not resolve.
func TestMustGetPanicsOnUnresolvedPath(t *testing.T) {
	root := makeStruct("Root", makeField("Name", makeString("hello")))
	assert.Panics(t, func() {
		MustGet(root, "Missing")
	})
}

// TestMustGetPanicMessageContainsExpr verifies the panic message includes the path.
func TestMustGetPanicMessageContainsExpr(t *testing.T) {
	root := makeStruct("Root")
	expr := "Orders.0.Customer.Name"
	defer func() {
		r := recover()
		require.NotNil(t, r)
		msg, ok := r.(string)
		require.True(t, ok)
		assert.Contains(t, msg, expr)
	}()
	MustGet(root, expr)
}

// TestMustGetPanicMessageContainsFailingSegment verifies the panic message includes
// the specific segment that failed to resolve, not just the full expression.
func TestMustGetPanicMessageContainsFailingSegment(t *testing.T) {
	tests := []struct {
		name    string
		root    gobspect.Value
		expr    string
		wantSeg string // expected failing segment text in the panic message
	}{
		{
			name:    "field segment missing",
			root:    makeStruct("Root"),
			expr:    "Orders.0.Customer.Name",
			wantSeg: "Orders",
		},
		{
			name:    "index segment out of bounds",
			root:    makeStruct("Root", makeField("Orders", makeSlice())),
			expr:    "Orders.0.Customer.Name",
			wantSeg: "0",
		},
		{
			name:    "negative index out of bounds",
			root:    makeStruct("Root", makeField("Items", makeSlice())),
			expr:    "Items.-1",
			wantSeg: "-1",
		},
		{
			name:    "wildcard on empty slice",
			root:    makeSlice(),
			expr:    "*.Name",
			wantSeg: "*",
		},
		{
			name:    "existence filter no match",
			root:    makeSlice(makeStruct("Item", makeField("Price", makeInt(5)))),
			expr:    "[Status!].Price",
			wantSeg: "[Status!]",
		},
		{
			name:    "glob filter no match",
			root:    makeSlice(makeStruct("Item", makeField("Status", makeString("inactive")))),
			expr:    "[Status=active].Price",
			wantSeg: "[Status=active]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg string
			func() {
				defer func() {
					r := recover()
					require.NotNil(t, r)
					var ok bool
					msg, ok = r.(string)
					require.True(t, ok)
				}()
				MustGet(tt.root, tt.expr)
			}()
			assert.Contains(t, msg, tt.expr, "panic message should contain the full expression")
			assert.Contains(t, msg, tt.wantSeg, "panic message should contain the failing segment")
		})
	}
}

// — TestGetPath allValueTypes ————————————————————————————————————————————————

// TestGetPathAllValueTypes exercises GetPath against every concrete Value type
// to ensure none cause a panic.
func TestGetPathAllValueTypes(t *testing.T) {
	scalars := []struct {
		name  string
		value gobspect.Value
	}{
		{"IntValue", makeInt(1)},
		{"UintValue", gobspect.UintValue{V: 1}},
		{"FloatValue", gobspect.FloatValue{V: 1.0}},
		{"ComplexValue", gobspect.ComplexValue{Real: 1, Imag: 2}},
		{"BoolValue", makeBool(true)},
		{"StringValue", makeString("x")},
		{"BytesValue", gobspect.BytesValue{V: []byte{1}}},
		{"NilValue", gobspect.NilValue{}},
		{"OpaqueValue", gobspect.OpaqueValue{TypeName: "T"}},
	}

	for _, tt := range scalars {
		t.Run(tt.name+" empty path returns self", func(t *testing.T) {
			v, ok := Get(tt.value, "")
			require.True(t, ok)
			assert.Equal(t, tt.value, v)
		})

		t.Run(tt.name+" field segment returns false", func(t *testing.T) {
			_, ok := Get(tt.value, "Field")
			assert.False(t, ok)
		})

		t.Run(tt.name+" index segment returns false", func(t *testing.T) {
			_, ok := Get(tt.value, "0")
			assert.False(t, ok)
		})
	}
}

// — TestGetPathDirectOnCollections ——————————————————————————————————————————

// TestGetPathDirectOnSlice verifies Get works when root is a SliceValue directly.
func TestGetPathDirectOnSlice(t *testing.T) {
	slice := makeSlice(makeString("x"), makeString("y"))
	v, ok := Get(slice, "1")
	require.True(t, ok)
	assert.Equal(t, makeString("y"), v)
}

// TestGetPathDirectOnArray verifies Get works when root is an ArrayValue directly.
func TestGetPathDirectOnArray(t *testing.T) {
	arr := makeArray(makeInt(10), makeInt(20))
	v, ok := Get(arr, "0")
	require.True(t, ok)
	assert.Equal(t, makeInt(10), v)
}

// TestGetPathDirectOnMap verifies Get works when root is a MapValue directly.
func TestGetPathDirectOnMap(t *testing.T) {
	m := makeMap(entry(makeString("key"), makeString("value")))
	v, ok := Get(m, "key")
	require.True(t, ok)
	assert.Equal(t, makeString("value"), v)
}

// — TestGetPathWildcard ——————————————————————————————————————————————————————

// TestGetPathWildcardReturnsFirst verifies Get with * returns the first element.
func TestGetPathWildcardReturnsFirst(t *testing.T) {
	slice := makeSlice(makeString("first"), makeString("second"), makeString("third"))
	v, ok := Get(slice, "*")
	require.True(t, ok)
	assert.Equal(t, makeString("first"), v)
}

// TestGetPathWildcardOnEmptyCollectionReturnsFalse verifies Get with * on an
// empty slice returns (nil, false) without panicking.
func TestGetPathWildcardOnEmptyCollectionReturnsFalse(t *testing.T) {
	_, ok := Get(makeSlice(), "*")
	assert.False(t, ok)

	_, ok = Get(makeArray(), "*")
	assert.False(t, ok)

	_, ok = Get(makeMap(), "*")
	assert.False(t, ok)
}

// TestGetPathWildcardBeforeField verifies Get with *.Field navigates into the
// first element's field.
func TestGetPathWildcardBeforeField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(10))),
		makeStruct("Item", makeField("Price", makeInt(20))),
	)
	v, ok := Get(items, "*.Price")
	require.True(t, ok)
	assert.Equal(t, makeInt(10), v, "Get should return the first element's field")
}

// TestGetPathWildcardOnMap verifies Get with * on a MapValue returns the first
// entry value.
func TestGetPathWildcardOnMap(t *testing.T) {
	m := makeMap(
		entry(makeString("a"), makeInt(1)),
		entry(makeString("b"), makeInt(2)),
	)
	v, ok := Get(m, "*")
	require.True(t, ok)
	assert.Equal(t, makeInt(1), v)
}

// — TestGetPathDescent ————————————————————————————————————————————————————————

// TestGetPathDescentAtRoot verifies ..Name matches a field directly on the root.
func TestGetPathDescentAtRoot(t *testing.T) {
	root := makeStruct("Root",
		makeField("Price", makeInt(42)),
		makeField("Name", makeString("thing")),
	)
	v, ok := Get(root, "..Price")
	require.True(t, ok)
	assert.Equal(t, makeInt(42), v)
}

// TestGetPathDescentDepth1 verifies ..Name finds a field one level deep.
func TestGetPathDescentDepth1(t *testing.T) {
	inner := makeStruct("Inner", makeField("Price", makeInt(99)))
	root := makeStruct("Root", makeField("Orders", inner))

	v, ok := Get(root, "..Price")
	require.True(t, ok)
	assert.Equal(t, makeInt(99), v)
}

// TestGetPathDescentDepth2 verifies ..Name finds a field two levels deep.
func TestGetPathDescentDepth2(t *testing.T) {
	deep := makeStruct("Deep", makeField("Price", makeInt(7)))
	mid := makeStruct("Mid", makeField("Detail", deep))
	root := makeStruct("Root", makeField("Orders", mid))

	v, ok := Get(root, "..Price")
	require.True(t, ok)
	assert.Equal(t, makeInt(7), v)
}

// TestGetPathDescentNotFound verifies ..Name returns (nil, false) when absent everywhere.
func TestGetPathDescentNotFound(t *testing.T) {
	root := makeStruct("Root",
		makeField("A", makeStruct("A", makeField("B", makeInt(1)))),
	)
	_, ok := Get(root, "..Missing")
	assert.False(t, ok)
}

// TestGetPathDescentThroughInterface verifies ..Name unwraps InterfaceValue nodes.
func TestGetPathDescentThroughInterface(t *testing.T) {
	inner := makeStruct("Inner", makeField("Price", makeInt(55)))
	root := makeStruct("Root",
		makeField("Item", wrapped("Inner", inner)),
	)
	v, ok := Get(root, "..Price")
	require.True(t, ok)
	assert.Equal(t, makeInt(55), v)
}

// TestGetPathDescentFollowedBySegments verifies ..Name.SubField chaining.
func TestGetPathDescentFollowedBySegments(t *testing.T) {
	customer := makeStruct("Customer", makeField("Name", makeString("Alice")))
	order := makeStruct("Order", makeField("Customer", customer))
	root := makeStruct("Root", makeField("Orders", makeSlice(order)))

	// Find the first Orders slice element, then navigate .0.Customer.Name
	v, ok := Get(root, "..Orders.0.Customer.Name")
	require.True(t, ok)
	assert.Equal(t, makeString("Alice"), v)
}

// TestMustGetPanicMessageContainsDescentSeg verifies the panic message includes the
// ..Name segment string when recursive descent fails.
func TestMustGetPanicMessageContainsDescentSeg(t *testing.T) {
	root := makeStruct("Root")
	var msg string
	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			var ok bool
			msg, ok = r.(string)
			require.True(t, ok)
		}()
		MustGet(root, "..Missing")
	}()
	assert.Contains(t, msg, "..Missing")
}

// TestMustGetPanicMessageContainsContainsFilter verifies that the panic message
// for a failed [Field~pattern] filter segment includes the "~" operator string.
// This exercises the filterOpContains case in segString.
func TestMustGetPanicMessageContainsContainsFilter(t *testing.T) {
	// A root with Items that have no Tags field — [Tags~go] will fail to match.
	root := makeStruct("Root",
		makeField("Items", makeSlice(makeStruct("Item", makeField("Name", makeString("x"))))),
	)

	var msg string
	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			var ok bool
			msg, ok = r.(string)
			require.True(t, ok)
		}()
		MustGet(root, "Items[Tags~go]")
	}()
	assert.Contains(t, msg, "[Tags~go]")
}

// TestCollectFilteredNonCollection verifies that applying a filter to a
// non-collection value that does NOT match the filter produces nil results.
// Scalars like StringValue and IntValue have no fields, so filters on them
// never match and always produce nil.
func TestCollectFilteredNonCollection(t *testing.T) {
	// A StringValue has no fields; [Field!] cannot match it.
	got := All(makeString("hello"), "[Field!]")
	assert.Nil(t, got, "filter on a scalar should produce nil results")

	got = All(makeInt(42), "[Status=active]")
	assert.Nil(t, got, "filter on an int scalar should produce nil results")
}

// TestGetFilterOnNonCollectionMatchingStruct verifies that a filter segment
// applied directly to a StructValue (not a collection) acts as a predicate:
// when the struct passes the filter, Get returns the struct itself.
func TestGetFilterOnNonCollectionMatchingStruct(t *testing.T) {
	order := makeStruct("Order",
		makeField("Status", makeString("active")),
		makeField("Total", makeInt(50)),
	)

	// Filter applied directly to a struct — should match and return the struct.
	v, ok := Get(order, "[Status=active]")
	require.True(t, ok, "Get with [Status=active] on a matching struct should succeed")
	assert.Equal(t, order, v)

	// Non-matching filter should return (nil, false).
	_, ok = Get(order, "[Status=cancelled]")
	assert.False(t, ok, "Get with non-matching filter should return false")
}

// TestAllFilterOnNonCollectionViaPath verifies that Orders.0[Status=active]
// treats the filter as a predicate when applied to the single struct at Orders.0.
func TestAllFilterOnNonCollectionViaPath(t *testing.T) {
	order0 := makeStruct("Order",
		makeField("Status", makeString("active")),
		makeField("Total", makeInt(50)),
	)
	order1 := makeStruct("Order",
		makeField("Status", makeString("cancelled")),
		makeField("Total", makeInt(75)),
	)
	root := makeStruct("Root", makeField("Orders", makeSlice(order0, order1)))

	// order0 has Status=active, so Orders.0[Status=active] should return [order0].
	got := All(root, "Orders.0[Status=active]")
	require.Len(t, got, 1, "All should return exactly one result")
	assert.Equal(t, order0, got[0])

	// order1 has Status=cancelled, so Orders.1[Status=active] should return nil.
	got = All(root, "Orders.1[Status=active]")
	assert.Nil(t, got, "non-matching filter on a single struct should return nil")
}

// TestSegStringDefaultCase verifies that segString returns "?" for an unknown
// segment kind. This exercises the default branch.
func TestSegStringDefaultCase(t *testing.T) {
	s := segment{kind: segKind(999)}
	result := segString(s)
	assert.Equal(t, "?", result)
}

// TestWalkGetPathNilInterfaceInner verifies walkGetPath returns (nil, false) when
// it encounters a nil value mid-path (unwrapped InterfaceValue with nil inner).
func TestWalkGetPathNilInterfaceInner(t *testing.T) {
	// An InterfaceValue whose Value is nil — unwrapInterface returns nil, so the
	// next segment check must detect cur==nil and return false.
	nilWrapped := gobspect.InterfaceValue{TypeName: "T", Value: nil}
	root := makeStruct("Root", makeField("Child", nilWrapped))
	_, ok := Get(root, "Child.Field")
	assert.False(t, ok)
}

// TestWalkGetPathDefaultSegmentKind verifies the default case in walkGetPath
// (unknown segment kind) returns (nil, false) without panicking.
func TestWalkGetPathDefaultSegmentKind(t *testing.T) {
	root := makeString("x")
	// Build a Path with an unknown segment kind directly.
	p := Path{segs: []segment{{kind: segKind(999)}}}
	v, ok := GetPath(root, p)
	assert.False(t, ok)
	assert.Nil(t, v)
}

// TestSegStringOrAltFilter verifies that segString for an OR-group filter
// delegates to Path.String() to produce the canonical form.
func TestSegStringOrAltFilter(t *testing.T) {
	// Build an OR-group segment directly.
	orSeg := segment{
		kind: segFilter,
		orAlts: []segment{
			{kind: segFilter, name: "A", filterOp: filterOpGlob, filterPattern: "x"},
			{kind: segFilter, name: "B", filterOp: filterOpGlob, filterPattern: "y"},
		},
	}
	result := segString(orSeg)
	assert.Contains(t, result, "[A=x]")
	assert.Contains(t, result, "[B=y]")
	assert.Contains(t, result, "|")
}

// — TestGetPath projection ——————————————————————————————————————————————————

// TestGetPathProjectionStruct verifies projection on a struct yields an
// anonymous StructValue with only the requested fields.
func TestGetPathProjectionStruct(t *testing.T) {
	root := makeStruct("Item",
		makeField("SKU", makeString("ABC-123")),
		makeField("Price", makeInt(42)),
		makeField("Stock", makeInt(100)),
	)

	v, ok := Get(root, "SKU,Price")
	require.True(t, ok)

	sv, isSV := v.(gobspect.StructValue)
	require.True(t, isSV, "projection should return a StructValue")
	assert.Equal(t, ProjectionTypeName, sv.TypeName, "projected struct should have ProjectionTypeName")
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "SKU", sv.Fields[0].Name)
	assert.Equal(t, makeString("ABC-123"), sv.Fields[0].Value)
	assert.Equal(t, "Price", sv.Fields[1].Name)
	assert.Equal(t, makeInt(42), sv.Fields[1].Value)
}

// TestGetPathProjectionMissingField verifies that a projected field not present
// in the source struct is filled with NilValue.
func TestGetPathProjectionMissingField(t *testing.T) {
	root := makeStruct("Item",
		makeField("SKU", makeString("ABC-123")),
	)

	v, ok := Get(root, "SKU,MissingField")
	require.True(t, ok)

	sv := v.(gobspect.StructValue)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "SKU", sv.Fields[0].Name)
	assert.Equal(t, makeString("ABC-123"), sv.Fields[0].Value)
	assert.Equal(t, "MissingField", sv.Fields[1].Name)
	assert.Equal(t, gobspect.NilValue{}, sv.Fields[1].Value)
}

// TestGetPathProjectionMap verifies projection on a MapValue with string keys.
func TestGetPathProjectionMap(t *testing.T) {
	m := makeMap(
		entry(makeString("color"), makeString("red")),
		entry(makeString("size"), makeString("large")),
		entry(makeString("weight"), makeInt(10)),
	)

	v, ok := Get(m, "color,size")
	require.True(t, ok)

	sv := v.(gobspect.StructValue)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "color", sv.Fields[0].Name)
	assert.Equal(t, makeString("red"), sv.Fields[0].Value)
	assert.Equal(t, "size", sv.Fields[1].Name)
	assert.Equal(t, makeString("large"), sv.Fields[1].Value)
}

// TestGetPathProjectionOnScalar verifies that projection on a non-struct/map
// returns (nil, false).
func TestGetPathProjectionOnScalar(t *testing.T) {
	_, ok := Get(makeString("hello"), "A,B")
	assert.False(t, ok)

	_, ok = Get(makeInt(42), "A,B")
	assert.False(t, ok)
}

// TestGetPathProjectionAfterWildcard verifies *.SKU,Price returns the
// projection of the first element.
func TestGetPathProjectionAfterWildcard(t *testing.T) {
	items := makeSlice(
		makeStruct("Item",
			makeField("SKU", makeString("A")),
			makeField("Price", makeInt(10)),
			makeField("Stock", makeInt(50)),
		),
		makeStruct("Item",
			makeField("SKU", makeString("B")),
			makeField("Price", makeInt(20)),
			makeField("Stock", makeInt(30)),
		),
	)

	v, ok := Get(items, "*.SKU,Price")
	require.True(t, ok, "Get should resolve *.SKU,Price")

	sv := v.(gobspect.StructValue)
	assert.Equal(t, ProjectionTypeName, sv.TypeName)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "SKU", sv.Fields[0].Name)
	assert.Equal(t, makeString("A"), sv.Fields[0].Value)
}

// TestGetNestedProjection verifies that a "/"-separated projection field
// navigates into a nested struct and uses the leaf name as the column header.
func TestGetNestedProjection(t *testing.T) {
	root := makeStruct("Item",
		makeField("Name", makeString("Widget")),
		makeField("Address", makeStruct("Address",
			makeField("City", makeString("Portland")),
			makeField("Zip", makeString("97201")),
		)),
	)

	v, ok := Get(root, "Name,Address/Zip")
	require.True(t, ok)

	sv := v.(gobspect.StructValue)
	assert.Equal(t, ProjectionTypeName, sv.TypeName)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "Name", sv.Fields[0].Name)
	assert.Equal(t, makeString("Widget"), sv.Fields[0].Value)
	assert.Equal(t, "Zip", sv.Fields[1].Name)
	assert.Equal(t, makeString("97201"), sv.Fields[1].Value)
}

// TestGetNestedProjectionMissing verifies that a sub-path that does not exist
// produces a NilValue for that column.
func TestGetNestedProjectionMissing(t *testing.T) {
	root := makeStruct("Item",
		makeField("Name", makeString("Widget")),
	)

	v, ok := Get(root, "Name,Address/Zip")
	require.True(t, ok)

	sv := v.(gobspect.StructValue)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "Zip", sv.Fields[1].Name)
	assert.Equal(t, gobspect.NilValue{}, sv.Fields[1].Value)
}

// TestGetNestedProjectionThreeLevels verifies three-level "/" navigation
// within a projection alongside a flat field.
func TestGetNestedProjectionThreeLevels(t *testing.T) {
	root := makeStruct("Order",
		makeField("ID", makeInt(99)),
		makeField("Shipping", makeStruct("Shipping",
			makeField("Address", makeStruct("Address",
				makeField("Zip", makeString("10001")),
			)),
		)),
	)

	v, ok := Get(root, "ID,Shipping/Address/Zip")
	require.True(t, ok)

	sv := v.(gobspect.StructValue)
	assert.Equal(t, ProjectionTypeName, sv.TypeName)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "ID", sv.Fields[0].Name)
	assert.Equal(t, makeInt(99), sv.Fields[0].Value)
	assert.Equal(t, "Zip", sv.Fields[1].Name)
	assert.Equal(t, makeString("10001"), sv.Fields[1].Value)
}

// TestGetBareSlashIsLiteralName locks in the documented semantics of "/"
// outside a projection: a bare token containing "/" is a literal field/key
// name (string map keys may contain slashes), never nested navigation.
func TestGetBareSlashIsLiteralName(t *testing.T) {
	m := gobspect.MapValue{Entries: []gobspect.MapEntry{
		{Key: makeString("path/to"), Value: makeInt(7)},
	}}
	v, ok := Get(m, "path/to")
	require.True(t, ok, "a map key containing '/' must be reachable as a literal name")
	assert.Equal(t, makeInt(7), v)

	root := makeStruct("Item",
		makeField("Address", makeStruct("Address",
			makeField("Zip", makeString("97201")),
		)),
	)
	_, ok = Get(root, "Address/Zip")
	assert.False(t, ok, "bare 'Address/Zip' is a literal name, not navigation; use 'Address.Zip'")
	v, ok = Get(root, "Address.Zip")
	require.True(t, ok)
	assert.Equal(t, makeString("97201"), v)
}

// TestSegStringProjectionCase verifies segString output for a segProject segment.
func TestSegStringProjectionCase(t *testing.T) {
	s := segment{kind: segProject, projectFields: []string{"A", "B", "C"}}
	result := segString(s)
	assert.Equal(t, "A,B,C", result)
}

// — Get/All unified semantics —————————————————————————————————————————————————

// TestGetFirstMatchAcrossElements verifies Get returns the first full match in
// document order: a fan-out segment (* or a filter) must not commit to the
// first element when the rest of the path fails on it. Realistic with gob,
// which omits zero-valued fields.
func TestGetFirstMatchAcrossElements(t *testing.T) {
	root := makeStruct("Root",
		makeField("Items", makeSlice(
			makeStruct("Item", makeField("Price", makeInt(5))), // no Name
			makeStruct("Item", makeField("Price", makeInt(7)), makeField("Name", makeString("x"))),
		)),
	)

	v, ok := Get(root, "Items.*.Name")
	require.True(t, ok, "Get must find the first element that has Name")
	assert.Equal(t, makeString("x"), v)

	v, ok = Get(root, "Items[Price>0].Name")
	require.True(t, ok, "filter survivors without Name must be skipped")
	assert.Equal(t, makeString("x"), v)

	// Agreement with All: Get is All's first result.
	all := All(root, "Items.*.Name")
	require.NotEmpty(t, all)
	assert.Equal(t, all[0], v)
}

// TestGetUnwrapsTerminalInterfaceValue verifies Get and All agree on the shape
// of results that land on an interface-wrapped node: both return the inner
// concrete value.
func TestGetUnwrapsTerminalInterfaceValue(t *testing.T) {
	inner := makeStruct("Inner", makeField("X", makeInt(1)))
	root := makeStruct("Root", makeField("Child", wrapped("Inner", inner)))

	gv, ok := Get(root, "Child")
	require.True(t, ok)
	assert.Equal(t, gobspect.Value(inner), gv, "Get returns the unwrapped value")

	av := All(root, "Child")
	require.Len(t, av, 1)
	assert.Equal(t, gv, av[0], "Get and All agree")

	// Empty path on a wrapped root unwraps too.
	gv, ok = Get(wrapped("Inner", inner), "")
	require.True(t, ok)
	assert.Equal(t, gobspect.Value(inner), gv)
}

// — helpers ——————————————————————————————————————————————————————————————————

// mustParse is a test helper that calls Parse and panics on error.
func mustParse(expr string) Path {
	p, err := Parse(expr)
	if err != nil {
		panic(err)
	}
	return p
}

// TestFilterOnEmptyCollectionYieldsNothing verifies an element filter on an
// empty slice/array/map yields no results rather than testing the container
// itself as a predicate.
func TestFilterOnEmptyCollectionYieldsNothing(t *testing.T) {
	root := makeStruct("Root",
		makeField("Empty", makeSlice()),
		makeField("EmptyArr", makeArray()),
		makeField("EmptyMap", makeMap()),
		makeField("Full", makeSlice(makeStruct("Item", makeField("A", makeInt(1))))),
	)

	for _, expr := range []string{"Empty[Missing!!]", "EmptyArr[Missing!!]", "EmptyMap[Missing!!]", "Empty[X!]", "Empty[X=abc]"} {
		t.Run(expr, func(t *testing.T) {
			assert.Nil(t, All(root, expr))
			_, ok := Get(root, expr)
			assert.False(t, ok)
		})
	}

	// A non-empty collection still filters its elements.
	require.Len(t, All(root, "Full[Missing!!]"), 1)
}

// TestMustGetPanicMessageOperators verifies the blamed segment renders each
// filter operator as written, not a '=' fallback.
func TestMustGetPanicMessageOperators(t *testing.T) {
	root := makeStruct("Root",
		makeField("Items", makeSlice(makeStruct("Item",
			makeField("Price", makeInt(50)),
			makeField("Name", makeString("x")),
		))),
	)

	tests := []struct{ expr, seg string }{
		{"Items[Price!!]", "[Price!!]"},
		{"Items[Price!=*]", "[Price!=*]"},
		{"Items[Name!~go]", "[Name!~go]"},
		{"Items[Price==99]", "[Price==99]"},
		{"Items[Price<10]", "[Price<10]"},
		{"Items[Price>100]", "[Price>100]"},
		{"Items[Price<=10]", "[Price<=10]"},
		{"Items[Price>=100]", "[Price>=100]"},
		{`Items[Name="a=b"]`, `[Name="a=b"]`}, // quoting survives re-rendering
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			want := fmt.Sprintf("query.MustGet: path %q did not resolve at segment %q", tt.expr, tt.seg)
			assert.PanicsWithValue(t, want, func() { MustGet(root, tt.expr) })
		})
	}
}
