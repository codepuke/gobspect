package gobspect_test

import (
	"math"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
)

func TestCompareValuesCrossKind(t *testing.T) {
	tests := []struct {
		name  string
		a     gobspect.Value
		b     gobspect.Value
		wantA int
	}{
		{"nil < bool", gobspect.NilValue{}, gobspect.BoolValue{}, -1},
		{"bool < int", gobspect.BoolValue{}, gobspect.IntValue{}, -1},
		{"bool < uint", gobspect.BoolValue{V: true}, gobspect.UintValue{V: 0}, -1},
		{"bool < float", gobspect.BoolValue{V: true}, gobspect.FloatValue{V: 0}, -1},
		{"int < string", gobspect.IntValue{}, gobspect.StringValue{}, -1},
		{"uint < string", gobspect.UintValue{}, gobspect.StringValue{}, -1},
		{"float < string", gobspect.FloatValue{}, gobspect.StringValue{}, -1},
		{"string < bytes", gobspect.StringValue{}, gobspect.BytesValue{}, -1},
		{"bytes < opaque", gobspect.BytesValue{}, gobspect.OpaqueValue{}, -1},
		{"opaque < struct", gobspect.OpaqueValue{}, gobspect.StructValue{}, -1},
		{"opaque < map", gobspect.OpaqueValue{}, gobspect.MapValue{}, -1},
		{"opaque < slice", gobspect.OpaqueValue{}, gobspect.SliceValue{}, -1},
		{"opaque < array", gobspect.OpaqueValue{}, gobspect.ArrayValue{}, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gobspect.CompareValues(tc.a, tc.b)
			assert.Equal(t, tc.wantA, got)
			got = gobspect.CompareValues(tc.b, tc.a)
			assert.Equal(t, -tc.wantA, got)
		})
	}
}

func TestCompareValuesTransitivity(t *testing.T) {
	a := gobspect.NilValue{}
	b := gobspect.BoolValue{V: false}
	c := gobspect.StringValue{V: "hello"}

	assert.Equal(t, -1, gobspect.CompareValues(a, b), "a < b")
	assert.Equal(t, -1, gobspect.CompareValues(b, c), "b < c")
	assert.Equal(t, -1, gobspect.CompareValues(a, c), "a < c (transitivity)")
}

func TestCompareValuesSameKind(t *testing.T) {
	tests := []struct {
		name string
		a    gobspect.Value
		b    gobspect.Value
		want int
	}{
		{"bool false < true", gobspect.BoolValue{V: false}, gobspect.BoolValue{V: true}, -1},
		{"bool true > false", gobspect.BoolValue{V: true}, gobspect.BoolValue{V: false}, 1},
		{"bool equal", gobspect.BoolValue{V: true}, gobspect.BoolValue{V: true}, 0},
		{"int less", gobspect.IntValue{V: 1}, gobspect.IntValue{V: 2}, -1},
		{"int greater", gobspect.IntValue{V: 2}, gobspect.IntValue{V: 1}, 1},
		{"int equal", gobspect.IntValue{V: 42}, gobspect.IntValue{V: 42}, 0},
		{"int negative", gobspect.IntValue{V: -1}, gobspect.IntValue{V: 0}, -1},
		{"uint less", gobspect.UintValue{V: 1}, gobspect.UintValue{V: 2}, -1},
		{"uint equal", gobspect.UintValue{V: 5}, gobspect.UintValue{V: 5}, 0},
		{"float less", gobspect.FloatValue{V: 1.5}, gobspect.FloatValue{V: 2.5}, -1},
		{"float equal", gobspect.FloatValue{V: 3.14}, gobspect.FloatValue{V: 3.14}, 0},
		{"string apple < banana", gobspect.StringValue{V: "apple"}, gobspect.StringValue{V: "banana"}, -1},
		{"string equal", gobspect.StringValue{V: "hello"}, gobspect.StringValue{V: "hello"}, 0},
		{"string greater", gobspect.StringValue{V: "z"}, gobspect.StringValue{V: "a"}, 1},
		{"bytes less", gobspect.BytesValue{V: []byte{1}}, gobspect.BytesValue{V: []byte{2}}, -1},
		{"bytes equal", gobspect.BytesValue{V: []byte{0xff}}, gobspect.BytesValue{V: []byte{0xff}}, 0},
		{"bytes greater", gobspect.BytesValue{V: []byte{2}}, gobspect.BytesValue{V: []byte{1}}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, gobspect.CompareValues(tc.a, tc.b))
		})
	}
}

func TestCompareValuesNumericPrecision(t *testing.T) {
	t.Run("int64 max vs max-1 uses int64 path", func(t *testing.T) {
		a := gobspect.IntValue{V: math.MaxInt64}
		b := gobspect.IntValue{V: math.MaxInt64 - 1}
		assert.Equal(t, 1, gobspect.CompareValues(a, b))
		assert.Equal(t, -1, gobspect.CompareValues(b, a))
		assert.Equal(t, 0, gobspect.CompareValues(a, a))
	})

	t.Run("uint64 max vs max-1 uses uint64 path", func(t *testing.T) {
		a := gobspect.UintValue{V: math.MaxUint64}
		b := gobspect.UintValue{V: math.MaxUint64 - 1}
		assert.Equal(t, 1, gobspect.CompareValues(a, b))
		assert.Equal(t, -1, gobspect.CompareValues(b, a))
	})
}

