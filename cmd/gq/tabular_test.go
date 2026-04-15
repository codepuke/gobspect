package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
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
	tp := newTabularPrinter(&buf, ',', false)

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
	tp := newTabularPrinter(&buf, '\t', false)

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
	tp := newTabularPrinter(&buf, ',', true) // noHeaders = true

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
	tp := newTabularPrinter(&buf, ',', false)

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
	tp := newTabularPrinter(&buf, ',', false)

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
	tp := newTabularPrinter(&buf, ',', false)

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
	tp := newTabularPrinter(&buf, ',', true) // noHeaders = true

	require.NoError(t, tp.WriteValue(gobspect.IntValue{V: 7}))
	tp.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1, "just 1 data row, no header")
	assert.Equal(t, "7", lines[0])
}
