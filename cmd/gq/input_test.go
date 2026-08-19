package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"strings"
	"testing"

	dsbzip2 "github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz"
)

// — -read-limit ———————————————————————————————————————————————————————————————

// TestRun_ReadLimitExceeded verifies a stream larger than -read-limit fails
// with a decode error instead of being read to completion.
func TestRun_ReadLimitExceeded(t *testing.T) {
	type wide struct{ S string }
	r := gobEncodeValues(t, wide{S: strings.Repeat("x", 4096)})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-read-limit", "64"}, r, &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stderr.String(), "exceeds MaxBytes limit of 64")
}

// TestRun_ReadLimitZeroIsUnlimited verifies the default of 0 imposes no cap.
func TestRun_ReadLimitZeroIsUnlimited(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-read-limit", "0"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Alice")
}

// TestRun_ReadLimitNegativeRejected verifies negative limits are a usage
// error.
func TestRun_ReadLimitNegativeRejected(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-read-limit", "-1"}, r, &stdout, &stderr)

	assert.Equal(t, 2, exitCode)
	assert.Contains(t, stderr.String(), "-read-limit must be non-negative")
}

// TestRun_ReadLimitIsUniversal verifies -read-limit combines with mode flags
// (it caps input in every mode, so flag validation must not reject it).
func TestRun_ReadLimitIsUniversal(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-schema", "-read-limit", "1048576"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "testPerson")
}

// TestRun_ReadLimitCapsDecompressedBytes pins the decompression-bomb defense:
// the limit applies to decompressed bytes, so a small gzip expanding past the
// cap errors quickly instead of being decoded in full.
func TestRun_ReadLimitCapsDecompressedBytes(t *testing.T) {
	type wide struct{ S string }
	var plain bytes.Buffer
	require.NoError(t, gob.NewEncoder(&plain).Encode(wide{S: strings.Repeat("a", 1<<16)}))

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, err := gz.Write(plain.Bytes())
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.Less(t, compressed.Len(), 1024, "fixture must compress well below the limit")

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-read-limit", "1024"}, bytes.NewReader(compressed.Bytes()), &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stderr.String(), "exceeds MaxBytes limit of 1024")
}

// — sniffed compression formats ———————————————————————————————————————————————

// TestRun_CompressedStdinFormats verifies each supported compression format
// is detected by content on stdin and decoded transparently.
func TestRun_CompressedStdinFormats(t *testing.T) {
	var plain bytes.Buffer
	require.NoError(t, gob.NewEncoder(&plain).Encode(testPerson{Name: "Zoe"}))
	stream := plain.Bytes()

	compressors := []struct {
		name     string
		compress func(t *testing.T, data []byte) []byte
	}{
		{name: "zstd", compress: func(t *testing.T, data []byte) []byte {
			var buf bytes.Buffer
			w, err := zstd.NewWriter(&buf)
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			return buf.Bytes()
		}},
		{name: "xz", compress: func(t *testing.T, data []byte) []byte {
			var buf bytes.Buffer
			w, err := xz.NewWriter(&buf)
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			return buf.Bytes()
		}},
		{name: "bzip2", compress: func(t *testing.T, data []byte) []byte {
			var buf bytes.Buffer
			w, err := dsbzip2.NewWriter(&buf, &dsbzip2.WriterConfig{Level: dsbzip2.BestSpeed})
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			return buf.Bytes()
		}},
		{name: "zip", compress: func(t *testing.T, data []byte) []byte {
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			f, err := w.Create("stream.gob")
			require.NoError(t, err)
			_, err = f.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			return buf.Bytes()
		}},
	}

	for _, tc := range compressors {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := run([]string{".Name"}, bytes.NewReader(tc.compress(t, stream)), &stdout, &stderr)

			require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
			assert.Contains(t, stdout.String(), "Zoe")
		})
	}
}

// TestRun_ZipTwoEntriesRejected verifies the single-entry zip rule surfaces
// as a clean gq error.
func TestRun_ZipTwoEntriesRejected(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"a.gob", "b.gob"} {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte("x"))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	var stdout, stderr bytes.Buffer
	exitCode := run(nil, bytes.NewReader(buf.Bytes()), &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stderr.String(), "exactly one file, got 2")
}
