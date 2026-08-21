---
title: Gob Wire Format Reference
---

# Gob Wire Format Reference

This page is a reference for the [`encoding/gob`](https://pkg.go.dev/encoding/gob) wire format — the byte-level encoding that gobspect reads. You do not need any of this to use the library, but it helps when interpreting gobspect's output: the type IDs, sparse struct fields, opaque blobs, and message framing described here all surface directly in the decoded value tree and in error messages. The authoritative specification is the [gob package documentation](https://pkg.go.dev/encoding/gob).

## Integer Encoding

All framing and structural values use gob's variable-length integer encoding.

**Unsigned integers:** If the value is < 128, it is sent as a single byte. Otherwise it is sent as a big-endian byte stream preceded by one byte holding the negated byte count. Examples:

- `0` → `0x00`
- `127` → `0x7F`
- `128` → `0xFF 0x80` (byte count = 1, negated = 0xFF)
- `256` → `0xFE 0x01 0x00` (byte count = 2, negated = 0xFE)

**Signed integers:** Encoded as unsigned integers after zig-zag transformation. Non-negative values are sent as `2*x`, negative values as `~(2*x)` (bitwise complement). This ensures small-magnitude values use few bytes regardless of sign.

Note that this scheme is *not* the same as the varints in Go's `encoding/binary` package.

## Predefined Type IDs

These are hardcoded and never appear as type definitions in the stream:

| ID | Type |
|---|---|
| 1 | bool |
| 2 | int (all signed integer sizes) |
| 3 | uint (all unsigned integer sizes) |
| 4 | float (float32 and float64) |
| 5 | []byte |
| 6 | string |
| 7 | complex (complex64 and complex128) |
| 8 | interface{} |

`[]byte` is a special case: it is a builtin type with its own ID, not a slice of uint8 with a sliceType definition.

## Bootstrap Type IDs

Used to decode the type definition system itself. A decoder must understand these without ever seeing definitions for them:

| ID | Type |
|---|---|
| 16 | wireType |
| 17 | arrayType |
| 18 | CommonType |
| 19 | sliceType |
| 20 | structType |
| 21 | fieldType |
| 22 | []fieldType |
| 23 | mapType |

## Stream Grammar

A gob stream is a sequence of length-prefixed messages. Each message carries either a type definition (negative type ID) or a value (positive type ID):

```
GobStream        = DelimitedMessage*
DelimitedMessage = uint(length) Message
Message          = TypeSequence TypedValue
TypeSequence     = (TypeDefinition DelimitedTypeDefinition*)?
DelimitedTypeDefinition = uint(length) TypeDefinition
TypeDefinition   = int(-typeId) encodingOfWireType
TypedValue       = int(typeId) Value
```

Key points:

- **Negative type IDs signal type definitions. Positive type IDs signal values.**
- **Type IDs are session-scoped.** They are assigned by the encoder in the order types are first sent, so the same Go type can have different IDs in different streams. A decoder builds a fresh type registry per stream.
- **User-defined type IDs start at 64.** IDs below 64 are reserved for the builtin and bootstrap types. gobspect rejects a stream that tries to define a reserved ID, or that defines the same ID twice — errors of the form `type definition for reserved ID …` or `duplicate definition for type ID …` indicate a corrupt (or hostile) stream.

Each call to `Encoder.Encode` on the producing side emits the type definitions the value needs (only those not already sent) followed by a value message. A stream of many encoded values is therefore front-loaded with type definitions and then settles into compact value messages (interface values are the one case where a single `Encode` spans several messages — see the Interface Encoding section):

:::examples stream-multiple-values

## Value Encoding

```
Value           = SingletonValue | StructValue
SingletonValue  = uint(0) FieldValue
FieldValue      = builtinValue | ArrayValue | MapValue | SliceValue
                  | StructValue | InterfaceValue

StructValue     = (uint(fieldDelta) FieldValue)*
```

**Struct fields are encoded sparsely.** The encoder tracks the index of the last field it sent, starting at −1 before any field. Each transmitted field is preceded by a uint delta — the gap from that previous index — so the first field of a struct (index 0) carries delta 1, and consecutive fields each carry delta 1. Zero-valued fields are omitted entirely, which shows up as a larger delta. A delta of 0 terminates the struct.

**Non-struct top-level values** (ints, strings, slices, maps, …) are encoded as a singleton: a single `uint(0)` marker — the "delta" addressing field 0 of an implicit one-field struct — followed immediately by the value. There is no trailing terminator after a singleton value.

**Slices, arrays, and maps** are encoded as a uint element count followed by the elements back to back (key then value, alternating, for maps). Elements carry no field deltas — just their raw value encodings.

:::examples encode-struct

### An annotated example

Encoding the int `7` produces four bytes:

```
03 04 00 0e
```

| Bytes | Meaning |
|---|---|
| `03` | message length: 3 body bytes follow |
| `04` | signed type ID 2 (int) — zig-zag encoded as 2×2 = 4 |
| `00` | singleton marker `uint(0)` |
| `0e` | the value 7 — zig-zag encoded as 2×7 = 14 |

No type definition message is needed because `int` is a predefined type.

## Type Definitions (wireType)

A type definition message carries a `wireType` — a struct with seven optional fields, exactly one of which is set. The set field determines the kind:

```go
wireType struct {
    ArrayT           *arrayType       // field 1
    SliceT           *sliceType       // field 2
    StructT          *structType      // field 3
    MapT             *mapType         // field 4
    GobEncoderT      *gobEncoderType  // field 5
    BinaryMarshalerT *gobEncoderType  // field 6
    TextMarshalerT   *gobEncoderType  // field 7
}
```

The per-kind descriptors:

```go
CommonType struct { Name string; Id int }

arrayType  struct { CommonType; Elem typeId; Len int }
sliceType  struct { CommonType; Elem typeId }
mapType    struct { CommonType; Key typeId; Elem typeId }
structType struct { CommonType; Field []fieldType }
fieldType  struct { Name string; Id int }

gobEncoderType struct { CommonType }
```

**Embedded structs are not flattened on the wire.** Although `CommonType` is written as an embedded field in the Go source above, gob encodes it as a *nested struct at field 1* of each containing type — its `Name` and `Id` are fields of that inner struct, not inlined fields of the outer one. Every descriptor therefore begins with field delta 1 introducing a complete, terminated `CommonType` struct, after which the remaining fields (`Elem`, `Key`, `Len`, `Field`) follow at deltas 2 and up.

## Interface Encoding

Interface-typed values embed the concrete type's name, any type definitions the receiver has not seen yet, and the concrete value:

```
InterfaceValue       = NilInterfaceValue | NonNilInterfaceValue
NilInterfaceValue    = uint(0)
NonNilInterfaceValue = ConcreteTypeName InlineTypeDefs InterfaceContents
ConcreteTypeName     = uint(nameLength) name
InlineTypeDefs       = (int(-typeId) encodingOfWireType)*
InterfaceContents    = int(concreteTypeId) uint(valueLength) Value
```

The concrete type name is a registered string (e.g. `"time.Time"`), capped at 1024 bytes. A zero name length signals a nil interface. The value body is length-prefixed so a decoder that does not know the concrete type could skip it; a zero-length value body is invalid.

Two details make interface values more intricate than the grammar suggests:

- **Inline type definitions are not length-delimited.** Unlike top-level type definition messages, the definitions inside `InlineTypeDefs` are a bare negated type ID followed directly by the `wireType` body — there is no `uint(length)` prefix.
- **An interface value can span multiple stream messages.** In the common case the encoder ends the current message after the concrete type name, sends the concrete type's definitions as ordinary top-level type definition messages, and delivers the concrete type ID and value bytes in a *continuation message* — a subsequent outer message whose body continues the interrupted value. A single decoded value containing interfaces can therefore consume several wire messages.

:::examples interface-values

## Opaque Encoder Blobs

When a wireType has `GobEncoderT`, `BinaryMarshalerT`, or `TextMarshalerT` set, the value is a raw byte blob: a uint byte length followed by exactly that many bytes. No structural information is provided — the format is defined entirely by the implementing type's marshal method.

How gobspect interprets the blob depends on the variant:

- **`TextMarshalerT`** blobs are UTF-8 text by contract. gobspect always decodes them as strings, with no registry lookup. Note that Go 1.26 removed `TextMarshaler` support from gob — streams written by older Go versions still contain `TextMarshalerT` types, but newer encoders write those types as plain structs.
- **`GobEncoderT` and `BinaryMarshalerT`** blobs are handed to the opaque decoder registered for the type name, if any (built-ins cover `time.Time`, `big.Int`, and more — see the Opaque Types page). Undecoded blobs keep their raw bytes.
- **Blobs with an empty type name** — which occur when a `GobEncoder` type is encoded directly rather than through an interface, since `CommonType.Name` is empty in that case — are tried against decoders registered with `RegisterUnnamedDecoder`, in registration order.

## Limits

gobspect enforces hard bounds while decoding, matching or tightening the standard library's, so a corrupt or hostile stream fails with an error instead of exhausting memory. Exceeding any of these surfaces as a decode error:

| Limit | Value |
|---|---|
| Message body length | 64 MiB (2²⁶ bytes) |
| Struct field count per type definition | 65,536 |
| Slice, array, or map element count | 2³⁰ |
| String, `[]byte`, or opaque blob length | 2³⁰ bytes |
| Interface concrete type name | 1,024 bytes |
| Value nesting depth | 10,000 levels |

Wire-supplied lengths are also validated against the bytes actually remaining in the message before any allocation, so a fabricated length cannot force a large up-front allocation. Independently of these per-item bounds, `WithReadLimit` caps the total bytes read from a stream.

## How This Maps to gobspect

The wire concepts above appear directly in gobspect's API:

- **Value messages → `Stream.Values`.** Each top-level value in the stream yields one decoded `Value` — usually one positive-ID message, plus any continuation messages its interface fields pulled in. The singleton wrapper and struct field deltas are consumed during decoding and do not appear in the tree.
- **Sparse fields → missing entries in `StructValue.Fields`.** A field the encoder omitted (because it was zero-valued) is simply absent; gobspect does not invent zero placeholders, because the wire carries no record of them.
- **Type IDs → `GobTypeID` and `Stream.TypeByID`.** Composite and opaque nodes (`StructValue`, `MapValue`, `SliceValue`, `ArrayValue`, `OpaqueValue`) record their stream-scoped type ID in `GobTypeID` (also exposed via the `Value.TypeID` method). Pass it to `Stream.TypeByID` to recover the full definition.
- **Type definition messages → `TypeInfo`.** Every negative-ID message becomes a `TypeInfo` in `Stream.Types`, with `TypeRef` links between types; `Stream.Schema` renders them as Go-style declarations.
- **Encoder blobs → `OpaqueValue`.** The blob's bytes land in `Raw`, the wireType variant (`"gob"`, `"binary"`, or `"text"`) in `Encoding`, the `CommonType.Name` in `TypeName`, and any decoder output in `Decoded`.
- **Message framing → `MessageInfo`.** `Stream.Messages` iterates the raw framing without decoding values: each `MessageInfo` carries the message's `Index`, byte `Offset`, `BodyLen`, signed `TypeID` (negative = type definition), and raw `Body` — useful for size profiling or indexing a stream.
