// Command gq is a jq-inspired CLI for inspecting gob binary streams.
//
// Usage:
//
//	gq [flags] [query]
//
// With no arguments, reads from stdin and prints all top-level values.
// With one positional argument, it is the query expression; input comes from
// --file or stdin.
// Two or more positional arguments is an error.
//
// Use -f / --file to specify an input file. Without it, stdin is used.
//
// Query expressions use the gobspect/query path syntax (dot-separated field
// names, integer indices, wildcards, filters). An empty or "." expression is
// the identity: it matches the whole value.
//
// Examples:
//
//	gq -f data.gob
//	gq -f data.gob .Header.Timestamp
//	cat data.gob | gq .Items.*
//	gq --schema -f data.gob
//	gq --format json -f data.gob .Name
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"golang.org/x/term"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("gq", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gq [flags] [query]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "flags:")
		fs.PrintDefaults()
	}

	fileFlag := fs.String("file", "", "input file (default: stdin)")
	fs.StringVar(fileFlag, "f", "", "input file (shorthand for --file)")
	formatFlag := fs.String("format", "pretty", "output format: pretty, json, csv, or tsv")
	schemaFlag := fs.Bool("schema", false, "print Go-style type schema and exit")
	typesFlag := fs.Bool("types", false, "print type definitions as JSON and exit")
	indexFlag := fs.Int("index", -1, "print only the Nth value (0-based); -1 = all")
	bytesFlag := fs.String("bytes", "hex", "byte rendering: hex, base64, or literal")
	maxBytesFlag := fs.Int("max-bytes", 64, "truncation limit for byte slices (0 = no limit)")
	colorFlag := fs.Bool("color", false, "force color output on")
	noColorFlag := fs.Bool("no-color", false, "force color output off")
	rawFlag := fs.Bool("r", false, "raw string: for string results, omit surrounding quotes")
	compactFlag := fs.Bool("compact", false, "compact JSON output (no indentation)")
	nullOnMissFlag := fs.Bool("null-on-miss", false, "print null/nothing instead of exiting 1 when path not found")
	timeFormatFlag := fs.String("time-format", "", "layout for time.Time values (default: RFC3339Nano)")
	noHeadersFlag := fs.Bool("no-headers", false, "suppress header row in csv/tsv output")
	heteroFlag := fs.String("hetero", "first", "heterogeneous-type handling for csv/tsv: first, reject, union, or partition")
	limitFlag := fs.Int("limit", 0, "stop after N results (0 = no limit)")
	offsetFlag := fs.Int("offset", 0, "skip the first N results")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	args = fs.Args()

	// Resolve queryExpr and inputPath from positional arguments.
	// Positional args are always query expressions; use -f/--file for the input file.
	var queryExpr string
	inputPath := *fileFlag // empty = stdin

	switch len(args) {
	case 0:
		// No args: identity query, use --file or stdin.
	case 1:
		// One arg: it is the query expression.
		queryExpr = args[0]
	default:
		fmt.Fprintln(stderr, "gq: too many arguments")
		fs.Usage()
		return 2
	}

	// Normalise the query: "." is the identity; ".Foo" → "Foo" (leading dot stripped).
	// Preserve ".." which is valid descent syntax.
	if queryExpr == "." {
		queryExpr = ""
	} else if strings.HasPrefix(queryExpr, ".") && !strings.HasPrefix(queryExpr, "..") {
		queryExpr = queryExpr[1:]
	}

	// Parse --hetero before flag validation so we can reject bad values early.
	heteroMode, heteroOK := parseHeteroMode(*heteroFlag)
	if !heteroOK {
		fmt.Fprintf(stderr, "gq: unknown --hetero value %q; use first, reject, union, or partition\n", *heteroFlag)
		return 2
	}

	if *limitFlag < 0 {
		fmt.Fprintln(stderr, "gq: --limit must be non-negative")
		return 2
	}
	if *offsetFlag < 0 {
		fmt.Fprintln(stderr, "gq: --offset must be non-negative")
		return 2
	}

	// Validate flags and combinations.
	warnings, err := validateFlags(*schemaFlag, *typesFlag, queryExpr, *formatFlag, *indexFlag, *limitFlag, *offsetFlag, *compactFlag, *rawFlag, *colorFlag, *noColorFlag)
	if err != nil {
		fmt.Fprintf(stderr, "gq: %v\n", err)
		return 2
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr, "gq: %s\n", w)
	}

	// Parse path before opening any file so bad expressions fail fast.
	path, err := query.Parse(queryExpr)
	if err != nil {
		fmt.Fprintf(stderr, "gq: invalid query expression %q: %v\n", queryExpr, err)
		return 2
	}

	// Open input.
	var r io.Reader
	if inputPath == "" {
		r = stdin
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		defer f.Close()
		r = f
	}

	// Build inspector options.
	var inspOpts []gobspect.Option
	if *timeFormatFlag != "" {
		inspOpts = append(inspOpts, gobspect.WithTimeFormat(*timeFormatFlag))
	}
	ins := gobspect.New(inspOpts...)

	// Resolve format options.
	bytesFormat, ok := parseBytesFormat(*bytesFlag)
	if !ok {
		fmt.Fprintf(stderr, "gq: unknown --bytes value %q; use hex, base64, or literal\n", *bytesFlag)
		return 2
	}

	// Validate --format.
	switch *formatFlag {
	case "pretty", "json", "csv", "tsv":
		// ok
	default:
		fmt.Fprintf(stderr, "gq: unknown --format value %q; use pretty, json, csv, or tsv\n", *formatFlag)
		return 2
	}

	// Determine color: auto (TTY detection) unless forced.
	useColor := false
	if *colorFlag {
		useColor = true
	} else if !*noColorFlag {
		if f, ok := stdout.(*os.File); ok {
			useColor = isTerminal(f)
		}
	}

	// --schema mode.
	if *schemaFlag {
		schema, err := ins.Stream(r).Schema()
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		var s string
		if useColor {
			s = schema.Format(gobspect.WithColor(gobspect.ANSIColorScheme))
		} else {
			s = schema.String()
		}
		fmt.Fprintln(stdout, s)
		return 0
	}

	// --types mode.
	if *typesFlag {
		s := ins.Stream(r)
		_, err := s.Collect()
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		out, jerr := json.MarshalIndent(s.Types(), "", "  ")
		if jerr != nil {
			fmt.Fprintf(stderr, "gq: marshaling types: %v\n", jerr)
			return 1
		}
		stdout.Write(out)
		fmt.Fprintln(stdout)
		return 0
	}

	// Build Format options.
	fmtOpts := []gobspect.FormatOption{
		gobspect.WithBytesFormat(bytesFormat),
		gobspect.WithMaxBytes(*maxBytesFlag),
	}

	// Value mode: build a Stream so the tabular printer can call TypeByID.
	stream := ins.Stream(r)

	// Set up tabular printer for csv/tsv.
	var tp *tabularPrinter
	if *formatFlag == "csv" || *formatFlag == "tsv" {
		delim := rune(',')
		if *formatFlag == "tsv" {
			delim = '\t'
		}
		tp = newTabularPrinter(stdout,
			withDelimiter(delim),
			withNoHeaders(*noHeadersFlag),
			withStream(stream),
			withBytesFormat(bytesFormat),
			withMaxBytes(*maxBytesFlag),
			withHeterogeneousMode(heteroMode),
		)
		defer func() {
			if flushErr := tp.Flush(); flushErr != nil {
				if errors.Is(flushErr, syscall.EPIPE) {
					return
				}
				fmt.Fprintf(stderr, "gq: %v\n", flushErr)
			}
		}()
	}

	// Value mode: iterate the stream.
	idx := 0
	anyMatch := false
	exitCode := 0
	resultN := 0 // absolute index of each result across the whole stream

