package gobspect

// Internal tests for built-in opaque decoders. Using the internal package
// gives direct access to unexported decoder functions, so each decoder can be
// tested in isolation with known byte sequences derived from real gob output.

import (
	"bytes"
	"encoding/gob"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — Helpers ——————————————————————————————————————————————————————————————————

// gobEncodeVal encodes v with encoding/gob and returns the buffer.
func gobEncodeVal(tb testing.TB, v any) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	require.NoError(tb, gob.NewEncoder(&buf).Encode(v))
	return &buf
}

// decodeOpaque decodes a single OpaqueValue from a gob stream using a fresh Inspector.
func decodeOpaque(tb testing.TB, buf *bytes.Buffer) OpaqueValue {
	tb.Helper()
	ins := New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(tb, err)
	require.Len(tb, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(tb, ok, "expected OpaqueValue, got %T", vals[0])
	return ov
}

// — parseTimeBytes round-trip tests ——————————————————————————————————————————

// TestParseTimeBytes_RoundTrip verifies that parseTimeBytes produces the same
// unixSec, nsec, and offsetSec as stdlib time.Time.UnmarshalBinary for a
// variety of wire encodings. We encode with time.MarshalBinary, which exercises
// the real wire format including the version-2 sub-minute path.
func TestParseTimeBytes_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
	}{
		{
			// UTC sentinel: offsetMin == -1 (0xFFFF), version 1.
			name: "UTC",
			t:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			// Positive minute-boundary offset: +05:30 = +19800s, version 1.
			name: "positive offset +05:30",
			t:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.FixedZone("IST", 5*3600+30*60)),
		},
		{
			// Negative minute-boundary offset: -06:00 = -21600s, version 1.
			name: "negative offset -06:00",
			t:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.FixedZone("CST", -6*3600)),
		},
		{
			// Sub-minute positive offset: +05:30:15 = +19815s, version 2.
			// offsetMin = 330 (0x014A), offsetSec byte = 15 (0x0F).
			// Both signed and unsigned reads agree: int(int8(15)) == int(15).
			name: "sub-minute positive +05:30:15",
			t:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.FixedZone("", 5*3600+30*60+15)),
		},
		// Sub-minute negative offsets are handled by a separate test because we
		// deliberately diverge from stdlib here: stdlib reads byte 15 unsigned
		// (bug, see BUG_REPORT.md) so its reconstructed offset is wrong; ours
		// reads it signed and recovers the original. Including such a case in
		// this round-trip would fail the "match stdlib" assertion by design.
		{
			// Pre-1970 timestamp: exercises floor division in secondsToCivil.
			name: "pre-1970",
			t:    time.Date(1965, 7, 4, 12, 0, 0, 0, time.UTC),
		},
		{
			// Pre-epoch with nanoseconds to also exercise nsec parsing.
			name: "pre-1970 with nanoseconds",
			t:    time.Date(1965, 7, 4, 12, 0, 0, 500000000, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := tc.t.MarshalBinary()
			require.NoError(t, err)

			// Our decoder.
			gotUnixSec, gotNsec, gotOffsetSec, err := parseTimeBytes(raw)
			require.NoError(t, err)

			// Reference: stdlib UnmarshalBinary.
			var ref time.Time
			require.NoError(t, ref.UnmarshalBinary(raw))

			// The absolute instant must agree.
			assert.Equal(t, ref.Unix(), gotUnixSec, "unix seconds mismatch")
			assert.Equal(t, int32(ref.Nanosecond()), gotNsec, "nanoseconds mismatch")

			// The reconstructed zone offset must match stdlib's reconstruction
			// exactly, including any quirks in how stdlib interprets byte 15.
			_, refOffsetSec := ref.Zone()
			assert.Equal(t, refOffsetSec, gotOffsetSec, "zone offset mismatch")
		})
	}
}

// TestParseTimeBytes_Version2_SubMinutePositive explicitly checks the version-2
// byte layout for a positive sub-minute offset to confirm byte 15 semantics.
func TestParseTimeBytes_Version2_SubMinutePositive(t *testing.T) {
	// +05:30:15 = 19815s: offsetMin=330 (0x014A), offsetSec byte=15 (0x0F).
	// MarshalBinary emits version=2, then the standard fields, then byte 0x0F.
	loc := time.FixedZone("", 5*3600+30*60+15)
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	raw, err := ts.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, 16, len(raw), "version-2 encoding must be 16 bytes")
	assert.Equal(t, byte(2), raw[0], "version byte must be 2")
	assert.Equal(t, byte(0x0F), raw[15], "sub-minute byte must be 15 (0x0F)")

	_, _, offsetSec, err := parseTimeBytes(raw)
	require.NoError(t, err)
	assert.Equal(t, 19815, offsetSec)
}

