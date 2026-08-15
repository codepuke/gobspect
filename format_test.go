package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strings"
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
	vals, err := ins.Stream(buf).Collect()
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

func TestFormat_WithMaxBytes_PrintableUTF8(t *testing.T) {
	// 1 MiB of printable UTF-8, which would otherwise take the shortcut that
	// renders byte slices as a string and bypasses MaxBytes entirely.
	large := make([]byte, 1024*1024)
	for i := range large {
		large[i] = 'A'
	}

	got := formatFirst(t, WrapBytes{V: large}, gobspect.WithMaxBytes(64))

	// Assert on the length rather than the content: a failure here must not
	// dump a megabyte of output.
	assert.Less(t, len(got), 1024, "Output should be bounded and not render the entire 1 MiB slice")
	assert.Contains(t, got, "…")
}

func TestFormat_BytesNonPrintable(t *testing.T) {
	got := formatFirst(t, WrapBytes{V: []byte{0xDE, 0xAD, 0xBE, 0xEF}})
	assert.Equal(t, "WrapBytes{\n  V: deadbeef\n}", got)
}

func TestFormat_EmptyBytes(t *testing.T) {
	nilGot := gobspect.Format(gobspect.BytesValue{V: nil})
	emptyGot := gobspect.Format(gobspect.BytesValue{V: []byte{}})

	assert.Equal(t, "[]", nilGot)
	assert.Equal(t, "[]", emptyGot)
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

// redactNestedInner / redactNestedOuter exercise key redaction when the redacted
// field's rendered form is multi-line (i.e. contains newlines and indentation).
type redactNestedInner struct {
	Secret string
	Extra  string
}

type redactNestedOuter struct {
	Public string
	Inner  redactNestedInner
}

// TestFormat_WithRedactKeys_NestedStructMultiLine documents and tests the
// behavior when a redacted field renders as a multi-line struct.
//
// Decision: when TextLength == 0 and the rendered value is multi-line, redact
// emits a short fixed placeholder ("***", 3 chars) rather than counting the
// rune length of the full indented rendering (which includes newlines,
// indentation, and braces — none of which reflect meaningful value length).
// Setting TextLength > 0 always overrides this and emits exactly that many
// fill characters.
func TestFormat_WithRedactKeys_NestedStructMultiLine(t *testing.T) {
	v := redactNestedOuter{
		Public: "visible",
		Inner:  redactNestedInner{Secret: "topsecret", Extra: "alsoSecret"},
	}

	t.Run("length_preserving_multiline_uses_short_placeholder", func(t *testing.T) {
		// TextLength == 0: multi-line rendered value must NOT produce a
		// long flat run of asterisks. Expect exactly "***".
		got := formatFirst(t, v,
			gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Inner"}}))
		assert.Equal(t, "redactNestedOuter{\n  Public: \"visible\"\n  Inner: ***\n}", got)
		assert.NotContains(t, got, "topsecret")
		assert.NotContains(t, got, "alsoSecret")
		// Must not produce a long run of asterisks (old buggy behavior was 71+).
		assert.NotContains(t, got, "****")
	})

	t.Run("explicit_text_length_respected_even_when_multiline", func(t *testing.T) {
		// TextLength > 0 always wins regardless of multi-line.
		got := formatFirst(t, v,
			gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Inner"}, TextLength: 5}))
		assert.Contains(t, got, "Inner: *****")
		assert.NotContains(t, got, "topsecret")
	})

	t.Run("single_line_field_still_length_preserving", func(t *testing.T) {
		// Non-nested (single-line) fields continue to preserve rendered length.
		// "topsecret" renders as `"topsecret"` (11 chars) → 11 stars.
		v2 := redactNestedOuter{Public: "visible", Inner: redactNestedInner{Secret: "topsecret"}}
		got := formatFirst(t, v2,
			gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Secret"}, Char: '*'}))
		assert.Contains(t, got, `Secret: ***********`)
		assert.NotContains(t, got, "topsecret")
	})
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
	vals, err := ins.Stream(buf).Collect()
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

// schemaFor encodes v, drains the stream, and returns FormatSchema output.
func schemaFor(tb testing.TB, v any, opts ...gobspect.SchemaFormatOption) string {
	tb.Helper()
	buf := gobEncode(tb, v)
	ins := gobspect.New()
	s := ins.Stream(buf)
	_, err := s.Collect()
	require.NoError(tb, err)
	return gobspect.FormatSchema(s.Types()).Format(opts...)
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
		gobspect.SchemaWithIndent("\t"))
	assert.Contains(t, got, "\tName  string")
	assert.Contains(t, got, "\tPt    Point")
	assert.Contains(t, got, "\tX  int")
}

func TestFormatSchema_Empty(t *testing.T) {
	got := gobspect.FormatSchema(nil).String()
	assert.Equal(t, "", got)
}

// TestSchemaJSON covers the machine-readable Schema.JSON output. This is a
// stable external contract consumed by downstream tooling so we assert the
// exact shape.
func TestSchemaJSON(t *testing.T) {
	schema := gobspect.FormatSchema([]gobspect.TypeInfo{
		{
			ID: 65, Name: "Point", Kind: gobspect.KindStruct,
			Fields: []gobspect.FieldInfo{
				{Name: "X", TypeID: 2},
				{Name: "Y", TypeID: 2},
			},
		},
		{
			ID: 66, Name: "Names", Kind: gobspect.KindSlice,
			Elem: &gobspect.TypeRef{ID: 6, Name: "string"},
		},
		{
			ID: 67, Name: "UserID", Kind: gobspect.KindGobEncoder,
		},
	})

	out, err := schema.JSONIndent("", "  ")
	require.NoError(t, err)

	s := string(out)
	// Top-level array.
	assert.True(t, strings.HasPrefix(strings.TrimSpace(s), "["))

	// Struct entries include fields with name+type keys.
	assert.Contains(t, s, `"name": "Point"`)
	assert.Contains(t, s, `"kind": "struct"`)
	assert.Contains(t, s, `"name": "X"`)
	assert.Contains(t, s, `"type": "int"`)

	// Slice entries include a "target" rendering of the inline type.
	assert.Contains(t, s, `"name": "Names"`)
	assert.Contains(t, s, `"target": "[]string"`)

	// Opaque types include the annotation rather than fields.
	assert.Contains(t, s, `"name": "UserID"`)
	assert.Contains(t, s, `"annotation": "GobEncoder"`)

	// Compact form round-trips through encoding/json.
	compact, err := schema.JSON()
	require.NoError(t, err)
	assert.NotContains(t, string(compact), "\n")
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

// TestFormat_ComplexNegativeZeroImag verifies that a ComplexValue with a
// negative-zero imaginary part renders as "(real-0i)" — not "(real+-0i)".
func TestFormat_ComplexNegativeZeroImag(t *testing.T) {
	got := formatFirst(t, WrapComplex{V: complex(1, math.Copysign(0, -1))})
	assert.NotContains(t, got, "+-", "negative-zero imag must not produce '+-'")
	assert.Equal(t, "WrapComplex{\n  V: (1-0i)\n}", got)
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

// TestSchema verifies that Stream.Schema returns the same result as
// Stream.Collect + FormatSchema.
func TestSchema(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "origin", Pt: Point{X: 1, Y: 2}})
	ins := gobspect.New()
	schemaAST, err := ins.Stream(buf).Schema()
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

// TestFormatSchema_UnknownElemRef verifies that an elem TypeRef whose ID is
// absent from the byID map and which carries no Name renders as "?".
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

// TestFormatSchema_GenericTypeNotDropped verifies that a user-defined named type
// whose reflect name contains brackets due to a generic instantiation (e.g.
// "Pair[int,string]") is NOT excluded from the schema output. The old filter
// used strings.ContainsAny(name, "[]* ") which incorrectly dropped such names.
func TestFormatSchema_GenericTypeNotDropped(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
	}{
		{"generic_struct", "Pair[int,string]"},
		{"generic_with_package", "main.Wrapper[int]"},
		{"generic_multiple_params", "Result[string,error]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ti := gobspect.TypeInfo{
				ID:   100,
				Name: tt.typeName,
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldInfo{
					{Name: "Value", TypeID: 5}, // string builtin
				},
			}
			got := gobspect.FormatSchema([]gobspect.TypeInfo{ti}).String()
			assert.Contains(t, got, tt.typeName,
				"generic type %q should appear in schema output", tt.typeName)
		})
	}
}

// TestFormatSchema_MechanicalNamesStillDropped verifies that mechanically-generated
// anonymous type names ([]T, [N]T, map[K]V, *T) are still excluded from the
// top-level schema even after the generic-name fix.
func TestFormatSchema_MechanicalNamesStillDropped(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
	}{
		{"slice", "[]int"},
		{"named_slice", "[]main.Item"},
		{"array", "[4]int"},
		{"map", "map[string]int"},
		{"map_named_val", "map[string]main.Item"},
		{"pointer", "*main.Thing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ti := gobspect.TypeInfo{
				ID:   200,
				Name: tt.typeName,
				Kind: gobspect.KindStruct,
			}
			got := gobspect.FormatSchema([]gobspect.TypeInfo{ti}).String()
			assert.NotContains(t, got, tt.typeName,
				"mechanical type name %q should be excluded from schema output", tt.typeName)
		})
	}
}

