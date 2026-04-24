# gobspect/sortval

Package `sortval` provides sorting utilities for sequences of [`gobspect.Value`](../README.md) nodes by struct field keys.

```
import "github.com/codepuke/gobspect/sortval"
```

## Overview

`sortval` is designed for pipelines that collect decoded values and need to present them in a user-specified field order. It handles multi-key sorting, case-insensitive comparison, descending order, and optional exclusion of rows that lack any of the sort fields.

```go
stream := ins.Stream(r)

// Collect all results, then sort by "Name" ascending.
spec, err := sortval.ParseSortSpec("Name", false, false, false)
if err != nil { ... }

var results []gobspect.Value
for v, err := range stream.Values() {
    if err != nil { ... }
    results = append(results, v)
}

sorted := sortval.SortMatches(sortval.SeqOf(results), spec)
for _, v := range sorted {
    fmt.Println(gobspect.Format(v))
}
```

## SortSpec

```go
type SortKey struct {
    Field string
    Desc  bool // true = descending for this key
}

type SortSpec struct {
    Keys        []SortKey // in priority order (first key is primary)
    Fold        bool      // case-insensitive string comparison
    DropMissing bool      // exclude rows missing ALL sort keys
}
```

Each key carries its own direction, so a single spec can mix ascending and descending. Build a `SortSpec` with `ParseSortSpec` or by constructing it directly.

### ParseSortSpec

```go
func ParseSortSpec(keysFlag string, defaultDesc, fold, dropMissing bool) (SortSpec, error)
```

`keysFlag` is a comma-separated list of field names. Each entry may include a direction suffix: `Name:asc` or `Score:desc` (case-insensitive). Entries without a suffix inherit `defaultDesc`. Returns an error if `keysFlag` is empty, an entry is empty, or a suffix is unrecognized.

```go
// Sort by "Name" ascending, then "Score" descending.
spec, err := sortval.ParseSortSpec("Name,Score:desc", false, false, false)

// Sort everything descending unless an explicit ":asc" suffix appears.
spec, err := sortval.ParseSortSpec("Score,Name", true, false, false)
```

### Compare

```go
func (s SortSpec) Compare(a, b gobspect.Value) int
```

Compares two values by the spec's `Keys` in priority order. Uses [`gobspect.CompareValues`](../README.md) for normal comparison or [`gobspect.CompareValuesFold`](../README.md) when `Fold` is true. Returns -1, 0, or +1. Each key's `Desc` flag is applied independently. Fields missing from a row produce a `NilValue{}` for comparison.

## SortMatches

```go
func SortMatches(matches iter.Seq[gobspect.Value], spec SortSpec) []gobspect.Value
```

Drains the sequence into a slice, optionally filters rows missing all sort keys (when `spec.DropMissing` is true), and sorts the result stably using `spec.Compare`. Returns the sorted slice.

```go
sorted := sortval.SortMatches(sortval.SeqOf(results), spec)
```

## SeqOf

```go
func SeqOf(vals []gobspect.Value) iter.Seq[gobspect.Value]
```

Converts a `[]gobspect.Value` slice into an `iter.Seq[gobspect.Value]`. Useful when you have already accumulated results and want to pass them to `SortMatches`.

## Field extraction

Sorting operates on struct fields by name. Non-struct values and structs that lack the requested field produce a `NilValue{}` for that key — they sort to the top in ascending order. `InterfaceValue` wrappers are unwrapped automatically before field lookup.

Fields that exist on some rows but not others are handled gracefully: present rows sort by the real value, absent rows produce `NilValue{}`.

## DropMissing

When `SortSpec.DropMissing` is true, `SortMatches` excludes any row that is missing **all** sort keys. A row is considered missing a key if the field is absent from the struct or the value is not a struct at all. If a row has at least one of the specified keys, it is kept.

```go
// Exclude rows where none of "Score" or "Rank" are present.
spec, _ := sortval.ParseSortSpec("Score,Rank", false, false, true)
sorted := sortval.SortMatches(sortval.SeqOf(results), spec)
```

## Value comparison

`sortval` delegates value comparison to `gobspect.CompareValues` and `gobspect.CompareValuesFold`. The ordering across kinds is:

| Kind | Notes |
|------|-------|
| `NilValue` | lowest |
| `BoolValue` | false < true |
| `IntValue` | numeric order |
| `UintValue` | numeric order |
| `FloatValue` | numeric order |
| `StringValue` | lexicographic (fold: case-insensitive) |
| `BytesValue` | lexicographic byte comparison |
| `OpaqueValue` | by formatted string |
| all others | by `gobspect.Format(v)` string |
