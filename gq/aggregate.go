package gq

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
)

// AggregateOp selects the reduction applied by [Aggregate].
type AggregateOp int

const (
	// OpCount counts matches.
	OpCount AggregateOp = iota
	// OpSum sums the numeric targets.
	OpSum
	// OpMin takes the minimum numeric target.
	OpMin
	// OpMax takes the maximum numeric target.
	OpMax
	// OpAvg averages the numeric targets.
	OpAvg
)

// String returns the op's name: "count", "sum", "min", "max", or "avg".
func (op AggregateOp) String() string {
	switch op {
	case OpCount:
		return "count"
	case OpSum:
		return "sum"
	case OpMin:
		return "min"
	case OpMax:
		return "max"
	case OpAvg:
		return "avg"
	default:
		return fmt.Sprintf("AggregateOp(%d)", int(op))
	}
}

// ParseAggregateOp parses an op name as returned by [AggregateOp.String].
func ParseAggregateOp(s string) (AggregateOp, bool) {
	switch s {
	case "count":
		return OpCount, true
	case "sum":
		return OpSum, true
	case "min":
		return OpMin, true
	case "max":
		return OpMax, true
	case "avg":
		return OpAvg, true
	default:
		return OpCount, false
	}
}

// ErrNonNumeric is wrapped into the error returned by [Aggregate] when a
// numeric op reaches a non-numeric target value. Callers use [errors.Is] to
// distinguish this usage error from a stream decode error.
var ErrNonNumeric = errors.New("non-numeric value for aggregation")

// AggregateResult is the outcome of one [Aggregate] call.
type AggregateResult struct {
	Op    AggregateOp
	Count int64 // matches seen; also the result value for OpCount

	// Valid reports whether a numeric result exists. It is false for OpMin,
	// OpMax, and OpAvg over zero matches, in which case String returns
	// "null". OpCount and OpSum are always valid (an empty sum is 0).
	Valid bool

	// IsInt reports that the result is integer-exact and held in Int. When
	// false the result is in Float — either because a float value appeared,
	// an integer didn't fit in int64, or the running sum overflowed.
	IsInt bool
	Int   int64
	Float float64
}

// String renders the result the way the gq command prints it: "null" when no
// numeric result exists, exact integers in full, and floats compactly
// (integer-valued floats drop the trailing ".0").
func (r AggregateResult) String() string {
	if !r.Valid {
		return "null"
	}
	if r.IsInt {
		return strconv.FormatInt(r.Int, 10)
	}
	return formatFloat(r.Float)
}

// Aggregate drains s, applies match to each top-level value, and reduces the
// matches according to op.
//
// For numeric ops, value selects the number within each match: an empty
// (zero) Path uses the match itself, and a non-empty Path takes the first
// resolution only, so the arithmetic stays well-defined; matches where the
// path resolves to nothing are skipped. Interface wrappers around the target
// are unwrapped through every nested layer.
//
// Stream decode errors are returned as-is. A non-numeric target returns an
// error matching [ErrNonNumeric].
func Aggregate(s *gobspect.Stream, match query.Path, op AggregateOp, value query.Path) (AggregateResult, error) {
	var acc numAcc
	var matchCount int64

	for v, err := range s.Values() {
		if err != nil {
			return AggregateResult{}, err
		}
		for result := range query.AllPathSeq(v, match) {
			matchCount++
			if op == OpCount {
				continue
			}
			target := result
			if !value.IsEmpty() {
				// Apply the numeric path to the result; take only the first
				// resolution so arithmetic stays well-defined.
				r, ok := query.GetPath(result, value)
				if !ok {
					continue
				}
				target = r
			}
			i, f, isInt, ok := toNumeric(target)
			if !ok {
				return AggregateResult{}, fmt.Errorf("%w: %s", ErrNonNumeric, gobspect.ValueKind(target))
			}
			acc.push(i, f, isInt)
		}
	}

	res := AggregateResult{Op: op, Count: matchCount}
	switch op {
	case OpCount:
		res.Valid = true
		res.IsInt = true
		res.Int = matchCount
		res.Float = float64(matchCount)
	case OpSum:
		// The empty sum is 0 (on the float path, matching the accumulator's
		// zero state), never "null".
		res.Valid = true
		res.IsInt = acc.intMode
		res.Int = acc.sumInt
		res.Float = acc.sumFloat
	case OpMin:
		res.Valid = acc.sawAny
		res.IsInt = acc.intMode
		res.Int = acc.minInt
		res.Float = acc.minFloat
	case OpMax:
		res.Valid = acc.sawAny
		res.IsInt = acc.intMode
		res.Int = acc.maxInt
		res.Float = acc.maxFloat
	case OpAvg:
		res.Valid = acc.count > 0
		if res.Valid {
			res.Float = acc.sumFloat / float64(acc.count)
		}
	}
	return res, nil
}

// numAcc accumulates numeric aggregation state. Integer inputs are tracked
// exactly in int64 alongside the float64 mirror; sum and min/max degrade to
// the float path only when a float value appears, an integer doesn't fit in
// int64, or the running sum overflows — so e.g. summing IDs above 2^53 stays
// exact instead of silently rounding.
type numAcc struct {
	count  int64
	sawAny bool

	intMode bool // all values so far were int64-exact and sumInt hasn't overflowed
	sumInt  int64
	minInt  int64
	maxInt  int64

	sumFloat float64
	minFloat float64
	maxFloat float64
}

// push records one value. For integer-exact inputs isInt is true and i holds
// the value; f always holds the float64 mirror.
func (a *numAcc) push(i int64, f float64, isInt bool) {
	if !a.sawAny {
		a.sawAny = true
		a.intMode = isInt
		a.sumInt, a.minInt, a.maxInt = i, i, i
		a.sumFloat, a.minFloat, a.maxFloat = f, f, f
		a.count = 1
		return
	}
	a.count++
	a.sumFloat += f
	a.minFloat = min(a.minFloat, f)
	a.maxFloat = max(a.maxFloat, f)
	if a.intMode {
		if !isInt {
			a.intMode = false
			return
		}
		if s, ok := addInt64(a.sumInt, i); ok {
			a.sumInt = s
		} else {
			a.intMode = false
			return
		}
		a.minInt = min(a.minInt, i)
		a.maxInt = max(a.maxInt, i)
	}
}

// addInt64 adds two int64s, reporting false on overflow.
func addInt64(a, b int64) (int64, bool) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, false
	}
	return s, true
}

// toNumeric extracts a numeric value from v, unwrapping any interface
// layers. isInt reports that i holds the exact value; f always holds the
// float64 form. Returns ok=false for non-numeric kinds (including strings
// and opaques).
func toNumeric(v gobspect.Value) (i int64, f float64, isInt, ok bool) {
	switch n := gobspect.Unwrap(v).(type) {
	case gobspect.IntValue:
		return n.V, float64(n.V), true, true
	case gobspect.UintValue:
		if n.V <= math.MaxInt64 {
			return int64(n.V), float64(n.V), true, true
		}
		return 0, float64(n.V), false, true
	case gobspect.FloatValue:
		return 0, n.V, false, true
	}
	return 0, 0, false, false
}

// formatFloat renders a numeric accumulator compactly. Integer-valued floats
// drop the trailing ".0" so e.g. a count-like sum of 10 reads as "10" instead
// of "10.000000". The bounds check keeps the int64 conversion in range: out of
// range it is implementation-defined (arm64 saturates, making exactly 2^63
// print as MaxInt64).
func formatFloat(f float64) string {
	if f >= math.MinInt64 && f < math.MaxInt64 && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
