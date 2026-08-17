package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iface is a shorthand for building interface-wrapped values in tests.
func iface(name string, v gobspect.Value) gobspect.Value {
	return gobspect.InterfaceValue{TypeName: name, Value: v}
}

// holder wraps v in a single-field struct, so the same pair can be compared
// both at the top level and one level down inside a composite.
func holder(v gobspect.Value) gobspect.Value {
	return gobspect.StructValue{
		TypeName: "H",
		Fields:   []gobspect.Field{{Name: "V", Value: v}},
	}
}

// TestComparerInterfaceTypeNameUniformDepth is the regression test for the
// defect this API replaced: CompareValues ignored InterfaceValue.TypeName at
// the top level but honored it one level down, because nested values fell
// through to the Format-output comparison. The same pair must now get the same
// answer at every depth, under either setting.
func TestComparerInterfaceTypeNameUniformDepth(t *testing.T) {
	a := iface("pkg.Miles", gobspect.IntValue{V: 5})
	b := iface("pkg.Kilos", gobspect.IntValue{V: 5})

	structA := iface("pkg.Dog", gobspect.StructValue{
		TypeName: "Animal", Fields: []gobspect.Field{{Name: "N", Value: gobspect.StringValue{V: "x"}}},
	})
	structB := iface("pkg.Cat", gobspect.StructValue{
		TypeName: "Animal", Fields: []gobspect.Field{{Name: "N", Value: gobspect.StringValue{V: "x"}}},
	})

	honor := gobspect.Comparer{}
	ignore := gobspect.Comparer{IgnoreInterfaceTypeName: true}

	for _, tc := range []struct {
		name string
		a, b gobspect.Value
	}{
		{"scalar concrete", a, b},
		{"struct concrete", structA, structB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Honoring: different names mean different values, at any depth.
			assert.False(t, honor.Equal(tc.a, tc.b), "bare")
			assert.False(t, honor.Equal(holder(tc.a), holder(tc.b)), "nested")
			assert.NotZero(t, honor.Compare(tc.a, tc.b), "bare compare")
			assert.NotZero(t, honor.Compare(holder(tc.a), holder(tc.b)), "nested compare")

			// Ignoring: same values, at any depth.
			assert.True(t, ignore.Equal(tc.a, tc.b), "bare")
			assert.True(t, ignore.Equal(holder(tc.a), holder(tc.b)), "nested")
			assert.Zero(t, ignore.Compare(tc.a, tc.b), "bare compare")
			assert.Zero(t, ignore.Compare(holder(tc.a), holder(tc.b)), "nested compare")
		})
	}
}

// TestComparerZeroValueMatchesPackageFuncs pins the documented equivalence
// between the zero Comparer and the package-level functions.
func TestComparerZeroValueMatchesPackageFuncs(t *testing.T) {
	pairs := [][2]gobspect.Value{
		{gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}},
		{iface("A", gobspect.IntValue{V: 1}), iface("B", gobspect.IntValue{V: 1})},
		{holder(iface("A", gobspect.StringValue{V: "x"})), holder(iface("A", gobspect.StringValue{V: "x"}))},
		{gobspect.StringValue{V: "abc"}, gobspect.StringValue{V: "ABC"}},
		{gobspect.NilValue{}, gobspect.NilValue{}},
	}
	for _, p := range pairs {
		assert.Equal(t, gobspect.Equal(p[0], p[1]), gobspect.Comparer{}.Equal(p[0], p[1]))
		assert.Equal(t, gobspect.CompareValues(p[0], p[1]), gobspect.Comparer{}.Compare(p[0], p[1]))
		assert.Equal(t, gobspect.CompareValuesFold(p[0], p[1]), gobspect.Comparer{Fold: true}.Compare(p[0], p[1]))
	}
}

