package gobspect_test

import (
	"bytes"
	"encoding"
	"encoding/gob"
	"testing"

	gobspect "github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ————————————————————————————————————————————————————————————————————————————
// Opaque fixtures

// gobEncoderType implements gob.GobEncoder / gob.GobDecoder.
type gobEncoderType struct{ S string }

func (g *gobEncoderType) GobEncode() ([]byte, error) { return []byte(g.S), nil }
func (g *gobEncoderType) GobDecode(b []byte) error   { g.S = string(b); return nil }

// binaryType implements encoding.BinaryMarshaler / BinaryUnmarshaler.
type binaryType struct{ S string }

func (b *binaryType) MarshalBinary() ([]byte, error) { return []byte(b.S), nil }
func (b *binaryType) UnmarshalBinary(d []byte) error { b.S = string(d); return nil }

// textType implements encoding.TextMarshaler / TextUnmarshaler.
type textType struct{ S string }

func (t *textType) MarshalText() ([]byte, error) { return []byte(t.S), nil }
func (t *textType) UnmarshalText(d []byte) error { t.S = string(d); return nil }

// Ensure interface satisfaction.
var _ gob.GobEncoder = (*gobEncoderType)(nil)
var _ encoding.BinaryMarshaler = (*binaryType)(nil)
var _ encoding.TextMarshaler = (*textType)(nil)

// ————————————————————————————————————————————————————————————————————————————
// Helpers

func decode(tb testing.TB, v any) []gobspect.Value {
	tb.Helper()
	buf := gobEncode(tb, v)
	ins := gobspect.New()
	vals, err := ins.Decode(buf)
	require.NoError(tb, err)
	require.Len(tb, vals, 1)
	return vals
}

// ————————————————————————————————————————————————————————————————————————————
// Builtin scalar types
//
// Gob only transmits user-defined named types or structs at the top level.
// For builtin scalars we wrap them in a single-field struct.

type WrapBool struct{ V bool }
type WrapInt struct{ V int }
type WrapUint struct{ V uint }
type WrapFloat struct{ V float64 }
type WrapComplex struct{ V complex128 }
type WrapString struct{ V string }
type WrapBytes struct{ V []byte }

func TestDecode_Bool(t *testing.T) {
	vals := decode(t, WrapBool{V: true})
	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	require.Len(t, sv.Fields, 1)
	bv, ok := sv.Fields[0].Value.(gobspect.BoolValue)
	require.True(t, ok)
	assert.True(t, bv.V)
}

func TestDecode_BoolFalse(t *testing.T) {
	// false is the zero value — gob omits it; Fields should be empty.
	vals := decode(t, WrapBool{V: false})
	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Empty(t, sv.Fields)
}

func TestDecode_Int(t *testing.T) {
	vals := decode(t, WrapInt{V: -42})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	iv := sv.Fields[0].Value.(gobspect.IntValue)
	assert.Equal(t, int64(-42), iv.V)
}

func TestDecode_Uint(t *testing.T) {
	vals := decode(t, WrapUint{V: 99})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	uv := sv.Fields[0].Value.(gobspect.UintValue)
	assert.Equal(t, uint64(99), uv.V)
}

func TestDecode_Float(t *testing.T) {
	vals := decode(t, WrapFloat{V: 3.14})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	fv := sv.Fields[0].Value.(gobspect.FloatValue)
	assert.InDelta(t, 3.14, fv.V, 1e-9)
}

func TestDecode_Complex(t *testing.T) {
	vals := decode(t, WrapComplex{V: 1 + 2i})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	cv := sv.Fields[0].Value.(gobspect.ComplexValue)
	assert.InDelta(t, 1.0, cv.Real, 1e-9)
	assert.InDelta(t, 2.0, cv.Imag, 1e-9)
}

func TestDecode_String(t *testing.T) {
	vals := decode(t, WrapString{V: "hello"})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	sv2 := sv.Fields[0].Value.(gobspect.StringValue)
	assert.Equal(t, "hello", sv2.V)
}

func TestDecode_Bytes(t *testing.T) {
	vals := decode(t, WrapBytes{V: []byte{1, 2, 3}})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	bv := sv.Fields[0].Value.(gobspect.BytesValue)
	assert.Equal(t, []byte{1, 2, 3}, bv.V)
}

// ————————————————————————————————————————————————————————————————————————————
// Struct values

func TestDecode_SimpleStruct(t *testing.T) {
	vals := decode(t, Point{X: 10, Y: 20})
	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "Point", sv.TypeName)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "X", sv.Fields[0].Name)
	assert.Equal(t, int64(10), sv.Fields[0].Value.(gobspect.IntValue).V)
	assert.Equal(t, "Y", sv.Fields[1].Name)
	assert.Equal(t, int64(20), sv.Fields[1].Value.(gobspect.IntValue).V)
}

