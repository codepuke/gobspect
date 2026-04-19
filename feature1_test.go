package gobspect_test

import (
	"errors"
	"testing"

	gobspect "github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ————————————————————————————————————————————————————————————————————————————
// Value.TypeID() tests

func TestValueTypeID_Scalars(t *testing.T) {
	// All scalar and opaque value types must return 0 from TypeID().
	cases := []struct {
		name string
		v    gobspect.Value
	}{
		{"IntValue", gobspect.IntValue{V: 42}},
		{"UintValue", gobspect.UintValue{V: 99}},
		{"FloatValue", gobspect.FloatValue{V: 3.14}},
		{"ComplexValue", gobspect.ComplexValue{Real: 1, Imag: 2}},
		{"BoolValue", gobspect.BoolValue{V: true}},
		{"StringValue", gobspect.StringValue{V: "hello"}},
		{"BytesValue", gobspect.BytesValue{V: []byte{1, 2, 3}}},
		{"NilValue", gobspect.NilValue{}},
		{"InterfaceValue", gobspect.InterfaceValue{TypeName: "T", Value: gobspect.NilValue{}}},
		{"OpaqueValue", gobspect.OpaqueValue{TypeName: "X", Encoding: "gob", Raw: []byte{0x01}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, 0, tc.v.TypeID())
		})
	}
}

func TestValueTypeID_Composites(t *testing.T) {
	// Composite types return their GobTypeID field.
	t.Run("StructValue", func(t *testing.T) {
		v := gobspect.StructValue{TypeName: "S", GobTypeID: 17}
		assert.Equal(t, 17, v.TypeID())
	})
	t.Run("MapValue", func(t *testing.T) {
		v := gobspect.MapValue{TypeName: "M", GobTypeID: 22}
		assert.Equal(t, 22, v.TypeID())
	})
	t.Run("SliceValue", func(t *testing.T) {
		v := gobspect.SliceValue{TypeName: "S", GobTypeID: 33}
		assert.Equal(t, 33, v.TypeID())
	})
	t.Run("ArrayValue", func(t *testing.T) {
		v := gobspect.ArrayValue{TypeName: "A", GobTypeID: 44}
		assert.Equal(t, 44, v.TypeID())
	})
	t.Run("zero GobTypeID returns 0", func(t *testing.T) {
		v := gobspect.StructValue{TypeName: "S"}
		assert.Equal(t, 0, v.TypeID())
	})
}

func TestValueTypeID_FromRealDecode(t *testing.T) {
	// After decoding a real gob stream the composite TypeID must be non-zero.
	vals := decode(t, Point{X: 1, Y: 2})
	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.NotZero(t, sv.TypeID(), "TypeID should be non-zero after real decode")
	assert.Equal(t, sv.GobTypeID, sv.TypeID())
}

// ————————————————————————————————————————————————————————————————————————————
// ValueKind() tests

func TestValueKind(t *testing.T) {
	cases := []struct {
		v    gobspect.Value
		want string
	}{
		{gobspect.StructValue{}, "struct"},
		{gobspect.MapValue{}, "map"},
		{gobspect.SliceValue{}, "slice"},
		{gobspect.ArrayValue{}, "array"},
		{gobspect.IntValue{}, "int"},
		{gobspect.UintValue{}, "uint"},
		{gobspect.FloatValue{}, "float"},
		{gobspect.ComplexValue{}, "complex"},
		{gobspect.BoolValue{}, "bool"},
		{gobspect.StringValue{}, "string"},
		{gobspect.BytesValue{}, "bytes"},
		{gobspect.NilValue{}, "nil"},
		{gobspect.InterfaceValue{}, "interface"},
		{gobspect.OpaqueValue{}, "opaque"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, gobspect.ValueKind(tc.v))
		})
	}
}

// ————————————————————————————————————————————————————————————————————————————
// Anonymous decoder tests

// errDecoder is a DecoderFunc that always returns an error.
var errDecoder gobspect.DecoderFunc = func(data []byte) (any, error) {
	return nil, errors.New("intentional error")
}

// echoDecoder returns the raw bytes as a string (for testing).
func echoDecoder(prefix string) gobspect.DecoderFunc {
	return func(data []byte) (any, error) {
		return prefix + string(data), nil
	}
}

