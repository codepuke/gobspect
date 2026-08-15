package gobspect

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"fmt"
	"strings"
)

// Equal reports whether a and b are structurally equal Values. Equality is
// strict: the two kinds must match, composite shapes must line up exactly, and
// primitives compare by native value. Cross-kind numeric equivalence
// (e.g. IntValue{5} vs FloatValue{5}) returns false — use [CompareValues] if
// you need the permissive numeric coercion.
//
// InterfaceValue wrappers are unwrapped on both sides before comparison. An
// InterfaceValue's outer TypeName does not participate in equality: only the
// inner concrete value matters.
//
// Floats compare structurally, not by IEEE semantics: NaN equals NaN (and a
// complex value with NaN parts equals its bitwise twin). Anything else would
// make a value unequal to itself and report phantom differences when a stream
// is diffed against an identical copy.
func Equal(a, b Value) bool {
	if iv, ok := a.(InterfaceValue); ok {
		a = iv.Value
	}
	if iv, ok := b.(InterfaceValue); ok {
		b = iv.Value
	}
	switch av := a.(type) {
	case NilValue:
		_, ok := b.(NilValue)
		return ok
	case BoolValue:
		bv, ok := b.(BoolValue)
		return ok && av.V == bv.V
	case IntValue:
		bv, ok := b.(IntValue)
		return ok && av.V == bv.V
	case UintValue:
		bv, ok := b.(UintValue)
		return ok && av.V == bv.V
	case FloatValue:
		bv, ok := b.(FloatValue)
		return ok && floatEq(av.V, bv.V)
	case ComplexValue:
		bv, ok := b.(ComplexValue)
		return ok && floatEq(av.Real, bv.Real) && floatEq(av.Imag, bv.Imag)
	case StringValue:
		bv, ok := b.(StringValue)
		return ok && av.V == bv.V
	case BytesValue:
		bv, ok := b.(BytesValue)
		return ok && bytes.Equal(av.V, bv.V)
	case OpaqueValue:
		bv, ok := b.(OpaqueValue)
		if !ok {
			return false
		}
		return av.TypeName == bv.TypeName &&
			av.Encoding == bv.Encoding &&
			bytes.Equal(av.Raw, bv.Raw)
	case StructValue:
		bv, ok := b.(StructValue)
		if !ok || len(av.Fields) != len(bv.Fields) {
			return false
		}
		if av.TypeName != bv.TypeName {
			return false
		}
		for i := range av.Fields {
			if av.Fields[i].Name != bv.Fields[i].Name {
				return false
			}
			if !Equal(av.Fields[i].Value, bv.Fields[i].Value) {
				return false
			}
		}
		return true
	case MapValue:
		bv, ok := b.(MapValue)
		if !ok || len(av.Entries) != len(bv.Entries) {
			return false
		}
		// Map entries are order-insensitive for equality: find each av entry
		// in bv by key, then compare values.
		used := make([]bool, len(bv.Entries))
		for _, ae := range av.Entries {
			matched := false
			for i, be := range bv.Entries {
				if used[i] {
					continue
				}
				if Equal(ae.Key, be.Key) && Equal(ae.Value, be.Value) {
					used[i] = true
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	case SliceValue:
		bv, ok := b.(SliceValue)
		if !ok || len(av.Elems) != len(bv.Elems) {
			return false
		}
		for i := range av.Elems {
			if !Equal(av.Elems[i], bv.Elems[i]) {
				return false
			}
		}
		return true
	case ArrayValue:
		bv, ok := b.(ArrayValue)
		if !ok || av.Len != bv.Len || len(av.Elems) != len(bv.Elems) {
			return false
		}
		for i := range av.Elems {
			if !Equal(av.Elems[i], bv.Elems[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// Total ordering across Value kinds:
//
//	NilValue < BoolValue < IntValue/UintValue/FloatValue < ComplexValue < StringValue < BytesValue < OpaqueValue < everything else

// CompareValues returns -1, 0, or +1 ordering a before, equal to, or after b.
// InterfaceValue is unwrapped from both sides before dispatch.
// Same-kind numerics: Int vs Int uses int64, Uint vs Uint uses uint64.
// Cross-numeric comparisons use float64; large integer values near the limits
// of float64 precision may not compare correctly.
// Floats order per [cmp.Compare]: NaN sorts below every other value and
// equals itself, keeping the ordering total. Complex values order by real
// part, then imaginary part.
// Composite types (struct, map, slice, array) fall back to [Format] output.
func CompareValues(a, b Value) int {
	if iv, ok := a.(InterfaceValue); ok {
		a = iv.Value
	}
	if iv, ok := b.(InterfaceValue); ok {
		b = iv.Value
	}

	oa, ob := kindOrder(a), kindOrder(b)
	if oa != ob {
		return cmpInt(oa, ob)
	}

	switch av := a.(type) {
	case NilValue:
		return 0

	case BoolValue:
		bv := b.(BoolValue)
		return cmpInt(boolInt(av.V), boolInt(bv.V))

	case IntValue:
		switch bv := b.(type) {
		case IntValue:
			return cmp64(av.V, bv.V)
		case UintValue:
			return cmpFloat(float64(av.V), float64(bv.V))
		case FloatValue:
			return cmpFloat(float64(av.V), bv.V)
		}

	case UintValue:
		switch bv := b.(type) {
		case UintValue:
			return cmpU64(av.V, bv.V)
		case IntValue:
			return cmpFloat(float64(av.V), float64(bv.V))
		case FloatValue:
			return cmpFloat(float64(av.V), bv.V)
		}

	case FloatValue:
		switch bv := b.(type) {
		case FloatValue:
			return cmpFloat(av.V, bv.V)
		case IntValue:
			return cmpFloat(av.V, float64(bv.V))
		case UintValue:
			return cmpFloat(av.V, float64(bv.V))
		}

	case ComplexValue:
		bv := b.(ComplexValue)
		if c := cmpFloat(av.Real, bv.Real); c != 0 {
			return c
		}
		return cmpFloat(av.Imag, bv.Imag)

	case StringValue:
		bv := b.(StringValue)
		return cmpInt(strings.Compare(av.V, bv.V), 0)

	case BytesValue:
		bv := b.(BytesValue)
		return cmpInt(bytes.Compare(av.V, bv.V), 0)

	case OpaqueValue:
		bv := b.(OpaqueValue)
		as, bs := opaqueStr(av), opaqueStr(bv)
		return cmpInt(strings.Compare(as, bs), 0)
	}

	// Composite types: compare by Format output as last resort.
	return cmpInt(strings.Compare(Format(a), Format(b)), 0)
}

// CompareValuesFold is identical to [CompareValues] except strings are compared
// case-insensitively using strings.ToLower. It does not handle all Unicode case
// equivalences (e.g. ß vs SS).
func CompareValuesFold(a, b Value) int {
	if iv, ok := a.(InterfaceValue); ok {
		a = iv.Value
	}
	if iv, ok := b.(InterfaceValue); ok {
		b = iv.Value
	}

	if av, ok := a.(StringValue); ok {
		if bv, ok := b.(StringValue); ok {
			return cmpInt(strings.Compare(strings.ToLower(av.V), strings.ToLower(bv.V)), 0)
		}
	}
	return CompareValues(a, b)
}

func kindOrder(v Value) int {
	switch v.(type) {
	case NilValue:
		return 0
	case BoolValue:
		return 1
	case IntValue, UintValue, FloatValue:
		return 2
	case ComplexValue:
		return 3
	case StringValue:
		return 4
	case BytesValue:
		return 5
	case OpaqueValue:
		return 6
	default:
		return 7
	}
}

func opaqueStr(v OpaqueValue) string {
	if v.Decoded != nil {
		return fmt.Sprint(v.Decoded)
	}
	return hex.EncodeToString(v.Raw)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmp64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpU64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// cmpFloat orders floats totally: NaN sorts below every other value and
// equals itself. A comparator where NaN equals everything (as a<b/a>b tests
// would report) is intransitive and corrupts sort orderings built on it.
func cmpFloat(a, b float64) int {
	return cmp.Compare(a, b)
}

// floatEq is structural float equality: NaN equals NaN.
func floatEq(a, b float64) bool {
	return a == b || (a != a && b != b)
}