outer:
	for v, err := range stream.Values() {
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}

		// --index filtering.
		if *indexFlag >= 0 && idx != *indexFlag {
			idx++
			continue
		}

		// Apply query path and stream results lazily.
		for result := range query.AllPathSeq(v, path) {
			anyMatch = true // set before offset check so offset-past-end doesn't trigger path-not-found

			pos := resultN
			resultN++
			if pos < *offsetFlag {
				continue
			}

			var writeErr error
			if tp != nil {
				writeErr = tp.WriteValue(result)
			} else {
				writeErr = printValue(result, stdout, *formatFlag, *rawFlag, *compactFlag, useColor, fmtOpts)
			}
			if writeErr != nil {
				if errors.Is(writeErr, syscall.EPIPE) {
					return 0
				}
				fmt.Fprintf(stderr, "gq: %v\n", writeErr)
				return 1
			}

			if *limitFlag > 0 && resultN-*offsetFlag >= *limitFlag {
				break outer
			}
		}

		idx++

		// If --index was specified and we just processed it, stop.
		if *indexFlag >= 0 && idx > *indexFlag {
			break
		}
	}

	// Report path-not-found when query was non-empty and nothing matched.
	if queryExpr != "" && !anyMatch {
		if *nullOnMissFlag {
			fmt.Fprintln(stdout, "null")
		} else {
			fmt.Fprintf(stderr, "gq: path %q not found\n", queryExpr)
			exitCode = 1
		}
	}

	return exitCode
}

