# gq

`gq` is a command-line tool for inspecting Go gob binary streams, in the spirit of jq. It decodes arbitrary `.gob` files without requiring the original type definitions and prints human-readable output.

## Installation

```
go install github.com/codepuke/gobspect/cmd/gq@latest
```

To build a local binary into the `dist/` folder from the repository root:

```sh
go build -o dist/gq ./cmd/gq
```

Requires Go 1.26 or later.

### Demo Data

To generate sample data to play with, compile and run the included `demo` tool:

```sh
go build -o dist/demo ./cmd/demo
./dist/demo dist/demo_data.gob

# See the embedded type declarations
./dist/gq -schema -f dist/demo_data.gob

# Print the very first order in the stream
./dist/gq -index 0 -f dist/demo_data.gob

# Extract just the first 10 customer names
./dist/gq -limit 10 -f dist/demo_data.gob .Customer

# Traverse into the line items and extract the first 10 prices over 30
./dist/gq -limit 10 -f dist/demo_data.gob '.Items[Price>30].Price'
```

## Usage

```
gq [flags] [query]
```

- **No arguments** — reads from stdin (or `-f`), prints all values.
- **One positional argument** — it is the query expression; input comes from `-f` or stdin.
- **Two or more positional arguments** — error.

Query expressions use dot-separated field names in the spirit of jq. A leading `.` is accepted and stripped (`.Field` and `Field` are equivalent). An empty expression — or no expression at all — matches the entire value.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `-file` | stdin | Input file path; if omitted, reads from stdin |
| `-format` | `pretty` | Output format: `pretty`, `json`, `jsonl`, `csv`, or `tsv` |
| `-schema` | false | Print type schema and exit |
| `-schema-format` | `go` | Schema output format: `go` (Go-style declarations) or `json` (machine-readable) |
| `-types` | false | Print raw type definitions as JSON and exit |
| `-stats` | false | Print stream-level statistics (message counts, per-type breakdown, field presence) and exit; pairs with `-format json` |
| `-index N` | -1 | Print only the Nth value (0-based); -1 = all |
| `-bytes` | `hex` | Byte slice rendering: `hex`, `base64`, or `literal` |
| `-max-bytes N` | 64 | Truncation limit for byte slices; 0 = no limit |
| `-color` | auto | Force color on |
| `-no-color` | auto | Force color off |
| `-r` | false | Raw string: omit quotes for string results; `pretty` only. Also applies to strings wrapped in an interface value (common for top-level `any` fields). |
| `-compact` | false | Compact JSON output (no indentation); `json` only |
| `-no-headers` | false | Suppress header row; `csv`/`tsv` only |
| `-null-on-miss` | false | Print `null` instead of exiting 1 when a path is not found |
| `-time-format` | RFC3339Nano | Go time layout for `time.Time` values |
| `-hetero` | `first` | Heterogeneous-type handling for `csv`/`tsv`: `first`, `reject`, `union`, or `partition` (see below) |
| `-limit N` | 0 | Stop after N results (0 = no limit) |
| `-offset N` | 0 | Skip the first N results |
| `-sort` | `""` | Comma-separated column names to sort by. Each entry may take a `:asc` or `:desc` suffix (e.g. `Name,Score:desc`) |
| `-sort-desc` | `false` | Default direction for entries without an explicit suffix |
| `-sort-fold` | `false` | Case-insensitive string comparison in sort |
| `-sort-drop-missing` | `false` | Exclude rows missing all sort keys |
| `-skip-errors` | `false` | Skip value messages that fail to decode and continue; type-def errors remain fatal |
| `-diff PATH` | `""` | Structural diff against another gob file, aligned by index; exits 1 when changes exist |
| `-count` | `false` | After filtering, print the number of matches and exit |
| `-sum PATH` | `""` | Sum a numeric path over the matches and exit |
| `-min PATH` | `""` | Minimum of a numeric path over the matches |
| `-max PATH` | `""` | Maximum of a numeric path over the matches |
| `-avg PATH` | `""` | Average of a numeric path over the matches |
| `-nonfinite` | `strings` | JSON rendering of non-finite floats (NaN, ±Inf): `strings` (`"NaN"`, `"+Inf"`, `"-Inf"`) or `null`; `json`/`jsonl` only |
| `-read-limit N` | 0 | Maximum decompressed bytes to read from the input; 0 = no limit. Errors out instead of decoding past the cap — set it when inspecting untrusted files (a small compressed input can expand enormously) |

