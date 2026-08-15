package query

import (
	"path"
	"strings"

	"github.com/codepuke/gobspect"
)

// matchesFilter reports whether the value v satisfies filter segment seg.
// v should already be unwrapped from any InterfaceValue before calling.
func matchesFilter(v gobspect.Value, seg segment) bool {
	if seg.kind != segFilter {
		return false
	}

	// OR group: short-circuit over alternatives.
	if len(seg.orAlts) > 0 {
		for _, alt := range seg.orAlts {
			if matchesFilter(v, alt) {
				return true
			}
		}
		return false
	}

	if seg.filterOp == filterOpExist {
		return fieldPresent(v, seg.name)
	}

	if seg.filterOp == filterOpNotExist {
		return !fieldPresent(v, seg.name)
	}

	if seg.filterOp == filterOpNegGlob {
		sv, ok := fieldAsString(v, seg.name)
		if !ok {
			return false
		}
		matched, err := path.Match(seg.filterPattern, sv)
		return err == nil && !matched
	}

	if seg.filterOp == filterOpNegContains {
		fv := fieldValue(v, seg.name)
		if fv == nil {
			return false
		}
		fv = unwrapInterface(fv)
		switch col := fv.(type) {
		case gobspect.SliceValue:
			return !anyElemMatchesGlob(col.Elems, seg.filterPattern)
		case gobspect.ArrayValue:
			return !anyElemMatchesGlob(col.Elems, seg.filterPattern)
		case gobspect.MapValue:
			for _, e := range col.Entries {
				if k, ok := keyString(e.Key); ok {
					if matched, err := path.Match(seg.filterPattern, k); err == nil && matched {
						return false
					}
				}
			}
			return true
		default:
			return false
		}
	}

	if seg.filterOp == filterOpContains {
		fv := fieldValue(v, seg.name)
		if fv == nil {
			return false
		}
		fv = unwrapInterface(fv)
		switch col := fv.(type) {
		case gobspect.SliceValue:
			return anyElemMatchesGlob(col.Elems, seg.filterPattern)
		case gobspect.ArrayValue:
			return anyElemMatchesGlob(col.Elems, seg.filterPattern)
		case gobspect.MapValue:
			for _, e := range col.Entries {
				if k, ok := keyString(e.Key); ok {
					if matched, err := path.Match(seg.filterPattern, k); err == nil && matched {
						return true
					}
				}
			}
			return false
		default:
			return false
		}
	}

	if seg.filterOp == filterOpNumEq || seg.filterOp == filterOpNumLT || seg.filterOp == filterOpNumGT ||
		seg.filterOp == filterOpNumLTE || seg.filterOp == filterOpNumGTE {
		fv := fieldValue(v, seg.name)
		if fv == nil {
			return false
		}
		fv = unwrapInterface(fv)
		// BoolValue: only == is supported; use the pre-parsed bool value.
		if bv, ok := fv.(gobspect.BoolValue); ok {
			if seg.filterOp != filterOpNumEq {
				return false
			}
			if !seg.filterBoolOK {
				return false
			}
			return seg.filterBoolVal == bv.V
		}
		// For numeric types, require a successfully parsed numeric pattern.
		if !seg.filterNumOK {
			return false
		}
		return numericCmp(fv, seg.filterOp, seg)
	}

	// filterOpGlob
	sv, ok := fieldAsString(v, seg.name)
	if !ok {
		return false
	}
	matched, err := path.Match(seg.filterPattern, sv)
	return err == nil && matched
}

// numericCmp compares v against the numeric target in seg using op.
// It uses high-precision integer comparison if both v and the pattern
// are pure integers (e.g., [ID==123] vs IntValue), which avoids the
// precision loss that occurs when casting large integers to float64
// (which has only 53 bits of mantissa). For FloatValue or patterns
// containing decimal points/exponents, it falls back to float64 comparison.
// Returns false if v is not a numeric type.
func numericCmp(v gobspect.Value, op filterOp, seg segment) bool {
	switch n := v.(type) {
	case gobspect.IntValue:
		if seg.filterIntOK {
			return intCmp(n.V, op, seg.filterIntVal)
		}
		if seg.filterUintOK {
			// Pattern parsed as uint64 but not int64: target > MaxInt64,
			// so any int64 value is strictly less than it.
			return cmpBeyond(op, true)
		}
		if valueLess, ok := integerPatternBeyond(seg.filterPattern); ok {
			return cmpBeyond(op, valueLess)
		}
	case gobspect.UintValue:
		if seg.filterUintOK {
			return uintCmp(n.V, op, seg.filterUintVal)
		}
		if seg.filterIntOK {
			// Pattern parsed as int64 but not uint64: target < 0,
			// so any uint64 value is strictly greater than it.
			return cmpBeyond(op, false)
		}
		if valueLess, ok := integerPatternBeyond(seg.filterPattern); ok {
			return cmpBeyond(op, valueLess)
		}
	}

	// Fallback to float64 comparison (precision loss for large integers).
	var fv float64
	switch n := v.(type) {
	case gobspect.IntValue:
		fv = float64(n.V)
	case gobspect.UintValue:
		fv = float64(n.V)
	case gobspect.FloatValue:
		fv = n.V
	default:
		return false
	}
	target := seg.filterNumVal
	switch op {
	case filterOpNumEq:
		return fv == target
	case filterOpNumLT:
		return fv < target
	case filterOpNumGT:
		return fv > target
	case filterOpNumLTE:
		return fv <= target
	case filterOpNumGTE:
		return fv >= target
	}
	return false
}

