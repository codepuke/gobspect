package gobspect

// Internal tests for built-in opaque decoders. Using the internal package
// gives direct access to unexported decoder functions, so each decoder can be
// tested in isolation with known byte sequences derived from real gob output.

import (
	"bytes"
	"encoding/gob"
	"math/big"
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
	vals, err := ins.Decode(buf)
	require.NoError(tb, err)
	require.Len(tb, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(tb, ok, "expected OpaqueValue, got %T", vals[0])
	return ov
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
	vals, err := ins.Decode(buf)
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
	vals, err := ins.Decode(buf)
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
	vals, err := ins.Decode(buf)
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
	vals, err := ins.Decode(buf)
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

func TestDecodeShopspringDecimal_123_45(t *testing.T) {
	// Hand-constructed binary format: big.Int 12345 (sign/version byte 0x02 +
	// big-endian abs 0x30 0x39) followed by int32(-2) as big-endian bytes.
	// 12345 × 10^(-2) = 123.45
	data := []byte{
		0x02, 0x30, 0x39, // big.Int 12345: version/sign=0x02, abs=0x3039
		0xFF, 0xFF, 0xFF, 0xFE, // int32(-2) big-endian
	}
	got, err := decodeShopspringDecimal(data)
	require.NoError(t, err)
	assert.Equal(t, "123.45", got)
}

// — WithTimeFormat ————————————————————————————————————————————————————————————

func TestWithTimeFormat_DateOnly(t *testing.T) {
	ts := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	ins := New(WithTimeFormat("2006-01-02"))
	buf := gobEncodeVal(t, ts)
	vals, err := ins.Decode(buf)
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
	vals, err := ins.Decode(buf)
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

	vals1, err := insDefault.Decode(buf1)
	require.NoError(t, err)
	vals2, err := insCustom.Decode(buf2)
	require.NoError(t, err)

	assert.Equal(t, vals1[0].(OpaqueValue).Decoded, vals2[0].(OpaqueValue).Decoded)
}

// — New() pre-registration —————————————————————————————————————————————————

func TestNew_BuiltinsRegistered(t *testing.T) {
	ins := New()
	assert.Contains(t, ins.decoders, "Time")
	assert.Contains(t, ins.decoders, "")
	assert.Contains(t, ins.decoders, "uuid.UUID")
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
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(OpaqueValue)
	require.True(t, ok)
	assert.True(t, called, "custom decoder was not invoked")
	assert.Equal(t, "custom", ov.Decoded)
}
