---
title: API Guide
---

gobspect decodes arbitrary `encoding/gob` streams without the original Go types. This page is a guided tour of the library's API: creating an inspector, iterating a stream, working with the Value AST, and rendering results as text, JSON, schemas, or statistics. For the exhaustive generated reference, see [pkg.go.dev](https://pkg.go.dev/github.com/codepuke/gobspect).

```go
import "github.com/codepuke/gobspect"
```

## Inspector and options

`Inspector` is the entry point. Create one with `New`, which pre-registers built-in decoders for common opaque types (`time.Time`, `math/big.Int`, `math/big.Float`, `math/big.Rat`, UUIDs, `shopspring/decimal.Decimal`):

```go
func New(opts ...Option) *Inspector
```

Options configure decoding behavior:

```go
func WithReadLimit(n int64) Option        // cap total bytes read; 0 = no limit
func WithTimeFormat(layout string) Option // layout for time.Time values; default time.RFC3339Nano
func WithSkipCorruptValues(b bool) Option // skip corrupt value messages instead of aborting
```

`WithReadLimit` is the primary safety knob when inspecting untrusted input:

```go
ins := gobspect.New(gobspect.WithReadLimit(10 << 20)) // 10 MiB
```

`WithSkipCorruptValues` makes the inspector continue past individual value messages that fail to decode — useful for archived logs with occasional bad records. Each skipped message is counted and available via `Stream.SkipCount`. Errors in type-definition messages remain fatal, because they would leave the type registry inconsistent and every subsequent value undecodable.

### Registering opaque decoders

Types that implement `GobEncoder`, `BinaryMarshaler`, or `TextMarshaler` appear in the output as opaque byte blobs. A `DecoderFunc` turns those bytes into something readable:

```go
type DecoderFunc func([]byte) (any, error)

func (ins *Inspector) RegisterDecoder(typeName string, dec DecoderFunc)
func (ins *Inspector) RegisterUnnamedDecoder(dec DecoderFunc)
```

`RegisterDecoder` adds or overrides the decoder for a type name (the same key can override a built-in). The returned value should be a simple Go type — string, int, float, map — suitable for display; it does not need to reconstruct the original Go type.

One caveat: when a `GobEncoder` type is encoded directly rather than through an interface, the gob wire format records an empty type name, so name-keyed decoders never match it. `RegisterUnnamedDecoder` handles this case: decoders registered with it are tried in registration order against every unnamed opaque value, and the first one that returns without error wins.

`TextMarshaler` blobs are always UTF-8 strings, so they are decoded automatically with no registration needed.

## Streaming and iteration

`Inspector.Stream` begins decoding a gob stream:

```go
func (ins *Inspector) Stream(r io.Reader) *Stream
```

Decoding is lazy — nothing is read from `r` until you advance the iterator returned by `Values`, or call a helper like `Collect`, `Schema`, or `Stats` that drains the stream.

```go
func (s *Stream) Values() iter.Seq2[Value, error]
func (s *Stream) Collect() ([]Value, error)
```

`Values` yields one decoded `Value` per top-level gob value. On error it yields `(nil, err)` and stops. Breaking out of the loop early is safe; the iterator will not read past the break point.

```go
ins := gobspect.New()
for v, err := range ins.Stream(r).Values() {
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(gobspect.Format(v))
}
```

`Collect` drains the remainder of the stream and returns all values at once. If iteration was partially consumed, it returns only the values not yet yielded; on error it returns the values collected so far alongside the error.

A `Stream` is **single-use**: calling `Values` (or `Messages`) a second time, or after a draining helper, panics. It is not safe for concurrent use, and it does not own its reader — close the underlying `io.Reader` yourself if needed.

### Type metadata during iteration

Type definitions are accumulated as the stream is consumed:

```go
func (s *Stream) Types() []TypeInfo
func (s *Stream) TypeByID(id int) (TypeInfo, bool)
```

`Types` returns the live slice of `TypeInfo` in wire order; it grows as decoding proceeds, and the returned slice must not be mutated (copy with `slices.Clone` for a snapshot). By the time a value is yielded, every type its type graph references is already present and cross-references (`TypeRef.Name`) are resolved. `TypeByID` gives O(1) lookup by stream-scoped type ID — pair it with a value's `TypeID()` method to find the definition for the value you just received.

:::examples stream-types

### Wire-level framing

`Messages` iterates the raw length-prefixed messages without decoding value bodies:

```go
func (s *Stream) Messages() iter.Seq2[MessageInfo, error]

type MessageInfo struct {
    Index   int    // 0-based message counter
    Offset  int64  // byte offset of the length prefix
    BodyLen int    // length of the message body
    TypeID  int    // signed type ID: negative = type def, positive = value
    Body    []byte // raw body bytes
}

func (m MessageInfo) IsTypeDef() bool
```

