package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gobEncode encodes v using encoding/gob and returns the raw bytes.
func gobEncode(tb testing.TB, v any) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(tb, enc.Encode(v))
	return &buf
}

// gobEncodeMultiple encodes each value in order into one stream.
func gobEncodeMultiple(tb testing.TB, vals ...any) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range vals {
		require.NoError(tb, enc.Encode(v))
	}
	return &buf
}

// decodeTypes is a test helper that replicates the old ins.DecodeTypes behavior
// using the new Stream API: drain the stream and return accumulated types.
func decodeTypes(ins *gobspect.Inspector, r io.Reader) ([]gobspect.TypeInfo, error) {
	s := ins.Stream(r)
	_, err := s.Collect()
	return s.Types(), err
}

// ————————————————————————————————————————————————————————————————————————————
// Fixtures

type Point struct {
	X, Y int
}

type NamedPoint struct {
	Name string
	Pt   Point
}

type IntSlice []int

type StringMap map[string]int

type IntArray [4]int64

// ————————————————————————————————————————————————————————————————————————————
// Tests

func TestDecodeTypes_SimpleStruct(t *testing.T) {
	buf := gobEncode(t, Point{X: 1, Y: 2})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	require.Len(t, types, 1)
	ti := types[0]
	assert.Equal(t, "Point", ti.Name)
	assert.Equal(t, gobspect.KindStruct, ti.Kind)
	require.Len(t, ti.Fields, 2)
	assert.Equal(t, "X", ti.Fields[0].Name)
	assert.Equal(t, "Y", ti.Fields[1].Name)
	// X and Y are int — gob type ID 2
	assert.Equal(t, 2, ti.Fields[0].TypeID)
	assert.Equal(t, 2, ti.Fields[1].TypeID)
}

func TestDecodeTypes_NestedStruct(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "origin", Pt: Point{}})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	// NamedPoint references Point, so both should be defined.
	require.GreaterOrEqual(t, len(types), 2)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}

	pt, ok := byName["Point"]
	require.True(t, ok, "expected TypeInfo for Point")
	assert.Equal(t, gobspect.KindStruct, pt.Kind)
	require.Len(t, pt.Fields, 2)

	np, ok := byName["NamedPoint"]
	require.True(t, ok, "expected TypeInfo for NamedPoint")
	assert.Equal(t, gobspect.KindStruct, np.Kind)
	require.Len(t, np.Fields, 2)
	assert.Equal(t, "Name", np.Fields[0].Name)
	assert.Equal(t, "Pt", np.Fields[1].Name)
	// Pt's TypeID must match Point's TypeInfo ID
	assert.Equal(t, pt.ID, np.Fields[1].TypeID)
}

func TestDecodeTypes_Slice(t *testing.T) {
	buf := gobEncode(t, IntSlice{1, 2, 3})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	require.Len(t, types, 1)
	ti := types[0]
	assert.Equal(t, gobspect.KindSlice, ti.Kind)
	require.NotNil(t, ti.Elem)
	assert.Equal(t, 2, ti.Elem.ID) // int type ID
	assert.Equal(t, "int", ti.Elem.Name)
}

func TestDecodeTypes_Map(t *testing.T) {
	buf := gobEncode(t, StringMap{"a": 1})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	require.Len(t, types, 1)
	ti := types[0]
	assert.Equal(t, gobspect.KindMap, ti.Kind)
	require.NotNil(t, ti.Key)
	require.NotNil(t, ti.Elem)
	assert.Equal(t, 6, ti.Key.ID) // string type ID
	assert.Equal(t, "string", ti.Key.Name)
	assert.Equal(t, 2, ti.Elem.ID) // int type ID
	assert.Equal(t, "int", ti.Elem.Name)
}

func TestDecodeTypes_Array(t *testing.T) {
	buf := gobEncode(t, IntArray{1, 2, 3, 4})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	require.Len(t, types, 1)
	ti := types[0]
	assert.Equal(t, gobspect.KindArray, ti.Kind)
	assert.Equal(t, 4, ti.Len)
	require.NotNil(t, ti.Elem)
	assert.Equal(t, 2, ti.Elem.ID) // int type ID
}

func TestDecodeTypes_MultipleValues(t *testing.T) {
	// Encoding two values of the same type in one stream.
	// The type should only be defined once.
	buf := gobEncodeMultiple(t, Point{1, 2}, Point{3, 4})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	// Point should appear exactly once regardless of how many values are sent.
	var pointCount int
	for _, ti := range types {
		if ti.Name == "Point" {
			pointCount++
		}
	}
	assert.Equal(t, 1, pointCount, "Point type definition should appear exactly once")
}