// — FloatValue formatting ——————————————————————————————————————————————————————

// TestFormat_FloatValue verifies that FloatValue renders with full precision and
// in a form that is always distinguishable from an IntValue.
func TestFormat_FloatValue(t *testing.T) {
	tests := []struct {
		name string
		v    float64
		// wantContains is a substring that must appear in the output (used for
		// special values whose exact rendering may be platform-dependent).
		wantContains string
		// wantExact, when non-empty, is the exact expected string.
		wantExact string
		// notInt asserts the output does NOT look like a bare integer.
		notInt bool
	}{
		{
			name:      "integer_valued_float",
			v:         1.0,
			wantExact: "1.0",
			notInt:    true,
		},
		{
			name:      "pi_full_precision",
			v:         math.Pi,
			wantExact: "3.141592653589793",
			notInt:    true,
		},
		{
			name:      "small_exponent",
			v:         1e-20,
			wantExact: "1e-20",
			notInt:    true,
		},
		{
			name:         "nan",
			v:            math.NaN(),
			wantContains: "NaN",
		},
		{
			name:         "positive_inf",
			v:            math.Inf(1),
			wantContains: "Inf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gobspect.Format(gobspect.FloatValue{V: tt.v})
			if tt.wantExact != "" {
				assert.Equal(t, tt.wantExact, got)
			}
			if tt.wantContains != "" {
				assert.Contains(t, got, tt.wantContains)
			}
			if tt.notInt {
				// Must not be indistinguishable from an integer: must contain
				// '.', 'e', 'E', 'N' (NaN), or 'I' (Inf).
				hasFloat := false
				for _, ch := range got {
					if ch == '.' || ch == 'e' || ch == 'E' || ch == 'N' || ch == 'I' {
						hasFloat = true
						break
					}
				}
				assert.True(t, hasFloat, "float output %q is indistinguishable from an integer", got)
			}
		})
	}
}

