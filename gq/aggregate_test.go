package gq_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/gq"
	"github.com/codepuke/gobspect/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type row struct {
	N int64
	F float64
	U uint64
	S string
}

func TestParseAggregateOp(t *testing.T) {
	tests := []struct {
		in     string
		want   gq.AggregateOp
		wantOK bool
	}{
		{"count", gq.OpCount, true},
		{"sum", gq.OpSum, true},
		{"min", gq.OpMin, true},
		{"max", gq.OpMax, true},
		{"avg", gq.OpAvg, true},
		{"", gq.OpCount, false},
		{"SUM", gq.OpCount, false},
		{"median", gq.OpCount, false},
	}
	for _, tt := range tests {
		t.Run("input_"+tt.in, func(t *testing.T) {
			got, ok := gq.ParseAggregateOp(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAggregateOpString(t *testing.T) {
	for _, op := range []gq.AggregateOp{gq.OpCount, gq.OpSum, gq.OpMin, gq.OpMax, gq.OpAvg} {
		back, ok := gq.ParseAggregateOp(op.String())
		require.True(t, ok, "op %d must round-trip", int(op))
		assert.Equal(t, op, back)
	}
}

func TestAggregate(t *testing.T) {
	rows := []any{
		row{N: 3, F: 0.5, U: 10},
		row{N: -1, F: 1.5, U: 20},
		row{N: 7, F: 2.0, U: 30},
	}

	tests := []struct {
		name      string
		op        gq.AggregateOp
		valuePath string
		want      string
		wantCount int64
	}{
		{name: "count", op: gq.OpCount, want: "3", wantCount: 3},
		{name: "sum ints stays exact", op: gq.OpSum, valuePath: ".N", want: "9", wantCount: 3},
		{name: "min ints", op: gq.OpMin, valuePath: ".N", want: "-1", wantCount: 3},
		{name: "max ints", op: gq.OpMax, valuePath: ".N", want: "7", wantCount: 3},
		{name: "avg ints", op: gq.OpAvg, valuePath: ".N", want: "3", wantCount: 3},
		{name: "sum floats", op: gq.OpSum, valuePath: ".F", want: "4", wantCount: 3},
		{name: "avg floats", op: gq.OpAvg, valuePath: ".F", want: "1.3333333333333333", wantCount: 3},
		{name: "min uints", op: gq.OpMin, valuePath: ".U", want: "10", wantCount: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var valuePath query.Path
			if tt.valuePath != "" {
				valuePath = mustPath(t, tt.valuePath)
			}
			res, err := gq.Aggregate(streamOf(t, rows...), query.Path{}, tt.op, valuePath)
			require.NoError(t, err)
			assert.Equal(t, tt.op, res.Op)
			assert.Equal(t, tt.wantCount, res.Count)
			assert.Equal(t, tt.want, res.String())
		})
	}
}

func TestAggregateEmpty(t *testing.T) {
	// A stream whose query matches nothing: min/max/avg are null, count is 0,
	// and the empty sum is 0 (matching the gq command).
	tests := []struct {
		op   gq.AggregateOp
		want string
	}{
		{gq.OpCount, "0"},
		{gq.OpSum, "0"},
		{gq.OpMin, "null"},
		{gq.OpMax, "null"},
		{gq.OpAvg, "null"},
	}
	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			stream := streamOf(t, row{N: 1})
			res, err := gq.Aggregate(stream, mustPath(t, ".Missing"), tt.op, query.Path{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.String())
			if tt.op != gq.OpCount && tt.op != gq.OpSum {
				assert.False(t, res.Valid)
			}
		})
	}
}

// TestAggregateIntExactAbove2p53 verifies integer sums stay exact past the
// float64 mantissa: 2^53+1 survives where a float path would round.
func TestAggregateIntExactAbove2p53(t *testing.T) {
	big := int64(1) << 53
	res, err := gq.Aggregate(
		streamOf(t, row{N: big}, row{N: 1}),
		query.Path{}, gq.OpSum, mustPath(t, ".N"),
	)
	require.NoError(t, err)
	assert.True(t, res.IsInt)
	assert.Equal(t, "9007199254740993", res.String(), "2^53 + 1 must not round")
}

// TestAggregateOverflowDegradesToFloat verifies int64 sum overflow flips to
// the float64 path instead of wrapping.
func TestAggregateOverflowDegradesToFloat(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, row{N: math.MaxInt64}, row{N: math.MaxInt64}),
		query.Path{}, gq.OpSum, mustPath(t, ".N"),
	)
	require.NoError(t, err)
	assert.False(t, res.IsInt, "overflowed sum must leave int mode")
	assert.InEpsilon(t, 2*float64(math.MaxInt64), res.Float, 1e-9)
}