func TestDecode_NestedStruct(t *testing.T) {
	vals := decode(t, NamedPoint{Name: "origin", Pt: Point{X: 1, Y: 2}})
	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "NamedPoint", sv.TypeName)
	require.Len(t, sv.Fields, 2)
	assert.Equal(t, "Name", sv.Fields[0].Name)
	assert.Equal(t, "origin", sv.Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Pt", sv.Fields[1].Name)
	inner, ok := sv.Fields[1].Value.(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "Point", inner.TypeName)
	assert.Equal(t, int64(1), inner.Fields[0].Value.(gobspect.IntValue).V)
	assert.Equal(t, int64(2), inner.Fields[1].Value.(gobspect.IntValue).V)
}

func TestDecode_StructZeroFields(t *testing.T) {
	// Zero-valued fields are omitted by gob.
	vals := decode(t, Point{X: 5, Y: 0})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1) // only X
	assert.Equal(t, "X", sv.Fields[0].Name)
}

// ————————————————————————————————————————————————————————————————————————————
// Slice values

func TestDecode_Slice(t *testing.T) {
	vals := decode(t, IntSlice{10, 20, 30})
	sv, ok := vals[0].(gobspect.SliceValue)
	require.True(t, ok)
	assert.Equal(t, "IntSlice", sv.TypeName)
	require.Len(t, sv.Elems, 3)
	assert.Equal(t, int64(10), sv.Elems[0].(gobspect.IntValue).V)
	assert.Equal(t, int64(20), sv.Elems[1].(gobspect.IntValue).V)
	assert.Equal(t, int64(30), sv.Elems[2].(gobspect.IntValue).V)
}

func TestDecode_SliceEmpty(t *testing.T) {
	vals := decode(t, IntSlice{})
	sv, ok := vals[0].(gobspect.SliceValue)
	require.True(t, ok)
	assert.Empty(t, sv.Elems)
}

// ————————————————————————————————————————————————————————————————————————————
// Map values

func TestDecode_Map(t *testing.T) {
	vals := decode(t, StringMap{"a": 1, "b": 2})
	mv, ok := vals[0].(gobspect.MapValue)
	require.True(t, ok)
	assert.Equal(t, "StringMap", mv.TypeName)
	assert.Equal(t, 2, len(mv.Entries))
	// Build a Go map from entries for order-independent assertion.
	got := make(map[string]int64, len(mv.Entries))
	for _, e := range mv.Entries {
		k := e.Key.(gobspect.StringValue).V
		v := e.Value.(gobspect.IntValue).V
		got[k] = v
	}
	assert.Equal(t, map[string]int64{"a": 1, "b": 2}, got)
}

// ————————————————————————————————————————————————————————————————————————————
// Array values

func TestDecode_Array(t *testing.T) {
	vals := decode(t, IntArray{1, 2, 3, 4})
	av, ok := vals[0].(gobspect.ArrayValue)
	require.True(t, ok)
	assert.Equal(t, "IntArray", av.TypeName)
	assert.Equal(t, 4, av.Len)
	require.Len(t, av.Elems, 4)
	assert.Equal(t, int64(1), av.Elems[0].(gobspect.IntValue).V)
	assert.Equal(t, int64(4), av.Elems[3].(gobspect.IntValue).V)
}