// TestComparerFold checks that the Fold setting reaches the same strings the
// standalone CompareValuesFold did, and composes with the type-name setting.
func TestComparerFold(t *testing.T) {
	upper, lower := gobspect.StringValue{V: "ABC"}, gobspect.StringValue{V: "abc"}

	assert.NotZero(t, gobspect.Comparer{}.Compare(upper, lower))
	assert.Zero(t, gobspect.Comparer{Fold: true}.Compare(upper, lower))

	// Fold composes with IgnoreInterfaceTypeName rather than overriding it.
	a, b := iface("A", upper), iface("B", lower)
	assert.NotZero(t, gobspect.Comparer{Fold: true}.Compare(a, b), "type names still differ")
	assert.Zero(t, gobspect.Comparer{Fold: true, IgnoreInterfaceTypeName: true}.Compare(a, b))
}

// TestComparerMixedInterfaceShapes covers the documented leniency: a value read
// through an interface still equals the same value read directly, because a
// one-sided wrapper is unwrapped rather than treated as a difference.
func TestComparerMixedInterfaceShapes(t *testing.T) {
	bare := gobspect.IntValue{V: 7}
	for _, c := range []gobspect.Comparer{{}, {IgnoreInterfaceTypeName: true}} {
		assert.True(t, c.Equal(iface("T", bare), bare))
		assert.True(t, c.Equal(bare, iface("T", bare)))
		assert.Zero(t, c.Compare(iface("T", bare), bare))
	}
}

// TestComparerNestedInterfaceReflexivity is the regression test for the
// reflexivity defect: Equal peeled a single interface layer, so a nested
// InterfaceValue reached no case in the type switch and a value compared
// unequal to itself.
func TestComparerNestedInterfaceReflexivity(t *testing.T) {
	values := []gobspect.Value{
		iface("Outer", iface("Inner", gobspect.NilValue{})),
		iface("A", iface("B", iface("C", gobspect.IntValue{V: 1}))),
		holder(iface("Outer", iface("Inner", gobspect.StringValue{V: "x"}))),
	}
	for _, c := range []gobspect.Comparer{{}, {IgnoreInterfaceTypeName: true}, {Fold: true}} {
		for _, v := range values {
			assert.True(t, c.Equal(v, v), "Equal must be reflexive: %#v", v)
			assert.Zero(t, c.Compare(v, v), "Compare must be reflexive: %#v", v)
		}
	}
}

// TestComparerNilInnerInterface covers hand-constructed InterfaceValues whose
// inner Value is a nil interface rather than a NilValue. The decoder never
// emits these, but Value is an exported type so callers can build them, and
// they must not panic or compare asymmetrically.
func TestComparerNilInnerInterface(t *testing.T) {
	nilInner := gobspect.InterfaceValue{TypeName: "T"}
	sameNil := gobspect.InterfaceValue{TypeName: "T"}
	other := gobspect.InterfaceValue{TypeName: "T", Value: gobspect.IntValue{V: 1}}

	for _, c := range []gobspect.Comparer{{}, {IgnoreInterfaceTypeName: true}} {
		assert.True(t, c.Equal(nilInner, sameNil))
		assert.Zero(t, c.Compare(nilInner, sameNil))

		assert.False(t, c.Equal(nilInner, other))
		assert.False(t, c.Equal(other, nilInner))
		assert.Equal(t, sign(c.Compare(nilInner, other)), -sign(c.Compare(other, nilInner)),
			"ordering must stay antisymmetric with a nil inner value")
	}

	// A differing outer name still decides before the inner value is reached.
	assert.False(t, gobspect.Equal(nilInner, gobspect.InterfaceValue{TypeName: "U"}))
}

