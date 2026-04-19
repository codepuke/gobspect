package main

import (
	"bytes"
	"encoding/csv"
	"encoding/gob"
	"slices"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — cellString tests —————————————————————————————————————————————————————————

func TestCellStringScalars(t *testing.T) {
	tests := []struct {
		name string
		v    gobspect.Value
		want string
	}{
		{"string", gobspect.StringValue{V: "hello"}, "hello"},
		{"int", gobspect.IntValue{V: 42}, "42"},
		{"negative int", gobspect.IntValue{V: -7}, "-7"},
		{"uint", gobspect.UintValue{V: 99}, "99"},
		{"float", gobspect.FloatValue{V: 3.14}, "3.14"},
		{"complex positive imag", gobspect.ComplexValue{Real: 1.5, Imag: 2}, "(1.5+2i)"},
		{"complex negative imag", gobspect.ComplexValue{Real: 1.5, Imag: -2}, "(1.5-2i)"},
		{"bool true", gobspect.BoolValue{V: true}, "true"},
		{"bool false", gobspect.BoolValue{V: false}, "false"},
		{"nil", gobspect.NilValue{}, ""},
		{"bytes", gobspect.BytesValue{V: []byte{0xca, 0xfe}}, "cafe"},
		{"bytes empty", gobspect.BytesValue{V: []byte{}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cellString(tt.v))
		})
	}
}

func TestCellStringComplexTypes(t *testing.T) {
	tests := []struct {
		name string
		v    gobspect.Value
		want string
	}{
		{"struct", gobspect.StructValue{TypeName: "Foo"}, "(struct)"},
		{"slice", gobspect.SliceValue{ElemType: "int"}, "(array)"},
		{"array", gobspect.ArrayValue{ElemType: "int"}, "(array)"},
		{"map", gobspect.MapValue{KeyType: "string", ElemType: "int"}, "(map)"},
		{"opaque raw", gobspect.OpaqueValue{TypeName: "T", Raw: []byte{1, 2}}, "(opaque)"},
		{"opaque decoded string", gobspect.OpaqueValue{TypeName: "time.Time", Decoded: "2024-01-01T00:00:00Z"}, "2024-01-01T00:00:00Z"},
		{"opaque decoded int", gobspect.OpaqueValue{TypeName: "big.Int", Decoded: 42}, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cellString(tt.v))
		})
	}
}

func TestCellStringInterfaceUnwrap(t *testing.T) {
	iv := gobspect.InterfaceValue{
		TypeName: "MyType",
		Value:    gobspect.StringValue{V: "inner"},
	}
	assert.Equal(t, "inner", cellString(iv))
}

// — tabularPrinter tests ————————————————————————————————————————————————————

func TestTabularPrinterCSVStruct(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	sv := gobspect.StructValue{
		TypeName: "",
		Fields: []gobspect.Field{
			{Name: "SKU", Value: gobspect.StringValue{V: "ABC-123"}},
			{Name: "Price", Value: gobspect.IntValue{V: 42}},
		},
	}

	require.NoError(t, tp.WriteValue(sv))
	tp.Flush()
	require.NoError(t, tp.Error())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2, "should have header + 1 data row")
	assert.Equal(t, "SKU,Price", lines[0])
	assert.Equal(t, "ABC-123,42", lines[1])
}

func TestTabularPrinterTSV(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter('\t'), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "A", Value: gobspect.StringValue{V: "x"}},
			{Name: "B", Value: gobspect.IntValue{V: 1}},
		},
	}

	require.NoError(t, tp.WriteValue(sv))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "A\tB", lines[0])
	assert.Equal(t, "x\t1", lines[1])
}

func TestTabularPrinterNoHeaders(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withNoHeaders(true), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "SKU", Value: gobspect.StringValue{V: "X"}},
		},
	}

	require.NoError(t, tp.WriteValue(sv))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1, "no header, just 1 data row")
	assert.Equal(t, "X", lines[0])
}

