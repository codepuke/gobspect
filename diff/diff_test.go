package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/diff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiff_EqualValuesReturnNil(t *testing.T) {
	a := gobspect.StringValue{V: "x"}
	b := gobspect.StringValue{V: "x"}
	assert.Nil(t, diff.Diff(a, b))
}

func TestDiff_ChangedLeaf(t *testing.T) {
	d := diff.Diff(gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2})
	require.NotNil(t, d)
	c, ok := d.(diff.Changed)
	require.True(t, ok, "expected Changed, got %T", d)
	assert.Equal(t, gobspect.IntValue{V: 1}, c.Before)
	assert.Equal(t, gobspect.IntValue{V: 2}, c.After)
}

func TestDiff_Struct_AddedRemovedChangedFields(t *testing.T) {
	a := gobspect.StructValue{TypeName: "T", Fields: []gobspect.Field{
		{Name: "A", Value: gobspect.IntValue{V: 1}},
		{Name: "B", Value: gobspect.StringValue{V: "old"}},
		{Name: "C", Value: gobspect.IntValue{V: 42}}, // only in a
	}}
	b := gobspect.StructValue{TypeName: "T", Fields: []gobspect.Field{
		{Name: "A", Value: gobspect.IntValue{V: 1}},           // unchanged
		{Name: "B", Value: gobspect.StringValue{V: "new"}},    // changed
		{Name: "D", Value: gobspect.BoolValue{V: true}},       // only in b
	}}
	d := diff.Diff(a, b)
	require.NotNil(t, d)
	sd, ok := d.(diff.StructDelta)
	require.True(t, ok, "expected StructDelta, got %T", d)

	require.Len(t, sd.Fields, 3, "expected B changed, C removed, D added")
	// B first (from a's order), then C (removed), then D (added)
	assert.Equal(t, "B", sd.Fields[0].Name)
	_, bChanged := sd.Fields[0].Delta.(diff.Changed)
	assert.True(t, bChanged, "B should be Changed")

	assert.Equal(t, "C", sd.Fields[1].Name)
	_, cRemoved := sd.Fields[1].Delta.(diff.Removed)
	assert.True(t, cRemoved)

	assert.Equal(t, "D", sd.Fields[2].Name)
	_, dAdded := sd.Fields[2].Delta.(diff.Added)
	assert.True(t, dAdded)
}

func TestDiff_Map_ByKeyFormat(t *testing.T) {
	a := gobspect.MapValue{Entries: []gobspect.MapEntry{
		{Key: gobspect.StringValue{V: "a"}, Value: gobspect.IntValue{V: 1}},
		{Key: gobspect.StringValue{V: "b"}, Value: gobspect.IntValue{V: 2}},
	}}
	b := gobspect.MapValue{Entries: []gobspect.MapEntry{
		{Key: gobspect.StringValue{V: "b"}, Value: gobspect.IntValue{V: 3}}, // changed
		{Key: gobspect.StringValue{V: "c"}, Value: gobspect.IntValue{V: 9}}, // added; a is removed
	}}
	d := diff.Diff(a, b)
	require.NotNil(t, d)
	md, ok := d.(diff.MapDelta)
	require.True(t, ok, "expected MapDelta, got %T", d)
	assert.Len(t, md.Entries, 3)
}

func TestDiff_Slice_PositionAlign(t *testing.T) {
	a := gobspect.SliceValue{Elems: []gobspect.Value{
		gobspect.IntValue{V: 1},
		gobspect.IntValue{V: 2},
		gobspect.IntValue{V: 3},
	}}
	b := gobspect.SliceValue{Elems: []gobspect.Value{
		gobspect.IntValue{V: 1}, // unchanged
		gobspect.IntValue{V: 9}, // changed
		// position 2 removed; b has 2 elems
	}}
	d := diff.Diff(a, b)
	require.NotNil(t, d)
	sl, ok := d.(diff.SliceDelta)
	require.True(t, ok, "expected SliceDelta, got %T", d)
	require.Len(t, sl.Elems, 2)
	assert.Equal(t, 1, sl.Elems[0].Index)
	assert.Equal(t, 2, sl.Elems[1].Index)
	_, removed := sl.Elems[1].Delta.(diff.Removed)
	assert.True(t, removed, "trailing a[2] should be Removed")
}

