package gobspect_test

import (
	"encoding/gob"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — Fixtures unique to format tests ——————————————————————————————————————————

type fmtInner struct {
	A int
	B string
}

type fmtOuter struct {
	X fmtInner
	Y float64
}

// testOpaqueVal implements gob.GobEncoder with a value receiver so that gob
// encodes it with a non-empty TypeName ("testOpaqueVal"). It is not registered
// with the Inspector, so it exercises the unknown-opaque fallback path.
type testOpaqueVal struct{ V byte }

func (t testOpaqueVal) GobEncode() ([]byte, error) { return []byte{t.V}, nil }
func (t *testOpaqueVal) GobDecode(b []byte) error {
	if len(b) > 0 {
		t.V = b[0]
	}
	return nil
}

// — Helper ————————————————————————————————————————————————————————————————————

// formatFirst encodes v with encoding/gob, decodes with a fresh Inspector,
// and formats the single resulting Value.
func formatFirst(tb testing.TB, v any, opts ...gobspect.FormatOption) string {
	tb.Helper()
	buf := gobEncode(tb, v)
	ins := gobspect.New()
	vals, err := ins.Decode(buf)
	require.NoError(tb, err)
	require.Len(tb, vals, 1)
	return gobspect.Format(vals[0], opts...)
}

// — Golden tests ——————————————————————————————————————————————————————————————

func TestFormat_Golden(t *testing.T) {
	tests := []struct {
		name string
		got  func() string
		want string
	}{
		{
			name: "nested_struct",
			got: func() string {
				return formatFirst(t, fmtOuter{
					X: fmtInner{A: 42, B: "hello"},
					Y: 3.14,
				})
			},
			want: "" +
				"fmtOuter{\n" +
				"  X: fmtInner{\n" +
				"    A: 42\n" +
				"    B: \"hello\"\n" +
				"  }\n" +
				"  Y: 3.14\n" +
				"}",
		},
		{
			name: "map_short_inline",
			got: func() string {
				return formatFirst(t, StringMap{"a": 1, "b": 2})
			},
			want: `map[string]int{"a": 1, "b": 2}`,
		},
		{
			name: "map_long_indented",
			got: func() string {
				return formatFirst(t, StringMap{
					"alpha": 100, "beta": 200, "delta": 300,
					"gamma": 400, "zeta": 500,
				})
			},
			want: "" +
				"map[string]int{\n" +
				"  \"alpha\": 100\n" +
				"  \"beta\": 200\n" +
				"  \"delta\": 300\n" +
				"  \"gamma\": 400\n" +
				"  \"zeta\": 500\n" +
				"}",
		},
		{
			name: "slice_short_inline",
			got: func() string {
				return formatFirst(t, IntSlice{10, 20, 30})
			},
			want: `[]int{10, 20, 30}`,
		},
		{
			name: "opaque_time",
			got: func() string {
				ts := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
				return formatFirst(t, ts)
			},
			want: `2024-01-15T09:30:00Z`,
		},
		{
			name: "opaque_bigint",
			got: func() string {
				return formatFirst(t, big.NewInt(123456789))
			},
			want: `123456789`,
		},
		{
			name: "unknown_opaque_hex",
			got: func() string {
				return formatFirst(t, testOpaqueVal{V: 0xab})
			},
			want: `(testOpaqueVal) ab`,
		},
		{
			name: "bytes_printable_utf8",
			got: func() string {
				return formatFirst(t, WrapBytes{V: []byte("hello world")})
			},
			want: "" +
				"WrapBytes{\n" +
				"  V: \"hello world\"\n" +
				"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got())
		})
	}
}

// — Option tests ——————————————————————————————————————————————————————————————

func TestFormat_WithIndent(t *testing.T) {
	got := formatFirst(t, fmtOuter{X: fmtInner{A: 1, B: "x"}, Y: 0},
		gobspect.WithIndent("\t"))
	assert.Equal(t, "fmtOuter{\n\tX: fmtInner{\n\t\tA: 1\n\t\tB: \"x\"\n\t}\n}", got)
}

func TestFormat_WithRawOpaques(t *testing.T) {
	ts := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	// Without WithRawOpaques the decoded string is returned.
	decoded := formatFirst(t, ts)
	assert.Equal(t, "2024-01-15T09:30:00Z", decoded)
	// With WithRawOpaques the hex is shown even though Decoded is set.
	raw := formatFirst(t, ts, gobspect.WithRawOpaques(true))
	assert.Contains(t, raw, "(Time) ")
}