Color is enabled automatically when stdout is a terminal and disabled when piping or redirecting.

`-count`/`-sum`/`-min`/`-max`/`-avg` are mutually exclusive and select an
aggregation mode; `-sum`/`-min`/`-max` keep full `int64`/`uint64` precision,
degrading to floating point only when a float value appears or an accumulator
overflows. Flags that would be ignored by the selected mode — for example
`-format csv` with `-stats`, or `-schema` combined with `-diff` — are rejected
with a usage error (exit 2) rather than silently dropped. `-h`/`-help` prints
usage to stdout and exits 0.

Compressed input — gzip, zstd, xz, bzip2, or a single-file zip archive — is
detected by content, not filename: a compressed file passed to `-f`/`-diff` is
decompressed regardless of its extension, matching the stdin behavior. One
compression layer is removed; `-read-limit` bounds the decompressed bytes read.

## Query syntax

Expressions are dot-separated path segments. The full syntax is defined by the [`query`](../../query/README.md) package.

| Expression | Meaning |
|------------|---------|
| `.` or `""` | Identity — the whole value |
| `.Field` | Struct field or string map key named `Field` |
| `.0` | First element of a slice or array |
| `.-1` | Last element |
| `.*` | All elements of a slice, array, or map |
| `..Field` | Recursive descent: find `Field` at any depth |
| `[Field!]` | Filter: keep elements where `Field` exists (`[Field!!]`: where it is absent) |
| `[Field=pattern]` | Filter: glob match (`*` and `?` wildcards); `[Field!=pattern]` negates |
| `[Field~pattern]` | Filter: `Field` is a slice/array/map containing a string matching the glob; `[Field!~pattern]` negates |
| `[Field==3.14]` | Filter: numeric/bool comparison (`==`, `<`, `>`, `<=`, `>=`) |
| `[F1=a]\|[F2=b]` | Filter: OR of multiple conditions |
| `A,B,C` | Field projection: returns a struct subset with only the named fields |

Filters and wildcards fan out: when a query matches multiple values, each is printed on its own line.

## Examples

```sh
# Print all values in a stream
gq -f data.gob

# Navigate to a nested field
gq -f data.gob .Header.Timestamp

# Pipe from stdin
cat data.gob | gq .Foo

# Print the Go-style type schema
gq -schema -f data.gob

# Output as JSON
gq -format json -f data.gob

# Export specific fields as CSV
gq -format csv -f data.gob .Items.*.SKU,Price

# Extract a string field without quotes
gq -r -f session.gob .Username

# Find all orders where Status is "shipped"
gq -f records.gob '.Orders.*[Status=shipped]'

# Inspect raw bytes as base64
gq -bytes base64 -f message.gob .Payload

# Compact JSON output
gq -format json -compact -f data.gob

# Only the first value from a multi-value stream
gq -index 0 -f stream.gob

# Export to TSV without headers
gq -format tsv -no-headers -f records.gob .Orders.*.ID,Customer,Total > orders.tsv

# .gz files (and gzipped stdin) are decompressed automatically
gq -schema -f data.gob.gz
cat data.gob.gz | gq -schema
```

## Output

### Pretty (default)

Structs render as indented field trees. Collections render inline when short, indented when long. Opaque types such as `time.Time` and `math/big.Int` are decoded and shown as readable values.

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

### JSON (`-format json`)

Each value is a discriminated-union object with a `"kind"` field:

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

Use `-compact` for single-line JSON.

### JSON Lines (`-format jsonl`)

One compact JSON value per line; ideal for piping into downstream tools like `jq`:

```sh
gq -format jsonl -f data.gob .Orders.* | jq 'select(.fields[].value.v == "Alice")'
```

The structure of each line is identical to the `-format json` object above, but collapsed onto a single line — no wrapping array, no indentation.

### Tabular (`-format csv` and `-format tsv`)

Tabular formats export a normalized grid. Column order follows the Go type definition for the first matched struct, so sparse gob instances (where zero fields are omitted on the wire) still produce correctly aligned rows.

```
$ gq -format csv -f data.gob '.Items.*'
SKU,Quantity,Price
A1,2,9.99
B5,1,4.50
```

Field projections (`.Field1,Field2`) define the column set explicitly:

```
$ gq -format csv -f data.gob '.Items.*.SKU,Price'
SKU,Price
A1,9.99
B5,4.50
```

**Heterogeneous struct types** — if the query matches structs of two different Go types, the `-hetero` flag controls the behavior (default: `first`):

| Mode | Behavior |
|------|----------|
| `first` | Silently skip rows whose type differs from the first row's type |
| `reject` | Return an error on any type mismatch |
| `union` | Emit one rectangular table with the union of all columns; every row gets empty cells for columns its type lacks. Rows are buffered until the stream ends (the other modes stream row-by-row) |
| `partition` | Emit a blank line and a new header when the type changes |

Field projections are always accepted regardless of source type, so `.ID,Customer` works across any struct that has those fields.

### Structural diff (`-diff`)

Compares two gob streams, aligning top-level values by index, and emits a per-position delta tree. Useful for spotting regressions between archived snapshots:

```sh
gq -diff snapshot-2025-01.gob -f snapshot-2025-02.gob
```

Exits 0 when the streams are identical, 1 when any position differs. Pair with `-format json` for a machine-readable delta document suitable for review tools.

Text output is colorized with ANSI escapes when stdout is a TTY — additions in green, removals in red, struct/map/slice headers in bold cyan, stream-position markers (`[N]`) dimmed. Use `-no-color` to force plain output (for piping into `less -R`-less readers or file redirects), or `-color` to force color on regardless of detection. `-format json` output is always plain.

### Aggregation (`-count`, `-sum`, `-min`, `-max`, `-avg`)

Reduce the match set to a single scalar and print it. The aggregator takes a path expression relative to each match (for `-sum`/`-min`/`-max`/`-avg`); `-count` needs no path.

```sh
gq -count -f orders.gob .Orders.*
gq -sum Price -f orders.gob .Items.*
gq -avg Score -f runs.gob '.Results.*[Status=ok]'
```

These are single-pass over the stream and never materialise the full match set in memory.

Gob omits zero-valued fields on the wire, so a match whose aggregation path is absent (an encoded zero) is skipped: `-avg` divides by the count of present values only, and `-min`/`-max` never see the omitted zeros.

### Statistics (`-stats`)

Summarises a stream without emitting individual values. Handy for sizing a file, spotting the dominant types, or seeing which struct fields are populated most often:

```
$ gq -stats -f data.gob
messages: 1507 (values: 1500, type defs: 7)
body bytes: 347289
opaque values: 2304 decoded, 0 undecoded

by type:
  Order                     1500 values    347012 bytes  (struct)
    Customer                 1500 (100.0%)
    Items                    1498 ( 99.9%)
    ...
```

Pair with `-format json` for machine-readable output suitable for aggregation:

```sh
gq -stats -format json -f data.gob | jq '.ByType[0]'
```

### Schema (`-schema`)

Renders the Go-style type declarations embedded in the stream:

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

Pass `-schema-format json` for a machine-readable rendering suitable for downstream tooling (code generators, documentation pipelines, compatibility checkers):

```sh
gq -schema -schema-format json -f data.gob
```

The output is a JSON array of type declarations with `name`, `kind`, and kind-specific keys (`fields`, `target`, `annotation`).

## Pagination

`-offset` and `-limit` control which results are returned from the full match set.

- `-offset N` skips the first N matches.
- `-limit N` stops after N matches (0 means no limit).