This is a cheap way to profile per-message sizes or locate message boundaries. `Messages` consumes the stream just like `Values` does, and unlike `Values` it does not register type definitions — if you need both decoded values and framing, run two streams over the data.

### Skip counting

```go
func (s *Stream) SkipCount() int
```

Reports how many corrupt value messages have been skipped so far under `WithSkipCorruptValues`. Always zero in strict mode.

## The Value AST

Every decoded value is a `Value`, a sealed interface implemented by exactly 14 concrete node types. Dispatch with a type switch:

```go
type Value interface {
    // TypeID returns the stream-scoped type ID for composite and opaque
    // values; pure scalars return 0.
    TypeID() int
    // unexported sealing method
}
```

The node types:

| Node | Represents | Notable fields |
|------|------------|----------------|
| `StructValue` | gob struct | `TypeName`, `GobTypeID`, `Fields []Field` |
| `MapValue` | gob map | `TypeName`, `GobTypeID`, `KeyType`, `ElemType`, `Entries []MapEntry` |
| `SliceValue` | gob slice | `TypeName`, `GobTypeID`, `ElemType`, `Elems []Value` |
| `ArrayValue` | gob array | `TypeName`, `GobTypeID`, `ElemType`, `Len`, `Elems []Value` |
| `IntValue` | signed integer | `V int64` |
| `UintValue` | unsigned integer | `V uint64` |
| `FloatValue` | float | `V float64` |
| `ComplexValue` | complex number | `Real, Imag float64` |
| `BoolValue` | boolean | `V bool` |
| `StringValue` | string | `V string` |
| `BytesValue` | `[]byte` | `V []byte` |
| `NilValue` | nil pointer or nil interface | — |
| `InterfaceValue` | interface value | `TypeName` (concrete type name), `Value` (inner value, or `NilValue`) |
| `OpaqueValue` | GobEncoder / BinaryMarshaler / TextMarshaler blob | `TypeName`, `GobTypeID`, `Encoding` (`"gob"`, `"binary"`, or `"text"`), `Raw []byte`, `Decoded any` |

Composite nodes and `OpaqueValue` carry a `GobTypeID` field — the stream-scoped type ID, also returned by their `TypeID()` method — which you can pass to `Stream.TypeByID`. Scalar nodes return 0 from `TypeID()`.

`OpaqueValue.Decoded` holds the best-effort decoded form produced by a registered decoder (a formatted string or primitive Go value), or `nil` if no decoder matched. The raw bytes are always preserved in `Raw`.

Two helpers work on any `Value`:

```go
func ValueKind(v Value) string // "struct", "map", "int", "opaque", ...
func Unwrap(v Value) Value     // strips every InterfaceValue layer
```

`Unwrap` handles nested interfaces (gob streams do produce them) and normalises a nil interface to `NilValue`, so it never returns an `InterfaceValue`.

## Formatting

`Format` renders any `Value` as a human-readable string; `FormatTo` writes the same output to an `io.Writer` and propagates write errors:

```go
func Format(v Value, opts ...FormatOption) string
func FormatTo(w io.Writer, v Value, opts ...FormatOption) error
```

Structs are always rendered as indented field trees. Maps, slices, and arrays are inlined when the formatted form fits within the inline width (default 72 characters) and indented otherwise. Map entries are sorted by formatted key for deterministic output.

The full option set:

```go
func WithIndent(indent string) FormatOption       // indentation string; default two spaces
func WithInlineWidth(n int) FormatOption          // inline threshold for maps/slices/arrays; default 72
func WithMaxBytes(n int) FormatOption             // max raw bytes shown for bytes/opaques; default 64, 0 = no limit
func WithRawOpaques(raw bool) FormatOption        // show raw bytes even when Decoded is set
func WithBytesFormat(f BytesFormat) FormatOption  // BytesHex (default), BytesBase64, BytesLiteral
func WithMapOrder(order MapOrder) FormatOption    // MapOrderSorted (default) or MapOrderInsertion
func WithColor(scheme ColorScheme) FormatOption   // syntax highlighting via a ColorScheme
func WithRedactKeys(cfg RedactConfig) FormatOption
func WithRedactTypes(cfg RedactTypesConfig) FormatOption
```

:::examples format-options

### Byte rendering

`BytesFormat` selects hex (default), standard padded base64, or Go-literal style (`[]byte{0xde, ...}`). When set explicitly via `WithBytesFormat`, the printable-UTF-8 shortcut (rendering byte slices as quoted strings) is suppressed. `WithMaxBytes` truncates the slice before encoding and appends `…` when truncation occurs.