// BenchmarkFmtMapLargeStructKeys measures the cost of formatting a MapValue
// with 10 000 struct-valued keys. Before the precomputed-key optimisation the
// sort comparator called fmtValue on every comparison, rendering each key
// O(n log n) times instead of once.
func BenchmarkFmtMapLargeStructKeys(b *testing.B) {
	const n = 10_000
	entries := make([]gobspect.MapEntry, n)
	for i := range n {
		entries[i] = gobspect.MapEntry{
			Key: gobspect.StructValue{
				TypeName: "Key",
				Fields: []gobspect.Field{
					{Name: "A", Value: gobspect.IntValue{V: int64(i)}},
					{Name: "B", Value: gobspect.StringValue{V: fmt.Sprintf("k%d", i)}},
				},
			},
			Value: gobspect.StringValue{V: fmt.Sprintf("v%d", i)},
		}
	}
	mv := gobspect.MapValue{
		KeyType:  "Key",
		ElemType: "string",
		Entries:  entries,
	}
	b.ResetTimer()
	for b.Loop() {
		_ = gobspect.Format(mv)
	}
}

// — FormatTo tests ——————————————————————————————————————————————————————————

// limitWriter is a writer that errors after writing N bytes total.
type limitWriter struct {
	limit int
	n     int
}

var errLimitReached = errors.New("limit reached")

func (w *limitWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.n
	if remaining <= 0 {
		return 0, errLimitReached
	}
	if len(p) > remaining {
		w.n += remaining
		return remaining, errLimitReached
	}
	w.n += len(p)
	return len(p), nil
}