func TestDiff_InterfaceUnwrap(t *testing.T) {
	a := gobspect.InterfaceValue{TypeName: "any", Value: gobspect.IntValue{V: 7}}
	b := gobspect.IntValue{V: 7}
	assert.Nil(t, diff.Diff(a, b), "interface wrapper should not affect equality")
}

func TestDiffStreams_IndexAligned(t *testing.T) {
	a := []gobspect.Value{
		gobspect.IntValue{V: 1},
		gobspect.IntValue{V: 2},
	}
	b := []gobspect.Value{
		gobspect.IntValue{V: 1}, // unchanged
		gobspect.IntValue{V: 3}, // changed
		gobspect.IntValue{V: 4}, // added
	}
	sd := diff.DiffStreams(a, b)
	require.Len(t, sd.Entries, 2)
	assert.Equal(t, 1, sd.Entries[0].Index)
	_, changed := sd.Entries[0].Delta.(diff.Changed)
	assert.True(t, changed)

	assert.Equal(t, 2, sd.Entries[1].Index)
	_, added := sd.Entries[1].Delta.(diff.Added)
	assert.True(t, added)
}

func TestFormat_StructDelta(t *testing.T) {
	a := gobspect.StructValue{TypeName: "Person", Fields: []gobspect.Field{
		{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
		{Name: "Age", Value: gobspect.IntValue{V: 30}},
	}}
	b := gobspect.StructValue{TypeName: "Person", Fields: []gobspect.Field{
		{Name: "Name", Value: gobspect.StringValue{V: "Alicia"}},
		{Name: "Age", Value: gobspect.IntValue{V: 31}},
	}}
	s := diff.Format(diff.Diff(a, b))
	assert.Contains(t, s, "Person {")
	assert.Contains(t, s, "Name:")
	assert.Contains(t, s, "Alice")
	assert.Contains(t, s, "Alicia")
}

func TestToJSON_Roundtrip(t *testing.T) {
	d := diff.Diff(gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2})
	out, err := diff.ToJSON(d)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"kind":"changed"`)
	assert.Contains(t, string(out), `"before"`)
	assert.Contains(t, string(out), `"after"`)
}

// TestFormatTo_WriterPath exercises the io.Writer shortcut.
func TestFormatTo_WriterPath(t *testing.T) {
	d := diff.Diff(gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2})
	var sb strings.Builder
	require.NoError(t, diff.FormatTo(&sb, d))
	assert.Contains(t, sb.String(), "- 1")
	assert.Contains(t, sb.String(), "+ 2")
}

// TestToJSONIndent verifies the pretty JSON form.
func TestToJSONIndent(t *testing.T) {
	d := diff.Diff(gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2})
	out, err := diff.ToJSONIndent(d, "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(out), "\n")
}

// TestStreamToJSON_Roundtrip covers both StreamToJSON variants.
func TestStreamToJSON_Roundtrip(t *testing.T) {
	sd := diff.DiffStreams(
		[]gobspect.Value{gobspect.IntValue{V: 1}},
		[]gobspect.Value{gobspect.IntValue{V: 2}},
	)
	compact, err := diff.StreamToJSON(sd)
	require.NoError(t, err)
	assert.Contains(t, string(compact), `"entries"`)
	assert.NotContains(t, string(compact), "\n")

	indented, err := diff.StreamToJSONIndent(sd, "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(indented), "\n")
}

// TestHasChanges covers the convenience predicate on Delta.
func TestHasChanges(t *testing.T) {
	assert.False(t, diff.HasChanges(nil))
	assert.True(t, diff.HasChanges(diff.Changed{Before: gobspect.IntValue{V: 1}, After: gobspect.IntValue{V: 2}}))

	sd := diff.DiffStreams(
		[]gobspect.Value{gobspect.IntValue{V: 1}},
		[]gobspect.Value{gobspect.IntValue{V: 1}},
	)
	assert.False(t, diff.StreamHasChanges(sd))
}

// TestFormatStream_OutputText verifies per-entry header rendering.
func TestFormatStream_OutputText(t *testing.T) {
	sd := diff.DiffStreams(
		[]gobspect.Value{gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}},
		[]gobspect.Value{gobspect.IntValue{V: 1}, gobspect.IntValue{V: 99}},
	)
	out := diff.FormatStream(sd)
	assert.Contains(t, out, "[1]")
	assert.Contains(t, out, "- 2")
	assert.Contains(t, out, "+ 99")

	// Empty StreamDelta returns empty string.
	empty := diff.DiffStreams(
		[]gobspect.Value{gobspect.IntValue{V: 1}},
		[]gobspect.Value{gobspect.IntValue{V: 1}},
	)
	assert.Empty(t, diff.FormatStream(empty))
}

// TestToJSON_SliceAndArrayDelta verifies JSON emission for the collection
// delta types (exercises elemDeltasToJSON).
func TestToJSON_SliceAndArrayDelta(t *testing.T) {
	sliceDelta := diff.Diff(
		gobspect.SliceValue{Elems: []gobspect.Value{gobspect.IntValue{V: 1}}},
		gobspect.SliceValue{Elems: []gobspect.Value{gobspect.IntValue{V: 2}}},
	)
	out, err := diff.ToJSON(sliceDelta)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"kind":"slice"`)
	assert.Contains(t, string(out), `"elems"`)

	arrDelta := diff.Diff(
		gobspect.ArrayValue{Len: 1, Elems: []gobspect.Value{gobspect.IntValue{V: 1}}},
		gobspect.ArrayValue{Len: 1, Elems: []gobspect.Value{gobspect.IntValue{V: 2}}},
	)
	out, err = diff.ToJSON(arrDelta)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"kind":"array"`)
}

// — Color output tests ————————————————————————————————————————————————————

// TestFormat_PlainIsUnchanged guards the existing text format against drift
// when the color layer is introduced. The output should be byte-identical to
// what the v0.2.0 formatter produced before color support.
func TestFormat_PlainIsUnchanged(t *testing.T) {
	a := gobspect.StructValue{TypeName: "Person", Fields: []gobspect.Field{
		{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
		{Name: "Age", Value: gobspect.IntValue{V: 30}},
	}}
	b := gobspect.StructValue{TypeName: "Person", Fields: []gobspect.Field{
		{Name: "Name", Value: gobspect.StringValue{V: "Alicia"}},
		{Name: "Age", Value: gobspect.IntValue{V: 31}},
	}}
	got := diff.Format(diff.Diff(a, b))
	want := "~ Person {\n" +
		"  Name:\n" +
		"    - \"Alice\"\n" +
		"    + \"Alicia\"\n" +
		"  Age:\n" +
		"    - 30\n" +
		"    + 31\n" +
		"}\n"
	assert.Equal(t, want, got)
	assert.NotContains(t, got, "\x1b[", "plain mode must not emit ANSI escapes")
}

// TestFormat_ColorWrapsLines verifies that additions, removals, and composite
// headers carry the ANSI wrapping when WithColor is set.
func TestFormat_ColorWrapsLines(t *testing.T) {
	a := gobspect.StructValue{TypeName: "Person", Fields: []gobspect.Field{
		{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
	}}
	b := gobspect.StructValue{TypeName: "Person", Fields: []gobspect.Field{
		{Name: "Name", Value: gobspect.StringValue{V: "Alicia"}},
	}}
	got := diff.Format(diff.Diff(a, b), diff.WithColor(diff.ANSIColorScheme))

	assert.Contains(t, got, "\x1b[31m- \"Alice\"\x1b[0m", "removal should be wrapped red")
	assert.Contains(t, got, "\x1b[32m+ \"Alicia\"\x1b[0m", "addition should be wrapped green")
	assert.Contains(t, got, "\x1b[1;36m~ Person {\x1b[0m", "struct header wrapped bold-cyan")
	assert.Contains(t, got, "\x1b[1;36m}\x1b[0m", "closing brace wrapped bold-cyan")
	// Field name line ("  Name:") is NOT in an added/removed role — keep it plain.
	assert.Contains(t, got, "\n  Name:\n", "structural field lines must not be color-wrapped")
}

// TestFormatStream_ColorIndexMarker covers the per-entry [N] position marker.
func TestFormatStream_ColorIndexMarker(t *testing.T) {
	sd := diff.DiffStreams(
		[]gobspect.Value{gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}},
		[]gobspect.Value{gobspect.IntValue{V: 1}, gobspect.IntValue{V: 99}},
	)
	got := diff.FormatStream(sd, diff.WithColor(diff.ANSIColorScheme))
	assert.Contains(t, got, "\x1b[2m[1]\x1b[0m", "stream index wrapped dim")
	assert.Contains(t, got, "\x1b[31m- 2\x1b[0m")
	assert.Contains(t, got, "\x1b[32m+ 99\x1b[0m")

	// No-op stream must stay empty even with color.
	empty := diff.DiffStreams(
		[]gobspect.Value{gobspect.IntValue{V: 1}},
		[]gobspect.Value{gobspect.IntValue{V: 1}},
	)
	assert.Empty(t, diff.FormatStream(empty, diff.WithColor(diff.ANSIColorScheme)))
}

// TestFormat_NoColorScheme_IsPlain confirms that explicitly passing NoColorScheme
// is equivalent to passing no option at all.
func TestFormat_NoColorScheme_IsPlain(t *testing.T) {
	d := diff.Diff(gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2})
	assert.Equal(t, diff.Format(d), diff.Format(d, diff.WithColor(diff.NoColorScheme)))
}

// — Realistic shape coverage ——————————————————————————————————————————————

// helper builders for nested shapes used by the tests below.

func strField(name, v string) gobspect.Field {
	return gobspect.Field{Name: name, Value: gobspect.StringValue{V: v}}
}
func intField(name string, v int64) gobspect.Field {
	return gobspect.Field{Name: name, Value: gobspect.IntValue{V: v}}
}
func structVal(typeName string, fields ...gobspect.Field) gobspect.StructValue {
	return gobspect.StructValue{TypeName: typeName, Fields: fields}
}

// TestDiff_Nested_Struct_In_Struct exercises multi-level descent. A change
// at each depth must produce a Changed leaf at exactly that depth and leave
// sibling branches out of the delta.
func TestDiff_Nested_Struct_In_Struct(t *testing.T) {
	makeOrder := func(name, street, city, zip string) gobspect.StructValue {
		return structVal("Order",
			gobspect.Field{Name: "Customer", Value: structVal("Person",
				strField("Name", name),
				gobspect.Field{Name: "Address", Value: structVal("Address",
					strField("Street", street),
					strField("City", city),
					strField("Zip", zip),
				)},
			)},
			strField("Status", "open"),
		)
	}
	a := makeOrder("Alice", "1 Oak St", "Springfield", "00001")
	b := makeOrder("Alicia", "1 Oak St", "Springfield", "00002") // Name + Zip changed

	d := diff.Diff(a, b)
	require.NotNil(t, d)
	root, ok := d.(diff.StructDelta)
	require.True(t, ok, "root should be StructDelta, got %T", d)
	require.Len(t, root.Fields, 1, "only Customer should differ; Status is untouched")
	assert.Equal(t, "Customer", root.Fields[0].Name)

	customer, ok := root.Fields[0].Delta.(diff.StructDelta)
	require.True(t, ok, "Customer should be StructDelta, got %T", root.Fields[0].Delta)
	require.Len(t, customer.Fields, 2, "Name and Address should differ")

	// Find the Name and Address deltas (order follows a's field order).
	var nameDelta, addrDelta diff.Delta
	for _, f := range customer.Fields {
		switch f.Name {
		case "Name":
			nameDelta = f.Delta
		case "Address":
			addrDelta = f.Delta
		}
	}
	_, nameChanged := nameDelta.(diff.Changed)
	assert.True(t, nameChanged, "Name should be Changed at depth 2")

	addrSD, ok := addrDelta.(diff.StructDelta)
	require.True(t, ok, "Address should be StructDelta at depth 2")
	require.Len(t, addrSD.Fields, 1, "only Zip should differ under Address")
	assert.Equal(t, "Zip", addrSD.Fields[0].Name)
	_, zipChanged := addrSD.Fields[0].Delta.(diff.Changed)
	assert.True(t, zipChanged, "Zip should be Changed at depth 3")
}

// TestDiff_Slice_Of_Structs_Positional covers the common case of a slice
// whose elements are structs. One changed, one added, one removed.
func TestDiff_Slice_Of_Structs_Positional(t *testing.T) {
	mk := func(sku string, qty int64) gobspect.Value {
		return structVal("LineItem", strField("SKU", sku), intField("Qty", qty))
	}
	a := gobspect.SliceValue{ElemType: "LineItem", Elems: []gobspect.Value{
		mk("A1", 1), mk("B2", 2), mk("C3", 3),
	}}
	b := gobspect.SliceValue{ElemType: "LineItem", Elems: []gobspect.Value{
		mk("A1", 1), mk("B2", 9), mk("C3", 3), mk("D4", 4),
	}}
	d := diff.Diff(a, b)
	require.NotNil(t, d)
	sd, ok := d.(diff.SliceDelta)
	require.True(t, ok, "slice of structs should produce SliceDelta")
	require.Len(t, sd.Elems, 2, "position 1 (Qty change) and position 3 (Added) differ")
	assert.Equal(t, 1, sd.Elems[0].Index)
	_, inner1 := sd.Elems[0].Delta.(diff.StructDelta)
	assert.True(t, inner1, "position 1 delta should descend as StructDelta, not Changed")
	assert.Equal(t, 3, sd.Elems[1].Index)
	_, added := sd.Elems[1].Delta.(diff.Added)
	assert.True(t, added, "position 3 should be Added")

	// And the reverse direction: shortening the slice yields a Removed.
	short := gobspect.SliceValue{Elems: []gobspect.Value{mk("A1", 1), mk("B2", 2)}}
	d2 := diff.Diff(a, short)
	sd2, ok := d2.(diff.SliceDelta)
	require.True(t, ok)
	require.Len(t, sd2.Elems, 1)
	_, removed := sd2.Elems[0].Delta.(diff.Removed)
	assert.True(t, removed, "shortening a slice should produce Removed at the tail")
}

// TestDiff_Map_Of_Structs_ByKey covers map-of-struct with adds/removes/changes.
func TestDiff_Map_Of_Structs_ByKey(t *testing.T) {
	mkEntry := func(k, v string) gobspect.MapEntry {
		return gobspect.MapEntry{Key: gobspect.StringValue{V: k}, Value: structVal("User", strField("Name", v))}
	}
	a := gobspect.MapValue{KeyType: "string", ElemType: "User", Entries: []gobspect.MapEntry{
		mkEntry("alice", "Alice"), // shared, changed below
		mkEntry("bob", "Bob"),     // only in a → Removed
	}}
	b := gobspect.MapValue{KeyType: "string", ElemType: "User", Entries: []gobspect.MapEntry{
		mkEntry("alice", "Alicia"),   // changed Name
		mkEntry("charlie", "Charlie"), // only in b → Added
	}}
	d := diff.Diff(a, b)
	require.NotNil(t, d)
	md, ok := d.(diff.MapDelta)
	require.True(t, ok, "map diff should produce MapDelta")
	require.Len(t, md.Entries, 3)

	byKey := map[string]diff.Delta{}
	for _, e := range md.Entries {
		k := gobspect.Format(e.Key)
		byKey[k] = e.Delta
	}

	aliceDelta, ok := byKey[`"alice"`].(diff.StructDelta)
	require.True(t, ok, "alice should be StructDelta (inner Name changed), not Changed")
	require.Len(t, aliceDelta.Fields, 1)
	assert.Equal(t, "Name", aliceDelta.Fields[0].Name)

	_, bobRemoved := byKey[`"bob"`].(diff.Removed)
	assert.True(t, bobRemoved, "bob should be Removed")
	_, charlieAdded := byKey[`"charlie"`].(diff.Added)
	assert.True(t, charlieAdded, "charlie should be Added")
}

// TestDiff_InterfaceWrapping_InsideComposites verifies that InterfaceValue is
// unwrapped transparently at every depth, not just the root.
func TestDiff_InterfaceWrapping_InsideComposites(t *testing.T) {
	wrap := func(s gobspect.StructValue) gobspect.Value {
		return gobspect.InterfaceValue{TypeName: "any", Value: s}
	}

	p1 := structVal("Payload", strField("Note", "one"))
	p2 := structVal("Payload", strField("Note", "two"))

	aWrapped := gobspect.SliceValue{ElemType: "any", Elems: []gobspect.Value{wrap(p1), wrap(p2)}}
	bUnwrapped := gobspect.SliceValue{ElemType: "any", Elems: []gobspect.Value{p1, p2}}

	// Same contents, just different interface-wrapping: must diff to nothing.
	assert.Nil(t, diff.Diff(aWrapped, bUnwrapped),
		"InterfaceValue wrapping must not register as a difference inside a slice")

	// Now modify one wrapped struct: the delta should point to the mutated slot.
	p2b := structVal("Payload", strField("Note", "three"))
	aMod := gobspect.SliceValue{ElemType: "any", Elems: []gobspect.Value{wrap(p1), wrap(p2b)}}
	d := diff.Diff(aWrapped, aMod)
	require.NotNil(t, d)
	sd, ok := d.(diff.SliceDelta)
	require.True(t, ok)
	require.Len(t, sd.Elems, 1)
	assert.Equal(t, 1, sd.Elems[0].Index)
	_, inner := sd.Elems[0].Delta.(diff.StructDelta)
	assert.True(t, inner, "wrapped inner struct change must surface as StructDelta")
}

// TestDiff_Heterogeneous_Kind_Same_Position verifies that a kind mismatch at
// one slice position surfaces as Changed, not as a composite descent.
func TestDiff_Heterogeneous_Kind_Same_Position(t *testing.T) {
	a := gobspect.SliceValue{Elems: []gobspect.Value{
		gobspect.IntValue{V: 1},
		gobspect.IntValue{V: 2},
	}}
	b := gobspect.SliceValue{Elems: []gobspect.Value{
		gobspect.IntValue{V: 1},
		gobspect.StringValue{V: "two"},
	}}
	d := diff.Diff(a, b)
	sd, ok := d.(diff.SliceDelta)
	require.True(t, ok)
	require.Len(t, sd.Elems, 1)
	c, ok := sd.Elems[0].Delta.(diff.Changed)
	require.True(t, ok, "kind mismatch should surface as Changed, got %T", sd.Elems[0].Delta)
	_, bIsString := c.After.(gobspect.StringValue)
	assert.True(t, bIsString)
}

// TestDiff_StructTypeName_Mismatch verifies that two structs that happen to
// share field shapes but have different TypeName are treated as fully
// Changed, not as an empty StructDelta.
func TestDiff_StructTypeName_Mismatch(t *testing.T) {
	a := structVal("A", intField("X", 1))
	b := structVal("B", intField("X", 1))
	d := diff.Diff(a, b)
	_, ok := d.(diff.Changed)
	assert.True(t, ok, "different TypeName must be Changed, got %T", d)
}

// TestDiff_Array_FixedLen_Equal_Elems confirms nil is returned for equal
// arrays — no spurious ArrayDelta with empty elem list.
func TestDiff_Array_FixedLen_Equal_Elems(t *testing.T) {
	a := gobspect.ArrayValue{Len: 3, Elems: []gobspect.Value{
		gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}, gobspect.IntValue{V: 3},
	}}
	b := gobspect.ArrayValue{Len: 3, Elems: []gobspect.Value{
		gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}, gobspect.IntValue{V: 3},
	}}
	assert.Nil(t, diff.Diff(a, b))
}

// TestDiff_Deep_Unchanged_Returns_Nil builds a multi-level tree and compares
// it with itself; the entire diff must collapse to nil.
func TestDiff_Deep_Unchanged_Returns_Nil(t *testing.T) {
	build := func() gobspect.StructValue {
		return structVal("Root",
			gobspect.Field{Name: "Items", Value: gobspect.SliceValue{Elems: []gobspect.Value{
				structVal("Item", strField("SKU", "A1"), intField("Qty", 2)),
				structVal("Item", strField("SKU", "B2"), intField("Qty", 7)),
			}}},
			gobspect.Field{Name: "Meta", Value: gobspect.MapValue{Entries: []gobspect.MapEntry{
				{Key: gobspect.StringValue{V: "env"}, Value: gobspect.StringValue{V: "prod"}},
				{Key: gobspect.StringValue{V: "region"}, Value: gobspect.StringValue{V: "us-east"}},
			}}},
			gobspect.Field{Name: "Payload", Value: gobspect.InterfaceValue{
				TypeName: "any", Value: structVal("Inner", intField("N", 99)),
			}},
		)
	}
	assert.Nil(t, diff.Diff(build(), build()),
		"deeply-equal tree must diff to nil, not an empty composite delta")
}

// TestDiff_NilValue_VsMissing_Vs_Scalar covers the nil/absent interactions
// that surface as Removed/Added (not as a fake Changed{nil,nil}).
func TestDiff_NilValue_VsMissing_Vs_Scalar(t *testing.T) {
	// 1. NilValue vs NilValue — equal.
	assert.Nil(t, diff.Diff(gobspect.NilValue{}, gobspect.NilValue{}))

	// 2. NilValue vs IntValue{0} — different kinds, must be Changed.
	c, ok := diff.Diff(gobspect.NilValue{}, gobspect.IntValue{V: 0}).(diff.Changed)
	require.True(t, ok, "nil vs int must be Changed")
	_, hasNil := c.Before.(gobspect.NilValue)
	assert.True(t, hasNil)

	// 3. Struct field present on one side only. The present side carries a
	// NilValue; absent side has no such field. Must be Removed, not Changed.
	a := structVal("T",
		gobspect.Field{Name: "F", Value: gobspect.NilValue{}},
		strField("G", "y"),
	)
	b := structVal("T", strField("G", "y"))
	d := diff.Diff(a, b)
	sd, ok := d.(diff.StructDelta)
	require.True(t, ok, "field-presence diff should yield StructDelta, got %T", d)
	require.Len(t, sd.Fields, 1)
	_, removed := sd.Fields[0].Delta.(diff.Removed)
	assert.True(t, removed, "field present only on a should be Removed, got %T", sd.Fields[0].Delta)
}

// TestDiff_StreamMixedTypes_Realistic exercises DiffStreams over a mix of
// real struct kinds, with interface-wrapped payloads.
func TestDiff_StreamMixedTypes_Realistic(t *testing.T) {
	wrap := func(inner gobspect.StructValue) gobspect.Value {
		return gobspect.InterfaceValue{TypeName: "any", Value: inner}
	}
	order := func(id int64, customer string) gobspect.Value {
		return structVal("Order",
			intField("ID", id),
			strField("Customer", customer),
			gobspect.Field{Name: "Items", Value: gobspect.SliceValue{Elems: []gobspect.Value{
				structVal("LineItem", strField("SKU", "A1"), intField("Qty", 1)),
			}}},
		)
	}
	event := func(name string, payload string) gobspect.Value {
		return structVal("Event",
			strField("Name", name),
			gobspect.Field{Name: "Payload", Value: wrap(structVal("StringPayload", strField("Value", payload)))},
		)
	}

	a := []gobspect.Value{
		order(1, "Alice"),
		event("login", "OK"),
		order(2, "Bob"),
	}
	b := []gobspect.Value{
		order(1, "Alice"),      // unchanged
		event("login", "DENY"), // payload changed
		order(2, "Bobby"),      // customer changed
	}
	sd := diff.DiffStreams(a, b)
	require.Len(t, sd.Entries, 2, "positions 1 and 2 differ")

	// Position 1 should descend through Event → Payload (interface) → inner struct.
	e1 := sd.Entries[0]
	assert.Equal(t, 1, e1.Index)
	evt, ok := e1.Delta.(diff.StructDelta)
	require.True(t, ok, "Event delta should be StructDelta")
	// exactly one changed field: Payload
	require.Len(t, evt.Fields, 1)
	assert.Equal(t, "Payload", evt.Fields[0].Name)
	// Inner diff is a StructDelta (inside the InterfaceValue wrapper).
	_, innerStruct := evt.Fields[0].Delta.(diff.StructDelta)
	assert.True(t, innerStruct, "interface-wrapped inner must surface as StructDelta")

	// Position 2: top-level Customer change inside Order.
	e2 := sd.Entries[1]
	assert.Equal(t, 2, e2.Index)
	ord, ok := e2.Delta.(diff.StructDelta)
	require.True(t, ok)
	require.Len(t, ord.Fields, 1)
	assert.Equal(t, "Customer", ord.Fields[0].Name)
}

// TestFormat_Nested_RenderingSnapshot pins the exact plain-text shape for a
// nested delta. If anyone later tweaks indent or prefixes, this snapshot will
// fail loudly — a good thing for external consumers of Format.
func TestFormat_Nested_RenderingSnapshot(t *testing.T) {
	a := structVal("Order",
		gobspect.Field{Name: "Customer", Value: structVal("Person",
			strField("Name", "Alice"),
		)},
	)
	b := structVal("Order",
		gobspect.Field{Name: "Customer", Value: structVal("Person",
			strField("Name", "Alicia"),
		)},
	)
	got := diff.Format(diff.Diff(a, b))
	want := "~ Order {\n" +
		"  Customer:\n" +
		"    ~ Person {\n" +
		"      Name:\n" +
		"        - \"Alice\"\n" +
		"        + \"Alicia\"\n" +
		"    }\n" +
		"}\n"
	assert.Equal(t, want, got)
}

// TestToJSON_Nested_RoundTripsThroughEncodingJSON verifies the external JSON
// contract survives a real round-trip through encoding/json.
func TestToJSON_Nested_RoundTripsThroughEncodingJSON(t *testing.T) {
	a := structVal("Order",
		gobspect.Field{Name: "Items", Value: gobspect.SliceValue{Elems: []gobspect.Value{
			structVal("Item", intField("Qty", 1)),
		}}},
	)
	b := structVal("Order",
		gobspect.Field{Name: "Items", Value: gobspect.SliceValue{Elems: []gobspect.Value{
			structVal("Item", intField("Qty", 2)),
		}}},
	)
	out, err := diff.ToJSONIndent(diff.Diff(a, b), "", "  ")
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	assert.Equal(t, "struct", parsed["kind"])
	fields := parsed["fields"].([]any)
	require.Len(t, fields, 1)
	itemField := fields[0].(map[string]any)
	assert.Equal(t, "Items", itemField["name"])
	itemDelta := itemField["delta"].(map[string]any)
	assert.Equal(t, "slice", itemDelta["kind"])
	elems := itemDelta["elems"].([]any)
	require.Len(t, elems, 1)
	first := elems[0].(map[string]any)
	assert.Equal(t, float64(0), first["index"])
	firstDelta := first["delta"].(map[string]any)
	assert.Equal(t, "struct", firstDelta["kind"])
}
