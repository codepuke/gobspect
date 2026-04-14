// Package gobspect is a decode-only introspection library for Go's
// encoding/gob wire format. It reads arbitrary gob streams without requiring
// the original Go types, producing a structured [Value] AST and human-readable
// output via [Format].
//
// # Basic usage
//
//	ins := gobspect.New()
//
//	values, err := ins.Decode(r)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for _, v := range values {
//	    fmt.Println(gobspect.Format(v))
//	}
//
// # Value AST
//
// [Decode] returns a []Value. Each [Value] is one of the concrete node types
// listed below; use a type switch to dispatch on them:
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
// [DecodeTypes] returns [TypeInfo] for every type definition in the stream
// without decoding values. [DecodeStream] returns both type definitions and
// values together in a [StreamResult], which includes partial results on error.
//
// # Opaque types
//
// Types that implement GobEncoder, BinaryMarshaler, or TextMarshaler are
// represented as [OpaqueValue]. [New] pre-registers decoders for common types:
//
//   - time.Time (encoded as RFC 3339)
//   - math/big.Int and math/big.Rat (auto-detected)
//   - UUID types from github.com/google/uuid and github.com/gofrs/uuid
//
// Additional decoders can be registered with [Inspector.RegisterDecoder].
// TextMarshaler blobs are always decoded as UTF-8 strings automatically.
//
// # Formatting
//
// [Format] renders any [Value] as a human-readable string. Structs are always
// indented; maps, slices, and arrays are inlined when short (≤72 chars) and
// indented otherwise. Map entries are sorted by key for deterministic output.
// Use [WithIndent], [WithMaxBytes], and [WithRawOpaques] to adjust the output.
//
// # Decoding limits
//
// Pass [WithOptions] to [New] to set a maximum recursion depth ([Options.MaxDepth])
// or a maximum byte limit ([Options.MaxBytes]):
//
//	ins := gobspect.New(gobspect.WithOptions(gobspect.Options{
//	    MaxDepth: 64,
//	    MaxBytes: 10 << 20, // 10 MiB
//	}))
package gobspect
