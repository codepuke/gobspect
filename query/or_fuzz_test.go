// Fuzzing baseline: 2026-04-17. Ran for 300s with no failures and 654 corpus entries.
package query

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/codepuke/gobspect"
)

func FuzzORGroups(f *testing.F) {
	seeds := []string{
		"[a=1]|[b=2]",
		"[a=1]|[b=2]|[c=3]",
		"Foo[a=1]|[b=2]",
		"Foo[a=1]|[b=2].Bar",
		"Foo[a=1]|[b=2].Bar[c=3]|[d=4]",
		"[Name=a|b]",
		"[Name=\"a|b\"]|[Other=x]",
		"..[a=1]|[b=2]",
		"Items.*[Status=active]|[Status=pending]",
		"[a!]|[b!!]",
		"[a==1]|[a>=2]",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, expr string) {
		p, err := Parse(expr)
		if err != nil {
			return
		}

		// Invariant 5: parent-field zeroing.
		assertORGroupInvariant(t, p)

		// Invariant 1: round-trip stability.
		s1 := p.String()
		p2, err := Parse(s1)
		if err != nil {
			t.Fatalf("round-trip re-parse failed: input=%q s1=%q err=%v", expr, s1, err)
		}
		if !reflect.DeepEqual(p, p2) {
			t.Fatalf("round-trip AST drift: input=%q s1=%q", expr, s1)
		}
		if s2 := p2.String(); s2 != s1 {
			t.Fatalf("round-trip String drift: s1=%q s2=%q", s1, s2)
		}

		// Invariants 2, 3, 4: exercise matching.
		exerciseMatching(t, p)
	})
}

func assertORGroupInvariant(t *testing.T, p Path) {
	for _, s := range p.segs {
		if len(s.orAlts) > 0 {
			if s.name != "" || s.filterOp != 0 || s.filterPattern != "" ||
				s.filterNumVal != 0 || s.filterNumOK ||
				s.filterIntVal != 0 || s.filterIntOK ||
				s.filterUintVal != 0 || s.filterUintOK ||
				s.filterBoolVal || s.filterBoolOK {
				t.Errorf("OR parent fields not zeroed: %+v", s)
			}
		}
	}
}

func exerciseMatching(t *testing.T, p Path) {
	// Fixed test fixture covering various types.
	fixture := makeStruct("Fixture",
		field_("a", makeInt(1)),
		field_("b", makeInt(2)),
		field_("c", makeInt(3)),
		field_("Name", makeString("a|b")),
		field_("Other", makeString("x")),
		field_("Status", makeString("active")),
		field_("Enabled", makeBool(true)),
		field_("Tags", makeSlice(makeString("devops"), makeString("cloud"))),
	)

	// Also generate some random values to test against.
	randomValues := []gobspect.Value{
		fixture,
		makeStruct("Empty"),
		makeMap(entry(makeString("a"), makeInt(1))),
		makeInt(42),
		makeString("hello"),
	}

	for _, v := range randomValues {
		v = unwrapInterface(v)
		for _, seg := range p.segs {
			if seg.kind != segFilter || len(seg.orAlts) == 0 {
				continue
			}

			// Invariant 2: Match equivalence under de-sugaring.
			resGroup := matchesFilter(v, seg)
			
			anyMatch := false
			for _, alt := range seg.orAlts {
				if matchesFilter(v, alt) {
					anyMatch = true
					break
				}
			}
			if resGroup != anyMatch {
				t.Errorf("Match non-equivalence: v=%+v seg=%+v got=%v want=%v", v, seg, resGroup, anyMatch)
			}

			// Invariant 3: Commutativity.
			alts := make([]segment, len(seg.orAlts))
			copy(alts, seg.orAlts)
			rand.Shuffle(len(alts), func(i, j int) { alts[i], alts[j] = alts[j], alts[i] })
			
			shuffledSeg := seg
			shuffledSeg.orAlts = alts
			resShuffled := matchesFilter(v, shuffledSeg)
			if resShuffled != resGroup {
				t.Errorf("Commutativity failure: v=%+v original=%+v shuffled=%+v", v, seg, shuffledSeg)
			}

			// Associativity (partial check): Nest them.
			if len(seg.orAlts) >= 3 {
				// (A|B)|C
				nestedSeg := seg
				nestedSeg.orAlts = []segment{
					{
						kind:   segFilter,
						orAlts: []segment{seg.orAlts[0], seg.orAlts[1]},
					},
					seg.orAlts[2],
				}
				// Append any remaining
				if len(seg.orAlts) > 3 {
					nestedSeg.orAlts = append(nestedSeg.orAlts, seg.orAlts[3:]...)
				}
				
				resNested := matchesFilter(v, nestedSeg)
				if resNested != resGroup {
					t.Errorf("Associativity failure: v=%+v original=%+v nested=%+v", v, seg, nestedSeg)
				}
			}
		}
	}
}
