package gobspect

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormatOption configures the behaviour of [Format].
type FormatOption func(*formatConfig)

// BytesFormat controls how raw byte slices are rendered by [Format].
type BytesFormat int

const (
	BytesHex     BytesFormat = iota // lowercase hex string (default)
	BytesBase64                     // standard base64 (RFC 4648, with padding)
	BytesLiteral                    // Go-style: []byte{0xde, 0xad, ...}
)

// RedactConfig controls value redaction by field or map key name.
//
// When TextLength is 0 (the default), the number of fill characters emitted
// depends on the rendered form of the value being redacted:
//   - Single-line values: emit len([]rune(rendered)) fill chars, preserving
//     the visual width of the original text.
//   - Multi-line values (e.g. nested structs): emit exactly 3 fill chars ("***").
//     Counting runes across a multi-line rendering is not meaningful — the rune
//     total includes indentation, newlines, and braces rather than content length —
//     and inserting a flat string where a multi-line value was breaks visual
//     alignment regardless. A short placeholder is unambiguous and clean.
//
// Set TextLength > 0 to always emit exactly that many fill chars, regardless of
// the original rendered length or whether it is single- or multi-line.
//
// Note: Char is repeated by Unicode code point, not by terminal display width.
// Multi-byte fill characters (e.g. '█') produce the requested number of code
// points; terminal column width may differ.
type RedactConfig struct {
	Keys       []string // exact field/key names that trigger redaction
	Char       rune     // fill character; defaults to '*'
	TextLength int      // number of Char runes to emit; 0 = see type-level doc
}

// RedactTypesConfig controls value redaction by type name.
type RedactTypesConfig struct {
	Types      []string // type names that trigger redaction
	Char       rune     // fill character; defaults to '*'
	TextLength int      // number of Char runes to emit; 0 = preserve original length
}

type formatConfig struct {
	indent              string
	maxBytes            int
	rawOpaques          bool
	redactKeys          *RedactConfig
	redactTypes         *RedactTypesConfig
	bytesFormat         BytesFormat
	bytesFormatExplicit bool
}

// WithIndent sets the indentation string used for nested output. Default: "  ".
func WithIndent(indent string) FormatOption {
	return func(c *formatConfig) { c.indent = indent }
}

// WithMaxBytes sets the maximum number of raw bytes rendered for [OpaqueValue]
// and [BytesValue] output. Default: 64. Zero means no limit. Applies to all
// byte formats (hex, base64, literal): the byte slice is truncated before
// encoding when this limit is exceeded.
func WithMaxBytes(n int) FormatOption {
	return func(c *formatConfig) { c.maxBytes = n }
}

// WithRawOpaques controls whether decoded [OpaqueValue]s still show their raw
// bytes. When true, the raw bytes are always shown even when [OpaqueValue.Decoded] is set.
func WithRawOpaques(raw bool) FormatOption {
	return func(c *formatConfig) { c.rawOpaques = raw }
}

// WithRedactKeys redacts the rendered value of any struct field or map entry
// whose key matches one of cfg.Keys (case-sensitive exact match). The value is
// replaced at render time; the AST is never modified.
//
// Key matching for struct fields is by field name. For map entries, matching is
// by the formatted key string (the result of rendering the key through Format).
// Glob and regex matching are intentionally out of scope.
func WithRedactKeys(cfg RedactConfig) FormatOption {
	return func(c *formatConfig) { c.redactKeys = &cfg }
}

// WithRedactTypes redacts all values whose type name matches one of the names in
// cfg.Types. Checked against [StructValue.TypeName], [InterfaceValue.TypeName],
// and [OpaqueValue.TypeName]. May be combined with [WithRedactKeys]; both rules
// apply. cfg.Char and cfg.TextLength control the fill character and output
// length, identical to their meaning in [RedactConfig].
func WithRedactTypes(cfg RedactTypesConfig) FormatOption {
	return func(c *formatConfig) { c.redactTypes = &cfg }
}

// WithBytesFormat controls how [BytesValue] and [OpaqueValue.Raw] are rendered.
// When set explicitly, the printable UTF-8 shortcut (render as a quoted string)
// is suppressed and the requested format is always used.
func WithBytesFormat(f BytesFormat) FormatOption {
	return func(c *formatConfig) { c.bytesFormat = f; c.bytesFormatExplicit = true }
}