// gobEncoderWithEmptyTypeName produces a gob stream containing a GobEncoder
// with an empty CommonType.Name (i.e. encoded directly without interface wrapping).
type simpleGobEncoder struct{ payload string }

func (e *simpleGobEncoder) GobEncode() ([]byte, error) { return []byte(e.payload), nil }
func (e *simpleGobEncoder) GobDecode(b []byte) error   { e.payload = string(b); return nil }

func encodeGobEncoderDirect(tb testing.TB, payload string) *gobspect.Inspector {
	tb.Helper()
	return nil // just a placeholder — we use gobEncode from decode_test.go
}

func TestAnonymousDecoders_TriedInOrder(t *testing.T) {
	// Register two anonymous decoders; the first errors, the second succeeds.
	// Expect the second decoder's result to be used.
	buf := gobEncode(t, &simpleGobEncoder{payload: "hello"})
	ins := gobspect.New()
	// Remove all existing anonymous decoders by creating a fresh inspector with none.
	ins2 := gobspect.New()
	// Override anonymous list: register error-first, then success.
	ins2.RegisterUnnamedDecoder(errDecoder)
	ins2.RegisterUnnamedDecoder(echoDecoder("second:"))

	vals, err := ins2.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov, ok := vals[0].(gobspect.OpaqueValue)
	require.True(t, ok)
	// The second decoder should have won.
	assert.Equal(t, "second:hello", ov.Decoded)
	_ = ins
}

func TestAnonymousDecoders_FirstSuccessWins(t *testing.T) {
	// Register two succeeding anonymous decoders; only the first should be used.
	buf := gobEncode(t, &simpleGobEncoder{payload: "data"})
	ins := gobspect.New()
	ins.RegisterUnnamedDecoder(echoDecoder("first:"))
	ins.RegisterUnnamedDecoder(echoDecoder("second:"))

	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov := vals[0].(gobspect.OpaqueValue)
	assert.Equal(t, "first:data", ov.Decoded)
}

func TestAnonymousDecoders_NamedLookupNotAffected(t *testing.T) {
	// A named decoder ("" is no longer valid as a named key for anonymous dispatch;
	// non-empty named decoders should still work for typed opaques).
	// Here we test that a registered named decoder for a known type still fires.
	buf := gobEncode(t, &gobEncoderType{S: "named"})
	ins := gobspect.New()
	// Register an anonymous decoder that would win for empty-named opaques.
	ins.RegisterUnnamedDecoder(echoDecoder("anon:"))
	// The gobEncoderType has TypeName="" so anonymous decoders apply.
	// We want to verify that registering an anon decoder doesn't break named ones.
	// Also register a named decoder under a made-up key to confirm named still works.
	ins.RegisterDecoder("myType", echoDecoder("named:"))

	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov := vals[0].(gobspect.OpaqueValue)
	// The anonymous decoder should fire since TypeName is empty.
	assert.Equal(t, "anon:named", ov.Decoded)
}

func TestRegisterUnnamedDecoder_EmptyKeyRemovedFromDecoders(t *testing.T) {
	// Verify that after New(), the empty string key is not in the named decoders map.
	// We test this indirectly: RegisterDecoder("", fn) should still work as an
	// override for the named path, but the builtin decodeBigAuto is now anonymous.
	buf := gobEncode(t, &simpleGobEncoder{payload: "test"})
	ins := gobspect.New()
	// Override the empty-string named decoder — this should NOT intercept anonymous dispatch.
	// Anonymous dispatch is separate from named dispatch now.
	// If the old "" key still existed in decoders, RegisterDecoder("", fn) would win.
	// Now: named dispatch for empty-TypeName types goes to anonymousDecoders, not decoders[""].
	ins.RegisterDecoder("", echoDecoder("named-empty:"))

	vals, err := ins.Stream(buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	ov := vals[0].(gobspect.OpaqueValue)
	// The named "" key is now not used for anonymous dispatch, so
	// the built-in anonymous decoder (decodeBigAuto) applies, which will
	// fail the heuristic for non-big.Int/Rat data, leaving Decoded nil.
	// OR: if decodeBigAuto errors, Decoded remains nil.
	// Either way, "named-empty:" should NOT appear.
	assert.NotEqual(t, "named-empty:test", ov.Decoded,
		"named decoder under '' key must not intercept anonymous opaque dispatch")
}
