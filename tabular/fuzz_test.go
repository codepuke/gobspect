// Fuzzing baseline: 2026-08-17. Ran 3h, no failures, 845 corpus entries.
package tabular

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
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

var fuzzDelimiters = []rune{',', '\t', ';', '|', ' ', 'é'}

var fuzzModes = []HeterogeneousMode{
	HeterogeneousFirstWins,
	HeterogeneousReject,
	HeterogeneousUnion,
	HeterogeneousPartition,
}

// FuzzTabular writes values decoded from arbitrary gob streams as CSV/TSV.
//
// The property worth having here is rectangularity: every emitted record must
// carry the same number of fields as its header. Cell content comes straight
// from hostile decoded strings, so a delimiter, quote, or newline that escapes
// its cell would silently corrupt the table — and the corruption is invisible
// unless the output is parsed back. Union and partition modes are exempt from
// the check because they legitimately emit more than one header.
func FuzzTabular(f *testing.F) {
	for i, data := range gobFixtures(f) {
		f.Add(data, uint8(i))
	}

	ins := gobspect.New(gobspect.WithSkipCorruptValues(true), gobspect.WithReadLimit(1<<20))

	f.Fuzz(func(t *testing.T, data []byte, knob uint8) {
		delim := fuzzDelimiters[int(knob)%len(fuzzDelimiters)]
		mode := fuzzModes[int(knob/8)%len(fuzzModes)]
		noHeaders := knob&0x80 != 0
		bytesFormat := []gobspect.BytesFormat{
			gobspect.BytesHex, gobspect.BytesBase64, gobspect.BytesLiteral,
		}[int(knob/4)%3]

		stream := ins.Stream(bytes.NewReader(data))
		vals, _ := stream.Collect()

		for _, v := range vals {
			_ = CellString(v)
		}

		var buf bytes.Buffer
		p := NewPrinter(&buf,
			WithDelimiter(delim),
			WithHeterogeneousMode(mode),
			WithNoHeaders(noHeaders),
			WithBytesFormat(bytesFormat),
			WithMaxBytes(int(knob)%17),
			WithStream(stream),
		)

		wrote := 0
		for _, v := range vals {
			if err := p.WriteValue(v); err != nil {
				// Reject mode reports type changes as errors by design.
				continue
			}
			wrote++
		}
		if err := p.Flush(); err != nil {
			return
		}

		if wrote == 0 || buf.Len() == 0 {
			return
		}
		// Only the schema-locking modes promise a single rectangular table.
		if mode != HeterogeneousFirstWins && mode != HeterogeneousReject {
			return
		}
		if !validCSVDelimiter(delim) {
			return
		}

		r := csv.NewReader(bytes.NewReader(buf.Bytes()))
		r.Comma = delim
		r.FieldsPerRecord = 0 // enforce: every record matches the first
		for {
			_, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("emitted table does not re-parse (delim=%q mode=%d noHeaders=%v): %v\noutput: %q",
					delim, mode, noHeaders, err, buf.String())
			}
		}
	})
}

// validCSVDelimiter mirrors the rules encoding/csv enforces on Comma.
func validCSVDelimiter(r rune) bool {
	return r != 0 && r != '"' && r != '\r' && r != '\n' && r != 0xFFFD
}
