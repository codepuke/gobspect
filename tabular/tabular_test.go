package tabular_test

import (
	"bytes"
	"encoding/csv"
	"encoding/gob"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"github.com/codepuke/gobspect/tabular"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// — CellString tests —————————————————————————————————————————————————————————

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
			assert.Equal(t, tt.want, tabular.CellString(tt.v))
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
		{"slice", gobspect.SliceValue{ElemType: "int"}, "(slice)"},
		{"array", gobspect.ArrayValue{ElemType: "int"}, "(array)"},
		{"map", gobspect.MapValue{KeyType: "string", ElemType: "int"}, "(map)"},
		{"opaque raw", gobspect.OpaqueValue{TypeName: "T", Raw: []byte{1, 2}}, "(opaque)"},
		{"opaque decoded string", gobspect.OpaqueValue{TypeName: "time.Time", Decoded: "2024-01-01T00:00:00Z"}, "2024-01-01T00:00:00Z"},
		{"opaque decoded int", gobspect.OpaqueValue{TypeName: "big.Int", Decoded: 42}, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tabular.CellString(tt.v))
		})
	}
}

func TestCellStringInterfaceUnwrap(t *testing.T) {
	iv := gobspect.InterfaceValue{
		TypeName: "MyType",
		Value:    gobspect.StringValue{V: "inner"},
	}
	assert.Equal(t, "inner", tabular.CellString(iv))
}

// TestPrinter_InterfaceWrappedBytesHonorFormat verifies that a []byte behind an
// interface-typed field still respects the configured bytes format and
// truncation, rather than falling through to unconditional hex.
func TestPrinter_InterfaceWrappedBytesHonorFormat(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','),
		tabular.WithBytesFormat(gobspect.BytesBase64))

	sv := gobspect.StructValue{Fields: []gobspect.Field{
		{Name: "Blob", Value: gobspect.InterfaceValue{
			TypeName: "[]uint8",
			Value:    gobspect.BytesValue{V: []byte{0xca, 0xfe}},
		}},
	}}
	require.NoError(t, p.WriteValue(sv))
	require.NoError(t, p.Flush())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "yv4=", lines[1], "interface-wrapped bytes must use the base64 format, not hex")
}

// — Printer tests ————————————————————————————————————————————————————————————

func TestPrinterCSVStruct(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "SKU", Value: gobspect.StringValue{V: "ABC-123"}},
			{Name: "Price", Value: gobspect.IntValue{V: 42}},
		},
	}

	require.NoError(t, p.WriteValue(sv))
	require.NoError(t, p.Flush())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2, "should have header + 1 data row")
	assert.Equal(t, "SKU,Price", lines[0])
	assert.Equal(t, "ABC-123,42", lines[1])
}

func TestPrinterTSV(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter('\t'), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "A", Value: gobspect.StringValue{V: "x"}},
			{Name: "B", Value: gobspect.IntValue{V: 1}},
		},
	}

	require.NoError(t, p.WriteValue(sv))
	p.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "A\tB", lines[0])
	assert.Equal(t, "x\t1", lines[1])
}

func TestPrinterNoHeaders(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithNoHeaders(true), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "SKU", Value: gobspect.StringValue{V: "X"}},
		},
	}

	require.NoError(t, p.WriteValue(sv))
	p.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1, "no header, just 1 data row")
	assert.Equal(t, "X", lines[0])
}

func TestPrinterNonStruct(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	require.NoError(t, p.WriteValue(gobspect.StringValue{V: "hello"}))
	require.NoError(t, p.WriteValue(gobspect.IntValue{V: 42}))
	p.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "value", lines[0])
	assert.Equal(t, "hello", lines[1])
	assert.Equal(t, "42", lines[2])
}

func TestPrinterMultipleStructs(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

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

	require.NoError(t, p.WriteValue(sv1))
	require.NoError(t, p.WriteValue(sv2))
	p.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "Name,Age", lines[0])
	assert.Equal(t, "Alice,30", lines[1])
	assert.Equal(t, "Bob,25", lines[2])
}

func TestPrinterCSVQuotesCommas(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	sv := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Note", Value: gobspect.StringValue{V: "hello, world"}},
		},
	}

	require.NoError(t, p.WriteValue(sv))
	p.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "Note", lines[0])
	assert.Equal(t, `"hello, world"`, lines[1])
}

func TestPrinterNonStructNoHeaders(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithNoHeaders(true), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	require.NoError(t, p.WriteValue(gobspect.IntValue{V: 7}))
	p.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1, "just 1 data row, no header")
	assert.Equal(t, "7", lines[0])
}

// — Stream-backed printer tests ——————————————————————————————————————————————

type tabularPoint struct {
	X int
	Y int
	Z int
}

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