// printValue renders and writes a single value to w.
func printValue(v gobspect.Value, w io.Writer, format string, raw, compact, color bool, fmtOpts []gobspect.FormatOption) error {
	switch format {
	case "json":
		var out []byte
		var err error
		if compact {
			out, err = gobspect.ToJSON(v)
		} else {
			out, err = gobspect.ToJSONIndent(v, "", "  ")
		}
		if err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w)
		return err

	default: // "pretty"
		opts := fmtOpts
		if color {
			opts = append(opts, gobspect.WithColor(gobspect.ANSIColorScheme))
		}
		if raw {
			target := v
			if iv, ok := target.(gobspect.InterfaceValue); ok {
				target = iv.Value
			}
			if sv, ok := target.(gobspect.StringValue); ok {
				_, err := fmt.Fprintln(w, sv.V)
				return err
			}
		}
		if err := gobspect.FormatTo(w, v, opts...); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w)
		return err
	}
}

// parseHeteroMode converts a flag string to a HeterogeneousMode constant.
func parseHeteroMode(s string) (HeterogeneousMode, bool) {
	switch strings.ToLower(s) {
	case "first":
		return HeterogeneousFirstWins, true
	case "reject":
		return HeterogeneousReject, true
	case "union":
		return HeterogeneousUnion, true
	case "partition":
		return HeterogeneousPartition, true
	default:
		return HeterogeneousFirstWins, false
	}
}

// parseBytesFormat converts a flag string to a BytesFormat constant.
func parseBytesFormat(s string) (gobspect.BytesFormat, bool) {
	switch strings.ToLower(s) {
	case "hex":
		return gobspect.BytesHex, true
	case "base64":
		return gobspect.BytesBase64, true
	case "literal":
		return gobspect.BytesLiteral, true
	default:
		return gobspect.BytesHex, false
	}
}

// isTerminal reports whether f is connected to a terminal.
// Uses golang.org/x/term which calls GetConsoleMode on Windows and
// isatty-style ioctl on Unix, handling ConPTY and WSL correctly.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func validateFlags(schema, types bool, queryExpr string, format string, index, limit, offset int, compact, raw, color, noColor bool) (warnings []string, err error) {
	if color && noColor {
		return nil, fmt.Errorf("cannot use --color and --no-color together")
	}
	if schema && queryExpr != "" {
		warnings = append(warnings, "query expression has no effect with --schema; ignoring")
	}
	if types && queryExpr != "" {
		warnings = append(warnings, "query expression has no effect with --types; ignoring")
	}
	if schema && format != "pretty" {
		warnings = append(warnings, fmt.Sprintf("--format %s has no effect with --schema; ignoring", format))
	}
	if schema && index >= 0 {
		warnings = append(warnings, "--index has no effect with --schema; ignoring")
	}
	if schema && (limit > 0 || offset > 0) {
		warnings = append(warnings, "--limit/--offset has no effect with --schema; ignoring")
	}
	if types && (limit > 0 || offset > 0) {
		warnings = append(warnings, "--limit/--offset has no effect with --types; ignoring")
	}
	if compact && format != "json" {
		warnings = append(warnings, fmt.Sprintf("--compact has no effect with --format %s; ignoring", format))
	}
	if raw && format != "pretty" {
		warnings = append(warnings, fmt.Sprintf("-r has no effect with --format %s; ignoring", format))
	}
	return warnings, nil
}