// redact replaces original with a fill character string. If char is zero, '*'
// is used. If textLength > 0, exactly that many characters are emitted.
// When textLength is 0:
//   - Single-line originals: emit len([]rune(original)) fill chars.
//   - Multi-line originals (contain '\n'): emit 3 fill chars ("***").
//     See [RedactConfig] for the rationale.
func redact(original string, char rune, textLength int) string {
	ch := char
	if ch == 0 {
		ch = '*'
	}
	n := textLength
	if n == 0 {
		if strings.ContainsRune(original, '\n') {
			n = 3
		} else {
			n = len([]rune(original))
		}
	}
	return strings.Repeat(string(ch), n)
}

// redactWithKeyCfg applies redaction using a RedactConfig.
func redactWithKeyCfg(original string, cfg RedactConfig) string {
	return redact(original, cfg.Char, cfg.TextLength)
}

// redactForType returns a redacted placeholder using the redactTypes config.
func redactForType(cfg *formatConfig) string {
	if cfg.redactTypes != nil {
		return redact("[redacted]", cfg.redactTypes.Char, cfg.redactTypes.TextLength)
	}
	return redact("[redacted]", 0, 0)
}

// inlineThreshold is the maximum character count for inline collection rendering.
const inlineThreshold = 72

// Format renders v as a human-readable string. Structs are always rendered as
// indented field trees. Maps, slices, and arrays are rendered inline when their
// formatted length fits within 72 characters and none of their elements require
// multi-line rendering; otherwise they are indented. Map entries are sorted by
// formatted key for deterministic output.
func Format(v Value, opts ...FormatOption) string {
	cfg := &formatConfig{
		indent:   "  ",
		maxBytes: 64,
	}
	for _, o := range opts {
		o(cfg)
	}
	return fmtValue(v, cfg, 0)
}

func fmtValue(v Value, cfg *formatConfig, depth int) string {
	// Type-based redaction: check before dispatching to concrete renderer.
	if cfg.redactTypes != nil {
		var typeName string
		switch v := v.(type) {
		case StructValue:
			typeName = v.TypeName
		case InterfaceValue:
			typeName = v.TypeName
		case OpaqueValue:
			typeName = v.TypeName
		}
		if typeName != "" && containsString(cfg.redactTypes.Types, typeName) {
			return redactForType(cfg)
		}
	}

	switch v := v.(type) {
	case BoolValue:
		if v.V {
			return "true"
		}
		return "false"
	case IntValue:
		return fmt.Sprintf("%d", v.V)
	case UintValue:
		return fmt.Sprintf("%d", v.V)
	case FloatValue:
		s := strconv.FormatFloat(v.V, 'g', -1, 64)
		// Ensure integer-valued floats are distinguishable from IntValue:
		// if the output contains no '.', 'e', or 'E' it looks like a bare
		// integer, so append ".0". NaN and Inf are never bare integers.
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s
	case ComplexValue:
		if math.Signbit(v.Imag) {
			return fmt.Sprintf("(%g%gi)", v.Real, v.Imag)
		}
		return fmt.Sprintf("(%g+%gi)", v.Real, v.Imag)
	case StringValue:
		return fmt.Sprintf("%q", v.V)
	case BytesValue:
		return fmtBytes(v.V, cfg)
	case NilValue:
		return "nil"
	case InterfaceValue:
		return fmtInterface(v, cfg, depth)
	case OpaqueValue:
		return fmtOpaque(v, cfg)
	case StructValue:
		return fmtStruct(v, cfg, depth)
	case MapValue:
		return fmtMap(v, cfg, depth)
	case SliceValue:
		return fmtSlice(v, cfg, depth)
	case ArrayValue:
		return fmtArray(v, cfg, depth)
	default:
		return fmt.Sprintf("?(unknown %T)", v)
	}
}

// fmtBytes renders a []byte value. When no explicit BytesFormat is set,
// printable UTF-8 slices are rendered as a Go-quoted string. Otherwise the
// requested format is used.
func fmtBytes(b []byte, cfg *formatConfig) string {
	if !cfg.bytesFormatExplicit && isPrintableUTF8(b) {
		truncated, ellipsis := truncateBytes(b, cfg.maxBytes)
		s := fmt.Sprintf("%q", truncated)
		if ellipsis {
			s += "…"
		}
		return s
	}
	return fmtBytesWithFormat(b, cfg)
}