The same machinery is exported standalone:

```go
func FormatBytes(b []byte, format BytesFormat, maxBytes int) string
func ParseBytesFormat(s string) (BytesFormat, bool)
```

`ParseBytesFormat` accepts `"hex"`, `"base64"`, or `"literal"` (case-insensitive); an empty string maps to `BytesHex`, and any unrecognised value returns `(BytesHex, false)`.

### Color

`ColorScheme` assigns a `Style` (a prefix/suffix pair) to each token role — field names, type headers, strings, numbers, and so on. `ANSIColorScheme` is a pre-built scheme for terminal output; `NoColorScheme` (the zero value) is the identity and produces byte-identical plain output. `WithMapOrder(MapOrderInsertion)` skips key sorting and renders map entries in wire order.

### Redaction

Redaction replaces values at render time — the AST is never modified:

```go
type RedactConfig struct {
    Keys       []string // exact field/key names that trigger redaction
    Char       rune     // fill character; defaults to '*'
    TextLength int      // fill chars to emit; 0 = adaptive (see below)
}
```

`WithRedactKeys` matches struct field names and formatted map keys exactly (case-sensitive; no globs or regex). `WithRedactTypes` takes a `RedactTypesConfig` with a `Types` list checked against `StructValue`, `InterfaceValue`, and `OpaqueValue` type names. The two may be combined.

When `TextLength` is 0, single-line values are replaced by fill characters matching their rendered rune length, while multi-line values (such as nested structs) are replaced by exactly three fill characters (`***`). Set `TextLength > 0` to always emit that exact count.

:::examples redact-output

## JSON output

`ToJSON` serializes a `Value` as a discriminated-union JSON object; every node carries a `"kind"` field:

```go
func ToJSON(v Value, opts ...JSONOption) ([]byte, error)
func ToJSONIndent(v Value, prefix, indent string, opts ...JSONOption) ([]byte, error)

func WithNonFiniteAsNull(b bool) JSONOption
```

:::examples to-json

Field mapping per kind:

| Kind | Extra JSON fields |
|------|-------------------|
| `struct` | `typeName`, `typeId`, `fields: [{name, value}]` |
| `map` | `typeName`, `typeId`, `keyType`, `elemType`, `entries: [{key, value}]` |
| `slice` | `typeName`, `typeId`, `elemType`, `elems: [value]` |
| `array` | `typeName`, `typeId`, `elemType`, `len`, `elems: [value]` |
| `int` | `v` (number) |
| `uint` | `v` (number) |
| `float` | `v` (number; see non-finite note) |
| `complex` | `real`, `imag` (numbers; see non-finite note) |
| `bool` | `v` (bool) |
| `string` | `v` (string), or `v` (base64) + `encoding: "base64"` for invalid UTF-8 |
| `bytes` | `v` (base64 string), `encoding: "base64"` |
| `nil` | _(no extra fields)_ |
| `interface` | `typeName`, `value` |
| `opaque` | `typeName`, `typeId`, `encoding`, `raw` (base64), `decoded` |

Two edge cases keep the output lossless and valid:

- **Non-finite floats.** JSON has no number representation for `NaN` or infinities, so by default they are emitted as the strings `"NaN"`, `"+Inf"`, and `"-Inf"` — including the real and imaginary parts of complex values. Pass `WithNonFiniteAsNull(true)` to emit JSON `null` instead, for consumers that expect `v` to never hold a string.
- **Non-UTF-8 strings.** Gob strings are arbitrary byte sequences, and `encoding/json` would silently corrupt invalid UTF-8. Such strings are emitted as base64 with an `"encoding": "base64"` marker, the same treatment `bytes` gets.

## Schema extraction

`Stream.Schema` drains the stream and returns every type definition as a formatted `Schema`:

```go
func (s *Stream) Schema() (*Schema, error)
```

Value messages are decoded and discarded — only type information survives — and a partial schema is returned alongside any error. There is no types-only fast path; the stream must be fully decoded to reach every definition.

:::examples schema-extract

A `Schema` holds `TypeDecl` entries (name, kind, struct `FieldDecl` lists, target expressions for slices/maps/arrays, and annotations for opaque types) and renders several ways:

```go
func (s *Schema) String() string
func (s *Schema) Format(opts ...SchemaFormatOption) string
func (s *Schema) FormatTo(w io.Writer, opts ...SchemaFormatOption) error
func (s *Schema) JSON() ([]byte, error)
func (s *Schema) JSONIndent(prefix, indent string) ([]byte, error)
func (s *Schema) TypeByName(name string) (*TypeDecl, bool)
```

`String` produces a Go-style type declaration block. `SchemaWithColor` and `SchemaWithIndent` are the rendering options; `JSON` emits a machine-readable array of declarations for code generators and compatibility checkers.

