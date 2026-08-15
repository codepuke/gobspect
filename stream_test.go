package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"io"
	"slices"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamPoint is package-level so gob assigns a stable name.
type streamPoint struct{ X, Y int }

// streamOrder is a second struct type used to test heterogeneous streams.
type streamOrder struct {
	ID    int
	Total float64
}

func encodeStream(tb testing.TB, vals ...any) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range vals {
		require.NoError(tb, enc.Encode(v))
	}
	return &buf
}

// closeTrackingReader wraps an io.Reader and records whether Close was called.
type closeTrackingReader struct {
	r      io.Reader
	closed bool
}

func (c *closeTrackingReader) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *closeTrackingReader) Close() error               { c.closed = true; return nil }

// TestStream_ValuesAndTypesInterleaved encodes two different struct types and
// verifies that, at each yielded value, Types() has grown and all TypeRef.Name
// fields are non-empty.
func TestStream_ValuesAndTypesInterleaved(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2}, streamOrder{ID: 7, Total: 9.99})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	prevLen := 0
	for v, err := range stream.Values() {
		require.NoError(t, err)
		_ = v
		types := stream.Types()
		assert.Greater(t, len(types), prevLen, "Types() should grow after each value")
		prevLen = len(types)

		for _, ti := range types {
			if ti.Elem != nil {
				assert.NotEmpty(t, ti.Elem.Name, "Elem.Name should be resolved by the time a value is yielded")
			}
			if ti.Key != nil {
				assert.NotEmpty(t, ti.Key.Name, "Key.Name should be resolved by the time a value is yielded")
			}
		}
	}
	assert.Greater(t, prevLen, 0, "at least one type must be defined")
}

// TestStream_TypesLiveGrows verifies that snapshots of Types() at three points
// during iteration are each a prefix of the next.
func TestStream_TypesLiveGrows(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2}, streamOrder{7, 9.99}, streamPoint{3, 4})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	var snapshots [][]gobspect.TypeInfo
	for _, err := range stream.Values() {
		require.NoError(t, err)
		snapshots = append(snapshots, slices.Clone(stream.Types()))
	}

	require.GreaterOrEqual(t, len(snapshots), 2, "need at least two snapshots to compare")
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]
		assert.GreaterOrEqual(t, len(curr), len(prev), "types slice should only grow")
		// Each earlier snapshot must be a prefix of the later one.
		assert.Equal(t, prev, curr[:len(prev)], "earlier snapshot must be a prefix of the later one")
	}
}

// TestStream_ValuesSingleUse verifies that calling Values() twice panics.
func TestStream_ValuesSingleUse(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2}, streamPoint{3, 4})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	// First call: fully consume.
	var count int
	for _, err := range stream.Values() {
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 2, count)

	// Second call must panic.
	assert.Panics(t, func() {
		for range stream.Values() {
		}
	}, "Values() on a consumed Stream must panic")
}

// TestStream_CollectAfterPartialValues verifies that Collect() after a partial
// Values() iteration returns only the remaining values.
func TestStream_CollectAfterPartialValues(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2}, streamPoint{3, 4}, streamPoint{5, 6})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	// Consume exactly 2 values then break.
	consumed := 0
	for _, err := range stream.Values() {
		require.NoError(t, err)
		consumed++
		if consumed == 2 {
			break
		}
	}
	require.Equal(t, 2, consumed)

	// Collect() should return only the third value.
	remaining, err := stream.Collect()
	require.NoError(t, err)
	require.Len(t, remaining, 1, "expected exactly 1 remaining value after consuming 2")
	sv, ok := remaining[0].(gobspect.StructValue)
	require.True(t, ok)
	require.Len(t, sv.Fields, 2)
	// The third point has X=5, Y=6.
	assert.Equal(t, gobspect.IntValue{V: 5}, sv.Fields[0].Value)
}

// TestStream_CollectEquivalentToOldDecode verifies that Collect() returns the
// same values as iterating Values() manually.
func TestStream_CollectEquivalentToOldDecode(t *testing.T) {
	buf1 := encodeStream(t, streamPoint{10, 20}, streamPoint{30, 40})
	buf2 := encodeStream(t, streamPoint{10, 20}, streamPoint{30, 40})

	ins := gobspect.New()

	// Collect.
	collected, err := ins.Stream(buf1).Collect()
	require.NoError(t, err)

	// Manual iteration.
	var manual []gobspect.Value
	for v, err := range ins.Stream(buf2).Values() {
		require.NoError(t, err)
		manual = append(manual, v)
	}

	require.Len(t, collected, 2)
	require.Len(t, manual, 2)
	// Both should produce identical StructValues.
	for i := range collected {
		c := collected[i].(gobspect.StructValue)
		m := manual[i].(gobspect.StructValue)
		assert.Equal(t, c.TypeName, m.TypeName)
		assert.Equal(t, c.Fields, m.Fields)
	}
}

