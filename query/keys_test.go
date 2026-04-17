package query

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeysPathStructValue verifies field names are returned in declaration order.
func TestKeysPathStructValue(t *testing.T) {
	root := makeStruct("Person",
		field_("Name", makeString("Alice")),
		field_("Age", makeInt(30)),
		field_("Active", makeBool(true)),
	)

	keys, ok := Keys(root, "")
	require.True(t, ok)
	assert.Equal(t, []string{"Name", "Age", "Active"}, keys)
}

// TestKeysPathEmptyStruct verifies an empty StructValue returns an empty slice, not nil.
func TestKeysPathEmptyStruct(t *testing.T) {
	root := makeStruct("Empty")

	keys, ok := Keys(root, "")
	require.True(t, ok)
	assert.Equal(t, []string{}, keys)
}

// TestKeysPathSliceValue verifies index strings are returned for a slice.
func TestKeysPathSliceValue(t *testing.T) {
	slice := makeSlice(makeString("a"), makeString("b"), makeString("c"))
	root := makeStruct("Root", field_("Items", slice))

	keys, ok := Keys(root, "Items")
	require.True(t, ok)
	assert.Equal(t, []string{"0", "1", "2"}, keys)
}

// TestKeysPathEmptySlice verifies an empty slice returns an empty slice, not nil.
func TestKeysPathEmptySlice(t *testing.T) {
	slice := makeSlice()
	root := makeStruct("Root", field_("Items", slice))

	keys, ok := Keys(root, "Items")
	require.True(t, ok)
	assert.Equal(t, []string{}, keys)
}

// TestKeysPathArrayValue verifies index strings are returned for an array.
func TestKeysPathArrayValue(t *testing.T) {
	arr := makeArray(makeInt(10), makeInt(20), makeInt(30))
	root := makeStruct("Root", field_("Arr", arr))

	keys, ok := Keys(root, "Arr")
	require.True(t, ok)
	assert.Equal(t, []string{"0", "1", "2"}, keys)
}

// TestKeysPathEmptyArray verifies an empty array returns an empty slice, not nil.
func TestKeysPathEmptyArray(t *testing.T) {
	arr := makeArray()
	root := makeStruct("Root", field_("Arr", arr))

	keys, ok := Keys(root, "Arr")
	require.True(t, ok)
	assert.Equal(t, []string{}, keys)
}

// TestKeysPathMapValue verifies that only StringValue keys are returned for a map.
func TestKeysPathMapValue(t *testing.T) {
	m := makeMap(
		entry(makeString("foo"), makeInt(1)),
		entry(makeString("bar"), makeInt(2)),
		entry(makeString("baz"), makeInt(3)),
	)
	root := makeStruct("Root", field_("Meta", m))

	keys, ok := Keys(root, "Meta")
	require.True(t, ok)
	assert.Equal(t, []string{"foo", "bar", "baz"}, keys)
}

// TestKeysPathMapNonStringKeys verifies that non-string map keys are excluded.
func TestKeysPathMapNonStringKeys(t *testing.T) {
	m := makeMap(
		entry(makeInt(1), makeString("one")),
		entry(makeString("two"), makeString("2")),
		entry(makeInt(3), makeString("three")),
	)

	keys, ok := Keys(m, "")
	require.True(t, ok)
	assert.Equal(t, []string{"two"}, keys)
}

// TestKeysPathMapAllNonStringKeys verifies a map with no string keys returns (nil, false).
func TestKeysPathMapAllNonStringKeys(t *testing.T) {
	m := makeMap(
		entry(makeInt(1), makeString("one")),
		entry(makeInt(2), makeString("two")),
	)

	keys, ok := Keys(m, "")
	assert.False(t, ok)
	assert.Nil(t, keys)
}

// TestKeysPathEmptyMap verifies an empty map returns an empty slice.
func TestKeysPathEmptyMap(t *testing.T) {
	m := makeMap()

	keys, ok := Keys(m, "")
	require.True(t, ok)
	assert.Equal(t, []string{}, keys)
}

// TestKeysPathInterfaceValueUnwrapped verifies InterfaceValue is unwrapped before dispatch.
func TestKeysPathInterfaceValueUnwrapped(t *testing.T) {
	inner := makeStruct("Inner",
		field_("X", makeInt(1)),
		field_("Y", makeInt(2)),
	)
	root := makeStruct("Root", field_("Child", wrapped("Inner", inner)))

	keys, ok := Keys(root, "Child")
	require.True(t, ok)
	assert.Equal(t, []string{"X", "Y"}, keys)
}

// TestKeysPathInterfaceValueAtRoot verifies unwrapping when root itself is wrapped.
func TestKeysPathInterfaceValueAtRoot(t *testing.T) {
	inner := makeStruct("Inner",
		field_("A", makeString("a")),
		field_("B", makeString("b")),
	)
	root := wrapped("Inner", inner)

	keys, ok := Keys(root, "")
	require.True(t, ok)
	assert.Equal(t, []string{"A", "B"}, keys)
}

// TestKeysPathScalarsReturnFalse verifies scalar node types return (nil, false).
func TestKeysPathScalarsReturnFalse(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			keys, ok := Keys(tt.value, "")
			assert.False(t, ok)
			assert.Nil(t, keys)
		})
	}
}

// TestKeysPathUnresolvedPathReturnsFalse verifies (nil, false) when the path does not resolve.
func TestKeysPathUnresolvedPathReturnsFalse(t *testing.T) {
	root := makeStruct("Root", field_("Name", makeString("hi")))

	keys, ok := Keys(root, "Missing")
	assert.False(t, ok)
	assert.Nil(t, keys)
}

// TestKeysPanicsOnInvalidSyntax verifies that Keys panics on a bad expression.
func TestKeysPanicsOnInvalidSyntax(t *testing.T) {
	root := makeStruct("Root")
	assert.Panics(t, func() {
		Keys(root, ".bad")
	})
	assert.Panics(t, func() {
		Keys(root, "Field.")
	})
}

// TestKeysPathNestedNavigation verifies Keys can navigate a multi-segment path
// and then return keys at the destination node.
func TestKeysPathNestedNavigation(t *testing.T) {
	customer := makeStruct("Customer",
		field_("Name", makeString("Alice")),
		field_("Email", makeString("alice@example.com")),
	)
	order := makeStruct("Order",
		field_("Customer", customer),
		field_("Total", makeInt(100)),
	)
	orders := makeSlice(order)
	root := makeStruct("Root", field_("Orders", orders))

	// Keys at Orders.0.Customer should return Customer's field names.
	keys, ok := Keys(root, "Orders.0.Customer")
	require.True(t, ok)
	assert.Equal(t, []string{"Name", "Email"}, keys)

	// Keys at Orders should return index strings.
	keys, ok = Keys(root, "Orders")
	require.True(t, ok)
	assert.Equal(t, []string{"0"}, keys)
}

// TestKeysPathPrecompiledPath verifies KeysPath works with a pre-compiled Path.
func TestKeysPathPrecompiledPath(t *testing.T) {
	root := makeStruct("Root",
		field_("A", makeString("a")),
		field_("B", makeString("b")),
	)

	p, err := Parse("")
	require.NoError(t, err)

	keys, ok := KeysPath(root, p)
	require.True(t, ok)
	assert.Equal(t, []string{"A", "B"}, keys)
}
