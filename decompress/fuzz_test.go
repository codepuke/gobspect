// Fuzzing baseline: 2026-08-18. Ran 20m, no failures, 1264 corpus entries
// (41.6M execs).
package decompress_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/codepuke/gobspect/decompress"
)

// fuzzReadCap bounds how much decompressed output the harness drains, so a
// small input expanding enormously (a bomb-shaped stream) cannot stall the
// fuzzer. Reader itself imposes no output limit by design; that is the
// caller's job (gobspect.WithReadLimit on the decode side).
const fuzzReadCap = 1 << 20

// FuzzReader feeds arbitrary bytes through the magic-byte sniffer and every
// codec behind it. Properties: no panic anywhere; unrecognized input must
// pass through byte-identical; and Close must be safe after both full and
// partial reads.
func FuzzReader(f *testing.F) {
	seed := gobStream(f, struct {
		N int
		S string
	}{N: 42, S: "seed"})
	f.Add([]byte{})
	f.Add(seed)
	f.Add(gzipBytes(f, seed))
	f.Add(zstdBytes(f, seed))
	f.Add(xzBytes(f, seed))
	f.Add(bzip2Bytes(f, seed))
	f.Add(zipBytes(f, map[string][]byte{"s.gob": seed}))
	f.Add(zipBytes(f, map[string][]byte{"a": seed, "b": seed}))
	f.Add(zipBytes(f, nil))
	// Each magic followed by garbage.
	for _, magic := range [][]byte{
		{0x1f, 0x8b}, {0x28, 0xb5, 0x2f, 0xfd}, {0xfd, '7', 'z', 'X', 'Z', 0x00},
		[]byte("BZh"), []byte("PK\x03\x04"), []byte("PK\x05\x06"),
	} {
		f.Add(append(append([]byte{}, magic...), 0xde, 0xad, 0xbe, 0xef))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		recognized := isRecognized(data)

		r, err := decompress.Reader(bytes.NewReader(data))
		if err != nil {
			if !recognized {
				t.Fatalf("Reader errored on unrecognized input: %v", err)
			}
			return
		}
		out, readErr := io.ReadAll(io.LimitReader(r, fuzzReadCap))
		if closeErr := r.Close(); closeErr != nil && readErr == nil && !recognized {
			t.Fatalf("Close errored on passthrough input: %v", closeErr)
		}

		if !recognized {
			if readErr != nil {
				t.Fatalf("passthrough read errored: %v", readErr)
			}
			want := data
			if len(want) > fuzzReadCap {
				want = want[:fuzzReadCap]
			}
			if !bytes.Equal(out, want) {
				t.Fatalf("passthrough not byte-identical: in %d bytes, out %d bytes", len(data), len(out))
			}
		}

		// Close after a partial read must also be safe.
		if r2, err := decompress.Reader(bytes.NewReader(data)); err == nil {
			var one [1]byte
			r2.Read(one[:]) //nolint:errcheck
			r2.Close()      //nolint:errcheck
		}
	})
}

// isRecognized mirrors the sniffer's dispatch table for use as a test oracle.
func isRecognized(data []byte) bool {
	for _, magic := range [][]byte{
		{0x1f, 0x8b}, {0x28, 0xb5, 0x2f, 0xfd}, {0xfd, '7', 'z', 'X', 'Z', 0x00},
		[]byte("BZh"), []byte("PK\x03\x04"), []byte("PK\x05\x06"),
	} {
		if bytes.HasPrefix(data, magic) {
			return true
		}
	}
	return false
}
