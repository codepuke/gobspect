package gobspect

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecodeStructValue_LargeDeltaNoPanic ensures that a crafted gob message
// with an overflowing field delta (max uint64, which wraps to -1 when cast to
// int) returns an error rather than panicking with a negative slice index.
//
// Without the guard added to decodeStructValue, the flow is:
//
//	delta = ^uint64(0)  →  int(delta) = -1  →  fieldIdx = -1 + (-1) = -2
//	def.Fields[-2]  → runtime panic: index out of range
func TestDecodeStructValue_LargeDeltaNoPanic(t *testing.T) {
	// Gob varint encoding of ^uint64(0):
	//   first byte 0xF8  →  -int(int8(0xF8)) = 8  →  read 8 more bytes
	//   8 × 0xFF  →  value = 0xFFFFFFFFFFFFFFFF = ^uint64(0)
	overflowDelta := []byte{0xF8, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	def := &wireStructType{
		Common: wireCommonType{Name: "TestStruct"},
		Fields: []wireFieldType{
			{Name: "X", ID: 2}, // single int field
		},
	}

	ins := New()
	sd := newStreamDecoder(wrapWithLimit(bytes.NewReader(nil), 0))
	vd := newValueDecoder(ins, sd)
	mr := &messageReader{cur: bytes.NewReader(overflowDelta)}

	_, err := vd.decodeStructValue(65, def, mr)
	require.Error(t, err, "expected an error, not a panic")
	assert.Contains(t, err.Error(), "out of range")
}

// TestDecodeStructValue_DeltaEqualToFieldCount_Error ensures an oversized field
// delta is rejected on the uint64 delta itself, before any conversion to int.
func TestDecodeStructValue_DeltaEqualToFieldCount_Error(t *testing.T) {
	// Deltas run from fieldIdx=-1, so for a 2-field struct delta=2 lands on the
	// last valid index and delta=3 is one past the end.
	def := &wireStructType{
		Common: wireCommonType{Name: "TwoFields"},
		Fields: []wireFieldType{
			{Name: "A", ID: 2},
			{Name: "B", ID: 2},
		},
	}
	deltaBytes := []byte{3} // gob encodes uints below 128 as a single byte

	ins := New()
	sd := newStreamDecoder(wrapWithLimit(bytes.NewReader(nil), 0))
	vd := newValueDecoder(ins, sd)
	mr := &messageReader{cur: bytes.NewReader(deltaBytes)}

	_, err := vd.decodeStructValue(65, def, mr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}