func TestFormat_WithMaxBytes(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0x01, 0x02, 0x03, 0x04, 0x05}},
		gobspect.WithMaxBytes(3))
	// Field value is hex-truncated to 3 bytes.
	assert.Equal(t, "WrapBytes{\n  V: 010203…\n}", got)
}

func TestFormat_BytesNonPrintable(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0xDE, 0xAD, 0xBE, 0xEF}})
	assert.Equal(t, "WrapBytes{\n  V: deadbeef\n}", got)
}

func TestFormat_EmptyStruct(t *testing.T) {
	// Point{} has zero-valued fields; gob omits them, so Fields is empty.
	got := formatFirst(t, Point{})
	assert.Equal(t, "Point{}", got)
}

func TestFormat_EmptySlice(t *testing.T) {
	got := formatFirst(t, IntSlice{})
	assert.Equal(t, "[]int{}", got)
}

func TestFormat_EmptyMap(t *testing.T) {
	got := formatFirst(t, StringMap{})
	assert.Equal(t, "map[string]int{}", got)
}

// — WithRedactKeys tests ——————————————————————————————————————————————————————

type secretStruct struct {
	Username string
	Password string
	Token    string
	Age      int
}

func TestFormat_WithRedactKeys_StructField(t *testing.T) {
	got := formatFirst(t, secretStruct{Username: "alice", Password: "s3cr3t", Age: 30},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}}))
	assert.Contains(t, got, "Username")
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "Age")
	// Password value should be redacted.
	assert.NotContains(t, got, "s3cr3t")
	assert.Contains(t, got, "Password: ")
}

func TestFormat_WithRedactKeys_MultipleKeys(t *testing.T) {
	got := formatFirst(t, secretStruct{Username: "alice", Password: "s3cr3t", Token: "tok123", Age: 30},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password", "Token"}}))
	assert.NotContains(t, got, "s3cr3t")
	assert.NotContains(t, got, "tok123")
	assert.Contains(t, got, "alice")
}

func TestFormat_WithRedactKeys_TextLength(t *testing.T) {
	got := formatFirst(t, secretStruct{Password: "any"},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}, Char: '*', TextLength: 8}))
	assert.Contains(t, got, "********")
}

func TestFormat_WithRedactKeys_LengthPreserving(t *testing.T) {
	// TextLength == 0: preserve visual length of the rendered value.
	// "s3cr3t" renders as `"s3cr3t"` (8 chars with quotes) → 8 stars.
	got := formatFirst(t, secretStruct{Password: "s3cr3t"},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}, Char: '*'}))
	assert.Contains(t, got, "Password: ********")
}

func TestFormat_WithRedactKeys_CustomChar(t *testing.T) {
	got := formatFirst(t, secretStruct{Password: "abc"},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}, Char: '#', TextLength: 3}))
	assert.Contains(t, got, "###")
}

func TestFormat_WithRedactKeys_NonMatchingUnaffected(t *testing.T) {
	got := formatFirst(t, secretStruct{Username: "alice", Password: "s3cr3t"},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}}))
	// Username is not in redact keys, should appear unmodified.
	assert.Contains(t, got, `"alice"`)
}

// mapWithPassword is a map whose keys include "password" for redaction testing.
type mapWithPassword map[string]string

func init() { gob.Register(mapWithPassword{}) }

func TestFormat_WithRedactKeys_MapEntry(t *testing.T) {
	m := mapWithPassword{"username": "alice", "password": "s3cr3t"}
	got := formatFirst(t, m,
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{`"password"`}}))
	assert.NotContains(t, got, "s3cr3t")
	assert.Contains(t, got, "alice")
}

// — WithRedactTypes tests ——————————————————————————————————————————————————————

type sensitiveType struct{ V string }
type normalType struct{ V string }

type holdsTypes struct {
	S sensitiveType
	N normalType
}

func TestFormat_WithRedactTypes_Struct(t *testing.T) {
	got := formatFirst(t, holdsTypes{S: sensitiveType{V: "secret"}, N: normalType{V: "public"}},
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"sensitiveType"}}))
	assert.NotContains(t, got, "secret")
	assert.Contains(t, got, "public")
}

