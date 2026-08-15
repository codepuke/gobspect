package gobspect_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEqual_NaNIsReflexive documents that structural equality treats NaN as
// equal to NaN, so a value equals itself and self-diffs are empty.
func TestEqual_NaNIsReflexive(t *testing.T) {
	nan := gobspect.FloatValue{V: math.NaN()}
	assert.True(t, gobspect.Equal(nan, nan), "NaN float must equal itself structurally")

	cnan := gobspect.ComplexValue{Real: math.NaN(), Imag: math.NaN()}
	assert.True(t, gobspect.Equal(cnan, cnan), "complex with NaN parts must equal itself")

	// Distinct NaN bit patterns still compare equal (both are NaN).
	other := gobspect.FloatValue{V: math.Float64frombits(0x7FF8000000000001)}
	assert.True(t, gobspect.Equal(nan, other))
}

// TestCompareValues_NaNTotalOrder ensures NaN sorts consistently (below every
// number) so the comparator stays a strict weak ordering.
func TestCompareValues_NaNTotalOrder(t *testing.T) {
	nan := gobspect.FloatValue{V: math.NaN()}
	one := gobspect.FloatValue{V: 1}
	assert.Equal(t, -1, gobspect.CompareValues(nan, one), "NaN sorts before a number")
	assert.Equal(t, 1, gobspect.CompareValues(one, nan))
	assert.Equal(t, 0, gobspect.CompareValues(nan, nan), "NaN equals NaN under the total order")
}

// TestCompareValues_ComplexNumericOrder ensures complex values order by
// (real, imag) numerically rather than lexicographically by formatted string.
func TestCompareValues_ComplexNumericOrder(t *testing.T) {
	a := gobspect.ComplexValue{Real: 2, Imag: 0}
	b := gobspect.ComplexValue{Real: 10, Imag: 0}
	assert.Equal(t, -1, gobspect.CompareValues(a, b), "2 < 10 numerically, not lexicographically")

	c := gobspect.ComplexValue{Real: 2, Imag: 5}
	assert.Equal(t, -1, gobspect.CompareValues(a, c), "tie on real, order by imag")

	// Complex sits between the numeric kinds and strings in the total order.
	assert.Equal(t, -1, gobspect.CompareValues(gobspect.IntValue{V: 1}, a))
	assert.Equal(t, -1, gobspect.CompareValues(a, gobspect.StringValue{V: "x"}))
}

// TestToJSON_NonFinite verifies NaN/±Inf serialize as strings by default and
// as null under WithNonFiniteAsNull, instead of failing the whole document.
func TestToJSON_NonFinite(t *testing.T) {
	v := gobspect.StructValue{Fields: []gobspect.Field{
		{Name: "N", Value: gobspect.FloatValue{V: math.NaN()}},
		{Name: "P", Value: gobspect.FloatValue{V: math.Inf(1)}},
		{Name: "M", Value: gobspect.FloatValue{V: math.Inf(-1)}},
	}}

	out, err := gobspect.ToJSON(v)
	require.NoError(t, err, "non-finite floats must not fail the document")
	s := string(out)
	assert.Contains(t, s, `"NaN"`)
	assert.Contains(t, s, `"+Inf"`)
	assert.Contains(t, s, `"-Inf"`)

	outNull, err := gobspect.ToJSON(v, gobspect.WithNonFiniteAsNull(true))
	require.NoError(t, err)
	// Every non-finite "v" is null; confirm it parses and holds nulls.
	var doc map[string]any
	require.NoError(t, json.Unmarshal(outNull, &doc))
	assert.NotContains(t, string(outNull), "NaN")
}

// TestToJSON_NonUTF8String verifies invalid-UTF-8 string values are base64
// encoded rather than silently corrupted with U+FFFD.
func TestToJSON_NonUTF8String(t *testing.T) {
	v := gobspect.StringValue{V: "\xff\xfe\x00bad"}
	out, err := gobspect.ToJSON(v)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	assert.Equal(t, "base64", doc["encoding"], "invalid UTF-8 must be marked base64")
	// The valid-UTF-8 case stays a plain string with no encoding marker.
	out2, err := gobspect.ToJSON(gobspect.StringValue{V: "ok"})
	require.NoError(t, err)
	assert.NotContains(t, string(out2), "base64")
}

// TestFormat_ComplexPositiveInfImag verifies +Inf imaginary parts don't
// produce a doubled sign like "(1++Infi)".
func TestFormat_ComplexPositiveInfImag(t *testing.T) {
	v := gobspect.ComplexValue{Real: 1, Imag: math.Inf(1)}
	out := gobspect.Format(v)
	assert.Equal(t, "(1+Infi)", out) // "%g" already carries the sign; not doubled
	assert.NotContains(t, out, "++")
}

// TestFormat_EmptyTextMarshaler verifies an opaque value whose decoded form is
// the empty string renders visibly rather than as nothing.
func TestFormat_EmptyTextMarshaler(t *testing.T) {
	v := gobspect.OpaqueValue{TypeName: "T", Encoding: "text", Decoded: ""}
	assert.Equal(t, `""`, gobspect.Format(v))
}

// TestFormat_DeepNestingIsLinear guards against the exponential re-render
// regression: a deeply nested single-element slice must format quickly.
func TestFormat_DeepNestingIsLinear(t *testing.T) {
	var v gobspect.Value = gobspect.StringValue{V: strings.Repeat("x", 100)}
	for range 60 {
		v = gobspect.SliceValue{ElemType: "any", Elems: []gobspect.Value{v}}
	}
	// With per-level re-rendering this would take exponential time; here it is
	// effectively instant. The test simply completing is the assertion.
	out := gobspect.Format(v, gobspect.WithColor(gobspect.ANSIColorScheme))
	assert.NotEmpty(t, out)
}

// TestFormat_RedactionWidthIgnoresColor verifies redacted-field fill length is
// computed from the plain rendering, not the color-escaped one.
func TestFormat_RedactionWidthIgnoresColor(t *testing.T) {
	v := gobspect.StructValue{Fields: []gobspect.Field{
		{Name: "secret", Value: gobspect.StringValue{V: "hunter2"}},
	}}
	out := gobspect.Format(v,
		gobspect.WithColor(gobspect.ANSIColorScheme),
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"secret"}}),
	)
	// "hunter2" quotes to 9 runes; redaction must emit exactly 9 stars, not
	// inflated by ANSI escape bytes.
	assert.Contains(t, out, strings.Repeat("*", 9))
	assert.NotContains(t, out, strings.Repeat("*", 10))
}