func TestPrinter_SparseStructsFromSameType(t *testing.T) {
	buf := gobEncodeTabular(t,
		tabularPoint{X: 1, Y: 2 /* Z: 0 */},
		tabularPoint{X: 3, Y: 4, Z: 5},
	)

	ins := gobspect.New()
	stream := ins.Stream(buf)

	var out bytes.Buffer
	p := tabular.NewPrinter(&out, tabular.WithDelimiter(','), tabular.WithStream(stream), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))

	for v, err := range stream.Values() {
		require.NoError(t, err)
		require.NoError(t, p.WriteValue(v))
	}
	require.NoError(t, p.Flush())

	r := csv.NewReader(strings.NewReader(out.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 data rows")

	header := rows[0]
	assert.Equal(t, []string{"X", "Y", "Z"}, header, "header must use type-definition order")

	assert.Equal(t, "1", rows[1][0])
	assert.Equal(t, "2", rows[1][1])
	assert.Equal(t, "", rows[1][2], "omitted zero field must be empty string")

	assert.Equal(t, "3", rows[2][0])
	assert.Equal(t, "4", rows[2][1])
	assert.Equal(t, "5", rows[2][2])

	for i, row := range rows[1:] {
		assert.Len(t, row, len(header), "row %d must have %d columns", i+1, len(header))
	}
}

func TestPrinter_HeterogeneousStructTypesRejected(t *testing.T) {
	buf := gobEncodeTabular(t,
		tabularPoint{X: 1, Y: 2, Z: 3},
		tabularOrder{ID: 7, Total: 9.99},
	)

	ins := gobspect.New()
	stream := ins.Stream(buf)

	var out bytes.Buffer
	p := tabular.NewPrinter(&out, tabular.WithDelimiter(','), tabular.WithStream(stream), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64), tabular.WithHeterogeneousMode(tabular.HeterogeneousReject))

	var writeErr error
	for v, err := range stream.Values() {
		require.NoError(t, err)
		if writeErr = p.WriteValue(v); writeErr != nil {
			break
		}
	}
	require.Error(t, writeErr, "heterogeneous types must produce an error")
	assert.Contains(t, writeErr.Error(), "projection", "error must mention projection as the escape hatch")
}

func TestPrinter_HeterogeneousTypesAllowedWithProjection(t *testing.T) {
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
	p := tabular.NewPrinter(&out, tabular.WithDelimiter(','), tabular.WithBytesFormat(gobspect.BytesHex), tabular.WithMaxBytes(64))
	require.NoError(t, p.WriteValue(sv1))
	require.NoError(t, p.WriteValue(sv2))
	p.Flush()

	r := csv.NewReader(strings.NewReader(out.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 data rows")
	assert.Equal(t, []string{"Name", "Score"}, rows[0])
	assert.Equal(t, []string{"Alice", "99"}, rows[1])
	assert.Equal(t, []string{"Bob", "42"}, rows[2])
}

func TestPrinter_BytesRespectsFormat(t *testing.T) {
	payload := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}

	tests := []struct {
		name     string
		format   gobspect.BytesFormat
		maxBytes int
		want     string
	}{
		{"hex default no truncation", gobspect.BytesHex, 0, "deadbeef0001"},
		{"hex truncated to 3 bytes", gobspect.BytesHex, 3, "deadbe…"},
		{"base64", gobspect.BytesBase64, 0, "3q2+7wAB"},
		{"literal", gobspect.BytesLiteral, 0, "[]byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithBytesFormat(tt.format), tabular.WithMaxBytes(tt.maxBytes))

			sv := gobspect.StructValue{
				Fields: []gobspect.Field{
					{Name: "Data", Value: gobspect.BytesValue{V: payload}},
				},
			}
			require.NoError(t, p.WriteValue(sv))
			require.NoError(t, p.Flush())

			r := csv.NewReader(strings.NewReader(buf.String()))
			rows, err := r.ReadAll()
			require.NoError(t, err)
			require.Len(t, rows, 2, "header + 1 data row")
			assert.Equal(t, "Data", rows[0][0], "header cell")
			assert.Equal(t, tt.want, rows[1][0], "data cell")
		})
	}
}

// — Heterogeneous mode tests —————————————————————————————————————————————————

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

func TestPrinter_FirstWins(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','))

	require.NoError(t, p.WriteValue(makePoint(100, 1, 2, 3)))
	require.NoError(t, p.WriteValue(makeOrder(200, 7, 9.99)))
	require.NoError(t, p.WriteValue(makePoint(100, 4, 5, 6)))
	require.NoError(t, p.Flush())

	r := csv.NewReader(strings.NewReader(buf.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 point rows; order row must be dropped")
	assert.Equal(t, []string{"X", "Y", "Z"}, rows[0])
	assert.Equal(t, []string{"1", "2", "3"}, rows[1])
	assert.Equal(t, []string{"4", "5", "6"}, rows[2])
}

func TestPrinter_Reject(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithHeterogeneousMode(tabular.HeterogeneousReject))

	require.NoError(t, p.WriteValue(makePoint(100, 1, 2, 3)))
	err := p.WriteValue(makeOrder(200, 7, 9.99))
	require.Error(t, err, "second type must return an error in Reject mode")
	assert.Contains(t, err.Error(), "tabularOrder")
	assert.Contains(t, err.Error(), "tabularPoint")
}

