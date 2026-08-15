# gobspect/tabular

Package `tabular` writes [`gobspect.Value`](../README.md) nodes as CSV or TSV rows.

```
import "github.com/codepuke/gobspect/tabular"
```

## Overview

`tabular` is designed for presenting decoded gob streams as flat tabular data. It automatically derives a header row from the first struct's field definitions, aligns sparse gob rows to the canonical column order, and provides several strategies for handling streams that contain more than one struct type.

```go
stream := ins.Stream(r)

var buf bytes.Buffer
tp := tabular.NewPrinter(&buf,
    tabular.WithStream(stream),
    tabular.WithDelimiter(','),
)

for v, err := range stream.Values() {
    if err != nil { ... }
    if err := tp.WriteValue(v); err != nil { ... }
}

if err := tp.Flush(); err != nil { ... }

fmt.Print(buf.String())
// Name,Score
// alice,42
// bob,17
```

## Creating a Printer

```go
func NewPrinter(out io.Writer, opts ...Option) *Printer
```

Defaults: comma delimiter, headers enabled, `HeterogeneousFirstWins`, `BytesHex`, no byte truncation.

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithDelimiter(r rune)` | `','` | Column separator. Use `'\t'` for TSV. |
| `WithNoHeaders(b bool)` | `false` | Suppress the header row when true. |
| `WithHeterogeneousMode(m HeterogeneousMode)` | `HeterogeneousFirstWins` | Controls mixed-type rows. |
| `WithBytesFormat(f gobspect.BytesFormat)` | `gobspect.BytesHex` | Rendering format for `BytesValue` cells. |
| `WithMaxBytes(n int)` | `0` (no limit) | Truncate byte slices to at most n bytes before encoding. |
| `WithStream(s *gobspect.Stream)` | `nil` | Enables canonical column ordering via `stream.TypeByID`. |

### WithStream

When a `*gobspect.Stream` is provided, the printer looks up the canonical type definition for the first struct via `stream.TypeByID`. This ensures column order matches the original Go struct field declaration order, even when gob's sparse encoding omits zero-valued fields from some rows.

Without a stream, column order is derived from the fields present in the first row.

## Writing values

### WriteValue

```go
func (p *Printer) WriteValue(v gobspect.Value) error
```

Writes a single value as a tabular row. On the first call it emits the header row (unless `WithNoHeaders` was set).

- **Struct values**: each field becomes a column. Fields absent from a row (gob omits zero-valued fields) are emitted as empty strings so every row has the same column count as the header.
- **Scalar values**: a single-column table with header `"value"`.
- **Interface values**: unwrapped automatically before writing.

### Flush

```go
func (p *Printer) Flush() error
```

Flushes the underlying `csv.Writer` and returns any write error. Always call `Flush` after all rows have been written.

## Heterogeneous modes

When a gob stream contains structs of different types, `HeterogeneousMode` controls the behavior:

```go
type HeterogeneousMode int

const (
    HeterogeneousFirstWins  HeterogeneousMode = iota // silently drop rows of a different type (default)
    HeterogeneousReject                              // return an error on type change
    HeterogeneousUnion                               // one rectangular table with the union of all columns (buffered)
    HeterogeneousPartition                           // emit a blank line + new header on type change
)
```

### ParseHeterogeneousMode

```go
func ParseHeterogeneousMode(s string) (HeterogeneousMode, bool)
```

Converts `"first"`, `"reject"`, `"union"`, or `"partition"` (case-insensitive) to the corresponding constant. An empty string maps to `HeterogeneousFirstWins`. Returns `(HeterogeneousFirstWins, false)` for unrecognised values.

```go
mode, ok := tabular.ParseHeterogeneousMode("union")
if !ok { ... }
tp := tabular.NewPrinter(&buf, tabular.WithHeterogeneousMode(mode))
```

### union mode

`HeterogeneousUnion` emits one rectangular table whose header is the union of every row's columns, in first-appearance order. Each row is padded with empty cells for the columns its own type lacks. Because the full column set is not known until the final row, union mode buffers all rows and writes them on `Flush` (the other modes stream row-by-row).

```
Name,Score,Rank
alice,42,
bob,17,3
```

### partition mode

`HeterogeneousPartition` flushes, emits a blank line, then starts a new section with a fresh header whenever the struct type changes.

```
Name,Score
alice,42

Name,Level
dave,7
```

### reject mode

`HeterogeneousReject` returns an error immediately when a row of a different type arrives. The error message suggests using a field projection query to unify columns.

## Projection structs

When a value is a projection struct (produced by a `gobspect/query` projection expression like `.Name,Score`), the printer uses the struct's own field order and accepts values from any source type without triggering heterogeneous-type logic.

## CellString

```go
func CellString(v gobspect.Value) string
```

Converts a single `Value` to a flat string for a CSV cell. `BytesValue` is rendered as lowercase hex. Nested composite types (`StructValue`, `SliceValue`, `ArrayValue`, `MapValue`) produce descriptive placeholders: `"(struct)"`, `"(slice)"`, etc.

`CellString` does not respect `WithBytesFormat` or `WithMaxBytes` — it always uses hex. Use `Printer.WriteValue` for format-aware rendering.

| Type | Output |
|------|--------|
| `StringValue` | raw string value |
| `IntValue` / `UintValue` | decimal |
| `FloatValue` | `%g` notation |
| `ComplexValue` | `(real+imagi)` / `(real-imagi)` |
| `BoolValue` | `"true"` / `"false"` |
| `NilValue` | `""` (empty) |
| `BytesValue` | lowercase hex |
| `OpaqueValue` with decoded string | decoded string |
| `OpaqueValue` without decoded value | `"(opaque)"` |
| `InterfaceValue` | unwrapped and converted recursively |
| `StructValue` | `"(struct)"` |
| `SliceValue` | `"(slice)"` |
| `ArrayValue` | `"(array)"` |
| `MapValue` | `"(map)"` |
