package query

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNumericPrecision(t *testing.T) {
	t.Run("UintValue MaxUint64", func(t *testing.T) {
		// math.MaxUint64 = 18446744073709551615
		items := makeSlice(
			makeStruct("Item", field_("ID", makeUint(math.MaxUint64))),
		)

		// This should match
		got := All(items, "[ID==18446744073709551615]")
		require.Len(t, got, 1, "Should match MaxUint64")
	})

	t.Run("UintValue MaxUint64 - 5", func(t *testing.T) {
		// math.MaxUint64 - 5 = 18446744073709551610
		items := makeSlice(
			makeStruct("Item", field_("ID", makeUint(math.MaxUint64-5))),
		)

		// This should NOT match 18446744073709551615.
		// Today it falsely matches because both cast to the same float64.
		got := All(items, "[ID==18446744073709551615]")
		assert.Empty(t, got, "Should NOT match MaxUint64-5 against MaxUint64 pattern")
	})

	t.Run("IntValue MinInt64", func(t *testing.T) {
		// math.MinInt64 = -9223372036854775808
		items := makeSlice(
			makeStruct("Item", field_("ID", makeInt(math.MinInt64))),
			makeStruct("Item", field_("ID", makeInt(math.MinInt64+100))),
		)

		got := All(items, "[ID==-9223372036854775808]")
		require.Len(t, got, 1, "Should match exactly MinInt64")
		assert.Equal(t, makeInt(math.MinInt64), MustGet(got[0], "ID"))

		// Test that MinInt64+100 does NOT match MinInt64 pattern
		got2 := All(items, "[ID==-9223372036854775808]")
		// If this matches both, it's a precision bug.
		require.Len(t, got2, 1)
	})

	t.Run("IntValue 2^53 + 1", func(t *testing.T) {
		// 2^53 = 9007199254740992
		// 2^53 + 1 = 9007199254740993
		val := int64(1) << 53
		target := val + 1

		items := makeSlice(
			makeStruct("Item", field_("N", makeInt(val))),    // 2^53
			makeStruct("Item", field_("N", makeInt(target))), // 2^53 + 1
		)

		// Today, both will match because both round to 2^53 as float64.
		got := All(items, "[N==9007199254740993]")
		require.Len(t, got, 1, "Should match ONLY 9007199254740993, not 9007199254740992")
		assert.Equal(t, makeInt(target), MustGet(got[0], "N"))
	})

	t.Run("IntValue vs uint-only pattern (2^63)", func(t *testing.T) {
		// 9223372036854775808 = MaxInt64+1 parses as uint64 only. An int64
		// value is necessarily below it; the old float64 fallback collapsed
		// MaxInt64 and 2^63 into the same float.
		items := makeSlice(
			makeStruct("Item", field_("ID", makeInt(math.MaxInt64))),
		)
		assert.Empty(t, All(items, "[ID==9223372036854775808]"))
		assert.Len(t, All(items, "[ID<9223372036854775808]"), 1)
		assert.Len(t, All(items, "[ID<=9223372036854775808]"), 1)
		assert.Empty(t, All(items, "[ID>9223372036854775808]"))
		assert.Empty(t, All(items, "[ID>=9223372036854775808]"))
	})

	t.Run("UintValue vs negative pattern", func(t *testing.T) {
		items := makeSlice(
			makeStruct("Item", field_("ID", makeUint(5))),
		)
		assert.Empty(t, All(items, "[ID==-1]"))
		assert.Len(t, All(items, "[ID>-1]"), 1)
		assert.Len(t, All(items, "[ID>=-1]"), 1)
		assert.Empty(t, All(items, "[ID<-1]"))
		assert.Empty(t, All(items, "[ID<=-1]"))
	})

	t.Run("Integer pattern beyond uint64", func(t *testing.T) {
		// 18446744073709551616 = 2^64 parses as neither int64 nor uint64;
		// every integer value is necessarily below it.
		items := makeSlice(
			makeStruct("Item", field_("A", makeUint(math.MaxUint64))),
			makeStruct("Item", field_("A", makeInt(math.MaxInt64))),
		)
		assert.Len(t, All(items, "[A<18446744073709551616]"), 2)
		assert.Empty(t, All(items, "[A==18446744073709551616]"))
		assert.Empty(t, All(items, "[A>18446744073709551616]"))
	})

	t.Run("Integer pattern below int64", func(t *testing.T) {
		// -9223372036854775809 = MinInt64-1; every integer value is above it.
		items := makeSlice(
			makeStruct("Item", field_("A", makeInt(math.MinInt64))),
		)
		assert.Len(t, All(items, "[A>-9223372036854775809]"), 1)
		assert.Empty(t, All(items, "[A==-9223372036854775809]"))
		assert.Empty(t, All(items, "[A<-9223372036854775809]"))
	})

	t.Run("Cross-type comparison", func(t *testing.T) {
		items := makeSlice(
			makeStruct("Item", field_("Price", makeFloat(10.0))),
		)

		// [Price==10] vs FloatValue{10.0}
		// "10" is a pure integer literal, so it will parse as int64 and uint64 too.
		// But since v is FloatValue, it should fall back to float64 comparison.
		got := All(items, "[Price==10]")
		require.Len(t, got, 1)
		assert.Equal(t, makeFloat(10.0), MustGet(got[0], "Price"))
	})
}
