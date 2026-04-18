# gobspect/query

Path-based navigation of decoded [`gobspect`](https://github.com/codepuke/gobspect) `Value` trees. Navigate nested structs, slices, and maps without writing type switches.

```
go get github.com/codepuke/gobspect/query
```

## Quick start

```go
root, _ := gobspect.New().Decode(r) // decode a gob stream

// Extract a single value
name, ok := query.Get(root[0], "Orders.0.Customer.Name")

// Collect all values matching a wildcard path
names := query.All(root[0], "Orders.*.Customer.Name")
```

`root` is a `[]gobspect.Value` — one entry per top-level value in the stream. `root[0]` is typically either a **struct** that contains a field like `Orders`, or a **slice** of records directly. Both shapes work:

```go
// Struct root: the encoded value is a struct with an Orders field
query.All(root[0], "Orders[Status=active]")       // navigate to Orders, then filter
query.All(root[0], "Orders.*.Customer.Name")       // navigate, expand, navigate

// Slice root: the encoded value is itself a slice of orders
query.All(root[0], "[Status=active]")              // filter the top-level slice directly
query.All(root[0], "*.Customer.Name")              // expand elements, then navigate
```

Every path feature composes with both shapes. `[Status=active].Items.*` on a slice root is equivalent to `Orders[Status=active].Items.*` on a struct root that has an `Orders` field.

```go
// Discover what keys are available at a node
keys, _ := query.Keys(root[0], "Orders.0")
// → ["ID", "Customer", "Items", "PlacedAt"]

// Panic-on-miss variant for tests and scripts
name = query.MustGet(root[0], "Orders.0.Customer.Name")
```

## Path syntax

Segments are separated by `.`.

| Segment | Navigates |
|---|---|
| `Name` | Named field of a struct |
| `0`, `42` | Non-negative integer index into a slice or array |
| `-1`, `-2` | Negative index: `-1` = last element, `-2` = second-to-last, etc. |
| `*` | All elements of a slice, array, or map (produces multiple results; use with `All`) |
| `key` | Entry in a `map[string]T` whose key equals `key` |
| `[Field!]` | Filter: keep only elements that have `Field` set |
| `[Field!!]` | Filter: keep only elements that do NOT have `Field` set |
| `[Field=pattern]` | Filter: keep only elements where `Field` is a string matching `pattern` |
| `[Field!=pattern]`| Filter: keep only elements where `Field` is a string NOT matching `pattern` |
| `[Field~pattern]` | Filter: keep only elements where `Field` is a slice/array/map containing a string matching `pattern` |
| `[Field!~pattern]`| Filter: keep only elements where `Field` is a slice/array/map NOT containing a string matching `pattern` |
| `[Field==value]`  | Filter: keep only elements where `Field` is a number equal to `value`, or a bool equal to `true`/`false` (also `<`, `>`, `<=`, `>=` for numbers) |
| `..Name` | Recursive descent: find all nodes named `Name` at any depth |
| `..[Filter]` | Wildcard recursive descent: traverse all depths, keep nodes matching `Filter` |
| `A,B,C` | Field projection: returns an anonymous struct containing only the requested fields (see [Field projection](#field-projection)) |

An empty path (`""`) resolves to the root value itself.

`InterfaceValue` nodes are unwrapped transparently: you do not need to add extra segments to step through an interface wrapper.

## Filter syntax

Filters appear inside `[…]` and narrow a slice, array, or map to only the elements that match.

### Quoting patterns

If a filter pattern contains any of the filter operator characters (`!`, `=`, `~`, `<`, `>`), you should enclose the pattern in double quotes `"` to ensure it is parsed correctly. Inner quotes can be escaped with `\"`.

```go
query.All(root, `Orders[Formula="a<b"]`)           // pattern contains <
query.All(root, `Orders[Status="done!"]`)          // pattern contains !
query.All(root, `Orders[Name="say \"hi\""]`)       // escaped quotes
```

### Existence filter `[Field!]`

Keeps elements where `Field` is present and was encoded on the wire. Because Go's `encoding/gob` omits zero-valued struct fields, a field that is present was necessarily non-zero when encoded.

```go
// Orders that have a Customer field set
query.All(root, "Orders[Customer!]")

// Line items from orders that have a discount set
query.All(root, "Orders[Discount!].*.Items")
```

### Equality filter `[Field=pattern]`

Keeps elements where `Field` is a string matching `pattern`. Only `string`-typed fields match; integer, bool, and other types are never matched by this filter.

Pattern matching follows `path.Match` glob semantics:

| Pattern | Matches |
|---|---|
| `active` | exactly `"active"` |
| `err*` | any string starting with `"err"` |
| `*_v2` | any string ending with `"_v2"` |
| `*foo*` | any string containing `"foo"` |
| `*` | any string, **including empty string** |
| `?*` | any **non-empty** string (one or more characters) |
| `ERR_?` | `"ERR_"` followed by exactly one character |

> **Tip:** To filter for fields that are present and non-empty, use `[Field=?*]` rather than `[Field=*]`. The `?` requires at least one character, so empty strings are excluded. This is especially useful for optional string fields that gob may encode as `""` when explicitly set to the zero value through an interface or pointer.

```go
query.All(root, "Orders[Status=active]")           // exact match
query.All(root, "Orders[Status=err*]")             // prefix match
query.All(root, "Orders[Status=?*].*.Customer")    // non-empty status only
```

### Contains filter `[Field~pattern]`

Keeps elements where `Field` is a slice, array, or map that contains at least one string entry matching `pattern`. Only `StringValue` elements (and map keys) are checked; non-string entries are silently skipped.

| Pattern | Matches elements containing… |
|---|---|
| `devops` | exactly `"devops"` |
| `prod*` | any string starting with `"prod"` |
| `*svc` | any string ending with `"svc"` |
| `*infra*` | any string containing `"infra"` |
| `*` | any string, **including empty string** |
| `?*` | any **non-empty** string |
| `ERR_?` | `"ERR_"` followed by exactly one character |

> **Note:** Use `~` for collection membership (the field is a slice/array/map). Use `=` when the field is a scalar string.

> **Tip:** `[Tags~?*]` requires at least one non-empty tag to be present in the `Tags` collection.

```go
// Resources whose Tags slice contains "devops"
query.All(root, "Resources[Tags~devops]")

// Resources tagged with any "prod-*" tag
query.All(root, "Resources[Tags~prod*]")

// Require at least one non-empty tag
query.All(root, "Resources[Tags~?*]")
```

### Numeric and bool comparison filter `[Field==value]`

Keeps elements where `Field` is a number or bool matching `value`. The `==`, `<`, `>`, `<=`, and `>=` operators are supported for numeric fields (`IntValue`, `UintValue`, `FloatValue`). Only `==` is supported for `BoolValue` fields.

| Pattern | Matches |
|---|---|
| `[Count==5]` | `Count` is exactly 5 |
| `[Price<100]` | `Price` is less than 100 |
| `[Price>=0]` | `Price` is zero or positive |
| `[Enabled==true]` | `Enabled` is `true` |
| `[Enabled==false]` | `Enabled` is `false` |

Bool literals are case-insensitive: `true`, `True`, `TRUE` all match `BoolValue{true}`. Any other word (e.g. `banana`) is a **parse-time error** — `Parse` returns a `*ParseError` and `All`/`Get`/`MustGet` panic.

```go
query.All(root, "Items[Count==5]")
query.All(root, "Items[Price<100]")
query.All(root, "Flags[Enabled==true]")   // bool field — exact match
query.All(root, "Flags[Enabled==TRUE]")   // case-insensitive — same result
```

> **Note:** Ordering operators (`<`, `>`, `<=`, `>=`) only work on numeric types. Applying them to a `BoolValue` field always returns no match.

## Recursive descent

The `..Name` and `..[Filter]` operators descend into the full subtree of the current node, at any depth, rather than navigating a single step.

### Named descent `..Name`

`..Name` visits every node in the subtree (depth-first, pre-order) and collects the ones named `Name`. The current node itself is also considered.

```go
// Find every Price field anywhere in a deep catalog struct
prices := query.All(root, "..Price")

// Chained descent: all Name fields within all Orders fields at any depth
names := query.All(root, "..Orders.*.Name")
```

`Get` returns the shallowest (first) match in depth-first pre-order. `All` returns every match.

Multiple descents compose: `..A..B` finds all `B` fields anywhere within all `A` fields found anywhere in the tree.

### Wildcard descent `..[Filter]`

`..[Filter]` visits every node in the subtree and keeps those matching `Filter`. This is useful for heterogeneous data where different types share a common field name.

```go
// Infrastructure inventory: Server, Laptop, and NetworkDevice records mixed in one slice
// Find all resources with Status == "active", regardless of their type
active := query.All(root, "Resources.*..[Status=active]")

// Narrow further: only resources tagged "devops" that are also active
tagged := query.All(root, "Resources.*..[Tags~devops][Status=active]")
```

> **Note:** `Get` returns the shallowest match; `All` returns all matches in depth-first pre-order.

> **Note:** Multiple descents compose. `..A..B` finds all `B` fields anywhere within all `A` fields anywhere in the tree.

## Field projection

You can extract multiple fields from a struct or map simultaneously using a comma-separated projection. The result is a synthetic, anonymous `StructValue` containing exactly the requested fields.

If a requested field does not exist in the source node, it is included in the output with a `NilValue`. This ensures that projected collections have a perfectly uniform shape, which is helpful when piping to tabular formatters like CSV.

```go
// Extract only SKU and Price from all items
query.All(root, "Items.*.SKU,Price")

// Extract fields from a single struct
query.Get(root, "Orders.0.Customer.Name,Email")
```

### Nested fields in projections

Use `/` as a path separator *within* a projection field to reach a nested struct. The column name in the output is the last component after the final `/`.

```go
// Items have a nested Address struct — pull Zip alongside flat fields
query.All(root, "Items.*.SKU,Price,Address/Zip")
// → each result: StructValue{SKU: …, Price: …, Zip: …}

// Three levels deep
query.All(root, "Items.*.ID,Shipping/Address/Zip")
// → each result: StructValue{ID: …, Zip: …}
```

`/` is only meaningful inside a projection segment (i.e. when a comma is also present). A bare token like `Address/Zip` with no comma is treated as a literal field name, not a nested path — use `Address.Zip` for regular single-value navigation.

Two projection fields that resolve to the same leaf name are a **parse-time error**:

```go
query.All(root, "Billing/Zip,Shipping/Zip")  // error: duplicate column "Zip"
```

## Map key navigation

Map navigation matches path segments (and filter field names) against map keys that are `StringValue` — i.e., maps declared as `map[string]T`. Maps with non-string keys (`map[int]T`, `map[uint64]T`, etc.) cannot be navigated by path: entries with non-string keys are silently skipped.

Numeric-looking map keys (for `map[string]T` with `"42"` as a key) ARE navigable — integer-looking segments fall back to string key lookup when the node is a map.

```go
// map[string]Address — works
query.Get(root, "AddressBook.home.Street")

// map[string]T where keys are "42", "100" — also works
query.Get(root, "AddressBook.42.Street")

// map[int]Order — index 0 is NOT navigable; use All + filter on a string field instead
```

## API reference

### One-off (string) functions

These accept a path expression as a plain string and panic if the expression is syntactically invalid. Use them in scripts, tests, and one-shot exploration where the path is a literal.

```go
func Get(root gobspect.Value, expr string) (gobspect.Value, bool)
func MustGet(root gobspect.Value, expr string) gobspect.Value
func All(root gobspect.Value, expr string) []gobspect.Value
func AllSeq(root gobspect.Value, expr string) iter.Seq[gobspect.Value]
func Keys(root gobspect.Value, expr string) ([]string, bool)
```

`Get` returns `(nil, false)` when the path does not resolve. `MustGet` panics with a message identifying the full expression and the failing segment. `All` returns `nil` (not an empty slice) when nothing matches. `AllSeq` is the lazy iterator variant — use it for early-break or streaming scenarios.

### Pre-compiled path functions

Use these when evaluating the same path against many roots, or when you need to handle syntax errors explicitly.

```go
func Parse(expr string) (Path, error)
func GetPath(root gobspect.Value, p Path) (gobspect.Value, bool)
func AllPath(root gobspect.Value, p Path) []gobspect.Value
func AllPathSeq(root gobspect.Value, p Path) iter.Seq[gobspect.Value]
func KeysPath(root gobspect.Value, p Path) ([]string, bool)
```

```go
p, err := query.Parse("Orders.*.Customer[Name=?*].Address")
if err != nil {
    log.Fatal(err)
}
for _, root := range roots {
    addrs := query.AllPath(root, p)
    // ...
}
```

`AllPathSeq` returns a lazy iterator; use it when you may break early or want to stream results without accumulating a slice:

```go
for addr := range query.AllPathSeq(root, p) {
    fmt.Println(addr)
    // break here is safe — iteration stops immediately
}
```

The one-off string counterpart is `AllSeq(root, expr)`, which panics on invalid expressions just like `All`.

### `Keys` for exploration

`Keys` returns the navigable keys at a given path — useful when you do not know the schema ahead of time.

```go
keys, ok := query.Keys(root, "")
// StructValue root → ["Orders", "Meta", "CreatedAt"]

keys, ok = query.Keys(root, "Orders")
// SliceValue → ["0", "1", "2"]

keys, ok = query.Keys(root, "Orders.0")
// StructValue → ["ID", "Customer", "Items", "PlacedAt"]

keys, ok = query.Keys(root, "Meta")
// MapValue (map[string]string) → ["env", "region", "version"]
```

Returns `(nil, false)` for scalar, opaque, and nil nodes that have no navigable children.
