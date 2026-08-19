// Package gobspect is a decode-only introspection library for Go's
// encoding/gob wire format. It reads arbitrary gob streams without requiring
// the original Go types, producing a structured [Value] AST and human-readable
// output via [Format].
//
// # Basic usage
//
//	ins := gobspect.New()
//	stream := ins.Stream(r)
//
//	for v, err := range stream.Values() {
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    fmt.Println(gobspect.Format(v))
//	}
//
//	// stream.Types() now contains every type encountered
//
// To collect all values at once:
//
//	values, err := ins.Stream(r).Collect()
//
// # Value AST
//
// [Stream.Values] returns an iterator over [Value] nodes. Each [Value] is one
// of the concrete node types listed below; use a type switch to dispatch on them:
//
//   - [StructValue] — a gob struct with named [Field] entries
//   - [MapValue] — a gob map with [MapEntry] key/value pairs
//   - [SliceValue] — a gob slice
//   - [ArrayValue] — a gob array
//   - [IntValue], [UintValue], [FloatValue], [ComplexValue] — numeric scalars
//   - [BoolValue] — a boolean
//   - [StringValue] — a string
//   - [BytesValue] — a []byte
//   - [InterfaceValue] — an interface value carrying a concrete type name and inner [Value]
//   - [NilValue] — a nil pointer or nil interface
//   - [OpaqueValue] — raw bytes produced by GobEncoder, BinaryMarshaler, or TextMarshaler,
//     with an optional best-effort decoded form in the Decoded field
//
// # Type metadata
//
// [Stream.Types] returns [TypeInfo] for every type definition in the stream as
// the stream is consumed. By the time a value is yielded by [Stream.Values],
// all type definitions it references are already present in [Stream.Types] and
// all [TypeRef.Name] fields are resolved. [Stream.TypeByID] provides O(1) lookup
// by stream-scoped type ID.
//
// [Stream.Schema] drains the stream and returns all type definitions formatted
// as a [Schema].
//
// # Looking up types during iteration
//
//	stream := ins.Stream(r)
//	for v, err := range stream.Values() {
//	    if err != nil { log.Fatal(err) }
//	    sv, ok := v.(gobspect.StructValue)
//	    if !ok { continue }
//	    ti, ok := stream.TypeByID(sv.TypeID())
//	    if ok {
//	        fmt.Printf("type %s has %d fields\n", ti.Name, len(ti.Fields))
//	    }
//	}
//
// # Opaque types
//
// Types that implement GobEncoder, BinaryMarshaler, or TextMarshaler are
// represented as [OpaqueValue]. [New] pre-registers decoders for common types:
//
//   - time.Time (encoded as RFC 3339 with nanosecond precision)
//   - math/big.Int and math/big.Rat (auto-detected by wire format)
//   - math/big.Float (registered under "math/big.Float")
//   - UUID types from github.com/google/uuid and github.com/gofrs/uuid
//   - github.com/shopspring/decimal.Decimal
//
// Additional decoders can be registered with [Inspector.RegisterDecoder].
// TextMarshaler blobs are always decoded as UTF-8 strings automatically.
//
// # Formatting
//
// [Format] renders any [Value] as a human-readable string. [FormatTo] writes
// the same output to an [io.Writer] and propagates write errors. Structs are
// always indented; maps, slices, and arrays are inlined when short (≤72 chars)
// and indented otherwise. Map entries are sorted by key for deterministic output.
// Use [WithIndent], [WithMaxBytes], and [WithRawOpaques] to adjust the output.
//
// # Decoding limits
//
// Pass [WithReadLimit] to [New] to cap total bytes read from a stream:
//
//	ins := gobspect.New(gobspect.WithReadLimit(10 << 20)) // 10 MiB
//
// # Path-based navigation
//
// The companion subpackage github.com/codepuke/gobspect/query provides
// path-based navigation of decoded Value trees. Use query.AllPathSeq for
// lazy, streaming enumeration of matching values (early-break safe), or
// query.AllPath to collect all matches into a slice.
//
// # Companion subpackages
//
// Beyond query, sibling subpackages cover sorting (sortval), CSV/TSV output
// (tabular), structural diffing (diff), transparent decompression by
// magic-byte sniffing (decompress), and the full query-run engine behind the
// gq command — pipeline, rendering, aggregation — as a library (gq).
package gobspect
