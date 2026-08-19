// Fuzzing baseline: 2026-08-18. Ran 20m, no failures, 1415 corpus entries
// (88.9M execs).
package gq

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"github.com/codepuke/gobspect/sortval"
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

// fuzzRenderCases spans the option surface Render exposes.
var fuzzRenderCases = []RenderOptions{
	{Format: FormatPretty},
	{Format: FormatPretty, Raw: true},
	{Format: FormatPretty, Color: true},
	{Format: FormatPretty, Raw: true, Color: true, FormatOptions: []gobspect.FormatOption{
		gobspect.WithBytesFormat(gobspect.BytesLiteral), gobspect.WithMaxBytes(3),
	}},
	{Format: FormatJSON},
	{Format: FormatJSON, Compact: true},
	{Format: FormatJSONL},
	{Format: FormatJSONL, JSONOptions: []gobspect.JSONOption{gobspect.WithNonFiniteAsNull(true)}},
}

// fuzzQueries are the path expressions the pipeline stage draws from.
var fuzzQueries = []string{"", ".Name", "..V", ".*", ".Items.*", ".Pt.X"}

// FuzzRender drives Value trees decoded from hostile streams through Render
// and the Pipeline. Decoded values are untrusted data; neither rendering nor
// the query/sort/offset/limit plumbing may panic on them, JSON output must be
// valid JSON, and every rendered value ends in exactly one trailing newline.
func FuzzRender(f *testing.F) {
	for i, data := range gobFixtures(f) {
		f.Add(data, uint8(i))
	}

	ins := gobspect.New(gobspect.WithSkipCorruptValues(true), gobspect.WithReadLimit(1<<20))

	f.Fuzz(func(t *testing.T, data []byte, knob uint8) {
		vals, _ := ins.Stream(bytes.NewReader(data)).Collect()

		for _, v := range vals {
			for _, o := range fuzzRenderCases {
				var buf bytes.Buffer
				if err := Render(&buf, v, o); err != nil {
					continue // encode errors are acceptable; panics are not
				}
				out := buf.Bytes()
				if len(out) == 0 || out[len(out)-1] != '\n' {
					t.Fatalf("Render output missing trailing newline: %q", out)
				}
				if o.Format == FormatJSON || o.Format == FormatJSONL {
					if !json.Valid(out) {
						t.Fatalf("Render produced invalid JSON: %q", out)
					}
				}
				if o.Format == FormatJSONL && bytes.Count(out, []byte("\n")) != 1 {
					t.Fatalf("jsonl output must be a single line: %q", out)
				}
			}
		}

		// Drive the same stream through the pipeline with knob-derived
		// parameters, rendering to io.Discard.
		path, err := query.Parse(query.NormalizeQuery(fuzzQueries[int(knob)%len(fuzzQueries)]))
		if err != nil {
			t.Fatalf("static fuzz query failed to parse: %v", err)
		}
		p := Pipeline{
			Path:   path,
			Index:  int(knob%3) - 1, // -1, 0, 1
			Offset: int(knob % 2),
			Limit:  int(knob % 4),
		}
		if knob%5 == 0 {
			spec, err := sortval.ParseSortSpec("Name,V:desc", false, knob%2 == 0, knob%4 == 0)
			if err != nil {
				t.Fatalf("static fuzz sort spec failed to parse: %v", err)
			}
			p.Sort = spec
		}
		stream := ins.Stream(bytes.NewReader(data))
		o := fuzzRenderCases[int(knob)%len(fuzzRenderCases)]
		if _, err := p.RunRender(stream, io.Discard, o); err != nil {
			// Decode and encode errors on hostile input are expected.
			_ = err
		}
	})
}