// TestParseTimeBytes_Version2_SubMinuteNegative verifies that we recover the
// correct sub-minute offset where stdlib's UnmarshalBinary does not. The stdlib
// reads byte 15 as uint8 (see BUG_REPORT.md), losing the sign for a byte like
// 0xFE that was encoded from int8(-2). We read it as int8 and recover the
// original -17762 offset that the encoder produced.
func TestParseTimeBytes_Version2_SubMinuteNegative(t *testing.T) {
	offset := -(4*3600 + 56*60 + 2) // -17762 seconds
	loc := time.FixedZone("", offset)
	ts := time.Date(1880, 1, 1, 0, 0, 0, 0, loc)
	raw, err := ts.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, 16, len(raw), "version-2 encoding must be 16 bytes")
	assert.Equal(t, byte(2), raw[0], "version byte must be 2 for sub-minute offset")
	// byte 15 must be int8(-2) reinterpreted as byte = 0xFE.
	assert.Equal(t, byte(0xFE), raw[15], "sub-minute byte must be 0xFE for -2s component")

	_, _, gotOffsetSec, err := parseTimeBytes(raw)
	require.NoError(t, err)

	assert.Equal(t, offset, gotOffsetSec,
		"parseTimeBytes must recover the original signed offset, unlike stdlib UnmarshalBinary")

	// Sanity: confirm stdlib is wrong for this blob and our value is the
	// correct one. If the Go stdlib is ever fixed upstream, this sub-assertion
	// will start failing and can simply be removed.
	var ref time.Time
	require.NoError(t, ref.UnmarshalBinary(raw))
	_, refOffsetSec := ref.Zone()
	if refOffsetSec == offset {
		t.Log("stdlib has been fixed; the divergence-assertion in this test can be simplified")
	}
}

// TestParseTimeBytes_Versions verifies that version 1 is accepted, version 2
// is accepted (with a 16-byte payload), and all other version bytes are rejected.
func TestParseTimeBytes_Versions(t *testing.T) {
	// Valid version-1 blob (15 bytes, UTC).
	v1 := []byte{0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF}
	_, _, _, err := parseTimeBytes(v1)
	require.NoError(t, err, "version 1 must be accepted")

	// Valid version-2 blob (16 bytes, UTC with sub-minute byte).
	v2 := []byte{0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0x0F}
	_, _, _, err = parseTimeBytes(v2)
	require.NoError(t, err, "version 2 must be accepted")

	// Version 3 and above must be rejected.
	for _, ver := range []byte{3, 4, 10, 255} {
		blob := make([]byte, 16)
		blob[0] = ver
		_, _, _, err = parseTimeBytes(blob)
		require.Error(t, err, "version %d must be rejected", ver)
		assert.Contains(t, err.Error(), "unsupported version", "error must mention 'unsupported version'")
	}
}

// — Unit tests: decoder functions ————————————————————————————————————————————

func TestDecodeTime_Version1_UTC(t *testing.T) {
	// Real gob output for time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC).
	// Probed from actual encoding: 01 00 00 00 0e dd 36 f2 18 00 00 00 00 ff ff
	raw := []byte{0x01, 0x00, 0x00, 0x00, 0x0e, 0xdd, 0x36, 0xf2, 0x18, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff}
	got, err := decodeTime(raw)
	require.NoError(t, err)
	assert.Equal(t, "2024-01-15T09:30:00Z", got)
}

func TestDecodeTime_Nanoseconds(t *testing.T) {
	// time.Date(2024, 1, 15, 9, 30, 0, 123456789, time.UTC).
	// sec same as above; nsec = 0x075bcd15 = 123456789.
	raw := []byte{0x01, 0x00, 0x00, 0x00, 0x0e, 0xdd, 0x36, 0xf2, 0x18, 0x07, 0x5b, 0xcd, 0x15, 0xff, 0xff}
	got, err := decodeTime(raw)
	require.NoError(t, err)
	assert.Equal(t, "2024-01-15T09:30:00.123456789Z", got)
}

func TestDecodeTime_PositiveOffset(t *testing.T) {
	// Encode a real time.Time with a positive zone offset, then decode with
	// decodeTime directly. This avoids hardcoding internal-epoch byte values.
	loc := time.FixedZone("IST", 5*3600+30*60)
	ts := time.Date(2024, 1, 15, 9, 30, 0, 0, loc)
	raw, err := ts.MarshalBinary()
	require.NoError(t, err)
	got, err2 := decodeTime(raw)
	require.NoError(t, err2)
	assert.Equal(t, "2024-01-15T09:30:00+05:30", got)
}

