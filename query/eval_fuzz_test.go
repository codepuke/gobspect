// Fuzzing baseline: 2026-08-17. Ran 3h, no failures, 1097 corpus entries.
package query

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codepuke/gobspect"
)

// gobFixtures reads the root package's .gob fixtures for use as fuzz seeds.
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

// FuzzQueryEval fuzzes path *evaluation*, not just parsing. FuzzParse and
// FuzzORGroups cover the parser and the filter predicate against hand-built
// fixtures; nothing drives a parsed path over a value tree decoded from a
// hostile gob stream, which is how gq actually uses this package.
//
// The consistency invariants matter because Get and All share traversal code:
// if Get reports a hit that All does not produce, or the iterator form diverges
// from the slice form, one of the two walks is wrong.
func FuzzQueryEval(f *testing.F) {
	exprs := []string{
		"",
		"Name",
		"Customer.Name",
		"Items.0",
		"Items.-1",
		"Orders.*",
		"..Name",
		"[Field!]",
		"[Status=active]",
		"[Tags~devops]",
		"[Count>=1]",
		"[Status=active]|[Status=pending]",
		"Items.*[Status=active]",
		"..*",
		"V",
		"X",
		"Pt.X",
	}

	fixtures := gobFixtures(f)
	for i, data := range fixtures {
		f.Add(data, exprs[i%len(exprs)])
	}
	for _, e := range exprs {
		f.Add(fixtures[0], e)
	}

	ins := gobspect.New(gobspect.WithSkipCorruptValues(true), gobspect.WithReadLimit(1<<20))

	f.Fuzz(func(t *testing.T, data []byte, expr string) {
		// NormalizeQuery is gq's pre-processing step, so it sees raw user
		// input. It must be idempotent and must not turn a parseable
		// expression into an unparseable one.
		norm := NormalizeQuery(expr)
		if again := NormalizeQuery(norm); again != norm {
			t.Fatalf("NormalizeQuery not idempotent: %q -> %q -> %q", expr, norm, again)
		}
		// Normalizing strips a leading "." and so may make an expression
		// parseable, but it must never break one that already parsed.
		if _, rawErr := Parse(expr); rawErr == nil {
			if _, normErr := Parse(norm); normErr != nil {
				t.Fatalf("NormalizeQuery broke a valid expression: %q -> %q (err=%v)",
					expr, norm, normErr)
			}
		}

		p, err := Parse(expr)
		if err != nil {
			return
		}

		stream := ins.Stream(bytes.NewReader(data))
		vals, _ := stream.Collect()

		for _, root := range vals {
			all := AllPath(root, p)

			// The iterator form must yield exactly what the slice form returns.
			var seq []gobspect.Value
			for v := range AllPathSeq(root, p) {
				seq = append(seq, v)
			}
			if len(seq) != len(all) {
				t.Fatalf("AllPathSeq yielded %d values, AllPath returned %d (expr %q)", len(seq), len(all), expr)
			}
			// Compared by rendered form rather than gobspect.Equal: this
			// target is about traversal, and should not fail because equality
			// is broken.
			for i := range all {
				if gobspect.Format(all[i]) != gobspect.Format(seq[i]) {
					t.Fatalf("AllPathSeq differs from AllPath at index %d (expr %q)", i, expr)
				}
			}

			// A hit from GetPath must be the first result of AllPath.
			got, ok := GetPath(root, p)
			if ok {
				if len(all) == 0 {
					t.Fatalf("GetPath found a value but AllPath returned none (expr %q)", expr)
				}
				if gobspect.Format(got) != gobspect.Format(all[0]) {
					t.Fatalf("GetPath result differs from AllPath[0] (expr %q)", expr)
				}
			}

			// Early-terminating the iterator must not panic.
			for range AllPathSeq(root, p) {
				break
			}

			// KeysPath only reports keys for map-shaped targets, so a false
			// second return is expected; the point is that it does not panic.
			KeysPath(root, p) //nolint:errcheck
		}

		// SchemaAt takes an untrusted type-expression string alongside the path.
		if schema, err := ins.Stream(bytes.NewReader(data)).Schema(); err == nil && schema != nil {
			SchemaAt(schema, "", p)   //nolint:errcheck
			SchemaAt(schema, expr, p) //nolint:errcheck
			for _, td := range schema.Types {
				SchemaAt(schema, td.Name, p) //nolint:errcheck
			}
		}
	})
}
