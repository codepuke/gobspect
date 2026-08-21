# Changelog

All notable changes to gobspect are tracked here. The format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Changed

- **Documentation snippet topic ids renamed** for cross-language consistency on
  the codepuke site. A topic renders every language's variant in one tabbed
  block, so the four ports (gobspect, gobts, pygob, gobdotnet) must agree on
  each id. All four had independently picked their own; the ids were reconciled
  on 2026-08-20 with ties broken toward gobts. Renamed in
  `example_stdlib_test.go` (and the matching `:::examples` reference in
  `docs/03-wire-format.md`):
  - `encode-nested-struct` → `nested-struct`
  - `encode-interface` → `interface-values`
  - `encode-time` → `time-values`

  gobspect's API-only topics (`query-*`, `to-json`, `diff-values`,
  `redact-output`, `format-options`, `stream-values`, `stream-types`,
  `schema-extract`, `register-decoder`, `gobencoder-type`) are unchanged — they
  render as single-tab blocks and collide with nothing. `gobencoder-type` was
  specifically reviewed against gobts's `custom-marshaler` and kept separate:
  it shows a Go type implementing `GobEncoder`/`GobDecode`, not codec
  registration.

  No library code, exported API, or wire behaviour changed. Note that
  `content/manifest.json` in codepuke pins gobspect at commit `129def3` with
  the old ids, so it needs a re-sync.

  Still divergent, tracked but not fixed: fixture data on the shared multi-tab
  topics. gobspect uses `Dog`/`Pet` for `interface-values` and
  `2024-03-14T15:09:26Z` for `time-values`, where the sister ports use other
  values; same-id variants are meant to show the same data.

## v0.3.1

The logic that downstream frontends (notably the gobspect-mcp server) had been
reimplementing from `cmd/gq` is now public API. Everything is additive: no
existing signature changed or was removed.

### Added

- **`gq` subpackage** (`github.com/codepuke/gobspect/gq`) — the gq query
  engine as a library:
  - `Pipeline` drives a stream through the standard result flow (query →
    index → sort → offset → limit) into a `Sink`, with turnkey drivers
    `RunRender` (pretty/json/jsonl) and `RunTabular` (CSV/TSV). `RunTabular`
    reads the printer's heterogeneous mode and sorts per struct-type
    partition automatically in partition mode, so the partition-sort behavior
    can no longer diverge between frontends. Sink errors come back wrapped in
    `*SinkError` (transparent to `errors.Is`/`errors.As`), keeping output
    failures distinguishable from decode failures.
  - `Render` writes one `Value` in the gq output formats with the raw,
    compact, and color switches.
  - `Aggregate` reduces query matches (`count`, `sum`, `min`, `max`, `avg`)
    with exact `int64` arithmetic degrading to `float64` only on overflow or
    float input; non-numeric targets report `ErrNonNumeric`.
- **`decompress` subpackage** (`github.com/codepuke/gobspect/decompress`) —
  `decompress.Reader` transparently decompresses gzip, zstd, xz, bzip2, and
  single-file zip archives, detected by magic-byte sniffing rather than file
  extension; unrecognized input passes through unchanged. Brings zstd
  (`klauspost/compress`) and xz (`ulikunitz/xz`) module dependencies, used
  only by this subpackage (plus `dsnet/compress` as a test-only bzip2
  writer).
- **`Unwrap`** in the root package strips every `InterfaceValue` layer
  (normalizing a nil inner value to `NilValue`) — the recursive unwrap the
  comparison layer gained in v0.2.3, now exported for consumers.
- **`tabular.Printer.HeterogeneousMode`** accessor, so pipeline drivers can
  derive partition behavior from the printer itself.
- **`gq -read-limit N`** flag wiring the long-exposed `WithReadLimit` option
  into the CLI: caps decompressed bytes read from the input (default 0 = no
  limit). With compressed input the cap applies post-decompression, so a
  decompression bomb errors out early instead of exhausting memory.
- **`gq` reads zstd, xz, bzip2, and zip input** (previously gzip only), on
  stdin, `-f`, and `-diff` alike, via the shared `decompress` sniffing.

