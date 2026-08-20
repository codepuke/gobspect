---
title: Overview
---

gobspect is a decode-only introspection library for Go's [`encoding/gob`](https://pkg.go.dev/encoding/gob) wire format. It reads arbitrary gob streams **without the original Go types** and produces a structured value tree plus human-readable output.

## What gobspect does

Standard `encoding/gob` decoding requires the type definitions that produced the stream. gobspect removes that requirement: it parses the wire format directly, reconstructs the type graph from the inline type definitions present in every gob stream, and yields a complete structural representation of the encoded data.

That makes it the right tool when:

- **You have an opaque `.gob` file** and no code to decode it — dump its contents and its Go-style type schema.
- **You need to inspect untrusted streams** — the decoder never panics on malformed input, returns all errors, and enforces hard resource limits (with an optional total read limit for hostile inputs).
- **You are building tooling** — debuggers, migration checkers, data pipelines — that must read gob data produced by code you do not control.

Output comes in two layers: a structural `Value` AST that preserves all wire information (type IDs, type names, field names, raw bytes for opaque blobs), and a `Format` function that renders any `Value` as readable text with built-in decoders for common opaque types such as `time.Time` and `big.Int`.

## Installation

```sh
go get github.com/codepuke/gobspect
```

Requires Go 1.26 or later.

## Quick start

Create an `Inspector` with `New`, wrap any `io.Reader` with `Stream`, then range over `Values` and print each decoded value with `Format`:

:::examples stream-values

`Values` yields one decoded value per top-level `Encode` call in the original stream, as it is read — the whole stream is never buffered, and an early `break` stops reading immediately. When you want everything as a slice instead, call `Collect` on the stream.

## Beyond decoding

The core package also provides:

- **Schema extraction** — `Stream.Schema` renders every type definition in a stream as Go-style type declarations, the fastest way to learn what an unknown file contains.
- **JSON output** — `ToJSON` and `ToJSONIndent` serialize any decoded value, with a `"kind"` discriminator on every node.
- **Stream statistics** — `Stream.Stats` profiles a stream: per-type record counts, byte consumption, field presence rates, and opaque decoder coverage.
- **Comparison** — `Equal`, `CompareValues`, and the configurable `Comparer` compare and order decoded values.
- **Custom opaque decoders** — `Inspector.RegisterDecoder` adds a `DecoderFunc` by type name to decode your own `GobEncoder` and `BinaryMarshaler` types.

Companion subpackages build on the core:

| Subpackage | Purpose |
|---|---|
| `query` | Path-based navigation of decoded value trees (`Orders.*.Customer.Name`) |
| `diff` | Structural diffing of two values or two streams |
| `sortval` | Sorting sequences of values by struct field keys |
| `tabular` | CSV/TSV output with header derivation and sparse-row alignment |
| `decompress` | Transparent decompression (gzip, zstd, xz, bzip2, zip) by magic-byte sniffing |
| `gq` | The query-run engine behind the gq command — pipeline, rendering, aggregation — as a library |

There is also **gq**, a jq-inspired command-line tool for inspecting gob streams from the terminal, no Go code required:

```sh
go install github.com/codepuke/gobspect/cmd/gq@latest
```

See the gq documentation pages for the flag reference and query syntax.

## Where to go next

- The API Guide page walks through the library in depth: the inspector, streams, the Value AST, formatting, and JSON.
- The Opaque Type Decoding page explains how `GobEncoder` and `BinaryMarshaler` blobs are decoded and which decoders ship built in.
- The Wire Format page is a full reference for the gob encoding itself.
- The exhaustive generated reference lives at [pkg.go.dev/github.com/codepuke/gobspect](https://pkg.go.dev/github.com/codepuke/gobspect).
