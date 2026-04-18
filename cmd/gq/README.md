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
| `-format` | `pretty` | Output format: `pretty`, `json`, `csv`, or `tsv` |
| `-schema` | false | Print Go-style type schema and exit |
| `-types` | false | Print type definitions as JSON and exit |
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

# Decompress on the fly (shell pipeline)
zcat data.gob.gz | gq -schema
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
| `union` | Grow the header when new columns appear; earlier rows get empty cells for the new columns |
| `partition` | Emit a blank line and a new header when the type changes |

Field projections are always accepted regardless of source type, so `.ID,Customer` works across any struct that has those fields.

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

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success, or broken pipe (stdout closed early) |
| 1 | Decode error, write error, or path not found (without `-null-on-miss`) |
| 2 | Usage error: bad flags, invalid query expression, or too many arguments |