### Fixed

- **`gq -r` unwraps nested interface layers.** The raw-string check peeled a
  single `InterfaceValue`, so a doubly-wrapped string printed as a quoted
  pretty value instead of the bare string — the same defect class fixed in
  `Equal`/`CompareValues` for v0.2.3. The extracted renderer (and the
  aggregation layer's numeric coercion, which shared the single-level peel)
  now unwrap recursively via `Unwrap`.

### Test / infrastructure

- Two new fuzz targets: `decompress.FuzzReader` (magic sniffing and all five
  codecs over arbitrary bytes, with a passthrough byte-identity oracle) and
  `gq.FuzzRender` (rendering and pipeline over hostile decoded values).
  `FuzzGQ` gained `-read-limit` argument coverage.
- The `cmd/gq` behavioral suite runs unmodified against the extracted engine
  — the extraction is behavior-preserving by that proof. Two white-box tests
  of moved helpers were ported: the `printValue` ANSI-color test now drives
  `gq.Render`, and the `formatFloat` int64-boundary test moved to the gq
  package.

## v0.2.3

### Fixed

- **`Equal` and `CompareValues` are reflexive for nested interface values.**
  Both peeled a single `InterfaceValue` layer before dispatching, so a value
  wrapped more than once — `InterfaceValue{Value: InterfaceValue{…}}`, which
  gob streams do produce — reached no case in the comparison switch and fell
  through to "not equal", even when compared against itself. `diff` inherited
  the fault and reported phantom differences between identical values. The
  same hole existed for an `InterfaceValue` holding a nil inner value; it now
  normalises to `NilValue`.
- **`CompareValues` no longer contradicts itself by nesting depth.** Interface
  type names were ignored at the top level but honored one level down, because
  composite values fall back to comparing `Format` output and `Format` renders
  the `(TypeName)` prefix. The same pair of values could compare equal or
  unequal depending only on how deeply they sat.

### Changed

- **An interface's concrete type name now participates in comparison.**
  `Equal` previously documented that an `InterfaceValue`'s `TypeName` was
  disregarded. For struct concretes that was harmless — the inner
  `StructValue.TypeName` already told `Dog` from `Cat` — but for named scalar
  types it is the only surviving distinction: `Miles(5)` and `Kilos(5)` both
  decode to `InterfaceValue{Value: IntValue{5}}`, and they compared equal.
  Values that differ only by the concrete type stored in an interface are now
  reported as different.

  A one-sided wrapper is still unwrapped, so a value read through an interface
  continues to equal the same value read directly.

### Added

- **`Comparer`**, a configurable comparison front end. `Comparer.Equal` and
  `Comparer.Compare` take the same arguments as the package-level functions,
  which are now shorthands for the zero value (and `Comparer{Fold: true}`).
  - `IgnoreInterfaceTypeName` restores the previous behavior, uniformly at
    every nesting depth. Set it when diffing streams from different builds of
    the same program: `TypeName` holds the fully-qualified type, so a module
    path change or package move otherwise makes every interface-typed field
    read as modified.
  - `Fold` selects case-insensitive string comparison, and composes with
    `IgnoreInterfaceTypeName` rather than excluding it.

### Test / infrastructure

- **Six new fuzz targets** covering previously unfuzzed surface: value
  rendering and JSON serialization (`FuzzRender`), comparison and diffing
  (`FuzzCompareDiff`), path evaluation over decoded streams (`FuzzQueryEval`),
  CSV/TSV output (`FuzzTabular`), sort-spec parsing and sorting
  (`FuzzSortval`), and the `gq` CLI end to end including gzip sniffing
  (`FuzzGQ`). Targets assert real properties — rendering determinism, JSON
  validity, ordering antisymmetry, permutation-preserving sorts, and
  rectangular CSV output — rather than only checking for panics.
- **`FuzzDecode` extended** to drive `Stream.Messages`, `Stream.Stats`, and
  the type-table accessors, and to repeat every pass with
  `WithSkipCorruptValues` enabled so the resync path is exercised.
- **`FuzzORGroups` made reproducible**: its alternative-shuffling step drew
  from the global `math/rand` source, so a commutativity failure need not
  reproduce from its saved corpus entry. It is now seeded from the input
  expression.
- A three-hour, nine-target fuzzing campaign found both defects above.
  Baselines are recorded at the top of each target's file.

## v0.2.2

### Fixed

- **`query`: signed integer filter literals compare correctly against uint
  fields.** `+`-prefixed literals (`[U==+5]`) and negative zero (`[U<=-0]`)
  were treated as below the uint64 range, silently inverting comparisons;
  `+`-prefixed literals above `MaxInt64` (`[M==+18446744073709551615]`) never
  matched.
- **`query`: element filters on empty collections yield nothing.** An empty
  slice, array, or map was mistaken for a scalar and tested as a predicate, so
  `Empty[Field!!]` wrongly yielded the empty container itself.
- **`query`: `MustGet` panic messages render the failing filter operator as
  written.** `!!`, `!=`, `!~`, `==`, `<`, `>`, `<=`, and `>=` all previously
  displayed as `=`; quoted patterns are now re-quoted.
- **`query`: `SchemaAt` accepts filter-as-predicate paths the runtime
  resolves.** A filter on a non-collection type (e.g. `[ID!]` on a struct
  root) now returns the type unchanged instead of a "not a collection" error.
- **`gq`: float aggregation results landing exactly on 2^63 print correctly.**
  The integer fast path in `-sum`/`-min`/`-max`/`-avg` output relied on an
  out-of-range float→int64 conversion, printing `9223372036854775807` (off by
  one, and platform-dependent) instead of `9.223372036854776e+18`.
- **`gq`: `-index` values below -1 are rejected** with a usage error instead
  of silently behaving like "all values".
- **`gq`: an accidentally committed 3.6 MB `cmd/gq/gq` binary was removed**
  from the repository and is now gitignored.

### Documentation

- `gq` README: the `[Field~pattern]` filter was documented as a substring
  match; it is a collection-contains match (slice/array/map with an entry
  matching the glob). The filter table now also covers `!!`, `!=`, `!~`, and
  the ordering comparisons, and the aggregation section notes that gob's
  omitted zero-valued fields are skipped by `-avg`/`-min`/`-max`.

## v0.2.1

### Security / robustness

The decoder now upholds its "never panics, never over-allocates on untrusted
input" contract against several crafted-stream attacks:

- **Recursion depth is bounded.** A self-referential type definition could
  previously drive value decoding (and `Schema()`) into unbounded recursion,
  crashing the process with an unrecoverable stack overflow. Both now enforce
  a depth limit.
- **Element and byte counts are bounded by the message body.** Slice, map,
  array, `[]byte`, string, and opaque lengths are validated against the bytes
  actually available and grown incrementally, so a tiny message can no longer
  force a multi-gigabyte allocation.
- **Type IDs are validated.** Definitions for reserved (builtin) IDs and
  duplicate redefinitions are rejected, matching the stdlib decoder — they
  could otherwise make `TypeByID`/`Schema` disagree with how values decode.
- **Opaque decoder panics are contained.** A registered decoder that panics on
  hostile input now degrades to raw-bytes display instead of taking down the
  caller.
- **Trailing bytes after a value are reported** rather than silently ignored.

### Fixed

- **shopspring/decimal.Decimal was decoded with the wrong byte order.** The
  4-byte exponent is at the *front* of the blob, not the end; real decimal
  values previously mis-decoded or errored. Extreme exponents are now rejected
  instead of panicking or allocating gigabytes. A fixture generated by the real
  library guards the layout.
- **Opaque decoders now match the bare wire type name.** `time.Time`,
  `uuid.UUID`, `decimal.Decimal`, and the `netip.*` types are keyed under the
  unqualified name gob actually emits (`Time`, `UUID`, `Decimal`, `Addr`, …),
  so interface-wrapped values decode; qualified aliases are kept.
- **`Format` no longer re-renders subtrees per nesting level**, which made
  deeply nested values take exponential time (an effective hang past ~40
  levels). Redaction width is now measured from the plain rendering, so ANSI
  color codes no longer inflate the number of fill characters.
- **`ToJSON` no longer fails the whole document on NaN/±Inf floats.** They
  serialize as `"NaN"`/`"+Inf"`/`"-Inf"` by default, or `null` under the new
  `WithNonFiniteAsNull` option (`gq -nonfinite null`). Non-UTF-8 string values
  are base64-encoded instead of being corrupted to U+FFFD.
- **`Equal` treats NaN as equal to NaN** so a value equals itself and self-
  diffs are empty; `CompareValues` orders NaN totally (below all numbers) and
  gives `ComplexValue` a proper numeric ordering slot instead of comparing it
  by formatted-string.
- **`diff` distinguishes map keys that format identically** (e.g. `int(1)` vs
  `uint(1)`), so a real change is no longer reported as "no difference"; long
  one-line diff values truncate on a rune boundary.
- **`tabular` union mode now emits one rectangular table** (buffered until
  `Flush`) instead of a mid-file header and ragged rows; scalar rows honor the
  heterogeneous-type mode; interface-wrapped `[]byte` cells honor the
  configured bytes format.
- **Various decoder edge cases**: `big.Rat`/`big.Float` no longer panic or
  over-allocate on 32-bit platforms or extreme exponents; out-of-range
  `time.Time` nanoseconds are rejected rather than rendered as garbage.

### `query`

- **`Get` now returns the first match of `All` in document order.** A fan-out
  segment (`*` or a filter) previously committed to the first element before
  evaluating the rest of the path, so `Get(root, "Items.*.Name")` failed when
  the first item had no `Name` even though later items did (common with gob,
  which omits zero-valued fields). `..` descent already behaved this way; all
  segment kinds now agree, and evaluation is lazy (stops at the first match).
- **`Get` results are unwrapped from `InterfaceValue`,** matching `All`. The
  same path previously returned the wrapper from `Get` but the inner concrete
  value from `All`.
- **`SchemaAt` supports wildcard descent (`..[Filter]`).** Filters following a
  wildcard descend are now resolved as per-node predicates — matching the
  runtime — instead of erroneously extracting element types twice, which made
  every `..[Filter]` path fail with "no candidate types match segment".
- **Numeric filters are exact across the full int64/uint64 range.** Integer
  patterns near or beyond the 2^63/2^64 boundaries (e.g.
  `[ID==9223372036854775808]`) previously fell into a float64 comparison that
  collapsed adjacent large integers, producing false matches and misses.
  Float comparison is now used only for `FloatValue` fields and float-syntax
  literals.
- **Interface-keyed maps are navigable.** Keys of a `map[any]T` arrive wrapped
  in `InterfaceValue`; path segments, filter field lookups, `[Field~pattern]`
  key matching, and `Keys` now unwrap them, so string keys behave like
  `map[string]T` keys.

### `gq`

- **Conflicting or ignored flag combinations are now rejected** (exit 2)
  instead of silently ignored — e.g. `-count -sum`, `-stats -format csv`,
  `-diff -schema`, or mode flags combined with flags they don't use.
- **`-h`/`-help` prints usage to stdout and exits 0** (previously exit 2).
- **Aggregations keep int64/uint64 precision**, degrading to float64 only when
  a float appears or an accumulator overflows, so summing large integer IDs no
  longer silently rounds.
- **gzip auto-detection is content-based for files too** — a gzipped `-f`/
  `-diff` input is decompressed regardless of its extension.
- **`-nonfinite strings|null`** selects the JSON rendering of non-finite
  floats; `-max-bytes` rejects negative values; output write errors on the
  diff/schema/types/stats paths are reported instead of dropped.

### Dependencies

- `golang.org/x/term` 0.42.0 → 0.45.0, `golang.org/x/sys` 0.43.0 → 0.47.0;
  `x/term` promoted to a direct dependency.

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
