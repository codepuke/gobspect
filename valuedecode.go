package gobspect

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"math/bits"
)

// messageReader is a mutable io.ByteReader whose underlying source can be
// switched mid-stream. decodeInterface uses this to span the boundary between
// the current outer message and the next one when decoding interface values.
type messageReader struct {
	cur io.ByteReader
}

func (mr *messageReader) ReadByte() (byte, error) {
	return mr.cur.ReadByte()
}

// srcRemaining reports the number of unread bytes in r's current source, or
// -1 when the source cannot say. Message bodies are fully buffered in a
// bytes.Reader, so within a message the count is exact; it is used to
// validate wire-supplied lengths before allocating.
func srcRemaining(r io.ByteReader) int {
	if mr, ok := r.(*messageReader); ok {
		r = mr.cur
	}
	if l, ok := r.(interface{ Len() int }); ok {
		return l.Len()
	}
	return -1
}

// maxDecodeDepth bounds value nesting. A hostile stream can define a
// self-referential type graph whose value encoding recurses once per input
// byte; without a bound that exhausts the goroutine stack, which is a fatal,
// unrecoverable runtime error rather than a panic.
const maxDecodeDepth = 10000

// valueDecoder decodes gob value messages into the Value AST.
// It shares the registry with the streamDecoder so type definitions
// registered during stream processing are visible here.
type valueDecoder struct {
	ins      *Inspector
	sd       *streamDecoder
	registry map[int]wireTypeDef
	depth    int
}

func newValueDecoder(ins *Inspector, sd *streamDecoder) *valueDecoder {
	return &valueDecoder{ins: ins, sd: sd, registry: sd.registry}
}

// decodeTopLevelValue decodes a top-level gob value from the message body.
//
// For struct types the field-delta sequence begins immediately.
// For all other types gob wraps the value in a synthetic single-field struct:
// uint(0) is the singleton "delta" (field 0 from position 0), followed by
// the encoded value. See encoding/gob encode.go, encodeSingle.
func (vd *valueDecoder) decodeTopLevelValue(typeID int, r *messageReader) (Value, error) {
	// Struct types start directly with field deltas — no singleton wrapper.
	if def, ok := vd.registry[typeID]; ok && def.StructT != nil {
		return vd.decodeStructValue(typeID, def.StructT, r)
	}

	// Non-struct: consume the singleton uint(0) prefix.
	delta, err := decodeUint(r)
	if err != nil {
		return nil, fmt.Errorf("gob: reading singleton prefix: %w", err)
	}
	if delta != 0 {
		return nil, fmt.Errorf("gob: expected singleton delta 0, got %d", delta)
	}
	return vd.decodeFieldValue(typeID, r)
}

// decodeFieldValue decodes a value of the given type from r.
// This is used for struct fields, slice/map/array elements, and the
// body of a singleton value (after the uint(0) prefix is stripped).
// It does NOT handle the singleton uint(0) prefix itself.
func (vd *valueDecoder) decodeFieldValue(typeID int, r *messageReader) (Value, error) {
	if vd.depth >= maxDecodeDepth {
		return nil, fmt.Errorf("gob: value nesting exceeds %d levels", maxDecodeDepth)
	}
	vd.depth++
	defer func() { vd.depth-- }()

	switch typeID {
	case 1:
		return vd.decodeBool(r)
	case 2:
		return vd.decodeSignedInt(r)
	case 3:
		return vd.decodeUnsignedInt(r)
	case 4:
		return vd.decodeFloat(r)
	case 5:
		return vd.decodeBytes(r)
	case 6:
		return vd.decodeString(r)
	case 7:
		return vd.decodeComplex(r)
	case 8:
		return vd.decodeInterface(r)
	}

	def, ok := vd.registry[typeID]
	if !ok {
		return nil, fmt.Errorf("gob: unknown type ID %d", typeID)
	}
	switch {
	case def.StructT != nil:
		return vd.decodeStructValue(typeID, def.StructT, r)
	case def.SliceT != nil:
		return vd.decodeSliceValue(typeID, def.SliceT, r)
	case def.MapT != nil:
		return vd.decodeMapValue(typeID, def.MapT, r)
	case def.ArrayT != nil:
		return vd.decodeArrayValue(typeID, def.ArrayT, r)
	case def.GobEncoderT != nil:
		return vd.decodeOpaqueValue(typeID, def.GobEncoderT.Common.Name, "gob", r)
	case def.BinaryMarshalerT != nil:
		return vd.decodeOpaqueValue(typeID, def.BinaryMarshalerT.Common.Name, "binary", r)
	case def.TextMarshalerT != nil:
		return vd.decodeOpaqueValue(typeID, def.TextMarshalerT.Common.Name, "text", r)
	default:
		return nil, fmt.Errorf("gob: wireTypeDef for ID %d has no recognised variant", typeID)
	}
}

