---
title: Opaque Type Decoding
---

When a Go type implements `GobEncoder`, `BinaryMarshaler`, or `TextMarshaler`, gob serializes it as an opaque byte blob accompanied only by a type name. The wire format carries no schema for the blob's internal structure, so gobspect decodes these blobs with out-of-band knowledge where it has it — and shows you the raw bytes where it doesn't. This page explains which types decode automatically, how decoders are matched to wire type names, and how to register your own.

## Why Some Values Arrive as Byte Blobs

Gob normally self-describes: struct, slice, and map types are transmitted as wire type definitions, and any reader can walk the values. A type with a custom marshaler opts out of that. The encoder calls the type's `GobEncode`, `MarshalBinary`, or `MarshalText` method and writes whatever bytes come back. Only the type knows what those bytes mean.

:::examples gobencoder-type

An introspection tool reading such a stream sees three things: a wire kind (`GobEncoderT`, `BinaryMarshalerT`, or `TextMarshalerT`), a type name (possibly empty — see below), and the blob. gobspect preserves all three in an `OpaqueValue` node and additionally attempts a best-effort decode.

## Decoding Strategy by Wire Kind

### TextMarshalerT — universal, no registry needed

> **Note:** Go 1.26 and later no longer use `TextMarshaler` encoding in gob — such types are encoded as plain structs. The `TextMarshalerT` path still works for streams produced by Go 1.25 and earlier.

By contract, `TextMarshaler.MarshalText()` returns valid UTF-8. The blob is always a human-readable string, so no per-type decoder is ever needed: every `TextMarshalerT` blob is decoded as `string(data)`, with no registry lookup.

Of the familiar stdlib candidates, only `regexp.Regexp` actually takes this path. `net/url.URL` and the `net/netip` types also implement `BinaryMarshaler`, which gob prefers, so they arrive as `BinaryMarshalerT`. `encoding/json.Number` implements no marshaler interface at all and encodes as a plain string.

### GobEncoderT and BinaryMarshalerT — per-type decoders

These blobs are arbitrary binary and require explicit knowledge of each type's format. gobspect ships decoders for common types and exposes a registry for your own. The `OpaqueValue.Encoding` field records which kind arrived: `"gob"`, `"binary"`, or `"text"`.

## How Decoders Match Type Names

The wire type name (`OpaqueValue.TypeName`) is what gob puts in `CommonType.Name`, and the rules are worth knowing before you register anything:

- **Value-receiver marshaler, encoded directly:** the name is `reflect.Type.Name()` — the bare, unqualified type name. `time.Time` arrives as `"Time"`, `shopspring/decimal.Decimal` as `"Decimal"`, `uuid.UUID` as `"UUID"`.
- **Pointer-receiver marshaler, encoded directly:** the name is empty. `reflect.Type.Name()` on a pointer type is `""`, so `*big.Int`, `*big.Rat`, and `*big.Float` all arrive with `TypeName == ""`.
- **Transmitted through an interface field:** the name is whatever was passed to `gob.Register`, which defaults to the path-qualified name (e.g. `"myapp/internal.SessionToken"`).

Two registration mechanisms cover these cases:

- `RegisterDecoder(typeName, fn)` keys a decoder by exact type name. It only fires when the wire name matches the key exactly, and it overrides any built-in registered under the same name.
- `RegisterUnnamedDecoder(fn)` appends a decoder to the list tried for opaque values whose wire type name is empty. Decoders run in registration order, and the first one to return a non-error, non-nil result wins. The built-in `big.Int`/`big.Rat` auto-detector is registered during `New()`, so it is always first in this list; it rejects blobs that don't match those two formats, letting your decoders handle everything else.

Named and unnamed lookups never mix: a blob with a non-empty type name is only offered to the decoder registered under that exact name, and a blob with an empty name is only offered to the unnamed list.

## Built-in Decoders

### time.Time

