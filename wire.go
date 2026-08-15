package gobspect

import (
	"fmt"
	"io"
)

// decodeUint reads a gob-encoded unsigned integer from r.
//
// Gob varint encoding: if the first byte b < 128, the value is b.
// Otherwise b is the negated byte count (e.g. 0xFF → 1 byte, 0xFE → 2 bytes),
// and the value follows as a big-endian unsigned integer.
func decodeUint(r io.ByteReader) (uint64, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if b <= 0x7F {
		return uint64(b), nil
	}
	n := -int(int8(b))
	if n > 8 {
		return 0, fmt.Errorf("gob: encoded unsigned integer out of range")
	}
	var x uint64
	for range n {
		b, err = r.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("gob: reading uint byte: %w", err)
		}
		x = x<<8 | uint64(b)
	}
	return x, nil
}

// decodeInt reads a gob-encoded signed integer from r.
//
// Signed integers are zig-zag encoded over unsigned: non-negative x → 2*x,
// negative x → ~(2*x). Decoded by reversing the transformation.
func decodeInt(r io.ByteReader) (int64, error) {
	u, err := decodeUint(r)
	if err != nil {
		return 0, err
	}
	x := int64(u >> 1)
	if u&1 != 0 {
		x = ^x
	}
	return x, nil
}

// readBytes reads exactly n bytes from r, one byte at a time.
//
// The length n comes off the wire, so it is validated against the bytes
// actually remaining in the source before the full buffer is allocated;
// when the remaining count is unknown the buffer grows as bytes arrive.
// Either way a hostile length cannot force a large allocation up front.
func readBytes(r io.ByteReader, n uint64) ([]byte, error) {
	if n > 1<<30 {
		return nil, fmt.Errorf("gob: size too large: %d", n)
	}
	capHint := uint64(4096)
	if rem := srcRemaining(r); rem >= 0 {
		if n > uint64(rem) {
			return nil, fmt.Errorf("gob: size %d exceeds %d remaining message bytes", n, rem)
		}
		capHint = n
	}
	buf := make([]byte, 0, min(n, capHint))
	for i := uint64(0); i < n; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("gob: reading byte %d of %d: %w", i, n, err)
		}
		buf = append(buf, b)
	}
	return buf, nil
}

// decodeString reads a gob-encoded string (uint length + bytes).
func decodeString(r io.ByteReader) (string, error) {
	n, err := decodeUint(r)
	if err != nil {
		return "", err
	}
	b, err := readBytes(r, n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// — bootstrap wireType structs —————————————————————————————————————————————
//
// These mirror the Go standard library's internal gob bootstrap types.
// They are used only during type-definition decoding.
//
// Important: gob does NOT flatten embedded structs. CommonType is encoded
// as a nested struct at field 1 of each containing type, not as inlined
// fields. See docs/wire-format.md and the reference implementation.

type wireCommonType struct {
	Name string
	ID   int
}

type wireArrayType struct {
	Common wireCommonType
	Elem   int
	Len    int
}

type wireSliceType struct {
	Common wireCommonType
	Elem   int
}

type wireMapType struct {
	Common wireCommonType
	Key    int
	Elem   int
}

type wireFieldType struct {
	Name string
	ID   int
}

type wireStructType struct {
	Common wireCommonType
	Fields []wireFieldType
}

type wireGobEncoderType struct {
	Common wireCommonType
}

// wireTypeDef holds one type definition as decoded from the stream.
// Exactly one of the pointer fields is non-nil.
type wireTypeDef struct {
	ArrayT           *wireArrayType
	SliceT           *wireSliceType
	StructT          *wireStructType
	MapT             *wireMapType
	GobEncoderT      *wireGobEncoderType
	BinaryMarshalerT *wireGobEncoderType
	TextMarshalerT   *wireGobEncoderType
}

// — struct reader ——————————————————————————————————————————————————————————

// structReader assists in reading a gob-encoded sparse struct.
// Gob encodes only non-zero fields, each preceded by a delta from the
// previous field number (1-based). A delta of 0 signals end of struct.
type structReader struct {
	r     io.ByteReader
	field int // last absolute field number read
	err   error
}

func newStructReader(r io.ByteReader) *structReader {
	return &structReader{r: r}
}

// nextField reads the next field delta and returns the absolute 1-based field
// number. Returns 0 at end of struct, -1 on error.
func (sr *structReader) nextField() int {
	if sr.err != nil {
		return -1
	}
	delta, err := decodeUint(sr.r)
	if err != nil {
		sr.err = err
		return -1
	}
	if delta == 0 {
		return 0
	}
	// Bootstrap structs have at most a handful of fields; a huge delta can
	// only come from a corrupt stream, and converting it to int unchecked
	// would wrap negative and masquerade as end-of-struct with no error.
	if delta > 1<<16 {
		sr.err = fmt.Errorf("gob: field delta %d out of range in bootstrap struct", delta)
		return -1
	}
	sr.field += int(delta)
	return sr.field
}

func (sr *structReader) uint() uint64 {
	if sr.err != nil {
		return 0
	}
	v, err := decodeUint(sr.r)
	if err != nil {
		sr.err = err
	}
	return v
}

func (sr *structReader) int() int64 {
	if sr.err != nil {
		return 0
	}
	v, err := decodeInt(sr.r)
	if err != nil {
		sr.err = err
	}
	return v
}

func (sr *structReader) string() string {
	if sr.err != nil {
		return ""
	}
	v, err := decodeString(sr.r)
	if err != nil {
		sr.err = err
	}
	return v
}

func (sr *structReader) skipUnknownField() {
	// We can't safely skip a gob value without its type descriptor.
	// Mark the error so callers surface the problem immediately.
	if sr.err == nil {
		sr.err = fmt.Errorf("gob: unexpected field %d in bootstrap struct", sr.field)
	}
}

// — per-struct decoders ————————————————————————————————————————————————————
//
// Gob field layout for bootstrap types (CommonType is always field 1,
// encoded as a nested struct — NOT flattened into the outer struct):
//
//   CommonType:     {1:Name string, 2:Id typeId}
//   arrayType:      {1:CommonType, 2:Elem typeId, 3:Len int}
//   sliceType:      {1:CommonType, 2:Elem typeId}
//   mapType:        {1:CommonType, 2:Key typeId, 3:Elem typeId}
//   structType:     {1:CommonType, 2:Field []fieldType}
//   gobEncoderType: {1:CommonType}
//   fieldType:      {1:Name string, 2:Id typeId}

func decodeCommonType(r io.ByteReader) (wireCommonType, error) {
	sr := newStructReader(r)
	var ct wireCommonType
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		switch field {
		case 1:
			ct.Name = sr.string()
		case 2:
			ct.ID = int(sr.int())
		default:
			sr.skipUnknownField()
		}
	}
	return ct, sr.err
}

func decodeWireArrayType(r io.ByteReader) (*wireArrayType, error) {
	sr := newStructReader(r)
	t := new(wireArrayType)
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		var err error
		switch field {
		case 1:
			t.Common, err = decodeCommonType(r)
			if err != nil {
				return nil, err
			}
		case 2:
			t.Elem = int(sr.int())
		case 3:
			t.Len = int(sr.int())
		default:
			sr.skipUnknownField()
		}
	}
	return t, sr.err
}

