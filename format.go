package gobspect

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormatOption configures the behavior of [Format].
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

// ParseBytesFormat converts a string to a [BytesFormat] constant.
// Accepts "hex", "base64", or "literal" (case-insensitive). An empty string
// maps to [BytesHex]. Returns (BytesHex, false) for any unrecognised value.
func ParseBytesFormat(s string) (BytesFormat, bool) {
	switch strings.ToLower(s) {
	case "", "hex":
		return BytesHex, true
	case "base64":
		return BytesBase64, true
	case "literal":
		return BytesLiteral, true
	default:
		return BytesHex, false
	}
}

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
// Multibyte fill characters (e.g. '█') produce the requested number of code
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

// Apply wraps s with the Style's Prefix and Suffix. A zero-valued Style
// returns s unchanged — it's the identity function, not an empty wrap — so
// consumers can thread a ColorScheme through a rendering pipeline without
// branching on whether color is on.
func (st Style) Apply(s string) string {
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
	hideIfaceTypeName   bool
}

// withoutIfaceTypeName suppresses the "(TypeName) " prefix on interface
// values. Internal only: [Comparer] uses it so that ignoring
// InterfaceValue.TypeName also covers interfaces reached through the
// composite comparison path, which compares rendered output.
func withoutIfaceTypeName() FormatOption {
	return func(c *formatConfig) { c.hideIfaceTypeName = true }
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
			return writeStr(w, cfg.color.Bool.Apply("true"))
		}
		return writeStr(w, cfg.color.Bool.Apply("false"))
	case IntValue:
		return writeStr(w, cfg.color.Number.Apply(fmt.Sprintf("%d", v.V)))
	case UintValue:
		return writeStr(w, cfg.color.Number.Apply(fmt.Sprintf("%d", v.V)))
	case FloatValue:
		s := strconv.FormatFloat(v.V, 'g', -1, 64)
		// Ensure integer-valued floats are distinguishable from IntValue:
		// if the output contains no '.', 'e', or 'E' it looks like a bare
		// integer, so append ".0". NaN and Inf are never bare integers.
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return writeStr(w, cfg.color.Number.Apply(s))
	case ComplexValue:
		// %g already carries a sign for -x and ±Inf; only prefix '+' when the
		// rendered imaginary part has none, so +Inf doesn't become "++Inf".
		im := fmt.Sprintf("%g", v.Imag)
		if im[0] != '+' && im[0] != '-' {
			im = "+" + im
		}
		s := fmt.Sprintf("(%g%si)", v.Real, im)
		return writeStr(w, cfg.color.Number.Apply(s))
	case StringValue:
		return writeStr(w, cfg.color.String.Apply(fmt.Sprintf("%q", v.V)))
	case BytesValue:
		return writeStr(w, cfg.color.Bytes.Apply(fmtBytes(v.V, cfg)))
	case NilValue:
		return writeStr(w, cfg.color.Nil.Apply("nil"))
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
// printable UTF-8 slices are rendered as a Go-quoted string. Otherwise, the
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
// anything else via fmt.Sprint. Otherwise, the raw bytes are rendered using
// the configured byte format, prefixed with "(TypeName) " when TypeName is non-empty.
func fmtOpaque(v OpaqueValue, cfg *formatConfig) string {
	if v.Decoded != nil && !cfg.rawOpaques {
		if s, ok := v.Decoded.(string); ok {
			if s == "" {
				// An empty decoded string (e.g. a TextMarshaler that emitted no
				// bytes) would otherwise render as nothing at all.
				return cfg.color.OpaqueValue.Apply(`""`)
			}
			return cfg.color.OpaqueValue.Apply(s)
		}
		return cfg.color.OpaqueValue.Apply(fmt.Sprint(v.Decoded))
	}
	rawStr := cfg.color.Bytes.Apply(fmtBytesWithFormat(v.Raw, cfg))
	if v.TypeName != "" {
		return cfg.color.OpaquePrefix.Apply("("+v.TypeName+")") + " " + rawStr
	}
	return rawStr
}

