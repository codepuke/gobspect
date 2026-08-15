package query

import (
	"fmt"
	"slices"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — TestAllPathEmptyPath ——————————————————————————————————————————————————————

// TestAllPathEmptyPath verifies the empty path returns a slice containing root.
func TestAllPathEmptyPath(t *testing.T) {
	root := makeString("hello")
	p, err := Parse("")
	require.NoError(t, err)
	got := AllPath(root, p)
	assert.Equal(t, []gobspect.Value{root}, got)
}

// — TestAllPathWildcardOnSlice ————————————————————————————————————————————————

// TestAllPathWildcardOnSlice verifies * expands all elements of a SliceValue.
func TestAllPathWildcardOnSlice(t *testing.T) {
	slice := makeSlice(makeString("a"), makeString("b"), makeString("c"))
	root := makeStruct("Root", makeField("Items", slice))

	got := All(root, "Items.*")
	require.Len(t, got, 3)
	assert.Equal(t, makeString("a"), got[0])
	assert.Equal(t, makeString("b"), got[1])
	assert.Equal(t, makeString("c"), got[2])
}

// TestAllPathWildcardOnArray verifies * expands all elements of an ArrayValue.
func TestAllPathWildcardOnArray(t *testing.T) {
	arr := makeArray(makeInt(10), makeInt(20), makeInt(30))
	root := makeStruct("Root", makeField("Arr", arr))

	got := All(root, "Arr.*")
	require.Len(t, got, 3)
	assert.Equal(t, makeInt(10), got[0])
	assert.Equal(t, makeInt(20), got[1])
	assert.Equal(t, makeInt(30), got[2])
}

// TestAllPathWildcardOnMap verifies * expands all entry values of a MapValue.
func TestAllPathWildcardOnMap(t *testing.T) {
	m := makeMap(
		entry(makeString("x"), makeInt(1)),
		entry(makeString("y"), makeInt(2)),
	)
	root := makeStruct("Root", makeField("Meta", m))

	got := All(root, "Meta.*")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
}

// — TestAllPathNestedWildcard —————————————————————————————————————————————————

// TestAllPathNestedWildcard covers the PRD example: Orders.*.Customer.Name
func TestAllPathNestedWildcard(t *testing.T) {
	orders := makeSlice(
		makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Alice"))))),
		makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Bob"))))),
		makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Carol"))))),
	)
	root := makeStruct("Root", makeField("Orders", orders))

	got := All(root, "Orders.*.Customer.Name")
	require.Len(t, got, 3)
	assert.Equal(t, makeString("Alice"), got[0])
	assert.Equal(t, makeString("Bob"), got[1])
	assert.Equal(t, makeString("Carol"), got[2])
}

// TestAllPathMultiWildcard covers paths with two consecutive wildcards.
func TestAllPathMultiWildcard(t *testing.T) {
	// Matrix: slice of slices.
	inner0 := makeSlice(makeInt(1), makeInt(2))
	inner1 := makeSlice(makeInt(3), makeInt(4))
	outer := makeSlice(inner0, inner1)
	root := makeStruct("Root", makeField("Matrix", outer))

	got := All(root, "Matrix.*.*")
	require.Len(t, got, 4)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
	assert.Equal(t, makeInt(3), got[2])
	assert.Equal(t, makeInt(4), got[3])
}

// TestAllPathWildcardBeforeField covers wildcard followed by a field segment.
func TestAllPathWildcardBeforeField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(10))),
		makeStruct("Item", makeField("Price", makeInt(20))),
	)

	got := All(items, "*.Price")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(10), got[0])
	assert.Equal(t, makeInt(20), got[1])
}

// TestAllPathWildcardBeforeIndex covers wildcard followed by an index segment.
func TestAllPathWildcardBeforeIndex(t *testing.T) {
	// Slice of slices; we want the first element of each inner slice.
	slice := makeSlice(
		makeSlice(makeString("a0"), makeString("a1")),
		makeSlice(makeString("b0"), makeString("b1")),
	)

	got := All(slice, "*.0")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("a0"), got[0])
	assert.Equal(t, makeString("b0"), got[1])
}

// — TestAllPathInterfaceValueTransparency —————————————————————————————————————

// TestAllPathInterfaceValueTransparency verifies InterfaceValue is unwrapped
// before wildcard expansion.
func TestAllPathInterfaceValueTransparency(t *testing.T) {
	// Slice of interface-wrapped structs.
	slice := makeSlice(
		wrapped("Item", makeStruct("Item", makeField("Name", makeString("X")))),
		wrapped("Item", makeStruct("Item", makeField("Name", makeString("Y")))),
	)
	root := makeStruct("Root", makeField("Items", slice))

	got := All(root, "Items.*.Name")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("X"), got[0])
	assert.Equal(t, makeString("Y"), got[1])
}

// TestAllPathInterfaceValueAtRoot verifies unwrapping when root itself is an
// InterfaceValue wrapping a slice.
func TestAllPathInterfaceValueAtRoot(t *testing.T) {
	slice := makeSlice(makeInt(1), makeInt(2))
	root := wrapped("[]int", slice)

	got := All(root, "*")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
}