// TestComparerOrderingIsTotal checks the properties a sort depends on:
// antisymmetry and transitivity across a set spanning several interface types
// and nesting depths.
func TestComparerOrderingIsTotal(t *testing.T) {
	vals := []gobspect.Value{
		gobspect.NilValue{},
		gobspect.IntValue{V: 1},
		iface("pkg.A", gobspect.IntValue{V: 1}),
		iface("pkg.B", gobspect.IntValue{V: 1}),
		iface("pkg.A", iface("pkg.Z", gobspect.IntValue{V: 1})),
		holder(iface("pkg.A", gobspect.StringValue{V: "x"})),
		holder(iface("pkg.B", gobspect.StringValue{V: "x"})),
		gobspect.StringValue{V: "s"},
	}

	for _, c := range []gobspect.Comparer{{}, {IgnoreInterfaceTypeName: true}, {Fold: true}} {
		for _, a := range vals {
			for _, b := range vals {
				ab, ba := c.Compare(a, b), c.Compare(b, a)
				require.Equal(t, sign(ab), -sign(ba), "antisymmetry: %#v vs %#v", a, b)

				for _, d := range vals {
					bd := c.Compare(b, d)
					if sign(ab) == sign(bd) && sign(ab) != 0 {
						require.Equal(t, sign(ab), sign(c.Compare(a, d)),
							"transitivity: %#v, %#v, %#v", a, b, d)
					}
				}
			}
		}
	}
}

// TestComparerEqualImpliesCompareZero pins the relationship the fuzz targets
// assert: Equal is strictly stronger than Compare, so equality must imply a
// zero ordering under the same settings.
func TestComparerEqualImpliesCompareZero(t *testing.T) {
	vals := []gobspect.Value{
		gobspect.NilValue{},
		gobspect.IntValue{V: 1},
		gobspect.FloatValue{V: 1},
		iface("pkg.A", gobspect.IntValue{V: 1}),
		iface("pkg.B", gobspect.IntValue{V: 1}),
		holder(iface("pkg.A", gobspect.StringValue{V: "x"})),
		gobspect.MapValue{Entries: []gobspect.MapEntry{{Key: gobspect.StringValue{V: "k"}, Value: gobspect.IntValue{V: 1}}}},
		gobspect.SliceValue{Elems: []gobspect.Value{gobspect.IntValue{V: 1}}},
	}
	for _, c := range []gobspect.Comparer{{}, {IgnoreInterfaceTypeName: true}, {Fold: true}} {
		for _, a := range vals {
			for _, b := range vals {
				if c.Equal(a, b) {
					assert.Zero(t, c.Compare(a, b), "Equal implies Compare==0: %#v vs %#v", a, b)
				}
			}
		}
	}
}

// TestComparerRealGobStream exercises the setting against values the decoder
// actually produces, rather than hand-built trees: two named scalar types
// stored in the same interface field differ only by TypeName on the wire.
func TestComparerRealGobStream(t *testing.T) {
	miles := decodeValue(t, CounterHolder{V: Miles(5)})
	kilos := decodeValue(t, CounterHolder{V: Kilos(5)})

	assert.False(t, gobspect.Equal(miles, kilos),
		"named scalar types in an interface must not be conflated")
	assert.NotZero(t, gobspect.CompareValues(miles, kilos))

	ignore := gobspect.Comparer{IgnoreInterfaceTypeName: true}
	assert.True(t, ignore.Equal(miles, kilos))
	assert.Zero(t, ignore.Compare(miles, kilos))

	// Same concrete type on both sides stays equal either way.
	miles2 := decodeValue(t, CounterHolder{V: Miles(5)})
	assert.True(t, gobspect.Equal(miles, miles2))
	assert.True(t, ignore.Equal(miles, miles2))
}

// Named scalar types that are distinguishable only by the concrete type name
// gob records for the interface field.
type Counter interface{ n() int }

type Miles int

func (Miles) n() int { return 0 }

type Kilos int

func (Kilos) n() int { return 0 }

type CounterHolder struct{ V Counter }

func decodeValue(t *testing.T, v any) gobspect.Value {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(v))
	vals, err := gobspect.New().Stream(bytes.NewReader(buf.Bytes())).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)
	return vals[0]
}

func init() {
	gob.Register(Miles(0))
	gob.Register(Kilos(0))
}