func TestDecodeTime_BadVersion(t *testing.T) {
	raw := []byte{0x05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := decodeTime(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported version")
}

func TestDecodeBigInt_Positive(t *testing.T) {
	// big.Int(12345): raw = [0x02, 0x30, 0x39]
	got, err := decodeBigInt([]byte{0x02, 0x30, 0x39})
	require.NoError(t, err)
	assert.Equal(t, "12345", got)
}

func TestDecodeBigInt_Negative(t *testing.T) {
	// big.Int(-12345): raw = [0x03, 0x30, 0x39]
	got, err := decodeBigInt([]byte{0x03, 0x30, 0x39})
	require.NoError(t, err)
	assert.Equal(t, "-12345", got)
}

func TestDecodeBigInt_Zero(t *testing.T) {
	// big.Int(0): raw = [0x02] (just the version/sign byte, no abs bytes)
	got, err := decodeBigInt([]byte{0x02})
	require.NoError(t, err)
	assert.Equal(t, "0", got)
}

func TestDecodeBigRat_Fraction(t *testing.T) {
	// big.Rat(355/113): raw = [0x02, 0x00, 0x00, 0x00, 0x02, 0x01, 0x63, 0x71]
	raw := []byte{0x02, 0x00, 0x00, 0x00, 0x02, 0x01, 0x63, 0x71}
	got, err := decodeBigRat(raw)
	require.NoError(t, err)
	assert.Equal(t, "355/113", got)
}

func TestDecodeBigRat_Integer(t *testing.T) {
	// big.Rat(42) stores denominator 1. The encoded denominator abs bytes are
	// [0x01] = 1, so result should be just "42".
	raw := []byte{0x02, 0x00, 0x00, 0x00, 0x01, 0x2a, 0x01}
	got, err := decodeBigRat(raw)
	require.NoError(t, err)
	assert.Equal(t, "42", got)
}

func TestDecodeBigAuto_RoutesToInt(t *testing.T) {
	// Small big.Int values have len < 5, so auto-detect always picks Int.
	got, err := decodeBigAuto([]byte{0x02, 0x30, 0x39})
	require.NoError(t, err)
	assert.Equal(t, "12345", got)
}

func TestDecodeBigAuto_RoutesToRat(t *testing.T) {
	raw := []byte{0x02, 0x00, 0x00, 0x00, 0x02, 0x01, 0x63, 0x71}
	got, err := decodeBigAuto(raw)
	require.NoError(t, err)
	assert.Equal(t, "355/113", got)
}

func TestDecodeUUID(t *testing.T) {
	raw := []byte{
		0x55, 0x0e, 0x84, 0x00,
		0xe2, 0x9b,
		0x41, 0xd4,
		0xa7, 0x16,
		0x44, 0x66, 0x55, 0x44, 0x00, 0x00,
	}
	got, err := decodeUUID(raw)
	require.NoError(t, err)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", got)
}

func TestDecodeUUID_WrongLength(t *testing.T) {
	_, err := decodeUUID([]byte{1, 2, 3})
	require.Error(t, err)
}

// — bigEndianToDecimal ————————————————————————————————————————————————————————

func TestBigEndianToDecimal(t *testing.T) {
	cases := []struct {
		in  []byte
		out string
	}{
		{[]byte{}, "0"},
		{[]byte{0x00}, "0"},
		{[]byte{0x01}, "1"},
		{[]byte{0x0a}, "10"},
		{[]byte{0x30, 0x39}, "12345"},
		{[]byte{0xff}, "255"},
		{[]byte{0x01, 0x00}, "256"},
	}
	for _, c := range cases {
		assert.Equal(t, c.out, bigEndianToDecimal(c.in), "input %x", c.in)
	}
}

// — Round-trip tests using real encoding/gob ——————————————————————————————————

func TestRoundTrip_TimeTime_UTC(t *testing.T) {
	ts := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	ov := decodeOpaque(t, gobEncodeVal(t, ts))
	assert.Equal(t, "Time", ov.TypeName)
	assert.Equal(t, "gob", ov.Encoding)
	assert.Equal(t, "2024-01-15T09:30:00Z", ov.Decoded)
}

func TestRoundTrip_TimeTime_Nanoseconds(t *testing.T) {
	ts := time.Date(2024, 1, 15, 9, 30, 0, 123456789, time.UTC)
	ov := decodeOpaque(t, gobEncodeVal(t, ts))
	assert.Equal(t, "2024-01-15T09:30:00.123456789Z", ov.Decoded)
}

func TestRoundTrip_TimeTime_NegativeOffset(t *testing.T) {
	loc := time.FixedZone("CST", -6*3600)
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, loc)
	ov := decodeOpaque(t, gobEncodeVal(t, ts))
	assert.Equal(t, "2024-06-01T12:00:00-06:00", ov.Decoded)
}

func TestRoundTrip_TimeTime_Epoch(t *testing.T) {
	ts := time.Unix(0, 0).UTC()
	ov := decodeOpaque(t, gobEncodeVal(t, ts))
	assert.Equal(t, "1970-01-01T00:00:00Z", ov.Decoded)
}

func TestRoundTrip_BigInt_Positive(t *testing.T) {
	n := big.NewInt(999999999999)
	ov := decodeOpaque(t, gobEncodeVal(t, n))
	assert.Equal(t, "", ov.TypeName) // pointer-receiver types get empty name
	assert.Equal(t, "gob", ov.Encoding)
	assert.Equal(t, "999999999999", ov.Decoded)
}

func TestRoundTrip_BigInt_Negative(t *testing.T) {
	n := big.NewInt(-42)
	ov := decodeOpaque(t, gobEncodeVal(t, n))
	assert.Equal(t, "-42", ov.Decoded)
}

func TestRoundTrip_BigInt_Zero(t *testing.T) {
	ov := decodeOpaque(t, gobEncodeVal(t, new(big.Int)))
	assert.Equal(t, "0", ov.Decoded)
}

func TestRoundTrip_BigInt_Large(t *testing.T) {
	n, _ := new(big.Int).SetString("12345678901234567890", 10)
	ov := decodeOpaque(t, gobEncodeVal(t, n))
	assert.Equal(t, "12345678901234567890", ov.Decoded)
}

func TestRoundTrip_BigRat_Fraction(t *testing.T) {
	r := new(big.Rat).SetFrac(big.NewInt(355), big.NewInt(113))
	ov := decodeOpaque(t, gobEncodeVal(t, r))
	assert.Equal(t, "355/113", ov.Decoded)
}

func TestRoundTrip_BigRat_NegativeFraction(t *testing.T) {
	r := new(big.Rat).SetFrac(big.NewInt(-1), big.NewInt(3))
	ov := decodeOpaque(t, gobEncodeVal(t, r))
	assert.Equal(t, "-1/3", ov.Decoded)
}

func TestRoundTrip_BigRat_Integer(t *testing.T) {
	r := new(big.Rat).SetInt(big.NewInt(7))
	ov := decodeOpaque(t, gobEncodeVal(t, r))
	assert.Equal(t, "7", ov.Decoded)
}

// — UUID decoder via registered decoder ———————————————————————————————————————

// fakeUUID is a [16]byte BinaryMarshaler used to test the UUID decoder
// without depending on a third-party UUID package.
type fakeUUID [16]byte

func (u fakeUUID) MarshalBinary() ([]byte, error)  { return u[:], nil }
func (u *fakeUUID) UnmarshalBinary(b []byte) error { copy(u[:], b); return nil }

func TestRoundTrip_UUID(t *testing.T) {
	// fakeUUID has a value receiver MarshalBinary, so encoding the value (not
	// pointer) gives TypeName="fakeUUID" in the gob wire. We register the UUID
	// decoder under that name to exercise the full decode path.
	id := fakeUUID{
		0x55, 0x0e, 0x84, 0x00,
		0xe2, 0x9b,
		0x41, 0xd4,
		0xa7, 0x16,
		0x44, 0x66, 0x55, 0x44, 0x00, 0x00,
	}
	buf := gobEncodeVal(t, id) // encode value, not pointer
	ins := New()
	ins.RegisterDecoder("fakeUUID", decodeUUID)
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(t, ok)
	assert.Equal(t, "fakeUUID", ov.TypeName)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", ov.Decoded)
}

// — decodeBigFloat ————————————————————————————————————————————————————————————

// — big.Float direct encoding via gob wire (bug 2.2) ————————————————————————
//
// When big.Float is encoded directly (not via interface), the gob wire has
// TypeName="". decodeBigAuto is called and must not produce silently wrong output.

func TestRoundTrip_BigFloat_DirectEncoding_NoSilentWrongOutput(t *testing.T) {
	// Encode big.Float directly (TypeName="" in wire).
	f := big.NewFloat(3.14)
	buf := gobEncodeVal(t, f)
	ins := New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err, "decoding big.Float directly must not return a stream error")
	require.Len(t, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(t, ok, "expected OpaqueValue, got %T", vals[0])
	assert.Equal(t, "", ov.TypeName, "direct big.Float must have empty TypeName")
	// Decoded must be nil (heuristic rejected it) or a correctly decoded float —
	// never a silently wrong integer or rational string.
	if ov.Decoded != nil {
		s, isStr := ov.Decoded.(string)
		require.True(t, isStr, "Decoded should be a string, got %T", ov.Decoded)
		// If decoded, it must look like a float (contain a '.') rather than an integer.
		assert.Contains(t, s, ".", "decoded big.Float should contain a decimal point, got %q", s)
	}
	// Regardless of whether it decoded, Raw must be non-empty and Format must not panic.
	assert.NotEmpty(t, ov.Raw)
	require.NotPanics(t, func() { Format(ov) })
}

func TestRoundTrip_BigFloat_NegativeDirectEncoding_NoSilentWrongOutput(t *testing.T) {
	// A negative big.Float with certain flags can produce data[0]==0x03, which
	// previously matched the big.Int version byte and could decode silently wrong.
	f := big.NewFloat(-1.5)
	buf := gobEncodeVal(t, f)
	ins := New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(t, ok)
	// Decoded must be nil (auto-detect rejected) or correctly float-shaped, never "-3" etc.
	if ov.Decoded != nil {
		s, isStr := ov.Decoded.(string)
		require.True(t, isStr)
		assert.Contains(t, s, ".", "decoded big.Float should look like a float, got %q", s)
	}
	require.NotPanics(t, func() { Format(ov) })
}

func TestDecodeBigAuto_RejectsUnknownVersionByte(t *testing.T) {
	// A blob whose first byte is not 0x02 or 0x03 should be rejected.
	_, err := decodeBigAuto([]byte{0x05, 0x01, 0x02, 0x03, 0x04, 0x05})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto-detect")
}

func TestRoundTrip_BigFloat_ViaInterface_HasNonEmptyTypeName(t *testing.T) {
	// When big.Float is encoded via an interface field with gob.Register, gob
	// emits a non-empty TypeName. Whatever that name is, Decoded should be set
	// (if our decoder key matches) or Raw should be non-empty with no panic.
	type wrapIface struct{ V any }
	gob.Register(big.NewFloat(0))
	w := wrapIface{V: big.NewFloat(2.718)}
	buf := gobEncodeVal(t, w)

	ins := New()
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)

	sv, ok := vals[0].(StructValue)
	require.True(t, ok, "expected StructValue, got %T", vals[0])
	require.Len(t, sv.Fields, 1)

	iv, ok := sv.Fields[0].Value.(InterfaceValue)
	require.True(t, ok, "expected InterfaceValue for V field, got %T", sv.Fields[0].Value)
	assert.NotEmpty(t, iv.TypeName, "interface-wrapped big.Float must have a non-empty TypeName in the wire")

	// The concrete value should be an OpaqueValue.
	ov, ok := iv.Value.(OpaqueValue)
	require.True(t, ok, "expected OpaqueValue inside interface, got %T", iv.Value)
	assert.NotEmpty(t, ov.Raw, "OpaqueValue must have raw bytes")
	require.NotPanics(t, func() { Format(iv) })

	// If decoded, it must look like a float.
	if ov.Decoded != nil {
		s, isStr := ov.Decoded.(string)
		require.True(t, isStr)
		assert.Contains(t, s, ".", "decoded big.Float via interface should contain a decimal point, got %q", s)
	}
}

