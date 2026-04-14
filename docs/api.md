# API Design

## Package Layout

```
gobspect/
├── doc.go             # Package-level godoc comment
├── types.go           # Value AST node types
├── decode.go          # Stream reader, message framing, type/value dispatch
├── valuedecode.go     # Value decoding: primitives, structs, maps, slices, arrays, opaques, interfaces
├── wire.go            # Wire format primitives
├── registry.go        # OpaqueDecoder registry
├── builtins.go        # Built-in opaque decoders
├── format.go          # Human-readable rendering of Value trees
├── decode_test.go     # Decode tests
├── builtins_test.go   # Built-in decoder tests
├── format_test.go     # Formatter golden tests
└── fuzz_test.go       # Fuzz tests
```

## Core Types

### Inspector

The top-level entry point. Holds the opaque decoder registry and decoding options.

```go
type Inspector struct {
    decoders map[string]OpaqueDecoder
    options  Options
}

type Options struct {
    // MaxDepth limits recursion depth for nested types. Zero means no limit.
    MaxDepth int
    // MaxBytes limits total bytes read from the stream. Zero means no limit.
    MaxBytes int64
}

func New(opts ...Option) *Inspector
func (ins *Inspector) RegisterDecoder(typeName string, dec OpaqueDecoder)
func (ins *Inspector) Decode(r io.Reader) ([]Value, error)

// Inspector-level options:
func WithOptions(o Options) Option
func WithTimeFormat(layout string) Option // re-registers time.Time decoder with custom layout
```

`New()` returns an inspector with all built-in opaque decoders pre-registered. Users call `RegisterDecoder` to add or override decoders for application-specific types.

### OpaqueDecoder

```go
// OpaqueDecoder decodes the raw bytes of a GobEncoder, BinaryMarshaler, or
// TextMarshaler blob into a human-meaningful value.
//
// The returned value should be a simple Go type (string, int, float, map, etc.)
// suitable for display. It does not need to reconstruct the original Go type.
type OpaqueDecoder func(data []byte) (any, error)
```

### Value AST

All node types implement the `Value` interface. The interface is sealed (unexported method).

```go
type Value interface {
    gobValue() // sealed
}

type StructValue struct {
    TypeName string  // from CommonType.Name, may be empty
    TypeID   int     // wire type ID
    Fields   []Field
}

type Field struct {
    Name  string
    Value Value
}

type MapValue struct {
    TypeName string
    TypeID   int
    KeyType  string // descriptive label for the key type
    ElemType string // descriptive label for the element type
    Entries  []MapEntry
}

type MapEntry struct {
    Key   Value
    Value Value
}

type SliceValue struct {
    TypeName string
    TypeID   int
    ElemType string
    Elems    []Value
}

type ArrayValue struct {
    TypeName string
    TypeID   int
    ElemType string
    Len      int
    Elems    []Value
}

type IntValue    struct{ V int64 }
type UintValue   struct{ V uint64 }
type FloatValue  struct{ V float64 }
type ComplexValue struct{ Real, Imag float64 }
type BoolValue   struct{ V bool }
type StringValue struct{ V string }
type BytesValue  struct{ V []byte }
type NilValue    struct{}

type InterfaceValue struct {
    TypeName string // concrete type name from the stream
    Value    Value  // the concrete value, or NilValue
}

type OpaqueValue struct {
    TypeName string // from CommonType.Name
    Encoding string // "gob", "binary", or "text"
    Raw      []byte // the undecoded blob
    Decoded  any    // best-effort decoded form, nil if no decoder matched
}
```

## Formatting

```go
func Format(v Value, opts ...FormatOption) string

type FormatOption func(*formatConfig)

func WithIndent(indent string) FormatOption        // default: "  "
func WithMaxBytes(n int) FormatOption              // max bytes shown for opaque/bytes, default: 64; applies to all byte formats
func WithRawOpaques(bool) FormatOption             // show raw bytes even when Decoded is present
func WithTimeFormat(layout string) Option          // Inspector-level option; re-registers time.Time decoder with custom layout; default: time.RFC3339Nano
func WithBytesFormat(f BytesFormat) FormatOption  // BytesHex (default), BytesBase64, BytesLiteral
func WithRedactKeys(cfg RedactConfig) FormatOption          // redact values at render time by field/key name
func WithRedactTypes(cfg RedactTypesConfig) FormatOption    // redact values by type name
```