// — TestAllPathEmptyResult ————————————————————————————————————————————————————

// TestAllPathEmptyResultOnEmptySlice verifies nil is returned for an empty slice.
func TestAllPathEmptyResultOnEmptySlice(t *testing.T) {
	slice := makeSlice() // zero elements
	root := makeStruct("Root", makeField("Items", slice))

	got := All(root, "Items.*")
	assert.Nil(t, got)
}

// TestAllPathEmptyResultOnEmptyMap verifies nil is returned for an empty map.
func TestAllPathEmptyResultOnEmptyMap(t *testing.T) {
	m := makeMap() // zero entries
	root := makeStruct("Root", makeField("Meta", m))

	got := All(root, "Meta.*")
	assert.Nil(t, got)
}

// TestAllPathEmptyResultMissingField verifies nil is returned when a field
// doesn't exist on any expanded element.
func TestAllPathEmptyResultMissingField(t *testing.T) {
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(10))),
		makeStruct("Item", makeField("Price", makeInt(20))),
	)
	root := makeStruct("Root", makeField("Items", items))

	got := All(root, "Items.*.Missing")
	assert.Nil(t, got)
}

// TestAllPathEmptyResultMissingPath verifies nil is returned when path does
// not resolve at all.
func TestAllPathEmptyResultMissingPath(t *testing.T) {
	root := makeStruct("Root", makeField("Name", makeString("hi")))

	got := All(root, "Missing.*")
	assert.Nil(t, got)
}

// TestAllPathPartialMiss verifies only resolving elements are included in the
// result when some wildcard expansions do not resolve.
func TestAllPathPartialMiss(t *testing.T) {
	// Mix of structs: some have "Name", some don't.
	items := makeSlice(
		makeStruct("Item", makeField("Name", makeString("Alice"))),
		makeStruct("Item", makeField("Price", makeInt(42))), // no Name
		makeStruct("Item", makeField("Name", makeString("Bob"))),
	)
	root := makeStruct("Root", makeField("Items", items))

	got := All(root, "Items.*.Name")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("Alice"), got[0])
	assert.Equal(t, makeString("Bob"), got[1])
}

// — TestAllPathScalarWildcard —————————————————————————————————————————————————

// TestAllPathWildcardOnScalar verifies * on a scalar returns nil (not a panic).
func TestAllPathWildcardOnScalar(t *testing.T) {
	root := makeString("just a string")
	got := All(root, "*")
	assert.Nil(t, got)
}

// — TestAllPathDirectOnCollections ——————————————————————————————————————————

// TestAllPathDirectOnSlice verifies All works when root is a SliceValue.
func TestAllPathDirectOnSlice(t *testing.T) {
	slice := makeSlice(makeString("x"), makeString("y"))
	got := All(slice, "*")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("x"), got[0])
	assert.Equal(t, makeString("y"), got[1])
}

// TestAllPathDirectOnMap verifies All works when root is a MapValue.
func TestAllPathDirectOnMap(t *testing.T) {
	m := makeMap(
		entry(makeString("a"), makeInt(1)),
		entry(makeString("b"), makeInt(2)),
	)
	got := All(m, "*")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
}

// — TestAll (convenience wrapper) ——————————————————————————————————————————

// TestAllPanicsOnInvalidSyntax verifies All panics for syntactically invalid paths.
func TestAllPanicsOnInvalidSyntax(t *testing.T) {
	root := makeString("x")
	assert.Panics(t, func() {
		All(root, ".bad")
	})
	assert.Panics(t, func() {
		All(root, "Items.")
	})
}

// TestAllDoesNotPanicOnMissing verifies All returns nil (not a panic) for a
// valid path that does not resolve.
func TestAllDoesNotPanicOnMissing(t *testing.T) {
	root := makeStruct("Root", makeField("Name", makeString("hi")))
	assert.NotPanics(t, func() {
		got := All(root, "Missing.*")
		assert.Nil(t, got)
	})
}

// — TestAllPathPath (pre-compiled Path) ———————————————————————————————————

// TestAllPathPreCompiled verifies AllPath with a pre-compiled Path.
func TestAllPathPreCompiled(t *testing.T) {
	slice := makeSlice(makeInt(1), makeInt(2), makeInt(3))
	root := makeStruct("Root", makeField("Nums", slice))

	p, err := Parse("Nums.*")
	require.NoError(t, err)

	got := AllPath(root, p)
	require.Len(t, got, 3)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
	assert.Equal(t, makeInt(3), got[2])
}

// — TestAllPathComplexComposition —————————————————————————————————————————————

// TestAllPathFieldWildcardField covers the PRD example with field on both sides
// of the wildcard and a prior index.
func TestAllPathFieldWildcardField(t *testing.T) {
	// Orders.0.Items.*.Price
	items := makeSlice(
		makeStruct("Item", makeField("Price", makeInt(5))),
		makeStruct("Item", makeField("Price", makeInt(15))),
	)
	order0 := makeStruct("Order", makeField("Items", items))
	orders := makeSlice(order0)
	root := makeStruct("Root", makeField("Orders", orders))

	got := All(root, "Orders.0.Items.*.Price")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(5), got[0])
	assert.Equal(t, makeInt(15), got[1])
}