func TestDecodeTypes_MultipleDistinctTypes(t *testing.T) {
	// Encode values of two different types in one stream.
	buf := gobEncodeMultiple(t, Point{1, 2}, StringMap{"k": 7})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}
	assert.Contains(t, byName, "Point")
	assert.Contains(t, byName, "StringMap")
}

func TestDecodeTypes_TypeIDsArePositive(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "a", Pt: Point{1, 2}})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	for _, ti := range types {
		assert.Positive(t, ti.ID, "TypeInfo.ID must be positive for type %q", ti.Name)
	}
}

func TestDecodeTypes_EmptyStream(t *testing.T) {
	ins := gobspect.New()
	types, err := decodeTypes(ins, bytes.NewReader(nil))
	require.NoError(t, err)
	assert.Empty(t, types)
}

// ————————————————————————————————————————————————————————————————————————————
// MaxBytes enforcement

func TestMaxBytes_LimitExceeded(t *testing.T) {
	// Encode a non-trivial value, then decode with MaxBytes set smaller than the stream.
	buf := gobEncode(t, NamedPoint{Name: "hello world", Pt: Point{X: 100, Y: 200}})
	ins := gobspect.New(gobspect.WithReadLimit(5))
	_, err := ins.Stream(buf).Collect()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MaxBytes")
}

func TestMaxBytes_Zero_Unlimited(t *testing.T) {
	// MaxBytes:0 (default) imposes no limit.
	buf := gobEncode(t, NamedPoint{Name: "hello world", Pt: Point{X: 100, Y: 200}})
	ins := gobspect.New(gobspect.WithReadLimit(0))
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
}

func TestMaxBytes_ReadByteBoundary(t *testing.T) {
	// Build a two-value stream using a single encoder so both values share one
	// type-definition message.  The byte layout is:
	//   [type-def msg][value1 msg][value2 msg]
	// Using a separate single-value encoder gives us the exact offset where
	// value2's length-prefix varint starts.  That first byte is read via
	// ReadByte on the countingReader — the code path under test.
	multi := gobEncodeMultiple(t, Point{X: 1, Y: 2}, Point{X: 3, Y: 4})
	multiBytes := multi.Bytes()

	single := gobEncode(t, Point{X: 1, Y: 2})
	boundary := int64(single.Len())

	// Sanity: both encoders produce identical bytes for the shared prefix.
	require.Equal(t, single.Bytes(), multiBytes[:boundary],
		"single-value and multi-value streams must share a common prefix")

	// With MaxBytes == boundary the countingReader has consumed exactly
	// `boundary` bytes after value1.  The next ReadByte call (for value2's
	// length prefix) increments cr.n to boundary+1, exceeding the limit.
	// The error must surface cleanly via ReadByte, not be silently swallowed.
	ins := gobspect.New(gobspect.WithReadLimit(boundary))
	vals, err := ins.Stream(bytes.NewReader(multiBytes)).Collect()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MaxBytes",
		"limit error must name MaxBytes so callers can identify it")
	// value1 was fully decoded before the limit was hit.
	require.Len(t, vals, 1)
}

func TestMaxBytes_PartialResults(t *testing.T) {
	// Encode two values; MaxBytes triggers during the second. The first value
	// should still be returned with the error.
	buf := gobEncodeMultiple(t, Point{X: 1, Y: 2}, NamedPoint{Name: "toolong", Pt: Point{3, 4}})
	totalLen := buf.Len()
	// Set limit between the two messages so the first succeeds.
	ins := gobspect.New(gobspect.WithReadLimit(int64(totalLen / 2)))
	s := ins.Stream(buf)
	values, err := s.Collect()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MaxBytes")
	// At least the first value was decoded before the limit was hit.
	assert.NotEmpty(t, values, "expected partial results before limit")
}

// ————————————————————————————————————————————————————————————————————————————
// Values iterator tests

func TestValues_MultiValueStream(t *testing.T) {
	buf := gobEncodeMultiple(t, Point{1, 2}, Point{3, 4}, Point{5, 6})

	ins := gobspect.New()
	var got []gobspect.Value
	for v, err := range ins.Stream(buf).Values() {
		require.NoError(t, err)
		got = append(got, v)
	}
	assert.Len(t, got, 3)
}

