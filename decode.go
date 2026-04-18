package gobspect

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// countingReader wraps a byteReadReader and enforces a byte limit.
// When the total bytes read exceeds limit, both ReadByte and Read return an error.
type countingReader struct {
	r     byteReadReader
	n     int64
	limit int64
}

func (cr *countingReader) ReadByte() (byte, error) {
	b, err := cr.r.ReadByte()
	if err == nil {
		cr.n++
		if cr.n > cr.limit {
			// Return the byte alongside the error, consistent with Read which
			// returns n > 0 alongside the limit error.  All callers in the
			// decode path (decodeUint and friends) check err before using b,
			// so neither behaviour causes correctness problems — but returning
			// b makes the contract uniform.
			return b, fmt.Errorf("gob: stream exceeds MaxBytes limit of %d", cr.limit)
		}
	}
	return b, err
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	if err == nil && cr.n > cr.limit {
		return n, fmt.Errorf("gob: stream exceeds MaxBytes limit of %d", cr.limit)
	}
	return n, err
}

// wrapWithLimit wraps r in a countingReader when limit > 0.
func wrapWithLimit(r io.Reader, limit int64) byteReadReader {
	var br byteReadReader
	if b, ok := r.(byteReadReader); ok {
		br = b
	} else {
		br = bufio.NewReader(r)
	}
	if limit > 0 {
		return &countingReader{r: br, limit: limit}
	}
	return br
}

// DecoderFunc decodes the raw bytes of a GobEncoder, BinaryMarshaler, or
// TextMarshaler blob into a human-meaningful value.
//
// The returned value should be a simple Go type (string, int, float, map, etc.)
// suitable for display. It does not need to reconstruct the original Go type.
type DecoderFunc func([]byte) (any, error)

// OpaqueDecoder is kept for backward compatibility.
// New code should use [DecoderFunc].
type OpaqueDecoder = DecoderFunc

// Options configures decoding limits.
type Options struct {
	// MaxBytes limits total bytes read from the stream. Zero means no limit.
	MaxBytes int64
}

// Option is a functional option for [New].
type Option func(*Inspector)

// WithOptions sets decoding limits.
func WithOptions(o Options) Option {
	return func(ins *Inspector) { ins.options = o }
}

// WithTimeFormat sets the layout used to render time.Time opaque values.
// The layout must be a valid Go time format string (see the time package).
// Default: time.RFC3339Nano.
func WithTimeFormat(layout string) Option {
	return func(ins *Inspector) {
		ins.decoders["Time"] = makeTimeDecoder(layout)
	}
}

// Inspector is the top-level entry point. It holds the opaque decoder registry
// and decoding options. Create one with [New].
type Inspector struct {
	decoders         map[string]DecoderFunc
	anonymousDecoders []DecoderFunc
	options          Options
}

// New returns an Inspector with all built-in opaque decoders pre-registered.
func New(opts ...Option) *Inspector {
	ins := &Inspector{
		decoders: make(map[string]DecoderFunc),
	}
	registerBuiltins(ins)
	for _, o := range opts {
		o(ins)
	}
	return ins
}

// RegisterDecoder adds or overrides the opaque decoder for the given type name.
func (ins *Inspector) RegisterDecoder(typeName string, dec DecoderFunc) {
	ins.decoders[typeName] = dec
}

// RegisterAnonymousDecoder appends dec to the list of anonymous decoders tried
// for opaque values with an empty type name. Decoders are tried in registration
// order; the first one that returns a non-error result wins.
func (ins *Inspector) RegisterAnonymousDecoder(dec DecoderFunc) {
	ins.anonymousDecoders = append(ins.anonymousDecoders, dec)
}

// — Stream state ————————————————————————————————————————————————————————————

// byteReadReader satisfies both io.Reader and io.ByteReader.
type byteReadReader interface {
	io.Reader
	io.ByteReader
}

// streamDecoder holds per-stream state: the type registry and the source reader.
type streamDecoder struct {
	r        byteReadReader
	registry map[int]wireTypeDef // type ID → wireTypeDef
	types    []TypeInfo          // accumulated in definition order
	byID     map[int]int         // type ID → index in types
}

func newStreamDecoder(r byteReadReader) *streamDecoder {
	return &streamDecoder{
		r:        r,
		registry: make(map[int]wireTypeDef),
		byID:     make(map[int]int),
	}
}

