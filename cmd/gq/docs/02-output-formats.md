---
title: Output formats
---

gq can render decoded gob values as a human-readable tree, as JSON for scripting, or as a table for spreadsheets and data pipelines. This page covers the `-format` flag, what each format looks like, and the rendering knobs that apply across formats.

## Choosing a format

The `-format` flag selects one of five output formats:

| Format | Best for |
|--------|----------|
| `pretty` (default) | Reading in a terminal |
| `json` | Scripting, machine consumption |
| `jsonl` | Streaming one value per line into tools like `jq` |
| `csv` | Spreadsheets, data import |
| `tsv` | Tab-separated variant of csv |

```sh
gq -f data.gob                     # pretty, the default
gq -format json -f data.gob
gq -format jsonl -f data.gob .Orders.*
gq -format csv -f data.gob '.Items.*'
```

Some flags only make sense with a particular format, and gq rejects combinations where a flag would be silently ignored — you get a usage error (exit 2) instead of surprising output. For example, `-compact` outside `-format json` is an error (JSON Lines is always compact, so `-compact` with `jsonl` is rejected too), `-r` requires `pretty`, and `-nonfinite` requires `json` or `jsonl`.

`-format` itself is restricted in the special modes:

- **Diff mode** (`-diff`) supports `pretty`, `json`, and `jsonl`.
- **Stats mode** (`-stats`) supports `pretty` and `json`.
- **Schema, types, and aggregation modes** produce their own fixed output and reject `-format` entirely (schema output is controlled by `-schema-format` instead).

## Pretty output

The default format renders structs as indented field trees. Collections render inline when short and indented when long. Opaque types such as `time.Time` and `math/big.Int` are decoded and shown as readable values rather than raw bytes:

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

### Raw strings

When a query lands on a string, pretty output quotes it. Pass `-r` to omit the quotes — useful when feeding the value to another command:

```sh
gq -r -f session.gob .Username
```

`-r` also applies to strings wrapped in an interface value, which is common for top-level `any` fields. It is only meaningful with `pretty` output; combining it with another format is a usage error.

### Color

Pretty output is colorized with ANSI escapes when stdout is a terminal, and plain when piping or redirecting. Override the detection with `-color` (force on) or `-no-color` (force off):

```sh
gq -f data.gob | less -R          # piped: plain by default
gq -color -f data.gob | less -R   # force color through the pipe
```

`-color` and `-no-color` are mutually exclusive.

## JSON and JSON Lines

`-format json` renders each value as a discriminated-union object with a `"kind"` field, so downstream tools can tell a string from a number from a struct without guessing:

```json
{
  "kind": "struct",
  "typeName": "Order",
  "typeId": 64,
  "fields": [
    {"name": "Customer", "value": {"kind": "string", "v": "Alice"}},
    {"name": "ID",       "value": {"kind": "uint",   "v": 42}}
  ]
}
```

Pass `-compact` for single-line JSON with no indentation:

```sh
gq -format json -compact -f data.gob
```

`-format jsonl` emits one compact JSON value per line — the same object structure as `json`, but collapsed onto a single line with no wrapping array. It is ideal for piping into `jq`:

```sh
gq -format jsonl -f data.gob .Orders.* | jq 'select(.fields[].value.v == "Alice")'
```

### Non-finite floats

JSON has no literal for NaN or infinity. The `-nonfinite` flag controls how gq renders them in `json` and `jsonl` output:

- `strings` (default) — `"NaN"`, `"+Inf"`, `"-Inf"`
- `null` — every non-finite float becomes `null`

```sh
gq -format json -nonfinite null -f measurements.gob
```

## CSV and TSV

`-format csv` and `-format tsv` export a normalized grid. Column order follows the type definition embedded in the stream for the first matched struct — not the order fields happen to appear on the wire. Gob omits zero-valued fields when encoding, so individual values are often sparse; gq still produces correctly aligned rows, filling omitted fields with empty cells.

```sh
gq -format csv -f data.gob '.Items.*'
```

```
SKU,Quantity,Price
A1,2,9.99
B5,1,4.50
```

Field projections (`.Field1,Field2`) define the column set explicitly:

```sh
gq -format csv -f data.gob '.Items.*.SKU,Price'
```

```
SKU,Price
A1,9.99
B5,4.50
```

Suppress the header row with `-no-headers` — handy when appending to an existing file or feeding tools that expect data only:

```sh
gq -format tsv -no-headers -f records.gob .Orders.*.ID,Customer,Total > orders.tsv
```

## Heterogeneous types in tables

A table needs a single column set, but a query can match structs of more than one type. The `-hetero` flag controls what happens then (default: `first`):

| Mode | Behavior |
|------|----------|
| `first` | Silently skip rows whose type differs from the first row's type |
| `reject` | Return an error on any type mismatch |
| `union` | Emit one rectangular table with the union of all columns; every row gets empty cells for columns its type lacks |
| `partition` | Emit a blank line and a new header row whenever the type changes |

```sh
gq -format csv -hetero union -f mixed.gob '.*'
```

There is a streaming trade-off: `first`, `reject`, and `partition` stream row-by-row, while `union` must buffer all rows until the stream ends — it cannot know the full column set before seeing every type. For very large streams, prefer a streaming mode or a projection.

Field projections are always accepted regardless of source type, so `.ID,Customer` works across any struct that has those fields — often the simplest way to get one clean table out of a mixed stream.

## Rendering knobs

These flags tune how individual values are rendered, across formats.

### Byte slices

`-bytes` selects the rendering for `[]byte` values:

- `hex` (default) — hexadecimal digits
- `base64` — standard base64
- `literal` — raw bytes, printed as-is

```sh
gq -bytes base64 -f message.gob .Payload
```

By default byte slices are truncated after 64 bytes; `-max-bytes N` changes the limit, and `-max-bytes 0` disables truncation entirely:

```sh
gq -bytes hex -max-bytes 0 -f message.gob .Payload
```

### Time values

`-time-format` sets the layout for decoded `time.Time` values, using Go's reference-time layout syntax where the layout is written as how the moment `Mon Jan 2 15:04:05 2006 MST` would appear. The default is RFC 3339 with nanoseconds (`2006-01-02T15:04:05.999999999Z07:00`).

```sh
gq -time-format '2006-01-02 15:04' -f data.gob .Orders.*.PlacedAt
gq -time-format 'Jan 2, 2006' -f data.gob .Orders.*.PlacedAt
```

See the [Go time package documentation](https://pkg.go.dev/time#pkg-constants) for the full layout reference.
