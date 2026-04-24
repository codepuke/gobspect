# gobspect

`gobspect` is a decode-only introspection library for Go's `encoding/gob` wire format. It reads arbitrary gob streams without requiring the original Go types and produces a structured AST and human-readable output. Included in this repo is a query package and CLI inspection tool.

## gq — command-line tool

**[`gq`](cmd/gq/README.md)** is a jq-inspired CLI for inspecting gob streams from the terminal. No Go code required:

```sh
go install github.com/codepuke/gobspect/cmd/gq@latest
gq --schema data.gob          # print the Go-style type schema
gq .Orders.*.ID orders.gob    # navigate to a field across all slice elements
cat data.gob | gq --format json .Header
```

See the [gq README](cmd/gq/README.md) for the full flag reference, query syntax, and examples.

## Library overview

Standard `encoding/gob` decoding requires the original type definitions at decode time. `gobspect` removes this requirement: it parses the wire format directly, reconstructs the type graph from the inline type definitions present in every gob stream, and yields a structured representation of the encoded data.

This is useful for debugging serialized data, building inspection tools, or reading gob streams from code you do not control.

Two output layers are provided:

- **Structural AST** (`Value` and its subtypes): a complete representation of the wire data, preserving type IDs, type names, field names, and raw bytes for opaque blobs. This layer does not lose information.
- **Human-readable formatting** (`Format`): a text rendering of a `Value` tree, with built-in decoders for common opaque types.

## Installation

```
go get github.com/codepuke/gobspect
```

Requires Go 1.26 or later.

## Usage

### Schema inspection

When you encounter an unknown `.gob` file, `Stream.Schema` is usually the first call to make. It drains the stream, reads the type definitions embedded in every gob stream, and renders them as Go-style type declarations:

```go
ins := gobspect.New()
schema, err := ins.Stream(r).Schema() // r is any io.Reader
if err != nil {
    log.Fatal(err)
}
fmt.Println(schema)
```

For a stream that encodes an `Order` struct referencing `LineItem` and opaque types, the output looks like:

```
type LineItem struct {
  Price     Decimal  // GobEncoder
  Quantity  int
  SKU       string
}

type Order struct {
  Customer  string
  ID        uint
  Items     LineItem
  PlacedAt  Time  // GobEncoder
}
```

Opaque types — values that implement `GobEncoder` or `BinaryMarshaler` — appear as inline comments on fields that reference them, and as standalone declarations:

```
type Decimal // GobEncoder
type Time    // GobEncoder
```

The gob wire format records only the short type name and raw encoded bytes for opaque types — no underlying structure and no import path — so no valid Go type declaration can be produced.

`Stream.Schema` drains the stream and returns a `*Schema`. Use `Stream.Types` to access the structured `[]TypeInfo` slice directly, or call `FormatSchema` to convert it:

```go
ins := gobspect.New()
stream := ins.Stream(r)
values, err := stream.Collect()
if err != nil {
    log.Fatal(err)
}
schema := gobspect.FormatSchema(stream.Types())
fmt.Println(schema)
```

`Schema.Format` and `Schema.FormatTo` accept `FormatOption` values (e.g. `WithColor`, `WithIndent`) to control rendering.

### Decode a stream and format the output

```go
ins := gobspect.New()
values, err := ins.Stream(r).Collect() // r is any io.Reader
if err != nil {
    log.Fatal(err)
}
for _, v := range values {
    fmt.Println(gobspect.Format(v))
}
```

`New` returns an `Inspector` with all built-in opaque decoders pre-registered. `Collect` returns one `Value` per top-level `Encode` call in the original stream. A stream may contain multiple values.

### Stream values one at a time with an iterator

```go
ins := gobspect.New()
stream := ins.Stream(r)
for v, err := range stream.Values() {
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(gobspect.Format(v))
}
```

`Values` returns an `iter.Seq2[Value, error]` that yields each decoded value as it is read, without buffering the entire stream first. An early `break` is safe; the iterator stops reading immediately.

> **Note:** A `Stream` is single-use. Calling `Values` on an already-consumed `Stream` panics. Create a new `Stream` with `ins.Stream(r)` for each pass.

**When to prefer `Values` over `Collect`:**
- The stream is large and you want to process or discard each value before reading the next.
- You want to stop partway through (e.g., search for the first matching value).
- You want to integrate with other range-based pipelines.

