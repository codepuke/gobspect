# Changelog

All notable changes to gobspect are tracked here. The format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.2.0

### Breaking

- **`sortval.SortSpec.Desc bool` is replaced by per-key directions.** `Keys`
  is now `[]SortKey`, where `SortKey` is `{Field string; Desc bool}`, so a
  single spec can mix ascending and descending keys (e.g. `Name,Score:desc`).
  The signature of `ParseSortSpec` is unchanged except for the meaning of the
  third argument: `desc` is now the **default direction** applied only to keys
  with no explicit `:asc`/`:desc` suffix.
- **`gq -sort-desc` is now "default direction"**, not a hard override.
  Combine it with per-key `:asc` suffixes to mix directions.
- **Error messages from `Stream.Values()` and `Stream.Messages()`** are now
  wrapped with `gob: message N at offset B: …` prefixes. Tools that string-
  match against the previous error text will need to update.

### Added

- **`gq -diff PATH`** — structural diff against another `.gob` file, aligned
  by index. Exits 1 when any position differs, 0 otherwise. Pairs with
  `-format json` for a machine-readable delta tree.
- **`gq -stats`** — stream-level summary: message counts, body bytes, per-
  type record counts and byte consumption, struct field presence rates, and
  opaque decoder coverage. `-format json` emits the same data as JSON.
- **`gq -format jsonl`** — one compact JSON value per line, ideal for piping
  into `jq` without materialising the whole stream.
- **`gq -schema-format json`** — machine-readable schema output (array of
  type declarations with `name`, `kind`, `fields`/`target`/`annotation`)
  for downstream tooling.
- **`gq -skip-errors`** — continue past value-decode failures instead of
  aborting; the skip count is available via `Stream.SkipCount()`.
- **`gq -count`, `-sum`, `-min`, `-max`, `-avg`** — single-pass aggregators
  over the query match set. Numeric aggregators take a path expression
  relative to each match.
- **`gq -sort Name:asc,Score:desc`** — per-key sort directions alongside
  the existing multi-key sort.
- **Per-partition sort for `gq -hetero partition`** — the documented-but-
  unimplemented feature now sorts within each partition independently and
  emits partitions in arrival order.
- **`gobspect.Equal(a, b Value) bool`** — strict structural equality that
  complements `CompareValues`. Kind-for-kind only; no cross-numeric coercion.
- **`gobspect.MessageInfo` + `Stream.Messages()`** — iterator yielding one
  `MessageInfo` per length-prefixed frame (offset, body length, type ID,
  raw body), without forcing a full value decode. Enables size profiling
  and stream indexing.
- **`gobspect.WithSkipCorruptValues(bool)`** — Inspector option pair for the
  CLI's `-skip-errors`; type-definition errors remain fatal to keep the
  type registry consistent.
- **`Stream.Stats() (*Stats, error)`** + `Stats.Format`, `Stats.JSON`,
  `Stats.JSONIndent` — single-pass population-level statistics over a
  stream.
- **`Schema.JSON()` and `Schema.JSONIndent()`** — machine-readable schema
  rendering; mirrors the new `gq -schema-format json` output.
- **Static recursive-descent in `query.SchemaAt`** — `..Name` expressions
  are resolved by widening the search to every reachable type and returning
  the sorted, pipe-joined union of distinct result types (e.g. `int|string`).
  Previously this returned an error.
- **Built-in opaque decoders for `net/netip.Addr`, `netip.Prefix`,
  `netip.AddrPort`** — BinaryMarshaler blobs decoded via stdlib
  `UnmarshalBinary`, stored in `OpaqueValue.Decoded` as canonical strings
  so the Value AST stays free of inspected types.
- **New `gobspect/diff` subpackage** — `Diff`, `DiffStreams`, `Delta` AST
  (Added/Removed/Changed/StructDelta/MapDelta/SliceDelta/ArrayDelta), plus
  text and JSON renderers. The `gq -diff` flag is the CLI surface.
- **Colorized diff output** — `diff.ColorScheme`, `diff.ANSIColorScheme`, and
  `diff.WithColor` mirror the existing `gobspect.WithColor` pattern. `gq -diff`
  auto-enables ANSI color when stdout is a TTY; `-color` / `-no-color` force
  either mode. JSON diff output stays plain.

### Changed

- **`gq -sort-desc`** semantics moved from "flip every key" to "default for
  keys without `:asc`/`:desc`", enabling mixed-direction sorts.
- **Error reporting** now includes message index and byte offset for every
  value-level and framing-level decode failure (see Breaking).
- **`Inspector.WithReadLimit(0)` handling** is clarified: the byte counter
  is always active (it powers `MessageInfo.Offset` and error diagnostics);
  the zero limit simply disables the ceiling check.
- **`gobspect.Style.apply` renamed to `Style.Apply`** so other subpackages
  (notably `diff`) can render through the shared style type. Internal-only
  rename; no deprecation shim.

### Fixed

- **`time.Time` sub-minute negative-offset round-trip** now recovers the
  original zone offset. The stdlib's `time.UnmarshalBinary` reads byte 15 of
  the version-2 payload as `uint8`, losing the sign for values like `0xFE`
  (see BUG_REPORT.md). Our clean-room decoder reads it as `int8` and
  returns the correct offset — the decoder deliberately diverges from the
  stdlib bug.

### Test / infrastructure

- Coverage raised on previously-untested public API: `ParseBytesFormat`,
  `Schema.TypeByName`, `Stream.Stats`, `Stats.Format`, `Stats.JSON`,
  `Stats.JSONIndent`, the diff package's formatters, and per-feature CLI
  integration tests for all new flags.
- All v0.2.0 features land with behavior tests; no deprecation shims are
  maintained for pre-0.2 callers because the project has no external users
  yet.

## v0.1.0

Initial release.
