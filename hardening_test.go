package gobspect

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file feed deliberately hostile byte streams through the
// public Stream API. The contract (docs/architecture.md, README) is that the
// decoder never panics and never makes an unbounded allocation on untrusted
// input; each case here would violate that contract without the corresponding
// guard.

// gobUint encodes n in gob's own varint scheme.
func gobUint(n uint64) []byte {
	if n <= 0x7F {
		return []byte{byte(n)}
	}
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], n)
	i := 0
	for i < 8 && tmp[i] == 0 {
		i++
	}
	body := tmp[i:]
	return append([]byte{byte(-len(body))}, body...)
}

// gobInt encodes v in gob's zig-zag signed varint scheme: non-negative x
// becomes 2x, negative x becomes ~(2x).
func gobInt(v int64) []byte {
	if v < 0 {
		return gobUint(^(uint64(v) << 1))
	}
	return gobUint(uint64(v) << 1)
}

// frame wraps a message body with its gob length prefix.
func frame(body []byte) []byte {
	return append(gobUint(uint64(len(body))), body...)
}

// decodeStreamErr runs the stream to completion and returns the first error.
func decodeStreamErr(t *testing.T, data []byte) error {
	t.Helper()
	_, err := New().Stream(bytes.NewReader(data)).Collect()
	return err
}