// TestFormatTo_EquivalentToFormat verifies that FormatTo produces byte-identical
// output to Format for all existing test cases.
func TestFormatTo_EquivalentToFormat(t *testing.T) {
	tests := []struct {
		name  string
		value func(tb testing.TB) gobspect.Value
		opts  []gobspect.FormatOption
	}{
		{
			name:  "bool_true",
			value: func(tb testing.TB) gobspect.Value { return gobspect.BoolValue{V: true} },
		},
		{
			name:  "bool_false",
			value: func(tb testing.TB) gobspect.Value { return gobspect.BoolValue{V: false} },
		},
		{
			name:  "int_value",
			value: func(tb testing.TB) gobspect.Value { return gobspect.IntValue{V: -42} },
		},
		{
			name:  "uint_value",
			value: func(tb testing.TB) gobspect.Value { return gobspect.UintValue{V: 100} },
		},
		{
			name:  "float_integer_valued",
			value: func(tb testing.TB) gobspect.Value { return gobspect.FloatValue{V: 1.0} },
		},
		{
			name:  "float_pi",
			value: func(tb testing.TB) gobspect.Value { return gobspect.FloatValue{V: math.Pi} },
		},
		{
			name: "complex_positive_imag",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.ComplexValue{Real: 3, Imag: 4}
			},
		},
		{
			name: "complex_negative_imag",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.ComplexValue{Real: 3, Imag: -4}
			},
		},
		{
			name:  "string_value",
			value: func(tb testing.TB) gobspect.Value { return gobspect.StringValue{V: "hello"} },
		},
		{
			name:  "bytes_empty",
			value: func(tb testing.TB) gobspect.Value { return gobspect.BytesValue{V: nil} },
		},
		{
			name: "bytes_hex",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.BytesValue{V: []byte{0xde, 0xad, 0xbe, 0xef}}
			},
		},
		{
			name: "bytes_printable_utf8",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.BytesValue{V: []byte("hello world")}
			},
		},
		{
			name:  "nil_value",
			value: func(tb testing.TB) gobspect.Value { return gobspect.NilValue{} },
		},
		{
			name: "interface_nil",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.InterfaceValue{TypeName: "T", Value: gobspect.NilValue{}}
			},
		},
		{
			name: "interface_with_value",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.InterfaceValue{TypeName: "T", Value: gobspect.IntValue{V: 7}}
			},
		},
		{
			name: "opaque_with_decoded_string",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.OpaqueValue{TypeName: "time.Time", Raw: []byte{1, 2, 3}, Decoded: "2024-01-01T00:00:00Z"}
			},
		},
		{
			name: "opaque_raw_fallback",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.OpaqueValue{TypeName: "myType", Raw: []byte{0xab, 0xcd}}
			},
		},
		{
			name: "struct_empty",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.StructValue{TypeName: "Empty"}
			},
		},
		{
			name: "struct_nested",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, fmtOuter{X: fmtInner{A: 42, B: "hello"}, Y: 3.14})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
		},
		{
			name: "map_short_inline",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, StringMap{"a": 1, "b": 2})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
		},
		{
			name: "map_long_indented",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, StringMap{
					"alpha": 100, "beta": 200, "delta": 300, "gamma": 400, "zeta": 500,
				})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
		},
		{
			name: "slice_inline",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, IntSlice{10, 20, 30})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
		},
		{
			name: "array_inline",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, IntArray{1, 2, 3, 4})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
		},
		{
			name: "slice_empty",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.SliceValue{ElemType: "int"}
			},
		},
		{
			name: "array_empty",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.ArrayValue{ElemType: "string", Len: 5}
			},
		},
		{
			name: "with_indent_tab",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, fmtOuter{X: fmtInner{A: 1, B: "x"}, Y: 0})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
			opts: []gobspect.FormatOption{gobspect.WithIndent("\t")},
		},
		{
			name: "bytes_base64",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.BytesValue{V: []byte{0xde, 0xad}}
			},
			opts: []gobspect.FormatOption{gobspect.WithBytesFormat(gobspect.BytesBase64)},
		},
		{
			name: "bytes_literal",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.BytesValue{V: []byte{0xde, 0xad}}
			},
			opts: []gobspect.FormatOption{gobspect.WithBytesFormat(gobspect.BytesLiteral)},
		},
		{
			name: "with_max_bytes_truncation",
			value: func(tb testing.TB) gobspect.Value {
				return gobspect.BytesValue{V: []byte{0x01, 0x02, 0x03, 0x04, 0x05}}
			},
			opts: []gobspect.FormatOption{gobspect.WithMaxBytes(3)},
		},
		{
			name: "redact_keys",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, secretStruct{Username: "alice", Password: "s3cr3t", Age: 30})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
			opts: []gobspect.FormatOption{
				gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}}),
			},
		},
		{
			name: "redact_types",
			value: func(tb testing.TB) gobspect.Value {
				buf := gobEncode(tb, holdsTypes{S: sensitiveType{V: "secret"}, N: normalType{V: "public"}})
				ins := gobspect.New()
				vals, err := ins.Stream(buf).Collect()
				require.NoError(tb, err)
				require.Len(tb, vals, 1)
				return vals[0]
			},
			opts: []gobspect.FormatOption{
				gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"sensitiveType"}}),
			},
		},
		{
			name: "unknown_value_type",
			value: func(tb testing.TB) gobspect.Value {
				// Use a known value so Format doesn't panic; unknown path is covered by Format's default case.
				return gobspect.IntValue{V: 0}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.value(t)
			want := gobspect.Format(v, tt.opts...)
			var buf bytes.Buffer
			err := gobspect.FormatTo(&buf, v, tt.opts...)
			assert.NoError(t, err)
			assert.Equal(t, want, buf.String(), "FormatTo output must be byte-identical to Format output")
		})
	}
}

// TestFormatTo_LimitWriter verifies that FormatTo propagates write errors and stops early.
func TestFormatTo_LimitWriter(t *testing.T) {
	// Use a value that will produce at least several bytes of output.
	v := gobspect.StructValue{
		TypeName: "MyStruct",
		Fields: []gobspect.Field{
			{Name: "Alpha", Value: gobspect.StringValue{V: "hello world"}},
			{Name: "Beta", Value: gobspect.IntValue{V: 42}},
			{Name: "Gamma", Value: gobspect.FloatValue{V: 3.14}},
		},
	}

	// Compute the full output so we know how long it is.
	fullOutput := gobspect.Format(v)
	require.Greater(t, len(fullOutput), 5, "test value must produce more than 5 bytes")

	// Limit to 5 bytes — much less than the full output.
	lw := &limitWriter{limit: 5}
	err := gobspect.FormatTo(lw, v)

	// Must return a non-nil error.
	assert.Error(t, err, "FormatTo should return an error when the writer fails")

	// Must not have written more bytes than the limit.
	assert.LessOrEqual(t, lw.n, 5, "FormatTo must stop writing after the limit is reached")

	// The error must not be nil (i.e. writing stopped, not full output).
	assert.Less(t, lw.n, len(fullOutput), "FormatTo must not write the full output when the writer errors")
}

