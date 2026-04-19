package gobspect

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormatOption configures the behaviour of [Format].
type FormatOption func(*formatConfig)

// MapOrder controls the ordering of map entries in [Format] output.
type MapOrder int

const (
	MapOrderSorted    MapOrder = iota // default: sort entries by formatted key
	MapOrderInsertion                 // skip sorting; use wire (insertion) order
)

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

// Style is a prefix/suffix pair used to wrap rendered tokens with markup or
// escape sequences. A zero-valued Style applies no wrapping.
type Style struct {
	Prefix, Suffix string
}

// apply wraps s with the Style's Prefix and Suffix. A zero-valued Style
// returns s unchanged.
func (st Style) apply(s string) string {
	if st.Prefix == "" && st.Suffix == "" {
		return s
	}
	return st.Prefix + s + st.Suffix
}

// ColorScheme controls how different token roles are wrapped during rendering.
// A zero-valued ColorScheme produces no change to the output (identity).
type ColorScheme struct {
	FieldName    Style // struct field names, map keys for string-keyed maps
	TypeHeader   Style // struct/map/slice/array type name ("Foo", "[]int", "map[string]int")
	CloseBrace   Style // "{" and "}" — opening and closing braces
	String       Style // quoted string values
	Number       Style // int, uint, float, complex
	Bool         Style // true, false
	Nil          Style // nil
	OpaquePrefix Style // "(TypeName)" marker
	OpaqueValue  Style // the rendered opaque value after the prefix
	Bytes        Style // hex / base64 / literal byte output
}

// ANSIColorScheme is a pre-built ColorScheme that uses ANSI escape codes to
// produce syntax-highlighted terminal output. The colors match the coloring
// used by cmd/gq.
var ANSIColorScheme = ColorScheme{
	FieldName:    Style{Prefix: "\x1b[32m", Suffix: "\x1b[0m"},   // green
	TypeHeader:   Style{Prefix: "\x1b[1;36m", Suffix: "\x1b[0m"}, // bold cyan
	CloseBrace:   Style{},                                        // no color (plain "{" and "}")
	String:       Style{Prefix: "\x1b[33m", Suffix: "\x1b[0m"},   // yellow
	Number:       Style{Prefix: "\x1b[35m", Suffix: "\x1b[0m"},   // magenta
	Bool:         Style{Prefix: "\x1b[36m", Suffix: "\x1b[0m"},   // cyan
	Nil:          Style{Prefix: "\x1b[36m", Suffix: "\x1b[0m"},   // cyan
	OpaquePrefix: Style{Prefix: "\x1b[2m", Suffix: "\x1b[0m"},    // dim
	OpaqueValue:  Style{},                                        // delegated to inner value rendering
	Bytes:        Style{Prefix: "\x1b[2m", Suffix: "\x1b[0m"},    // dim
}

// NoColorScheme is a zero-valued ColorScheme that produces no color output.
var NoColorScheme = ColorScheme{}