// — Primitive type decoders ——————————————————————————————————————————————

// decodeBool reads a gob bool field value (uint, 0=false, nonzero=true).
func (vd *valueDecoder) decodeBool(r io.ByteReader) (Value, error) {
	u, err := decodeUint(r)
	return BoolValue{u != 0}, err
}

// decodeSignedInt reads a gob signed integer (zig-zag encoded).
func (vd *valueDecoder) decodeSignedInt(r io.ByteReader) (Value, error) {
	v, err := decodeInt(r)
	return IntValue{v}, err
}

// decodeUnsignedInt reads a gob unsigned integer.
func (vd *valueDecoder) decodeUnsignedInt(r io.ByteReader) (Value, error) {
	v, err := decodeUint(r)
	return UintValue{v}, err
}

// gobFloatBits decodes a gob-encoded float64.
// Gob transmits floats as byte-reversed IEEE 754 uint64 so that the
// exponent byte arrives first, giving better varint compression for
// integer-valued floats.
func gobFloatBits(r io.ByteReader) (float64, error) {
	u, err := decodeUint(r)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(bits.ReverseBytes64(u)), nil
}

func (vd *valueDecoder) decodeFloat(r io.ByteReader) (Value, error) {
	f, err := gobFloatBits(r)
	return FloatValue{f}, err
}

// decodeComplex reads a gob complex value: real part then imaginary part,
// each a byte-reversed IEEE 754 float64.
func (vd *valueDecoder) decodeComplex(r io.ByteReader) (Value, error) {
	re, err := gobFloatBits(r)
	if err != nil {
		return nil, err
	}
	im, err := gobFloatBits(r)
	return ComplexValue{re, im}, err
}

// decodeString reads a gob-encoded string (uint length + bytes).
func (vd *valueDecoder) decodeString(r io.ByteReader) (Value, error) {
	s, err := decodeString(r)
	return StringValue{s}, err
}

// decodeBytes reads a gob-encoded []byte (uint length + raw bytes).
func (vd *valueDecoder) decodeBytes(r io.ByteReader) (Value, error) {
	n, err := decodeUint(r)
	if err != nil {
		return nil, err
	}
	b, err := readBytes(r, n)
	return BytesValue{b}, err
}

// — Composite type decoders ——————————————————————————————————————————————

// decodeStructValue decodes a gob struct value.
//
// Struct fields are encoded sparsely: each present field is preceded by a
// uint delta (gap from the previous 0-based field index, starting from -1).
// Zero-valued fields are omitted. A delta of 0 terminates the sequence.
func (vd *valueDecoder) decodeStructValue(typeID int, def *wireStructType, r *messageReader) (Value, error) {
	sv := StructValue{
		TypeName:  def.Common.Name,
		GobTypeID: typeID,
	}
	fieldIdx := -1 // mirrors encoder's state.fieldnum, starts at -1
	for {
		delta, err := decodeUint(r)
		if err != nil {
			return nil, fmt.Errorf("gob: reading field delta in %q: %w", def.Common.Name, err)
		}
		if delta == 0 {
			break
		}
		// Guard against integer overflow: a delta larger than the total
		// field count can never be valid, and converting it to int would
		// wrap to a negative value and bypass the bounds check below.
		if delta > uint64(len(def.Fields)) {
			return nil, fmt.Errorf("gob: field delta %d out of range for struct %q (%d fields defined)", delta, def.Common.Name, len(def.Fields))
		}
		fieldIdx += int(delta)
		if fieldIdx >= len(def.Fields) {
			return nil, fmt.Errorf("gob: field index %d out of range for struct %q (%d fields defined)", fieldIdx, def.Common.Name, len(def.Fields))
		}
		fd := def.Fields[fieldIdx]
		fv, err := vd.decodeFieldValue(fd.ID, r)
		if err != nil {
			return nil, fmt.Errorf("gob: decoding field %q of %q: %w", fd.Name, def.Common.Name, err)
		}
		sv.Fields = append(sv.Fields, Field{Name: fd.Name, Value: fv})
	}
	return sv, nil
}

// decodeSliceValue decodes a gob slice: uint(count) then each element.
// Elements are encoded without field deltas — just their raw value encodings.
func (vd *valueDecoder) decodeSliceValue(typeID int, def *wireSliceType, r *messageReader) (Value, error) {
	count, err := decodeUint(r)
	if err != nil {
		return nil, err
	}
	if count > 1<<30 {
		return nil, fmt.Errorf("gob: slice element count %d exceeds limit", count)
	}
	sv := SliceValue{
		TypeName:  def.Common.Name,
		GobTypeID: typeID,
		ElemType:  vd.typeLabel(def.Elem),
		// The count comes off the wire; grow with append so a hostile count
		// cannot force a giant allocation before any element bytes exist.
		Elems: make([]Value, 0, min(count, 4096)),
	}
	for i := uint64(0); i < count; i++ {
		el, err := vd.decodeFieldValue(def.Elem, r)
		if err != nil {
			return nil, fmt.Errorf("gob: decoding slice element %d: %w", i, err)
		}
		sv.Elems = append(sv.Elems, el)
	}
	return sv, nil
}