// ————————————————————————————————————————————————————————————————————————————
// Multiple values in one stream

func TestDecode_MultipleValues(t *testing.T) {
	buf := gobEncodeMultiple(t, Point{1, 2}, Point{3, 4})
	ins := gobspect.New()
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 2)
	assert.Equal(t, int64(1), vals[0].(gobspect.StructValue).Fields[0].Value.(gobspect.IntValue).V)
	assert.Equal(t, int64(3), vals[1].(gobspect.StructValue).Fields[0].Value.(gobspect.IntValue).V)
}

// ————————————————————————————————————————————————————————————————————————————
// Interface values

type HasInterface struct {
	V any
}

func init() {
	gob.Register(Point{})
}

func TestDecode_InterfaceNil(t *testing.T) {
	vals := decode(t, HasInterface{V: nil})
	sv := vals[0].(gobspect.StructValue)
	// nil interface → field is omitted by gob (zero value)
	assert.Empty(t, sv.Fields)
}

func TestDecode_InterfaceNonNil(t *testing.T) {
	vals := decode(t, HasInterface{V: Point{X: 7, Y: 8}})
	sv := vals[0].(gobspect.StructValue)
	require.Len(t, sv.Fields, 1)
	iv, ok := sv.Fields[0].Value.(gobspect.InterfaceValue)
	require.True(t, ok)
	assert.Equal(t, "github.com/codepuke/gobspect_test.Point", iv.TypeName)
	inner, ok := iv.Value.(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, int64(7), inner.Fields[0].Value.(gobspect.IntValue).V)
	assert.Equal(t, int64(8), inner.Fields[1].Value.(gobspect.IntValue).V)
}

// ————————————————————————————————————————————————————————————————————————————
// Opaque values

func TestDecode_GobEncoder(t *testing.T) {
	buf := gobEncode(t, &gobEncoderType{S: "hello"})
	ins := gobspect.New()
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(gobspect.OpaqueValue)
	require.True(t, ok)
	assert.Equal(t, "gob", ov.Encoding)
	assert.Equal(t, []byte("hello"), ov.Raw)
}

func TestDecode_BinaryMarshaler(t *testing.T) {
	buf := gobEncode(t, &binaryType{S: "world"})
	ins := gobspect.New()
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(gobspect.OpaqueValue)
	require.True(t, ok)
	assert.Equal(t, "binary", ov.Encoding)
	assert.Equal(t, []byte("world"), ov.Raw)
}

func TestDecode_TextMarshaler(t *testing.T) {
	// In Go 1.26+, TextMarshaler support is disabled in encoding/gob; types
	// that implement TextMarshaler are encoded as plain structs.
	buf := gobEncode(t, &textType{S: "text!"})
	ins := gobspect.New()
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok, "expected StructValue (TextMarshaler encoded as struct in Go 1.26+)")
	require.Len(t, sv.Fields, 1)
	assert.Equal(t, "S", sv.Fields[0].Name)
	assert.Equal(t, "text!", sv.Fields[0].Value.(gobspect.StringValue).V)
}

func TestDecode_OpaqueWithRegisteredDecoder(t *testing.T) {
	// When a GobEncoder is encoded directly (not via interface), gob sends an
	// empty CommonType.Name in the wireType. The OpaqueValue.TypeName is therefore
	// ""; register the decoder under that key.
	buf := gobEncode(t, &gobEncoderType{S: "decoded"})
	ins := gobspect.New()
	ins.RegisterDecoder("", func(data []byte) (any, error) {
		return "got:" + string(data), nil
	})
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov := vals[0].(gobspect.OpaqueValue)
	assert.Equal(t, "got:decoded", ov.Decoded)
}

// ————————————————————————————————————————————————————————————————————————————
// Empty stream

func TestDecode_EmptyStream(t *testing.T) {
	ins := gobspect.New()
	vals, err := ins.Decode(bytes.NewReader(nil))
	require.NoError(t, err)
	assert.Empty(t, vals)
}