type formatConfig struct {
	indent              string
	maxBytes            int
	rawOpaques          bool
	redactKeys          *RedactConfig
	redactTypes         *RedactTypesConfig
	bytesFormat         BytesFormat
	bytesFormatExplicit bool
	color               ColorScheme
	inlineWidth         int
	mapOrder            MapOrder
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

// WithColor applies the given [ColorScheme] to the rendered output. Each token
// role is wrapped with its corresponding [Style]'s Prefix and Suffix. A
// zero-valued scheme ([NoColorScheme]) is the identity: no escape sequences are
// emitted and the output is byte-identical to calling [Format] without this option.
func WithColor(scheme ColorScheme) FormatOption {
	return func(c *formatConfig) { c.color = scheme }
}

// WithInlineWidth sets the maximum character count at which maps, slices, and
// arrays are rendered inline rather than indented. The plain-text (no-color)
// length of the full inline form is compared against n. Default: 72. Pass 0 to
// use the default explicitly.
func WithInlineWidth(n int) FormatOption {
	return func(c *formatConfig) { c.inlineWidth = n }
}

// WithMapOrder sets the ordering of map entries in [Format] output.
// The default ([MapOrderSorted]) sorts entries by their plain-text formatted
// key for deterministic output. [MapOrderInsertion] skips sorting and iterates
// entries in the order they appear in [MapValue.Entries] (wire order).
func WithMapOrder(order MapOrder) FormatOption {
	return func(c *formatConfig) { c.mapOrder = order }
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
			n = utf8.RuneCountInString(original)
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

// writeStr writes s to w and returns any error.
func writeStr(w io.Writer, s string) error {
	_, err := io.WriteString(w, s)
	return err
}

// FormatTo renders v as a human-readable string and writes it to w. Structs are
// always rendered as indented field trees. Maps, slices, and arrays are rendered
// inline when their formatted length fits within the inline width (default 72,
// overridden by [WithInlineWidth]) and none of their elements require multi-line
// rendering; otherwise they are indented. Map entries are sorted by formatted key
// for deterministic output. The first write error aborts rendering and is returned.
func FormatTo(w io.Writer, v Value, opts ...FormatOption) error {
	cfg := &formatConfig{
		indent:   "  ",
		maxBytes: 64,
	}
	for _, o := range opts {
		o(cfg)
	}
	return fmtValueTo(w, v, cfg, 0)
}

// Format renders v as a human-readable string. Structs are always rendered as
// indented field trees. Maps, slices, and arrays are rendered inline when their
// formatted length fits within the inline width (default 72, overridden by
// [WithInlineWidth]) and none of their elements require multi-line rendering;
// otherwise they are indented. Map entries are sorted by formatted key for
// deterministic output.
func Format(v Value, opts ...FormatOption) string {
	var sb strings.Builder
	_ = FormatTo(&sb, v, opts...)
	return sb.String()
}

// fmtValueTo writes the formatted form of v to w.
func fmtValueTo(w io.Writer, v Value, cfg *formatConfig, depth int) error {
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
		if typeName != "" && slices.Contains(cfg.redactTypes.Types, typeName) {
			return writeStr(w, redactForType(cfg))
		}
	}

	switch v := v.(type) {
	case BoolValue:
		if v.V {
			return writeStr(w, cfg.color.Bool.apply("true"))
		}
		return writeStr(w, cfg.color.Bool.apply("false"))
	case IntValue:
		return writeStr(w, cfg.color.Number.apply(fmt.Sprintf("%d", v.V)))
	case UintValue:
		return writeStr(w, cfg.color.Number.apply(fmt.Sprintf("%d", v.V)))
	case FloatValue:
		s := strconv.FormatFloat(v.V, 'g', -1, 64)
		// Ensure integer-valued floats are distinguishable from IntValue:
		// if the output contains no '.', 'e', or 'E' it looks like a bare
		// integer, so append ".0". NaN and Inf are never bare integers.
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return writeStr(w, cfg.color.Number.apply(s))
	case ComplexValue:
		var s string
		if math.Signbit(v.Imag) {
			s = fmt.Sprintf("(%g%gi)", v.Real, v.Imag)
		} else {
			s = fmt.Sprintf("(%g+%gi)", v.Real, v.Imag)
		}
		return writeStr(w, cfg.color.Number.apply(s))
	case StringValue:
		return writeStr(w, cfg.color.String.apply(fmt.Sprintf("%q", v.V)))
	case BytesValue:
		return writeStr(w, cfg.color.Bytes.apply(fmtBytes(v.V, cfg)))
	case NilValue:
		return writeStr(w, cfg.color.Nil.apply("nil"))
	case InterfaceValue:
		return fmtInterfaceTo(w, v, cfg, depth)
	case OpaqueValue:
		return writeStr(w, fmtOpaque(v, cfg))
	case StructValue:
		return fmtStructTo(w, v, cfg, depth)
	case MapValue:
		return fmtMapTo(w, v, cfg, depth)
	case SliceValue:
		return fmtSliceTo(w, v, cfg, depth)
	case ArrayValue:
		return fmtArrayTo(w, v, cfg, depth)
	default:
		return writeStr(w, fmt.Sprintf("?(unknown %T)", v))
	}
}

// fmtValue renders v as a string. Used internally for inline-vs-multiline
// decisions and for cases where a string intermediate is required (e.g. redact
// length computation, key sorting).
func fmtValue(v Value, cfg *formatConfig, depth int) string {
	var sb strings.Builder
	_ = fmtValueTo(&sb, v, cfg, depth)
	return sb.String()
}

// plainConfig returns a copy of cfg with color cleared. Used when computing
// canonical/plain-text keys for sorting, redact matching, and inline-length
// threshold checks, so that ANSI escape codes do not affect those decisions.
func plainConfig(cfg *formatConfig) *formatConfig {
	if cfg.color == (ColorScheme{}) {
		return cfg // fast path: already no color
	}
	cp := *cfg
	cp.color = ColorScheme{}
	return &cp
}

// fmtPlainValue renders v as a plain (no-color) string regardless of cfg.color.
// Used for canonical key computation and inline threshold decisions.
func fmtPlainValue(v Value, cfg *formatConfig, depth int) string {
	return fmtValue(v, plainConfig(cfg), depth)
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

// FormatBytes encodes b using the given BytesFormat, truncating to maxBytes raw
// bytes before encoding when maxBytes > 0 and len(b) exceeds it. A '…' suffix
// is appended when truncation occurs. An empty slice always returns "[]".
//
// The printable-UTF-8 shortcut used by [Format] for [BytesValue] is intentionally
// absent here; the requested format is always applied.
func FormatBytes(b []byte, format BytesFormat, maxBytes int) string {
	cfg := &formatConfig{bytesFormat: format, maxBytes: maxBytes}
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
			return cfg.color.OpaqueValue.apply(s)
		}
		return cfg.color.OpaqueValue.apply(fmt.Sprint(v.Decoded))
	}
	rawStr := cfg.color.Bytes.apply(fmtBytesWithFormat(v.Raw, cfg))
	if v.TypeName != "" {
		return cfg.color.OpaquePrefix.apply("("+v.TypeName+")") + " " + rawStr
	}
	return rawStr
}