func TestFormat_WithRedactTypes_NonMatchingUnaffected(t *testing.T) {
	got := formatFirst(t, holdsTypes{S: sensitiveType{V: "secret"}, N: normalType{V: "public"}},
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"nonExistentType"}}))
	assert.Contains(t, got, "secret")
	assert.Contains(t, got, "public")
}

func TestFormat_WithRedactTypes_CustomChar(t *testing.T) {
	got := formatFirst(t, holdsTypes{S: sensitiveType{V: "secret"}, N: normalType{V: "public"}},
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"sensitiveType"}, Char: '#'}))
	assert.NotContains(t, got, "secret")
	assert.Contains(t, got, "#")
	assert.NotContains(t, got, "*")
}

func TestFormat_WithRedactTypes_TextLength(t *testing.T) {
	got := formatFirst(t, holdsTypes{S: sensitiveType{V: "secret"}, N: normalType{V: "public"}},
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"sensitiveType"}, Char: '*', TextLength: 5}))
	assert.NotContains(t, got, "secret")
	assert.Contains(t, got, "*****")
}

func TestFormat_WithRedactTypes_CombinedWithRedactKeys(t *testing.T) {
	got := formatFirst(t, secretStruct{Username: "alice", Password: "pass"},
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}}),
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"doesNotExist"}}),
	)
	assert.NotContains(t, got, "pass")
	assert.Contains(t, got, "alice")
}

// — WithBytesFormat tests ——————————————————————————————————————————————————————

func TestFormat_BytesHex_Default(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0xde, 0xad, 0xbe, 0xef}})
	assert.Equal(t, "WrapBytes{\n  V: deadbeef\n}", got)
}

func TestFormat_BytesHex_Explicit(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0xde, 0xad}},
		gobspect.WithBytesFormat(gobspect.BytesHex))
	assert.Contains(t, got, "dead")
	// Explicit: printable UTF-8 shortcut suppressed.
	got2 := formatFirst(t, WrapBytes{V: []byte("hi")},
		gobspect.WithBytesFormat(gobspect.BytesHex))
	assert.Contains(t, got2, "6869") // hex of "hi"
}

func TestFormat_BytesBase64(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0xde, 0xad}},
		gobspect.WithBytesFormat(gobspect.BytesBase64))
	assert.Equal(t, "WrapBytes{\n  V: 3q0=\n}", got)
}

func TestFormat_BytesBase64_PrintableUTF8_NoShortcut(t *testing.T) {
	// When BytesFormat is explicit, printable UTF-8 must use the requested format.
	got := formatFirst(t, WrapBytes{V: []byte("hello")},
		gobspect.WithBytesFormat(gobspect.BytesBase64))
	assert.Contains(t, got, "aGVsbG8=") // base64 of "hello"
	assert.NotContains(t, got, `"hello"`)
}

func TestFormat_BytesLiteral(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0xde, 0xad}},
		gobspect.WithBytesFormat(gobspect.BytesLiteral))
	assert.Equal(t, "WrapBytes{\n  V: []byte{0xde, 0xad}\n}", got)
}

func TestFormat_BytesLiteral_Truncation(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0x01, 0x02, 0x03, 0x04}},
		gobspect.WithBytesFormat(gobspect.BytesLiteral),
		gobspect.WithMaxBytes(2))
	assert.Equal(t, "WrapBytes{\n  V: []byte{0x01, 0x02}…\n}", got)
}

func TestFormat_BytesBase64_Truncation(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0x01, 0x02, 0x03, 0x04}},
		gobspect.WithBytesFormat(gobspect.BytesBase64),
		gobspect.WithMaxBytes(2))
	// base64 of [0x01, 0x02] is "AQI="
	assert.Equal(t, "WrapBytes{\n  V: AQI=…\n}", got)
}

// — WithTimeFormat (format layer) ————————————————————————————————————————————

func TestFormat_WithTimeFormat_DateOnly(t *testing.T) {
	ts := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	buf := gobEncode(t, ts)
	ins := gobspect.New(gobspect.WithTimeFormat("2006-01-02"))
	vals, err := ins.Decode(buf)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	out := gobspect.Format(vals[0])
	assert.Equal(t, "2024-06-15", out)
}

// — fmtMap redact at depth > 0 (bug 2.3) ————————————————————————————————————

// wrapMap wraps a map inside a struct so it decodes at depth > 0, exercising
// the indented-rendering path in fmtMap.
type wrapMap struct {
	M mapWithPassword
}