If you already hold a `[]TypeInfo` (from `Stream.Types`), `FormatSchema` converts it directly:

```go
func FormatSchema(types []TypeInfo) *Schema
```

Types with mechanically generated names (like `"[]int"`) are excluded from the top level; anonymous types appear only inline within other declarations.

## Stream statistics

`Stream.Stats` drains the stream and returns a population-level summary:

```go
func (s *Stream) Stats() (*Stats, error)
```

`Stats` reports total message and body-byte counts, the split between type-definition and value messages, opaque decoder coverage (`DecodedOpaques` / `UndecodedOpaques`), and the count of messages skipped under `WithSkipCorruptValues`. Its `ByType` slice of `TypeStats` breaks values down by top-level type — value count, total body bytes, and per-field presence rates (`FieldPresence`), which reveal how often gob omitted each zero-valued struct field.

A `Stats` always describes a completely drained stream; on error, `Stats` returns `(nil, err)` rather than a partial pass. Render with `Stats.Format(w)` for a human-readable table, or `Stats.JSON()` / `Stats.JSONIndent` for aggregation across many files.

## Comparing values

```go
func Equal(a, b Value) bool
func CompareValues(a, b Value) int
func CompareValuesFold(a, b Value) int
```

`Equal` is strict structural equality: kinds must match, composite shapes must line up exactly, and primitives compare by native value — `IntValue{5}` is not equal to `FloatValue{5}`. Floats compare structurally rather than by IEEE semantics, so `NaN` equals `NaN`; anything else would report phantom differences when a stream is compared against an identical copy.

`CompareValues` returns -1, 0, or +1 with a total ordering: cross-kind numerics are coerced through `float64`, `NaN` sorts below everything else, complex values order by real then imaginary part, and composites fall back to comparing their `Format` output. `CompareValuesFold` adds case-insensitive string comparison.

For configurable semantics, use a `Comparer`:

```go
type Comparer struct {
    IgnoreInterfaceTypeName bool // compare only the concrete value inside interfaces
    Fold                    bool // case-insensitive string comparison
}

func (c Comparer) Equal(a, b Value) bool
func (c Comparer) Compare(a, b Value) int
```

By default an `InterfaceValue`'s `TypeName` participates in comparison — for a named scalar, the name is the only place the distinction between, say, `Miles(5)` and `Kilos(5)` survives. Set `IgnoreInterfaceTypeName` when comparing streams from different builds of the same program, where a module path change would otherwise make every interface-typed field read as modified. When only one side is interface-wrapped, the wrapper is unwrapped automatically, so a value read through an interface still equals the same value read directly.

## Companion subpackages

Six subpackages build on the core AST.

**`github.com/codepuke/gobspect/query`** — path-based navigation of `Value` trees without type switches: `query.Get(root, "Orders.0.Customer.Name")`, wildcards (`Orders.*.Name`), negative indexes, recursive descent (`..Price`), and filters (`..[Status=active]`). The convenience functions (`Get`, `All`, `Keys`, `AllSeq`) panic on syntactically invalid path expressions, following the `regexp.MustCompile` convention; `Parse` plus the path-typed variants (`GetPath`, `AllPath`, ...) give error-based handling.

:::examples query-get

**`github.com/codepuke/gobspect/diff`** — structural diffing of two `Value` trees or two streams. Produces a `Delta` AST mirroring the input structure, with text, color, and JSON rendering, plus `HasChanges` for quick regression checks. Leaf equality follows `gobspect.Equal`.

**`github.com/codepuke/gobspect/sortval`** — sorting utilities for sequences of `Value` nodes, with multi-key `SortSpec` support and a parser for CLI-style sort flags.

**`github.com/codepuke/gobspect/tabular`** — writes `Value` nodes as CSV or TSV rows, with column order derived from the stream's type definitions, configurable delimiters, and modes for heterogeneous row shapes.

**`github.com/codepuke/gobspect/decompress`** — transparent decompression by sniffing leading magic bytes: gzip, zstd, xz, bzip2, and zip. Anything else passes through unchanged, so wrapping an uncompressed stream is harmless.

**`github.com/codepuke/gobspect/gq`** — the query-run engine behind the `gq` command as a library: pipeline (query matching, indexing, sorting, offset/limit), rendering (pretty, json, jsonl), and numeric aggregation (count, sum, min, max, avg). Built for embedding gq's exact semantics in other frontends.

## Further reference

This page is a curated tour, not an exhaustive listing. For every exported symbol with full godoc — including the ones elided here — see [pkg.go.dev/github.com/codepuke/gobspect](https://pkg.go.dev/github.com/codepuke/gobspect).