func decodeWireSliceType(r io.ByteReader) (*wireSliceType, error) {
	sr := newStructReader(r)
	t := new(wireSliceType)
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		var err error
		switch field {
		case 1:
			t.Common, err = decodeCommonType(r)
			if err != nil {
				return nil, err
			}
		case 2:
			t.Elem = int(sr.int())
		default:
			sr.skipUnknownField()
		}
	}
	return t, sr.err
}

func decodeWireMapType(r io.ByteReader) (*wireMapType, error) {
	sr := newStructReader(r)
	t := new(wireMapType)
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		var err error
		switch field {
		case 1:
			t.Common, err = decodeCommonType(r)
			if err != nil {
				return nil, err
			}
		case 2:
			t.Key = int(sr.int())
		case 3:
			t.Elem = int(sr.int())
		default:
			sr.skipUnknownField()
		}
	}
	return t, sr.err
}

func decodeWireFieldType(r io.ByteReader) (wireFieldType, error) {
	sr := newStructReader(r)
	var ft wireFieldType
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		switch field {
		case 1:
			ft.Name = sr.string()
		case 2:
			ft.ID = int(sr.int())
		default:
			sr.skipUnknownField()
		}
	}
	return ft, sr.err
}

func decodeWireStructType(r io.ByteReader) (*wireStructType, error) {
	sr := newStructReader(r)
	t := new(wireStructType)
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		var err error
		switch field {
		case 1:
			t.Common, err = decodeCommonType(r)
			if err != nil {
				return nil, err
			}
		case 2:
			count := sr.uint()
			if sr.err != nil {
				break
			}
			if count > 1<<16 {
				sr.err = fmt.Errorf("gob: struct field count %d exceeds limit", count)
				break
			}
			t.Fields = make([]wireFieldType, count)
			for i := range t.Fields {
				t.Fields[i], err = decodeWireFieldType(r)
				if err != nil {
					sr.err = err
					break
				}
			}
		default:
			sr.skipUnknownField()
		}
	}
	return t, sr.err
}

func decodeWireGobEncoderType(r io.ByteReader) (*wireGobEncoderType, error) {
	sr := newStructReader(r)
	t := new(wireGobEncoderType)
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		var err error
		switch field {
		case 1:
			t.Common, err = decodeCommonType(r)
			if err != nil {
				return nil, err
			}
		default:
			sr.skipUnknownField()
		}
	}
	return t, sr.err
}

// decodeWireType decodes a gob-encoded wireType struct from r.
// The wireType struct has 7 optional pointer fields (exactly one is set).
// Field deltas: 1=ArrayT, 2=SliceT, 3=StructT, 4=MapT,
//
//	5=GobEncoderT, 6=BinaryMarshalerT, 7=TextMarshalerT.
func decodeWireType(r io.ByteReader) (wireTypeDef, error) {
	sr := newStructReader(r)
	var def wireTypeDef
	for field := sr.nextField(); field > 0; field = sr.nextField() {
		if sr.err != nil {
			break
		}
		var err error
		switch field {
		case 1:
			def.ArrayT, err = decodeWireArrayType(r)
		case 2:
			def.SliceT, err = decodeWireSliceType(r)
		case 3:
			def.StructT, err = decodeWireStructType(r)
		case 4:
			def.MapT, err = decodeWireMapType(r)
		case 5:
			def.GobEncoderT, err = decodeWireGobEncoderType(r)
		case 6:
			def.BinaryMarshalerT, err = decodeWireGobEncoderType(r)
		case 7:
			def.TextMarshalerT, err = decodeWireGobEncoderType(r)
		default:
			sr.skipUnknownField()
		}
		if err != nil {
			return wireTypeDef{}, fmt.Errorf("gob: decoding wireType field %d: %w", field, err)
		}
	}
	return def, sr.err
}