When `-sort` is not set, pagination follows **wire order** — the order in which values appear in the gob stream. This order is not guaranteed to be stable across re-encodings.

When `-sort` is set, `-offset` and `-limit` apply to the **sorted result set**, making pagination stable across pages as long as the sort keys are unique (or the input is otherwise stable). See [Sorting](#sorting) below.

```sh
# First 10 results
gq -limit 10 -f data.gob .Orders.*

# Results 21–30
gq -offset 20 -limit 10 -f data.gob .Orders.*
```

## Sorting

`-sort` accepts a single column name or a comma-separated list of column names and sorts all matches before emitting output. Sorting works on match sets produced by both direct struct queries (`.Orders.*`) and [projection queries](#tabular--format-csv-and--format-tsv) (`.Items.*.SKU,Price,Address/Zip`).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-sort` | `""` | Comma-separated column names to sort by. Each entry may take a `:asc` or `:desc` suffix (e.g. `Name,Score:desc`) |
| `-sort-desc` | `false` | Default direction for entries without an explicit suffix |
| `-sort-fold` | `false` | Case-insensitive string comparison in sort |
| `-sort-drop-missing` | `false` | Exclude rows missing all sort keys |

When `-sort-drop-missing` is false (the default), rows that are missing all sort keys sort as if the key were `nil` (lowest rank in ascending order). When true, those rows are excluded from output entirely.

When `-sort-fold` is true, string comparisons use Go's Unicode case-folding tables. This is not locale-aware.

### Kind-order total ordering

Values of different kinds are ordered as follows:

- `NilValue` < `BoolValue` < `IntValue`/`UintValue`/`FloatValue` (numeric) < `StringValue` < `BytesValue` < `OpaqueValue` < everything else

Within the numeric group, values are compared by magnitude (cross-kind comparisons use `float64`; very large integers near the limits of `float64` precision may not compare exactly). Composite types (struct, map, slice, array) are compared by their formatted string representation and are documented as a last resort — not meaningful for most inputs.

### Projection integration

When a query uses the `/`-depth projection syntax, the projected column name is the **last `/`-component**. For example, `.Items.*.SKU,Address/Zip` produces a column named `Zip`, so `-sort Zip` matches it:

```sh
gq -sort Zip .Items.*.SKU,Price,Address/Zip
```

See the [Tabular](#tabular--format-csv-and--format-tsv) section for more on field projections.

### Memory usage

`-sort` **materializes all matches in memory** before emitting any output. For very large streams this may be infeasible. In that case, omit `-sort` and sort externally — for example, pipe CSV output through `sort(1)`:

```sh
gq -format csv -f data.gob .Orders.* | sort -t, -k2
```

### Interaction with pagination

`-sort` combined with `-offset`/`-limit` paginates over **sorted** data. Without `-sort`, pagination follows wire order.

```sh
# Page 3 of results sorted by date (10 per page)
gq -sort Date -offset 20 -limit 10 .Orders.*
```

### Interaction with `-hetero`

- **`-hetero partition`**: `-sort` sorts within each partition independently; partitions are emitted in wire-arrival order of their type.
- **`-hetero union`**: `-sort` sorts across the unified row set. Rows from an earlier type that lack a sort key introduced by a later type's expanded columns follow the `-sort-drop-missing` rule.

### Examples

```sh
# Sort orders by customer name
gq -sort Customer .Orders.*

# Multi-key sort: status then date, descending
gq -sort Status,Date -sort-desc .Orders.*

# Mixed directions per-key: status ascending, date descending
gq -sort Status,Date:desc .Orders.*

# Paginate sorted results
gq -sort Date -offset 20 -limit 10 .Orders.*

# Sort projected columns (Address/Zip projects as column "Zip")
gq -sort Zip .Items.*.SKU,Price,Address/Zip

# Case-insensitive, drop records missing the sort field
gq -sort Name -sort-fold -sort-drop-missing .People.*
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success, or broken pipe (stdout closed early) |
| 1 | Decode error, write error, or path not found (without `-null-on-miss`) |
| 2 | Usage error: bad flags, invalid query expression, or too many arguments |