func TestTabularPrinterNonStruct(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	require.NoError(t, tp.WriteValue(gobspect.StringValue{V: "hello"}))
	require.NoError(t, tp.WriteValue(gobspect.IntValue{V: 42}))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "value", lines[0])
	assert.Equal(t, "hello", lines[1])
	assert.Equal(t, "42", lines[2])
}

func TestTabularPrinterMultipleStructs(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	sv1 := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
			{Name: "Age", Value: gobspect.IntValue{V: 30}},
		},
	}
	sv2 := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "Bob"}},
			{Name: "Age", Value: gobspect.IntValue{V: 25}},
		},
	}

	require.NoError(t, tp.WriteValue(sv1))
	require.NoError(t, tp.WriteValue(sv2))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "Name,Age", lines[0])
	assert.Equal(t, "Alice,30", lines[1])
	assert.Equal(t, "Bob,25", lines[2])
}

func TestTabularPrinterCSVQuotesCommas(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Note", Value: gobspect.StringValue{V: "hello, world"}},
		},
	}

	require.NoError(t, tp.WriteValue(sv))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "Note", lines[0])
	// CSV writer should quote the value containing a comma.
	assert.Equal(t, `"hello, world"`, lines[1])
}

func TestTabularPrinterNonStructNoHeaders(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withNoHeaders(true), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	require.NoError(t, tp.WriteValue(gobspect.IntValue{V: 7}))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1, "just 1 data row, no header")
	assert.Equal(t, "7", lines[0])
}

// — Stream-backed tabular printer tests ————————————————————————————————————

// tabularPoint is the struct type used for sparse struct tabular tests.
type tabularPoint struct {
	X int
	Y int
	Z int
}

// tabularOrder is a second struct type used for heterogeneous type tests.
type tabularOrder struct {
	ID    int
	Total float64
}

func gobEncodeTabular(tb testing.TB, vals ...any) *bytes.Buffer {
	tb.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range vals {
		require.NoError(tb, enc.Encode(v))
	}
	return &buf
}