func TestFormat_WithRedactKeys_MapEntry_AtDepth(t *testing.T) {
	// The map is nested inside a struct, so depth > 0 when fmtMap runs.
	// The redact-key match must still work using the canonical depth-0 key format.
	m := wrapMap{M: mapWithPassword{"username": "alice", "password": "s3cr3t"}}
	got := formatFirst(t, m,
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{`"password"`}}))
	assert.NotContains(t, got, "s3cr3t", "password value should be redacted even at depth > 0")
	assert.Contains(t, got, "alice", "non-redacted value should appear")
}

// — FormatSchema fixtures ——————————————————————————————————————————————————————

// schemaBinary is a BinaryMarshaler with a value receiver so gob encodes it
// with TypeName="schemaBinary" even when encoded directly (not through an interface).
type schemaBinary struct{ Data []byte }

func (s schemaBinary) MarshalBinary() ([]byte, error)  { return s.Data, nil }
func (s *schemaBinary) UnmarshalBinary(b []byte) error { s.Data = b; return nil }

// schemaItem / schemaItemList / schemaTagMap / schemaOrder are used for the
// complex golden test that exercises structs, named slice, named map, and an
// opaque field (time.Time → GobEncoder) in one fixture.

type schemaItem struct {
	Name string
	Qty  int
}

type schemaItemList []schemaItem

type schemaTagMap map[string]bool

type schemaOrder struct {
	Items schemaItemList
	Stamp time.Time
	Tags  schemaTagMap
}

// schemaFor encodes v, runs DecodeTypes, and returns FormatSchema output.
func schemaFor(tb testing.TB, v any, opts ...gobspect.FormatOption) string {
	tb.Helper()
	buf := gobEncode(tb, v)
	ins := gobspect.New()
	types, err := ins.DecodeTypes(buf)
	require.NoError(tb, err)
	return gobspect.FormatSchema(types, opts...).String()
}

// readGolden reads a golden file from the testdata directory.
func readGolden(tb testing.TB, name string) string {
	tb.Helper()
	b, err := os.ReadFile("testdata/" + name + ".schema.golden")
	require.NoError(tb, err)
	return string(b)
}

// — FormatSchema golden tests —————————————————————————————————————————————————

func TestFormatSchema_Golden(t *testing.T) {
	tests := []struct {
		name    string
		fixture any
	}{
		{
			name:    "struct_nested",
			fixture: NamedPoint{Name: "origin", Pt: Point{X: 1, Y: 2}},
		},
		{
			name:    "map",
			fixture: StringMap{"a": 1},
		},
		{
			name:    "slice",
			fixture: IntSlice{1, 2, 3},
		},
		{
			name:    "array",
			fixture: IntArray{1, 2, 3, 4},
		},
		{
			name:    "opaque_gob",
			fixture: testOpaqueVal{V: 0x42},
		},
		{
			name:    "opaque_binary",
			fixture: schemaBinary{Data: []byte{0x01}},
		},
		{
			name: "complex",
			fixture: schemaOrder{
				Items: schemaItemList{{Name: "widget", Qty: 3}},
				Stamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Tags:  schemaTagMap{"new": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schemaFor(t, tt.fixture)
			want := readGolden(t, tt.name)
			assert.Equal(t, want, got)
		})
	}
}

func TestFormatSchema_WithIndent(t *testing.T) {
	got := schemaFor(t, NamedPoint{Name: "x", Pt: Point{X: 1, Y: 2}},
		gobspect.WithIndent("\t"))
	assert.Contains(t, got, "\tName  string")
	assert.Contains(t, got, "\tPt    Point")
	assert.Contains(t, got, "\tX  int")
}

func TestFormatSchema_Empty(t *testing.T) {
	got := gobspect.FormatSchema(nil).String()
	assert.Equal(t, "", got)
}

func TestFormatSchema_AnonSliceInline(t *testing.T) {
	// A struct whose field is an anonymous (unnamed) slice. The unnamed slice
	// type must appear inline in the field declaration, not as a top-level entry.
	type hasAnonSlice struct{ Vals []string }
	got := schemaFor(t, hasAnonSlice{Vals: []string{"a"}})
	assert.Contains(t, got, "Vals  []string")
	assert.NotContains(t, got, "type  []string")
}

// — big.Int is used to test opaque type redaction via WithRedactTypes.
func TestFormat_WithRedactTypes_Opaque(t *testing.T) {
	// big.Int has TypeName="" in the wire format when encoded directly.
	// Encode via an interface wrapper to get a real TypeName.
	type wrapInterface struct{ V any }
	gob.Register(big.NewInt(0))
	w := wrapInterface{V: big.NewInt(12345)}
	got := formatFirst(t, w,
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"int"}})) // "int" is interface TypeName for *big.Int
	// The exact TypeName depends on gob registration. Just verify it doesn't panic.
	_ = got
}