// TestAllPathNegativeIndexThenWildcard covers a negative index before a wildcard.
func TestAllPathNegativeIndexThenWildcard(t *testing.T) {
	inner := makeSlice(makeString("last0"), makeString("last1"))
	outer := makeSlice(
		makeSlice(makeString("first0"), makeString("first1")),
		inner,
	)
	root := makeStruct("Root", makeField("Matrix", outer))

	// Matrix.-1.* should expand the last inner slice.
	got := All(root, "Matrix.-1.*")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("last0"), got[0])
	assert.Equal(t, makeString("last1"), got[1])
}

// — TestAllPathDescent ————————————————————————————————————————————————————————

// TestAllDescentCollectsMultipleDepths verifies ..Name collects all occurrences
// across multiple depths in depth-first pre-order (shallowest first).
func TestAllDescentCollectsMultipleDepths(t *testing.T) {
	// root.Price = 1, root.A.Price = 2, root.A.B.Price = 3
	deepest := makeStruct("C", makeField("Price", makeInt(3)))
	mid := makeStruct("B", makeField("Price", makeInt(2)), makeField("B", deepest))
	root := makeStruct("Root",
		makeField("Price", makeInt(1)),
		makeField("A", mid),
	)

	got := All(root, "..Price")
	require.Len(t, got, 3)
	assert.Equal(t, makeInt(1), got[0], "shallowest first")
	assert.Equal(t, makeInt(2), got[1])
	assert.Equal(t, makeInt(3), got[2])
}

// TestAllDescentNoMatch verifies ..Name returns nil when field absent everywhere.
func TestAllDescentNoMatch(t *testing.T) {
	root := makeStruct("Root",
		makeField("A", makeStruct("A", makeField("B", makeInt(1)))),
	)
	got := All(root, "..Missing")
	assert.Nil(t, got)
}

// TestAllDescentInStructAndMap verifies ..Name finds name in both struct fields
// and nested map values.
func TestAllDescentInStructAndMap(t *testing.T) {
	// root.Price = 10
	// root.Meta (map) has entry "sub" → struct with Price = 20
	subStruct := makeStruct("Sub", makeField("Price", makeInt(20)))
	m := makeMap(entry(makeString("sub"), subStruct))
	root := makeStruct("Root",
		makeField("Price", makeInt(10)),
		makeField("Meta", m),
	)

	got := All(root, "..Price")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(10), got[0])
	assert.Equal(t, makeInt(20), got[1])
}

// TestAllDescentFollowedByFilter verifies ..Orders[Status=active] works.
func TestAllDescentFollowedByFilter(t *testing.T) {
	active := makeStruct("Order", makeField("Status", makeString("active")), makeField("ID", makeInt(1)))
	inactive := makeStruct("Order", makeField("Status", makeString("inactive")), makeField("ID", makeInt(2)))
	orders := makeSlice(active, inactive)
	root := makeStruct("Root", makeField("Orders", orders))

	// ..Orders[Status=active] — finds Orders slice, then filters it
	got := All(root, "..Orders[Status=active]")
	require.Len(t, got, 1)
	sv, ok := got[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, makeInt(1), func() gobspect.Value {
		for _, f := range sv.Fields {
			if f.Name == "ID" {
				return f.Value
			}
		}
		return nil
	}())
}

// TestAllDescentFollowedByWildcard verifies ..Items.* expands all items at any depth.
func TestAllDescentFollowedByWildcard(t *testing.T) {
	items1 := makeSlice(makeInt(1), makeInt(2))
	items2 := makeSlice(makeInt(3), makeInt(4))
	inner := makeStruct("Inner", makeField("Items", items2))
	root := makeStruct("Root",
		makeField("Items", items1),
		makeField("Sub", inner),
	)

	got := All(root, "..Items.*")
	require.Len(t, got, 4)
	assert.Equal(t, makeInt(1), got[0])
	assert.Equal(t, makeInt(2), got[1])
	assert.Equal(t, makeInt(3), got[2])
	assert.Equal(t, makeInt(4), got[3])
}

// TestAllDescentSequential verifies two sequential descents: ..A..B
func TestAllDescentSequential(t *testing.T) {
	// root.A.B = "x", root.A.Sub.B = "y"
	bVal1 := makeString("x")
	bVal2 := makeString("y")
	subA := makeStruct("SubA", makeField("B", bVal2))
	aStruct := makeStruct("A", makeField("B", bVal1), makeField("Sub", subA))
	root := makeStruct("Root", makeField("A", aStruct))

	got := All(root, "..A..B")
	require.Len(t, got, 2)
	assert.Equal(t, bVal1, got[0])
	assert.Equal(t, bVal2, got[1])
}

// TestAllDescentOnSliceRoot verifies ..Name on a SliceValue root finds Name in
// each element and their descendants.
func TestAllDescentOnSliceRoot(t *testing.T) {
	elem0 := makeStruct("Item", makeField("Price", makeInt(5)))
	elem1 := makeStruct("Item", makeField("Price", makeInt(10)))
	root := makeSlice(elem0, elem1)

	got := All(root, "..Price")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(5), got[0])
	assert.Equal(t, makeInt(10), got[1])
}

