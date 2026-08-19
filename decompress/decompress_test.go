package decompress_test

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"io"
	"testing"

	dsbzip2 "github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/decompress"
)

// payload is deliberately binary and long enough to span internal buffers.
var payload = bytes.Repeat([]byte("gobspect \x00\x01\xfe binary payload "), 512)

func gzipBytes(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func zstdBytes(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func xzBytes(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// bzip2Bytes uses the dsnet writer: the standard library's compress/bzip2 is
// read-only, so fixture generation needs a third-party writer as a test-only
// dependency.
func bzip2Bytes(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := dsbzip2.NewWriter(&buf, &dsbzip2.WriterConfig{Level: dsbzip2.BestSpeed})
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// zipBytes builds a zip archive holding the named entries.
func zipBytes(t testing.TB, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range entries {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestReaderRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		compress func(testing.TB, []byte) []byte
	}{
		{name: "gzip", compress: gzipBytes},
		{name: "zstd", compress: zstdBytes},
		{name: "xz", compress: xzBytes},
		{name: "bzip2", compress: bzip2Bytes},
		{name: "zip single entry", compress: func(tb testing.TB, data []byte) []byte {
			return zipBytes(tb, map[string][]byte{"data.gob": data})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := decompress.Reader(bytes.NewReader(tt.compress(t, payload)))
			require.NoError(t, err)
			got, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, payload, got)
			assert.NoError(t, r.Close())
		})
	}
}

func TestReaderPassthrough(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{name: "plain data", in: payload},
		{name: "empty input", in: []byte{}},
		{name: "one byte", in: []byte{0x42}},
		{name: "shorter than any magic", in: []byte{0x1f}}, // half a gzip magic
		{name: "partial bzip2 magic", in: []byte("BZ")},
		{name: "partial zip magic", in: []byte("PK\x01\x02")}, // central-directory, not local header
		{name: "five bytes resembling xz", in: []byte{0xfd, '7', 'z', 'X', 'Z'}},
		{name: "gob stream", in: gobStream(t, struct{ N int }{N: 7})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := decompress.Reader(bytes.NewReader(tt.in))
			require.NoError(t, err)
			got, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, tt.in, got, "unrecognized input must pass through byte-identical")
			assert.NoError(t, r.Close())
		})
	}
}

func TestReaderCorruptAfterMagic(t *testing.T) {
	// A valid magic followed by garbage must error — at Reader for formats
	// that validate headers eagerly, or at first Read for lazy ones. Neither
	// may succeed with garbage output, and neither may panic.
	tests := []struct {
		name string
		in   []byte
	}{
		{name: "gzip", in: []byte{0x1f, 0x8b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{name: "zstd", in: []byte{0x28, 0xb5, 0x2f, 0xfd, 0xff, 0xff, 0xff, 0xff}},
		{name: "xz", in: append([]byte{0xfd, '7', 'z', 'X', 'Z', 0x00}, 0xff, 0xff)},
		{name: "bzip2", in: []byte("BZh\xff\xff\xff\xff")},
		{name: "zip", in: []byte("PK\x03\x04\xff\xff\xff\xff")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := decompress.Reader(bytes.NewReader(tt.in))
			if err != nil {
				return // eager header validation: fine
			}
			_, err = io.ReadAll(r)
			assert.Error(t, err, "garbage after a valid magic must not read cleanly")
			r.Close()
		})
	}
}

func TestReaderTruncated(t *testing.T) {
	tests := []struct {
		name     string
		compress func(testing.TB, []byte) []byte
	}{
		{name: "gzip", compress: gzipBytes},
		{name: "zstd", compress: zstdBytes},
		{name: "xz", compress: xzBytes},
		{name: "bzip2", compress: bzip2Bytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			full := tt.compress(t, payload)
			cut := full[:len(full)/2]
			r, err := decompress.Reader(bytes.NewReader(cut))
			if err != nil {
				return
			}
			_, err = io.ReadAll(r)
			assert.Error(t, err, "truncated stream must not read cleanly")
			r.Close()
		})
	}
}

