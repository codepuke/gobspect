package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNamedDescentExhaustive covers "..Name" against every arrangement of a
// container that holds the named key, nests another one beneath it, or both.
func TestNamedDescentExhaustive(t *testing.T) {
	t.Run("map key matches at the root", func(t *testing.T) {
		// map[string]int{"Name": 42}
		root := makeMap(entry(makeString("Name"), makeInt(42)))
		got := All(root, "..Name")
		require.Len(t, got, 1)
		assert.Equal(t, makeInt(42), got[0])
	})

	t.Run("map key matches one level down", func(t *testing.T) {
		// map[string]map[string]int{"X": {"Name": 42}}
		inner := makeMap(entry(makeString("Name"), makeInt(42)))
		root := makeMap(entry(makeString("X"), inner))
		got := All(root, "..Name")
		require.Len(t, got, 1)
		assert.Equal(t, makeInt(42), got[0])
	})

	t.Run("nested maps both match", func(t *testing.T) {
		// map[string]map[string]int{"Name": {"Name": 42}}
		inner := makeMap(entry(makeString("Name"), makeInt(42)))
		root := makeMap(entry(makeString("Name"), inner))

		got := All(root, "..Name")
		require.Len(t, got, 2)
		assert.Equal(t, inner, got[0], "outer value first (pre-order)")
		assert.Equal(t, makeInt(42), got[1], "inner value second")
	})

	t.Run("struct field and its map key both match", func(t *testing.T) {
		// struct{ Name map[string]int }{Name: {"Name": 42}}
		m := makeMap(entry(makeString("Name"), makeInt(42)))
		root := makeStruct("S", makeField("Name", m))

		got := All(root, "..Name")
		require.Len(t, got, 2)
		assert.Equal(t, m, got[0], "map itself first")
		assert.Equal(t, makeInt(42), got[1], "map value second")
	})
}
