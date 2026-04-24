// Command gq is a jq-inspired CLI for inspecting gob binary streams.
//
// Usage:
//
//	gq [flags] [query]
//
// With no arguments, reads from stdin and prints all top-level values.
// With one positional argument, it is the query expression; input comes from
// -file or stdin.
// Two or more positional arguments is an error.
//
// Use -f / -file to specify an input file. Without it, stdin is used.
//
// Query expressions use the gobspect/query path syntax (dot-separated field
// names, integer indices, wildcards, filters). An empty or "." expression is
// the identity: it matches the whole value.
//
// Examples:
//
//	gq -f data.gob
//	gq -f data.gob .Header.Timestamp
//	gq -f data.gob.gz           # .gz files are decompressed automatically
//	cat data.gob | gq .Items.*
//	gq -schema -f data.gob
//	gq -format json -f data.gob .Name
package main

import (
	"compress/gzip"
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
	"github.com/codepuke/gobspect/sortval"
	"github.com/codepuke/gobspect/tabular"
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
	fs.StringVar(fileFlag, "f", "", "input file (shorthand for -file)")
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
	sortFlag := fs.String("sort", "", "comma-separated column names to sort by")
	sortDescFlag := fs.Bool("sort-desc", false, "reverse sort order for all keys")
	sortFoldFlag := fs.Bool("sort-fold", false, "case-insensitive string comparison in sort")
	sortDropFlag := fs.Bool("sort-drop-missing", false, "exclude rows missing all sort keys")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	args = fs.Args()

	var queryExpr string
	inputPath := *fileFlag

	switch len(args) {
	case 0:
	case 1:
		queryExpr = args[0]
	default:
		fmt.Fprintln(stderr, "gq: too many arguments")
		fs.Usage()
		return 2
	}

	queryExpr = query.NormalizeQuery(queryExpr)

	// Parse -hetero before flag validation so we can reject bad values early.
	heteroMode, heteroOK := tabular.ParseHeterogeneousMode(*heteroFlag)
	if !heteroOK {
		fmt.Fprintf(stderr, "gq: unknown -hetero value %q; use first, reject, union, or partition\n", *heteroFlag)
		return 2
	}

	if *limitFlag < 0 {
		fmt.Fprintln(stderr, "gq: -limit must be non-negative")
		return 2
	}
	if *offsetFlag < 0 {
		fmt.Fprintln(stderr, "gq: -offset must be non-negative")
		return 2
	}

	warnings, err := validateFlags(*schemaFlag, *typesFlag, queryExpr, *formatFlag, *indexFlag, *limitFlag, *offsetFlag, *compactFlag, *rawFlag, *colorFlag, *noColorFlag, *sortFlag, *sortDescFlag, *sortFoldFlag, *sortDropFlag, *nullOnMissFlag, *timeFormatFlag)
	if err != nil {
		fmt.Fprintf(stderr, "gq: %v\n", err)
		return 2
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr, "gq: %s\n", w)
	}

	path, err := query.Parse(queryExpr)
	if err != nil {
		fmt.Fprintf(stderr, "gq: invalid query expression %q: %v\n", queryExpr, err)
		return 2
	}

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
		if strings.HasSuffix(inputPath, ".gz") {
			gz, err := gzip.NewReader(f)
			if err != nil {
				fmt.Fprintf(stderr, "gq: opening gzip stream: %v\n", err)
				return 1
			}
			defer gz.Close()
			r = gz
		}
	}

	var inspOpts []gobspect.Option
	if *timeFormatFlag != "" {
		inspOpts = append(inspOpts, gobspect.WithTimeFormat(*timeFormatFlag))
	}
	ins := gobspect.New(inspOpts...)

	bytesFormat, ok := gobspect.ParseBytesFormat(*bytesFlag)
	if !ok {
		fmt.Fprintf(stderr, "gq: unknown -bytes value %q; use hex, base64, or literal\n", *bytesFlag)
		return 2
	}

	switch *formatFlag {
	case "pretty", "json", "csv", "tsv":
	default:
		fmt.Fprintf(stderr, "gq: unknown -format value %q; use pretty, json, csv, or tsv\n", *formatFlag)
		return 2
	}

	useColor := false
	if *colorFlag {
		useColor = true
	} else if !*noColorFlag {
		if f, ok := stdout.(*os.File); ok {
			useColor = isTerminal(f)
		}
	}

	if *schemaFlag {
		schema, err := ins.Stream(r).Schema()
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		var s string
		if useColor {
			s = schema.Format(gobspect.SchemaWithColor(gobspect.ANSIColorScheme))
		} else {
			s = schema.String()
		}
		fmt.Fprintln(stdout, s)
		return 0
	}

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

	fmtOpts := []gobspect.FormatOption{
		gobspect.WithBytesFormat(bytesFormat),
		gobspect.WithMaxBytes(*maxBytesFlag),
	}

	stream := ins.Stream(r)

	var tp *tabular.Printer
	if *formatFlag == "csv" || *formatFlag == "tsv" {
		delim := rune(',')
		if *formatFlag == "tsv" {
			delim = '\t'
		}
		tp = tabular.NewPrinter(stdout,
			tabular.WithDelimiter(delim),
			tabular.WithNoHeaders(*noHeadersFlag),
			tabular.WithStream(stream),
			tabular.WithBytesFormat(bytesFormat),
			tabular.WithMaxBytes(*maxBytesFlag),
			tabular.WithHeterogeneousMode(heteroMode),
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

	var sortSpec sortval.SortSpec
	if *sortFlag != "" {
		sortSpec, err = sortval.ParseSortSpec(*sortFlag, *sortDescFlag, *sortFoldFlag, *sortDropFlag)
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 2
		}
	}

	idx := 0
	anyMatch := false
	exitCode := 0
	resultN := 0

	if len(sortSpec.Keys) > 0 {
		var allResults []gobspect.Value
		for v, err := range stream.Values() {
			if err != nil {
				fmt.Fprintf(stderr, "gq: %v\n", err)
				return 1
			}

			if *indexFlag >= 0 && idx != *indexFlag {
				idx++
				continue
			}

			for result := range query.AllPathSeq(v, path) {
				anyMatch = true
				allResults = append(allResults, result)
			}

			idx++

			if *indexFlag >= 0 && idx > *indexFlag {
				break
			}
		}

		sorted := sortval.SortMatches(sortval.SeqOf(allResults), sortSpec)

		for pos, result := range sorted {
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
			resultN++
			if *limitFlag > 0 && resultN >= *limitFlag {
				break
			}
		}
	} else {
	outer:
		for v, err := range stream.Values() {
			if err != nil {
				fmt.Fprintf(stderr, "gq: %v\n", err)
				return 1
			}

			if *indexFlag >= 0 && idx != *indexFlag {
				idx++
				continue
			}

			for result := range query.AllPathSeq(v, path) {
				anyMatch = true

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

			if *indexFlag >= 0 && idx > *indexFlag {
				break
			}
		}
	}

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

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func validateFlags(schema, types bool, queryExpr string, format string, index, limit, offset int, compact, raw, color, noColor bool, sort string, sortDesc, sortFold, sortDrop bool, nullOnMiss bool, timeFormat string) (warnings []string, err error) {
	if color && noColor {
		return nil, fmt.Errorf("cannot use -color and -no-color together")
	}
	if schema && queryExpr != "" {
		warnings = append(warnings, "query expression has no effect with -schema; ignoring")
	}
	if types && queryExpr != "" {
		warnings = append(warnings, "query expression has no effect with -types; ignoring")
	}
	if schema && format != "pretty" {
		warnings = append(warnings, fmt.Sprintf("-format %s has no effect with -schema; ignoring", format))
	}
	if types && format != "pretty" {
		warnings = append(warnings, fmt.Sprintf("-format %s has no effect with -types; ignoring", format))
	}
	if schema && index >= 0 {
		warnings = append(warnings, "-index has no effect with -schema; ignoring")
	}
	if types && index >= 0 {
		warnings = append(warnings, "-index has no effect with -types; ignoring")
	}
	if schema && (limit > 0 || offset > 0) {
		warnings = append(warnings, "-limit/-offset has no effect with -schema; ignoring")
	}
	if types && (limit > 0 || offset > 0) {
		warnings = append(warnings, "-limit/-offset has no effect with -types; ignoring")
	}
	if compact && format != "json" {
		warnings = append(warnings, fmt.Sprintf("-compact has no effect with -format %s; ignoring", format))
	}
	if raw && format != "pretty" {
		warnings = append(warnings, fmt.Sprintf("-r has no effect with -format %s; ignoring", format))
	}
	if schema && sort != "" {
		warnings = append(warnings, "-sort has no effect with -schema; ignoring")
	}
	if types && sort != "" {
		warnings = append(warnings, "-sort has no effect with -types; ignoring")
	}
	if sort == "" && (sortDesc || sortFold || sortDrop) {
		warnings = append(warnings, "-sort-* flags have no effect without -sort")
	}
	if schema && nullOnMiss {
		warnings = append(warnings, "-null-on-miss has no effect with -schema; ignoring")
	}
	if types && nullOnMiss {
		warnings = append(warnings, "-null-on-miss has no effect with -types; ignoring")
	}
	if schema && timeFormat != "" {
		warnings = append(warnings, "-time-format has no effect with -schema; ignoring")
	}
	if types && timeFormat != "" {
		warnings = append(warnings, "-time-format has no effect with -types; ignoring")
	}
	return warnings, nil
}
