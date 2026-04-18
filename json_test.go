package gobspect_test

import (
	"encoding/json"
	"testing"
	"time"

	gobspect "github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeFirst encodes v with encoding/gob, decodes with a fresh Inspector,
// and returns the single resulting Value.
func decodeFirst(tb testing.TB, v any) gobspect.Value {
	tb.Helper()
	buf := gobEncode(tb, v)
	ins := gobspect.New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(tb, err)
	require.Len(tb, vals, 1)
	return vals[0]
}

// toJSONMap runs ToJSON and unmarshals into map[string]any.
func toJSONMap(tb testing.TB, v gobspect.Value) map[string]any {
	tb.Helper()
	b, err := gobspect.ToJSON(v)
	require.NoError(tb, err)
	var m map[string]any
	require.NoError(tb, json.Unmarshal(b, &m))
	return m
}

// — Kind discriminator tests ——————————————————————————————————————————————————

func TestToJSON_Kinds(t *testing.T) {
	tests := []struct {
		name      string
		value     gobspect.Value
		wantKind  string
		checkMore func(t *testing.T, m map[string]any)
	}{
		{
			name:     "bool_true",
			value:    gobspect.BoolValue{V: true},
			wantKind: "bool",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, true, m["v"])
			},
		},
		{
			name:     "bool_false",
			value:    gobspect.BoolValue{V: false},
			wantKind: "bool",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, false, m["v"])
			},
		},
		{
			name:     "int",
			value:    gobspect.IntValue{V: -123},
			wantKind: "int",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.EqualValues(t, -123, m["v"])
			},
		},
		{
			name:     "uint",
			value:    gobspect.UintValue{V: 456},
			wantKind: "uint",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.EqualValues(t, 456, m["v"])
			},
		},
		{
			name:     "float",
			value:    gobspect.FloatValue{V: 3.14},
			wantKind: "float",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.InDelta(t, 3.14, m["v"], 1e-9)
			},
		},
		{
			name:     "complex",
			value:    gobspect.ComplexValue{Real: 1.0, Imag: -2.0},
			wantKind: "complex",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.InDelta(t, 1.0, m["real"], 1e-9)
				assert.InDelta(t, -2.0, m["imag"], 1e-9)
			},
		},
		{
			name:     "string",
			value:    gobspect.StringValue{V: "hello"},
			wantKind: "string",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, "hello", m["v"])
			},
		},
		{
			name:     "bytes",
			value:    gobspect.BytesValue{V: []byte{0xde, 0xad}},
			wantKind: "bytes",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, "base64", m["encoding"])
				// base64 of 0xdead
				assert.Equal(t, "3q0=", m["v"])
			},
		},
		{
			name:     "nil",
			value:    gobspect.NilValue{},
			wantKind: "nil",
		},
		{
			name:     "interface",
			value:    gobspect.InterfaceValue{TypeName: "Foo", Value: gobspect.IntValue{V: 7}},
			wantKind: "interface",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, "Foo", m["typeName"])
				inner, ok := m["value"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "int", inner["kind"])
			},
		},
		{
			name:     "opaque_with_decoded",
			value:    gobspect.OpaqueValue{TypeName: "Tok", Encoding: "binary", Raw: []byte{0x01}, Decoded: "val"},
			wantKind: "opaque",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, "Tok", m["typeName"])
				assert.Equal(t, "binary", m["encoding"])
				assert.Equal(t, "AQ==", m["raw"]) // base64(0x01)
				assert.Equal(t, "val", m["decoded"])
			},
		},
		{
			name:     "opaque_no_decoded",
			value:    gobspect.OpaqueValue{TypeName: "Unknown", Encoding: "gob", Raw: []byte{0x02}},
			wantKind: "opaque",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Nil(t, m["decoded"])
			},
		},
		{
			name: "struct",
			value: gobspect.StructValue{
				TypeName:  "MyStruct",
				GobTypeID: 42,
				Fields: []gobspect.Field{
					{Name: "X", Value: gobspect.IntValue{V: 1}},
					{Name: "Y", Value: gobspect.StringValue{V: "hi"}},
				},
			},
			wantKind: "struct",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, "MyStruct", m["typeName"])
				assert.EqualValues(t, 42, m["typeId"])
				fields, ok := m["fields"].([]any)
				require.True(t, ok)
				require.Len(t, fields, 2)
				f0 := fields[0].(map[string]any)
				assert.Equal(t, "X", f0["name"])
				v0 := f0["value"].(map[string]any)
				assert.Equal(t, "int", v0["kind"])
			},
		},
		{
			name: "map",
			value: gobspect.MapValue{
				TypeName:  "M",
				GobTypeID: 5,
				KeyType:  "string",
				ElemType: "int",
				Entries: []gobspect.MapEntry{
					{Key: gobspect.StringValue{V: "a"}, Value: gobspect.IntValue{V: 1}},
				},
			},
			wantKind: "map",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.Equal(t, "string", m["keyType"])
				assert.Equal(t, "int", m["elemType"])
				entries, ok := m["entries"].([]any)
				require.True(t, ok)
				require.Len(t, entries, 1)
				e := entries[0].(map[string]any)
				k := e["key"].(map[string]any)
				assert.Equal(t, "string", k["kind"])
			},
		},
		{
			name: "slice",
			value: gobspect.SliceValue{
				TypeName:  "S",
				GobTypeID: 3,
				ElemType: "int",
				Elems:    []gobspect.Value{gobspect.IntValue{V: 10}, gobspect.IntValue{V: 20}},
			},
			wantKind: "slice",
			checkMore: func(t *testing.T, m map[string]any) {
				elems, ok := m["elems"].([]any)
				require.True(t, ok)
				assert.Len(t, elems, 2)
			},
		},
		{
			name: "array",
			value: gobspect.ArrayValue{
				TypeName:  "A",
				GobTypeID: 4,
				ElemType: "int",
				Len:      3,
				Elems:    []gobspect.Value{gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}, gobspect.IntValue{V: 3}},
			},
			wantKind: "array",
			checkMore: func(t *testing.T, m map[string]any) {
				assert.EqualValues(t, 3, m["len"])
				elems, ok := m["elems"].([]any)
				require.True(t, ok)
				assert.Len(t, elems, 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := toJSONMap(t, tt.value)
			assert.Equal(t, tt.wantKind, m["kind"])
			if tt.checkMore != nil {
				tt.checkMore(t, m)
			}
		})
	}
}