// TestAllDescentPreOrder verifies the depth-first pre-order: parent result
// appears before child result.
func TestAllDescentPreOrder(t *testing.T) {
	inner := makeStruct("Inner", makeField("X", makeInt(2)))
	root := makeStruct("Root",
		makeField("X", makeInt(1)),
		makeField("Inner", inner),
	)

	got := All(root, "..X")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), got[0], "parent X should come before child X")
	assert.Equal(t, makeInt(2), got[1])
}

// — TestAllWildcardDescent (..[Filter]) ——————————————————————————————————————

// TestWildcardDescentHeterogeneousTypes verifies ..[Status=active] collects
// matching elements across heterogeneous struct types at different depths.
func TestWildcardDescentHeterogeneousTypes(t *testing.T) {
	// root contains Server, Laptop, NetworkDevice sub-structs all with Status field.
	server := makeStruct("Server", makeField("Status", makeString("active")), makeField("Host", makeString("srv1")))
	laptop := makeStruct("Laptop", makeField("Status", makeString("inactive")), makeField("User", makeString("alice")))
	netdev := makeStruct("NetworkDevice", makeField("Status", makeString("active")), makeField("IP", makeString("10.0.0.1")))
	root := makeStruct("Root",
		makeField("Server", server),
		makeField("Laptop", laptop),
		makeField("NetworkDevice", netdev),
	)

	got := All(root, "..[Status=active]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("active"), MustGet(got[0], "Status"))
	assert.Equal(t, makeString("active"), MustGet(got[1], "Status"))
}

// TestWildcardDescentFieldAbsentOnSomeTypes verifies ..[Field=pattern] only
// matches types that have the named field; types without it are silently skipped.
func TestWildcardDescentFieldAbsentOnSomeTypes(t *testing.T) {
	withStatus := makeStruct("A", makeField("Status", makeString("active")), makeField("ID", makeInt(1)))
	withoutStatus := makeStruct("B", makeField("Other", makeString("x")), makeField("ID", makeInt(2)))
	alsoWithStatus := makeStruct("C", makeField("Status", makeString("active")), makeField("ID", makeInt(3)))
	root := makeStruct("Root",
		makeField("A", withStatus),
		makeField("B", withoutStatus),
		makeField("C", alsoWithStatus),
	)

	got := All(root, "..[Status=active]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestWildcardDescentNoMatchReturnsNil verifies ..[Field=pattern] returns nil
// when nothing in the tree matches.
func TestWildcardDescentNoMatchReturnsNil(t *testing.T) {
	root := makeStruct("Root",
		makeField("A", makeStruct("A", makeField("Status", makeString("inactive")))),
		makeField("B", makeStruct("B", makeField("Status", makeString("pending")))),
	)

	got := All(root, "..[Status=active]")
	assert.Nil(t, got)
}

// TestWildcardDescentExistFilter verifies ..[Field!] collects all elements that
// have the named field anywhere in the tree.
func TestWildcardDescentExistFilter(t *testing.T) {
	withName1 := makeStruct("X", makeField("Name", makeString("alpha")), makeField("Val", makeInt(1)))
	noName := makeStruct("Y", makeField("Val", makeInt(2)))
	withName2 := makeStruct("Z", makeField("Name", makeString("beta")), makeField("Val", makeInt(3)))
	root := makeStruct("Root",
		makeField("X", withName1),
		makeField("Y", noName),
		makeField("Z", withName2),
	)

	got := All(root, "..[Name!]")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("alpha"), MustGet(got[0], "Name"))
	assert.Equal(t, makeString("beta"), MustGet(got[1], "Name"))
}

// TestWildcardDescentContainsFilter verifies ..[Tags~pattern] collects structs
// where the Tags slice contains a matching element, across all depths.
func TestWildcardDescentContainsFilter(t *testing.T) {
	res1 := makeStruct("Resource",
		makeField("Tags", makeSlice(makeString("devops"), makeString("cloud"))),
		makeField("ID", makeInt(1)),
	)
	res2 := makeStruct("Resource",
		makeField("Tags", makeSlice(makeString("security"))),
		makeField("ID", makeInt(2)),
	)
	res3 := makeStruct("Resource",
		makeField("Tags", makeSlice(makeString("devops"), makeString("ci"))),
		makeField("ID", makeInt(3)),
	)
	root := makeStruct("Root",
		makeField("R1", res1),
		makeField("Sub", makeStruct("Sub",
			makeField("R2", res2),
			makeField("R3", res3),
		)),
	)

	got := All(root, "..[Tags~devops]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestWildcardDescentTwoChainedFilters verifies ..[Field=p][Field2~tag] applies
// both filters to every node in the tree.
func TestWildcardDescentTwoChainedFilters(t *testing.T) {
	match := makeStruct("Item",
		makeField("Status", makeString("active")),
		makeField("Tags", makeSlice(makeString("devops"))),
		makeField("ID", makeInt(1)),
	)
	noStatus := makeStruct("Item",
		makeField("Status", makeString("inactive")),
		makeField("Tags", makeSlice(makeString("devops"))),
		makeField("ID", makeInt(2)),
	)
	noTag := makeStruct("Item",
		makeField("Status", makeString("active")),
		makeField("Tags", makeSlice(makeString("security"))),
		makeField("ID", makeInt(3)),
	)
	root := makeStruct("Root",
		makeField("A", match),
		makeField("B", noStatus),
		makeField("C", noTag),
	)

	got := All(root, "..[Status=active][Tags~devops]")
	require.Len(t, got, 1)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
}

// TestWildcardDescentInterfaceWrapped verifies ..[Field=pattern] matches
// InterfaceValue-wrapped elements at any depth.
func TestWildcardDescentInterfaceWrapped(t *testing.T) {
	elem1 := wrapped("Item", makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(1))))
	elem2 := wrapped("Item", makeStruct("Item", makeField("Status", makeString("inactive")), makeField("ID", makeInt(2))))
	elem3 := wrapped("Item", makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(3))))
	root := makeSlice(elem1, elem2, elem3)

	got := All(root, "..[Status=active]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(got[1], "ID"))
}

// TestWildcardDescentFlatTree verifies ..[Status=active] on a flat (depth-1) tree
// produces the same results as Items[Status=active] (filter on the slice directly).
func TestWildcardDescentFlatTree(t *testing.T) {
	active1 := makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(1)))
	inactive := makeStruct("Item", makeField("Status", makeString("inactive")), makeField("ID", makeInt(2)))
	active2 := makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(3)))
	root := makeStruct("Root", makeField("Items", makeSlice(active1, inactive, active2)))

	// Items[Status=active] filters the slice elements directly (no descent).
	// Items..[Status=active] uses wildcard descent starting from Items (a slice):
	// it visits each element and tests it with [Status=active], so it should
	// produce the same two matching structs.
	direct := All(root, "Items[Status=active]")
	descent := All(root, "Items..[Status=active]")

	// Both paths must produce identical results.
	require.Len(t, descent, 2)
	assert.Equal(t, direct, descent)
	assert.Equal(t, makeInt(1), MustGet(descent[0], "ID"))
	assert.Equal(t, makeInt(3), MustGet(descent[1], "ID"))
}

