package main

import (
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
)

// HeterogeneousMode controls what the tabular printer does when a row with a
// different struct type arrives after the first row has locked the schema.
type HeterogeneousMode int

const (
	// HeterogeneousFirstWins silently drops rows whose type differs from the
	// first row's type. No error is returned; the row is simply skipped.
	HeterogeneousFirstWins HeterogeneousMode = iota

	// HeterogeneousReject returns an error when a row with a different type
	// arrives after the schema has been locked.
	HeterogeneousReject

	// HeterogeneousUnion grows the header when new columns appear. Rows that
	// were already printed cannot be backfilled with the new columns; they
	// receive empty cells for any column that did not exist at the time they
	// were written. Rows from the new type receive empty cells for columns
	// they do not have.
	HeterogeneousUnion

	// HeterogeneousPartition emits a blank line followed by a new header row
	// whenever the struct type changes, then continues printing with the new
	// type's schema.
	HeterogeneousPartition
)

// tabularOption is a functional option for newTabularPrinter.
type tabularOption func(*tabularPrinter)

// withDelimiter sets the CSV/TSV column delimiter.
func withDelimiter(r rune) tabularOption {
	return func(tp *tabularPrinter) { tp.w.Comma = r }
}

// withNoHeaders suppresses the header row when b is true.
func withNoHeaders(b bool) tabularOption {
	return func(tp *tabularPrinter) { tp.noHeaders = b }
}

// withHeterogeneousMode sets the heterogeneous-type handling mode.
func withHeterogeneousMode(m HeterogeneousMode) tabularOption {
	return func(tp *tabularPrinter) { tp.heteroMode = m }
}

// withBytesFormat sets the rendering format for BytesValue cells.
func withBytesFormat(f gobspect.BytesFormat) tabularOption {
	return func(tp *tabularPrinter) { tp.bytesFormat = f }
}

// withMaxBytes sets the byte-slice truncation limit (0 = no limit).
func withMaxBytes(n int) tabularOption {
	return func(tp *tabularPrinter) { tp.maxBytes = n }
}

// withStream sets the gobspect.Stream used for canonical type lookups.
func withStream(s *gobspect.Stream) tabularOption {
	return func(tp *tabularPrinter) { tp.stream = s }
}

// tabularPrinter writes gobspect.Value nodes as CSV or TSV rows.
//
// Column order is determined by the canonical type definition for the first
// struct received, looked up via stream.TypeByID. For projection structs
// (TypeName == query.ProjectionTypeName), the struct's own fields define the
// column order, and heterogeneous source types are accepted without error.
//
// When structs of a locked type arrive with fewer fields than the header (gob
// sparse encoding omits zero-value fields), the missing columns are emitted as
// empty strings so every row has the same column count as the header.
//
// The behavior when structs of a different type arrive is controlled by
// HeterogeneousMode:
//   - HeterogeneousFirstWins (default): silently drop mismatched rows.
//   - HeterogeneousReject: return an error.
//   - HeterogeneousUnion: grow the header to include new columns. NOTE: rows
//     already printed cannot be backfilled; they retain empty cells for new
//     columns.
//   - HeterogeneousPartition: emit a blank line and a new header row when the
//     type changes, then continue with the new schema.
type tabularPrinter struct {
	w          *csv.Writer
	rawWriter  io.Writer // underlying writer, needed for blank-line in Partition mode
	stream     *gobspect.Stream
	noHeaders  bool
	headerDone bool
	headers    []string // canonical column names, set on first struct

	// Heterogeneous-type tracking.
	heteroMode   HeterogeneousMode
	lockedTypeID int    // stream-scoped type ID of the first non-projection struct
	hasLock      bool   // true once lockedTypeID is set
	lockedName   string // display name of locked type, for error messages

	projMode    bool // true when first struct had TypeName == query.ProjectionTypeName
	rowCount    int  // data rows written so far (for error messages)
	bytesFormat gobspect.BytesFormat
	maxBytes    int
}

// newTabularPrinter creates a tabularPrinter that writes to out.
// Defaults: comma delimiter, headers on, HeterogeneousFirstWins, BytesHex, no truncation.
// stream may be nil; when nil, TypeByID lookups are skipped and column order
// falls back to the struct's own Fields slice (same as projection mode).
func newTabularPrinter(out io.Writer, opts ...tabularOption) *tabularPrinter {
	w := csv.NewWriter(out)
	w.Comma = ','
	tp := &tabularPrinter{
		w:          w,
		rawWriter:  out,
		bytesFormat: gobspect.BytesHex,
	}
	for _, o := range opts {
		o(tp)
	}
	return tp
}

// cellString converts a single Value to a flat string suitable for a CSV cell,
// respecting the printer's configured bytes format and truncation limit.
// Complex nested types are squashed to descriptive placeholders.
func (tp *tabularPrinter) cellString(v gobspect.Value) string {
	switch v := v.(type) {
	case gobspect.BytesValue:
		return gobspect.FormatBytes(v.V, tp.bytesFormat, tp.maxBytes)
	default:
		return cellString(v)
	}
}

