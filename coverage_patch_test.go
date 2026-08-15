package gobspect_test

// Targeted tests to exercise otherwise-untested public API surface. These
// complement the existing per-feature test files and focus on tiny wrappers
// and straight-through methods that slip past behavior-oriented tests.

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseBytesFormat exercises each accepted literal plus the fallback.
func TestParseBytesFormat(t *testing.T) {
	tests := []struct {
		in     string
		want   gobspect.BytesFormat
		wantOK bool
	}{
		{"hex", gobspect.BytesHex, true},
		{"base64", gobspect.BytesBase64, true},
		{"literal", gobspect.BytesLiteral, true},
		{"HEX", gobspect.BytesHex, true},
		{"unknown", gobspect.BytesHex, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := gobspect.ParseBytesFormat(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

// TestSchemaTypeByName exercises the lookup method.
func TestSchemaTypeByName(t *testing.T) {
	schema := gobspect.FormatSchema([]gobspect.TypeInfo{
		{ID: 65, Name: "Alpha", Kind: gobspect.KindStruct, Fields: []gobspect.FieldInfo{{Name: "X", TypeID: 2}}},
		{ID: 66, Name: "Beta", Kind: gobspect.KindSlice, Elem: &gobspect.TypeRef{ID: 6, Name: "string"}},
	})
	alpha, ok := schema.TypeByName("Alpha")
	require.True(t, ok)
	assert.Equal(t, "Alpha", alpha.Name)

	_, ok = schema.TypeByName("Missing")
	assert.False(t, ok)
}

// TestStream_Stats_CoreFields covers Stream.Stats() against a small fixture
// without going through the CLI. Verifies counts, per-type breakdown, and
// field presence.
func TestStream_Stats_CoreFields(t *testing.T) {
	type rec struct {
		A int
		B string
		C int
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(rec{A: 1, B: "x", C: 2}))
	require.NoError(t, enc.Encode(rec{A: 0, B: "y", C: 3})) // A omitted (zero)
	require.NoError(t, enc.Encode(rec{A: 4}))               // only A; B zero & C zero

	ins := gobspect.New()
	s := ins.Stream(&buf)
	stats, err := s.Stats()
	require.NoError(t, err)

	assert.Equal(t, 3, stats.ValueMessages)
	assert.GreaterOrEqual(t, stats.TypeDefMessages, 1)
	assert.GreaterOrEqual(t, stats.TotalMessages, stats.ValueMessages+stats.TypeDefMessages)

	require.Len(t, stats.ByType, 1, "only one struct type")
	ts := stats.ByType[0]
	assert.Equal(t, 3, ts.ValueCount)
	// Field presence: A appears in 2/3 (record 2 has A=0 omitted), B in 2/3,
	// C in 2/3.
	assert.Equal(t, 2, ts.FieldPresence["A"])
	assert.Equal(t, 2, ts.FieldPresence["B"])
	assert.Equal(t, 2, ts.FieldPresence["C"])
}

// TestStats_FormatAndJSON exercises the two Stats renderers.
func TestStats_FormatAndJSON(t *testing.T) {
	type rec struct{ V int }
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(rec{V: 1}))
	require.NoError(t, enc.Encode(rec{V: 2}))

	ins := gobspect.New()
	stats, err := ins.Stream(&buf).Stats()
	require.NoError(t, err)

	var sb strings.Builder
	require.NoError(t, stats.Format(&sb))
	assert.Contains(t, sb.String(), "values: 2")

	jb, err := stats.JSON()
	require.NoError(t, err)
	assert.Contains(t, string(jb), `"ValueMessages":2`)

	indented, err := stats.JSONIndent("", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(indented), `"ValueMessages": 2`)
}

// TestStream_StatsConsumedPanics confirms that Stats shares the single-use
// contract with Values and Messages.
func TestStream_StatsConsumedPanics(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(struct{ X int }{X: 1}))

	ins := gobspect.New()
	s := ins.Stream(&buf)
	_, err := s.Stats()
	require.NoError(t, err)

	defer func() { assert.NotNil(t, recover(), "second Stats must panic") }()
	_, _ = s.Stats()
}
