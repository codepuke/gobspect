---
title: "Schema, stats, aggregation, diff"
---

Beyond printing values, `gq` has a set of special modes that answer questions *about* a stream: what types it contains (`-schema`, `-types`), how big it is and what dominates it (`-stats`), a single number computed over the matches (`-count`, `-sum`, `-min`, `-max`, `-avg`), and how it differs from another stream (`-diff`). This page covers each in turn.

## Mode flags

Each of these flags selects an exclusive mode:

| Flag | Mode |
|------|------|
| `-schema` | Print the recovered type schema and exit |
| `-types` | Print raw type definitions as JSON and exit |
| `-stats` | Print stream-level statistics and exit |
| `-diff PATH` | Structural diff against another gob file |
| `-count`, `-sum`, `-min`, `-max`, `-avg` | Aggregate the match set to a single value |

Only one mode may be active at a time. Combining two mode flags — say `-schema` with `-diff`, or `-sum` with `-count` — is a usage error: `gq` exits 2 with a message naming the conflict rather than guessing which one you meant.

The same strictness applies to flags that a mode would ignore. Passing `-format csv` with `-stats`, or `-sort` with `-schema`, is rejected with a usage error instead of being silently dropped. Input-selection and decode flags (`-f`, `-color`/`-no-color`, `-skip-errors`, `-read-limit`) work in every mode; the Flag Reference page has the full compatibility table.

## Schema

`-schema` renders the Go-style type declarations embedded in the stream — the shape of the data, recovered without any access to the original source code:

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

Types whose values were encoded as opaque byte blobs are annotated with a comment (`// GobEncoder`, and similar annotations for the other marshaling interfaces) so you can see where custom binary encodings hide inside otherwise plain structs.

Pass `-schema-format json` for a machine-readable rendering suitable for downstream tooling — code generators, documentation pipelines, compatibility checkers:

```sh
gq -schema -schema-format json -f data.gob
```

The output is a JSON array of type declarations with `name`, `kind`, and kind-specific keys (`fields`, `target`, `annotation`).

For a lower-level view, `-types` prints the raw wire-format type definitions as JSON — the type IDs, field deltas, and element type references exactly as they appear in the stream. It is mostly useful for debugging gob encodings themselves; for everyday "what's in this file" questions, `-schema` is the friendlier answer.

```sh
gq -types -f data.gob
```

## Stream statistics

`-stats` summarizes a stream without emitting individual values. It is handy for sizing a file, spotting the dominant types, or seeing which struct fields are actually populated:

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

The per-type breakdown lists how many values of each type appear and how many bytes they occupy. For struct types it goes one level deeper: a field-presence table showing what fraction of values carry each field. Because gob omits zero-valued fields on the wire, presence percentages double as a quick profile of your data — a field at 12% is zero-valued in 88% of records.

The `opaque values` line counts binary blobs from `GobEncoder`-style types: how many `gq` decoded with a built-in decoder (such as `time.Time` or `math/big.Int`) versus how many it could only show as raw bytes.

Pair with `-format json` for machine-readable statistics suitable for scripting or trend tracking:

```sh
gq -stats -format json -f data.gob | jq '.ByType[0]'
```

`-stats` supports only two output formats: `pretty` (the default) and `json`. Anything else is a usage error.

## Aggregation

The aggregation flags reduce the match set to a single scalar and print it. `-count` needs no path; `-sum`, `-min`, `-max`, and `-avg` take a path expression evaluated relative to each match:

```sh
# How many orders are in the stream?
gq -count -f orders.gob .Orders.*

# Total of all line-item prices
gq -sum Price -f orders.gob .Items.*

# Average score of successful runs only
gq -avg Score -f runs.gob '.Results.*[Status=ok]'

# Largest order ID
gq -max ID -f orders.gob .Orders.*
```

The query expression filters the match set first; the aggregation path then picks the numeric field out of each match. This composes naturally with everything on the Query Language page — filters, wildcards, recursive descent.

Aggregations are **single-pass and constant-memory**: they stream through the file without materializing the match set, so they are safe on files far larger than RAM.

`-sum`, `-min`, and `-max` keep full `int64`/`uint64` precision, degrading to floating point only when a float value appears in the data or an integer accumulator overflows. Small integer sums stay exact.

One caveat comes from the wire format itself: gob omits zero-valued fields, so a match whose aggregation path is absent (an encoded zero) is *skipped*, not counted as zero. `-avg` divides by the count of present values only, and `-min`/`-max` never see the omitted zeros. If your data legitimately contains zeros in the aggregated field, the true minimum may be lower and the true average may differ from what `gq` reports — cross-check with `-count` on a `[Field!]` existence filter if the distinction matters:

```sh
# How many matches actually carry a Price on the wire?
gq -count -f orders.gob '.Items.*[Price!]'
```

## Structural diff

`-diff` compares two gob streams, aligning top-level values by position (the first value of one stream against the first value of the other, and so on), and emits a per-position delta tree. It is useful for spotting regressions between archived snapshots:

```sh
gq -diff snapshot-2025-01.gob -f snapshot-2025-02.gob
```

The main input (`-f` or stdin) is compared against the file named by `-diff`. Both sides get automatic decompression — a gzipped snapshot diffs against a zstd one without any extra flags.

The exit code doubles as a signal: **0 when the streams are identical, 1 when any position differs**, which makes `-diff` usable directly in shell conditionals and CI checks:

```sh
if ! gq -diff expected.gob -f actual.gob >/dev/null; then
  echo "snapshot drifted"
fi
```

Text output is colorized when stdout is a terminal — additions in green, removals in red, struct/map/slice headers in bold cyan, stream-position markers (`[N]`) dimmed. Use `-no-color` to force plain output for redirects and pagers that don't pass ANSI escapes through, or `-color` to force color on regardless of detection.

For machine consumption, pair `-diff` with `-format json` (one indented delta document) or `-format jsonl` (one compact delta per line), both always plain, uncolored output:

```sh
gq -diff old.gob -f new.gob -format json > delta.json
```

`-diff` supports `pretty`, `json`, and `jsonl` output; `csv` and `tsv` are usage errors in this mode. `-time-format` also applies, so timestamps inside the delta render in the layout you choose.