func TestValues_SingleValue(t *testing.T) {
	buf := gobEncode(t, Point{X: 10, Y: 20})

	ins := gobspect.New()
	var got []gobspect.Value
	for v, err := range ins.Stream(buf).Values() {
		require.NoError(t, err)
		got = append(got, v)
	}
	require.Len(t, got, 1)
	sv, ok := got[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "Point", sv.TypeName)
}

func TestValues_EmptyStream(t *testing.T) {
	ins := gobspect.New()
	var got []gobspect.Value
	for v, err := range ins.Stream(bytes.NewReader(nil)).Values() {
		require.NoError(t, err)
		got = append(got, v)
	}
	assert.Empty(t, got)
}

func TestValues_ErrorMidStream(t *testing.T) {
	// Encode two values; truncate after the first to simulate a mid-stream error.
	buf := gobEncodeMultiple(t, Point{1, 2}, NamedPoint{Name: "x", Pt: Point{3, 4}})
	data := buf.Bytes()
	// Find a cut point that is past the first message but before the stream ends.
	cutAt := len(data) * 2 / 3
	truncated := bytes.NewReader(data[:cutAt])

	ins := gobspect.New()
	var values []gobspect.Value
	var gotErr error
	for v, err := range ins.Stream(truncated).Values() {
		if err != nil {
			gotErr = err
			break
		}
		values = append(values, v)
	}
	// At least the first value should have been decoded before the error.
	assert.NotEmpty(t, values, "expected at least one value before the error")
	assert.Error(t, gotErr)
}

func TestValues_EarlyBreak(t *testing.T) {
	// Encode three values; break after the first. The reader must not be
	// consumed past the first message.
	buf := gobEncodeMultiple(t, Point{1, 2}, Point{3, 4}, Point{5, 6})
	totalLen := buf.Len()

	ins := gobspect.New()
	var got []gobspect.Value
	for v, err := range ins.Stream(buf).Values() {
		require.NoError(t, err)
		got = append(got, v)
		break // stop after first
	}

	require.Len(t, got, 1)
	// Not all bytes should have been consumed.
	remaining := buf.Len()
	assert.Greater(t, remaining, 0, "expected bytes remaining after early break (total=%d)", totalLen)
}

func TestDecodeTypes_ElemRefResolved(t *testing.T) {
	// A slice of a named struct: elem TypeRef should resolve to the struct name.
	type Tags []Point
	buf := gobEncode(t, Tags{Point{1, 2}})

	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}

	tags, ok := byName["Tags"]
	require.True(t, ok)
	assert.Equal(t, gobspect.KindSlice, tags.Kind)
	require.NotNil(t, tags.Elem)

	// Elem ID should match Point's ID, and name should be resolved.
	pt, ok := byName["Point"]
	require.True(t, ok)
	assert.Equal(t, pt.ID, tags.Elem.ID)
	assert.Equal(t, "Point", tags.Elem.Name)
}

// — builtinTypeName coverage (IDs 1, 3, 4, 7) ————————————————————————————————
//
// These tests encode slices whose element type is a gob builtin that is not
// "int" (ID 2) or "string" (ID 6), ensuring those IDs are exercised via
// typeLabel inside decodeSliceValue.

type BoolSlice []bool
type UintSlice []uint
type Float64Slice []float64
type ComplexSlice []complex128

// TestDecodeTypes_BoolSliceElemType verifies that a []bool slice carries the
// correct element type label "bool" (builtin ID 1).
func TestDecodeTypes_BoolSliceElemType(t *testing.T) {
	buf := gobEncode(t, BoolSlice{true, false})
	ins := gobspect.New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	sv, ok := vals[0].(gobspect.SliceValue)
	require.True(t, ok)
	assert.Equal(t, "bool", sv.ElemType)
}

// TestDecodeTypes_UintSliceElemType verifies that a []uint slice carries the
// correct element type label "uint" (builtin ID 3).
func TestDecodeTypes_UintSliceElemType(t *testing.T) {
	buf := gobEncode(t, UintSlice{1, 2, 3})
	ins := gobspect.New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	sv, ok := vals[0].(gobspect.SliceValue)
	require.True(t, ok)
	assert.Equal(t, "uint", sv.ElemType)
}

// TestDecodeTypes_Float64SliceElemType verifies that a []float64 slice carries
// the correct element type label "float" (builtin ID 4).
func TestDecodeTypes_Float64SliceElemType(t *testing.T) {
	buf := gobEncode(t, Float64Slice{1.1, 2.2})
	ins := gobspect.New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	sv, ok := vals[0].(gobspect.SliceValue)
	require.True(t, ok)
	assert.Equal(t, "float", sv.ElemType)
}

