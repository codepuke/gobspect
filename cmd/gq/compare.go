package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/codepuke/gobspect"
)

// Total ordering across gobspect.Value kinds:
//
//	NilValue < BoolValue < IntValue/UintValue/FloatValue (numeric bucket) < StringValue < BytesValue < OpaqueValue < everything else

// kindOrder returns the bucket index for cross-kind comparison.
func kindOrder(v gobspect.Value) int {
	switch v.(type) {
	case gobspect.NilValue:
		return 0
	case gobspect.BoolValue:
		return 1
	case gobspect.IntValue, gobspect.UintValue, gobspect.FloatValue:
		return 2
	case gobspect.StringValue:
		return 3
	case gobspect.BytesValue:
		return 4
	case gobspect.OpaqueValue:
		return 5
	default:
		return 6
	}
}

// compareValues returns -1, 0, or +1 ordering a before, equal to, or after b.
// InterfaceValue is unwrapped from both sides before dispatch.
// Same-kind numerics: Int vs Int uses int64, Uint vs Uint uses uint64.
// Cross-numeric comparisons (Int vs Float, Uint vs Float, Int vs Uint) use
// float64; large integer values near the limits of float64 precision may not
// compare correctly.
// Composite types (struct, map, slice, array) are compared by gobspect.Format
// output as a last resort.
func compareValues(a, b gobspect.Value) int {
	if iv, ok := a.(gobspect.InterfaceValue); ok {
		a = iv.Value
	}
	if iv, ok := b.(gobspect.InterfaceValue); ok {
		b = iv.Value
	}

	oa, ob := kindOrder(a), kindOrder(b)
	if oa != ob {
		return cmp(oa, ob)
	}

	switch av := a.(type) {
	case gobspect.NilValue:
		return 0

	case gobspect.BoolValue:
		bv := b.(gobspect.BoolValue)
		ai, bi := boolInt(av.V), boolInt(bv.V)
		return cmp(ai, bi)

	case gobspect.IntValue:
		switch bv := b.(type) {
		case gobspect.IntValue:
			return cmp64(av.V, bv.V)
		case gobspect.UintValue:
			return cmpFloat(float64(av.V), float64(bv.V))
		case gobspect.FloatValue:
			return cmpFloat(float64(av.V), bv.V)
		}

	case gobspect.UintValue:
		switch bv := b.(type) {
		case gobspect.UintValue:
			return cmpU64(av.V, bv.V)
		case gobspect.IntValue:
			return cmpFloat(float64(av.V), float64(bv.V))
		case gobspect.FloatValue:
			return cmpFloat(float64(av.V), bv.V)
		}

	case gobspect.FloatValue:
		switch bv := b.(type) {
		case gobspect.FloatValue:
			return cmpFloat(av.V, bv.V)
		case gobspect.IntValue:
			return cmpFloat(av.V, float64(bv.V))
		case gobspect.UintValue:
			return cmpFloat(av.V, float64(bv.V))
		}

	case gobspect.StringValue:
		bv := b.(gobspect.StringValue)
		return cmp(strings.Compare(av.V, bv.V), 0)

	case gobspect.BytesValue:
		bv := b.(gobspect.BytesValue)
		return cmp(bytes.Compare(av.V, bv.V), 0)

	case gobspect.OpaqueValue:
		bv := b.(gobspect.OpaqueValue)
		as, bs := opaqueStr(av), opaqueStr(bv)
		return cmp(strings.Compare(as, bs), 0)
	}

	// Composite types (struct, map, slice, array): compare by Format as last resort.
	return cmp(strings.Compare(gobspect.Format(a), gobspect.Format(b)), 0)
}

// compareValuesFold is identical to compareValues except strings are compared
// via strings.ToLower before byte comparison.
// Note: ToLower operates on Unicode code points using simple case folding,
// which does not handle all Unicode case equivalences (e.g. ß vs SS).
func compareValuesFold(a, b gobspect.Value) int {
	if iv, ok := a.(gobspect.InterfaceValue); ok {
		a = iv.Value
	}
	if iv, ok := b.(gobspect.InterfaceValue); ok {
		b = iv.Value
	}

	if av, ok := a.(gobspect.StringValue); ok {
		if bv, ok := b.(gobspect.StringValue); ok {
			return cmp(strings.Compare(strings.ToLower(av.V), strings.ToLower(bv.V)), 0)
		}
	}
	return compareValues(a, b)
}

// opaqueStr returns the comparison string for an OpaqueValue: fmt.Sprint of
// Decoded when non-nil, otherwise the lowercase hex encoding of Raw.
func opaqueStr(v gobspect.OpaqueValue) string {
	if v.Decoded != nil {
		return fmt.Sprint(v.Decoded)
	}
	return hex.EncodeToString(v.Raw)
}

// boolInt maps false→0, true→1.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cmp returns -1, 0, or +1 for a <, ==, > b.
func cmp[T interface{ ~int | ~int64 | ~int32 }](a, b T) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// cmp64 compares two int64 values.
func cmp64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// cmpU64 compares two uint64 values.
func cmpU64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// cmpFloat compares two float64 values.
func cmpFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