// readMessage reads the next length-prefixed message from the stream.
// Returns (nil, io.EOF) when the stream is exhausted cleanly.
func (sd *streamDecoder) readMessage() ([]byte, error) {
	n, err := decodeUint(sd.r)
	if err != nil {
		return nil, err // may be io.EOF — caller distinguishes
	}
	if n > 1<<26 {
		return nil, fmt.Errorf("gob: message length %d exceeds sanity limit", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(sd.r, buf); err != nil {
		return nil, fmt.Errorf("gob: reading message body: %w", err)
	}
	return buf, nil
}

// nextRawMessage reads the next stream message and returns the raw signed
// type ID and a reader positioned at the message body (after the type ID).
// rawID < 0 means type definition; rawID > 0 means value.
func (sd *streamDecoder) nextRawMessage() (rawID int64, r *bytes.Reader, err error) {
	buf, err := sd.readMessage()
	if err != nil {
		return 0, nil, err
	}
	br := bytes.NewReader(buf)
	rawID, err = decodeInt(br)
	if err != nil {
		return 0, nil, fmt.Errorf("gob: reading type ID: %w", err)
	}
	return rawID, br, nil
}

// processTypeDef decodes a wireType definition from the message body and
// registers it via registerAndResolve.
func (sd *streamDecoder) processTypeDef(id int, r io.ByteReader) error {
	def, err := decodeWireType(r)
	if err != nil {
		return fmt.Errorf("gob: decoding wireType for ID %d: %w", id, err)
	}
	return sd.registerAndResolve(id, def)
}

// registerAndResolve registers a new type definition, converts it to TypeInfo,
// back-fills TypeRef.Name for any existing types that reference this ID, and
// records the index in byID for O(1) lookup.
func (sd *streamDecoder) registerAndResolve(id int, def wireTypeDef) error {
	sd.registry[id] = def
	ti, err := sd.wireTypeToTypeInfo(id, def)
	if err != nil {
		return fmt.Errorf("gob: converting wireType to TypeInfo for ID %d: %w", id, err)
	}
	// Back-fill existing TypeRefs that were waiting for this ID.
	if name := wireTypeDefName(def); name != "" {
		for i := range sd.types {
			if sd.types[i].Key != nil && sd.types[i].Key.ID == id && sd.types[i].Key.Name == "" {
				sd.types[i].Key.Name = name
			}
			if sd.types[i].Elem != nil && sd.types[i].Elem.ID == id && sd.types[i].Elem.Name == "" {
				sd.types[i].Elem.Name = name
			}
		}
	}
	sd.byID[id] = len(sd.types)
	sd.types = append(sd.types, ti)
	return nil
}

// wireTypeToTypeInfo converts an internal wireTypeDef to the public TypeInfo.
// It resolves TypeRef names from the registry where possible.
func (sd *streamDecoder) wireTypeToTypeInfo(id int, def wireTypeDef) (TypeInfo, error) {
	switch {
	case def.ArrayT != nil:
		t := def.ArrayT
		return TypeInfo{
			ID:   id,
			Name: t.Common.Name,
			Kind: KindArray,
			Elem: sd.resolveRef(t.Elem),
			Len:  t.Len,
		}, nil

	case def.SliceT != nil:
		t := def.SliceT
		return TypeInfo{
			ID:   id,
			Name: t.Common.Name,
			Kind: KindSlice,
			Elem: sd.resolveRef(t.Elem),
		}, nil

	case def.MapT != nil:
		t := def.MapT
		return TypeInfo{
			ID:   id,
			Name: t.Common.Name,
			Kind: KindMap,
			Key:  sd.resolveRef(t.Key),
			Elem: sd.resolveRef(t.Elem),
		}, nil

	case def.StructT != nil:
		t := def.StructT
		fi := make([]FieldInfo, len(t.Fields))
		for i, f := range t.Fields {
			fi[i] = FieldInfo{Name: f.Name, TypeID: f.ID}
		}
		return TypeInfo{
			ID:     id,
			Name:   t.Common.Name,
			Kind:   KindStruct,
			Fields: fi,
		}, nil

	case def.GobEncoderT != nil:
		return TypeInfo{
			ID:   id,
			Name: def.GobEncoderT.Common.Name,
			Kind: KindGobEncoder,
		}, nil

	case def.BinaryMarshalerT != nil:
		return TypeInfo{
			ID:   id,
			Name: def.BinaryMarshalerT.Common.Name,
			Kind: KindBinaryMarshaler,
		}, nil

	case def.TextMarshalerT != nil:
		return TypeInfo{
			ID:   id,
			Name: def.TextMarshalerT.Common.Name,
			Kind: KindTextMarshaler,
		}, nil

	default:
		return TypeInfo{}, fmt.Errorf("gob: wireTypeDef has no recognised variant set for ID %d", id)
	}
}

// resolveRef creates a TypeRef for the given type ID, filling in the name
// if the type is already known (builtin or previously defined).
func (sd *streamDecoder) resolveRef(id int) *TypeRef {
	ref := &TypeRef{ID: id}
	if name, ok := builtinTypeName(id); ok {
		ref.Name = name
		return ref
	}
	if def, ok := sd.registry[id]; ok {
		ref.Name = wireTypeDefName(def)
	}
	return ref
}

// builtinTypeName returns the canonical name for a predefined gob type ID.
func builtinTypeName(id int) (string, bool) {
	switch id {
	case 1:
		return "bool", true
	case 2:
		return "int", true
	case 3:
		return "uint", true
	case 4:
		return "float", true
	case 5:
		return "[]byte", true
	case 6:
		return "string", true
	case 7:
		return "complex", true
	case 8:
		return "interface{}", true
	}
	return "", false
}

// wireTypeDefName extracts the Name from whichever variant is set.
func wireTypeDefName(def wireTypeDef) string {
	switch {
	case def.ArrayT != nil:
		return def.ArrayT.Common.Name
	case def.SliceT != nil:
		return def.SliceT.Common.Name
	case def.MapT != nil:
		return def.MapT.Common.Name
	case def.StructT != nil:
		return def.StructT.Common.Name
	case def.GobEncoderT != nil:
		return def.GobEncoderT.Common.Name
	case def.BinaryMarshalerT != nil:
		return def.BinaryMarshalerT.Common.Name
	case def.TextMarshalerT != nil:
		return def.TextMarshalerT.Common.Name
	}
	return ""
}