// fmtInterfaceTo renders an InterfaceValue to w. A nil interface renders as
// "nil". A non-nil interface renders the concrete value, prefixed with
// "(TypeName) " when TypeName is non-empty.
func fmtInterfaceTo(w io.Writer, v InterfaceValue, cfg *formatConfig, depth int) error {
	if _, ok := v.Value.(NilValue); ok {
		return writeStr(w, cfg.color.Nil.Apply("nil"))
	}
	if v.TypeName != "" && !cfg.hideIfaceTypeName {
		if err := writeStr(w, cfg.color.OpaquePrefix.Apply("("+v.TypeName+")")+" "); err != nil {
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
		return writeStr(w, cfg.color.TypeHeader.Apply(name)+cfg.color.CloseBrace.Apply("{}"))
	}
	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.Apply(name)+cfg.color.CloseBrace.Apply("{")+"\n"); err != nil {
		return err
	}
	for _, f := range v.Fields {
		if err := writeStr(w, fieldIndent); err != nil {
			return err
		}
		if err := writeStr(w, cfg.color.FieldName.Apply(f.Name)+": "); err != nil {
			return err
		}
		rendered := fmtValue(f.Value, cfg, depth+1)
		if cfg.redactKeys != nil && slices.Contains(cfg.redactKeys.Keys, f.Name) {
			// Measure the plain rendering: ANSI escape sequences in the colored
			// form would inflate the redaction width.
			rendered = redactWithKeyCfg(fmtPlainValue(f.Value, cfg, depth+1), *cfg.redactKeys)
		}
		if err := writeStr(w, rendered); err != nil {
			return err
		}
		if err := writeStr(w, "\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.Apply("}"))
}

// fmtMapTo renders a MapValue to w. Entries are sorted by their formatted key
// for deterministic output. Short maps render inline; long maps render indented.
//
// Each entry's display key and value are rendered exactly once and the strings
// reused for both the inline-fit probe and the final output. Re-rendering per
// decision would make deeply nested values exponentially expensive to format.
// Single-line renders contain no depth-dependent indentation (depth only
// affects text following a newline), so strings rendered at depth+1 are valid
// in the inline form too.
func fmtMapTo(w io.Writer, v MapValue, cfg *formatConfig, depth int) error {
	header := "map[" + v.KeyType + "]" + v.ElemType
	if len(v.Entries) == 0 {
		return writeStr(w, cfg.color.TypeHeader.Apply(header)+cfg.color.CloseBrace.Apply("{}"))
	}

	pcfg := plainConfig(cfg)
	noColor := pcfg == cfg // true when cfg already has no color scheme
	type keyedEntry struct {
		canonKey  string // plain depth-0 key: sort order and redact matching
		dispKey   string // rendered key as displayed
		dispVal   string // rendered value as displayed (post-redaction)
		plainLen  int    // plain-text length of "key: value", inline candidates only
		multiline bool
	}
	keyed := make([]keyedEntry, len(v.Entries))
	for i, e := range v.Entries {
		ke := keyedEntry{dispKey: fmtValue(e.Key, cfg, depth+1)}
		keyMultiline := strings.ContainsRune(ke.dispKey, '\n')
		if noColor && !keyMultiline {
			ke.canonKey = ke.dispKey
		} else {
			ke.canonKey = fmtPlainValue(e.Key, cfg, 0)
		}

		redacted := cfg.redactKeys != nil && slices.Contains(cfg.redactKeys.Keys, ke.canonKey)
		ke.dispVal = fmtValue(e.Value, cfg, depth+1)
		plainVal := ke.dispVal
		if !noColor {
			plainVal = fmtValue(e.Value, pcfg, depth+1)
		}
		if redacted {
			// Measure the plain rendering: ANSI escape sequences would
			// inflate the redaction width.
			ke.dispVal = redactWithKeyCfg(plainVal, *cfg.redactKeys)
			plainVal = ke.dispVal
		}
		ke.multiline = keyMultiline || strings.ContainsRune(plainVal, '\n')
		if !ke.multiline {
			ke.plainLen = len(ke.canonKey) + len(": ") + len(plainVal)
		}
		keyed[i] = ke
	}
	if cfg.mapOrder != MapOrderInsertion {
		slices.SortFunc(keyed, func(a, b keyedEntry) int {
			return strings.Compare(a.canonKey, b.canonKey)
		})
	}

	// Inline when every entry is single-line and the plain-text total fits.
	canInline := true
	plainLen := len(header) + len("{}") + 2*(len(keyed)-1) // braces + ", " separators
	for _, ke := range keyed {
		if ke.multiline {
			canInline = false
			break
		}
		plainLen += ke.plainLen
	}
	if canInline {
		width := cfg.inlineWidth
		if width == 0 {
			width = 72
		}
		if plainLen <= width {
			parts := make([]string, len(keyed))
			for i, ke := range keyed {
				parts[i] = ke.dispKey + ": " + ke.dispVal
			}
			return writeStr(w, cfg.color.TypeHeader.Apply(header)+
				cfg.color.CloseBrace.Apply("{")+
				strings.Join(parts, ", ")+
				cfg.color.CloseBrace.Apply("}"))
		}
	}

	// Indented rendering.
	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.Apply(header)+cfg.color.CloseBrace.Apply("{")+"\n"); err != nil {
		return err
	}
	for _, ke := range keyed {
		if err := writeStr(w, fieldIndent+ke.dispKey+": "+ke.dispVal+"\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.Apply("}"))
}

// fmtSliceTo renders a SliceValue to w inline when short, indented when long.
func fmtSliceTo(w io.Writer, v SliceValue, cfg *formatConfig, depth int) error {
	return fmtListTo(w, "[]"+v.ElemType, v.Elems, cfg, depth)
}

// fmtArrayTo renders an ArrayValue to w inline when short, indented when long.
func fmtArrayTo(w io.Writer, v ArrayValue, cfg *formatConfig, depth int) error {
	return fmtListTo(w, fmt.Sprintf("[%d]%s", v.Len, v.ElemType), v.Elems, cfg, depth)
}

// fmtListTo renders slice/array elements inline when every element is
// single-line and the plain-text total fits the inline width, indented
// otherwise.
//
// Each element is rendered exactly once and the string reused for both the
// inline-fit probe and the final output. Re-rendering per decision would make
// deeply nested values exponentially expensive to format. Single-line renders
// contain no depth-dependent indentation (depth only affects text following a
// newline), so strings rendered at depth+1 are valid in the inline form too.
func fmtListTo(w io.Writer, header string, elems []Value, cfg *formatConfig, depth int) error {
	if len(elems) == 0 {
		return writeStr(w, cfg.color.TypeHeader.Apply(header)+cfg.color.CloseBrace.Apply("{}"))
	}

	pcfg := plainConfig(cfg)
	noColor := pcfg == cfg
	parts := make([]string, len(elems))
	canInline := true
	plainLen := len(header) + len("{}") + 2*(len(elems)-1) // braces + ", " separators
	for i, e := range elems {
		parts[i] = fmtValue(e, cfg, depth+1)
		if canInline {
			// ANSI escapes contain no newline, so the display string is
			// multi-line exactly when the plain one is.
			if strings.ContainsRune(parts[i], '\n') {
				canInline = false
			} else if noColor {
				plainLen += len(parts[i])
			} else {
				plainLen += len(fmtValue(e, pcfg, depth+1))
			}
		}
	}
	if canInline {
		width := cfg.inlineWidth
		if width == 0 {
			width = 72
		}
		if plainLen <= width {
			return writeStr(w, cfg.color.TypeHeader.Apply(header)+
				cfg.color.CloseBrace.Apply("{")+
				strings.Join(parts, ", ")+
				cfg.color.CloseBrace.Apply("}"))
		}
	}

	prefix := strings.Repeat(cfg.indent, depth)
	fieldIndent := strings.Repeat(cfg.indent, depth+1)
	if err := writeStr(w, cfg.color.TypeHeader.Apply(header)+cfg.color.CloseBrace.Apply("{")+"\n"); err != nil {
		return err
	}
	for _, part := range parts {
		if err := writeStr(w, fieldIndent+part+"\n"); err != nil {
			return err
		}
	}
	return writeStr(w, prefix+cfg.color.CloseBrace.Apply("}"))
}