// TestTabularPrinter_SparseStructsFromSameType encodes two instances of the
// same struct type where the first has a zero field (omitted on the wire) and
// the second has all fields. Verifies that the header uses type-definition
// order and sparse rows have empty cells for omitted fields.
func TestTabularPrinter_SparseStructsFromSameType(t *testing.T) {
	// First instance: Z=0 is omitted by gob sparse encoding.
	// Second instance: all fields present.
	buf := gobEncodeTabular(t,
		tabularPoint{X: 1, Y: 2 /* Z: 0 */},
		tabularPoint{X: 3, Y: 4, Z: 5},
	)

	ins := gobspect.New()
	stream := ins.Stream(buf)

	var out bytes.Buffer
	tp := newTabularPrinter(&out, withDelimiter(','), withStream(stream), withBytesFormat(gobspect.BytesHex), withMaxBytes(64))

	for v, err := range stream.Values() {
		require.NoError(t, err)
		require.NoError(t, tp.WriteValue(v))
	}
	tp.Flush()
	require.NoError(t, tp.Error())

	r := csv.NewReader(strings.NewReader(out.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 data rows")

	header := rows[0]
	assert.Equal(t, []string{"X", "Y", "Z"}, header, "header must use type-definition order")

	// Row 1: X=1, Y=2, Z="" (zero field omitted by gob).
	assert.Equal(t, "1", rows[1][0])
	assert.Equal(t, "2", rows[1][1])
	assert.Equal(t, "", rows[1][2], "omitted zero field must be empty string")

	// Row 2: X=3, Y=4, Z=5.
	assert.Equal(t, "3", rows[2][0])
	assert.Equal(t, "4", rows[2][1])
	assert.Equal(t, "5", rows[2][2])

	// Every row must have the same column count as the header.
	for i, row := range rows[1:] {
		assert.Len(t, row, len(header), "row %d must have %d columns", i+1, len(header))
	}
}

// TestTabularPrinter_HeterogeneousStructTypesRejected verifies that structs of
// two different named types cause an error after the first type mismatch when
// using HeterogeneousReject mode.
func TestTabularPrinter_HeterogeneousStructTypesRejected(t *testing.T) {
	buf := gobEncodeTabular(t,
		tabularPoint{X: 1, Y: 2, Z: 3},
		tabularOrder{ID: 7, Total: 9.99},
	)

	ins := gobspect.New()
	stream := ins.Stream(buf)

	var out bytes.Buffer
	tp := newTabularPrinter(&out, withDelimiter(','), withStream(stream), withBytesFormat(gobspect.BytesHex), withMaxBytes(64), withHeterogeneousMode(HeterogeneousReject))

	var writeErr error
	for v, err := range stream.Values() {
		require.NoError(t, err)
		if writeErr = tp.WriteValue(v); writeErr != nil {
			break
		}
	}
	require.Error(t, writeErr, "heterogeneous types must produce an error")
	assert.Contains(t, writeErr.Error(), "projection", "error must mention projection as the escape hatch")
}

// TestTabularPrinter_HeterogeneousTypesAllowedWithProjection verifies that
// projection structs (TypeName == query.ProjectionTypeName) from different
// source types are accepted and produce a uniform grid.
func TestTabularPrinter_HeterogeneousTypesAllowedWithProjection(t *testing.T) {
	// Simulate two projection results (synthetic structs from buildProjection).
	// Both have TypeName == query.ProjectionTypeName and the same field names.
	sv1 := gobspect.StructValue{
		TypeName: query.ProjectionTypeName,
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
			{Name: "Score", Value: gobspect.IntValue{V: 99}},
		},
	}
	sv2 := gobspect.StructValue{
		TypeName: query.ProjectionTypeName,
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: "Bob"}},
			{Name: "Score", Value: gobspect.IntValue{V: 42}},
		},
	}

	var out bytes.Buffer
	tp := newTabularPrinter(&out, withDelimiter(','), withBytesFormat(gobspect.BytesHex), withMaxBytes(64)) // nil stream: projection mode
	require.NoError(t, tp.WriteValue(sv1))
	require.NoError(t, tp.WriteValue(sv2))
	tp.Flush()

	r := csv.NewReader(strings.NewReader(out.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 data rows")
	assert.Equal(t, []string{"Name", "Score"}, rows[0])
	assert.Equal(t, []string{"Alice", "99"}, rows[1])
	assert.Equal(t, []string{"Bob", "42"}, rows[2])
}

// TestTabularPrinter_BytesRespectsFormat verifies that the tabularPrinter
// renders BytesValue cells using the configured bytesFormat and maxBytes.
func TestTabularPrinter_BytesRespectsFormat(t *testing.T) {
	payload := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}

	tests := []struct {
		name     string
		format   gobspect.BytesFormat
		maxBytes int
		want     string
	}{
		{
			name:     "hex default no truncation",
			format:   gobspect.BytesHex,
			maxBytes: 0,
			want:     "deadbeef0001",
		},
		{
			name:     "hex truncated to 3 bytes",
			format:   gobspect.BytesHex,
			maxBytes: 3,
			want:     "deadbe…",
		},
		{
			name:     "base64",
			format:   gobspect.BytesBase64,
			maxBytes: 0,
			want:     "3q2+7wAB",
		},
		{
			name:     "literal",
			format:   gobspect.BytesLiteral,
			maxBytes: 0,
			want:     "[]byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tp := newTabularPrinter(&buf, withDelimiter(','), withBytesFormat(tt.format), withMaxBytes(tt.maxBytes))

			sv := gobspect.StructValue{
				Fields: []gobspect.Field{
					{Name: "Data", Value: gobspect.BytesValue{V: payload}},
				},
			}
			require.NoError(t, tp.WriteValue(sv))
			tp.Flush()
			require.NoError(t, tp.Error())

			// Parse via CSV reader to handle any quoting the writer may have applied.
			r := csv.NewReader(strings.NewReader(buf.String()))
			rows, err := r.ReadAll()
			require.NoError(t, err)
			require.Len(t, rows, 2, "header + 1 data row")
			assert.Equal(t, "Data", rows[0][0], "header cell")
			assert.Equal(t, tt.want, rows[1][0], "data cell")
		})
	}
}

