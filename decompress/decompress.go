// Package decompress transparently decompresses a stream by sniffing its
// leading magic bytes. It exists so every gobspect frontend — the gq command,
// MCP servers, library consumers — recognizes the same compression formats
// the same way: by content, never by file extension.
//
// Recognized formats: gzip, zstd, xz, bzip2, and zip. Anything else passes
// through unchanged, so wrapping an uncompressed gob stream is harmless.
//
// The format set is fixed deliberately. A registration hook would be unusable
// from prebuilt binaries like gq, and a library consumer needing an exotic
// format can wrap their own decompressor around the reader before handing it
// to gobspect.
package decompress

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Magic byte prefixes for the recognized formats.
var (
	magicGzip     = []byte{0x1f, 0x8b}
	magicZstd     = []byte{0x28, 0xb5, 0x2f, 0xfd}
	magicXz       = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}
	magicBzip2    = []byte("BZh")
	magicZip      = []byte("PK\x03\x04")
	magicZipEmpty = []byte("PK\x05\x06") // empty archive: recognized to reject clearly
)

// maxMagicLen is the longest prefix any recognized format needs (xz, 6 bytes).
const maxMagicLen = 6

// Reader sniffs r's leading magic bytes and returns a reader over the
// decompressed content. Unrecognized content — including input shorter than
// the magics — passes through unchanged. Detection is content-based only: a
// gzipped stream is decompressed no matter what any filename said, and a
// misleading extension cannot cause a wrong codec. A single compression layer
// is removed; nested layers (a gzipped zip, say) are not recursed into.
//
// Zip input is buffered fully in memory, because the archive directory lives
// at the end (archive/zip needs an io.ReaderAt). The archive must contain
// exactly one file entry, which becomes the stream. Cap untrusted input
// upstream — e.g. with an io.LimitReader — if unbounded buffering is a
// concern; for the decompressed side, use gobspect.WithReadLimit.
//
// Close releases decompressor resources. It does not close r: the caller
// owns the underlying reader's lifetime, consistent with Inspector.Stream.
func Reader(r io.Reader) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	// A short peek is not an error: input smaller than the longest magic is
	// by definition unrecognized and passes through. Read errors surface on
	// the first downstream Read with the decoder's own context.
	head, _ := br.Peek(maxMagicLen)

	switch {
	case bytes.HasPrefix(head, magicGzip):
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("opening gzip stream: %w", err)
		}
		return readCloser{r: gz, closers: []io.Closer{gz}}, nil

	case bytes.HasPrefix(head, magicZstd):
		zr, err := zstd.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("opening zstd stream: %w", err)
		}
		return readCloser{r: zr, closers: []io.Closer{zstdCloser{zr}}}, nil

	case bytes.HasPrefix(head, magicXz):
		xr, err := xz.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("opening xz stream: %w", err)
		}
		return io.NopCloser(xr), nil

	case bytes.HasPrefix(head, magicBzip2):
		return io.NopCloser(bzip2.NewReader(br)), nil

	case bytes.HasPrefix(head, magicZip), bytes.HasPrefix(head, magicZipEmpty):
		return zipEntry(br)

	default:
		return io.NopCloser(br), nil
	}
}

// zipEntry buffers the archive and returns a reader over its single file
// entry.
func zipEntry(br io.Reader) (io.ReadCloser, error) {
	data, err := io.ReadAll(br)
	if err != nil {
		return nil, fmt.Errorf("reading zip archive: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening zip archive: %w", err)
	}
	var entries []*zip.File
	for _, e := range zr.File {
		if !e.FileInfo().IsDir() {
			entries = append(entries, e)
		}
	}
	if len(entries) != 1 {
		return nil, fmt.Errorf("zip archive must contain exactly one file, got %d", len(entries))
	}
	rc, err := entries[0].Open()
	if err != nil {
		return nil, fmt.Errorf("opening zip entry %q: %w", entries[0].Name, err)
	}
	return rc, nil
}

// readCloser pairs a reader with the closers that release its decompressor
// state. The underlying source reader is deliberately absent from closers.
type readCloser struct {
	r       io.Reader
	closers []io.Closer
}

func (c readCloser) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c readCloser) Close() error {
	var first error
	for _, cl := range c.closers {
		if err := cl.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// zstdCloser adapts *zstd.Decoder, whose Close returns nothing, to io.Closer.
type zstdCloser struct{ d *zstd.Decoder }

func (z zstdCloser) Close() error { z.d.Close(); return nil }