// fmtInterfaceTo renders an InterfaceValue to w. A nil interface renders as
// "nil". A non-nil interface renders the concrete value, prefixed with
// "(TypeName) " when TypeName is non-empty.
func fmtInterfaceTo(w io.Writer, v InterfaceValue, cfg *formatConfig, depth int) error {
	if _, ok := v.Value.(NilValue); ok {
		return writeStr(w, cfg.color.Nil.apply("nil"))
	}
	if v.TypeName != "" {
		if err := writeStr(w, cfg.color.OpaquePrefix.apply("("+v.TypeName+")")+" "); err != nil {
			return err
		}
	}
	return fmtValueTo(w, v.Value, cfg, depth)
}

// fmtStructTo renders a StructValue as an indented field tree to w. Empty
// structs render as "TypeName{}".
func fmtStructTo(w io.Writer, v StructValue, cfg *formatConfig, depth int) error {
	name := v.TypeName
	if name == "" {
		name = "struct"
	}
	if len(v.Fields) == 0 {
		return writeStr(w, cfg.color.TypeHeader.apply(name)+cfg.color.CloseBrace.apply("{}"))
	}
	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.apply(name)+cfg.color.CloseBrace.apply("{")+"\n"); err != nil {
		return err
	}
	for _, f := range v.Fields {
		if err := writeStr(w, fieldIndent); err != nil {
			return err
		}
		if err := writeStr(w, cfg.color.FieldName.apply(f.Name)+": "); err != nil {
			return err
		}
		rendered := fmtValue(f.Value, cfg, depth+1)
		if cfg.redactKeys != nil && slices.Contains(cfg.redactKeys.Keys, f.Name) {
			rendered = redactWithKeyCfg(rendered, *cfg.redactKeys)
		}
		if err := writeStr(w, rendered); err != nil {
			return err
		}
		if err := writeStr(w, "\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.apply("}"))
}


