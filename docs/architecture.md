# Architecture

gobspect is a decode-only introspection library for Go's `encoding/gob` wire format. It reads arbitrary gob streams without requiring the original Go types and produces a structured, human-readable representation of the contents.

## Design Principles

- **Decode only.** Encoding is out of scope. If you control the types, use `encoding/gob` directly.
- **Zero runtime dependencies on inspected types.** The library never imports `time`, `math/big`, `github.com/google/uuid`, etc. Opaque type decoders are self-contained reimplementations of the relevant binary formats.
- **Two-layer output.** A structural AST preserves all wire information. A formatting layer renders it for humans. Consumers choose which layer they need.
- **Extensible opaque decoding.** Users register their own decoders for application-specific `GobEncoder`/`BinaryMarshaler` types via a public registry.

## Package Layout

```
gobspect/
├── decode.go          # Stream reader, message framing, type/value dispatch
├── valuedecode.go     # Value decoding: primitives, structs, maps, slices, arrays, opaques, interfaces
├── types.go           # Value AST node types
├── wire.go            # Wire format primitives (varint, type ID, wireType decoding)
├── registry.go        # Opaque decoder registry and built-in registration
├── builtins.go        # Decoders for std lib opaque types (time.Time, big.Int, etc.)
├── format.go          # Human-readable rendering of Value trees
├── doc.go             # Package documentation
└── (various)_test.go  # Tests
```

## Two-Layer Model

### Layer 1: Structural AST

The core output of decoding is a tree of `Value` nodes. This layer preserves everything from the wire format: type IDs, type names, field names, raw bytes for opaque blobs. It never loses information and is the foundation for programmatic consumers.

```go
type Value interface {
    gobValue()
}
```

Concrete node types: `StructValue`, `MapValue`, `SliceValue`, `ArrayValue`, `IntValue`, `UintValue`, `FloatValue`, `ComplexValue`, `BoolValue`, `StringValue`, `BytesValue`, `NilValue`, `OpaqueValue`, `InterfaceValue`.

See [api.md](api.md) for the full type definitions.

### Layer 2: Human-Readable Formatting

A `Format(Value, ...FormatOption) string` function walks the AST and produces readable output. Opinionated choices:

| Type | Rendered as |
|---|---|
| `time.Time` | RFC 3339 with nanoseconds |
| UUID | `8-4-4-4-12` hex |
| `big.Int` / `big.Rat` / `big.Float` | Decimal string |
| `shopspring/decimal` | Reconstructed decimal string |
| Any `TextMarshalerT` | The UTF-8 string as-is |
| Unknown `GobEncoderT` / `BinaryMarshalerT` | `(type.Name) 0a1b2c3d…` hex |
| `[]byte` | Hex, or quoted string if valid printable UTF-8 |
| Structs | Indented field tree |

## Stream Processing

Gob streams are sequential. The decoder maintains a per-stream type registry built from inline type definitions. Processing order:

1. Read message byte count.
2. If the type ID is negative, it is a type definition. Decode the `wireType` and register it.
3. If the type ID is positive, it is a value. Look up the type, decode the value into the AST.
4. Repeat until EOF.

The decoder yields one `Value` per top-level `Encode` call in the original stream. A stream may contain multiple values.

## Error Handling

The decoder operates on untrusted input. It must not panic. All size fields are bounds-checked before allocation. Errors are returned, not logged. Partial results are returned alongside errors when possible — a stream that decodes 5 values and then hits corruption should return those 5 values plus the error.

Hard limits enforced per message: 64 MiB (1<<26) maximum message size, 65536 maximum struct fields, 1<<30 maximum elements in slices, maps, and arrays.