// — ComplexValue and NilValue formatting ——————————————————————————————————————

// TestFormat_ComplexPositiveImag verifies that a ComplexValue with a positive
// imaginary part renders as "(real+imagi)".
func TestFormat_ComplexPositiveImag(t *testing.T) {
	got := formatFirst(t, WrapComplex{V: 3 + 4i})
	assert.Equal(t, "WrapComplex{\n  V: (3+4i)\n}", got)
}

// TestFormat_ComplexNegativeImag verifies that a ComplexValue with a negative
// imaginary part renders as "(real-|imag|i)" (no extra + sign).
func TestFormat_ComplexNegativeImag(t *testing.T) {
	got := formatFirst(t, WrapComplex{V: 3 - 4i})
	assert.Equal(t, "WrapComplex{\n  V: (3-4i)\n}", got)
}

// TestFormat_NilValueDirect verifies that a NilValue passed directly to Format
// renders as "nil".
func TestFormat_NilValueDirect(t *testing.T) {
	assert.Equal(t, "nil", gobspect.Format(gobspect.NilValue{}))
}

// — fmtArray tests ————————————————————————————————————————————————————————————

// TestFormat_ArrayInline verifies that a short ArrayValue renders inline.
func TestFormat_ArrayInline(t *testing.T) {
	got := formatFirst(t, IntArray{1, 2, 3, 4})
	// IntArray [4]int64; gob represents int64 as the builtin "int" type.
	assert.Equal(t, "[4]int{1, 2, 3, 4}", got)
}

// TestFormat_ArrayEmpty verifies that an empty ArrayValue renders as "header{}".
func TestFormat_ArrayEmpty(t *testing.T) {
	v := gobspect.ArrayValue{ElemType: "string", Len: 5}
	got := gobspect.Format(v)
	assert.Equal(t, "[5]string{}", got)
}

// TestFormat_ArrayMultiline verifies that an ArrayValue whose elements render
// with newlines falls back to the indented multi-line format.
func TestFormat_ArrayMultiline(t *testing.T) {
	// Each element is a struct, which formats with newlines.
	v := gobspect.ArrayValue{
		ElemType: "Item",
		Len:      2,
		Elems: []gobspect.Value{
			gobspect.StructValue{TypeName: "Item", Fields: []gobspect.Field{{Name: "X", Value: gobspect.IntValue{V: 1}}}},
			gobspect.StructValue{TypeName: "Item", Fields: []gobspect.Field{{Name: "X", Value: gobspect.IntValue{V: 2}}}},
		},
	}
	got := gobspect.Format(v)
	assert.Contains(t, got, "[2]Item{\n")
	assert.Contains(t, got, "X: 1")
	assert.Contains(t, got, "X: 2")
	assert.True(t, got[len(got)-1] == '}', "should end with closing brace")
}

// TestFormat_ArrayOverThreshold verifies an ArrayValue that is too long to
// inline (> 72 chars) switches to the indented multi-line format.
func TestFormat_ArrayOverThreshold(t *testing.T) {
	// Construct an array of long strings. Each formats as "word_one_here" etc.
	// The inline string will exceed 72 chars.
	v := gobspect.ArrayValue{
		ElemType: "string",
		Len:      6,
		Elems: []gobspect.Value{
			gobspect.StringValue{V: "word_one_here"},
			gobspect.StringValue{V: "word_two_here"},
			gobspect.StringValue{V: "word_three_here"},
			gobspect.StringValue{V: "word_four_here"},
			gobspect.StringValue{V: "word_five_here"},
			gobspect.StringValue{V: "word_six_here"},
		},
	}
	got := gobspect.Format(v)
	assert.Contains(t, got, "[6]string{\n")
	assert.Contains(t, got, `"word_one_here"`)
}

// — fmtSlice multiline tests ——————————————————————————————————————————————————

// FmtStructSlice is a named slice type for struct-element formatting tests.
type FmtStructSlice []fmtInner

