// Package tabular writes gobspect.Value nodes as CSV or TSV rows.
package tabular

import (
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
)

// HeterogeneousMode controls what the Printer does when a row with a different
// struct type arrives after the first row has locked the schema.
type HeterogeneousMode int

const (
	// HeterogeneousFirstWins silently drops rows whose type differs from the
	// first row's type.
	HeterogeneousFirstWins HeterogeneousMode = iota

	// HeterogeneousReject returns an error when a row with a different type
	// arrives after the schema has been locked.
	HeterogeneousReject

	// HeterogeneousUnion grows the header when new columns appear. Rows already
	// printed cannot be backfilled; they receive empty cells for new columns.
	HeterogeneousUnion

	// HeterogeneousPartition emits a blank line followed by a new header row
	// whenever the struct type changes, then continues with the new type's schema.
	HeterogeneousPartition
)

// ParseHeterogeneousMode converts a string to a HeterogeneousMode constant.
// Accepts "first", "reject", "union", or "partition" (case-insensitive).
// An empty string maps to HeterogeneousFirstWins. Returns (HeterogeneousFirstWins, false)
// for unrecognised values.
func ParseHeterogeneousMode(s string) (HeterogeneousMode, bool) {
	switch strings.ToLower(s) {
	case "", "first":
		return HeterogeneousFirstWins, true
	case "reject":
		return HeterogeneousReject, true
	case "union":
		return HeterogeneousUnion, true
	case "partition":
		return HeterogeneousPartition, true
	default:
		return HeterogeneousFirstWins, false
	}
}

// Option is a functional option for [NewPrinter].
type Option func(*Printer)

// WithDelimiter sets the CSV/TSV column delimiter.
func WithDelimiter(r rune) Option {
	return func(p *Printer) { p.w.Comma = r }
}

// WithNoHeaders suppresses the header row when b is true.
func WithNoHeaders(b bool) Option {
	return func(p *Printer) { p.noHeaders = b }
}

// WithHeterogeneousMode sets the heterogeneous-type handling mode.
func WithHeterogeneousMode(m HeterogeneousMode) Option {
	return func(p *Printer) { p.heteroMode = m }
}

// WithBytesFormat sets the rendering format for BytesValue cells.
func WithBytesFormat(f gobspect.BytesFormat) Option {
	return func(p *Printer) { p.bytesFormat = f }
}

// WithMaxBytes sets the byte-slice truncation limit (0 = no limit).
func WithMaxBytes(n int) Option {
	return func(p *Printer) { p.maxBytes = n }
}

// WithStream sets the [gobspect.Stream] used for canonical type-definition
// lookups. When nil, column order falls back to the struct's own Fields slice.
func WithStream(s *gobspect.Stream) Option {
	return func(p *Printer) { p.stream = s }
}

// Printer writes gobspect.Value nodes as CSV or TSV rows.
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
// [HeterogeneousMode].
type Printer struct {
	w         *csv.Writer
	rawWriter io.Writer
	stream    *gobspect.Stream
	noHeaders bool
	headerDone bool
	headers   []string

	heteroMode   HeterogeneousMode
	lockedTypeID int
	hasLock      bool
	lockedName   string

	projMode    bool
	rowCount    int
	bytesFormat gobspect.BytesFormat
	maxBytes    int
}

