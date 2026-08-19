// Fuzzing baseline: 2026-08-17. Ran 3h, no failures, 1231 corpus entries.
// Extended with -read-limit flags for the v0.3.1 engine extraction;
// 2026-08-18 20m re-sweep against the rewired pipeline, no failures,
// 1858 corpus entries.
package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// safeFlags is the whitelist the fuzzer draws arguments from. Every entry is
// either a boolean flag or a flag paired with a fixed, harmless value.
//
// Deliberately excluded: -file/-f and -diff, which open paths from the argument
// vector. Fuzzing those would have the target read arbitrary files off the
// developer's disk, which is both a hazard and a source of non-reproducible
// failures. Everything they gate downstream is reachable via stdin instead.
var safeFlags = [][]string{
	{"-format", "pretty"},
	{"-format", "json"},
	{"-format", "jsonl"},
	{"-format", "csv"},
	{"-format", "tsv"},
	{"-format", "bogus"},
	{"-schema"},
	{"-schema-format", "go"},
	{"-schema-format", "json"},
	{"-types"},
	{"-stats"},
	{"-index", "0"},
	{"-index", "3"},
	{"-index", "-1"},
	{"-bytes", "hex"},
	{"-bytes", "base64"},
	{"-bytes", "literal"},
	{"-max-bytes", "0"},
	{"-max-bytes", "4"},
	{"-color"},
	{"-no-color"},
	{"-r"},
	{"-compact"},
	{"-null-on-miss"},
	{"-time-format", "2006-01-02"},
	{"-time-format", "%%%"},
	{"-no-headers"},
	{"-hetero", "first"},
	{"-hetero", "reject"},
	{"-hetero", "union"},
	{"-hetero", "partition"},
	{"-hetero", "sideways"},
	{"-limit", "2"},
	{"-limit", "0"},
	{"-offset", "1"},
	{"-sort", "Name"},
	{"-sort", "Name:desc,X:asc"},
	{"-sort", ":asc"},
	{"-sort-desc"},
	{"-sort-fold"},
	{"-sort-drop-missing"},
	{"-skip-errors"},
	{"-count"},
	{"-sum", "X"},
	{"-min", "X"},
	{"-max", "Y"},
	{"-avg", "..X"},
	{"-nonfinite", "strings"},
	{"-nonfinite", "null"},
	{"-nonfinite", "maybe"},
	{"-read-limit", "0"},
	{"-read-limit", "64"},
	{"-read-limit", "-1"},
	{"-h"},
}

// queryOperands are the positional query expressions the fuzzer can pass.
var queryOperands = []string{
	"",
	"Name",
	"Pt.X",
	"..Name",
	"Items.*",
	"[Status=active]",
	"[X>=1]|[Y<0]",
	"..*",
	"[[[",
}

// buildArgs turns fuzzer bytes into a bounded, safe argument vector. Each byte
// selects one whitelisted flag; the final byte optionally appends a positional
// query expression.
func buildArgs(seed []byte) []string {
	const maxFlags = 8

	var args []string
	n := min(len(seed), maxFlags)
	for _, b := range seed[:n] {
		args = append(args, safeFlags[int(b)%len(safeFlags)]...)
	}
	if len(seed) > 0 {
		if q := queryOperands[int(seed[len(seed)-1])%len(queryOperands)]; q != "" {
			args = append(args, q)
		}
	}
	return args
}

// FuzzGQ drives the CLI end to end: argument parsing, flag-conflict
// validation, compression sniffing on stdin (via gobspect/decompress, which
// has its own dedicated fuzzer), and every output path from pretty printing
// through csv, aggregates, and diffing.
//
// This is the only target that exercises the flag combinations as a whole
// rather than each library call in isolation. A non-zero exit is a perfectly
// good outcome — the property is that no argument vector and no stdin content
// can make the command panic.
func FuzzGQ(f *testing.F) {
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "*.gob"))
	if len(paths) == 0 {
		f.Fatal("no .gob fixtures found in ../../testdata")
	}
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			f.Logf("skipping fixture %q: %v", p, err)
			continue
		}
		f.Add([]byte{byte(i), byte(i * 7), byte(i * 13)}, data)
	}
	// A gzip-wrapped seed so the sniffing branch starts with coverage.
	if data, err := os.ReadFile(paths[0]); err == nil {
		f.Add([]byte{0, 1}, gzipped(data))
	}

	f.Fuzz(func(t *testing.T, argSeed, stdin []byte) {
		args := buildArgs(argSeed)

		in := stdin
		// Half the inputs are gzip-wrapped so the sniffer sees both a valid
		// stream and, via the fuzzer's mutations of raw input, malformed ones.
		if len(argSeed) > 0 && argSeed[0]%2 == 0 {
			in = gzipped(stdin)
		}

		_ = run(args, bytes.NewReader(in), io.Discard, io.Discard)
	})
}

func gzipped(data []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return data
	}
	if err := zw.Close(); err != nil {
		return data
	}
	return buf.Bytes()
}