// fmtBytesWithFormat encodes b using the BytesFormat in cfg, without the
// printable-UTF-8 shortcut. Used directly for OpaqueValue.Raw rendering.
func fmtBytesWithFormat(b []byte, cfg *formatConfig) string {
	if len(b) == 0 {
		return "[]"
	}
	switch cfg.bytesFormat {
	case BytesBase64:
		truncated, ellipsis := truncateBytes(b, cfg.maxBytes)
		s := base64.StdEncoding.EncodeToString(truncated)
		if ellipsis {
			s += "…"
		}
		return s
	case BytesLiteral:
		truncated, ellipsis := truncateBytes(b, cfg.maxBytes)
		parts := make([]string, len(truncated))
		for i, byt := range truncated {
			parts[i] = fmt.Sprintf("0x%02x", byt)
		}
		s := "[]byte{" + strings.Join(parts, ", ") + "}"
		if ellipsis {
			s += "…"
		}
		return s
	default: // BytesHex
		return fmtHex(b, cfg.maxBytes)
	}
}

// truncateBytes returns b truncated to maxBytes (if maxBytes > 0 and len(b)
// exceeds it) and a boolean indicating whether truncation occurred.
func truncateBytes(b []byte, maxBytes int) ([]byte, bool) {
	if maxBytes > 0 && len(b) > maxBytes {
		return b[:maxBytes], true
	}
	return b, false
}