// NewPrinter creates a Printer that writes to out.
// Defaults: comma delimiter, headers on, HeterogeneousFirstWins, BytesHex, no truncation.
func NewPrinter(out io.Writer, opts ...Option) *Printer {
	w := csv.NewWriter(out)
	w.Comma = ','
	p := &Printer{
		w:           w,
		rawWriter:   out,
		bytesFormat: gobspect.BytesHex,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// cellString converts a single Value to a flat string for a CSV cell,
// respecting the printer's configured bytes format and truncation limit.
func (p *Printer) cellString(v gobspect.Value) string {
	if bv, ok := v.(gobspect.BytesValue); ok {
		return gobspect.FormatBytes(bv.V, p.bytesFormat, p.maxBytes)
	}
	return CellString(v)
}

// WriteValue writes a single Value as a tabular row. On the first call it also
// emits a header row (unless WithNoHeaders was set).
func (p *Printer) WriteValue(v gobspect.Value) error {
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		v = iv.Value
	}

	sv, isStruct := v.(gobspect.StructValue)
	if isStruct {
		return p.writeStructRow(sv)
	}
	return p.writeScalarRow(v)
}

// writeStructRow writes header (on first call) and a field-per-column row.
func (p *Printer) writeStructRow(sv gobspect.StructValue) error {
	p.rowCount++

	if sv.TypeName == query.ProjectionTypeName {
		if !p.headerDone {
			p.headerDone = true
			p.projMode = true
			p.headers = fieldNames(sv.Fields)
			if !p.noHeaders {
				if err := p.w.Write(p.headers); err != nil {
					return err
				}
			}
		}
		return p.w.Write(p.cellStrings(sv.Fields))
	}

	if !p.headerDone {
		p.headerDone = true
		p.hasLock = true
		p.lockedTypeID = sv.GobTypeID
		p.lockedName = p.typeName(sv.GobTypeID, sv.TypeName)

		p.headers = p.canonicalHeaders(sv)
		if !p.noHeaders {
			if err := p.w.Write(p.headers); err != nil {
				return err
			}
		}
		return p.w.Write(p.sparseRow(sv))
	}

	if p.hasLock && sv.GobTypeID != 0 && sv.GobTypeID != p.lockedTypeID {
		incomingName := p.typeName(sv.GobTypeID, sv.TypeName)
		switch p.heteroMode {
		case HeterogeneousFirstWins:
			return nil

		case HeterogeneousReject:
			return fmt.Errorf("row %d has type %q but table is locked to %q; use a field projection like '.Items.*.Field1,Field2' to unify columns",
				p.rowCount, incomingName, p.lockedName)

		case HeterogeneousUnion:
			p.growHeaders(sv)
			return p.w.Write(p.sparseRow(sv))

		case HeterogeneousPartition:
			p.w.Flush()
			fmt.Fprintln(p.rawWriter)
			p.lockedTypeID = sv.GobTypeID
			p.lockedName = incomingName
			p.headers = p.canonicalHeaders(sv)
			if !p.noHeaders {
				if err := p.w.Write(p.headers); err != nil {
					return err
				}
			}
			return p.w.Write(p.sparseRow(sv))
		}
	}

	return p.w.Write(p.sparseRow(sv))
}

// growHeaders extends p.headers with any field names in sv not already present.
// Used by HeterogeneousUnion.
func (p *Printer) growHeaders(sv gobspect.StructValue) {
	existing := make(map[string]bool, len(p.headers))
	for _, h := range p.headers {
		existing[h] = true
	}
	newCols := p.canonicalHeaders(sv)
	added := false
	for _, name := range newCols {
		if !existing[name] {
			p.headers = append(p.headers, name)
			existing[name] = true
			added = true
		}
	}
	if added && !p.noHeaders {
		_ = p.w.Write(p.headers)
	}
}

// canonicalHeaders returns the header slice for sv. It tries TypeByID first
// for definition-order fields; falls back to the struct's own Fields.
func (p *Printer) canonicalHeaders(sv gobspect.StructValue) []string {
	if p.stream != nil && sv.GobTypeID != 0 {
		if ti, ok := p.stream.TypeByID(sv.GobTypeID); ok && len(ti.Fields) > 0 {
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
func (p *Printer) typeName(id int, fallback string) string {
	if p.stream != nil {
		if ti, ok := p.stream.TypeByID(id); ok && ti.Name != "" {
			return ti.Name
		}
	}
	return fallback
}

// sparseRow builds a row in header order, filling empty strings for absent fields.
func (p *Printer) sparseRow(sv gobspect.StructValue) []string {
	byName := make(map[string]gobspect.Value, len(sv.Fields))
	for _, f := range sv.Fields {
		byName[f.Name] = f.Value
	}
	row := make([]string, len(p.headers))
	for i, name := range p.headers {
		if v, ok := byName[name]; ok {
			row[i] = p.cellString(v)
		}
	}
	return row
}

// writeScalarRow writes a single-column row for non-struct values.
func (p *Printer) writeScalarRow(v gobspect.Value) error {
	p.rowCount++
	if !p.headerDone {
		p.headerDone = true
		if !p.noHeaders {
			if err := p.w.Write([]string{"value"}); err != nil {
				return err
			}
		}
	}
	return p.w.Write([]string{p.cellString(v)})
}

// Flush flushes the underlying CSV writer and returns any write error.
func (p *Printer) Flush() error {
	p.w.Flush()
	return p.w.Error()
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
func (p *Printer) cellStrings(fields []gobspect.Field) []string {
	row := make([]string, len(fields))
	for i, f := range fields {
		row[i] = p.cellString(f.Value)
	}
	return row
}

// CellString converts a single Value to a flat string suitable for a CSV cell.
// BytesValue is rendered as lowercase hex. Complex nested types are squashed to
// descriptive placeholders. Use [Printer.WriteValue] for format-aware rendering
// of bytes (respecting the configured [BytesFormat] and max-bytes limit).
func CellString(v gobspect.Value) string {
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
		return CellString(v.Value)
	case gobspect.StructValue:
		return "(struct)"
	case gobspect.SliceValue:
		return "(slice)"
	case gobspect.ArrayValue:
		return "(array)"
	case gobspect.MapValue:
		return "(map)"
	default:
		return fmt.Sprintf("%v", v)
	}
}