// TestStream_SchemaDrainsAndDiscards verifies that Schema() produces a Schema
// containing all types and that Collect() afterward returns nothing.
func TestStream_SchemaDrainsAndDiscards(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2}, streamOrder{7, 9.99})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	schema, err := stream.Schema()
	require.NoError(t, err)
	require.NotNil(t, schema)
	assert.NotEmpty(t, schema.Types, "schema must contain type declarations")

	// After Schema(), stream is exhausted.
	remaining, err := stream.Collect()
	require.NoError(t, err)
	assert.Empty(t, remaining, "Collect() after Schema() must return nothing")
}

// TestStream_SchemaPartialOnError verifies that Schema() returns the partial
// schema alongside an error for a corrupted stream.
func TestStream_SchemaPartialOnError(t *testing.T) {
	// Encode some valid data, then append garbage.
	buf := encodeStream(t, streamPoint{1, 2})
	buf.Write([]byte{0x01, 0x02, 0x03, 0xff, 0xfe}) // corruption

	ins := gobspect.New()
	schema, err := ins.Stream(buf).Schema()
	assert.Error(t, err, "corrupted stream must produce an error")
	assert.NotNil(t, schema, "partial schema must be returned alongside error")
}

// TestStream_TypesNotMutated verifies that two calls to Types() from inside the
// iteration loop return the same backing array (live slice contract).
func TestStream_TypesNotMutated(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	for _, err := range stream.Values() {
		require.NoError(t, err)
		types1 := stream.Types()
		types2 := stream.Types()
		if len(types1) > 0 && len(types2) > 0 {
			// Same backing array: pointer to first element must be identical.
			assert.Equal(t, &types1[0], &types2[0], "Types() must return the same backing slice")
		}
	}
}

// TestStream_TypeByID verifies that TypeByID returns correct TypeInfo for known
// IDs and (zero, false) for unknown IDs.
func TestStream_TypeByID(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2})

	ins := gobspect.New()
	stream := ins.Stream(buf)

	// Before advancing: no types known yet.
	_, ok := stream.TypeByID(65535)
	assert.False(t, ok, "no types known before stream is advanced")

	// Drain the stream.
	_, err := stream.Collect()
	require.NoError(t, err)

	types := stream.Types()
	require.NotEmpty(t, types)

	// Every type in Types() should be findable by ID.
	for _, ti := range types {
		found, ok := stream.TypeByID(ti.ID)
		require.True(t, ok, "TypeByID(%d) must find TypeInfo for %q", ti.ID, ti.Name)
		assert.Equal(t, ti, found)
	}

	// An unknown ID should return (zero, false).
	_, ok = stream.TypeByID(0)
	assert.False(t, ok, "TypeByID(0) must return false for unknown ID")
}

// TestStream_ReaderNotClosed verifies that Stream does not close the underlying
// io.Reader even after the stream is fully drained.
func TestStream_ReaderNotClosed(t *testing.T) {
	buf := encodeStream(t, streamPoint{1, 2})
	tracker := &closeTrackingReader{r: buf}

	ins := gobspect.New()
	_, err := ins.Stream(tracker).Collect()
	require.NoError(t, err)

	assert.False(t, tracker.closed, "Stream must not close the underlying reader")
}

// — MessageInfo / Messages() tests —————————————————————————————————————————

// TestStream_MessagesCountsAndOffsets verifies that the Messages iterator
// yields one MessageInfo per wire message, with monotonically-increasing
// Index and Offset values and the raw body available.
func TestStream_MessagesCountsAndOffsets(t *testing.T) {
	buf := encodeStream(t,
		streamPoint{X: 1, Y: 2},
		streamPoint{X: 3, Y: 4},
		streamPoint{X: 5, Y: 6},
	)
	rawLen := buf.Len()

	ins := gobspect.New()
	s := ins.Stream(buf)

	var infos []gobspect.MessageInfo
	for m, err := range s.Messages() {
		require.NoError(t, err)
		infos = append(infos, m)
	}

	// Encoding three values of a brand-new type gives 4 messages: 1 type
	// definition + 3 values.
	require.Len(t, infos, 4)

	// Indices 0..3.
	for i, m := range infos {
		assert.Equal(t, i, m.Index, "Index should be monotonic")
		assert.NotEmpty(t, m.Body, "Body should never be empty")
	}

	// First message is a type definition (negative TypeID).
	assert.True(t, infos[0].IsTypeDef(), "expected first message to be a type definition, got type ID %d", infos[0].TypeID)
	// Remaining messages are values (positive TypeID).
	for _, m := range infos[1:] {
		assert.False(t, m.IsTypeDef())
		assert.Positive(t, m.TypeID)
	}

	// Offsets strictly increase across messages.
	for i := 1; i < len(infos); i++ {
		assert.Greater(t, infos[i].Offset, infos[i-1].Offset, "Offset should strictly increase")
	}

	// The total bytes consumed should not exceed the original buffer size.
	last := infos[len(infos)-1]
	assert.LessOrEqual(t, int(last.Offset)+last.BodyLen, rawLen+16)
}