// TestFormatTo_StringsBuilderNilError verifies that FormatTo on a strings.Builder
// returns nil (strings.Builder.Write never errors).
func TestFormatTo_StringsBuilderNilError(t *testing.T) {
	v := gobspect.StructValue{
		TypeName: "T",
		Fields: []gobspect.Field{
			{Name: "X", Value: gobspect.IntValue{V: 1}},
		},
	}
	var buf bytes.Buffer
	err := gobspect.FormatTo(&buf, v)
	assert.NoError(t, err)
	assert.Equal(t, gobspect.Format(v), buf.String())
}

// — WithColor tests ————————————————————————————————————————————————————————————

// TestFormat_NoColorDefault verifies that Format with no options produces
// ANSI-escape-free output.
func TestFormat_NoColorDefault(t *testing.T) {
	v := gobspect.StructValue{
		TypeName: "MyStruct",
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "alice"}},
			{Name: "Age", Value: gobspect.IntValue{V: 30}},
		},
	}
	got := gobspect.Format(v)
	assert.NotContains(t, got, "\x1b[", "default Format must not emit ANSI escape codes")
	assert.Contains(t, got, "MyStruct{")
	assert.Contains(t, got, "Name: ")
}

// TestFormat_ANSIColor verifies that WithColor(ANSIColorScheme) wraps field
// names with the expected ANSI green escape and type headers with bold cyan.
func TestFormat_ANSIColor(t *testing.T) {
	v := gobspect.StructValue{
		TypeName: "MyStruct",
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "alice"}},
			{Name: "Age", Value: gobspect.IntValue{V: 30}},
		},
	}
	got := gobspect.Format(v, gobspect.WithColor(gobspect.ANSIColorScheme))

	assert.Contains(t, got, "\x1b[32mName\x1b[0m", "field name should be green")
	assert.Contains(t, got, "\x1b[32mAge\x1b[0m", "field name should be green")
	assert.Contains(t, got, "\x1b[1;36mMyStruct\x1b[0m{", "type header should be bold cyan (braces plain)")
	assert.Contains(t, got, "\x1b[33m\"alice\"\x1b[0m", "string value should be yellow")
	assert.Contains(t, got, "\x1b[35m30\x1b[0m", "number value should be magenta")
}

// TestFormat_CustomScheme verifies that a caller-provided scheme with XML-style
// tags produces expected tag-style output.
func TestFormat_CustomScheme(t *testing.T) {
	scheme := gobspect.ColorScheme{
		FieldName:  gobspect.Style{Prefix: "<field>", Suffix: "</field>"},
		TypeHeader: gobspect.Style{Prefix: "<type>", Suffix: "</type>"},
		String:     gobspect.Style{Prefix: "<str>", Suffix: "</str>"},
		Number:     gobspect.Style{Prefix: "<num>", Suffix: "</num>"},
	}
	v := gobspect.StructValue{
		TypeName: "Order",
		Fields: []gobspect.Field{
			{Name: "ID", Value: gobspect.IntValue{V: 42}},
			{Name: "Label", Value: gobspect.StringValue{V: "hello"}},
		},
	}
	got := gobspect.Format(v, gobspect.WithColor(scheme))
	assert.Contains(t, got, "<field>ID</field>")
	assert.Contains(t, got, "<field>Label</field>")
	assert.Contains(t, got, "<type>Order</type>{")
	assert.Contains(t, got, "<num>42</num>")
	assert.Contains(t, got, `<str>"hello"</str>`)
}

// TestFormat_ZeroValueSchemeIsNoop verifies that WithColor(ColorScheme{}) produces
// output byte-identical to calling Format with no options.
func TestFormat_ZeroValueSchemeIsNoop(t *testing.T) {
	v := gobspect.StructValue{
		TypeName: "Z",
		Fields: []gobspect.Field{
			{Name: "X", Value: gobspect.IntValue{V: 7}},
		},
	}
	plain := gobspect.Format(v)
	withNoop := gobspect.Format(v, gobspect.WithColor(gobspect.NoColorScheme))
	assert.Equal(t, plain, withNoop, "zero-valued ColorScheme must be identity")
}