func TestPrinter_Union(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithHeterogeneousMode(tabular.HeterogeneousUnion))

	require.NoError(t, p.WriteValue(makePoint(100, 1, 2, 3)))
	require.NoError(t, p.WriteValue(makeOrder(200, 7, 9.99)))
	require.NoError(t, p.WriteValue(makePoint(100, 4, 5, 6)))
	require.NoError(t, p.Flush())

	// Union output must be one rectangular table: a single header carrying the
	// union of all columns, and every row padded with empty cells for columns
	// its type lacks. encoding/csv's Reader enforces the rectangle.
	r := csv.NewReader(&buf)
	rows, err := r.ReadAll()
	require.NoError(t, err, "union output must be rectangular CSV")
	require.Len(t, rows, 4, "header + 3 rows")
	assert.Equal(t, []string{"X", "Y", "Z", "ID", "Total"}, rows[0])
	assert.Equal(t, []string{"1", "2", "3", "", ""}, rows[1])
	assert.Equal(t, []string{"", "", "", "7", "9.99"}, rows[2])
	assert.Equal(t, []string{"4", "5", "6", "", ""}, rows[3])
}

func TestPrinter_Partition(t *testing.T) {
	var buf bytes.Buffer
	p := tabular.NewPrinter(&buf, tabular.WithDelimiter(','), tabular.WithHeterogeneousMode(tabular.HeterogeneousPartition))

	require.NoError(t, p.WriteValue(makePoint(100, 1, 2, 3)))
	require.NoError(t, p.WriteValue(makePoint(100, 4, 5, 6)))
	require.NoError(t, p.WriteValue(makeOrder(200, 7, 9.99)))
	require.NoError(t, p.WriteValue(makeOrder(200, 8, 19.50)))
	require.NoError(t, p.Flush())

	output := buf.String()

	assert.Contains(t, output, "\n\n", "blank line must separate the two tables")

	sections := strings.Split(strings.TrimRight(output, "\n"), "\n\n")
	require.Len(t, sections, 2, "output must have exactly two sections separated by a blank line")

	s1Lines := strings.Split(strings.TrimSpace(sections[0]), "\n")
	require.Len(t, s1Lines, 3, "first section: header + 2 rows")
	assert.Equal(t, "X,Y,Z", s1Lines[0])
	assert.Equal(t, "1,2,3", s1Lines[1])
	assert.Equal(t, "4,5,6", s1Lines[2])

	s2Lines := strings.Split(strings.TrimSpace(sections[1]), "\n")
	require.Len(t, s2Lines, 3, "second section: header + 2 rows")
	assert.Equal(t, "ID,Total", s2Lines[0])
	assert.Equal(t, "7,9.99", s2Lines[1])
	assert.Equal(t, "8,19.5", s2Lines[2])
}

func TestProjectionBypassesHeteroCheck(t *testing.T) {
	modes := []struct {
		name string
		mode tabular.HeterogeneousMode
	}{
		{"first wins", tabular.HeterogeneousFirstWins},
		{"reject", tabular.HeterogeneousReject},
		{"union", tabular.HeterogeneousUnion},
		{"partition", tabular.HeterogeneousPartition},
	}

	for _, mm := range modes {
		t.Run(mm.name, func(t *testing.T) {
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
			p := tabular.NewPrinter(&out, tabular.WithDelimiter(','), tabular.WithHeterogeneousMode(mm.mode))
			require.NoError(t, p.WriteValue(sv1), "first projection row must not error")
			require.NoError(t, p.WriteValue(sv2), "second projection row must not error")
			require.NoError(t, p.Flush())

			r := csv.NewReader(strings.NewReader(out.String()))
			rows, err := r.ReadAll()
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(rows), 3, "must have at least header + 2 data rows")
		})
	}
}

func TestPrinterHeterogeneousModeAccessor(t *testing.T) {
	var out bytes.Buffer

	p := tabular.NewPrinter(&out)
	assert.Equal(t, tabular.HeterogeneousFirstWins, p.HeterogeneousMode(), "default mode")

	p = tabular.NewPrinter(&out, tabular.WithHeterogeneousMode(tabular.HeterogeneousPartition))
	assert.Equal(t, tabular.HeterogeneousPartition, p.HeterogeneousMode())
}