// — Round-trip test using real gob encoding ——————————————————————————————————

type jsonTestStruct struct {
	Name  string
	Score int
}

func TestToJSON_RealGobStruct(t *testing.T) {
	v := decodeFirst(t, jsonTestStruct{Name: "Alice", Score: 99})
	m := toJSONMap(t, v)

	assert.Equal(t, "struct", m["kind"])
	assert.Equal(t, "jsonTestStruct", m["typeName"])

	fields, ok := m["fields"].([]any)
	require.True(t, ok)
	require.Len(t, fields, 2)

	f0 := fields[0].(map[string]any)
	assert.Equal(t, "Name", f0["name"])
	nameVal := f0["value"].(map[string]any)
	assert.Equal(t, "string", nameVal["kind"])
	assert.Equal(t, "Alice", nameVal["v"])
}

// — OpaqueValue.Decoded normalization ————————————————————————————————————————

func TestToJSON_OpaqueDecodedNormalization(t *testing.T) {
	// time.Time produces a string decoded value — already JSON-safe.
	ts := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	v := decodeFirst(t, ts)
	m := toJSONMap(t, v)

	assert.Equal(t, "opaque", m["kind"])
	decoded, ok := m["decoded"].(string)
	require.True(t, ok)
	assert.Contains(t, decoded, "2024-01-15")
}

// TestToJSONIndent_Basic verifies ToJSONIndent produces indented JSON output.
func TestToJSONIndent_Basic(t *testing.T) {
	v := gobspect.StructValue{
		TypeName: "Point",
		Fields: []gobspect.Field{
			{Name: "X", Value: gobspect.IntValue{V: 3}},
			{Name: "Y", Value: gobspect.IntValue{V: 7}},
		},
	}
	b, err := gobspect.ToJSONIndent(v, "", "  ")
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, "\n")
	assert.Contains(t, s, `"X"`)
	assert.Contains(t, s, `"Y"`)
}

// TestToJSON_OpaqueDecodedNonJSONSafe verifies normalizeOpaqueDecoded converts
// a non-JSON-serializable Decoded value to its string representation.
// A channel value cannot be marshaled by encoding/json and triggers the fallback.
func TestToJSON_OpaqueDecodedNonJSONSafe(t *testing.T) {
	// Use a registered decoder that returns a non-JSON-serializable value (channel).
	ch := make(chan int)
	v := gobspect.OpaqueValue{
		TypeName: "chan",
		Encoding: "gob",
		Raw:      []byte{0x01},
		Decoded:  ch, // channels cannot be JSON-marshaled
	}
	b, err := gobspect.ToJSON(v)
	require.NoError(t, err)
	// The fallback converts the channel to its fmt.Sprint representation.
	assert.Contains(t, string(b), "decoded")
}
