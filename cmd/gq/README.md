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

To generate sample data to play with, you can compile and run the included `demo` tool:

```sh
go build -o dist/demo ./cmd/demo
./dist/demo dist/demo_data.gob

# Test it out!

# See the embedded type declarations
./dist/gq --schema dist/demo_data.gob

# Print the very first order in the stream
./dist/gq --index 0 dist/demo_data.gob

# Extract just the customer names
./dist/gq '.Customer' dist/demo_data.gob | head -n 10

# Traverse into the line items to extract prices over 30
./dist/gq '.Items[Price>30].Price' dist/demo_data.gob | head -n 10
```

## Usage

```
gq [flags] [query] [file]
```

- **No arguments** — reads from stdin, prints all values.
- **One argument** — treated as a file path if it exists on disk; otherwise treated as a query expression on stdin.
- **Two arguments** — first is the query expression, second is the file.

Query expressions use dot-separated field names, identical to how jq addresses JSON. A leading `.` is accepted and stripped for jq compatibility (`.Field` and `Field` are equivalent). The identity expression `.` or an empty expression matches the entire value.

## Quick start

```sh
# Print all values in a stream
gq data.gob

# Navigate to a nested field
gq .Header.Timestamp data.gob

# Pipe from stdin
cat data.gob | gq .Items.0.Name

# Print the Go-style type schema embedded in the stream
gq --schema data.gob

# Output as JSON
gq --format json data.gob

# Export specific fields as a CSV
gq --format csv .Items.*.SKU,Price data.gob
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `pretty` | Output format: `pretty`, `json`, `csv`, or `tsv` |
| `--no-headers` | false | Suppress header row in `csv` and `tsv` output |
| `--schema` | false | Print Go-style type schema from the stream and exit |
| `--types` | false | Print type definitions as JSON and exit |
| `--index N` | -1 | Print only the Nth value (0-based); -1 prints all |
| `--bytes` | `hex` | Byte slice rendering: `hex`, `base64`, or `literal` |
| `--max-bytes N` | 64 | Truncation limit for byte slices; 0 = no limit |
| `--color` | auto | Force color on |
| `--no-color` | auto | Force color off |
| `-r` | false | Raw string: for string results, omit surrounding quotes |
| `--compact` | false | Compact JSON output (no indentation) |
| `--null-on-miss` | false | Print `null` instead of exiting 1 when a path is not found |
| `--time-format` | RFC3339Nano | Go time layout for `time.Time` values |

Color is enabled automatically when stdout is a terminal and disabled when piping or redirecting.

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
| `[Field!]` | Filter: keep elements where `Field` exists |
| `[Field=pattern]` | Filter: glob match (`*` and `?` wildcards) |
| `[Field~text]` | Filter: substring match |
| `[Field==3.14]` | Filter: numeric equality |
| `[F1=a]\|[F2=b]` | Filter: OR of multiple conditions |
| `A,B,C` | Field projection: returns an anonymous struct subset with only the requested fields |

Filters and wildcards fan out: when a query matches multiple values, each is printed on its own line.

## Examples

```sh
# Schema of an unknown file
gq --schema unknown.gob

# Extract a string field, stripping quotes
gq -r .Username session.gob

# Find all Order IDs in a stream of records
gq .Orders.*.ID records.gob

# Get the third order
gq .Orders.2 records.gob

# Find all orders where Status is "shipped"
gq '.Orders.*[Status=shipped]' records.gob

# Inspect raw bytes as base64
gq --bytes base64 .Payload message.gob

# Full JSON output, compact
gq --format json --compact data.gob

# Only the first value from a multi-value stream
gq --index 0 stream.gob

# Export selected fields to a TSV file without headers
gq --format tsv --no-headers .Orders.*.ID,Customer,Total records.gob > orders.tsv

# Decompress on the fly (shell pipeline)
zcat data.gob.gz | gq --schema
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

### JSON (`--format json`)

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

### Tabular (`--format csv` and `--format tsv`)

Tabular formats are designed to work alongside field projections (`.Field1,Field2`) to export a normalized grid of data. 

```
$ gq --format csv '.Items.*.SKU,Quantity,Price' data.gob
SKU,Quantity,Price
A1,2,9.99
B5,1,4.50
```

Headers are automatically sourced from the fields of the first matched struct (can be suppressed with `--no-headers`). Complex nested data types are omitted from cells and replaced with descriptive placeholders like `(struct)`, `(array)`, `(map)`, or `(opaque)`.

### Schema (`--schema`)

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

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Decode error, or path not found (and `--null-on-miss` not set) |
| 2 | Usage error: bad flags, invalid query expression, or too many arguments |