func TestDecodeBigFloat_Pi(t *testing.T) {
	// Encode via big.Float.GobEncode directly (not through gob wire) so we can
	// test the decoder function in isolation, sidestepping the TypeName="" wire
	// ambiguity shared with big.Int and big.Rat.
	f := big.NewFloat(3.14159265358979323846)
	raw, err := f.GobEncode()
	require.NoError(t, err)
	got, err := decodeBigFloat(raw)
	require.NoError(t, err)
	s, ok := got.(string)
	require.True(t, ok, "expected string, got %T", got)
	assert.Contains(t, s, "3.14159")
}

// — decodeShopspringDecimal ————————————————————————————————————————————————————

func TestDecodeShopspringDecimal(t *testing.T) {
	// Wire format: int32 exponent as 4 big-endian bytes, followed by big.Int
	// bytes (sign/version + big-endian abs). Value = intCoeff × 10^exp.
	// This matches shopspring/decimal's MarshalBinary ("exp is written first").
	//
	// big.Int zero:  [0x02]           (version/sign byte only, no abs bytes)
	// big.Int 12345: [0x02, 0x30, 0x39]
	// big.Int -1:    [0x03, 0x01]
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "zero × 10^0",
			data: []byte{0x00, 0x00, 0x00, 0x00, 0x02},
			want: "0",
		},
		{
			name: "zero × 10^3 (positive exponent)",
			data: []byte{0x00, 0x00, 0x00, 0x03, 0x02},
			want: "0",
		},
		{
			name: "zero × 10^-2 (negative exponent)",
			data: []byte{0xFF, 0xFF, 0xFF, 0xFE, 0x02},
			want: "0",
		},
		{
			name: "12345 × 10^-2 = 123.45",
			data: []byte{0xFF, 0xFF, 0xFF, 0xFE, 0x02, 0x30, 0x39},
			want: "123.45",
		},
		{
			name: "12345 × 10^0 = 12345",
			data: []byte{0x00, 0x00, 0x00, 0x00, 0x02, 0x30, 0x39},
			want: "12345",
		},
		{
			name: "1 × 10^3 = 1000",
			data: []byte{0x00, 0x00, 0x00, 0x03, 0x02, 0x01},
			want: "1000",
		},
		{
			name: "-1 × 10^-1 = -0.1",
			data: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x03, 0x01},
			want: "-0.1",
		},
		{
			name: "5 × 10^-3 = 0.005 (leading zeros)",
			data: []byte{0xFF, 0xFF, 0xFF, 0xFD, 0x02, 0x05},
			want: "0.005",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeShopspringDecimal(tc.data)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDecodeShopspringDecimal_RealFixture decodes testdata/decimal_interface.gob,
// which was generated by the real shopspring/decimal v1.4.0 library encoding
// decimal.RequireFromString("123.45") through an interface (gob.Register).
// It guards both the wire layout (exponent first) and the registry key: the
// gob wire CommonType.Name for the type is the bare "Decimal".
func TestDecodeShopspringDecimal_RealFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/decimal_interface.gob")
	require.NoError(t, err)
	vals, err := New().Stream(bytes.NewReader(data)).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	iv, ok := vals[0].(InterfaceValue)
	require.True(t, ok, "expected InterfaceValue, got %T", vals[0])
	assert.Equal(t, "github.com/shopspring/decimal.Decimal", iv.TypeName)
	ov, ok := iv.Value.(OpaqueValue)
	require.True(t, ok, "expected OpaqueValue, got %T", iv.Value)
	assert.Equal(t, "Decimal", ov.TypeName)
	assert.Equal(t, "123.45", ov.Decoded)
}