// WriteValue writes a single Value as a tabular row. On the first call it also
// emits a header row (unless --no-headers was set).
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
	tp.rowCount++

	// Projection case: projection structs always use their own fields for
	// column order. Heterogeneous source types are allowed regardless of mode.
	if sv.TypeName == query.ProjectionTypeName {
		if !tp.headerDone {
			tp.headerDone = true
			tp.projMode = true
			tp.headers = fieldNames(sv.Fields)
			if !tp.noHeaders {
				if err := tp.w.Write(tp.headers); err != nil {
					return err
				}
			}
		}
		return tp.w.Write(tp.cellStrings(sv.Fields))
	}

	// Non-projection: use TypeByID to get canonical field order.
	if !tp.headerDone {
		tp.headerDone = true
		tp.hasLock = true
		tp.lockedTypeID = sv.GobTypeID
		tp.lockedName = tp.typeName(sv.GobTypeID, sv.TypeName)

		tp.headers = tp.canonicalHeaders(sv)
		if !tp.noHeaders {
			if err := tp.w.Write(tp.headers); err != nil {
				return err
			}
		}
		return tp.w.Write(tp.sparseRow(sv))
	}

	// Schema is already locked. Check whether this row has a different type.
	if tp.hasLock && sv.GobTypeID != 0 && sv.GobTypeID != tp.lockedTypeID {
		incomingName := tp.typeName(sv.GobTypeID, sv.TypeName)
		switch tp.heteroMode {
		case HeterogeneousFirstWins:
			// Silently skip.
			return nil

		case HeterogeneousReject:
			return fmt.Errorf("row %d has type %q but table is locked to %q; use a field projection like '.Items.*.Field1,Field2' to unify columns",
				tp.rowCount, incomingName, tp.lockedName)

		case HeterogeneousUnion:
			// Grow the header with any columns this type adds.
			// NOTE: rows already printed cannot be backfilled with new columns.
			tp.growHeaders(sv)
			return tp.w.Write(tp.sparseRow(sv))

		case HeterogeneousPartition:
			// Emit a blank line, then a new header, then the row.
			tp.w.Flush()
			fmt.Fprintln(tp.rawWriter)
			// Reset header state for the new type.
			tp.lockedTypeID = sv.GobTypeID
			tp.lockedName = incomingName
			tp.headers = tp.canonicalHeaders(sv)
			if !tp.noHeaders {
				if err := tp.w.Write(tp.headers); err != nil {
					return err
				}
			}
			return tp.w.Write(tp.sparseRow(sv))
		}
	}

	return tp.w.Write(tp.sparseRow(sv))
}

// growHeaders extends tp.headers with any field names in sv that are not
// already present. Used by HeterogeneousUnion.
func (tp *tabularPrinter) growHeaders(sv gobspect.StructValue) {
	existing := make(map[string]bool, len(tp.headers))
	for _, h := range tp.headers {
		existing[h] = true
	}
	newCols := tp.canonicalHeaders(sv)
	added := false
	for _, name := range newCols {
		if !existing[name] {
			tp.headers = append(tp.headers, name)
			existing[name] = true
			added = true
		}
	}
	if added && !tp.noHeaders {
		// Re-emit the header row to reflect the new columns.
		// Rows already written do not get updated (streaming limitation).
		_ = tp.w.Write(tp.headers)
	}
}

// canonicalHeaders returns the header slice for sv. It tries TypeByID first
// for definition-order fields; falls back to the struct's own Fields.
func (tp *tabularPrinter) canonicalHeaders(sv gobspect.StructValue) []string {
	if tp.stream != nil && sv.GobTypeID != 0 {
		if ti, ok := tp.stream.TypeByID(sv.GobTypeID); ok && len(ti.Fields) > 0 {
			names := make([]string, len(ti.Fields))
			for i, f := range ti.Fields {
				names[i] = f.Name
			}
			return names
		}
	}
	return fieldNames(sv.Fields)
}

// typeName returns the human-readable type name for a type ID, using TypeByID
// when available, falling back to fallback.
func (tp *tabularPrinter) typeName(id int, fallback string) string {
	if tp.stream != nil {
		if ti, ok := tp.stream.TypeByID(id); ok && ti.Name != "" {
			return ti.Name
		}
	}
	return fallback
}

// sparseRow builds a row in header order, filling empty strings for absent fields.
func (tp *tabularPrinter) sparseRow(sv gobspect.StructValue) []string {
	byName := make(map[string]gobspect.Value, len(sv.Fields))
	for _, f := range sv.Fields {
		byName[f.Name] = f.Value
	}
	row := make([]string, len(tp.headers))
	for i, name := range tp.headers {
		if v, ok := byName[name]; ok {
			row[i] = tp.cellString(v)
		}
		// missing field → empty string (zero value of string)
	}
	return row
}

// writeScalarRow writes a single-column row for non-struct values.
func (tp *tabularPrinter) writeScalarRow(v gobspect.Value) error {
	tp.rowCount++
	if !tp.headerDone {
		tp.headerDone = true
		if !tp.noHeaders {
			if err := tp.w.Write([]string{"value"}); err != nil {
				return err
			}
		}
	}
	return tp.w.Write([]string{tp.cellString(v)})
}

// Flush flushes the underlying CSV writer and returns any write error.
func (tp *tabularPrinter) Flush() error {
	tp.w.Flush()
	return tp.w.Error()
}

// Error returns any error from the underlying CSV writer.
func (tp *tabularPrinter) Error() error {
	return tp.w.Error()
}

// fieldNames extracts field names from a Fields slice.
func fieldNames(fields []gobspect.Field) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return names
}

// cellStrings converts a Fields slice to a row of cell strings using the
// printer's configured bytes format.
func (tp *tabularPrinter) cellStrings(fields []gobspect.Field) []string {
	row := make([]string, len(fields))
	for i, f := range fields {
		row[i] = tp.cellString(f.Value)
	}
	return row
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