// decodeMapValue decodes a gob map: uint(count) then alternating key/value pairs.
func (vd *valueDecoder) decodeMapValue(typeID int, def *wireMapType, r *messageReader) (Value, error) {
	count, err := decodeUint(r)
	if err != nil {
		return nil, err
	}
	if count > 1<<30 {
		return nil, fmt.Errorf("gob: map entry count %d exceeds limit", count)
	}
	mv := MapValue{
		TypeName:  def.Common.Name,
		GobTypeID: typeID,
		KeyType:   vd.typeLabel(def.Key),
		ElemType:  vd.typeLabel(def.Elem),
		// Wire-supplied count: grow with append, as in decodeSliceValue.
		Entries: make([]MapEntry, 0, min(count, 4096)),
	}
	for i := uint64(0); i < count; i++ {
		key, err := vd.decodeFieldValue(def.Key, r)
		if err != nil {
			return nil, fmt.Errorf("gob: decoding map key %d: %w", i, err)
		}
		val, err := vd.decodeFieldValue(def.Elem, r)
		if err != nil {
			return nil, fmt.Errorf("gob: decoding map value %d: %w", i, err)
		}
		mv.Entries = append(mv.Entries, MapEntry{Key: key, Value: val})
	}
	return mv, nil
}

// decodeArrayValue decodes a gob array: uint(count) then each element.
// The count should equal def.Len but we decode whatever count is in the stream.
func (vd *valueDecoder) decodeArrayValue(typeID int, def *wireArrayType, r *messageReader) (Value, error) {
	count, err := decodeUint(r)
	if err != nil {
		return nil, err
	}
	if count > 1<<30 {
		return nil, fmt.Errorf("gob: array element count %d exceeds limit", count)
	}
	av := ArrayValue{
		TypeName:  def.Common.Name,
		GobTypeID: typeID,
		ElemType:  vd.typeLabel(def.Elem),
		Len:       def.Len,
		// Wire-supplied count: grow with append, as in decodeSliceValue.
		Elems: make([]Value, 0, min(count, 4096)),
	}
	for i := uint64(0); i < count; i++ {
		el, err := vd.decodeFieldValue(def.Elem, r)
		if err != nil {
			return nil, fmt.Errorf("gob: decoding array element %d: %w", i, err)
		}
		av.Elems = append(av.Elems, el)
	}
	return av, nil
}

// decodeOpaqueValue reads an opaque blob (GobEncoder, BinaryMarshaler, or
// TextMarshaler). The wire encoding is uint(length) + raw bytes.
func (vd *valueDecoder) decodeOpaqueValue(typeID int, typeName, encoding string, r io.ByteReader) (Value, error) {
	n, err := decodeUint(r)
	if err != nil {
		return nil, err
	}
	raw, err := readBytes(r, n)
	if err != nil {
		return nil, err
	}
	ov := OpaqueValue{
		TypeName:  typeName,
		GobTypeID: typeID,
		Encoding:  encoding,
		Raw:       raw,
	}
	if encoding == "text" {
		// TextMarshaler blobs are always valid UTF-8 strings by contract.
		ov.Decoded = string(raw)
	} else if typeName == "" {
		// Empty type name: try anonymous decoders in registration order,
		// stopping at the first one that returns a usable result.
		for _, dec := range vd.ins.anonymousDecoders {
			if decoded, decErr := safeDecode(dec, raw); decErr == nil && decoded != nil {
				ov.Decoded = decoded
				break
			}
		}
	} else if dec, ok := vd.ins.decoders[typeName]; ok {
		if decoded, decErr := safeDecode(dec, raw); decErr == nil {
			ov.Decoded = decoded
		}
	}
	return ov, nil
}

// safeDecode invokes an opaque DecoderFunc, converting a panic into an error.
// The dispatch key is the wire-supplied type name, so a hostile stream can
// route arbitrary blobs to any registered decoder; a decoder tripping over
// such input must degrade to raw-bytes display, not take the process down.
func safeDecode(dec DecoderFunc, raw []byte) (decoded any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("gob: opaque decoder panicked: %v", p)
		}
	}()
	return dec(raw)
}

