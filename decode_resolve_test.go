package gobspect

// Tests for incremental TypeRef name resolution.
//
// With the old resolveAllRefs approach, TypeRef.Name was filled in at
// end-of-stream. With registerAndResolve, names are back-filled inline as each
// new type definition is processed. The tests here verify:
//  1. When a forward reference exists (slice defined before its element struct),
//     Elem.Name starts empty and is back-filled as soon as the struct def arrives.
//  2. The observable stream-level invariant: by the time a value is yielded,
//     all TypeRefs in Types() are resolved.

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveTestPt and resolveTestPtSlice are package-level types so that gob
// assigns them stable, non-empty names. When encoded, gob emits the slice type
// definition before the element struct definition, producing a stream with a
// forward reference in the Elem TypeRef.
type resolveTestPt struct{ X, Y int }
type resolveTestPtSlice []resolveTestPt

func encodeResolveStream(tb testing.TB) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(tb, enc.Encode(resolveTestPtSlice{resolveTestPt{1, 2}}))
	return &buf
}

// TestStream_IncrementalNameResolution directly exercises the registerAndResolve
// helper by constructing artificial wireTypeDefs (no gob stream needed).
// It verifies that:
//   - Adding a slice of an unknown type leaves Elem.Name empty.
//   - Adding the element type back-fills Elem.Name in the slice TypeInfo.
func TestStream_IncrementalNameResolution(t *testing.T) {
	sd := newStreamDecoder(wrapWithLimit(strings.NewReader(""), 0))

	// Register type 65: a slice whose Elem (type 66) is not yet known.
	sliceDef := wireTypeDef{SliceT: &wireSliceType{
		Common: wireCommonType{Name: "[]TestPoint"},
		Elem:   66,
	}}
	require.NoError(t, sd.registerAndResolve(65, sliceDef))

	require.Len(t, sd.types, 1)
	require.NotNil(t, sd.types[0].Elem)
	assert.Equal(t, "", sd.types[0].Elem.Name, "Elem.Name should be empty before type 66 is defined")

	// Register type 66: the struct that type 65 references.
	structDef := wireTypeDef{StructT: &wireStructType{
		Common: wireCommonType{Name: "TestPoint"},
		Fields: []wireFieldType{{Name: "X", ID: 2}, {Name: "Y", ID: 2}},
	}}
	require.NoError(t, sd.registerAndResolve(66, structDef))

	require.Len(t, sd.types, 2)
	assert.Equal(t, "TestPoint", sd.types[0].Elem.Name,
		"Elem.Name should be back-filled after type 66 is registered")
}

// TestStream_ElemRefResolvedByValueYield verifies the stream-level invariant:
// when Values() yields a value, all TypeRef.Name fields in Types() are resolved.
func TestStream_ElemRefResolvedByValueYield(t *testing.T) {
	t.Run("NormalCompletion", func(t *testing.T) {
		buf := encodeResolveStream(t)
		ins := New()
		stream := ins.Stream(buf)

		for _, err := range stream.Values() {
			require.NoError(t, err)
			// Check resolution after the value is yielded.
			for _, ti := range stream.Types() {
				if ti.Elem != nil {
					assert.NotEmpty(t, ti.Elem.Name, "Elem.Name should be resolved when value is yielded")
				}
			}
		}
	})

	t.Run("EarlyBreak", func(t *testing.T) {
		buf := encodeResolveStream(t)
		ins := New()
		stream := ins.Stream(buf)

		for _, err := range stream.Values() {
			require.NoError(t, err)
			break
		}

		for _, ti := range stream.Types() {
			if ti.Elem != nil {
				assert.NotEmpty(t, ti.Elem.Name, "Elem.Name should be resolved even after early break")
			}
		}
	})
}