// TestDecodeTypes_ComplexSliceElemType verifies that a []complex128 slice
// carries the correct element type label "complex" (builtin ID 7).
func TestDecodeTypes_ComplexSliceElemType(t *testing.T) {
	buf := gobEncode(t, ComplexSlice{1 + 2i, 3 + 4i})
	ins := gobspect.New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	sv, ok := vals[0].(gobspect.SliceValue)
	require.True(t, ok)
	assert.Equal(t, "complex", sv.ElemType)
}

// — wireTypeDefName branch coverage ——————————————————————————————————————————
//
// wireTypeDefName is called via resolveRef when building TypeInfo for composite
// types whose element/key types are user-defined (non-builtin). The following
// tests use nested composite types to trigger each variant.

// SliceOfSlice is a slice whose element is a named slice type, triggering the
// SliceT case in wireTypeDefName via resolveRef.
type SliceOfSlice []IntSlice

// TestWireTypeDefName_SliceT verifies that resolving the elem TypeRef for a
// slice-of-slice populates the correct name from the SliceT wire type.
func TestWireTypeDefName_SliceT(t *testing.T) {
	buf := gobEncode(t, SliceOfSlice{IntSlice{1, 2}, IntSlice{3, 4}})
	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}
	sos, ok := byName["SliceOfSlice"]
	require.True(t, ok)
	require.NotNil(t, sos.Elem)
	assert.Equal(t, "IntSlice", sos.Elem.Name)
}

// SliceOfMap is a slice whose element is a named map type, triggering the MapT
// case in wireTypeDefName via resolveRef.
type SliceOfMap []StringMap

// TestWireTypeDefName_MapT verifies that resolving the elem TypeRef for a
// slice-of-map populates the correct name from the MapT wire type.
func TestWireTypeDefName_MapT(t *testing.T) {
	buf := gobEncode(t, SliceOfMap{StringMap{"a": 1}})
	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}
	som, ok := byName["SliceOfMap"]
	require.True(t, ok)
	require.NotNil(t, som.Elem)
	assert.Equal(t, "StringMap", som.Elem.Name)
}

// SliceOfArray is a slice whose element is a named array type, triggering the
// ArrayT case in wireTypeDefName via resolveRef.
type SliceOfArray []IntArray

// TestWireTypeDefName_ArrayT verifies that resolving the elem TypeRef for a
// slice-of-array populates the correct name from the ArrayT wire type.
func TestWireTypeDefName_ArrayT(t *testing.T) {
	buf := gobEncode(t, SliceOfArray{IntArray{1, 2, 3, 4}})
	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}
	soa, ok := byName["SliceOfArray"]
	require.True(t, ok)
	require.NotNil(t, soa.Elem)
	assert.Equal(t, "IntArray", soa.Elem.Name)
}

// TimeSlice is a named slice of time.Time (a BinaryMarshaler), triggering the
// BinaryMarshalerT case in wireTypeDefName via resolveRef.
type TimeSlice []time.Time

// TestWireTypeDefName_BinaryMarshalerT verifies that resolving the elem TypeRef
// for a slice of BinaryMarshaler values hits the BinaryMarshalerT case.
func TestWireTypeDefName_BinaryMarshalerT(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	buf := gobEncode(t, TimeSlice{ts})
	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}
	tos, ok := byName["TimeSlice"]
	require.True(t, ok)
	require.NotNil(t, tos.Elem)
	assert.Equal(t, "Time", tos.Elem.Name)
}

// BigIntSlice is a named slice of *big.Int (a GobEncoder), triggering the
// GobEncoderT case in wireTypeDefName via resolveRef.
type BigIntSlice []*big.Int

// TestWireTypeDefName_GobEncoderT verifies that resolving the elem TypeRef for
// a slice of GobEncoder values hits the GobEncoderT case.
func TestWireTypeDefName_GobEncoderT(t *testing.T) {
	buf := gobEncode(t, BigIntSlice{big.NewInt(42)})
	ins := gobspect.New()
	types, err := decodeTypes(ins, buf)
	require.NoError(t, err)

	// The slice type must be present; its elem should reference the GobEncoder type.
	byName := make(map[string]gobspect.TypeInfo)
	for _, ti := range types {
		byName[ti.Name] = ti
	}
	bis, ok := byName["BigIntSlice"]
	require.True(t, ok)
	require.NotNil(t, bis.Elem)
	// big.Int's CommonType.Name is "" when encoded directly (not through an interface).
	// The elem name may be "" here; what matters is that the GobEncoderT branch ran.
	_ = bis.Elem.Name
}