// TestWildcardDescentDeepTree verifies ..[Status=active] collects matching
// elements from all levels of a tree with depth 3+.
func TestWildcardDescentDeepTree(t *testing.T) {
	// depth 1: root.A has Status=active
	// depth 2: root.A.Sub has Status=inactive
	// depth 3: root.A.Sub.Deep has Status=active
	// depth 1: root.B has no Status
	deep := makeStruct("Deep", makeField("Status", makeString("active")), makeField("Level", makeInt(3)))
	sub := makeStruct("Sub", makeField("Status", makeString("inactive")), makeField("Level", makeInt(2)), makeField("Deep", deep))
	a := makeStruct("A", makeField("Status", makeString("active")), makeField("Level", makeInt(1)), makeField("Sub", sub))
	b := makeStruct("B", makeField("Other", makeString("no status here")))
	root := makeStruct("Root",
		makeField("A", a),
		makeField("B", b),
	)

	got := All(root, "..[Status=active]")
	require.Len(t, got, 2)
	assert.Equal(t, makeInt(1), MustGet(got[0], "Level"), "depth-1 match first")
	assert.Equal(t, makeInt(3), MustGet(got[1], "Level"), "depth-3 match second")
}

// — descendAll edge case tests ————————————————————————————————————————————————

// TestDescendAll_EmptyStruct verifies that ..Name on an empty struct produces
// no results (descendAll returns nil for a StructValue with no Fields).
func TestDescendAll_EmptyStruct(t *testing.T) {
	root := makeStruct("Empty")
	got := All(root, "..Name")
	assert.Nil(t, got)
}

// TestDescendAll_EmptySlice verifies that ..Name on an empty slice produces
// no results (descendAll returns nil for a SliceValue with no Elems).
func TestDescendAll_EmptySlice(t *testing.T) {
	root := makeSlice()
	got := All(root, "..Name")
	assert.Nil(t, got)
}

// TestDescendAll_EmptyArray verifies that ..Name on an empty array produces
// no results (descendAll returns nil for an ArrayValue with no Elems).
func TestDescendAll_EmptyArray(t *testing.T) {
	root := makeArray()
	got := All(root, "..Name")
	assert.Nil(t, got)
}

// TestDescendAll_EmptyMap verifies that ..Name on an empty map produces no
// results (descendAll returns nil for a MapValue with no Entries).
func TestDescendAll_EmptyMap(t *testing.T) {
	root := makeMap()
	got := All(root, "..Name")
	assert.Nil(t, got)
}

// TestAllWalkStepIndexFails verifies allWalk returns nil when a segIndex segment
// is out of bounds (the `!ok` branch in allWalk's segIndex case).
func TestAllWalkStepIndexFails(t *testing.T) {
	// A path with index 99 on a 1-element array.
	arr := makeArray(makeInt(1))
	got := All(arr, "99")
	assert.Nil(t, got)
}

