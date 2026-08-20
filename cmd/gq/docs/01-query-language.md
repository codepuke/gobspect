---
title: Query language
---

## Anatomy of a query

A query is the single optional positional argument to `gq`:

```sh
gq [flags] [query]
```

- **No positional argument** — every value in the stream is printed.
- **One positional argument** — it is the query expression; input still comes from `-f` or stdin.
- **Two or more positional arguments** — a usage error (exit 2).

Expressions are dot-separated path segments. A leading `.` is accepted and stripped, so `.Field` and `Field` are equivalent; write whichever you prefer. An empty expression — or no expression at all — matches the entire value.

A gob stream may contain several top-level values, and the query is applied to each of them in turn. The top-level value is typically either a struct (navigate into its fields: `.Orders.*`) or itself a slice of records (start with `*` or a filter directly: `'.*[Status=active]'`).

Quote any expression containing `*`, `[`, `]`, `!`, `~`, `<`, `>`, or `|` — otherwise the shell will glob, history-expand, or redirect before `gq` ever sees the query. Single quotes are the safe default:

```sh
gq -f records.gob '.Orders.*[Status=shipped].Customer'
```

At a glance:

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
| `[Field~pattern]` | Filter: `Field` is a collection containing a string matching the glob; `[Field!~pattern]` negates |
| `[Field==3.14]` | Filter: numeric/bool comparison (`==`, `<`, `>`, `<=`, `>=`) |
| `[F1=a]\|[F2=b]` | Filter: OR of multiple conditions |
| `A,B,C` | Field projection: a struct subset with only the named fields |

The rest of this page walks through each of these.

## Field and index navigation

`.Field` steps into a named struct field. Chain segments to walk deeper:

```sh
gq -f data.gob .Order.Customer.Name
```

The same segment syntax navigates **string-keyed maps**: `.AddressBook.home` looks up the `"home"` key of a `map[string]Address`. Map keys that merely look numeric still work — `.AddressBook.42` finds the `"42"` key, because integer-looking segments fall back to string key lookup when the node is a map. Maps with non-string keys (`map[int]T` and the like) cannot be navigated by path; use a filter on the values instead. Values wrapped in an interface (`any` fields) are unwrapped transparently — no extra segment needed to step through the wrapper.

Slices and arrays are indexed numerically. Non-negative indices count from the front; negative indices count from the back:

```sh
gq -f data.gob .Orders.0        # first order
gq -f data.gob .Orders.-1       # last order
gq -f data.gob .Orders.-2       # second-to-last
```

`.*` expands to **all** elements of a slice, array, or map. This is a fan-out: when a query matches multiple values, each result is printed on its own line (or as its own row in tabular formats):

```sh
gq -f data.gob '.Orders.*.Customer'
```

Fan-out segments compose — `.Orders.*.Items.*.SKU` yields every SKU of every item of every order. The match set that fan-outs produce is what the `-sort`, `-offset`, `-limit`, and aggregation flags operate on (see the Selecting, Sorting, and Paging page).

## Recursive descent

`..Field` searches the entire subtree of the current node — at any depth — and collects every node named `Field`, in depth-first order:

```sh
# Every Price field anywhere in a deeply nested catalog
gq -f catalog.gob '..Price'

# Descend, then keep navigating: all Name fields inside any Orders field
gq -f data.gob '..Orders.*.Name'
```

Multiple descents compose: `..A..B` finds all `B` fields anywhere within all `A` fields found anywhere in the tree.

`..[Filter]` is the wildcard form: it visits every node in the subtree and keeps the ones matching the filter, regardless of their name or type. This shines with heterogeneous data, where different record types share a field name:

```sh
# Servers, laptops, and network devices mixed in one slice —
# find everything with Status "active", whatever its type
gq -f inventory.gob '.Resources.*..[Status=active]'

# Chain filters to narrow further
gq -f inventory.gob '.Resources.*..[Tags~devops][Status=active]'
```

## Filters

Filters appear inside `[…]` after a segment and narrow a slice, array, or map to the elements that match. Several filters in a row AND together; `|` between bracketed filters ORs them.

**Existence and absence.** `[Field!]` keeps elements where `Field` was encoded on the wire; `[Field!!]` keeps elements where it was not. Go's gob format omits zero-valued struct fields, so "present" means the field was non-zero when encoded:

```sh
# Orders that have a Discount set
gq -f data.gob '.Orders[Discount!]'

# Orders with no Discount at all
gq -f data.gob '.Orders[Discount!!]'
```

**Glob equality.** `[Field=pattern]` keeps elements where `Field` is a **string** matching the glob pattern; other field types never match. `[Field!=pattern]` negates. `*` matches any run of characters (including none) and `?` matches exactly one:

| Pattern | Matches |
|---------|---------|
| `active` | exactly `"active"` |
| `err*` | any string starting with `"err"` |
| `*_v2` | any string ending with `"_v2"` |
| `*foo*` | any string containing `"foo"` |
| `*` | any string, **including the empty string** |
| `?*` | any **non-empty** string |
| `ERR_?` | `"ERR_"` followed by exactly one character |

```sh
gq -f data.gob '.Orders[Status=active]'      # exact match
gq -f data.gob '.Orders[Status=err*]'        # prefix match
gq -f data.gob '.Orders[Status=?*]'          # present AND non-empty
```

Prefer `[Field=?*]` over `[Field=*]` when you want "non-empty": the `?` requires at least one character.

**Contains.** `[Field~pattern]` keeps elements where `Field` is a slice, array, or map containing at least one string entry matching the glob; `[Field!~pattern]` negates. Use `~` when the field is a collection, `=` when it is a scalar string:

```sh
# Resources whose Tags slice contains "devops"
gq -f inventory.gob '.Resources[Tags~devops]'

# Tagged with any "prod-*" tag
gq -f inventory.gob '.Resources[Tags~prod*]'
```

**Numeric and bool comparison.** `[Field==value]` compares numbers and bools; `<`, `>`, `<=`, `>=` additionally work for numbers (never for bools):

```sh
gq -f data.gob '.Items[Count==5]'
gq -f data.gob '.Items[Price<100]'
gq -f data.gob '.Items[Price>=0.5]'
gq -f data.gob '.Flags[Enabled==true]'
```

Bool literals are case-insensitive (`true`, `True`, `TRUE`); any other word after `==` on a bool is a query syntax error (exit 2). Integer comparisons are exact across the full 64-bit signed and unsigned ranges; floating-point comparison is used only when the field is a float or the literal has float syntax (a `.` or an exponent).

**OR-ing conditions.** Join bracketed filters with `|` for a logical OR:

```sh
gq -f data.gob '.Orders[Status=shipped]|[Status=delivered]'
```

**Quoting patterns.** If a pattern contains a filter operator character (`!`, `=`, `~`, `<`, `>`), enclose it in double quotes inside the expression so the query parser reads it literally; escape inner quotes with `\"`. Combined with the shell's single quotes:

```sh
gq -f data.gob '.Orders[Formula="a<b"]'
gq -f data.gob '.Orders[Status="done!"]'
gq -f data.gob '.Orders[Name="say \"hi\""]'
```

## Field projection

A comma-separated segment like `A,B,C` projects a struct or map down to just the named fields. The result is a synthetic struct containing exactly those fields, in the order you name them:

```sh
gq -f data.gob '.Items.*.SKU,Price'
```

If a requested field does not exist in a particular record, it is included with a `nil` value rather than dropped. Projected collections therefore have a perfectly uniform shape, which is exactly what tabular output wants — in `-format csv`/`-format tsv`, the projection defines the column set:

```sh
gq -format csv -f data.gob '.Items.*.SKU,Price'
```

```
SKU,Price
A1,9.99
B5,4.50
```

**Nested fields** are reached with `/` inside a projection component. The output column is named after the last `/`-component:

```sh
# Pull the nested Address.Zip up alongside flat fields — column name "Zip"
gq -format csv -f data.gob '.Items.*.SKU,Price,Address/Zip'

# Three levels deep — column name "Zip"
gq -format csv -f data.gob '.Items.*.ID,Shipping/Address/Zip'
```

`/` is only meaningful inside a projection (when a comma is present). A bare `Address/Zip` with no comma is treated as a literal field name — use `Address.Zip` for ordinary single-value navigation. Two projection components resolving to the same leaf name (`Billing/Zip,Shipping/Zip`) are a syntax error, since the columns would collide.

Projections also work across heterogeneous struct types: `.ID,Customer` produces a row for any struct that has those fields, whatever its type name.

## Misses and null

When a query matches nothing — the path names a field that does not exist, an index out of range, a filter no element passes — `gq` prints a `path not found` error to stderr and **exits 1**. That makes misses easy to detect in scripts:

```sh
gq -f data.gob .Order.Discount || echo "no discount"
```

Pass `-null-on-miss` to treat a miss as data instead of failure: `gq` prints `null` and exits 0. This is the right mode when the output feeds a pipeline that expects a value either way:

```sh
gq -null-on-miss -format json -f data.gob .Order.Discount | jq .
```

Note the distinction from filters: a fan-out that matches *some* elements is a success even if it filtered most of them away. Exit 1 means the query produced no results at all.