func TestCompareValuesCrossNumeric(t *testing.T) {
	t.Run("int vs float uses float64 path", func(t *testing.T) {
		a := gobspect.IntValue{V: 2}
		b := gobspect.FloatValue{V: 3.0}
		assert.Equal(t, -1, gobspect.CompareValues(a, b))
		assert.Equal(t, 1, gobspect.CompareValues(b, a))
	})

	t.Run("uint vs float uses float64 path", func(t *testing.T) {
		a := gobspect.UintValue{V: 5}
		b := gobspect.FloatValue{V: 4.9}
		assert.Equal(t, 1, gobspect.CompareValues(a, b))
		assert.Equal(t, -1, gobspect.CompareValues(b, a))
	})

	t.Run("int vs uint uses float64 path", func(t *testing.T) {
		a := gobspect.IntValue{V: 10}
		b := gobspect.UintValue{V: 20}
		assert.Equal(t, -1, gobspect.CompareValues(a, b))
		assert.Equal(t, 1, gobspect.CompareValues(b, a))
	})
}

func TestCompareValuesOpaques(t *testing.T) {
	t.Run("both decoded compare via fmt.Sprint", func(t *testing.T) {
		a := gobspect.OpaqueValue{TypeName: "T", Decoded: "aaa"}
		b := gobspect.OpaqueValue{TypeName: "T", Decoded: "bbb"}
		assert.Equal(t, -1, gobspect.CompareValues(a, b))
		assert.Equal(t, 1, gobspect.CompareValues(b, a))
		assert.Equal(t, 0, gobspect.CompareValues(a, a))
	})

	t.Run("both raw compare hex", func(t *testing.T) {
		a := gobspect.OpaqueValue{TypeName: "T", Raw: []byte{0x01}}
		b := gobspect.OpaqueValue{TypeName: "T", Raw: []byte{0x02}}
		assert.Equal(t, -1, gobspect.CompareValues(a, b))
		assert.Equal(t, 1, gobspect.CompareValues(b, a))
	})

	t.Run("decoded vs raw compares via fmt.Sprint vs hex", func(t *testing.T) {
		a := gobspect.OpaqueValue{TypeName: "T", Decoded: "abc"}
		b := gobspect.OpaqueValue{TypeName: "T", Raw: []byte{0x01}}
		result := gobspect.CompareValues(a, b)
		assert.True(t, result == -1 || result == 0 || result == 1)
	})
}

func TestCompareValuesInterfaceUnwrap(t *testing.T) {
	inner1 := gobspect.StringValue{V: "apple"}
	inner2 := gobspect.StringValue{V: "banana"}
	wrapped1 := gobspect.InterfaceValue{TypeName: "string", Value: inner1}
	wrapped2 := gobspect.InterfaceValue{TypeName: "string", Value: inner2}

	t.Run("both wrapped", func(t *testing.T) {
		assert.Equal(t, -1, gobspect.CompareValues(wrapped1, wrapped2))
		assert.Equal(t, 1, gobspect.CompareValues(wrapped2, wrapped1))
		assert.Equal(t, 0, gobspect.CompareValues(wrapped1, wrapped1))
	})

	t.Run("one side wrapped", func(t *testing.T) {
		assert.Equal(t, -1, gobspect.CompareValues(wrapped1, inner2))
		assert.Equal(t, 1, gobspect.CompareValues(wrapped2, inner1))
	})

	t.Run("neither wrapped", func(t *testing.T) {
		assert.Equal(t, -1, gobspect.CompareValues(inner1, inner2))
	})
}

func TestCompareValuesFold(t *testing.T) {
	t.Run("Apple < banana under CompareValues (A=65 < b=98)", func(t *testing.T) {
		a := gobspect.StringValue{V: "Apple"}
		b := gobspect.StringValue{V: "banana"}
		assert.Equal(t, -1, gobspect.CompareValues(a, b))
	})

	t.Run("Apple < banana under CompareValuesFold (apple < banana)", func(t *testing.T) {
		a := gobspect.StringValue{V: "Apple"}
		b := gobspect.StringValue{V: "banana"}
		assert.Equal(t, -1, gobspect.CompareValuesFold(a, b))
	})

	t.Run("Banana < apple under CompareValues (B=66 < a=97)", func(t *testing.T) {
		a := gobspect.StringValue{V: "Banana"}
		b := gobspect.StringValue{V: "apple"}
		assert.Equal(t, -1, gobspect.CompareValues(a, b))
	})

	t.Run("Banana > apple under CompareValuesFold (banana > apple)", func(t *testing.T) {
		a := gobspect.StringValue{V: "Banana"}
		b := gobspect.StringValue{V: "apple"}
		assert.Equal(t, 1, gobspect.CompareValuesFold(a, b))
	})

	t.Run("fold equality", func(t *testing.T) {
		a := gobspect.StringValue{V: "Hello"}
		b := gobspect.StringValue{V: "hello"}
		assert.Equal(t, 0, gobspect.CompareValuesFold(a, b))
	})

	t.Run("fold non-string uses same logic as CompareValues", func(t *testing.T) {
		a := gobspect.IntValue{V: 1}
		b := gobspect.IntValue{V: 2}
		assert.Equal(t, gobspect.CompareValues(a, b), gobspect.CompareValuesFold(a, b))
	})
}