// — Heterogeneous mode tests ————————————————————————————————————————————————

// makePoint and makeOrder build StructValues with a GobTypeID to simulate
// stream-decoded structs of different types.
func makePoint(typeID int, x, y, z int) gobspect.StructValue {
	return gobspect.StructValue{
		TypeName:  "tabularPoint",
		GobTypeID: typeID,
		Fields: []gobspect.Field{
			{Name: "X", Value: gobspect.IntValue{V: int64(x)}},
			{Name: "Y", Value: gobspect.IntValue{V: int64(y)}},
			{Name: "Z", Value: gobspect.IntValue{V: int64(z)}},
		},
	}
}

func makeOrder(typeID int, id int, total float64) gobspect.StructValue {
	return gobspect.StructValue{
		TypeName:  "tabularOrder",
		GobTypeID: typeID,
		Fields: []gobspect.Field{
			{Name: "ID", Value: gobspect.IntValue{V: int64(id)}},
			{Name: "Total", Value: gobspect.FloatValue{V: total}},
		},
	}
}

// TestTabularPrinter_FirstWins verifies that in HeterogeneousFirstWins mode
// (the default), rows from a second struct type are silently dropped and only
// the first type's rows appear in the output.
func TestTabularPrinter_FirstWins(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','))
	// Default mode is HeterogeneousFirstWins; no explicit option needed.

	require.NoError(t, tp.WriteValue(makePoint(100, 1, 2, 3)))
	require.NoError(t, tp.WriteValue(makeOrder(200, 7, 9.99))) // should be silently dropped
	require.NoError(t, tp.WriteValue(makePoint(100, 4, 5, 6)))
	tp.Flush()
	require.NoError(t, tp.Error())

	r := csv.NewReader(strings.NewReader(buf.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 point rows; order row must be dropped")
	assert.Equal(t, []string{"X", "Y", "Z"}, rows[0])
	assert.Equal(t, []string{"1", "2", "3"}, rows[1])
	assert.Equal(t, []string{"4", "5", "6"}, rows[2])
}

// TestTabularPrinter_Reject verifies that in HeterogeneousReject mode,
// AddRow for a second struct type returns an error.
func TestTabularPrinter_Reject(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withHeterogeneousMode(HeterogeneousReject))

	require.NoError(t, tp.WriteValue(makePoint(100, 1, 2, 3)))
	err := tp.WriteValue(makeOrder(200, 7, 9.99))
	require.Error(t, err, "second type must return an error in Reject mode")
	assert.Contains(t, err.Error(), "tabularOrder")
	assert.Contains(t, err.Error(), "tabularPoint")
}

// TestTabularPrinter_Union verifies that in HeterogeneousUnion mode, the
// header grows when new columns appear. Earlier rows already printed receive
// empty cells for the new columns (streaming cannot backfill).
func TestTabularPrinter_Union(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withHeterogeneousMode(HeterogeneousUnion))

	require.NoError(t, tp.WriteValue(makePoint(100, 1, 2, 3)))
	require.NoError(t, tp.WriteValue(makeOrder(200, 7, 9.99))) // adds ID, Total columns
	require.NoError(t, tp.WriteValue(makePoint(100, 4, 5, 6)))
	tp.Flush()
	require.NoError(t, tp.Error())

	// The output contains multiple header rows: the initial one and the grown
	// one. Parse raw lines to check structure.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	// Find the last header row (the grown one) by looking for the widest row.
	// We know the grown header should have X,Y,Z,ID,Total.
	found := slices.Contains(lines, "X,Y,Z,ID,Total")
	assert.True(t, found, "grown header X,Y,Z,ID,Total must appear in output; got:\n%s", buf.String())

	// The order row must appear and have values in the ID and Total columns.
	foundOrderRow := false
	for _, line := range lines {
		// Order row: X and Y and Z are empty, ID=7, Total=9.99
		if strings.HasPrefix(line, ",,,7,") {
			foundOrderRow = true
			break
		}
	}
	assert.True(t, foundOrderRow, "order row with empty X/Y/Z and ID=7 must appear; got:\n%s", buf.String())
}