func intCmp(a int64, op filterOp, b int64) bool {
	switch op {
	case filterOpNumEq:
		return a == b
	case filterOpNumLT:
		return a < b
	case filterOpNumGT:
		return a > b
	case filterOpNumLTE:
		return a <= b
	case filterOpNumGTE:
		return a >= b
	}
	return false
}

func uintCmp(a uint64, op filterOp, b uint64) bool {
	switch op {
	case filterOpNumEq:
		return a == b
	case filterOpNumLT:
		return a < b
	case filterOpNumGT:
		return a > b
	case filterOpNumLTE:
		return a <= b
	case filterOpNumGTE:
		return a >= b
	}
	return false
}

// cmpBeyond resolves a comparison whose target lies entirely outside the
// field value's range: valueLess reports whether the value is necessarily
// less than the target. Equality is impossible either way.
func cmpBeyond(op filterOp, valueLess bool) bool {
	if valueLess {
		return op == filterOpNumLT || op == filterOpNumLTE
	}
	return op == filterOpNumGT || op == filterOpNumGTE
}

// integerPatternBeyond classifies a filter pattern that has integer syntax
// but parsed as neither int64 nor uint64 — it lies beyond both ranges.
// valueLess reports whether any in-range integer value is necessarily less
// than the target (positive overflow) rather than greater (negative
// overflow). ok is false for float-syntax patterns, which use the float64
// comparison path instead.
func integerPatternBeyond(pattern string) (valueLess, ok bool) {
	if pattern == "" || strings.ContainsAny(pattern, ".eE") {
		return false, false
	}
	return pattern[0] != '-', true
}

// anyElemMatchesGlob returns true if any element in elems is a StringValue
// (after unwrapping InterfaceValue) whose V matches the glob pattern.
func anyElemMatchesGlob(elems []gobspect.Value, pattern string) bool {
	for _, e := range elems {
		inner := unwrapInterface(e)
		if sv, ok := inner.(gobspect.StringValue); ok {
			if matched, err := path.Match(pattern, sv.V); err == nil && matched {
				return true
			}
		}
	}
	return false
}

// fieldValue returns the raw Value of the named field within v, or nil if absent.
// For StructValue it checks Fields; for MapValue it checks StringValue keys.
func fieldValue(v gobspect.Value, name string) gobspect.Value {
	switch n := v.(type) {
	case gobspect.StructValue:
		for _, f := range n.Fields {
			if f.Name == name {
				return f.Value
			}
		}
	case gobspect.MapValue:
		for _, e := range n.Entries {
			if k, ok := keyString(e.Key); ok && k == name {
				return e.Value
			}
		}
	}
	return nil
}

// fieldPresent reports whether the named field is present in v.
// For StructValue: checks Fields slice.
// For MapValue: checks whether a StringValue key equal to name is present.
func fieldPresent(v gobspect.Value, name string) bool {
	switch n := v.(type) {
	case gobspect.StructValue:
		for _, f := range n.Fields {
			if f.Name == name {
				return true
			}
		}
	case gobspect.MapValue:
		for _, e := range n.Entries {
			if k, ok := keyString(e.Key); ok && k == name {
				return true
			}
		}
	}
	return false
}

// fieldAsString returns the string value of the named field within v.
// Returns ("", false) if the field is absent or not a StringValue.
func fieldAsString(v gobspect.Value, name string) (string, bool) {
	switch n := v.(type) {
	case gobspect.StructValue:
		for _, f := range n.Fields {
			if f.Name == name {
				inner := unwrapInterface(f.Value)
				if sv, ok := inner.(gobspect.StringValue); ok {
					return sv.V, true
				}
				return "", false
			}
		}
	case gobspect.MapValue:
		for _, e := range n.Entries {
			if k, ok := keyString(e.Key); ok && k == name {
				inner := unwrapInterface(e.Value)
				if sv, ok := inner.(gobspect.StringValue); ok {
					return sv.V, true
				}
				return "", false
			}
		}
	}
	return "", false
}
