package query

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Interface-keyed maps (e.g. map[any]T) deliver their keys wrapped in
// InterfaceValue. Key matching must unwrap them, just like values.

func TestGetInterfaceWrappedMapKey(t *testing.T) {
	m := makeMap(
		entry(wrapped("string", makeString("a")), makeInt(1)),
		entry(wrapped("string", makeString("b")), makeInt(2)),
	)

	v, ok := Get(m, "a")
	require.True(t, ok)
	assert.Equal(t, makeInt(1), v)

	v, ok = Get(m, "b")
	require.True(t, ok)
	assert.Equal(t, makeInt(2), v)

	_, ok = Get(m, "c")
	assert.False(t, ok)
}

func TestKeysInterfaceWrappedMapKeys(t *testing.T) {
	m := makeMap(
		entry(wrapped("string", makeString("a")), makeInt(1)),
		entry(makeString("b"), makeInt(2)),            // plain key mixes fine
		entry(wrapped("int", makeInt(7)), makeInt(3)), // non-string key still skipped
	)

	keys, ok := Keys(m, "")
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, keys)
}

func TestFilterFieldInterfaceWrappedMapKey(t *testing.T) {
	items := makeSlice(
		makeMap(entry(wrapped("string", makeString("Status")), makeString("active"))),
		makeMap(entry(wrapped("string", makeString("Status")), makeString("inactive"))),
		makeMap(entry(wrapped("string", makeString("Other")), makeString("active"))),
	)

	// Glob filter: fieldAsString must find the wrapped "Status" key.
	got := All(items, "[Status=active]")
	require.Len(t, got, 1)

	// Existence filter: fieldPresent must find the wrapped key.
	got = All(items, "[Status!]")
	require.Len(t, got, 2)

	// Numeric filter path uses fieldValue.
	counts := makeSlice(
		makeMap(entry(wrapped("string", makeString("Count")), makeInt(5))),
		makeMap(entry(wrapped("string", makeString("Count")), makeInt(9))),
	)
	got = All(counts, "[Count>6]")
	require.Len(t, got, 1)
}

func TestContainsFilterInterfaceWrappedMapKeys(t *testing.T) {
	// [Tags~pattern] on a map field checks the map's KEYS; wrapped keys count.
	tags := makeMap(
		entry(wrapped("string", makeString("devops")), makeBool(true)),
		entry(wrapped("string", makeString("cloud")), makeBool(true)),
	)
	items := makeSlice(makeStruct("Item", field_("Tags", tags)))

	require.Len(t, All(items, "[Tags~devops]"), 1)
	require.Len(t, All(items, "[Tags~missing]"), 0)
	require.Len(t, All(items, "[Tags!~missing]"), 1)
	require.Len(t, All(items, "[Tags!~devops]"), 0)
}

// TestInterfaceKeyedMapDecodeIntegration goes through a real gob stream: a
// map[any]int with a string key decodes to InterfaceValue-wrapped keys, which
// must still be navigable.
func TestInterfaceKeyedMapDecodeIntegration(t *testing.T) {
	gob.Register("")
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(map[any]int{"a": 1}))

	var root gobspect.Value
	for v, err := range gobspect.New().Stream(&buf).Values() {
		require.NoError(t, err)
		root = v
	}
	mv, ok := root.(gobspect.MapValue)
	require.True(t, ok, "expected MapValue, got %T", root)
	require.Len(t, mv.Entries, 1)
	require.IsType(t, gobspect.InterfaceValue{}, mv.Entries[0].Key,
		"precondition: decoder wraps interface-typed keys")

	keys, ok := Keys(root, "")
	require.True(t, ok)
	assert.Equal(t, []string{"a"}, keys)

	v, ok := Get(root, "a")
	require.True(t, ok)
	assert.Equal(t, makeInt(1), v)
}
