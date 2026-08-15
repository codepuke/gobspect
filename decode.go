package gobspect

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
)

// countingReader wraps a byteReadReader and tracks the total number of bytes
// read. When limit > 0, ReadByte and Read also return an error once the total
// crosses limit.
type countingReader struct {
	r     byteReadReader
	n     int64
	limit int64 // zero = no limit
}

func (cr *countingReader) ReadByte() (byte, error) {
	b, err := cr.r.ReadByte()
	if err == nil {
		cr.n++
		if cr.limit > 0 && cr.n > cr.limit {
			// Return the byte alongside the error, consistent with Read which
			// returns n > 0 alongside the limit error.  All callers in the
			// decode path (decodeUint and friends) check err before using b,
			// so neither behavior causes correctness problems — but returning
			// b makes the contract uniform.
			return b, fmt.Errorf("gob: stream exceeds MaxBytes limit of %d", cr.limit)
		}
	}
	return b, err
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	if err == nil && cr.limit > 0 && cr.n > cr.limit {
		return n, fmt.Errorf("gob: stream exceeds MaxBytes limit of %d", cr.limit)
	}
	return n, err
}

// bytesRead returns the total byte count consumed so far.
func (cr *countingReader) bytesRead() int64 { return cr.n }

// wrapWithLimit wraps r in a countingReader so that its byte counter is
// available to the stream decoder. If limit > 0, the counter also enforces
// the maximum.
func wrapWithLimit(r io.Reader, limit int64) *countingReader {
	var br byteReadReader
	if b, ok := r.(byteReadReader); ok {
		br = b
	} else {
		br = bufio.NewReader(r)
	}
	return &countingReader{r: br, limit: limit}
}

// DecoderFunc decodes the raw bytes of a GobEncoder, BinaryMarshaler, or
// TextMarshaler blob into a human-meaningful value.
//
// The returned value should be a simple Go type (string, int, float, map, etc.)
// suitable for display. It does not need to reconstruct the original Go type.
type DecoderFunc func([]byte) (any, error)

// Option is a functional option for [New].
type Option func(*Inspector)

// WithReadLimit sets the maximum total bytes read from a stream. Zero means no limit.
func WithReadLimit(n int64) Option {
	return func(ins *Inspector) { ins.maxBytes = n }
}

// WithTimeFormat sets the layout used to render time.Time opaque values.
// The layout must be a valid Go time format string (see the time package).
// Default: time.RFC3339Nano.
func WithTimeFormat(layout string) Option {
	return func(ins *Inspector) {
		ins.decoders["Time"] = makeTimeDecoder(layout)
	}
}

// WithSkipCorruptValues configures the inspector to continue past individual
// value-message decode failures instead of aborting the stream. Each skipped
// message is counted and available via [Stream.SkipCount]. Errors in type-
// definition messages remain fatal because they would leave the type registry
// inconsistent and subsequent values undecodable.
//
// Enable this when inspecting archived logs that may contain occasional bad
// records; leave it off for strict validation.
func WithSkipCorruptValues(b bool) Option {
	return func(ins *Inspector) { ins.skipCorruptValues = b }
}