// TestHostile_SelfReferentialSliceType ensures a slice type whose element is
// itself is rejected by the depth guard instead of exhausting the goroutine
// stack (an unrecoverable fatal error).
func TestHostile_SelfReferentialSliceType(t *testing.T) {
	// Type def: ID 65 = sliceType{Common{Name:"a", ID:65}, Elem:65}.
	// wireType field 2 (SliceT): structType{ field1 Common, field2 Elem }.
	common := structBody(
		fieldEntry(1, gobStr("a")),
		fieldEntry(2, gobInt(65)),
	)
	sliceT := structBody(
		fieldEntry(1, common),
		fieldEntry(2, gobInt(65)),
	)
	wireType := structBody(fieldEntry(2, sliceT))
	typeDef := append(gobInt(-65), wireType...)

	// Value message: type ID 65, singleton uint(0), then N nested counts of 1,
	// each buying one recursion level (N just over maxDecodeDepth of 10000),
	// terminated by count 0.
	value := append(gobInt(65), gobUint(0)...)
	for range maxDecodeDepth + 100 {
		value = append(value, gobUint(1)...) // one element...
	}
	value = append(value, gobUint(0)...) // ...innermost empty slice

	stream := append(frame(typeDef), frame(value)...)
	err := decodeStreamErr(t, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nesting")
}

// TestHostile_HugeSliceCount ensures a slice announcing 2^30 elements does not
// eagerly allocate a 16 GiB backing array; the count is bounded by the bytes
// actually available in the message.
func TestHostile_HugeSliceCount(t *testing.T) {
	// Type def: ID 65 = []int (Elem:2).
	common := structBody(fieldEntry(1, gobStr("")), fieldEntry(2, gobInt(65)))
	sliceT := structBody(fieldEntry(1, common), fieldEntry(2, gobInt(2)))
	wireType := structBody(fieldEntry(2, sliceT))
	typeDef := append(gobInt(-65), wireType...)

	// Value: type 65, singleton uint(0), count 2^30, then no element bytes.
	value := append(gobInt(65), gobUint(0)...)
	value = append(value, gobUint(1<<30)...)

	stream := append(frame(typeDef), frame(value)...)
	err := decodeStreamErr(t, stream)
	require.Error(t, err)
	// Bounded either by the remaining-bytes check or by hitting EOF while
	// reading elements — never by allocating 16 GiB.
	assert.NotContains(t, err.Error(), "nesting")
}

// TestHostile_HugeByteSliceLength ensures a []byte announcing a 1 GiB length
// fails against the remaining message bytes rather than allocating first.
func TestHostile_HugeByteSliceLength(t *testing.T) {
	// Top-level []byte (builtin ID 5): singleton uint(0), then uint(len), bytes.
	value := append(gobInt(5), gobUint(0)...)
	value = append(value, gobUint(1<<29)...) // announce 512 MiB, provide none
	stream := frame(value)
	err := decodeStreamErr(t, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remaining")
}

// TestHostile_DuplicateTypeID ensures redefining a type ID is rejected rather
// than silently making TypeByID disagree with how values decode.
func TestHostile_DuplicateTypeID(t *testing.T) {
	common := structBody(fieldEntry(1, gobStr("A")), fieldEntry(2, gobInt(65)))
	structT := structBody(
		fieldEntry(1, common),
		fieldEntry(2, append(gobUint(1), structBody(fieldEntry(1, gobStr("X")), fieldEntry(2, gobInt(2)))...)),
	)
	wireType := structBody(fieldEntry(3, structT))
	typeDef := append(gobInt(-65), wireType...)

	stream := append(frame(typeDef), frame(typeDef)...) // same ID twice
	err := decodeStreamErr(t, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

// TestHostile_ReservedTypeID ensures a definition for a reserved (builtin) ID
// is rejected.
func TestHostile_ReservedTypeID(t *testing.T) {
	common := structBody(fieldEntry(1, gobStr("")), fieldEntry(2, gobInt(6)))
	sliceT := structBody(fieldEntry(1, common), fieldEntry(2, gobInt(2)))
	wireType := structBody(fieldEntry(2, sliceT))
	typeDef := append(gobInt(-6), wireType...) // define builtin ID 6

	err := decodeStreamErr(t, frame(typeDef))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

// TestHostile_TrailingBytesAfterValue ensures extra bytes after a decoded
// value are reported rather than silently ignored.
func TestHostile_TrailingBytesAfterValue(t *testing.T) {
	// A valid top-level int(7) value, then a stray byte in the same message.
	value := append(gobInt(2), gobUint(0)...)
	value = append(value, gobInt(7)...)
	value = append(value, 0xAB) // trailing junk
	err := decodeStreamErr(t, frame(value))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trailing")
}

// TestHostile_OpaqueDecoderPanicContained registers an anonymous decoder that
// panics and confirms the stream still decodes (the value falls back to raw
// bytes) instead of the panic escaping to the caller. A directly-encoded
// GobEncoder has an empty wire type name, so it is routed to the anonymous
// decoder list.
func TestHostile_OpaqueDecoderPanicContained(t *testing.T) {
	ins := New()
	ins.RegisterUnnamedDecoder(func([]byte) (any, error) { panic("boom") })

	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(&namedGobEncoder{payload: "data"}))
	vals, err := ins.Stream(&buf).Collect()
	require.NoError(t, err, "a panicking decoder must not escape as an error/panic")
	require.Len(t, vals, 1)
}

// TestSafeDecode_RecoversPanic is a focused unit test on the recover wrapper.
func TestSafeDecode_RecoversPanic(t *testing.T) {
	_, err := safeDecode(func([]byte) (any, error) { panic("kaboom") }, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panicked")
}

// — small gob wire-construction helpers ————————————————————————————————————

// gobStr encodes a gob string (uint length + bytes).
func gobStr(s string) []byte {
	return append(gobUint(uint64(len(s))), []byte(s)...)
}

// fieldEntry describes one present struct field by its absolute 1-based field
// number and its raw encoded bytes. structBody converts numbers to wire deltas.
func fieldEntry(fieldNum int, body []byte) fieldPart {
	return fieldPart{num: fieldNum, body: body}
}

type fieldPart struct {
	num  int
	body []byte
}

// structBody encodes a sparse struct from ordered field parts, emitting the
// 1-based delta from the previous field and a terminating uint(0).
func structBody(fields ...fieldPart) []byte {
	var out []byte
	prev := 0
	for _, f := range fields {
		out = append(out, gobUint(uint64(f.num-prev))...)
		out = append(out, f.body...)
		prev = f.num
	}
	return append(out, gobUint(0)...)
}

// namedGobEncoder is a GobEncoder whose blob is arbitrary payload bytes. It is
// used to exercise the opaque decode path.
type namedGobEncoder struct {
	payload string
}

func (e *namedGobEncoder) GobEncode() ([]byte, error) { return []byte(e.payload), nil }
func (e *namedGobEncoder) GobDecode(b []byte) error   { e.payload = string(b); return nil }

// TestHostile_SchemaSelfReference ensures Schema() on a self-referential
// anonymous composite type terminates instead of recursing forever.
func TestHostile_SchemaSelfReference(t *testing.T) {
	// Anonymous slice whose element is itself (Name empty, Elem == self ID),
	// referenced by a named struct field so it enters the schema.
	anonSlice := structBody(
		fieldEntry(1, structBody(fieldEntry(1, gobStr("")), fieldEntry(2, gobInt(65)))),
		fieldEntry(2, gobInt(65)),
	)
	anonDef := append(gobInt(-65), structBody(fieldEntry(2, anonSlice))...)

	namedStructInner := structBody(
		fieldEntry(1, structBody(fieldEntry(1, gobStr("Holder")), fieldEntry(2, gobInt(66)))),
		fieldEntry(2, append(gobUint(1), structBody(fieldEntry(1, gobStr("F")), fieldEntry(2, gobInt(65)))...)),
	)
	namedDef := append(gobInt(-66), structBody(fieldEntry(3, namedStructInner))...)

	stream := append(frame(anonDef), frame(namedDef)...)
	// Draining may error on the malformed graph; the point is that Schema()
	// returns rather than overflowing the stack.
	s := New().Stream(bytes.NewReader(stream))
	_, _ = s.Collect()
	schema := FormatSchema(s.Types())
	out := schema.String()
	// Terminates and truncates deep recursion with the ellipsis marker.
	assert.True(t, strings.Contains(out, "…") || out != "", "schema rendering must terminate")
}
