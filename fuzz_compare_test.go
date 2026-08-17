// Fuzzing baseline: 2026-08-17. Ran 3h, 1069 corpus entries. Found the
// Equal/CompareValues disagreement over InterfaceValue.TypeName (319 restarts).
package gobspect_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/diff"
)

// FuzzCompareDiff checks the ordering and diffing contracts over pairs of
// values decoded from two independent streams.
//
// CompareValues is documented as subtle around NaN and cross-numeric float64
// coercion, so its order axioms are worth asserting directly rather than
// assuming. Diff is checked against Equal: the two must agree on whether two
// values differ, otherwise one of them is wrong.
func FuzzCompareDiff(f *testing.F) {
	seeds := fuzzSeedCorpus(f, "testdata")
	seeds = append(seeds, multiValueSeed(f))

	for i, a := range seeds {
		// Pair each seed with itself (the equality case) and with its
		// successor (the difference case).
		f.Add(a, a)
		f.Add(a, seeds[(i+1)%len(seeds)])
	}

	ins := gobspect.New(gobspect.WithSkipCorruptValues(true), gobspect.WithReadLimit(1<<20))

	comparers := []gobspect.Comparer{
		{},
		{IgnoreInterfaceTypeName: true},
		{Fold: true},
		{Fold: true, IgnoreInterfaceTypeName: true},
	}

	f.Fuzz(func(t *testing.T, dataA, dataB []byte) {
		valsA, _ := ins.Stream(bytes.NewReader(dataA)).Collect()
		valsB, _ := ins.Stream(bytes.NewReader(dataB)).Collect()

		// Stream-level diff over the two collected slices.
		sd := diff.DiffStreams(valsA, valsB)
		_ = diff.StreamHasChanges(sd)
		_ = diff.FormatStream(sd)
		_ = diff.FormatStream(sd, diff.WithColor(diff.ANSIColorScheme))
		assertValidJSON(t, "diff.StreamToJSON", func() ([]byte, error) { return diff.StreamToJSON(sd) })
		assertValidJSON(t, "diff.StreamToJSONIndent", func() ([]byte, error) {
			return diff.StreamToJSONIndent(sd, "", "  ")
		})

		// Diffing a stream against itself must report no changes.
		if diff.StreamHasChanges(diff.DiffStreams(valsA, valsA)) {
			t.Fatalf("DiffStreams(a, a) reports changes (%d values)", len(valsA))
		}

		for i, a := range valsA {
			if i >= len(valsB) {
				break
			}
			b := valsB[i]

			// Every Comparer configuration must hold the same axioms.
			for _, c := range comparers {
				// Antisymmetry: swapping the arguments must flip the sign.
				if got, want := sign(c.Compare(a, b)), -sign(c.Compare(b, a)); got != want {
					t.Fatalf("Compare not antisymmetric at index %d under %+v: cmp(a,b)=%d cmp(b,a)=%d",
						i, c, c.Compare(a, b), c.Compare(b, a))
				}
				// Reflexivity, which the nested-interface defect broke.
				if !c.Equal(a, a) {
					t.Fatalf("Equal not reflexive at index %d under %+v", i, c)
				}
				if c.Compare(a, a) != 0 {
					t.Fatalf("Compare not reflexive at index %d under %+v", i, c)
				}
				// Equal is strictly stronger than Compare, so it must imply a
				// zero ordering under the same settings.
				if c.Equal(a, b) && c.Compare(a, b) != 0 {
					t.Fatalf("Equal says equal but Compare says %d at index %d under %+v",
						c.Compare(a, b), i, c)
				}
				// Ignoring the interface type name can only merge values, never
				// split them: anything equal while honoring the name stays equal
				// once the name is disregarded.
				if !c.IgnoreInterfaceTypeName {
					lax := c
					lax.IgnoreInterfaceTypeName = true
					if c.Equal(a, b) && !lax.Equal(a, b) {
						t.Fatalf("ignoring the interface type name split an equal pair at index %d", i)
					}
				}
			}

			// Equal is documented as strictly stronger than CompareValues:
			// it rejects cross-kind numeric equivalence (IntValue{5} vs
			// FloatValue{5}) and compares opaques by raw bytes rather than
			// decoded form, both of which CompareValues treats as equal. So
			// the implication only runs one way — Equal must imply cmp == 0,
			// but not the reverse.
			eq := gobspect.Equal(a, b)

			// Diff is permitted to be more forgiving than Equal
			// (it matches struct fields by name rather than by position), so
			// only the Equal-implies-no-changes direction holds.
			d := diff.Diff(a, b)
			if eq && diff.HasChanges(d) {
				t.Fatalf("Equal says equal but Diff reports changes at index %d", i)
			}
			if diff.HasChanges(diff.Diff(a, a)) {
				t.Fatalf("Diff(a, a) reports changes at index %d", i)
			}

			_ = diff.Format(d)
			_ = diff.Format(d, diff.WithColor(diff.ANSIColorScheme))
			diff.FormatTo(io.Discard, d) //nolint:errcheck
			assertValidJSON(t, "diff.ToJSON", func() ([]byte, error) { return diff.ToJSON(d) })
			assertValidJSON(t, "diff.ToJSONIndent", func() ([]byte, error) { return diff.ToJSONIndent(d, "", "  ") })
		}
	})
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