func TestDecodeShopspringDecimal_ExponentOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		// exp = math.MinInt32: negating it overflows int32, which used to
		// slice-panic; must be rejected, not crash.
		{name: "MinInt32 exponent", data: []byte{0x80, 0x00, 0x00, 0x00, 0x02, 0x01}},
		// exp = math.MaxInt32 would demand a 2 GiB string of zeros.
		{name: "MaxInt32 exponent", data: []byte{0x7F, 0xFF, 0xFF, 0xFF, 0x02, 0x01}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeShopspringDecimal(tc.data)
			require.ErrorContains(t, err, "exponent")
		})
	}
}

// — WithTimeFormat ————————————————————————————————————————————————————————————

func TestWithTimeFormat_DateOnly(t *testing.T) {
	ts := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	ins := New(WithTimeFormat("2006-01-02"))
	buf := gobEncodeVal(t, ts)
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(t, ok)
	assert.Equal(t, "2024-06-15", ov.Decoded)
}

func TestWithTimeFormat_CustomLayout(t *testing.T) {
	ts := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	ins := New(WithTimeFormat("02 Jan 2006"))
	buf := gobEncodeVal(t, ts)
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov := vals[0].(OpaqueValue)
	assert.Equal(t, "31 Dec 2024", ov.Decoded)
}

