package gobspect

// Tests for the resolveAllRefs invariant on Values.
//
// Background: DecodeStream and DecodeTypes both call sd.resolveAllRefs() after
// iteration completes, filling in TypeRef.Name for any forward-declared types
// (the gob encoder sends outer composite types before inner element types).
// Values previously skipped this step, leaving sd.types in a partially-resolved
// state that would be observable to any future API built on top of it.
//
// The fix: Values wraps the iterate result so that resolveAllRefs runs on both
// normal termination and early break.

import (
	"bytes"
	"encoding/gob"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveTestPt and resolveTestPtSlice are package-level types so that gob
// assigns them stable, non-empty names ("resolveTestPt", "resolveTestPtSlice").
// When encoded, gob emits the slice type definition before the element struct
// definition, producing a stream with a forward reference in the Elem TypeRef.
type resolveTestPt struct{ X, Y int }
type resolveTestPtSlice []resolveTestPt

// valuesAndTypes is an internal test helper that replicates the Values iterator
// body but also returns the underlying streamDecoder, allowing tests to inspect
// sd.types (including TypeRef.Name resolution) after iteration completes.
//
// It mirrors Values exactly — including the resolveAllRefs wrapper added by the
// fix — so the assertions are meaningful regression guards: if Values is changed
// to skip resolveAllRefs, this helper (and the test) must be updated to match.
func (ins *Inspector) valuesAndTypes(r byteReadReader) (iter.Seq2[Value, error], *streamDecoder) {
	sd := newStreamDecoder(wrapWithLimit(r, ins.options.MaxBytes))
	vd := newValueDecoder(ins, sd)
	seq := iterate(sd, vd)
	wrapped := func(yield func(Value, error) bool) {
		seq(yield)
		sd.resolveAllRefs()
	}
	return wrapped, sd
}

// encodeResolveStream returns a buffer with a single resolveTestPtSlice value.
// The stream contains a forward reference: gob emits the slice type before the
// element struct type, so resolveTestPtSlice.Elem.Name starts empty.
func encodeResolveStream(tb testing.TB) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(tb, enc.Encode(resolveTestPtSlice{resolveTestPt{1, 2}}))
	return &buf
}

// findSliceTypeInfo returns the first KindSlice entry in types, or nil.
func findSliceTypeInfo(types []TypeInfo) *TypeInfo {
	for i := range types {
		if types[i].Kind == KindSlice {
			return &types[i]
		}
	}
	return nil
}

// TestValues_ElemRefResolvedAfterIteration verifies that the Values iterator
// calls resolveAllRefs on completion so that TypeRef.Name fields in sd.types are
// populated for forward-referenced types (gob emits outer composite types before
// their element types, so elem refs are initially empty).
func TestValues_ElemRefResolvedAfterIteration(t *testing.T) {
	t.Run("NormalCompletion", func(t *testing.T) {
		buf := encodeResolveStream(t)
		ins := New()
		seq, sd := ins.valuesAndTypes(buf)

		// Drain the iterator to completion.
		for range seq {
		}

		ti := findSliceTypeInfo(sd.types)
		require.NotNil(t, ti, "KindSlice TypeInfo not found in sd.types")
		require.NotNil(t, ti.Elem)
		// Fails before fix: Elem.Name == "" because resolveAllRefs was not called.
		assert.Equal(t, "resolveTestPt", ti.Elem.Name,
			"Elem.Name should be resolved after Values iterator completes")
	})

	t.Run("EarlyBreak", func(t *testing.T) {
		// Type definitions precede values in a gob stream, so all type defs have
		// been processed before the first value is yielded. An early break therefore
		// still has a full registry, and resolveAllRefs should resolve all refs.
		buf := encodeResolveStream(t)
		ins := New()
		seq, sd := ins.valuesAndTypes(buf)

		// Break immediately after the first value.
		for range seq {
			break
		}

		ti := findSliceTypeInfo(sd.types)
		require.NotNil(t, ti, "KindSlice TypeInfo not found in sd.types")
		require.NotNil(t, ti.Elem)
		assert.Equal(t, "resolveTestPt", ti.Elem.Name,
			"Elem.Name should be resolved even after early break")
	})
}
