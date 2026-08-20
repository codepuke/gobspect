---
title: "gq: a jq for gob streams"
---

## What gq is

`gq` is a command-line tool for inspecting Go gob binary streams, in the spirit of jq. It decodes arbitrary `.gob` files **without requiring the original Go type definitions** — the gob wire format embeds enough type information to reconstruct every value, and `gq` reads that directly.

It is strictly decode-only: `gq` never writes or re-encodes gob data. What it does do:

- Dump any gob stream as human-readable, colorized output.
- Navigate and filter values with jq-style query expressions (`.Orders.*[Status=shipped].Customer`).
- Export to JSON, JSON Lines, CSV, or TSV for downstream tools.
- Recover the Go-style type schema embedded in a stream, summarize stream statistics, aggregate numeric fields, and diff two streams structurally.

Opaque types that gob encodes as binary blobs — `time.Time`, `math/big.Int`, UUIDs, decimals — are recognized and rendered as readable values, not hex dumps.

## Installation

```sh
go install github.com/codepuke/gobspect/cmd/gq@latest
```

Requires Go 1.26 or later to build. The result is a single static binary; the machine you run it on needs nothing else.

## First look at a file

Point `gq` at a file with `-f` (or pipe the stream into stdin) and it prints every value in the stream as an indented tree:

```sh
gq -f data.gob
```

```
Order{
  Customer: "Alice"
  ID: 42
  Items: []LineItem{
    LineItem{
      SKU: "A1"
      Quantity: 2
      Price: 9.99
    }
  }
  PlacedAt: 2024-03-01T12:00:00Z
}
```

Before diving into values, it often pays to look at the shape of the data. `-schema` renders the Go-style type declarations embedded in the stream:

```sh
gq -schema -f data.gob
```

```
type LineItem struct {
  Price     Decimal  // GobEncoder
  Quantity  int
  SKU       string
}

type Order struct {
  Customer  string
  ID        uint
  Items     []LineItem
  PlacedAt  Time  // GobEncoder
}
```

And `-stats` sizes the stream without printing individual values — message and byte counts, the dominant types, and how often each struct field is actually populated:

```sh
gq -stats -f data.gob
```

```
messages: 1507 (values: 1500, type defs: 7)
body bytes: 347289
opaque values: 2304 decoded, 0 undecoded

by type:
  Order                     1500 values    347012 bytes  (struct)
    Customer                 1500 (100.0%)
    Items                    1498 ( 99.9%)
    ...
```

The Schema, Stats, Aggregation, Diff page covers these modes in depth.

## A taste of queries

The optional positional argument is a query expression — dot-separated path segments in the spirit of jq:

```sh
# Navigate to a nested field
gq -f data.gob .Header.Timestamp

# Fan out over a collection: one result per line
gq -f data.gob '.Orders.*.Customer'

# Filter: all orders where Status is "shipped"
gq -f records.gob '.Orders.*[Status=shipped]'

# Project fields and export as CSV
gq -format csv -f data.gob '.Items.*.SKU,Price'
```

Quote expressions containing `*`, `[`, or `!` so the shell passes them through untouched. The full syntax — wildcards, recursive descent, filters, projections — is on the Query Language page, and the output side (`-format`, colors, tabular options) is on the Output Formats page.

## Compressed input

Compressed input — gzip, zstd, xz, bzip2, or a single-file zip archive — is detected by content, not filename. A compressed file passed to `-f` or `-diff` is decompressed automatically regardless of its extension, and the same applies to stdin:

```sh
gq -schema -f data.gob.gz
cat data.gob.gz | gq -schema
```

One compression layer is removed; a gzipped file inside a zip is not unwrapped twice.

When inspecting untrusted files, set `-read-limit N` to cap the number of decompressed bytes read — a small compressed input can expand enormously (a "zip bomb"). `gq` errors out instead of decoding past the cap; the default of 0 means no limit.

```sh
gq -read-limit 104857600 -f untrusted.gob.zst
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success, or broken pipe (stdout closed early, e.g. by `head`) |
| 1 | Decode error, write error, or path not found (without `-null-on-miss`) |
| 2 | Usage error: bad flags, invalid query expression, or too many arguments |

`-diff` also uses the exit code as a signal: 0 when the two streams are identical, 1 when any position differs. With `-null-on-miss`, a query that matches nothing prints `null` and exits 0 instead of 1 — see the Query Language page.