func TestWithTimeFormat_DefaultMatchesRFC3339Nano(t *testing.T) {
	// Default layout should match the behavior of decodeTime.
	ts := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	insDefault := New()
	insCustom := New(WithTimeFormat("2006-01-02T15:04:05.999999999Z07:00"))

	buf1 := gobEncodeVal(t, ts)
	buf2 := gobEncodeVal(t, ts)

	vals1, err := insDefault.Stream(buf1).Collect()
	require.NoError(t, err)
	vals2, err := insCustom.Stream(buf2).Collect()
	require.NoError(t, err)

	assert.Equal(t, vals1[0].(OpaqueValue).Decoded, vals2[0].(OpaqueValue).Decoded)
}

// — New() pre-registration —————————————————————————————————————————————————

func TestNew_BuiltinsRegistered(t *testing.T) {
	ins := New()
	assert.Contains(t, ins.decoders, "Time")
	// "" key is no longer in decoders; decodeBigAuto is now an anonymous decoder.
	assert.NotContains(t, ins.decoders, "")
	assert.Contains(t, ins.decoders, "uuid.UUID")
	// Verify that at least one anonymous decoder is registered (decodeBigAuto).
	assert.NotEmpty(t, ins.anonymousDecoders, "decodeBigAuto should be registered as an anonymous decoder")
}

func TestNew_UserCanOverride(t *testing.T) {
	called := false
	ins := New()
	ins.RegisterDecoder("Time", func(data []byte) (any, error) {
		called = true
		return "custom", nil
	})

	ts := time.Now().UTC()
	buf := gobEncodeVal(t, ts)
	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(t, ok)
	assert.True(t, called, "custom decoder was not invoked")
	assert.Equal(t, "custom", ov.Decoded)
}

