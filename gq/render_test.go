package gq_test

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/gq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeOne decodes the single encoded value from vals into a Value.
func decodeOne(t *testing.T, val any) gobspect.Value {
	t.Helper()
	vals, err := gobspect.New().Stream(encodeValues(t, val)).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	return vals[0]
}

// readGolden reads a golden file from the testdata directory.
func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".golden")
	require.NoError(t, err)
	return string(b)
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in     string
		want   gq.Format
		wantOK bool
	}{
		{"", gq.FormatPretty, true},
		{"pretty", gq.FormatPretty, true},
		{"json", gq.FormatJSON, true},
		{"jsonl", gq.FormatJSONL, true},
		{"csv", gq.FormatPretty, false},
		{"JSON", gq.FormatPretty, false},
		{"bogus", gq.FormatPretty, false},
	}
	for _, tt := range tests {
		t.Run("input_"+tt.in, func(t *testing.T) {
			got, ok := gq.ParseFormat(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRenderGolden(t *testing.T) {
	// Hand-built rather than decoded: gob allocates type IDs process-globally,
	// so a decoded struct's GobTypeID depends on which tests encoded types
	// first. A fixed value keeps the goldens deterministic.
	value := gobspect.StructValue{
		TypeName:  "person",
		GobTypeID: 64,
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
			{Name: "Score", Value: gobspect.IntValue{V: 42}},
		},
	}

	tests := []struct {
		name string
		opts gq.RenderOptions
	}{
		{name: "pretty_struct", opts: gq.RenderOptions{Format: gq.FormatPretty}},
		{name: "json_struct", opts: gq.RenderOptions{Format: gq.FormatJSON}},
		{name: "json_compact_struct", opts: gq.RenderOptions{Format: gq.FormatJSON, Compact: true}},
		{name: "jsonl_struct", opts: gq.RenderOptions{Format: gq.FormatJSONL}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, gq.Render(&out, value, tt.opts))
			assert.Equal(t, readGolden(t, tt.name), out.String())
		})
	}
}

func TestRenderRaw(t *testing.T) {
	tests := []struct {
		name string
		v    gobspect.Value
		want string
	}{
		{
			name: "plain string drops quotes",
			v:    gobspect.StringValue{V: "hello world"},
			want: "hello world\n",
		},
		{
			name: "single interface layer unwraps",
			v: gobspect.InterfaceValue{
				TypeName: "main.Greeting",
				Value:    gobspect.StringValue{V: "hi"},
			},
			want: "hi\n",
		},
		{
			// Regression: the pre-extraction copies in gq and gobspect-mcp
			// peeled only one InterfaceValue layer, so a nested wrapper fell
			// through to quoted pretty output — the same defect class fixed
			// in Equal/CompareValues for v0.2.3.
			name: "nested interface layers unwrap recursively",
			v: gobspect.InterfaceValue{
				TypeName: "outer",
				Value: gobspect.InterfaceValue{
					TypeName: "inner",
					Value:    gobspect.StringValue{V: "deep"},
				},
			},
			want: "deep\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, gq.Render(&out, tt.v, gq.RenderOptions{Format: gq.FormatPretty, Raw: true}))
			assert.Equal(t, tt.want, out.String())
		})
	}
}

// TestRenderRawNonString verifies raw mode falls back to normal pretty
// rendering for non-string values.
func TestRenderRawNonString(t *testing.T) {
	var raw, normal bytes.Buffer
	v := gobspect.IntValue{V: 7}
	require.NoError(t, gq.Render(&raw, v, gq.RenderOptions{Raw: true}))
	require.NoError(t, gq.Render(&normal, v, gq.RenderOptions{}))
	assert.Equal(t, normal.String(), raw.String())
}

// TestRenderRawOnlyAffectsPretty verifies Raw has no effect on JSON output,
// matching gq flag validation which rejects -r outside pretty anyway.
func TestRenderRawOnlyAffectsPretty(t *testing.T) {
	v := gobspect.StringValue{V: "s"}
	var withRaw, without bytes.Buffer
	require.NoError(t, gq.Render(&withRaw, v, gq.RenderOptions{Format: gq.FormatJSON, Raw: true}))
	require.NoError(t, gq.Render(&without, v, gq.RenderOptions{Format: gq.FormatJSON}))
	assert.Equal(t, without.String(), withRaw.String())
}

func TestRenderColor(t *testing.T) {
	value := decodeOne(t, person{Name: "Alice", Score: 42})

	var plain, colored bytes.Buffer
	require.NoError(t, gq.Render(&plain, value, gq.RenderOptions{}))
	require.NoError(t, gq.Render(&colored, value, gq.RenderOptions{Color: true}))

	assert.NotContains(t, plain.String(), "\x1b[", "plain output must be ANSI-free")
	assert.Contains(t, colored.String(), "\x1b[", "color output must contain ANSI escapes")
}

// TestRenderColorDoesNotMutateCallerOptions verifies the color append cannot
// scribble on spare capacity in a caller-owned FormatOptions slice.
func TestRenderColorDoesNotMutateCallerOptions(t *testing.T) {
	opts := make([]gobspect.FormatOption, 1, 4)
	opts[0] = gobspect.WithMaxBytes(4)
	shared := gq.RenderOptions{Color: true, FormatOptions: opts[:1]}

	value := decodeOne(t, person{Name: "Alice", Score: 1})
	var first, second bytes.Buffer
	require.NoError(t, gq.Render(&first, value, shared))
	require.NoError(t, gq.Render(&second, value, shared))

	assert.Equal(t, first.String(), second.String(), "repeat renders with shared options must agree")
	assert.Nil(t, opts[1:cap(opts)][0], "spare capacity of the caller slice must stay untouched")
}

func TestRenderFormatOptionsApply(t *testing.T) {
	value := decodeOne(t, struct{ Data []byte }{Data: bytes.Repeat([]byte{0xab}, 32)})

	var out bytes.Buffer
	require.NoError(t, gq.Render(&out, value, gq.RenderOptions{
		FormatOptions: []gobspect.FormatOption{gobspect.WithMaxBytes(4)},
	}))
	assert.Contains(t, out.String(), "abababab", "first 4 bytes rendered")
	assert.Equal(t, 1, strings.Count(out.String(), "abababab"), "output must truncate at 4 bytes")
}

func TestRenderJSONOptionsApply(t *testing.T) {
	// NaN needs the nonfinite handling: strings by default, null with the option.
	value := decodeOne(t, struct{ F float64 }{F: math.NaN()})

	var asString, asNull bytes.Buffer
	require.NoError(t, gq.Render(&asString, value, gq.RenderOptions{Format: gq.FormatJSONL}))
	require.NoError(t, gq.Render(&asNull, value, gq.RenderOptions{
		Format:      gq.FormatJSONL,
		JSONOptions: []gobspect.JSONOption{gobspect.WithNonFiniteAsNull(true)},
	}))
	assert.Contains(t, asString.String(), `"NaN"`)
	assert.NotContains(t, asNull.String(), `"NaN"`)
	assert.Contains(t, asNull.String(), "null")
}