// TestFormat_ANSIColor_Scalars exercises every scalar token type.
func TestFormat_ANSIColor_Scalars(t *testing.T) {
	tests := []struct {
		name      string
		value     gobspect.Value
		wantStyle string // ANSI prefix expected to appear in output
	}{
		{"bool_true", gobspect.BoolValue{V: true}, "\x1b[36m"},   // cyan
		{"bool_false", gobspect.BoolValue{V: false}, "\x1b[36m"}, // cyan
		{"int", gobspect.IntValue{V: -5}, "\x1b[35m"},            // magenta
		{"uint", gobspect.UintValue{V: 9}, "\x1b[35m"},           // magenta
		{"float", gobspect.FloatValue{V: 1.5}, "\x1b[35m"},       // magenta
		{"string", gobspect.StringValue{V: "x"}, "\x1b[33m"},     // yellow
		{"nil", gobspect.NilValue{}, "\x1b[36m"},                 // cyan
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gobspect.Format(tt.value, gobspect.WithColor(gobspect.ANSIColorScheme))
			assert.Contains(t, got, tt.wantStyle, "expected ANSI prefix in output for %s", tt.name)
			assert.Contains(t, got, "\x1b[0m", "expected ANSI reset in output for %s", tt.name)
		})
	}
}

// TestFormat_ANSIColor_OpaquePrefix verifies that opaque type prefix "(TypeName)"
// is wrapped in the OpaquePrefix (dim) style.
func TestFormat_ANSIColor_OpaquePrefix(t *testing.T) {
	v := gobspect.OpaqueValue{TypeName: "myType", Raw: []byte{0xab}}
	got := gobspect.Format(v, gobspect.WithColor(gobspect.ANSIColorScheme))
	// OpaquePrefix is dim (\x1b[2m)
	assert.Contains(t, got, "\x1b[2m(myType)\x1b[0m", "opaque prefix should be dim")
}

// TestFormat_ANSIColor_Bytes verifies that raw byte output is wrapped in dim style.
func TestFormat_ANSIColor_Bytes(t *testing.T) {
	v := gobspect.BytesValue{V: []byte{0xde, 0xad}}
	got := gobspect.Format(v, gobspect.WithColor(gobspect.ANSIColorScheme))
	assert.Contains(t, got, "\x1b[2m", "bytes should be dim")
}

// TestSchema_Format_WithColor verifies that Schema.Format with ANSIColorScheme
// contains ANSI codes for type names and field names.
func TestSchema_Format_WithColor(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "p", Pt: Point{X: 1, Y: 2}})
	ins := gobspect.New()
	s := ins.Stream(buf)
	_, err := s.Collect()
	require.NoError(t, err)
	schema := gobspect.FormatSchema(s.Types())

	got := schema.Format(gobspect.SchemaWithColor(gobspect.ANSIColorScheme))

	assert.Contains(t, got, "\x1b[1;36m", "schema type name should be bold cyan")
	assert.Contains(t, got, "\x1b[35mtype\x1b[0m", "schema 'type' keyword should be magenta")
	assert.Contains(t, got, "\x1b[32m", "schema field names should be green")
	plainGot := schema.Format()
	assert.NotContains(t, plainGot, "\x1b[", "plain schema.Format() should not contain ANSI codes")
}

// TestSchema_Format_NoColor verifies Schema.Format() (no options) matches Schema.String().
func TestSchema_Format_NoColor(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "p", Pt: Point{X: 1, Y: 2}})
	ins := gobspect.New()
	s := ins.Stream(buf)
	_, err := s.Collect()
	require.NoError(t, err)
	schema := gobspect.FormatSchema(s.Types())

	assert.Equal(t, schema.String(), schema.Format(), "Format() with no options must equal String()")
}

// TestSchema_Format_WithIndent verifies that Schema.Format(WithIndent("\t"))
// produces tab-indented output.
func TestSchema_Format_WithIndent(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "p", Pt: Point{X: 1, Y: 2}})
	ins := gobspect.New()
	s := ins.Stream(buf)
	_, err := s.Collect()
	require.NoError(t, err)
	schema := gobspect.FormatSchema(s.Types())

	got := schema.Format(gobspect.SchemaWithIndent("\t"))

	assert.Contains(t, got, "\tName", "fields should be tab-indented")
	assert.Contains(t, got, "\tPt", "fields should be tab-indented")
	assert.NotContains(t, got, "  Name", "should not use two-space indent when tab is specified")
}

// TestSchema_FormatTo_WritesCorrectly verifies that FormatTo writes output
// byte-identical to Format.
func TestSchema_FormatTo_WritesCorrectly(t *testing.T) {
	buf := gobEncode(t, NamedPoint{Name: "p", Pt: Point{X: 1, Y: 2}})
	ins := gobspect.New()
	s := ins.Stream(buf)
	_, err := s.Collect()
	require.NoError(t, err)
	schema := gobspect.FormatSchema(s.Types())

	t.Run("plain_matches_format", func(t *testing.T) {
		want := schema.Format()
		var out bytes.Buffer
		werr := schema.FormatTo(&out)
		require.NoError(t, werr)
		assert.Equal(t, want, out.String(), "FormatTo must be byte-identical to Format")
	})

	t.Run("with_indent_matches_format", func(t *testing.T) {
		want := schema.Format(gobspect.SchemaWithIndent("\t"))
		var out bytes.Buffer
		werr := schema.FormatTo(&out, gobspect.SchemaWithIndent("\t"))
		require.NoError(t, werr)
		assert.Equal(t, want, out.String(), "FormatTo with WithIndent must be byte-identical to Format with WithIndent")
	})

	t.Run("with_color_matches_format", func(t *testing.T) {
		want := schema.Format(gobspect.SchemaWithColor(gobspect.ANSIColorScheme))
		var out bytes.Buffer
		werr := schema.FormatTo(&out, gobspect.SchemaWithColor(gobspect.ANSIColorScheme))
		require.NoError(t, werr)
		assert.Equal(t, want, out.String(), "FormatTo with WithColor must be byte-identical to Format with WithColor")
	})
}