// TestTabularPrinter_Partition verifies that in HeterogeneousPartition mode,
// a blank line separates the two tables and each has its own header.
func TestTabularPrinter_Partition(t *testing.T) {
	var buf bytes.Buffer
	tp := newTabularPrinter(&buf, withDelimiter(','), withHeterogeneousMode(HeterogeneousPartition))

	require.NoError(t, tp.WriteValue(makePoint(100, 1, 2, 3)))
	require.NoError(t, tp.WriteValue(makePoint(100, 4, 5, 6)))
	require.NoError(t, tp.WriteValue(makeOrder(200, 7, 9.99)))
	require.NoError(t, tp.WriteValue(makeOrder(200, 8, 19.50)))
	tp.Flush()
	require.NoError(t, tp.Error())

	output := buf.String()

	// Must contain a blank line separating the two sections.
	assert.Contains(t, output, "\n\n", "blank line must separate the two tables")

	// Split on blank lines to get sections.
	sections := strings.Split(strings.TrimRight(output, "\n"), "\n\n")
	require.Len(t, sections, 2, "output must have exactly two sections separated by a blank line")

	// First section: point header + 2 point rows.
	s1Lines := strings.Split(strings.TrimSpace(sections[0]), "\n")
	require.Len(t, s1Lines, 3, "first section: header + 2 rows")
	assert.Equal(t, "X,Y,Z", s1Lines[0])
	assert.Equal(t, "1,2,3", s1Lines[1])
	assert.Equal(t, "4,5,6", s1Lines[2])

	// Second section: order header + 2 order rows.
	s2Lines := strings.Split(strings.TrimSpace(sections[1]), "\n")
	require.Len(t, s2Lines, 3, "second section: header + 2 rows")
	assert.Equal(t, "ID,Total", s2Lines[0])
	assert.Equal(t, "7,9.99", s2Lines[1])
	assert.Equal(t, "8,19.5", s2Lines[2])
}

// TestProjectionBypassesHeteroCheck verifies that rows with
// TypeName == query.ProjectionTypeName bypass heterogeneous-type checking
// regardless of the configured HeterogeneousMode.
func TestProjectionBypassesHeteroCheck(t *testing.T) {
	modes := []struct {
		name string
		mode HeterogeneousMode
	}{
		{"first wins", HeterogeneousFirstWins},
		{"reject", HeterogeneousReject},
		{"union", HeterogeneousUnion},
		{"partition", HeterogeneousPartition},
	}

	for _, mm := range modes {
		t.Run(mm.name, func(t *testing.T) {
			// Two projection structs with different field sets (simulating
			// projections from two different source types).
			sv1 := gobspect.StructValue{
				TypeName: query.ProjectionTypeName,
				Fields: []gobspect.Field{
					{Name: "Name", Value: gobspect.StringValue{V: "Alice"}},
					{Name: "Score", Value: gobspect.IntValue{V: 99}},
				},
			}
			sv2 := gobspect.StructValue{
				TypeName: query.ProjectionTypeName,
				Fields: []gobspect.Field{
					{Name: "Name", Value: gobspect.StringValue{V: "Bob"}},
					{Name: "Score", Value: gobspect.IntValue{V: 42}},
				},
			}

			var out bytes.Buffer
			tp := newTabularPrinter(&out, withDelimiter(','), withHeterogeneousMode(mm.mode))
			require.NoError(t, tp.WriteValue(sv1), "first projection row must not error")
			require.NoError(t, tp.WriteValue(sv2), "second projection row must not error")
			tp.Flush()
			require.NoError(t, tp.Error())

			r := csv.NewReader(strings.NewReader(out.String()))
			rows, err := r.ReadAll()
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(rows), 3, "must have at least header + 2 data rows")
		})
	}
}
