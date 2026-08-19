package gobspect

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"fmt"
	"strings"
)

// Comparer compares Values with configurable semantics. The zero Comparer
// matches the package-level [Equal] and [CompareValues] functions.
type Comparer struct {
	// IgnoreInterfaceTypeName drops [InterfaceValue.TypeName] from the
	// comparison, so that only the concrete value inside the interface
	// matters.
	//
	// By default the name participates: a gob stream records the concrete
	// type stored in an interface, and for a named scalar that name is the
	// only place the distinction survives. Miles(5) and Kilos(5) both decode
	// to InterfaceValue{Value: IntValue{5}} and differ solely by TypeName, so
	// ignoring it silently conflates them.
	//
	// Set this when comparing streams produced by different builds of the
	// same program. TypeName holds the fully-qualified type
	// ("example.com/m/pkg.Dog"), so a module path change or a package move
	// otherwise makes every interface-typed field read as modified even
	// though the data is unchanged.
	IgnoreInterfaceTypeName bool

	// Fold compares strings case-insensitively using strings.ToLower. It does
	// not handle all Unicode case equivalences (e.g. ß vs SS), and does not
	// reach strings nested inside composite values, which compare by [Format]
	// output.
	Fold bool
}

// Equal reports whether a and b are structurally equal Values. Equality is
// strict: the two kinds must match, composite shapes must line up exactly, and
// primitives compare by native value. Cross-kind numeric equivalence
// (e.g. IntValue{5} vs FloatValue{5}) returns false — use [CompareValues] if
// you need the permissive numeric coercion.
//
// An [InterfaceValue]'s TypeName participates in equality unless
// [Comparer.IgnoreInterfaceTypeName] is set. Interfaces nest, and every layer
// is compared. When only one side is interface-wrapped the wrappers are
// unwrapped and the concrete values compared, so a value read through an
// interface still equals the same value read directly.
//
// Floats compare structurally, not by IEEE semantics: NaN equals NaN (and a
// complex value with NaN parts equals its bitwise twin). Anything else would
// make a value unequal to itself and report phantom differences when a stream
// is diffed against an identical copy.
func Equal(a, b Value) bool { return Comparer{}.Equal(a, b) }

// Equal reports whether a and b are structurally equal under c's settings.
// See the package-level [Equal] for the comparison rules.
func (c Comparer) Equal(a, b Value) bool {
	if !c.IgnoreInterfaceTypeName {
		var same bool
		if a, b, same = peelIfaces(a, b); !same {
			return false
		}
	}
	a, b = Unwrap(a), Unwrap(b)
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
			if !c.Equal(av.Fields[i].Value, bv.Fields[i].Value) {
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
				if c.Equal(ae.Key, be.Key) && c.Equal(ae.Value, be.Value) {
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
			if !c.Equal(av.Elems[i], bv.Elems[i]) {
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
			if !c.Equal(av.Elems[i], bv.Elems[i]) {
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
// Same-kind numerics: Int vs Int uses int64, Uint vs Uint uses uint64.
// Cross-numeric comparisons use float64; large integer values near the limits
// of float64 precision may not compare correctly.
// Floats order per [cmp.Compare]: NaN sorts below every other value and
// equals itself, keeping the ordering total. Complex values order by real
// part, then imaginary part.
// Composite types (struct, map, slice, array) fall back to [Format] output.
//
// [InterfaceValue] layers order by TypeName before their concrete values,
// unless [Comparer.IgnoreInterfaceTypeName] is set. See [Equal] for how the
// name participates and why.
func CompareValues(a, b Value) int { return Comparer{}.Compare(a, b) }

// CompareValuesFold is [CompareValues] with case-insensitive string
// comparison. It is shorthand for Comparer{Fold: true}.Compare.
func CompareValuesFold(a, b Value) int { return Comparer{Fold: true}.Compare(a, b) }

// Compare orders a and b under c's settings, returning -1, 0, or +1.
// See the package-level [CompareValues] for the ordering rules.
func (c Comparer) Compare(a, b Value) int {
	if !c.IgnoreInterfaceTypeName {
		var same bool
		var ord int
		if a, b, same, ord = orderIfaces(a, b); !same {
			return ord
		}
	}
	a, b = Unwrap(a), Unwrap(b)

	if c.Fold {
		if av, ok := a.(StringValue); ok {
			if bv, ok := b.(StringValue); ok {
				return cmpInt(strings.Compare(strings.ToLower(av.V), strings.ToLower(bv.V)), 0)
			}
		}
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

	// Composite types: compare by Format output as last resort. Interface
	// layers nested inside a composite are rendered by Format, so the name
	// has to be suppressed there too when it is being ignored — otherwise
	// the setting would apply at the top level but not one level down.
	var opts []FormatOption
	if c.IgnoreInterfaceTypeName {
		opts = append(opts, withoutIfaceTypeName())
	}
	return cmpInt(strings.Compare(Format(a, opts...), Format(b, opts...)), 0)
}

// peelIfaces strips interface layers from a and b in lockstep while both sides
// are wrapped, reporting whether every TypeName matched. It stops as soon as
// either side is no longer an [InterfaceValue], leaving mixed shapes for the
// caller to unwrap and compare by concrete value.
func peelIfaces(a, b Value) (Value, Value, bool) {
	for {
		ai, aok := a.(InterfaceValue)
		bi, bok := b.(InterfaceValue)
		if !aok || !bok {
			return a, b, true
		}
		if ai.TypeName != bi.TypeName {
			return a, b, false
		}
		if ai.Value == nil || bi.Value == nil {
			return a, b, ai.Value == bi.Value
		}
		a, b = ai.Value, bi.Value
	}
}

// orderIfaces is peelIfaces for ordering: on the first TypeName
// mismatch it reports the lexicographic order of the two names.
func orderIfaces(a, b Value) (Value, Value, bool, int) {
	for {
		ai, aok := a.(InterfaceValue)
		bi, bok := b.(InterfaceValue)
		if !aok || !bok {
			return a, b, true, 0
		}
		if ai.TypeName != bi.TypeName {
			return a, b, false, cmpInt(strings.Compare(ai.TypeName, bi.TypeName), 0)
		}
		if ai.Value == nil || bi.Value == nil {
			return a, b, ai.Value == bi.Value, cmpInt(boolInt(ai.Value != nil), boolInt(bi.Value != nil))
		}
		a, b = ai.Value, bi.Value
	}
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
