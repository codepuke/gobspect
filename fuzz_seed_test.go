package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fuzzSeedCorpus returns a representative set of gob streams for seeding fuzz
// targets: every pre-generated fixture in testdata, plus inline encodings of
// value shapes the fixtures do not cover.
//
// dir is the path to the testdata directory relative to the calling package,
// so subpackage fuzz targets can reuse the root fixtures via "../testdata".
func fuzzSeedCorpus(tb testing.TB, dir string) [][]byte {
	tb.Helper()

	var seeds [][]byte

	entries, _ := filepath.Glob(filepath.Join(dir, "*.gob"))
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			tb.Logf("skipping fixture %q: %v", path, err)
			continue
		}
		seeds = append(seeds, data)
	}

	type seedVal struct {
		label string
		v     any
	}
	vals := []seedVal{
		{"int", WrapInt{V: 42}},
		{"string", WrapString{V: "hello, gob"}},
		{"struct", Point{X: 3, Y: 7}},
		{"map", StringMap{"foo": 42, "bar": -1}},
		{"slice", IntSlice{1, 2, 3, 4, 5}},
		{"nested struct", NamedPoint{Name: "origin", Pt: Point{X: 1, Y: 2}}},
		{"time.Time", time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)},
		{"interface", HasInterface{V: Point{X: 5, Y: 6}}},
	}
	for _, s := range vals {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(s.v); err != nil {
			tb.Logf("skipping seed %q: %v", s.label, err)
			continue
		}
		seeds = append(seeds, buf.Bytes())
	}

	// big.Int uses GobEncoder; encode via a pointer.
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(big.NewInt(123456789)); err == nil {
		seeds = append(seeds, buf.Bytes())
	}

	return seeds
}

// multiValueSeed returns a stream carrying several values of differing types,
// which exercises the multi-message and heterogeneous-type paths.
func multiValueSeed(tb testing.TB) []byte {
	tb.Helper()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range []any{
		NamedPoint{Name: "a", Pt: Point{X: 1, Y: 2}},
		NamedPoint{Name: "b", Pt: Point{X: 3, Y: 4}},
		Point{X: 9, Y: 9},
		WrapString{V: "tail"},
	} {
		if err := enc.Encode(v); err != nil {
			tb.Logf("skipping multi-value seed element: %v", err)
		}
	}
	return buf.Bytes()
}