// TestStream_MessagesSecondCallPanics verifies that Messages, like the other
// stream iterators, panics rather than silently yielding nothing when called a
// second time on an exhausted stream.
func TestStream_MessagesSecondCallPanics(t *testing.T) {
	buf := encodeStream(t, streamPoint{X: 1, Y: 2})

	ins := gobspect.New()
	s := ins.Stream(buf)

	for range s.Messages() {
		// drain
	}

	defer func() {
		r := recover()
		assert.NotNil(t, r, "second Messages call must panic")
	}()
	for range s.Messages() {
	}
}

// TestStream_ErrorMessageIncludesOffset verifies that value-level decode
// errors are wrapped with message index and byte offset context.
func TestStream_ErrorMessageIncludesOffset(t *testing.T) {
	// Encode a valid stream, then truncate it mid-value: dropping the final
	// byte of the last message body makes decodeTopLevelValue fail partway
	// through, which is exactly the case the offset context is meant to
	// describe.
	buf := encodeStream(t, streamPoint{X: 1, Y: 2})
	orig := buf.Bytes()
	truncated := bytes.NewReader(orig[:len(orig)-1])

	ins := gobspect.New()
	s := ins.Stream(truncated)
	var lastErr error
	for _, err := range s.Values() {
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	assert.Contains(t, lastErr.Error(), "message ")
	assert.Contains(t, lastErr.Error(), "offset ")
}

// — WithSkipCorruptValues / SkipCount tests ———————————————————————————————

// TestStream_SkipCorruptValues verifies that a corrupt value message is
// silently skipped when WithSkipCorruptValues is enabled, the skip count is
// incremented, and subsequent well-formed messages continue to decode.
func TestStream_SkipCorruptValues(t *testing.T) {
	// Encode three values. We'll splice a corrupt body into the middle one:
	// change a byte inside message 2's body to an invalid field delta that
	// will cause decodeStructValue to fail.
	buf := encodeStream(t,
		streamPoint{X: 1, Y: 2},
		streamPoint{X: 3, Y: 4},
		streamPoint{X: 5, Y: 6},
	)
	raw := buf.Bytes()

	// Walk framing to find message 2's body span. Framing-level Messages()
	// does not decode the value, so it succeeds on well-formed but
	// semantically-corrupt bodies too.
	var msgOffset int64
	var msgBodyLen int
	{
		ins := gobspect.New()
		s := ins.Stream(bytes.NewReader(raw))
		count := 0
		for m, err := range s.Messages() {
			require.NoError(t, err)
			if m.IsTypeDef() {
				continue
			}
			count++
			if count == 2 {
				msgOffset = m.Offset
				msgBodyLen = m.BodyLen
				break
			}
		}
	}
	require.Greater(t, msgBodyLen, 2)

	corrupted := make([]byte, len(raw))
	copy(corrupted, raw)
	// Length prefix for sub-128 bodies is a single byte; body starts at
	// msgOffset+1. The terminator is the last body byte. Flipping it to
	// a large non-zero value makes decodeStructValue see an out-of-range
	// field delta while leaving the framing intact.
	bodyStart := int(msgOffset) + 1
	lastBodyByte := bodyStart + msgBodyLen - 1
	require.Equal(t, byte(0), corrupted[lastBodyByte], "expected end-of-fields terminator at last body byte")
	corrupted[lastBodyByte] = 0x7F

	// With recovery OFF, decoding aborts mid-stream.
	{
		ins := gobspect.New()
		s := ins.Stream(bytes.NewReader(corrupted))
		_, err := s.Collect()
		assert.Error(t, err, "strict mode should surface an error")
		assert.Equal(t, 0, s.SkipCount())
	}

	// With recovery ON, the bad middle message is skipped; we still get
	// the first and third values.
	{
		ins := gobspect.New(gobspect.WithSkipCorruptValues(true))
		s := ins.Stream(bytes.NewReader(corrupted))
		vals, err := s.Collect()
		require.NoError(t, err)
		assert.Len(t, vals, 2, "expected two surviving values")
		assert.Equal(t, 1, s.SkipCount())
	}
}