`Format` produces a multi-line, indented, human-readable string. It is the default rendering path for CLI tools and debugging output.

### BytesFormat

```go
type BytesFormat int

const (
    BytesHex     BytesFormat = iota // lowercase hex string (default)
    BytesBase64                     // standard base64 (RFC 4648, with padding)
    BytesLiteral                    // Go-style: []byte{0xde, 0xad, ...}
)
```

When `BytesFormat` is set explicitly via `WithBytesFormat`, the printable-UTF-8 shortcut (render `[]byte` as a quoted string) is suppressed. `WithMaxBytes` applies to all formats: the byte slice is truncated before encoding, then `…` is appended when truncated.

### RedactConfig / RedactTypesConfig

```go
// RedactConfig controls value redaction by field or map key name.
type RedactConfig struct {
    Keys       []string // exact field/key names that trigger redaction
    Char       rune     // fill character; defaults to '*'
    TextLength int      // number of Char runes to emit; 0 = preserve original length
}

// RedactTypesConfig controls value redaction by type name.
type RedactTypesConfig struct {
    Types      []string // type names that trigger redaction
    Char       rune     // fill character; defaults to '*'
    TextLength int      // number of Char runes to emit; 0 = preserve original length
}
```

`WithRedactKeys(cfg RedactConfig)` applies redaction to matching struct fields and map entries. `WithRedactTypes(cfg RedactTypesConfig)` applies redaction by TypeName. Both may be combined.

## JSON Serialization

```go
func ToJSON(v Value) ([]byte, error)
func (sr *StreamResult) MarshalJSON() ([]byte, error)
```

`ToJSON` serializes a `Value` as a discriminated-union JSON object with a `"kind"` field. `StreamResult` implements `json.Marshaler` and produces `{"types": [...], "values": [...], "error": null|string}`.

### Kind field mapping

| Kind | Extra JSON fields |
|------|-------------------|
| `struct` | `typeName`, `typeId`, `fields: [{name, value}]` |
| `map` | `typeName`, `typeId`, `keyType`, `elemType`, `entries: [{key, value}]` |
| `slice` | `typeName`, `typeId`, `elemType`, `elems: [value]` |
| `array` | `typeName`, `typeId`, `elemType`, `len`, `elems: [value]` |
| `int` | `v` (number) |
| `uint` | `v` (number) |
| `float` | `v` (number) |
| `complex` | `real`, `imag` (numbers) |
| `bool` | `v` (bool) |
| `string` | `v` (string) |
| `bytes` | `v` (base64 string), `encoding: "base64"` |
| `nil` | _(no extra fields)_ |
| `interface` | `typeName`, `value` |
| `opaque` | `typeName`, `encoding`, `raw` (base64), `decoded` (JSON-safe or string fallback) |

## Type Metadata

For consumers that want to inspect the type graph independently of values:

```go
type TypeInfo struct {
    ID       int
    Name     string
    Kind     TypeKind // Struct, Map, Slice, Array, GobEncoder, BinaryMarshaler, TextMarshaler
    Fields   []FieldInfo   // for structs
    Key      *TypeRef      // for maps
    Elem     *TypeRef      // for maps, slices, arrays
    Len      int           // for arrays
}

type FieldInfo struct {
    Name   string
    TypeID int
}

type TypeRef struct {
    ID   int
    Name string // resolved name if available
}

type TypeKind int

const (
    KindStruct TypeKind = iota
    KindMap
    KindSlice
    KindArray
    KindGobEncoder
    KindBinaryMarshaler
    KindTextMarshaler
)

func (ins *Inspector) DecodeTypes(r io.Reader) ([]TypeInfo, error)
```

`DecodeTypes` reads the stream and returns only the type definitions without decoding values. Useful for schema inspection.

## Stream Metadata

```go
type StreamResult struct {
    Types  []TypeInfo // all type definitions encountered
    Values []Value    // all top-level values decoded
    Err    error      // first error encountered, nil if clean
}

func (ins *Inspector) DecodeStream(r io.Reader) *StreamResult
```

`DecodeStream` is the comprehensive variant that returns everything. `Decode` is a convenience wrapper that returns only values.