// Inspector is the top-level entry point. It holds the opaque decoder registry
// and decoding options. Create one with [New].
type Inspector struct {
	decoders          map[string]DecoderFunc
	anonymousDecoders []DecoderFunc
	maxBytes          int64
	skipCorruptValues bool
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

// RegisterUnnamedDecoder appends dec to the list of decoders tried for opaque
// values whose gob wire type name is empty. Decoders are tried in registration
// order; the first one that returns a non-error result wins.
func (ins *Inspector) RegisterUnnamedDecoder(dec DecoderFunc) {
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
	r           *countingReader
	registry    map[int]wireTypeDef // type ID → wireTypeDef
	types       []TypeInfo          // accumulated in definition order
	byID        map[int]int         // type ID → index in types
	pendingRefs map[int][]*TypeRef  // type ID → refs awaiting that ID's name
	msgIdx      int                 // 0-based counter of messages fully read
	msgStart    int64               // byte offset of the current (or next) message start
	msgLen      int                 // body length of the current message
}

func newStreamDecoder(r *countingReader) *streamDecoder {
	return &streamDecoder{
		r:           r,
		registry:    make(map[int]wireTypeDef),
		byID:        make(map[int]int),
		pendingRefs: make(map[int][]*TypeRef),
	}
}

// firstUserTypeID is the lowest type ID the gob encoder assigns to
// user-defined types. Definitions below it would shadow builtin or
// bootstrap types and are rejected, matching the stdlib decoder.
const firstUserTypeID = 64

// typeDefID converts the negated raw wire ID of a type definition to an int
// type ID, guarding against the negation overflowing on math.MinInt64.
func typeDefID(rawID int64) (int, error) {
	if rawID == math.MinInt64 {
		return 0, fmt.Errorf("gob: type definition ID %d out of range", rawID)
	}
	return int(-rawID), nil
}

// readMessage reads the next length-prefixed message from the stream.
// Records msgStart (byte offset of the length prefix) and msgLen (body length)
// before returning. Returns (nil, io.EOF) when the stream is exhausted cleanly.
func (sd *streamDecoder) readMessage() ([]byte, error) {
	sd.msgStart = sd.r.bytesRead()
	n, err := decodeUint(sd.r)
	if err != nil {
		return nil, err // may be io.EOF — caller distinguishes
	}
	if n > 1<<26 {
		return nil, fmt.Errorf("gob: message length %d exceeds sanity limit", n)
	}
	if sd.r.limit > 0 && int64(n) > sd.r.limit-sd.r.n {
		return nil, fmt.Errorf("gob: message length %d exceeds MaxBytes limit of %d", n, sd.r.limit)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(sd.r, buf); err != nil {
		return nil, fmt.Errorf("gob: reading message body: %w", err)
	}
	sd.msgLen = int(n)
	return buf, nil
}

// nextRawMessage reads the next stream message and returns the raw signed
// type ID, a reader positioned at the message body (after the type ID), and
// a MessageInfo describing the framing.
// rawID < 0 means type definition; rawID > 0 means value.
// msgIdx and msgStart reflect the message that was just read; callers should
// invoke advanceMessage() once they are done processing it, so that subsequent
// wrapErr calls (e.g. for value-body decode errors) still cite this message.
func (sd *streamDecoder) nextRawMessage() (rawID int64, r *bytes.Reader, info MessageInfo, err error) {
	buf, err := sd.readMessage()
	if err != nil {
		return 0, nil, MessageInfo{}, err
	}
	info = MessageInfo{
		Index:   sd.msgIdx,
		Offset:  sd.msgStart,
		BodyLen: sd.msgLen,
		Body:    buf,
	}
	br := bytes.NewReader(buf)
	rawID, err = decodeInt(br)
	if err != nil {
		// Not wrapped with message context here: callers wrap once at the
		// point the error is surfaced, so it is not prefixed twice.
		return 0, nil, info, fmt.Errorf("gob: reading type ID: %w", err)
	}
	info.TypeID = int(rawID)
	return rawID, br, info, nil
}

// advanceMessage increments the per-stream message counter. Callers invoke
// this once they finish processing the message most recently returned by
// nextRawMessage, so that the *next* call to nextRawMessage starts tracking
// the following message.
func (sd *streamDecoder) advanceMessage() { sd.msgIdx++ }

// wrapErr annotates err with the current message index and byte offset for
// better diagnostics. Returns err unchanged when err is nil or io.EOF.
func (sd *streamDecoder) wrapErr(err error) error {
	return sd.wrapErrAt(sd.msgIdx, sd.msgStart, err)
}

// wrapErrAt is wrapErr with an explicit message index and offset, for callers
// that must cite a message other than the current one — e.g. a value whose
// interface fields pulled in continuation messages should still be reported
// at the value message's own position.
func (sd *streamDecoder) wrapErrAt(msgIdx int, msgStart int64, err error) error {
	if err == nil || err == io.EOF {
		return err
	}
	return fmt.Errorf("gob: message %d at offset %d: %w", msgIdx, msgStart, err)
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
//
// Definitions for reserved IDs (builtin and bootstrap types) and duplicate
// definitions are rejected, matching the stdlib decoder: accepting either
// would let a stream make TypeByID and Schema disagree with how values are
// actually decoded.
func (sd *streamDecoder) registerAndResolve(id int, def wireTypeDef) error {
	if id < firstUserTypeID {
		return fmt.Errorf("gob: type definition for reserved ID %d", id)
	}
	if _, dup := sd.registry[id]; dup {
		return fmt.Errorf("gob: duplicate definition for type ID %d", id)
	}
	sd.registry[id] = def
	ti, err := sd.wireTypeToTypeInfo(id, def)
	if err != nil {
		return fmt.Errorf("gob: converting wireType to TypeInfo for ID %d: %w", id, err)
	}
	// Back-fill the TypeRefs that were waiting for this ID. Duplicate IDs are
	// rejected above, so this is the only definition the waiters can ever get.
	if name := wireTypeDefName(def); name != "" {
		for _, ref := range sd.pendingRefs[id] {
			ref.Name = name
		}
	}
	delete(sd.pendingRefs, id)
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
// if the type is already known (builtin or previously defined). Refs to
// not-yet-defined IDs are indexed in pendingRefs so registerAndResolve can
// fill the name in O(1) when the definition arrives.
func (sd *streamDecoder) resolveRef(id int) *TypeRef {
	ref := &TypeRef{ID: id}
	if name, ok := builtinTypeName(id); ok {
		ref.Name = name
		return ref
	}
	if def, ok := sd.registry[id]; ok {
		ref.Name = wireTypeDefName(def)
		return ref
	}
	sd.pendingRefs[id] = append(sd.pendingRefs[id], ref)
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