// TestFormat_ANSIColor_InlineCollection verifies that inline collections still
// contain ANSI codes when color is enabled.
func TestFormat_ANSIColor_InlineCollection(t *testing.T) {
	got := formatFirst(t, IntSlice{1, 2, 3}, gobspect.WithColor(gobspect.ANSIColorScheme))
	// Inline slice: "[]int{1, 2, 3}" — TypeHeader wraps "[]int", braces are plain.
	assert.Contains(t, got, "\x1b[1;36m[]int\x1b[0m{", "inline slice header should be bold cyan")
	assert.Contains(t, got, "\x1b[35m1\x1b[0m", "inline numbers should be magenta")
}

// TestFormat_ANSIColor_NoColorDoesNotChangeExisting verifies that the existing
// golden test outputs are ANSI-free when no color option is given.
func TestFormat_ANSIColor_NoColorDoesNotChangeExisting(t *testing.T) {
	v := fmtOuter{X: fmtInner{A: 42, B: "hello"}, Y: 3.14}
	plain := formatFirst(t, v)
	assert.NotContains(t, plain, "\x1b[", "no-option Format must remain ANSI-free")

	want := "" +
		"fmtOuter{\n" +
		"  X: fmtInner{\n" +
		"    A: 42\n" +
		"    B: \"hello\"\n" +
		"  }\n" +
		"  Y: 3.14\n" +
		"}"
	assert.Equal(t, want, plain, "plain output must remain byte-identical")
}

// — WithInlineWidth tests ——————————————————————————————————————————————————————

// — WithMapOrder tests ————————————————————————————————————————————————————————

// TestFormat_MapOrder verifies that:
//   - Default (no option): entries are sorted alphabetically by formatted key.
//   - WithMapOrder(MapOrderSorted): same sorted order.
//   - WithMapOrder(MapOrderInsertion): entries are in wire (insertion) order.
//
// The MapValue is constructed directly with entries in a known non-alphabetical
// order (z, a, m) so all three orderings are distinguishable.
func TestFormat_MapOrder(t *testing.T) {
	// Build a MapValue whose entries are in wire order: z, a, m.
	// Sorted order would be: a, m, z.
	mv := gobspect.MapValue{
		GobTypeID: 1,
		KeyType:   "string",
		ElemType:  "int",
		Entries: []gobspect.MapEntry{
			{Key: gobspect.StringValue{V: "z"}, Value: gobspect.IntValue{V: 3}},
			{Key: gobspect.StringValue{V: "a"}, Value: gobspect.IntValue{V: 1}},
			{Key: gobspect.StringValue{V: "m"}, Value: gobspect.IntValue{V: 2}},
		},
	}

	// Use a narrow inline width to force indented rendering so order is unambiguous.
	narrowWidth := gobspect.WithInlineWidth(1)

	t.Run("default_sorted", func(t *testing.T) {
		got := gobspect.Format(mv, narrowWidth)
		wantLines := []string{`"a": 1`, `"m": 2`, `"z": 3`}
		for i, line := range wantLines {
			assert.Contains(t, got, line, "sorted default: line %d", i)
		}
		// Verify positional order: a before m before z.
		aPos := strings.Index(got, `"a"`)
		mPos := strings.Index(got, `"m"`)
		zPos := strings.Index(got, `"z"`)
		assert.True(t, aPos < mPos && mPos < zPos, "default: want a < m < z order, got positions a=%d m=%d z=%d", aPos, mPos, zPos)
	})

	t.Run("explicit_sorted", func(t *testing.T) {
		got := gobspect.Format(mv, narrowWidth, gobspect.WithMapOrder(gobspect.MapOrderSorted))
		aPos := strings.Index(got, `"a"`)
		mPos := strings.Index(got, `"m"`)
		zPos := strings.Index(got, `"z"`)
		assert.True(t, aPos < mPos && mPos < zPos, "MapOrderSorted: want a < m < z order, got positions a=%d m=%d z=%d", aPos, mPos, zPos)
	})

	t.Run("insertion_order", func(t *testing.T) {
		got := gobspect.Format(mv, narrowWidth, gobspect.WithMapOrder(gobspect.MapOrderInsertion))
		// Wire order is z, a, m.
		zPos := strings.Index(got, `"z"`)
		aPos := strings.Index(got, `"a"`)
		mPos := strings.Index(got, `"m"`)
		assert.True(t, zPos < aPos && aPos < mPos, "MapOrderInsertion: want z < a < m order, got positions z=%d a=%d m=%d", zPos, aPos, mPos)
	})
}

