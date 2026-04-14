# Opaque Type Decoding

## The Problem

When a Go type implements `GobEncoder`, `BinaryMarshaler`, or `TextMarshaler`, gob serializes it as an opaque byte blob accompanied only by the type name. The wire format carries no schema for the blob's internal structure. An introspection library must either decode these blobs with out-of-band knowledge or present them as raw bytes.

## Decoding Strategy by Wire Kind

### TextMarshalerT — Universal, No Registry Needed

> **Warning:** Go 1.26+ no longer uses TextMarshaler encoding in gob. Types that implement `TextMarshaler` are encoded as plain structs. The `TextMarshalerT` decoder path still works for streams produced by Go 1.25 and earlier.

By contract, `TextMarshaler.MarshalText()` returns valid UTF-8. The blob is always a human-readable string. No per-type decoder is ever needed. The library handles all `TextMarshalerT` blobs with a single code path:

```go
return string(data)
```

Known std lib types using this path: `net/url.URL`, `net/netip.Addr`, `net/netip.AddrPort`, `net/netip.Prefix`, `regexp.Regexp`, `encoding/json.Number`.

### GobEncoderT and BinaryMarshalerT — Per-Type Decoders

These require explicit decoders. The library ships decoders for common types and exposes a registry for user-defined ones.

## Built-in Decoders

### time.Time (BinaryMarshalerT)

Two versions are supported.

**Version 1** — 15 bytes total:

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Version byte (`1`) |
| 1 | 8 | Seconds since Unix epoch, big-endian int64 |
| 9 | 4 | Nanoseconds, big-endian int32 |
| 13 | 2 | Timezone offset in minutes, big-endian int16 |

**Version 2** — 16 bytes total (adds sub-minute timezone precision):

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Version byte (`2`) |
| 1 | 8 | Seconds since Unix epoch, big-endian int64 |
| 9 | 4 | Nanoseconds, big-endian int32 |
| 13 | 2 | Timezone offset in minutes, big-endian int16 |
| 15 | 1 | Sub-minute timezone offset in seconds, signed int8 |

Rendered as: RFC 3339 with nanosecond precision. Example: `2024-01-15T09:30:00.123456789-06:00`

### math/big.Int (GobEncoderT)

Format: variable length.

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Packed sign/version byte: `(version << 1) \| negBit`. Version is always `1`, so the byte is `0x02` (positive or zero) or `0x03` (negative). Zero is indicated by the absence of absolute-value bytes, not by a distinct sign value. |
| 1 | remaining | Absolute value, big-endian unsigned bytes (absent when value is zero) |

Rendered as: decimal string. Example: `-12345678901234567890`

### math/big.Float (GobEncoderT)

Format: variable length.

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Packed metadata byte: precision bits (high), mode, accuracy, form, neg flag |
| 1 | 1 | Precision low bits (combined with byte 0 for full precision value) |
| 2 | 4 | Exponent, big-endian int32 (present only if form is finite) |
| 6 | remaining | Mantissa bytes |

Rendered as: decimal string. Example: `3.14159265358979323846`

### math/big.Rat (GobEncoderT)

Format: variable length.

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Sign/version byte (`(version << 1) \| negBit`, same encoding as `big.Int`) |
| 1 | 4 | Numerator absolute-value length, big-endian uint32 |
| 5 | n | Numerator absolute-value bytes |
| 5+n | remaining | Denominator absolute-value bytes (no sign byte; denominator is always positive) |

Rendered as: `numerator/denominator` or decimal if denominator is 1. Example: `355/113`

### UUID (BinaryMarshalerT)

Applies to both `github.com/google/uuid` and `github.com/gofrs/uuid`.

Format: exactly 16 bytes, raw RFC 4122 layout.

Rendered as: standard UUID string. Example: `550e8400-e29b-41d4-a716-446655440000`

The type name in the stream will be `uuid.UUID` for both libraries. The decoder matches on this name.

### shopspring/decimal.Decimal (GobEncoderT)

Format: `big.Int` coefficient followed by 4-byte exponent.

| Offset | Size | Content |
|---|---|---|
| 0 | len-4 | Coefficient, encoded as `big.Int` (sign byte + big-endian absolute value) |
| len-4 | 4 | Exponent, big-endian int32 |

The decimal value is `coefficient × 10^exponent`.

Rendered as: reconstructed decimal string. Example: `123.45` (coefficient=12345, exponent=-2)

## Fallback for Unknown Types

Unknown `GobEncoderT` and `BinaryMarshalerT` types are represented as `OpaqueValue` with `Decoded = nil`. The formatter renders them as:

```
(some/pkg.CustomType) 0a1b2c3d4e5f...
```

Type name in parentheses, followed by hex. Truncated at a configurable byte limit with `…` suffix.

> **Caveat:** When a `GobEncoder` type is encoded directly (not via an interface field), gob sends an empty `CommonType.Name` in the wireType. `OpaqueValue.TypeName` will be `""` in this case. The type name is only reliably populated when the value is transmitted through an interface field. Users registering decoders by name should be aware that name-based matching only works for interface-wrapped values.

## User-Registered Decoders

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

The key is the short type name (`CommonType.Name` from the wire format), not the full import path. The full import path only appears when the type is registered with `gob.Register` for transmission through an interface field — in that case, the wire name is the path-qualified name (e.g., `"myapp/internal.SessionToken"`), which should be used as the decoder key instead.

Registered decoders override built-in decoders for the same type name. The returned `any` is stored in `OpaqueValue.Decoded` and used by the formatter. Return simple types: strings, maps, slices of simple types. The formatter does not need to understand the structure — it uses `fmt.Sprint` as a last resort.

## Decoder Contract

An `OpaqueDecoder` function must:

- Not panic. Return errors for malformed input.
- Not retain references to the input slice. Copy if needed.
- Return a value suitable for `fmt.Sprint` display.
- Be safe for concurrent use (no shared mutable state).