Use `Collect` when you need all values as a slice.

### Inspect type definitions alongside values

```go
ins := gobspect.New()
stream := ins.Stream(r)
for v, err := range stream.Values() {
    if err != nil {
        log.Fatal(err)
    }
    // stream.Types() grows as the stream is consumed.
    fmt.Printf("decoded value; %d types known so far\n", len(stream.Types()))
    fmt.Println(gobspect.Format(v))
}
// After the loop, stream.Types() contains all type definitions.
for _, ti := range stream.Types() {
    fmt.Printf("type %s kind=%v fields=%d\n", ti.Name, ti.Kind, len(ti.Fields))
}
```

`Stream.Types` returns the live slice of `TypeInfo` for every type definition encountered in stream order. It grows incrementally as the stream is consumed.

### Inspecting raw framing

`Stream.Messages` returns an `iter.Seq2[MessageInfo, error]` that yields one entry per length-prefixed frame in the stream *without* decoding the value body. Each `MessageInfo` carries the byte `Offset`, `BodyLen`, signed `TypeID`, and the `Body` bytes. This is the right tool for size profiling, building a frame index, or comparing wire layouts:

```go
for m, err := range ins.Stream(r).Messages() {
    if err != nil { log.Fatal(err) }
    fmt.Printf("msg %d @%d len=%d typeID=%d typeDef=%v\n",
        m.Index, m.Offset, m.BodyLen, m.TypeID, m.IsTypeDef())
}
```

`Stream.Stats` wraps a full decode pass and returns population-level counts (per-type record totals, body byte consumption, struct field presence rates, opaque decoder coverage). Use it for a quick profile of an unknown file.

### Structural diff

The `gobspect/diff` subpackage compares two `Value` trees or two streams position-by-position, producing a `Delta` AST that can be rendered as text or JSON. The CLI exposes it via `gq -diff PATH` (exit code 1 when there are changes).

### Compressed streams

`Inspector.Stream` accepts any `io.Reader`, so compressed streams work by wrapping the reader before passing it in. For gzip, use `compress/gzip.NewReader`; apply the same pattern for any other compression format.

### Register a custom opaque decoder

