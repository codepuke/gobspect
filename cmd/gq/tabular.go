package main

import (
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/codepuke/gobspect"
)

// tabularPrinter writes gobspect.Value nodes as CSV or TSV rows.
type tabularPrinter struct {
	w          *csv.Writer
	noHeaders  bool
	headerDone bool
}

// newTabularPrinter creates a tabularPrinter that writes to out.
// delimiter is ',' for CSV or '\t' for TSV.
func newTabularPrinter(out io.Writer, delimiter rune, noHeaders bool) *tabularPrinter {
	w := csv.NewWriter(out)
	w.Comma = delimiter
	return &tabularPrinter{w: w, noHeaders: noHeaders}
}

// WriteValue writes a single Value as a tabular row. On the first call, it
// also emits a header row (unless --no-headers was set).
func (tp *tabularPrinter) WriteValue(v gobspect.Value) error {
	// Unwrap InterfaceValue so we see the concrete type.
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		v = iv.Value
	}

	sv, isStruct := v.(gobspect.StructValue)
	if isStruct {
		return tp.writeStructRow(sv)
	}
	return tp.writeScalarRow(v)
}

// writeStructRow writes header (on first call) and a field-per-column row.
func (tp *tabularPrinter) writeStructRow(sv gobspect.StructValue) error {
	if !tp.headerDone {
		tp.headerDone = true
		if !tp.noHeaders {
			headers := make([]string, len(sv.Fields))
			for i, f := range sv.Fields {
				headers[i] = f.Name
			}
			if err := tp.w.Write(headers); err != nil {
				return err
			}
		}
	}

	row := make([]string, len(sv.Fields))
	for i, f := range sv.Fields {
		row[i] = cellString(f.Value)
	}
	return tp.w.Write(row)
}

// writeScalarRow writes a single-column row for non-struct values.
func (tp *tabularPrinter) writeScalarRow(v gobspect.Value) error {
	if !tp.headerDone {
		tp.headerDone = true
		if !tp.noHeaders {
			if err := tp.w.Write([]string{"value"}); err != nil {
				return err
			}
		}
	}
	return tp.w.Write([]string{cellString(v)})
}

// Flush flushes the underlying CSV writer.
func (tp *tabularPrinter) Flush() {
	tp.w.Flush()
}

// Error returns any error from the underlying CSV writer.
func (tp *tabularPrinter) Error() error {
	return tp.w.Error()
}

// cellString converts a single Value to a flat string suitable for a CSV cell.
// Complex nested types are squashed to descriptive placeholders.
func cellString(v gobspect.Value) string {
	switch v := v.(type) {
	case gobspect.StringValue:
		return v.V
	case gobspect.IntValue:
		return fmt.Sprintf("%d", v.V)
	case gobspect.UintValue:
		return fmt.Sprintf("%d", v.V)
	case gobspect.FloatValue:
		return fmt.Sprintf("%g", v.V)
	case gobspect.ComplexValue:
		if v.Imag >= 0 {
			return fmt.Sprintf("(%g+%gi)", v.Real, v.Imag)
		}
		return fmt.Sprintf("(%g%gi)", v.Real, v.Imag)
	case gobspect.BoolValue:
		if v.V {
			return "true"
		}
		return "false"
	case gobspect.NilValue:
		return ""
	case gobspect.BytesValue:
		return hex.EncodeToString(v.V)
	case gobspect.OpaqueValue:
		if v.Decoded != nil {
			if s, ok := v.Decoded.(string); ok {
				return s
			}
			return fmt.Sprint(v.Decoded)
		}
		return "(opaque)"
	case gobspect.InterfaceValue:
		return cellString(v.Value)
	case gobspect.StructValue:
		return "(struct)"
	case gobspect.SliceValue:
		return "(array)"
	case gobspect.ArrayValue:
		return "(array)"
	case gobspect.MapValue:
		return "(map)"
	default:
		return fmt.Sprintf("%v", v)
	}
}
