---
title: How gobspect Works
---

gobspect is a decode-only introspection library for Go's `encoding/gob` wire format. It reads arbitrary gob streams without requiring the original Go types and produces a structured, human-readable representation of the contents. This page explains the design: what the library promises, how the pieces fit together, and where its limits are.

## Design Principles

- **Decode only.** Encoding is out of scope. If you control the types, use `encoding/gob` directly.
- **No third-party imports of inspected types.** Decoders for types like `github.com/google/uuid` and `github.com/shopspring/decimal` are clean-room reimplementations of their wire formats, so `go.mod` stays minimal and gobspect keeps working even if an upstream type changes or disappears. The standard library is a different matter: it is always available to consumers, so gobspect uses it internally where that aids correctness — for example, `net/netip` decoding delegates to the stdlib's own `UnmarshalBinary` to track its behavior exactly. Either way, inspected types never leak into the output: an `OpaqueValue.Decoded` field holds formatted strings or primitive Go values, never a `time.Time` or `netip.Addr`.
- **Two-layer output.** A structural AST preserves all wire information. Presentation layers render it for humans or machines. Consumers choose which layer they need.
- **Extensible opaque decoding.** You can register your own decoders for application-specific `GobEncoder`/`BinaryMarshaler` types, and override any built-in.

## Two-Layer Model

### Layer 1: the structural AST

The core output of decoding is a tree of `Value` nodes. This layer preserves everything from the wire format: type IDs, type names, field names, raw bytes for opaque blobs. It never loses information, and it is the foundation every other feature is built on.

```go
type Value interface {
    gobValue()
}
```

Concrete node types: `StructValue`, `MapValue`, `SliceValue`, `ArrayValue`, `IntValue`, `UintValue`, `FloatValue`, `ComplexValue`, `BoolValue`, `StringValue`, `BytesValue`, `NilValue`, `OpaqueValue`, `InterfaceValue`. Dispatch on them with a type switch. See the API Guide page for the full type definitions.

### Layer 2: presentation

`Format(v Value, opts ...FormatOption) string` walks the AST and produces readable text; `FormatTo` writes the same output to an `io.Writer`. Options such as `WithIndent`, `WithMaxBytes`, `WithBytesFormat`, `WithColor`, and the redaction options adjust the rendering without ever touching the underlying AST.

:::examples format-options

Formatting is not the only consumer of the AST. The same `Value` tree feeds several other output surfaces, all covered in depth on the API Guide page:

- **JSON** — `ToJSON` and `ToJSONIndent` render a `Value` as JSON.
- **Schema** — `Stream.Schema` drains a stream and returns every type definition it declares, formatted as a `Schema`.
- **Stats** — `Stream.Stats` accumulates per-type and per-field statistics across a whole stream.
- **Comparison** — `Equal`, `CompareValues`, and the configurable `Comparer` compare two `Value` trees structurally, which is also what powers the diff subpackage.

:::examples to-json

Because every one of these reads from the same lossless AST, they always agree with each other: what `Format` shows you is what `ToJSON` serializes and what `Equal` compares.

## Built-in Opaque Decoders

Types that implement `GobEncoder`, `BinaryMarshaler`, or `TextMarshaler` appear on the wire as opaque byte blobs. gobspect represents them as `OpaqueValue` nodes carrying the raw bytes, and pre-registers decoders for common types so they render meaningfully:

| Type | Rendered as |
|---|---|
| `time.Time` | RFC 3339 with nanoseconds (customizable via `WithTimeFormat`) |
| UUID (`google/uuid`, `gofrs/uuid`) | `8-4-4-4-12` hex |
| `big.Int` / `big.Rat` | Decimal string, auto-detected by wire format |
| `big.Float` | Decimal string, registered under `"math/big.Float"` |
| `shopspring/decimal.Decimal` | Reconstructed decimal string |
| `netip.Addr` | Canonical address string, e.g. `192.0.2.1`, `fe80::1%eth0` |
| `netip.Prefix` | CIDR string, e.g. `10.0.0.0/24` |
| `netip.AddrPort` | `192.0.2.1:80`, `[fe80::1]:8080` |
| Any `TextMarshalerT` | The UTF-8 string as-is |
| Unknown `GobEncoderT` / `BinaryMarshalerT` | `(TypeName) 0a1b2c3d…` hex, truncated at 64 bytes by default |
| `[]byte` | Quoted string if printable UTF-8, hex otherwise |