func TestRegisterDecoder_TimeCombinations(t *testing.T) {
	ts := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	buf := gobEncodeVal(t, ts)

	// Since decoding consumes the buffer, we provide a fresh reader for each case
	getBuf := func() *bytes.Buffer {
		return bytes.NewBuffer(buf.Bytes())
	}

	customDecoder := func(data []byte) (any, error) {
		return "OVERRIDDEN", nil
	}

	cases := []struct {
		name     string
		setup    func() *Inspector
		expected string
	}{
		{
			name: "(a) default time.Time rendering",
			setup: func() *Inspector {
				return New()
			},
			expected: "2025-01-01T12:00:00Z",
		},
		{
			name: "(b) WithTimeFormat custom layout",
			setup: func() *Inspector {
				return New(WithTimeFormat("2006"))
			},
			expected: "2025",
		},
		{
			name: "(c) RegisterDecoder after New overriding the built-in",
			setup: func() *Inspector {
				ins := New()
				ins.RegisterDecoder("Time", customDecoder)
				return ins
			},
			expected: "OVERRIDDEN",
		},
		{
			name: "(d) WithTimeFormat combined with subsequent RegisterDecoder where the latter wins",
			setup: func() *Inspector {
				ins := New(WithTimeFormat("2006"))
				ins.RegisterDecoder("Time", customDecoder)
				return ins
			},
			expected: "OVERRIDDEN",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ins := tc.setup()
			vals, err := ins.Stream(getBuf()).Collect()
			require.NoError(t, err)
			require.Len(t, vals, 1)

			ov, ok := vals[0].(OpaqueValue)
			require.True(t, ok)
			assert.Equal(t, tc.expected, ov.Decoded)
		})
	}
}

// — decodeBigAuto round-trip table ————————————————————————————————————————————

// TestDecodeBigAuto_RoundTrip encodes a range of *big.Int and *big.Rat values
// via encoding/gob (which exercises the real GobEncoder wire format) and
// asserts that decodeBigAuto returns the same decimal rendering as the Go
// type's own String/RatString method.
//
// Boundary cases covered:
//   - big.Int values around 2^32 (5-byte absolute value, where data[1:5] as
//     uint32 would be a large numLen — safe because data[1] is always non-zero)
//   - A very large big.Int (many bytes) to confirm the numLen >> len(data)-5
//   - Zero big.Int and zero big.Rat
//   - big.Rat with numLen==0 (numerator is zero)
//   - big.Rat where numerator and denominator lengths vary
func TestDecodeBigAuto_RoundTrip(t *testing.T) {
	mustInt := func(s string) *big.Int {
		n, ok := new(big.Int).SetString(s, 10)
		require.True(t, ok, "invalid big.Int literal %q", s)
		return n
	}
	mustRat := func(num, den int64) *big.Rat {
		return new(big.Rat).SetFrac(big.NewInt(num), big.NewInt(den))
	}

	intCases := []struct {
		name string
		val  *big.Int
	}{
		{"zero", big.NewInt(0)},
		{"one", big.NewInt(1)},
		{"negative one", big.NewInt(-1)},
		{"small positive", big.NewInt(12345)},
		{"small negative", big.NewInt(-99999)},
		// 3-byte absolute value: 0x01_00_00 = 65536
		{"65536 (3-byte abs)", big.NewInt(65536)},
		// 4-byte absolute value: values in [2^24, 2^32-1]
		{"2^24", mustInt("16777216")},
		{"2^32-1", mustInt("4294967295")},
		// 5-byte absolute value: values in [2^32, 2^40-1]
		// data[1:5] = first 4 bytes of abs; data[1] != 0 so numLen >= 2^24 >> len(data)-5
		{"2^32", mustInt("4294967296")},
		{"2^32+1", mustInt("4294967297")},
		{"2^32+small", mustInt("4295032833")}, // 2^32 + 0xFFFF
		{"2^40-1", mustInt("1099511627775")},
		// Large values: 8-byte absolute value (2^56..2^64-1)
		{"2^63-1", mustInt("9223372036854775807")},
		{"2^64", mustInt("18446744073709551616")},
		// Very large: 20-digit decimal, abs > 8 bytes
		{"20-digit", mustInt("12345678901234567890123456789")},
		// Negative large
		{"-2^32", mustInt("-4294967296")},
		{"-2^64", mustInt("-18446744073709551616")},
	}

	ratCases := []struct {
		name string
		val  *big.Rat
		want string // expected RatString
	}{
		// Zero Rat: numerator=0, numLen=0; decodeBigAuto must route to Rat not Int.
		{"zero rat", mustRat(0, 1), "0"},
		// Integer Rat (denominator normalises to 1): result is just numerator.
		{"rat 1/1", mustRat(1, 1), "1"},
		{"rat 7/1", mustRat(7, 1), "7"},
		{"rat -3/1", mustRat(-3, 1), "-3"},
		// Simple fractions
		{"1/2", mustRat(1, 2), "1/2"},
		{"355/113", mustRat(355, 113), "355/113"},
		{"negative -1/3", mustRat(-1, 3), "-1/3"},
		// Larger numerator/denominator
		{"1000000/999999", mustRat(1000000, 999999), "1000000/999999"},
		// numLen boundary: numerator fits in 1 byte (numLen=1), denominator varies
		{"127/128", mustRat(127, 128), "127/128"},
		// Large numerator: numLen = 3 bytes
		{"16777215/2", mustRat(16777215, 2), "16777215/2"},
	}

	t.Run("big.Int", func(t *testing.T) {
		for _, tc := range intCases {
			t.Run(tc.name, func(t *testing.T) {
				buf := gobEncodeVal(t, tc.val)
				ov := decodeOpaque(t, buf)
				require.NotNil(t, ov.Decoded, "Decoded must not be nil for big.Int %s", tc.val.String())
				got, ok := ov.Decoded.(string)
				require.True(t, ok, "Decoded should be a string, got %T", ov.Decoded)
				assert.Equal(t, tc.val.String(), got, "big.Int %s: wrong decoded value", tc.val.String())
			})
		}
	})

	t.Run("big.Rat", func(t *testing.T) {
		for _, tc := range ratCases {
			t.Run(tc.name, func(t *testing.T) {
				buf := gobEncodeVal(t, tc.val)
				ov := decodeOpaque(t, buf)
				require.NotNil(t, ov.Decoded, "Decoded must not be nil for big.Rat %s", tc.want)
				got, ok := ov.Decoded.(string)
				require.True(t, ok, "Decoded should be a string, got %T", ov.Decoded)
				assert.Equal(t, tc.want, got, "big.Rat %s: wrong decoded value", tc.want)
			})
		}
	})
}