`time.Time` implements `GobEncoder` with a value receiver (its `GobEncode` delegates to `MarshalBinary`), so it arrives as `GobEncoderT` with `Encoding: "gob"` and `TypeName: "Time"`. Two blob versions are supported.

**Version 1** — 15 bytes total:

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Version byte (`1`) |
| 1 | 8 | Seconds since Go's internal epoch — January 1, year 1, 00:00:00 UTC (not the Unix epoch) — big-endian int64 |
| 9 | 4 | Nanoseconds, big-endian int32 |
| 13 | 2 | Timezone offset in minutes east of UTC, big-endian int16. The value `-1` is a sentinel meaning UTC, not a −1-minute offset. |

**Version 2** — 16 bytes total (adds sub-minute timezone precision):

| Offset | Size | Content |
|---|---|---|
| 0–14 | 15 | Same as version 1 |
| 15 | 1 | Sub-minute timezone offset in seconds, signed int8 |

Rendered as RFC 3339. The fractional-second part trims trailing zeros and is omitted entirely when the nanosecond field is zero; a UTC offset renders as `Z`. Examples: `2024-01-15T09:30:00.123456789-06:00`, `2024-01-15T15:30:00Z`.

To render times with a different layout, pass the `WithTimeFormat` option to `New`:

```go
ins := gobspect.New(gobspect.WithTimeFormat(time.RFC1123))
```

The layout is a standard Go time format string; an empty layout means `time.RFC3339Nano`.

### math/big.Int and math/big.Rat (auto-detected)

Both types use pointer-receiver `GobEncode`, so their wire type name is empty and name-based lookup can never fire. Instead, a built-in unnamed decoder distinguishes them by format: both begin with a `(version << 1) | negBit` byte where the version is always `1`, so the first byte must be `0x02` or `0x03` — anything else is rejected as not-a-big-number. If bytes 1–4 read as a plausible big-endian numerator length, the blob is decoded as `big.Rat`; otherwise as `big.Int`.

**big.Int** — variable length:

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | `(version << 1) \| negBit`: `0x02` (positive or zero) or `0x03` (negative). Zero is indicated by the absence of absolute-value bytes, not a distinct sign value. |
| 1 | remaining | Absolute value, big-endian unsigned bytes (absent when the value is zero) |

Rendered as a decimal string, e.g. `-12345678901234567890`.

**big.Rat** — variable length:

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Sign/version byte, same encoding as `big.Int` (sign applies to the numerator) |
| 1 | 4 | Numerator absolute-value length, big-endian uint32 |
| 5 | n | Numerator absolute-value bytes |
| 5+n | remaining | Denominator absolute-value bytes (always positive, no sign byte) |

Rendered as `numerator/denominator`, or as a plain integer when the denominator is 1. Example: `355/113`.

### math/big.Float (not automatic)

The `big.Float` blob format, for reference:

| Offset | Size | Content |
|---|---|---|
| 0 | 1 | Version byte (`1`) |
| 1 | 1 | Packed flags: mode (3 bits), accuracy (2 bits), form (2 bits), negative flag (1 bit) |
| 2 | 4 | Precision in bits, big-endian uint32 |
| 6 | 4 | Exponent, big-endian int32 (present only when the form is finite) |
| 10 | remaining | Mantissa bytes |

**gobspect ships a `big.Float` decoder, but it never fires automatically.** Like `big.Int`, `*big.Float` uses a pointer receiver, so its wire type name is empty — and the built-in auto-detector deliberately rejects `big.Float` blobs (their version byte `0x01` is neither `0x02` nor `0x03`), because guessing between formats on empty-named blobs must stay conservative. The built-in decoder is registered under the key `"math/big.Float"`, which is useful for explicit lookups but never appears as a wire name.

If your streams contain `big.Float` values, register an unnamed decoder yourself:

```go
ins := gobspect.New()
ins.RegisterUnnamedDecoder(func(data []byte) (any, error) {
    var f big.Float
    if err := f.GobDecode(data); err != nil {
        return nil, err
    }
    return f.Text('g', -1), nil
})
```