Two caveats worth knowing:

- **Go 1.26 removed `TextMarshaler` support from gob.** Streams written by older Go versions may still contain `TextMarshalerT` blobs — gobspect decodes them as UTF-8 strings automatically — but streams from Go 1.26+ encode those types as plain structs instead.
- **Decoder lookups key on the type's bare name** as recorded in the stream (`"Time"`, `"UUID"`, `"Decimal"`). When a `GobEncoder` type is encoded directly rather than through an interface, gob records an empty name, so named lookups cannot match; the `(TypeName)` prefix in formatted output is likewise omitted when the name is empty.

Register your own decoder with `Inspector.RegisterDecoder`, keyed by type name. Registrations override built-ins for the same name:

:::examples register-decoder

## Stream Processing

Gob streams are sequential and self-describing: type definitions arrive inline, interleaved with values. The decoder maintains a per-stream type registry and processes messages in order:

1. Read the message byte count.
2. If the type ID is negative, it is a type definition. Decode it and register it.
3. If the type ID is positive, it is a value. Look up the type and decode the value into the AST.
4. Repeat until EOF.

The decoder yields one `Value` per top-level `Encode` call in the original stream; a stream may contain any number of values. On the wire, gob wraps non-struct top-level values in a synthetic single-field struct — gobspect strips that wrapper, so an encoded `int` yields an `IntValue` directly.

```go
ins := gobspect.New()
stream := ins.Stream(r)
```

`Stream.Values` returns a lazy iterator, so you can stop early without decoding the rest of the input; `Stream.Collect` drains everything into a slice. Type definitions accumulate as the stream is consumed and are available at any point through `Stream.Types` and `Stream.TypeByID` — by the time a value is yielded, every type it references is already registered.

:::examples stream-values

Because type IDs are scoped to a single stream, each call to `Inspector.Stream` builds a fresh registry; a `Stream` is single-use.

## Packages

The module is organized as a small core plus focused subpackages, each importable on its own:

- **`github.com/codepuke/gobspect`** — the core: stream decoding, the `Value` AST, formatting, JSON output, schema extraction, statistics, and structural comparison.
- **`github.com/codepuke/gobspect/query`** — path-based navigation of decoded `Value` trees (`"Orders.*.Customer.Name"`), including wildcards, filters, and recursive descent.
- **`github.com/codepuke/gobspect/diff`** — structural diffing of two `Value` trees or two streams, producing a `Delta` AST that mirrors the input structure.
- **`github.com/codepuke/gobspect/sortval`** — sorting sequences of `Value` nodes by struct-field keys.
- **`github.com/codepuke/gobspect/tabular`** — CSV/TSV rendering of `Value` nodes.
- **`github.com/codepuke/gobspect/decompress`** — transparent decompression by magic-byte sniffing (gzip, zstd, xz, bzip2, zip); uncompressed input passes through unchanged.
- **`github.com/codepuke/gobspect/gq`** — the gq query engine as a library: the result pipeline, output rendering, and aggregation behind the CLI.
- **`gq` (command)** — a jq-inspired command-line tool for inspecting gob streams, installable with:

```sh
go install github.com/codepuke/gobspect/cmd/gq@latest
```

The `gq` and `decompress` subpackages are libraries in their own right so that frontends beyond the CLI — an MCP server, your own tooling — share one implementation of query execution, rendering, aggregation, and compression sniffing, and behave identically to the `gq` command.

## Error Handling and Limits

The decoder operates on untrusted input. It never panics: all size fields are bounds-checked before allocation, and every failure is returned as an error. Partial results are preserved where possible — `Stream.Collect` returns the values decoded before an error alongside the error itself.

For streams that may contain occasional bad records, `WithSkipCorruptValues` tells the inspector to skip individual corrupt value messages and keep going, counting them in `Stream.SkipCount`. Errors in type-definition messages remain fatal, because they would leave the type registry inconsistent.

Hard limits are enforced per message: 64 MiB (1<<26) maximum message size, 65536 maximum struct fields, and 1<<30 maximum elements in slices, maps, and arrays. On top of those fixed guards, `WithReadLimit` caps the total bytes read from a stream — useful when the input's size is not known in advance:

```go
ins := gobspect.New(gobspect.WithReadLimit(10 << 20)) // 10 MiB
```
