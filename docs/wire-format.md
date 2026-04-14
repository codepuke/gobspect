# Gob Wire Format Reference

This is a condensed reference for implementers. The authoritative source is `encoding/gob/doc.go` in the Go standard library.

## Integer Encoding

All framing and structural values use gob's variable-length integer encoding.

**Unsigned integers:** If the value is < 128, it is sent as a single byte. Otherwise it is sent as a big-endian byte stream preceded by one byte holding the negated byte count. Examples:

- `0` → `0x00`
- `127` → `0x7F`
- `128` → `0xFF 0x80` (byte count = 1, negated = 0xFF)
- `256` → `0xFE 0x01 0x00` (byte count = 2, negated = 0xFE)

**Signed integers:** Encoded as unsigned integers after zig-zag transformation. Non-negative values are sent as `2*x`, negative values as `~(2*x)` (bitwise complement). This ensures small-magnitude values use few bytes regardless of sign.

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

## Bootstrap Type IDs

Used to decode the type definition system itself. The decoder must understand these without seeing definitions for them:

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

```
GobStream       = DelimitedMessage*
DelimitedMessage = uint(length) Message
Message         = TypeSequence TypedValue
TypeSequence    = (TypeDefinition DelimitedTypeDefinition*)?
DelimitedTypeDefinition = uint(length) TypeDefinition
TypeDefinition  = int(-typeId) encodingOfWireType
TypedValue      = int(typeId) Value
```

Key point: negative type IDs signal type definitions. Positive type IDs signal values. Type IDs are session-scoped and assigned by the encoder.

## Value Encoding

```
Value           = SingletonValue | StructValue
SingletonValue  = uint(0) FieldValue
FieldValue      = builtinValue | ArrayValue | MapValue | SliceValue
                  | StructValue | InterfaceValue

StructValue     = (uint(fieldDelta) FieldValue)*
```

Struct fields are encoded sparsely. Each field is preceded by a delta from the previous field index (1-based). A delta of zero signals the end of the struct. Zero-valued fields are omitted entirely.

Non-struct top-level values are wrapped in a synthetic single-field struct: the value is sent as field 1, followed by a zero terminator.

## Type Definitions (wireType)

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

Exactly one field is set per wireType. The set field determines the kind.

```go
CommonType struct { Name string; Id int }

arrayType  struct { CommonType; Elem typeId; Len int }
sliceType  struct { CommonType; Elem typeId }
mapType    struct { CommonType; Key typeId; Elem typeId }
structType struct { CommonType; Field []fieldType }
fieldType  struct { Name string; Id int }

gobEncoderType struct { CommonType }
```

## Interface Encoding

```
InterfaceValue     = NilInterfaceValue | NonNilInterfaceValue
NilInterfaceValue  = uint(0)
NonNilInterfaceValue = ConcreteTypeName TypeSequence InterfaceContents
ConcreteTypeName   = uint(nameLength) name
InterfaceContents  = int(concreteTypeId) uint(valueLength) Value
```

The concrete type name is a string (e.g., `"time.Time"`). The receiver must know how to decode this type — either via the type definitions already in the stream or via a pre-registered type.

## Opaque Encoder Blobs

When a wireType has `GobEncoderT`, `BinaryMarshalerT`, or `TextMarshalerT` set, the value is encoded as a raw byte slice. No structural information is provided — the blob's format is defined entirely by the implementing type's marshal method.

The blob is preceded by its byte length as a uint. The decoder reads exactly that many bytes and hands them to an opaque decoder if one is registered for the type name.
