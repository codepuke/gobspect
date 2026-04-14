package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codepuke/gobspect"
)

func FuzzDecode(f *testing.F) {
	// Seed corpus from pre-generated fixture files.
	entries, _ := filepath.Glob("testdata/*.gob")
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Logf("skipping fixture %q: %v", path, err)
			continue
		}
		f.Add(data)
	}

	// Additional inline seeds for value varieties not covered by fixtures.
	type seedVal struct {
		label string
		v     any
	}
	seeds := []seedVal{
		{"int", WrapInt{V: 42}},
		{"string", WrapString{V: "hello, gob"}},
		{"struct", Point{X: 3, Y: 7}},
		{"map", StringMap{"foo": 42, "bar": -1}},
		{"slice", IntSlice{1, 2, 3, 4, 5}},
		{"nested struct", NamedPoint{Name: "origin", Pt: Point{X: 1, Y: 2}}},
		{"time.Time", time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)},
		{"interface", HasInterface{V: Point{X: 5, Y: 6}}},
	}
	for _, s := range seeds {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(s.v); err != nil {
			f.Logf("skipping seed %q: %v", s.label, err)
			continue
		}
		f.Add(buf.Bytes())
	}

	// big.Int uses GobEncoder; encode via a pointer.
	{
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(big.NewInt(123456789)); err == nil {
			f.Add(buf.Bytes())
		}
	}

	ins := gobspect.New()

	f.Fuzz(func(t *testing.T, data []byte) {
		ins.Decode(bytes.NewReader(data))      //nolint:errcheck
		ins.DecodeTypes(bytes.NewReader(data)) //nolint:errcheck

		// Exercise the Values iterator path directly.
		for _, err := range ins.Values(bytes.NewReader(data)) { //nolint:errcheck
			_ = err
		}
	})
}
