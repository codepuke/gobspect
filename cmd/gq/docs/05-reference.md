---
title: "Flag reference"
---

This page is the complete reference for `gq`'s command line: the invocation forms, every flag with its default, which flags combine with which modes, and the exit-code contract. The topic pages explain each area in depth; this one is for looking things up.

## Invocation

```sh
gq [flags] [query]
```

- **No positional arguments** — reads from stdin (or the file given with `-f`) and prints all values.
- **One positional argument** — it is the query expression; input still comes from `-f` or stdin.
- **Two or more positional arguments** — usage error (exit 2).

Query expressions use dot-separated field names in the spirit of jq. A leading `.` is accepted and stripped (`.Field` and `Field` are equivalent), and an empty expression — or none at all — matches the entire value. The full syntax is on the Query Language page. Quote expressions containing `*`, `[`, `!`, or `|` so the shell passes them through untouched.

`-h` or `-help` prints usage to stdout and exits 0.

## All flags

### Input

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `-file` | stdin | Input file path; if omitted, reads from stdin. Compressed input (gzip, zstd, xz, bzip2, single-file zip) is detected by content and decompressed automatically — see the Overview page |
| `-read-limit N` | 0 | Maximum decompressed bytes to read from the input; 0 = no limit. Errors out instead of decoding past the cap |
| `-skip-errors` | false | Skip value messages that fail to decode and continue; type-definition errors remain fatal |

### Mode selectors

Each of these selects an exclusive mode; see the Schema, Stats, Aggregation, Diff page.

| Flag | Default | Description |
|------|---------|-------------|
| `-schema` | false | Print type schema and exit |
| `-schema-format` | `go` | Schema output format: `go` (Go-style declarations) or `json` (machine-readable); requires `-schema` |
| `-types` | false | Print raw type definitions as JSON and exit |
| `-stats` | false | Print stream-level statistics (message counts, per-type breakdown, field presence) and exit |
| `-diff PATH` | `""` | Structural diff against another gob file, aligned by index; exits 1 when changes exist |
| `-count` | false | After filtering, print the number of matches and exit |
| `-sum PATH` | `""` | Sum a numeric path over the matches and exit |
| `-min PATH` | `""` | Minimum of a numeric path over the matches |
| `-max PATH` | `""` | Maximum of a numeric path over the matches |
| `-avg PATH` | `""` | Average of a numeric path over the matches |

### Output format

Covered in depth on the Output Formats page.

| Flag | Default | Description |
|------|---------|-------------|
| `-format` | `pretty` | Output format: `pretty`, `json`, `jsonl`, `csv`, or `tsv` |
| `-compact` | false | Compact JSON output (no indentation); `-format json` only |
| `-no-headers` | false | Suppress header row; `csv`/`tsv` only |
| `-hetero` | `first` | Heterogeneous-type handling for `csv`/`tsv`: `first`, `reject`, `union`, or `partition` |
| `-nonfinite` | `strings` | JSON rendering of non-finite floats (NaN, ±Inf): `strings` (`"NaN"`, `"+Inf"`, `"-Inf"`) or `null`; `json`/`jsonl` only |

### Rendering

| Flag | Default | Description |
|------|---------|-------------|
| `-r` | false | Raw string: omit quotes for string results; `pretty` only. Also applies to strings wrapped in an interface value (common for top-level `any` fields) |
| `-bytes` | `hex` | Byte slice rendering: `hex`, `base64`, or `literal` |
| `-max-bytes N` | 64 | Truncation limit for byte slices; 0 = no limit |
| `-time-format` | RFC3339Nano | Go time layout for `time.Time` values |
| `-color` | auto | Force color on |
| `-no-color` | auto | Force color off |

Color is enabled automatically when stdout is a terminal and disabled when piping or redirecting; the two flags override the detection in either direction.

### Selection and pagination

Covered on the Selecting, Sorting, and Paging page.

| Flag | Default | Description |
|------|---------|-------------|
| `-index N` | -1 | Print only the Nth top-level value (0-based); -1 = all |
| `-offset N` | 0 | Skip the first N results |
| `-limit N` | 0 | Stop after N results; 0 = no limit |
| `-null-on-miss` | false | Print `null` instead of exiting 1 when a path is not found |

### Sorting

Also covered on the Selecting, Sorting, and Paging page. The three modifier flags require `-sort`.

| Flag | Default | Description |
|------|---------|-------------|
| `-sort` | `""` | Comma-separated column names to sort by. Each entry may take a `:asc` or `:desc` suffix (e.g. `Name,Score:desc`) |
| `-sort-desc` | false | Default direction for entries without an explicit suffix |
| `-sort-fold` | false | Case-insensitive string comparison in sort |
| `-sort-drop-missing` | false | Exclude rows missing all sort keys |