func TestReaderZipEntryRules(t *testing.T) {
	t.Run("two entries rejected", func(t *testing.T) {
		archive := zipBytes(t, map[string][]byte{"a.gob": payload, "b.gob": payload})
		_, err := decompress.Reader(bytes.NewReader(archive))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one file, got 2")
	})

	t.Run("empty archive rejected", func(t *testing.T) {
		archive := zipBytes(t, nil)
		_, err := decompress.Reader(bytes.NewReader(archive))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "got 0")
	})

	t.Run("directory entries ignored", func(t *testing.T) {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		_, err := w.Create("dir/")
		require.NoError(t, err)
		f, err := w.Create("dir/data.gob")
		require.NoError(t, err)
		_, err = f.Write(payload)
		require.NoError(t, err)
		require.NoError(t, w.Close())

		r, err := decompress.Reader(bytes.NewReader(buf.Bytes()))
		require.NoError(t, err)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, payload, got)
		assert.NoError(t, r.Close())
	})

	t.Run("truncated archive rejected", func(t *testing.T) {
		archive := zipBytes(t, map[string][]byte{"a.gob": payload})
		_, err := decompress.Reader(bytes.NewReader(archive[:len(archive)/2]))
		assert.Error(t, err)
	})
}

// closeTracker fails the test if the source reader's Close is ever called:
// Reader does not own the underlying stream.
type closeTracker struct {
	io.Reader
	t *testing.T
}

func (c *closeTracker) Close() error {
	c.t.Error("decompress.Reader must not close the source reader")
	return nil
}

func TestReaderDoesNotCloseSource(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   []byte
	}{
		{name: "gzip", in: gzipBytes(t, payload)},
		{name: "zstd", in: zstdBytes(t, payload)},
		{name: "passthrough", in: payload},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := &closeTracker{Reader: bytes.NewReader(tt.in), t: t}
			r, err := decompress.Reader(src)
			require.NoError(t, err)
			_, err = io.ReadAll(r)
			require.NoError(t, err)
			assert.NoError(t, r.Close())
		})
	}
}

// TestReaderSingleLayer pins the single-layer contract: doubly-compressed
// input comes back still wearing the inner layer.
func TestReaderSingleLayer(t *testing.T) {
	inner := gzipBytes(t, payload)
	outer := gzipBytes(t, inner)

	r, err := decompress.Reader(bytes.NewReader(outer))
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, inner, got, "only one compression layer is removed")
	assert.NoError(t, r.Close())
}

// gobStream encodes v with encoding/gob.
func gobStream(t testing.TB, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(v))
	return buf.Bytes()
}

// TestReaderEndToEndGob is the integration the package exists for: a
// compressed gob stream decodes through gobspect after decompression,
// regardless of which of the five formats compressed it.
func TestReaderEndToEndGob(t *testing.T) {
	type point struct{ X, Y int }
	stream := gobStream(t, point{X: 3, Y: 7})

	compressors := map[string]func(testing.TB, []byte) []byte{
		"gzip":  gzipBytes,
		"zstd":  zstdBytes,
		"xz":    xzBytes,
		"bzip2": bzip2Bytes,
		"zip": func(tb testing.TB, data []byte) []byte {
			return zipBytes(tb, map[string][]byte{"stream.gob": data})
		},
		"none": func(_ testing.TB, data []byte) []byte { return data },
	}
	for name, compress := range compressors {
		t.Run(name, func(t *testing.T) {
			r, err := decompress.Reader(bytes.NewReader(compress(t, stream)))
			require.NoError(t, err)
			defer r.Close()

			vals, err := gobspect.New().Stream(r).Collect()
			require.NoError(t, err)
			require.Len(t, vals, 1)
			sv, ok := vals[0].(gobspect.StructValue)
			require.True(t, ok)
			assert.Equal(t, "point", sv.TypeName)
		})
	}
}