// TestAllWalkNilInterfaceInner verifies allWalk returns nil when it encounters
// an InterfaceValue whose inner Value is nil, rather than panicking.
func TestAllWalkNilInterfaceInner(t *testing.T) {
	// An InterfaceValue wrapping nil; unwrapInterface returns nil.
	nilWrapped := gobspect.InterfaceValue{TypeName: "T", Value: nil}
	root := makeStruct("Root", makeField("Child", nilWrapped))
	got := All(root, "Child.Field")
	assert.Nil(t, got)
}

// TestAllWalkDefaultSegmentKind verifies the default case in allWalk (unknown
// segment kind) returns nil without panicking.
func TestAllWalkDefaultSegmentKind(t *testing.T) {
	root := makeString("x")
	p := Path{segs: []segment{{kind: segKind(999)}}}
	got := AllPath(root, p)
	assert.Nil(t, got)
}

// TestApplyFiltersToNodeEmptySegs verifies applyFiltersToNode falls back to
// allWalk when segs is empty (first branch: len(segs)==0).
func TestApplyFiltersToNodeEmptySegs(t *testing.T) {
	// No expression reaches this branch — it needs ".." with an empty remainder,
	// which the parser rejects — so call the unexported function directly.
	result := applyFiltersToNode(makeString("x"), nil)
	assert.Equal(t, []gobspect.Value{makeString("x")}, result)
}

// TestApplyFiltersToNodeNonFilterFirstSeg verifies applyFiltersToNode falls back
// to allWalk when segs[0].kind != segFilter.
func TestApplyFiltersToNodeNonFilterFirstSeg(t *testing.T) {
	// segs starts with a segField, not a segFilter.
	segs := []segment{{kind: segField, name: "Name"}}
	root := makeStruct("Item", makeField("Name", makeString("hello")))
	result := applyFiltersToNode(root, segs)
	assert.Equal(t, []gobspect.Value{makeString("hello")}, result)
}

// TestDescendAllNilInterfaceInner verifies descendAll returns nil when passed an
// InterfaceValue whose inner Value is nil.
func TestDescendAllNilInterfaceInner(t *testing.T) {
	nilWrapped := gobspect.InterfaceValue{TypeName: "T", Value: nil}
	result := descendAll(nilWrapped)
	assert.Nil(t, result)
}

// TestDescendAll_NonEmptyArray verifies that descendAll on an ArrayValue with
// elements returns those elements, enabling recursive descent through arrays.
func TestDescendAll_NonEmptyArray(t *testing.T) {
	// An array of structs each with a "Name" field.
	arr := makeArray(
		makeStruct("X", makeField("Name", makeString("alice"))),
		makeStruct("X", makeField("Name", makeString("bob"))),
	)
	got := All(arr, "..Name")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("alice"), got[0])
	assert.Equal(t, makeString("bob"), got[1])
}

// TestApplyFiltersToNodeNonFilterFallback exercises the applyFiltersToNode
// fallback through a real expression: "..[Status=active].Name" consumes the
// leading filter, then hands the non-filter "Name" remainder to allWalk. The
// two matches sit at different depths so the fallback is reached more than once.
func TestApplyFiltersToNodeNonFilterFallback(t *testing.T) {
	match1 := makeStruct("Item",
		makeField("Status", makeString("active")),
		makeField("Name", makeString("one")),
	)
	match2 := makeStruct("Item",
		makeField("Status", makeString("active")),
		makeField("Name", makeString("two")),
	)
	root := makeStruct("Root",
		makeField("A", match1),
		makeField("B", makeStruct("B", makeField("Sub", match2))),
	)

	got := All(root, "..[Status=active].Name")
	require.Len(t, got, 2)
	assert.Equal(t, makeString("one"), got[0])
	assert.Equal(t, makeString("two"), got[1])
}

// TestDescendAll_MapValues verifies that ..Name descends into MapValue entry
// values and collects the named field from each entry's value-struct.
func TestDescendAll_MapValues(t *testing.T) {
	// A map whose values are structs that each have a "Price" field.
	m := makeMap(
		entry(makeString("a"), makeStruct("Item", makeField("Price", makeInt(10)))),
		entry(makeString("b"), makeStruct("Item", makeField("Price", makeInt(20)))),
		entry(makeString("c"), makeStruct("Item")), // no Price
	)
	got := All(m, "..Price")
	require.Len(t, got, 2)
	// Order matches Entries order.
	assert.Equal(t, makeInt(10), got[0])
	assert.Equal(t, makeInt(20), got[1])
}

// — TestAllPath projection ——————————————————————————————————————————————————

// TestAllProjectionFanOut verifies *.SKU,Price returns a projected StructValue
// per element in the collection.
func TestAllProjectionFanOut(t *testing.T) {
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

	got := All(items, "*.SKU,Price")
	require.Len(t, got, 2)

	for i, v := range got {
		sv, ok := v.(gobspect.StructValue)
		require.True(t, ok, "result %d should be StructValue", i)
		assert.Equal(t, ProjectionTypeName, sv.TypeName, "projected struct should have ProjectionTypeName")
		require.Len(t, sv.Fields, 2)
		assert.Equal(t, "SKU", sv.Fields[0].Name)
		assert.Equal(t, "Price", sv.Fields[1].Name)
	}

	assert.Equal(t, makeString("A"), got[0].(gobspect.StructValue).Fields[0].Value)
	assert.Equal(t, makeString("B"), got[1].(gobspect.StructValue).Fields[0].Value)
}

