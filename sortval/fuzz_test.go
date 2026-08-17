// Fuzzing baseline: 2026-08-17. Ran 3h, 1074 corpus entries. Its 64 restarts
// were all the Equal defect surfacing through the determinism check, since
// fixed to compare rendered form instead.
package sortval

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codepuke/gobspect"
)

func gobFixtures(tb testing.TB) [][]byte {
	tb.Helper()

	var seeds [][]byte
	paths, _ := filepath.Glob(filepath.Join("..", "testdata", "*.gob"))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			tb.Logf("skipping fixture %q: %v", p, err)
			continue
		}
		seeds = append(seeds, data)
	}
	if len(seeds) == 0 {
		tb.Fatal("no .gob fixtures found in ../testdata")
	}
	return seeds
}

// FuzzSortval fuzzes both halves of the package: ParseSortSpec over an
// untrusted comma-separated key flag, and SortMatches over values decoded from
// an untrusted gob stream.
//
// Sorting must be a permutation — losing or duplicating a row is a data-loss
// bug that a panic check would never catch — and it must be deterministic,
// since sort.SliceStable with an inconsistent comparator can otherwise produce
// a different order on every run.
func FuzzSortval(f *testing.F) {
	keyFlags := []string{
		"Name",
		"Name,Score",
		"Name:asc",
		"Score:desc",
		"Name:asc,Score:desc",
		" Name , Score:DESC ",
		"X,Y",
		"V",
		"",
		":asc",
		"Name:",
		"Name:sideways",
	}

	fixtures := gobFixtures(f)
	for i, data := range fixtures {
		f.Add(keyFlags[i%len(keyFlags)], data)
	}
	for _, k := range keyFlags {
		f.Add(k, fixtures[0])
	}

	ins := gobspect.New(gobspect.WithSkipCorruptValues(true), gobspect.WithReadLimit(1<<20))

	f.Fuzz(func(t *testing.T, keysFlag string, data []byte) {
		vals, _ := ins.Stream(bytes.NewReader(data)).Collect()

		for _, defaultDesc := range []bool{false, true} {
			for _, fold := range []bool{false, true} {
				for _, dropMissing := range []bool{false, true} {
					spec, err := ParseSortSpec(keysFlag, defaultDesc, fold, dropMissing)
					if err != nil {
						continue
					}
					if len(spec.Keys) == 0 {
						t.Fatalf("ParseSortSpec(%q) succeeded with no keys", keysFlag)
					}

					sorted := SortMatches(SeqOf(vals), spec)

					if dropMissing {
						if len(sorted) > len(vals) {
							t.Fatalf("DropMissing grew the result: %d in, %d out (keys %q)",
								len(vals), len(sorted), keysFlag)
						}
					} else {
						if len(sorted) != len(vals) {
							t.Fatalf("sort changed the row count: %d in, %d out (keys %q)",
								len(vals), len(sorted), keysFlag)
						}
						// Reordering must not substitute rows either, so
						// compare the two multisets by rendered form.
						if in, out := tally(vals), tally(sorted); !sameTally(in, out) {
							t.Fatalf("sort is not a permutation of its input (keys %q)", keysFlag)
						}
					}

					// Determinism: sorting the same input twice must produce
					// the same order. Compared by rendered form rather than
					// gobspect.Equal, so that a defect in Equal shows up in
					// the target that tests Equal instead of masquerading as
					// a sorting bug here.
					again := SortMatches(SeqOf(vals), spec)
					if len(again) != len(sorted) {
						t.Fatalf("sort not deterministic in length: %d vs %d (keys %q)",
							len(sorted), len(again), keysFlag)
					}
					for i := range sorted {
						if gobspect.Format(sorted[i]) != gobspect.Format(again[i]) {
							t.Fatalf("sort not deterministic at index %d (keys %q)", i, keysFlag)
						}
					}

					// Compare must be antisymmetric over every pair it sees.
					// Bounded: the check is quadratic and a crafted stream can
					// decode to many values.
					n := min(len(sorted), 16)
					for i := range n {
						for j := range n {
							a, b := spec.Compare(sorted[i], sorted[j]), spec.Compare(sorted[j], sorted[i])
							if sign(a) != -sign(b) {
								t.Fatalf("Compare not antisymmetric at (%d,%d): %d vs %d (keys %q)",
									i, j, a, b, keysFlag)
							}
						}
					}
				}
			}
		}
	})
}

// tally counts values by their rendered form, giving a multiset that survives
// reordering.
func tally(vals []gobspect.Value) map[string]int {
	m := make(map[string]int, len(vals))
	for _, v := range vals {
		m[gobspect.Format(v)]++
	}
	return m
}

func sameTally(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, n := range a {
		if b[k] != n {
			return false
		}
	}
	return true
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