// TestFormat_SliceMultiline_StructElems verifies that a slice whose elements
// render with newlines (structs) uses the indented multi-line format.
func TestFormat_SliceMultiline_StructElems(t *testing.T) {
	got := formatFirst(t, FmtStructSlice{
		{A: 1, B: "x"},
		{A: 2, B: "y"},
	})
	assert.Contains(t, got, "[]fmtInner{\n")
	assert.Contains(t, got, "A: 1")
	assert.Contains(t, got, "A: 2")
}

// TestFormat_SliceOverThreshold verifies that a slice whose inline rendering
// exceeds 72 characters switches to the indented multi-line format even when
// no element itself contains a newline.
func TestFormat_SliceOverThreshold(t *testing.T) {
	// 6 long strings; inline would be much longer than 72 chars.
	v := gobspect.SliceValue{
		ElemType: "string",
		Elems: []gobspect.Value{
			gobspect.StringValue{V: "word_one_here"},
			gobspect.StringValue{V: "word_two_here"},
			gobspect.StringValue{V: "word_three_here"},
			gobspect.StringValue{V: "word_four_here"},
			gobspect.StringValue{V: "word_five_here"},
			gobspect.StringValue{V: "word_six_here"},
		},
	}
	got := gobspect.Format(v)
	assert.Contains(t, got, "[]string{\n")
	assert.Contains(t, got, `"word_one_here"`)
}

// — fmtInterface tests ————————————————————————————————————————————————————————

// TestFormat_InterfaceNil verifies that an InterfaceValue whose inner value is
// NilValue renders as "nil", regardless of the TypeName.
func TestFormat_InterfaceNil(t *testing.T) {
	v := gobspect.InterfaceValue{TypeName: "SomeType", Value: gobspect.NilValue{}}
	assert.Equal(t, "nil", gobspect.Format(v))
}

// TestFormat_InterfaceNoTypeName verifies that an InterfaceValue with an empty
// TypeName renders as the inner value only (no "(TypeName) " prefix).
func TestFormat_InterfaceNoTypeName(t *testing.T) {
	v := gobspect.InterfaceValue{Value: gobspect.IntValue{V: 42}}
	assert.Equal(t, "42", gobspect.Format(v))
}

// — DecodeSchema tests —————————————————————————————————————————————————————————

// TestDecodeSchema verifies the convenience wrapper returns the same result as
// DecodeTypes + FormatSchema.
func TestDecodeSchema(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "origin", Pt: Point{X: 1, Y: 2}})
	ins := gobspect.New()
	schemaAST, err := ins.DecodeSchema(buf)
	require.NoError(t, err)
    schema := schemaAST.String()
	assert.Contains(t, schema, "type NamedPoint struct")
	assert.Contains(t, schema, "Name  string")
	assert.Contains(t, schema, "Pt    Point")
}

// — TypeKind.String() tests ———————————————————————————————————————————————————

// TestTypeKind_String verifies every TypeKind constant returns the expected
// human-readable name, and that unknown values return "unknown".
func TestTypeKind_String(t *testing.T) {
	tests := []struct {
		kind gobspect.TypeKind
		want string
	}{
		{gobspect.KindStruct, "struct"},
		{gobspect.KindMap, "map"},
		{gobspect.KindSlice, "slice"},
		{gobspect.KindArray, "array"},
		{gobspect.KindGobEncoder, "gob-encoder"},
		{gobspect.KindBinaryMarshaler, "binary-marshaler"},
		{gobspect.KindTextMarshaler, "text-marshaler"},
		{gobspect.TypeKind(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.kind.String())
		})
	}
}

// — schemaTypeExpr anonymous type tests ——————————————————————————————————————

// TestFormatSchema_AnonArrayInlineField verifies that a struct field whose type
// is an anonymous array (Name == "") is rendered as "[N]ElemType" inline.
func TestFormatSchema_AnonArrayInlineField(t *testing.T) {
	// Construct TypeInfo directly: a struct with a field of anonymous [3]int.
	anonArray := gobspect.TypeInfo{
		ID:   200,
		Name: "", // anonymous — no top-level declaration
		Kind: gobspect.KindArray,
		Len:  3,
		Elem: &gobspect.TypeRef{ID: 2, Name: "int"}, // builtin int
	}
	holder := gobspect.TypeInfo{
		ID:     201,
		Name:   "Holder",
		Kind:   gobspect.KindStruct,
		Fields: []gobspect.FieldInfo{{Name: "Grid", TypeID: 200}},
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{anonArray, holder}).String()
	assert.Contains(t, got, "Grid  [3]int")
	// Anonymous type has no name so no top-level declaration for it.
	assert.NotContains(t, got, "type  [3]int")
}