// isPrintableUTF8 reports whether b is valid UTF-8 where every rune is
// printable. Empty slices return false so they render unambiguously as "[]"
// rather than the ambiguous "".
func isPrintableUTF8(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// fmtHex returns the lowercase hex encoding of b, truncated to maxBytes with a
// '…' suffix when maxBytes > 0 and len(b) exceeds it.
func fmtHex(b []byte, maxBytes int) string {
	if maxBytes > 0 && len(b) > maxBytes {
		return hex.EncodeToString(b[:maxBytes]) + "…"
	}
	return hex.EncodeToString(b)
}

// fmtOpaque renders an OpaqueValue. When Decoded is non-nil and rawOpaques is
// false the decoded representation is returned directly: strings as-is,
// anything else via fmt.Sprint. Otherwise the raw bytes are rendered using
// the configured byte format, prefixed with "(TypeName) " when TypeName is non-empty.
func fmtOpaque(v OpaqueValue, cfg *formatConfig) string {
	if v.Decoded != nil && !cfg.rawOpaques {
		if s, ok := v.Decoded.(string); ok {
			return s
		}
		return fmt.Sprint(v.Decoded)
	}
	rawStr := fmtBytesWithFormat(v.Raw, cfg)
	if v.TypeName != "" {
		return "(" + v.TypeName + ") " + rawStr
	}
	return rawStr
}

// fmtInterface renders an InterfaceValue. A nil interface renders as "nil".
// A non-nil interface renders the concrete value, prefixed with "(TypeName) "
// when TypeName is non-empty.
func fmtInterface(v InterfaceValue, cfg *formatConfig, depth int) string {
	if _, ok := v.Value.(NilValue); ok {
		return "nil"
	}
	inner := fmtValue(v.Value, cfg, depth)
	if v.TypeName != "" {
		return "(" + v.TypeName + ") " + inner
	}
	return inner
}

// fmtStruct renders a StructValue as an indented field tree. Empty structs
// render as "TypeName{}".
func fmtStruct(v StructValue, cfg *formatConfig, depth int) string {
	name := v.TypeName
	if name == "" {
		name = "struct"
	}
	if len(v.Fields) == 0 {
		return name + "{}"
	}
	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	var sb strings.Builder
	sb.WriteString(name)
	sb.WriteString("{\n")
	for _, f := range v.Fields {
		sb.WriteString(fieldIndent)
		sb.WriteString(f.Name)
		sb.WriteString(": ")
		rendered := fmtValue(f.Value, cfg, depth+1)
		if cfg.redactKeys != nil && containsString(cfg.redactKeys.Keys, f.Name) {
			rendered = redactWithKeyCfg(rendered, *cfg.redactKeys)
		}
		sb.WriteString(rendered)
		sb.WriteString("\n")
	}
	sb.WriteString(prefix)
	sb.WriteString("}")
	return sb.String()
}

// containsString reports whether s is an element of the slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// fmtMap renders a MapValue. Entries are sorted by their formatted key for
// deterministic output. Short maps render inline; long maps render indented.
func fmtMap(v MapValue, cfg *formatConfig, depth int) string {
	header := "map[" + v.KeyType + "]" + v.ElemType
	if len(v.Entries) == 0 {
		return header + "{}"
	}

	// Precompute the canonical (depth-0) key string for each entry once.
	// This avoids re-rendering every key on each comparison during sorting,
	// reducing O(n log n) renders to O(n).
	type keyedEntry struct {
		canonKey string
		entry    MapEntry
	}
	keyed := make([]keyedEntry, len(v.Entries))
	for i, e := range v.Entries {
		keyed[i] = keyedEntry{canonKey: fmtValue(e.Key, cfg, 0), entry: e}
	}
	sort.Slice(keyed, func(i, j int) bool {
		return keyed[i].canonKey < keyed[j].canonKey
	})

	// Attempt inline rendering: fail if any element is itself multi-line.
	parts := make([]string, 0, len(keyed))
	canInline := true
	for _, ke := range keyed {
		k := ke.canonKey
		vv := fmtValue(ke.entry.Value, cfg, 0)
		if cfg.redactKeys != nil && containsString(cfg.redactKeys.Keys, k) {
			vv = redactWithKeyCfg(vv, *cfg.redactKeys)
		}
		if strings.ContainsRune(k, '\n') || strings.ContainsRune(vv, '\n') {
			canInline = false
			break
		}
		parts = append(parts, k+": "+vv)
	}
	if canInline {
		inline := header + "{" + strings.Join(parts, ", ") + "}"
		if len(inline) <= inlineThreshold {
			return inline
		}
	}

	// Indented rendering.
	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("{\n")
	for _, ke := range keyed {
		sb.WriteString(fieldIndent)
		// Render the display key at depth+1 for correct indentation of any
		// nested struct/map keys; use the precomputed depth-0 canon key only
		// for redact matching.
		sb.WriteString(fmtValue(ke.entry.Key, cfg, depth+1))
		sb.WriteString(": ")
		rendered := fmtValue(ke.entry.Value, cfg, depth+1)
		if cfg.redactKeys != nil && containsString(cfg.redactKeys.Keys, ke.canonKey) {
			rendered = redactWithKeyCfg(rendered, *cfg.redactKeys)
		}
		sb.WriteString(rendered)
		sb.WriteString("\n")
	}
	sb.WriteString(prefix)
	sb.WriteString("}")
	return sb.String()
}

// fmtSlice renders a SliceValue inline when short, indented when long.
func fmtSlice(v SliceValue, cfg *formatConfig, depth int) string {
	header := "[]" + v.ElemType
	if len(v.Elems) == 0 {
		return header + "{}"
	}

	parts := make([]string, 0, len(v.Elems))
	canInline := true
	for _, e := range v.Elems {
		s := fmtValue(e, cfg, 0)
		if strings.ContainsRune(s, '\n') {
			canInline = false
			break
		}
		parts = append(parts, s)
	}
	if canInline {
		inline := header + "{" + strings.Join(parts, ", ") + "}"
		if len(inline) <= inlineThreshold {
			return inline
		}
	}

	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("{\n")
	for _, e := range v.Elems {
		sb.WriteString(fieldIndent)
		sb.WriteString(fmtValue(e, cfg, depth+1))
		sb.WriteString("\n")
	}
	sb.WriteString(prefix)
	sb.WriteString("}")
	return sb.String()
}

// fmtArray renders an ArrayValue inline when short, indented when long.
func fmtArray(v ArrayValue, cfg *formatConfig, depth int) string {
	header := fmt.Sprintf("[%d]%s", v.Len, v.ElemType)
	if len(v.Elems) == 0 {
		return header + "{}"
	}

	parts := make([]string, 0, len(v.Elems))
	canInline := true
	for _, e := range v.Elems {
		s := fmtValue(e, cfg, 0)
		if strings.ContainsRune(s, '\n') {
			canInline = false
			break
		}
		parts = append(parts, s)
	}
	if canInline {
		inline := header + "{" + strings.Join(parts, ", ") + "}"
		if len(inline) <= inlineThreshold {
			return inline
		}
	}

	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("{\n")
	for _, e := range v.Elems {
		sb.WriteString(fieldIndent)
		sb.WriteString(fmtValue(e, cfg, depth+1))
		sb.WriteString("\n")
	}
	sb.WriteString(prefix)
	sb.WriteString("}")
	return sb.String()
}