Your decoder runs after the built-in big-number auto-detector, which passes on anything that isn't `big.Int`- or `big.Rat`-shaped.

When it does run (explicitly or via a registration like the above), the built-in decoder renders a fixed-point decimal string, falling back to scientific notation when the exponent's magnitude exceeds 2¹⁶ — a hostile blob could otherwise demand a multi-hundred-megabyte string.

### UUID

Applies to both `github.com/google/uuid` and `github.com/gofrs/uuid`. Arrives as `BinaryMarshalerT`: exactly 16 raw bytes in RFC 4122 layout.

Rendered as the standard UUID string, e.g. `550e8400-e29b-41d4-a716-446655440000`.

The wire name is the bare `UUID` for both libraries. The decoder is registered under `UUID`, with `uuid.UUID` kept as an alias for explicit lookups.

### shopspring/decimal.Decimal

`GobEncode` delegates to `MarshalBinary`, so both share one layout — a fixed-size exponent followed by a `big.Int` coefficient:

| Offset | Size | Content |
|---|---|---|
| 0 | 4 | Exponent, big-endian int32 |
| 4 | remaining | Coefficient, encoded exactly like a `big.Int` blob (sign/version byte + big-endian absolute value) |

The decimal value is `coefficient × 10^exponent`. Exponents outside ±10000 are rejected: they cannot come from the real library, and an unchecked value could demand a gigabyte-scale rendered string.

Rendered as the reconstructed decimal string, e.g. `123.45` (coefficient 12345, exponent −2). Registered under the bare wire name `Decimal`, with `decimal.Decimal` and `shopspring/decimal.Decimal` as aliases.

### net/netip.Addr, netip.Prefix, netip.AddrPort

These arrive as `BinaryMarshalerT`. Decoding delegates to the stdlib's own `UnmarshalBinary` on a zero-valued receiver, and only the canonical `String()` result is stored — no `netip.*` type ever enters the AST.

| Type | Wire shape |
|---|---|
| `netip.Addr` | 4 bytes (IPv4), 16 bytes (IPv6), or 16 bytes + zone identifier |
| `netip.Prefix` | `Addr` bytes followed by a 1-byte prefix length |
| `netip.AddrPort` | `Addr` bytes followed by a 2-byte little-endian port |

Rendered in canonical textual form: `1.2.3.4`, `::1`, `10.0.0.0/24`, `1.2.3.4:80`, `[fe80::1]:8080`. Registered under the bare keys `Addr`, `Prefix`, and `AddrPort`, with the qualified `netip.*` keys as aliases.

## Fallback for Unknown Types

Unknown `GobEncoderT` and `BinaryMarshalerT` blobs are preserved as `OpaqueValue` nodes with `Decoded = nil`. The formatter renders them as the type name in parentheses followed by the raw bytes:

```
(some/pkg.CustomType) 0a1b2c3d4e5f…
```

The parenthesized prefix appears only when the type name is non-empty; the bytes render as lowercase hex by default, truncated with a `…` suffix (see the formatting section below). Nothing is ever discarded: the full blob stays in `OpaqueValue.Raw`.

> **Caveat:** a `GobEncoder` type with a *pointer-receiver* marshaler, encoded directly rather than through an interface field, has an empty wire type name — `OpaqueValue.TypeName` will be `""` and name-keyed decoders cannot match it. Use `RegisterUnnamedDecoder` for such types. Value-receiver types keep their bare name (`Time`, `Decimal`, …) even when encoded directly.

## Registering Your Own Decoders

:::examples register-decoder

`RegisterDecoder` adds or overrides the decoder for a wire type name:

```go
ins := gobspect.New()
ins.RegisterDecoder("SessionToken", func(data []byte) (any, error) {
    if len(data) < 8 {
        return nil, errors.New("session token too short")
    }
    created := binary.BigEndian.Uint64(data[:8])
    return map[string]any{
        "created": time.Unix(int64(created), 0).Format(time.RFC3339),
        "payload": hex.EncodeToString(data[8:]),
    }, nil
})
```