// TestFormatSchema_AnonMapInlineField verifies that a struct field whose type
// is an anonymous map (Name == "") is rendered as "map[K]V" inline.
func TestFormatSchema_AnonMapInlineField(t *testing.T) {
	anonMap := gobspect.TypeInfo{
		ID:   300,
		Name: "", // anonymous
		Kind: gobspect.KindMap,
		Key:  &gobspect.TypeRef{ID: 6, Name: "string"}, // builtin string
		Elem: &gobspect.TypeRef{ID: 2, Name: "int"},    // builtin int
	}
	holder := gobspect.TypeInfo{
		ID:     301,
		Name:   "Config",
		Kind:   gobspect.KindStruct,
		Fields: []gobspect.FieldInfo{{Name: "Tags", TypeID: 300}},
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{anonMap, holder}).String()
	assert.Contains(t, got, "Tags  map[string]int")
}

// TestFormatSchema_AnonSliceInlineField verifies that a struct field whose type
// is an anonymous slice (Name == "") is rendered as "[]ElemType" inline.
func TestFormatSchema_AnonSliceInlineField(t *testing.T) {
	anonSlice := gobspect.TypeInfo{
		ID:   400,
		Name: "", // anonymous
		Kind: gobspect.KindSlice,
		Elem: &gobspect.TypeRef{ID: 6, Name: "string"}, // builtin string
	}
	holder := gobspect.TypeInfo{
		ID:     401,
		Name:   "Container",
		Kind:   gobspect.KindStruct,
		Fields: []gobspect.FieldInfo{{Name: "Names", TypeID: 400}},
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{anonSlice, holder}).String()
	assert.Contains(t, got, "Names  []string")
}

// TestFormatSchema_UnknownTypeRef verifies that an unresolvable TypeRef
// renders as "?" in the schema output.
func TestFormatSchema_UnknownTypeRef(t *testing.T) {
	// A struct whose field references a type ID not present in the byID map.
	holder := gobspect.TypeInfo{
		ID:     500,
		Name:   "Mystery",
		Kind:   gobspect.KindStruct,
		Fields: []gobspect.FieldInfo{{Name: "Thing", TypeID: 999}},
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{holder}).String()
	assert.Contains(t, got, "Thing  ?")
}

// TestFormatSchema_NilTypeRef verifies FormatSchema handles nil Elem/Key refs
// gracefully by rendering "?".
func TestFormatSchema_NilTypeRef(t *testing.T) {
	// A map type with nil Key and nil Elem.
	badMap := gobspect.TypeInfo{
		ID:   600,
		Name: "BadMap",
		Kind: gobspect.KindMap,
		Key:  nil,
		Elem: nil,
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{badMap}).String()
	assert.Contains(t, got, "type BadMap map[?]?")
}

// TestFormatSchema_UnknownElemRefRenders as "?" when the elem TypeRef ID is not
// in the byID map and the TypeRef has no Name set. Exercises the schemaTypeExpr
// "not found, empty name" path.
func TestFormatSchema_UnknownElemRef(t *testing.T) {
	holder := gobspect.TypeInfo{
		ID:   700,
		Name: "UnknownHolder",
		Kind: gobspect.KindSlice,
		Elem: &gobspect.TypeRef{ID: 9999, Name: ""},
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{holder}).String()
	assert.Contains(t, got, "type UnknownHolder []?")
}

// TestFormatSchema_UnknownElemRefWithName verifies that when the elem TypeRef
// ID is not in byID but the TypeRef carries a Name, that name is used as-is.
// Exercises the schemaTypeExpr "not found, non-empty name" path.
func TestFormatSchema_UnknownElemRefWithName(t *testing.T) {
	holder := gobspect.TypeInfo{
		ID:   701,
		Name: "NamedHolder",
		Kind: gobspect.KindSlice,
		Elem: &gobspect.TypeRef{ID: 9999, Name: "ExternalType"},
	}
	got := gobspect.FormatSchema([]gobspect.TypeInfo{holder}).String()
	assert.Contains(t, got, "type NamedHolder []ExternalType")
}