// decodeInterface decodes a gob interface-typed field.
//
// Wire format for a non-nil interface:
//
//	uint(nameLen) name [(-typeId wireTypeDef)*] (+typeId) uint(valueLen) value
//
// The inline type definitions (negative IDs) occur within the current message
// body. The positive concrete type ID and the value bytes may be in the next
// outer stream message (a continuation message sent by the encoder).
// The continuation message body format is: (+typeId) uint(valueLen) value [remaining outer struct bytes].
//
// After this function returns, r is positioned at whatever bytes follow the
// value in the stream (e.g. the struct terminator for the enclosing struct).
func (vd *valueDecoder) decodeInterface(r *messageReader) (Value, error) {
	nameLen, err := decodeUint(r)
	if err != nil {
		return nil, err
	}
	if nameLen == 0 {
		return InterfaceValue{Value: NilValue{}}, nil
	}
	// The stdlib decoder caps registered type names at 1024 bytes; anything
	// longer cannot name a real registered type.
	if nameLen > 1024 {
		return nil, fmt.Errorf("gob: interface type name length %d exceeds limit", nameLen)
	}

	nameBytes, err := readBytes(r, nameLen)
	if err != nil {
		return nil, err
	}
	typeName := string(nameBytes)

	// Process the type sequence: zero or more inline type definitions
	// (negative type IDs followed by wireType bodies) embedded in the current
	// message body, then the positive concrete type ID.
	// The positive ID may arrive from:
	//   (a) the current message body (uncommon), or
	//   (b) a new outer stream message (the normal case for interface values).
	var concreteTypeID int
	for {
		id, err := decodeInt(r)
		if err == io.EOF {
			// Current message body exhausted; read the next outer message.
			rawID, msgR, _, streamErr := vd.sd.nextRawMessage()
			if streamErr != nil {
				return nil, fmt.Errorf("gob: reading continuation for interface %q: %w", typeName, streamErr)
			}
			if rawID < 0 {
				// Separate type def outer message: process and continue.
				defID, err2 := typeDefID(rawID)
				if err2 == nil {
					err2 = vd.sd.processTypeDef(defID, msgR)
				}
				if err2 != nil {
					return nil, err2
				}
				vd.sd.advanceMessage()
				r.cur = msgR // now exhausted; loop will read another message
				continue
			}
			// Positive type ID: the rest of this message is our value bytes.
			vd.sd.advanceMessage()
			r.cur = msgR
			concreteTypeID = int(rawID)
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gob: reading interface type sequence for %q: %w", typeName, err)
		}
		if id >= 0 {
			// Concrete type ID found directly in the current message body.
			concreteTypeID = int(id)
			break
		}
		// Negative: inline type def in the current message body.
		// Read wireType body and register it with incremental resolution.
		inlineID, err := typeDefID(id)
		if err != nil {
			return nil, err
		}
		def, defErr := decodeWireType(r)
		if defErr != nil {
			return nil, fmt.Errorf("gob: decoding inline wireType id %d for interface %q: %w", inlineID, typeName, defErr)
		}
		if err := vd.sd.registerAndResolve(inlineID, def); err != nil {
			return nil, fmt.Errorf("gob: registering inline wireType id %d for interface %q: %w", inlineID, typeName, err)
		}
	}

	// The encoder prefixes the concrete value bytes with their length.
	valueLen, err := decodeUint(r)
	if err != nil {
		return nil, fmt.Errorf("gob: reading interface value length for %q: %w", typeName, err)
	}
	if valueLen == 0 {
		return nil, fmt.Errorf("gob: interface %q has zero-length value body", typeName)
	}
	valueBytes, err := readBytes(r, valueLen)
	if err != nil {
		return nil, fmt.Errorf("gob: reading interface value body for %q: %w", typeName, err)
	}

	// Decode the concrete value using the top-level rules.
	concreteR := &messageReader{cur: bytes.NewReader(valueBytes)}
	concrete, err := vd.decodeTopLevelValue(concreteTypeID, concreteR)
	if err != nil {
		// Best-effort: preserve the type name even on decode failure.
		return InterfaceValue{TypeName: typeName, Value: NilValue{}},
			fmt.Errorf("gob: decoding interface concrete value %q: %w", typeName, err)
	}
	return InterfaceValue{TypeName: typeName, Value: concrete}, nil
}

// — Helper ———————————————————————————————————————————————————————————————

// typeLabel returns a descriptive string for the given type ID.
// Used to populate ElemType/KeyType labels in composite value nodes.
func (vd *valueDecoder) typeLabel(id int) string {
	if name, ok := builtinTypeName(id); ok {
		return name
	}
	if def, ok := vd.registry[id]; ok {
		if name := wireTypeDefName(def); name != "" {
			return name
		}
	}
	return fmt.Sprintf("type%d", id)
}