// TestAllProjectionPreservesOrder verifies that the field order in the
// projected result matches the query order, not the source struct order.
func TestAllProjectionPreservesOrder(t *testing.T) {
	root := makeStruct("Item",
		makeField("A", makeInt(1)),
		makeField("B", makeInt(2)),
		makeField("C", makeInt(3)),
	)

	// Query asks for C,A — reverse of the source order.
	got := All(root, "C,A")
	require.Len(t, got, 1)
	sv := got[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "C", sv.Fields[0].Name)
	assert.Equal(t, "A", sv.Fields[1].Name)
	assert.Equal(t, makeInt(3), sv.Fields[0].Value)
	assert.Equal(t, makeInt(1), sv.Fields[1].Value)
}

// TestAllProjectionOnScalar verifies projection on a scalar returns nil.
func TestAllProjectionOnScalar(t *testing.T) {
	got := All(makeString("hello"), "A,B")
	assert.Nil(t, got)
}

// TestAllNestedProjectionFanOut verifies that "*.Name,Address/Zip" over a slice
// returns one projected StructValue per element with the leaf as column name.
func TestAllNestedProjectionFanOut(t *testing.T) {
	items := makeSlice(
		makeStruct("Item",
			makeField("Name", makeString("Alpha")),
			makeField("Address", makeStruct("Address",
				makeField("Zip", makeString("97201")),
			)),
		),
		makeStruct("Item",
			makeField("Name", makeString("Beta")),
			makeField("Address", makeStruct("Address",
				makeField("Zip", makeString("10001")),
			)),
		),
	)

	got := All(items, "*.Name,Address/Zip")
	require.Len(t, got, 2)

	for i, want := range []struct{ name, zip string }{
		{"Alpha", "97201"},
		{"Beta", "10001"},
	} {
		sv := got[i].(gobspect.StructValue)
		assert.Equal(t, ProjectionTypeName, sv.TypeName)
		require.Len(t, sv.Fields, 2)
		assert.Equal(t, "Name", sv.Fields[0].Name)
		assert.Equal(t, makeString(want.name), sv.Fields[0].Value)
		assert.Equal(t, "Zip", sv.Fields[1].Name)
		assert.Equal(t, makeString(want.zip), sv.Fields[1].Value)
	}
}

// — TestAllPath_EmptyPathReturnsRoot —————————————————————————————————————————