Pick the key using the name-matching rules above: the bare type name for value-receiver types encoded directly, or the `gob.Register` name (usually path-qualified, e.g. `"myapp/internal.SessionToken"`) for values sent through interface fields. Built-ins are registered inside `New()` before your calls run, so registering under a built-in's name replaces it.

For types whose wire name is empty (pointer-receiver marshalers), use `RegisterUnnamedDecoder`. Unnamed decoders are tried in registration order against every empty-named blob, and the first to return a non-error, non-nil result wins — so each decoder should validate the format and return an error for blobs it doesn't recognize, leaving them for the next in line.

The returned `any` is stored in `OpaqueValue.Decoded` and used by the formatter. Return simple values — strings, numbers, maps and slices of simple values. The formatter renders decoded strings as-is and everything else via `fmt.Sprint`.

## When a Decoder Fails

Decoder failures are contained, never fatal:

- If a decoder returns an error, the value still decodes — `OpaqueValue.Decoded` simply stays `nil`, and the formatter falls back to the raw-bytes display.
- Panics inside a decoder are recovered and treated as errors. The type name that routes a blob to a decoder comes from the wire, so a hostile stream can feed any registered decoder arbitrary bytes; a decoder tripping over such input degrades to raw-bytes display rather than crashing the process.
- In the unnamed-decoder list, an error or `nil` result just moves on to the next decoder.

If your registered decoder appears to "do nothing", this is usually why: it returned an error (or panicked) on real input, and the formatter silently showed hex instead. Test the decoder directly against `OpaqueValue.Raw` to see the error.

## The OpaqueValue Node

Programmatic consumers of the AST see opaque blobs as `OpaqueValue`:

| Field | Type | Content |
|---|---|---|
| `TypeName` | `string` | The wire type name (`CommonType.Name`); may be empty for pointer-receiver types encoded directly |
| `GobTypeID` | `int` | The stream-scoped type ID; resolve with `Stream.TypeByID` |
| `Encoding` | `string` | `"gob"`, `"binary"`, or `"text"` for `GobEncoderT`, `BinaryMarshalerT`, and `TextMarshalerT` respectively |
| `Raw` | `[]byte` | The complete undecoded blob, always preserved |
| `Decoded` | `any` | The best-effort decoded form; `nil` when no decoder matched or the decoder failed |

`Decoded` only ever holds plain Go values — formatted strings, numbers, maps — never `time.Time`, `big.Int`, `uuid.UUID`, or other wrapper types, so reading the AST never requires importing the original types.

## Formatting Opaque Values

Several `Format` options interact with opaque values:

- `WithMaxBytes(n)` caps how many raw bytes are rendered before the `…` suffix. Default 64; zero means no limit.
- `WithBytesFormat(f)` selects the raw-bytes rendering: `BytesHex` (lowercase hex, the default), `BytesBase64`, or `BytesLiteral` (a Go `[]byte{0x0a, …}` literal).
- `WithRawOpaques(true)` renders the raw bytes instead of the decoded form even when `Decoded` is set — useful for inspecting the wire bytes a decoder consumed. The decoded form remains available in `OpaqueValue.Decoded`.
- `WithRedactTypes` matches against `OpaqueValue.TypeName`, so opaque values can be redacted by type name like any other value.

And on the decoding side, `WithTimeFormat(layout)` (an option to `New`, not `Format`) controls the layout of decoded `time.Time` values.

## Decoder Contract

A `DecoderFunc` has the signature `func([]byte) (any, error)` and should:

- Return errors for malformed input rather than panicking. (The library contains panics defensively, but an error is cheaper and carries context.)
- Not retain references to the input slice; copy if needed.
- Return a value suitable for `fmt.Sprint` display.
- Be safe for concurrent use — no shared mutable state.
