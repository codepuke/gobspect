package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamedDescentExhaustive(t *testing.T) {
	t.Run("Outer map with key Name pointing to a scalar", func(t *testing.T) {
		// m := map[string]int{"Name": 42}
		root := makeMap(entry(makeString("Name"), makeInt(42)))
		got := All(root, "..Name")
		require.Len(t, got, 1)
		assert.Equal(t, makeInt(42), got[0])
	})

	t.Run("Outer map with key X pointing to inner map with key Name", func(t *testing.T) {
		// m := map[string]map[string]int{"X": {"Name": 42}}
		inner := makeMap(entry(makeString("Name"), makeInt(42)))
		root := makeMap(entry(makeString("X"), inner))
		got := All(root, "..Name")
		require.Len(t, got, 1)
		assert.Equal(t, makeInt(42), got[0])
	})

	t.Run("Outer map with key Name pointing to inner map with key Name", func(t *testing.T) {
		// m := map[string]map[string]int{"Name": {"Name": 42}}
		inner := makeMap(entry(makeString("Name"), makeInt(42)))
		root := makeMap(entry(makeString("Name"), inner))
		
		// Expected: 
		// 1. stepField(root, "Name") finds 'inner' -> allWalk(inner, rest) returns [inner] (if rest is empty)
		//    Wait, the query is "..Name". so rest is empty.
		//    So it finds 'inner'.
		// 2. descendAll(root) returns [inner]. Recurse: allWalk(inner, "..Name")
		//    In allWalk(inner, "..Name"):
		//    a. stepField(inner, "Name") finds 42.
		//    b. descendAll(inner) returns [42]. Recurse: allWalk(42, "..Name") -> returns nil.
		// So results should be [inner, 42].
		
		got := All(root, "..Name")
		require.Len(t, got, 2)
		assert.Equal(t, inner, got[0], "outer value first (pre-order)")
		assert.Equal(t, makeInt(42), got[1], "inner value second")
	})

	t.Run("Struct with field Name of type map[string]int that has a key Name", func(t *testing.T) {
		// type S struct { Name map[string]int }
		// s := S{Name: map[string]int{"Name": 42}}
		m := makeMap(entry(makeString("Name"), makeInt(42)))
		root := makeStruct("S", field_("Name", m))

		// Expected:
		// 1. stepField(root, "Name") finds 'm'.
		// 2. descendAll(root) returns [m]. Recurse: allWalk(m, "..Name")
		//    In allWalk(m, "..Name"):
		//    a. stepField(m, "Name") finds 42.
		//    b. descendAll(m) returns [42]. Recurse: allWalk(42, "..Name") -> returns nil.
		// So results should be [m, 42].

		got := All(root, "..Name")
		require.Len(t, got, 2)
		assert.Equal(t, m, got[0], "map itself first")
		assert.Equal(t, makeInt(42), got[1], "map value second")
	})
}
