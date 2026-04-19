package gobspect

// Value is the sealed interface implemented by all AST node types.
// Use a type switch to dispatch on the concrete type.
type Value interface {
	gobValue()
	// TypeID returns the stream-scoped type ID for composite values
	// (StructValue, MapValue, SliceValue, ArrayValue).
	// Scalar and opaque values return 0.
	TypeID() int
}

// StructValue represents a decoded gob struct.
type StructValue struct {
	TypeName  string
	GobTypeID int
	Fields    []Field
}

// Field is a single named field within a StructValue.
type Field struct {
	Name  string
	Value Value
}

// MapValue represents a decoded gob map.
type MapValue struct {
	TypeName  string
	GobTypeID int
	KeyType   string // descriptive label for the key type
	ElemType  string // descriptive label for the element type
	Entries   []MapEntry
}

// MapEntry is a single key-value pair within a MapValue.
type MapEntry struct {
	Key   Value
	Value Value
}

// SliceValue represents a decoded gob slice.
type SliceValue struct {
	TypeName  string
	GobTypeID int
	ElemType  string
	Elems     []Value
}

// ArrayValue represents a decoded gob array.
type ArrayValue struct {
	TypeName  string
	GobTypeID int
	ElemType  string
	Len       int
	Elems     []Value
}

// IntValue holds a decoded signed integer.
type IntValue struct{ V int64 }

// UintValue holds a decoded unsigned integer.
type UintValue struct{ V uint64 }

// FloatValue holds a decoded floating-point number.
type FloatValue struct{ V float64 }

// ComplexValue holds a decoded complex number.
type ComplexValue struct{ Real, Imag float64 }

// BoolValue holds a decoded boolean.
type BoolValue struct{ V bool }

// StringValue holds a decoded string.
type StringValue struct{ V string }

// BytesValue holds a decoded []byte.
type BytesValue struct{ V []byte }

// NilValue represents a nil pointer or nil interface.
type NilValue struct{}

// InterfaceValue represents a decoded interface value.
type InterfaceValue struct {
	TypeName string // concrete type name from the stream
	Value    Value  // the concrete value, or NilValue for nil
}

// OpaqueValue holds the raw bytes for a GobEncoder, BinaryMarshaler, or
// TextMarshaler value, along with any best-effort decoded form.
type OpaqueValue struct {
	TypeName  string // from CommonType.Name
	GobTypeID int    // stream-scoped type ID; use with Stream.TypeByID
	Encoding  string // "gob", "binary", or "text"
	Raw       []byte // the undecoded blob
	Decoded   any    // best-effort decoded form, nil if no decoder matched
}

func (StructValue) gobValue()    {}
func (MapValue) gobValue()       {}
func (SliceValue) gobValue()     {}
func (ArrayValue) gobValue()     {}
func (IntValue) gobValue()       {}
func (UintValue) gobValue()      {}
func (FloatValue) gobValue()     {}
func (ComplexValue) gobValue()   {}
func (BoolValue) gobValue()      {}
func (StringValue) gobValue()    {}
func (BytesValue) gobValue()     {}
func (NilValue) gobValue()       {}
func (InterfaceValue) gobValue() {}
func (OpaqueValue) gobValue()    {}

// TypeID implementations — composites return their stream-scoped type ID;
// scalars and opaque values return 0.
func (v StructValue) TypeID() int  { return v.GobTypeID }
func (v MapValue) TypeID() int     { return v.GobTypeID }
func (v SliceValue) TypeID() int   { return v.GobTypeID }
func (v ArrayValue) TypeID() int   { return v.GobTypeID }
func (IntValue) TypeID() int       { return 0 }
func (UintValue) TypeID() int      { return 0 }
func (FloatValue) TypeID() int     { return 0 }
func (ComplexValue) TypeID() int   { return 0 }
func (BoolValue) TypeID() int      { return 0 }
func (StringValue) TypeID() int    { return 0 }
func (BytesValue) TypeID() int     { return 0 }
func (NilValue) TypeID() int       { return 0 }
func (InterfaceValue) TypeID() int  { return 0 }
func (v OpaqueValue) TypeID() int   { return v.GobTypeID }

// ValueKind returns a short lowercase string identifying the concrete type of v:
// "struct", "map", "slice", "array", "int", "uint", "float", "complex",
// "bool", "string", "bytes", "nil", "interface", "opaque", or "unknown".
func ValueKind(v Value) string {
	switch v.(type) {
	case StructValue:
		return "struct"
	case MapValue:
		return "map"
	case SliceValue:
		return "slice"
	case ArrayValue:
		return "array"
	case IntValue:
		return "int"
	case UintValue:
		return "uint"
	case FloatValue:
		return "float"
	case ComplexValue:
		return "complex"
	case BoolValue:
		return "bool"
	case StringValue:
		return "string"
	case BytesValue:
		return "bytes"
	case NilValue:
		return "nil"
	case InterfaceValue:
		return "interface"
	case OpaqueValue:
		return "opaque"
	default:
		return "unknown"
	}
}

// — Type metadata ——————————————————————————————————————————————————————————

// TypeKind classifies a type definition found in a gob stream.
type TypeKind int

const (
	KindStruct TypeKind = iota
	KindMap
	KindSlice
	KindArray
	KindGobEncoder
	KindBinaryMarshaler
	KindTextMarshaler
)

func (k TypeKind) String() string {
	switch k {
	case KindStruct:
		return "struct"
	case KindMap:
		return "map"
	case KindSlice:
		return "slice"
	case KindArray:
		return "array"
	case KindGobEncoder:
		return "gob-encoder"
	case KindBinaryMarshaler:
		return "binary-marshaler"
	case KindTextMarshaler:
		return "text-marshaler"
	default:
		return "unknown"
	}
}

// TypeInfo describes a single type definition encountered in a gob stream.
type TypeInfo struct {
	ID     int
	Name   string
	Kind   TypeKind
	Fields []FieldInfo // non-nil only for KindStruct
	Key    *TypeRef    // non-nil only for KindMap
	Elem   *TypeRef    // non-nil for KindMap, KindSlice, KindArray
	Len    int         // non-zero only for KindArray
}

// FieldInfo describes one field of a struct type.
type FieldInfo struct {
	Name   string
	TypeID int
}

// TypeRef is a reference to another type by its stream-scoped ID.
type TypeRef struct {
	ID   int
	Name string // resolved name if available in the registry
}