// fmtMapTo renders a MapValue to w. Entries are sorted by their formatted key
// for deterministic output. Short maps render inline; long maps render indented.
func fmtMapTo(w io.Writer, v MapValue, cfg *formatConfig, depth int) error {
	header := "map[" + v.KeyType + "]" + v.ElemType
	if len(v.Entries) == 0 {
		return writeStr(w, cfg.color.TypeHeader.apply(header)+cfg.color.CloseBrace.apply("{}"))
	}

	// Precompute the canonical (depth-0) plain-text key string for each entry.
	// Plain (no-color) keys are used for sorting and redact matching so that
	// ANSI escape codes do not affect those decisions.
	// This avoids re-rendering every key on each comparison during sorting,
	// reducing O(n log n) renders to O(n).
	pcfg := plainConfig(cfg)
	type keyedEntry struct {
		canonKey string
		entry    MapEntry
	}
	keyed := make([]keyedEntry, len(v.Entries))
	for i, e := range v.Entries {
		keyed[i] = keyedEntry{canonKey: fmtPlainValue(e.Key, cfg, 0), entry: e}
	}
	if cfg.mapOrder != MapOrderInsertion {
		slices.SortFunc(keyed, func(a, b keyedEntry) int {
			return strings.Compare(a.canonKey, b.canonKey)
		})
	}

	// Attempt inline rendering: use plain text for threshold check so ANSI
	// codes don't inflate the measured length.
	noColor := pcfg == cfg // true when cfg already has no color scheme
	plainParts := make([]string, 0, len(keyed))
	var colorParts []string
	if !noColor {
		colorParts = make([]string, 0, len(keyed))
	}
	canInline := true
	for _, ke := range keyed {
		plainK := ke.canonKey
		plainVV := fmtValue(ke.entry.Value, pcfg, 0)
		if cfg.redactKeys != nil && slices.Contains(cfg.redactKeys.Keys, plainK) {
			plainVV = redactWithKeyCfg(plainVV, *cfg.redactKeys)
		}
		if strings.ContainsRune(plainK, '\n') || strings.ContainsRune(plainVV, '\n') {
			canInline = false
			break
		}
		plainParts = append(plainParts, plainK+": "+plainVV)
		if !noColor {
			// Build the colored version only when a color scheme is active.
			colorK := fmtValue(ke.entry.Key, cfg, 0)
			colorVV := fmtValue(ke.entry.Value, cfg, 0)
			if cfg.redactKeys != nil && slices.Contains(cfg.redactKeys.Keys, plainK) {
				colorVV = redactWithKeyCfg(plainVV, *cfg.redactKeys)
			}
			colorParts = append(colorParts, colorK+": "+colorVV)
		}
	}
	if canInline {
		plainInline := header + "{" + strings.Join(plainParts, ", ") + "}"
		width := cfg.inlineWidth
		if width == 0 {
			width = 72
		}
		if len(plainInline) <= width {
			if noColor {
				return writeStr(w, plainInline)
			}
			colorInline := cfg.color.TypeHeader.apply(header) +
				cfg.color.CloseBrace.apply("{") +
				strings.Join(colorParts, ", ") +
				cfg.color.CloseBrace.apply("}")
			return writeStr(w, colorInline)
		}
	}

	// Indented rendering.
	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.apply(header)+cfg.color.CloseBrace.apply("{")+"\n"); err != nil {
		return err
	}
	for _, ke := range keyed {
		if err := writeStr(w, fieldIndent); err != nil {
			return err
		}
		// Render the display key at depth+1 for correct indentation of any
		// nested struct/map keys; use the precomputed depth-0 canon key only
		// for redact matching.
		if err := writeStr(w, fmtValue(ke.entry.Key, cfg, depth+1)); err != nil {
			return err
		}
		if err := writeStr(w, ": "); err != nil {
			return err
		}
		rendered := fmtValue(ke.entry.Value, cfg, depth+1)
		if cfg.redactKeys != nil && slices.Contains(cfg.redactKeys.Keys, ke.canonKey) {
			rendered = redactWithKeyCfg(rendered, *cfg.redactKeys)
		}
		if err := writeStr(w, rendered); err != nil {
			return err
		}
		if err := writeStr(w, "\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.apply("}"))
}