// TestFormat_WithInlineWidth verifies that the inline-vs-multiline threshold can
// be overridden via WithInlineWidth. We use a map whose plain-text inline
// rendering is between 30 and 120 characters (and above 72) so that:
//   - WithInlineWidth(120): renders inline (no newline inside the collection)
//   - WithInlineWidth(30):  renders multiline (newline inside the collection)
//   - default (no option):  renders multiline (default threshold is 72)
func TestFormat_WithInlineWidth(t *testing.T) {
	// Use a map that renders inline as something like:
	//   map[string]int{"alpha": 100, "beta": 200, "gamma": 300, "delta": 400}
	// which is ~56 chars — fits within 72 but not within 30.
	// We need something that's > 72 to also test the default-stays-multiline case.
	// Use a longer map: inline form ~85 chars, fits in 120 but not 72 or 30.
	m := StringMap{
		"alpha":   100,
		"beta":    200,
		"gamma":   300,
		"delta":   400,
		"epsilon": 500,
	}

	// Verify: inline form must be > 72 and <= 120 chars so all three assertions work.
	// map[string]int{"alpha": 100, "beta": 200, "delta": 400, "epsilon": 500, "gamma": 300}
	// is 84 chars — comfortably between 72 and 120.

	t.Run("wide_threshold_renders_inline", func(t *testing.T) {
		got := formatFirst(t, m, gobspect.WithInlineWidth(120))
		// A single-line inline rendering has no newline inside the collection.
		assert.NotContains(t, got, "\n", "WithInlineWidth(120) should render inline (no newline)")
	})

	t.Run("narrow_threshold_renders_multiline", func(t *testing.T) {
		got := formatFirst(t, m, gobspect.WithInlineWidth(30))
		assert.Contains(t, got, "\n", "WithInlineWidth(30) should render multiline")
	})

	t.Run("default_72_renders_multiline_for_long_map", func(t *testing.T) {
		// No option: default threshold 72. The map inline form is ~84 chars, so it
		// should fall back to multiline.
		got := formatFirst(t, m)
		assert.Contains(t, got, "\n", "default threshold 72 should render multiline for an 84-char inline form")
	})
}

// — FormatBytes tests ————————————————————————————————————————————————————————

func TestFormatBytes_AllFormats(t *testing.T) {
	tests := []struct {
		name     string
		b        []byte
		format   gobspect.BytesFormat
		maxBytes int
		want     string
	}{
		// Hex format
		{
			name:     "hex basic",
			b:        []byte{0xde, 0xad, 0xbe, 0xef},
			format:   gobspect.BytesHex,
			maxBytes: 0,
			want:     "deadbeef",
		},
		{
			name:     "hex truncated",
			b:        []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			format:   gobspect.BytesHex,
			maxBytes: 3,
			want:     "010203…",
		},
		{
			name:     "hex no truncation when under limit",
			b:        []byte{0xca, 0xfe},
			format:   gobspect.BytesHex,
			maxBytes: 64,
			want:     "cafe",
		},
		{
			name:     "hex empty",
			b:        []byte{},
			format:   gobspect.BytesHex,
			maxBytes: 0,
			want:     "[]",
		},

		// Base64 format
		{
			name:     "base64 basic",
			b:        []byte("hello"),
			format:   gobspect.BytesBase64,
			maxBytes: 0,
			want:     "aGVsbG8=",
		},
		{
			name:     "base64 truncated",
			b:        []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			format:   gobspect.BytesBase64,
			maxBytes: 3,
			want:     "AQID…",
		},
		{
			name:     "base64 empty",
			b:        []byte{},
			format:   gobspect.BytesBase64,
			maxBytes: 0,
			want:     "[]",
		},

		// Literal format
		{
			name:     "literal basic",
			b:        []byte{0x01, 0x02},
			format:   gobspect.BytesLiteral,
			maxBytes: 0,
			want:     "[]byte{0x01, 0x02}",
		},
		{
			name:     "literal truncated",
			b:        []byte{0x01, 0x02, 0x03, 0x04},
			format:   gobspect.BytesLiteral,
			maxBytes: 2,
			want:     "[]byte{0x01, 0x02}…",
		},
		{
			name:     "literal empty",
			b:        []byte{},
			format:   gobspect.BytesLiteral,
			maxBytes: 0,
			want:     "[]",
		},

		// maxBytes=0 means no limit
		{
			name:     "hex zero maxBytes is no limit",
			b:        []byte{0x01, 0x02, 0x03},
			format:   gobspect.BytesHex,
			maxBytes: 0,
			want:     "010203",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gobspect.FormatBytes(tt.b, tt.format, tt.maxBytes)
			assert.Equal(t, tt.want, got)
		})
	}
}