## Flag compatibility

`gq` never silently ignores a flag. Any flag that would have no effect in the selected mode — or that contradicts another flag — is rejected with a usage error (exit 2) naming the conflict. The rules:

**Mutual exclusions.**

- `-count`, `-sum`, `-min`, `-max`, and `-avg` are mutually exclusive; any one of them selects aggregation mode.
- The mode selectors (`-schema`, `-types`, `-stats`, `-diff`, and the aggregation flags) conflict with each other — exactly one mode at a time.
- `-color` and `-no-color` cannot be combined.

**Universal flags** work in every mode: `-f`/`-file`, `-color`, `-no-color`, `-skip-errors`, and `-read-limit`.

**Per-mode flags.** Beyond the universal set, each mode accepts only the flags that affect it:

| Mode | Extra flags accepted | Query expression | `-format` values |
|------|----------------------|------------------|-------------------|
| normal (no mode flag) | everything in the tables above | yes | `pretty`, `json`, `jsonl`, `csv`, `tsv` |
| `-diff` | `-format`, `-time-format` | no | `pretty`, `json`, `jsonl` |
| `-schema` | `-schema-format` | no | — |
| `-types` | none | no | — |
| `-stats` | `-format` | no | `pretty`, `json` |
| aggregation | `-time-format` | yes | — |

So `-format csv` with `-stats`, `-sort` with `-schema`, or a query expression alongside `-diff` are all usage errors rather than no-ops.

**No-effect combinations in normal mode.** Even without a mode flag, a flag that the chosen `-format` would ignore is rejected:

- `-compact` requires `-format json` (`jsonl` is always compact and rejects `-compact` explicitly).
- `-r` requires `-format pretty`.
- `-nonfinite` requires `-format json` or `jsonl`.
- `-sort-desc`, `-sort-fold`, and `-sort-drop-missing` require `-sort`.
- `-schema-format` requires `-schema`.

## Robustness and untrusted input

**Partially corrupt streams.** By default, the first value message that fails to decode aborts the run with exit 1. With `-skip-errors`, `gq` skips the failing value message and continues with the rest of the stream — useful for salvaging data from a truncated or partially corrupted file. Type-definition errors remain fatal even with `-skip-errors`: once a type definition is unreadable, every later value referencing it would be garbage, so there is nothing safe to continue with.

```sh
gq -skip-errors -f damaged.gob
```

**Decompression caps.** Compressed input is decompressed transparently (see the Overview page), which means a tiny file on disk can expand to an enormous stream in memory and output — the classic "zip bomb". When inspecting files you did not produce, set `-read-limit` to a byte budget you are comfortable with; `gq` errors out instead of decoding past the cap:

```sh
# Refuse to read more than 100 MiB of decompressed data
gq -read-limit 104857600 -f untrusted.gob.zst
```

The gob decoder itself also enforces hard internal limits (64 MiB per message, bounded element counts) regardless of `-read-limit`, so malformed length fields cannot trigger runaway allocation.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success, or broken pipe (stdout closed early, e.g. by `head`) |
| 1 | Decode error, write error, or path not found (without `-null-on-miss`) |
| 2 | Usage error: bad flags, invalid query expression, or too many arguments |

Nuances worth knowing when scripting:

- **Broken pipe is success.** `gq -f big.gob | head` exits 0 even though `head` closed the pipe early; you can page and truncate output freely.
- **Path not found is 1 — unless `-null-on-miss`.** A query that matches nothing normally exits 1 so scripts can detect the miss. With `-null-on-miss`, it prints `null` (or nothing, in tabular formats) and exits 0.
- **`-diff` reuses code 1 as its "streams differ" signal.** Identical streams exit 0; any difference exits 1, so a decode failure and a real difference are indistinguishable by exit code alone — check stderr when it matters.
- **`-h`/`-help` exits 0** and prints usage to stdout, not stderr.

## Getting sample data

If you want a stream to experiment with and have Go installed, the gobspect module ships a small generator that writes a synthetic order stream (structs, nested slices, maps, `time.Time` opaques) — no checkout required:

```sh
go run github.com/codepuke/gobspect/cmd/demo@latest demo_data.gob

# Then explore it
gq -schema -f demo_data.gob
gq -index 0 -f demo_data.gob
gq -limit 10 -f demo_data.gob .Customer
gq -limit 10 -f demo_data.gob '.Items[Price>30].Price'
```

Any gob file works, of course — anything written by Go's `encoding/gob` package, with or without compression, is fair game.