// TestAggregateMixedIntFloat verifies a float appearing mid-stream degrades
// the whole reduction to the float path.
func TestAggregateMixedIntFloat(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, struct{ V any }{V: int64(2)}, struct{ V any }{V: 0.5}),
		query.Path{}, gq.OpSum, mustPath(t, ".V"),
	)
	require.NoError(t, err)
	assert.False(t, res.IsInt)
	assert.Equal(t, "2.5", res.String())
}

// TestAggregateUintAboveMaxInt64 verifies large uints are numeric but not
// int64-exact.
func TestAggregateUintAboveMaxInt64(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, row{U: math.MaxUint64}),
		query.Path{}, gq.OpMax, mustPath(t, ".U"),
	)
	require.NoError(t, err)
	assert.False(t, res.IsInt)
	assert.Equal(t, float64(math.MaxUint64), res.Float)
}

func TestAggregateNonNumeric(t *testing.T) {
	_, err := gq.Aggregate(
		streamOf(t, row{S: "abc", N: 1}),
		query.Path{}, gq.OpSum, mustPath(t, ".S"),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gq.ErrNonNumeric))
	assert.Equal(t, "non-numeric value for aggregation: string", err.Error())
}

func TestAggregateDecodeError(t *testing.T) {
	stream := gobspect.New().Stream(strings.NewReader("\x07this is not gob"))
	_, err := gq.Aggregate(stream, query.Path{}, gq.OpCount, query.Path{})
	require.Error(t, err)
	assert.False(t, errors.Is(err, gq.ErrNonNumeric), "decode errors are not usage errors")
}

// TestAggregateValuePathMissTolerated verifies matches where the value path
// resolves to nothing are skipped, not errors.
func TestAggregateValuePathMissTolerated(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, struct{ V any }{V: int64(4)}, struct{ Other int }{Other: 1}),
		query.Path{}, gq.OpSum, mustPath(t, ".V"),
	)
	require.NoError(t, err)
	assert.Equal(t, "4", res.String())
	assert.Equal(t, int64(2), res.Count, "both top-level values match the identity path")
}

// TestAggregateNestedInterfaceTarget verifies the numeric target unwraps
// nested interface layers. The pre-extraction gq copy peeled one layer only —
// the same defect class fixed in Equal/CompareValues for v0.2.3.
func TestAggregateNestedInterfaceTarget(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, struct{ V any }{V: int64(6)}),
		query.Path{}, gq.OpSum, mustPath(t, ".V"),
	)
	require.NoError(t, err)
	assert.Equal(t, "6", res.String())

	// White-box check with a hand-built nested wrapper, which gob streams
	// produce for interfaces whose concrete value arrives interface-wrapped.
	nested := gobspect.InterfaceValue{
		TypeName: "outer",
		Value:    gobspect.InterfaceValue{TypeName: "inner", Value: gobspect.IntValue{V: 6}},
	}
	i, f, isInt, ok := gq.ToNumericForTest(nested)
	require.True(t, ok, "nested interface wrapper must still be numeric")
	assert.True(t, isInt)
	assert.Equal(t, int64(6), i)
	assert.Equal(t, 6.0, f)
}

// TestFormatFloatInt64Boundary pins the int64 fast path's bounds: exactly
// 2^63 must print in float form, not as a saturated int64. (Ported from the
// cmd/gq white-box suite when formatFloat moved here in v0.3.1.)
func TestFormatFloatInt64Boundary(t *testing.T) {
	assert.Equal(t, "9.223372036854776e+18", gq.FormatFloatForTest(9223372036854775808.0))
	assert.Equal(t, "-9223372036854775808", gq.FormatFloatForTest(-9223372036854775808.0))
	assert.Equal(t, "10", gq.FormatFloatForTest(10.0))
	assert.Equal(t, "3.5", gq.FormatFloatForTest(3.5))
	assert.Equal(t, "1e+19", gq.FormatFloatForTest(1e19))
}

func TestAggregateNaN(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, row{F: math.NaN()}),
		query.Path{}, gq.OpSum, mustPath(t, ".F"),
	)
	require.NoError(t, err)
	assert.Equal(t, "NaN", res.String())
}

func TestAggregateInf(t *testing.T) {
	res, err := gq.Aggregate(
		streamOf(t, row{F: math.Inf(1)}),
		query.Path{}, gq.OpMax, mustPath(t, ".F"),
	)
	require.NoError(t, err)
	assert.Equal(t, "+Inf", res.String())
}