Types that implement `GobEncoder` or `BinaryMarshaler` are serialized as opaque byte blobs. `gobspect` ships decoders for common standard library and third-party types (see [Built-in opaque decoders](#built-in-opaque-decoders)). For application-specific types, register a decoder by the type's `CommonType.Name` as it appears in the gob wire format (the short type name, not the full import path):

```go
ins := gobspect.New()
ins.RegisterDecoder("SessionToken", func(data []byte) (any, error) {
    if len(data) < 8 {
        return nil, errors.New("session token too short")
    }
    created := binary.BigEndian.Uint64(data[:8])
    payload := data[8:]
    return map[string]any{
        "created": time.Unix(int64(created), 0).Format(time.RFC3339),
        "payload": hex.EncodeToString(payload),
    }, nil
})
```

The returned value is stored in `OpaqueValue.Decoded` and used by `Format`. Registered decoders override built-in decoders for the same type name.

> **Note:** `CommonType.Name` is only populated in the wire format when a `GobEncoder` type is transmitted through an interface field. When such a type is encoded directly (not via an interface), gob sends an empty name and registry lookup by name cannot match. See [docs/opaque-types.md](docs/opaque-types.md) for details.

### JSON output

Individual values can be serialized with `gobspect.ToJSON(v)` (compact) or `gobspect.ToJSONIndent(v, "", "  ")` (pretty-printed).

```go
type Point struct{ X, Y int }

var buf bytes.Buffer
gob.NewEncoder(&buf).Encode(Point{X: 3, Y: 7})

ins := gobspect.New()
values, err := ins.Stream(&buf).Collect()
if err != nil {
    log.Fatal(err)
}

// Pretty-print a single value.
b, err := gobspect.ToJSONIndent(values[0], "", "  ")
```

The above produces output like:

```json
{
  "fields": [
    {"name": "X", "value": {"kind": "int", "v": 3}},
    {"name": "Y", "value": {"kind": "int", "v": 7}}
  ],
  "kind": "struct",
  "typeId": 64,
  "typeName": "Point"
}
```

> **Note:** `typeId` is session-scoped. The numeric value depends on the order type definitions appear in the stream and will differ between sessions.

Every node carries a `"kind"` discriminator. The full field mapping per kind is documented in [docs/api.md](docs/api.md).

### Format options

These options are passed to `Format(v, ...FormatOption)`:

| Option | Type | Description |
|---|---|---|
| `WithIndent(s)` | `FormatOption` | Indentation string for nested output. Default: `"  "` |
| `WithMaxBytes(n)` | `FormatOption` | Max bytes rendered for `BytesValue` and `OpaqueValue.Raw`. Default: 64. Zero = no limit. Applies to all byte formats. |
| `WithRawOpaques(bool)` | `FormatOption` | Always show raw bytes even when `OpaqueValue.Decoded` is set. |
| `WithBytesFormat(f)` | `FormatOption` | How `BytesValue` and `OpaqueValue.Raw` are rendered: `BytesHex` (default), `BytesBase64`, or `BytesLiteral`. When set explicitly, the printable-UTF-8 shortcut is suppressed. |
| `WithRedactKeys(cfg)` | `FormatOption` | Redact values at render time when the field or map-key name matches. See [Redacting sensitive fields](#redacting-sensitive-fields). |
| `WithRedactTypes(cfg)` | `FormatOption` | Redact values whose type name matches. Supports custom fill character and length. May be combined with `WithRedactKeys`. |

`WithTimeFormat(layout)` is an **`Inspector`-level option** passed to `New()`, not to `Format()`. It re-registers the `time.Time` decoder with a custom Go time layout. Default: `time.RFC3339Nano`.

```go
ins := gobspect.New(gobspect.WithTimeFormat("2006-01-02"))
```

```go
out := gobspect.Format(v,
    gobspect.WithIndent("\t"),      // default: two spaces
    gobspect.WithMaxBytes(128),     // max bytes shown for opaque/bytes, default: 64
    gobspect.WithRawOpaques(true),  // always show raw bytes even when Decoded is set
    gobspect.WithBytesFormat(gobspect.BytesBase64), // base64 instead of hex
)
```

### Redacting sensitive fields

`WithRedactKeys` replaces the rendered value of matching struct fields or map entries with a fill character string. The AST is never modified — redaction happens at render time only.

```go
out := gobspect.Format(v,
    gobspect.WithRedactKeys(gobspect.RedactConfig{
        Keys:       []string{"Password", "Token"},
        Char:       '*',
        TextLength: 0, // 0 = preserve visual length of the original rendered value
    }),
)
```

`WithRedactTypes` redacts all values whose `TypeName` matches, regardless of where they appear. It accepts a `RedactTypesConfig` that controls which types to redact and how the fill characters are rendered:

```go
out := gobspect.Format(v,
    gobspect.WithRedactTypes(gobspect.RedactTypesConfig{
        Types:      []string{"Sensitive", "SecretKey"},
        Char:       '*',
        TextLength: 0, // 0 = preserve visual length of the original rendered value
    }),
)
```

Both options may be combined; a value is redacted if it matches either rule.

Key matching for struct fields is by exact field name. For map entries, matching is by the formatted key string (e.g., `"\"password\""` for a string map key `"password"`). Case-sensitive exact match only.

## Built-in opaque decoders

The following types are decoded automatically when encountered in a stream:

| Type | Encoding | Formatted as |
|---|---|---|
| `time.Time` | `BinaryMarshaler` | RFC 3339 with nanosecond precision |
| `math/big.Int` | `GobEncoder` | Decimal string |
| `math/big.Float` | `GobEncoder` | Decimal string |
| `math/big.Rat` | `GobEncoder` | `numerator/denominator` or decimal |
| `github.com/google/uuid.UUID` | `BinaryMarshaler` | Standard UUID string |
| `github.com/gofrs/uuid.UUID` | `BinaryMarshaler` | Standard UUID string |
| `github.com/shopspring/decimal.Decimal` | `GobEncoder` | Reconstructed decimal string |
| Any `TextMarshaler` (pre-Go 1.26 streams) | `TextMarshaler` | UTF-8 string as-is |

Unknown `GobEncoder` and `BinaryMarshaler` types are stored as `OpaqueValue` with `Decoded = nil` and rendered as `(TypeName) <hex>`.

## Value AST types

All AST node types implement the sealed `Value` interface and can be inspected with a type switch:

```go
switch v := v.(type) {
case gobspect.StructValue:
    for _, f := range v.Fields {
        fmt.Printf("%s = %v\n", f.Name, f.Value)
    }
case gobspect.IntValue:
    fmt.Println(v.V)
case gobspect.OpaqueValue:
    fmt.Printf("opaque %s: %v\n", v.TypeName, v.Decoded)
// ... InterfaceValue, MapValue, SliceValue, ArrayValue,
//     UintValue, FloatValue, ComplexValue, BoolValue,
//     StringValue, BytesValue, NilValue
}
```

The full type definitions are documented in [docs/api.md](docs/api.md).

## Decoding limits

Limits can be set at construction time to bound resource use on untrusted input:

```go
ins := gobspect.New(gobspect.WithReadLimit(4 * 1024 * 1024)) // 4 MiB
```

`WithReadLimit` caps the total bytes read across the entire stream. Zero means no limit.

Hard limits are always enforced regardless of options: 64 MiB per message, 65536 struct fields, and 2^30 elements in slices, maps, and arrays.

## Error handling

The decoder does not panic on malformed input. All errors are returned. `Stream.Collect` returns partial results alongside any error: a stream that decodes successfully up to a corrupt message returns those values plus the error. The `Values` iterator similarly stops and yields `(nil, err)` on the first error.

## Query

The [`query`](query/README.md) subpackage (`github.com/codepuke/gobspect/query`) provides path-based navigation of decoded `Value` trees without manual type switches:

```go
v, ok    := query.Get(root, "Orders.0.Customer.Name")
names    := query.All(root, "Orders.*.Customer.Name")
keys, _  := query.Keys(root, "Orders.0")
```

For hot paths or explicit error handling, compile the expression once with `query.Parse` and reuse it with `query.GetPath` or `query.AllPath`:

```go
p, err := query.Parse("Orders.*.Customer.Name")
if err != nil { ... }
for _, root := range roots {
    names := query.AllPath(root, p)
}
```

For lazy, streaming enumeration (early-break safe), use `query.AllPathSeq`:

```go
for v := range query.AllPathSeq(root, p) {
    // process v; break at any time to stop early
}
```

See [query/README.md](query/README.md) for the full path syntax, filter expressions, and API reference.

## Sorting

The [`sortval`](sortval/README.md) subpackage (`github.com/codepuke/gobspect/sortval`) sorts sequences of `Value` nodes by struct field keys:

```go
spec, err := sortval.ParseSortSpec("Name,Score", false, false, false)
if err != nil { ... }

sorted := sortval.SortMatches(sortval.SeqOf(results), spec)
```

`ParseSortSpec` accepts a comma-separated list of field names plus flags for descending order (`desc`), case-insensitive comparison (`fold`), and exclusion of rows missing all sort keys (`dropMissing`). The comparison delegates to `gobspect.CompareValues` / `gobspect.CompareValuesFold`.

See [sortval/README.md](sortval/README.md) for the full API reference.

## Tabular output

The [`tabular`](tabular/README.md) subpackage (`github.com/codepuke/gobspect/tabular`) writes `Value` nodes as CSV or TSV rows:

```go
tp := tabular.NewPrinter(&buf,
    tabular.WithStream(stream),
    tabular.WithDelimiter(','),
    tabular.WithHeterogeneousMode(tabular.HeterogeneousUnion),
)

for v, err := range stream.Values() {
    if err != nil { ... }
    if err := tp.WriteValue(v); err != nil { ... }
}
tp.Flush()
```

The printer derives a header row from the first struct's field definitions, aligns sparse gob rows to the canonical column order, and supports four strategies for mixed-type streams: `FirstWins`, `Reject`, `Union`, and `Partition`.

See [tabular/README.md](tabular/README.md) for all options and heterogeneous-mode details.

## Documentation

- [docs/api.md](docs/api.md) - Full API reference including all Value node types and formatting options
- [docs/architecture.md](docs/architecture.md) - Design principles and two-layer model
- [docs/wire-format.md](docs/wire-format.md) - Gob wire format reference for implementers
- [docs/opaque-types.md](docs/opaque-types.md) - Opaque type decoding strategy and built-in decoder formats
- [docs/testing.md](docs/testing.md) - Fixture generation and golden file testing strategy

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on opening issues, making minimal focused changes, and the pull request process.

## License

[MIT](LICENSE.txt)
