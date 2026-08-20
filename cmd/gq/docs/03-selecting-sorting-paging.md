---
title: Selecting, sorting, and paging
---

A gob stream often holds many top-level values, and a query with wildcards or filters can produce many matches. This page covers the flags that narrow, order, and window that match set: `-index`, `-offset`, `-limit`, and the `-sort` family.

## Picking values by position

`-index N` prints only the Nth top-level value in the stream (0-based). The default, `-1`, prints all values.

```sh
# Print the very first value
gq -index 0 -f stream.gob

# The third value, as JSON
gq -index 2 -format json -f stream.gob
```

Positions follow **wire order** — the order in which values were written to the stream. That order is not guaranteed to be stable across re-encodings: if the producing program encodes from a Go map or another unordered source, the same data can arrive in a different order next time. Treat `-index` as a way to poke at a specific file, not as a durable identifier for a record.

## Pagination

`-offset` and `-limit` window the match set produced by the query:

- `-offset N` skips the first N matches.
- `-limit N` stops after N matches (0, the default, means no limit).

```sh
# First 10 results
gq -limit 10 -f data.gob .Orders.*

# Results 21–30
gq -offset 20 -limit 10 -f data.gob .Orders.*
```

Without `-sort`, pagination follows wire order, with the same stability caveat as `-index`. With `-sort`, `-offset` and `-limit` apply to the **sorted** result set, so pages are stable across invocations as long as the sort keys are unique (or the input is otherwise stable):

```sh
# Page 3 of results sorted by date (10 per page)
gq -sort Date -offset 20 -limit 10 -f data.gob .Orders.*
```

As a bonus, `-limit` without `-sort` lets gq stop reading the stream early — useful for a quick peek at a huge file.

## Sorting

`-sort` accepts a single column name or a comma-separated list, and sorts all matches before emitting output. Each entry may take a `:asc` or `:desc` suffix; entries without a suffix use the default direction, which is ascending unless `-sort-desc` is set.

```sh
# Sort orders by customer name
gq -sort Customer -f data.gob .Orders.*

# Multi-key sort: status then date, both descending
gq -sort Status,Date -sort-desc -f data.gob .Orders.*

# Mixed directions: status ascending, date descending
gq -sort Status,Date:desc -f data.gob .Orders.*
```

Sorting works with every output format and on match sets produced by both direct struct queries (`.Orders.*`) and field projections (`.Items.*.SKU,Price`).

Two more flags tune comparison behavior:

- `-sort-fold` makes string comparison case-insensitive, using Unicode case folding. This is not locale-aware — it folds characters by Unicode rules, not by any language's collation order.
- `-sort-drop-missing` excludes rows that are missing **all** sort keys. By default such rows are kept and sort as if the key were nil — the lowest rank, so they come first in ascending order and last in descending order.

```sh
# Case-insensitive, dropping records without a Name field
gq -sort Name -sort-fold -sort-drop-missing -f people.gob .People.*
```

Note that gob omits zero-valued fields on the wire, so a field that was encoded as `""` or `0` looks identical to one that was never set — both count as missing for `-sort-drop-missing`.

The three `-sort-*` flags are only meaningful alongside `-sort`; passing one without it is a usage error (exit 2).

## How mixed values compare

A sort key can land on values of different kinds — a field that holds a string in one record and a number in another. gq imposes a total ordering across kinds so the sort is always well-defined:

```
nil < bool < numeric < string < bytes < opaque < everything else
```

Within the numeric group, signed integers, unsigned integers, and floats compare by magnitude. Cross-kind numeric comparisons go through `float64`, so very large integers near the limits of `float64` precision (above roughly 2^53) may not compare exactly.

Composite values (structs, maps, slices, arrays) compare by their formatted text representation. This is a documented last resort to keep the ordering total — it is rarely a meaningful sort order, so pick a scalar column when you can.

## Sorting projected columns

Field projections can reach into nested structs with `/`: the projection `Address/Zip` pulls `Zip` out of the nested `Address` struct, and the resulting column is named after the **last** `/`-component. `-sort` matches on that column name:

```sh
# The projection Address/Zip produces a column named "Zip"
gq -sort Zip -f data.gob '.Items.*.SKU,Price,Address/Zip'
```

So the rule is: whatever name appears in the header row of csv/tsv output is the name `-sort` expects.

## Memory and large streams

`-sort` must see every match before it can emit the first row, so it **materializes the full match set in memory**. For most files this is fine; for very large streams it may not be. The streaming alternative is to leave `-sort` off and sort externally — for example, pipe csv output through `sort(1)`:

```sh
# Sort by the second column without holding the stream in memory
gq -format csv -no-headers -f data.gob .Orders.* | sort -t, -k2
```

`-offset`/`-limit` without `-sort` also stream, stopping early once the window is filled.

Sorting interacts with the `-hetero` table modes (described on the Output Formats page):

- **`-hetero partition`** — `-sort` sorts within each partition independently; the partitions themselves are emitted in the order each type first appears in the stream.
- **`-hetero union`** — `-sort` sorts across the unified row set. Rows from one type that lack a sort key contributed by another type's columns follow the usual missing-key rule: nil-ranked by default, excluded with `-sort-drop-missing`. Note that `union` buffers all rows anyway, so it adds no extra memory cost on top of `-sort`.
