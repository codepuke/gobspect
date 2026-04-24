package gobspect

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
)

// Total ordering across Value kinds:
//
//	NilValue < BoolValue < IntValue/UintValue/FloatValue < StringValue < BytesValue < OpaqueValue < everything else

// CompareValues returns -1, 0, or +1 ordering a before, equal to, or after b.
// InterfaceValue is unwrapped from both sides before dispatch.
// Same-kind numerics: Int vs Int uses int64, Uint vs Uint uses uint64.
// Cross-numeric comparisons use float64; large integer values near the limits
// of float64 precision may not compare correctly.
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
	case StringValue:
		return 3
	case BytesValue:
		return 4
	case OpaqueValue:
		return 5
	default:
		return 6
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

func cmpFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