// TestAllPath_EmptyPathReturnsRoot verifies that AllPath with a zero-segment
// path returns a single-element slice containing the root value, regardless of
// the root's type.
func TestAllPath_EmptyPathReturnsRoot(t *testing.T) {
	empty, err := Parse("")
	if err != nil {
		t.Fatalf("Parse(\"\") error: %v", err)
	}

	tests := []struct {
		name string
		root gobspect.Value
	}{
		{
			name: "scalar",
			root: gobspect.IntValue{V: 42},
		},
		{
			name: "struct",
			root: gobspect.StructValue{
				TypeName: "Foo",
				Fields: []gobspect.Field{
					{Name: "X", Value: gobspect.IntValue{V: 1}},
				},
			},
		},
		{
			name: "slice",
			root: gobspect.SliceValue{
				ElemType: "int",
				Elems:    []gobspect.Value{gobspect.IntValue{V: 1}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AllPath(tc.root, empty)
			assert.Equal(t, []gobspect.Value{tc.root}, got)
		})
	}
}

// — TestAllPathSeq / TestAllSeq iterators ————————————————————————————————————

// TestAllPathSeq_MatchesAllPath verifies that AllPathSeq produces the same
// results in the same order as AllPath for a representative set of cases.
func TestAllPathSeq_MatchesAllPath(t *testing.T) {
	tests := []struct {
		name string
		root gobspect.Value
		expr string
	}{
		{
			name: "empty path returns root",
			root: makeString("hello"),
			expr: "",
		},
		{
			name: "wildcard on slice",
			root: makeStruct("Root", makeField("Items", makeSlice(makeString("a"), makeString("b"), makeString("c")))),
			expr: "Items.*",
		},
		{
			name: "wildcard on array",
			root: makeStruct("Root", makeField("Arr", makeArray(makeInt(10), makeInt(20), makeInt(30)))),
			expr: "Arr.*",
		},
		{
			name: "wildcard on map",
			root: makeStruct("Root", makeField("Meta", makeMap(
				entry(makeString("x"), makeInt(1)),
				entry(makeString("y"), makeInt(2)),
			))),
			expr: "Meta.*",
		},
		{
			name: "nested wildcard",
			root: makeStruct("Root", makeField("Orders", makeSlice(
				makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Alice"))))),
				makeStruct("Order", makeField("Customer", makeStruct("Customer", makeField("Name", makeString("Bob"))))),
			))),
			expr: "Orders.*.Customer.Name",
		},
		{
			name: "multi wildcard matrix",
			root: makeStruct("Root", makeField("Matrix", makeSlice(
				makeSlice(makeInt(1), makeInt(2)),
				makeSlice(makeInt(3), makeInt(4)),
			))),
			expr: "Matrix.*.*",
		},
		{
			name: "descent named",
			root: makeStruct("Root",
				makeField("Price", makeInt(1)),
				makeField("A", makeStruct("B",
					makeField("Price", makeInt(2)),
					makeField("B", makeStruct("C", makeField("Price", makeInt(3)))),
				)),
			),
			expr: "..Price",
		},
		{
			name: "wildcard descent with filter",
			root: makeStruct("Root",
				makeField("A", makeStruct("A", makeField("Status", makeString("active")), makeField("ID", makeInt(1)))),
				makeField("B", makeStruct("B", makeField("Status", makeString("inactive")), makeField("ID", makeInt(2)))),
				makeField("C", makeStruct("C", makeField("Status", makeString("active")), makeField("ID", makeInt(3)))),
			),
			expr: "..[Status=active]",
		},
		{
			name: "filter on slice",
			root: makeStruct("Root", makeField("Items", makeSlice(
				makeStruct("Item", makeField("Status", makeString("active")), makeField("ID", makeInt(1))),
				makeStruct("Item", makeField("Status", makeString("inactive")), makeField("ID", makeInt(2))),
			))),
			expr: "Items[Status=active]",
		},
		{
			name: "projection fan-out",
			root: makeSlice(
				makeStruct("Item", makeField("SKU", makeString("A")), makeField("Price", makeInt(10)), makeField("Stock", makeInt(50))),
				makeStruct("Item", makeField("SKU", makeString("B")), makeField("Price", makeInt(20)), makeField("Stock", makeInt(30))),
			),
			expr: "*.SKU,Price",
		},
		{
			name: "missing field returns empty",
			root: makeStruct("Root", makeField("Name", makeString("hi"))),
			expr: "Missing.*",
		},
		{
			name: "partial miss",
			root: makeStruct("Root", makeField("Items", makeSlice(
				makeStruct("Item", makeField("Name", makeString("Alice"))),
				makeStruct("Item", makeField("Price", makeInt(42))),
				makeStruct("Item", makeField("Name", makeString("Bob"))),
			))),
			expr: "Items.*.Name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse(tc.expr)
			require.NoError(t, err)

			want := AllPath(tc.root, p)
			got := slices.Collect(AllPathSeq(tc.root, p))

			// Normalise nil vs empty-slice: AllPath returns nil on no match,
			// slices.Collect returns nil for an empty sequence too, so they
			// should both be nil here. But in case one returns []T{} vs nil,
			// treat both as "nothing".
			if len(want) == 0 && len(got) == 0 {
				return
			}
			assert.Equal(t, want, got)
		})
	}
}

// TestAllSeq_MatchesAll verifies AllSeq produces the same results as All.
func TestAllSeq_MatchesAll(t *testing.T) {
	root := makeStruct("Root", makeField("Items", makeSlice(
		makeString("a"), makeString("b"), makeString("c"),
	)))

	want := All(root, "Items.*")
	got := slices.Collect(AllSeq(root, "Items.*"))
	assert.Equal(t, want, got)
}

// TestAllSeq_PanicsOnInvalidSyntax verifies AllSeq panics for invalid paths,
// matching All's contract.
func TestAllSeq_PanicsOnInvalidSyntax(t *testing.T) {
	root := makeString("x")
	assert.Panics(t, func() {
		// Trigger the panic by ranging over the iterator.
		for range AllSeq(root, ".bad") {
		}
	})
}

// TestAllPathSeq_EarlyBreak verifies that early termination via yield=false
// stops traversal after the requested number of results, without visiting the
// remaining elements.
func TestAllPathSeq_EarlyBreak(t *testing.T) {
	// Build a slice with 10 elements so there are many potential matches.
	elems := make([]gobspect.Value, 10)
	for i := range elems {
		elems[i] = makeStruct(fmt.Sprintf("Item%d", i), makeField("ID", makeInt(int64(i))))
	}
	root := makeStruct("Root", makeField("Items", makeSlice(elems...)))

	p, err := Parse("Items.*")
	require.NoError(t, err)

	var collected []gobspect.Value
	for v := range AllPathSeq(root, p) {
		collected = append(collected, v)
		if len(collected) == 3 {
			break
		}
	}

	// Exactly 3 results were returned, not all 10.
	require.Len(t, collected, 3)
}

// — TestMustAll ———————————————————————————————————————————————————————————————

func TestMustAll_ReturnsMatches(t *testing.T) {
	root := makeStruct("Root", makeField("Items", makeSlice(makeString("a"), makeString("b"))))
	got := MustAll(root, "Items.*")
	require.Equal(t, []gobspect.Value{makeString("a"), makeString("b")}, got)
}

func TestMustAll_PanicsOnNoMatch(t *testing.T) {
	root := makeStruct("Root", makeField("Items", makeSlice(makeString("a"))))
	assert.Panics(t, func() { MustAll(root, "Missing.*") })
}

func TestMustAll_PanicsOnSyntaxError(t *testing.T) {
	root := makeString("x")
	assert.Panics(t, func() { MustAll(root, ".bad[") })
}