// fmtSliceTo renders a SliceValue to w inline when short, indented when long.
func fmtSliceTo(w io.Writer, v SliceValue, cfg *formatConfig, depth int) error {
	header := "[]" + v.ElemType
	if len(v.Elems) == 0 {
		return writeStr(w, cfg.color.TypeHeader.apply(header)+cfg.color.CloseBrace.apply("{}"))
	}

	pcfg := plainConfig(cfg)
	noColor := pcfg == cfg
	plainParts := make([]string, 0, len(v.Elems))
	var colorParts []string
	if !noColor {
		colorParts = make([]string, 0, len(v.Elems))
	}
	canInline := true
	for _, e := range v.Elems {
		plain := fmtValue(e, pcfg, 0)
		if strings.ContainsRune(plain, '\n') {
			canInline = false
			break
		}
		plainParts = append(plainParts, plain)
		if !noColor {
			colorParts = append(colorParts, fmtValue(e, cfg, 0))
		}
	}
	if canInline {
		plainInline := header + "{" + strings.Join(plainParts, ", ") + "}"
		width := cfg.inlineWidth
		if width == 0 {
			width = 72
		}
		if len(plainInline) <= width {
			if noColor {
				return writeStr(w, plainInline)
			}
			colorInline := cfg.color.TypeHeader.apply(header) +
				cfg.color.CloseBrace.apply("{") +
				strings.Join(colorParts, ", ") +
				cfg.color.CloseBrace.apply("}")
			return writeStr(w, colorInline)
		}
	}

	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.apply(header)+cfg.color.CloseBrace.apply("{")+"\n"); err != nil {
		return err
	}
	for _, e := range v.Elems {
		if err := writeStr(w, fieldIndent); err != nil {
			return err
		}
		if err := writeStr(w, fmtValue(e, cfg, depth+1)); err != nil {
			return err
		}
		if err := writeStr(w, "\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.apply("}"))
}

// fmtArrayTo renders an ArrayValue to w inline when short, indented when long.
func fmtArrayTo(w io.Writer, v ArrayValue, cfg *formatConfig, depth int) error {
	header := fmt.Sprintf("[%d]%s", v.Len, v.ElemType)
	if len(v.Elems) == 0 {
		return writeStr(w, cfg.color.TypeHeader.apply(header)+cfg.color.CloseBrace.apply("{}"))
	}

	pcfg := plainConfig(cfg)
	noColor := pcfg == cfg
	plainParts := make([]string, 0, len(v.Elems))
	var colorParts []string
	if !noColor {
		colorParts = make([]string, 0, len(v.Elems))
	}
	canInline := true
	for _, e := range v.Elems {
		plain := fmtValue(e, pcfg, 0)
		if strings.ContainsRune(plain, '\n') {
			canInline = false
			break
		}
		plainParts = append(plainParts, plain)
		if !noColor {
			colorParts = append(colorParts, fmtValue(e, cfg, 0))
		}
	}
	if canInline {
		plainInline := header + "{" + strings.Join(plainParts, ", ") + "}"
		width := cfg.inlineWidth
		if width == 0 {
			width = 72
		}
		if len(plainInline) <= width {
			if noColor {
				return writeStr(w, plainInline)
			}
			colorInline := cfg.color.TypeHeader.apply(header) +
				cfg.color.CloseBrace.apply("{") +
				strings.Join(colorParts, ", ") +
				cfg.color.CloseBrace.apply("}")
			return writeStr(w, colorInline)
		}
	}

	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.apply(header)+cfg.color.CloseBrace.apply("{")+"\n"); err != nil {
		return err
	}
	for _, e := range v.Elems {
		if err := writeStr(w, fieldIndent); err != nil {
			return err
		}
		if err := writeStr(w, fmtValue(e, cfg, depth+1)); err != nil {
			return err
		}
		if err := writeStr(w, "\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.apply("}"))
}