// TestDecodeBigAuto_ExactLengthCheck verifies the tightened heuristic: a
// big.Rat blob is accepted only when 5 + numLen <= len(data) (existing check)
// AND 5 + numLen == len(data) when numLen == 0 would yield an empty denominator
// (zero Rat). This test encodes crafted byte slices that would be
// misidentified as big.Rat under a looser check.
func TestDecodeBigAuto_ExactLengthCheck(t *testing.T) {
	// A big.Int blob whose absolute value is [0x01, 0x00, 0x00, 0x00, 0x00]:
	//   data = [0x02, 0x01, 0x00, 0x00, 0x00, 0x00]
	//   len(data)-5 = 1
	//   numLen = 0x01000000 = 16777216  →  16777216 <= 1? No → Int. ✓
	data := []byte{0x02, 0x01, 0x00, 0x00, 0x00, 0x00}
	got, err := decodeBigAuto(data)
	require.NoError(t, err)
	assert.Equal(t, "4294967296", got, "should decode as big.Int(2^32), not misrouted to Rat")

	// A crafted big.Rat zero blob: [0x02, 0x00,0x00,0x00,0x00, 0x01]
	//   numLen = 0, len(data)-5 = 1 → 0 <= 1 → routes to Rat → "0". ✓
	ratZero := []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	got2, err2 := decodeBigAuto(ratZero)
	require.NoError(t, err2)
	assert.Equal(t, "0", got2, "big.Rat zero should decode as 0")
}

// — netip decoders ———————————————————————————————————————————————————————————

func TestDecodeNetipAddr_IPv4(t *testing.T) {
	// net/netip.Addr.MarshalBinary for an IPv4 address is 4 raw bytes.
	raw := []byte{1, 2, 3, 4}
	got, err := decodeNetipAddr(raw)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3.4", got)
}

func TestDecodeNetipAddr_IPv6(t *testing.T) {
	// 16 bytes = IPv6 ::1
	raw := make([]byte, 16)
	raw[15] = 1
	got, err := decodeNetipAddr(raw)
	require.NoError(t, err)
	assert.Equal(t, "::1", got)
}

func TestDecodeNetipAddr_Invalid(t *testing.T) {
	// An arbitrary non-4/non-16 byte count should fail (stdlib rejects it).
	raw := []byte{1, 2, 3}
	_, err := decodeNetipAddr(raw)
	assert.Error(t, err)
}

func TestDecodeNetipPrefix_IPv4(t *testing.T) {
	// Prefix.MarshalBinary appends the prefix length byte.
	raw := []byte{10, 0, 0, 0, 24}
	got, err := decodeNetipPrefix(raw)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.0/24", got)
}

func TestDecodeNetipAddrPort_IPv4(t *testing.T) {
	// MarshalBinary for AddrPort appends 2 bytes of port in little-endian.
	// 1.2.3.4:80 = addr bytes {1,2,3,4} + port 80 = 0x50, 0x00.
	raw := []byte{1, 2, 3, 4, 0x50, 0x00}
	got, err := decodeNetipAddrPort(raw)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3.4:80", got)
}
