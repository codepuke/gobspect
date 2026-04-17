package query

import (
	"path"

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
				if sv, ok := e.Key.(gobspect.StringValue); ok {
					if matched, err := path.Match(seg.filterPattern, sv.V); err == nil && matched {
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
				if sv, ok := e.Key.(gobspect.StringValue); ok {
					if matched, err := path.Match(seg.filterPattern, sv.V); err == nil && matched {
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
		return numericCmp(fv, seg.filterOp, seg.filterNumVal)
	}

	// filterOpGlob
	sv, ok := fieldAsString(v, seg.name)
	if !ok {
		return false
	}
	matched, err := path.Match(seg.filterPattern, sv)
	return err == nil && matched
}

// numericCmp converts v to float64 and compares it against target using op.
// Returns false if v is not a numeric type.
func numericCmp(v gobspect.Value, op filterOp, target float64) bool {
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
			if sv, ok := e.Key.(gobspect.StringValue); ok && sv.V == name {
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
			if sv, ok := e.Key.(gobspect.StringValue); ok && sv.V == name {
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
			if sv, ok := e.Key.(gobspect.StringValue); ok && sv.V == name {
				inner := unwrapInterface(e.Value)
				if sv2, ok := inner.(gobspect.StringValue); ok {
					return sv2.V, true
				}
				return "", false
			}
		}
	}
	return "", false
}
