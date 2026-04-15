// Command gq is a jq-inspired CLI for inspecting gob binary streams.
//
// Usage:
//
//	gq [flags] [query] [file]
//
// With no arguments, reads from stdin and prints all top-level values.
// With one argument, it is treated as a file path if it exists on disk,
// otherwise as a query expression applied to stdin.
// With two arguments, the first is the query expression and the second is the file.
//
// Query expressions use the gobspect/query path syntax (dot-separated field
// names, integer indices, wildcards, filters). An empty or "." expression is
// the identity: it matches the whole value.
//
// Examples:
//
//	gq data.gob
//	gq .Header.Timestamp data.gob
//	cat data.gob | gq .Items.*
//	gq --schema data.gob
//	gq --format json .Name data.gob
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
)

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("gq", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: gq [flags] [query] [file]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

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

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}

	args := fs.Args()

	// Resolve queryExpr and inputPath from positional arguments.
	var queryExpr string
	var inputPath string // empty = stdin

	switch len(args) {
	case 0:
		// No args: identity query, stdin.
	case 1:
		// One arg: file if it exists on disk, otherwise query on stdin.
		if _, err := os.Stat(args[0]); err == nil {
			inputPath = args[0]
		} else {
			queryExpr = args[0]
		}
	case 2:
		queryExpr = args[0]
		inputPath = args[1]
	default:
		fmt.Fprintln(os.Stderr, "gq: too many arguments")
		fs.Usage()
		return 2
	}

	// Normalise the query: "." is the identity; ".Foo" → "Foo" (jq-style prefix).
	// Preserve ".." which is valid descent syntax.
	if queryExpr == "." {
		queryExpr = ""
	} else if strings.HasPrefix(queryExpr, ".") && !strings.HasPrefix(queryExpr, "..") {
		queryExpr = queryExpr[1:]
	}

	// Parse path before opening any file so bad expressions fail fast.
	path, err := query.Parse(queryExpr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gq: invalid query expression %q: %v\n", queryExpr, err)
		return 2
	}

	// Open input.
	var r io.Reader
	if inputPath == "" {
		r = os.Stdin
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gq: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "gq: unknown --bytes value %q; use hex, base64, or literal\n", *bytesFlag)
		return 2
	}

	// Validate --format.
	switch *formatFlag {
	case "pretty", "json", "csv", "tsv":
		// ok
	default:
		fmt.Fprintf(os.Stderr, "gq: unknown --format value %q; use pretty, json, csv, or tsv\n", *formatFlag)
		return 2
	}

	// Determine color: auto (TTY detection) unless forced.
	useColor := false
	if *colorFlag {
		useColor = true
	} else if !*noColorFlag {
		useColor = isTerminal(os.Stdout)
	}

	// --schema mode.
	if *schemaFlag {
		schema, err := ins.DecodeSchema(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gq: %v\n", err)
			return 1
		}
		if useColor {
			fmt.Println(colorizeSchema(schema))
		} else {
			fmt.Println(schema)
		}
		return 0
	}

	// --types mode.
	if *typesFlag {
		types, err := ins.DecodeTypes(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gq: %v\n", err)
			return 1
		}
		out, jerr := json.MarshalIndent(types, "", "  ")
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gq: marshaling types: %v\n", jerr)
			return 1
		}
		os.Stdout.Write(out)
		fmt.Println()
		return 0
	}

	// Build Format options.
	fmtOpts := []gobspect.FormatOption{
		gobspect.WithBytesFormat(bytesFormat),
		gobspect.WithMaxBytes(*maxBytesFlag),
	}

	// Set up tabular printer for csv/tsv.
	var tp *tabularPrinter
	if *formatFlag == "csv" || *formatFlag == "tsv" {
		delim := ','
		if *formatFlag == "tsv" {
			delim = '\t'
		}
		tp = newTabularPrinter(os.Stdout, delim, *noHeadersFlag)
		defer func() {
			tp.Flush()
		}()
	}

	// Value mode: iterate the stream.
	idx := 0
	anyMatch := false
	exitCode := 0

	for v, err := range ins.Values(r) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "gq: %v\n", err)
			return 1
		}

		// --index filtering.
		if *indexFlag >= 0 && idx != *indexFlag {
			idx++
			continue
		}

		// Apply query path.
		var results []gobspect.Value
		if len(path.String()) == 0 {
			results = []gobspect.Value{v}
		} else {
			results = query.AllPath(v, path)
		}

		if len(results) == 0 {
			if !*nullOnMissFlag {
				// Don't exit yet; try remaining values.
			}
			idx++
			continue
		}

		anyMatch = true

		for _, result := range results {
			if tp != nil {
				if err := tp.WriteValue(result); err != nil {
					fmt.Fprintf(os.Stderr, "gq: %v\n", err)
					return 1
				}
			} else {
				if err := printValue(result, *formatFlag, *rawFlag, *compactFlag, useColor, fmtOpts); err != nil {
					fmt.Fprintf(os.Stderr, "gq: %v\n", err)
					return 1
				}
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
			fmt.Println("null")
		} else {
			fmt.Fprintf(os.Stderr, "gq: path %q not found\n", queryExpr)
			exitCode = 1
		}
	}

	return exitCode
}

// printValue renders and writes a single value to stdout.
func printValue(v gobspect.Value, format string, raw, compact, color bool, fmtOpts []gobspect.FormatOption) error {
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
		os.Stdout.Write(out)
		fmt.Println()

	default: // "pretty"
		s := gobspect.Format(v, fmtOpts...)
		if raw {
			if sv, ok := v.(gobspect.StringValue); ok {
				s = sv.V
			}
		}
		if color {
			s = colorize(s)
		}
		fmt.Println(s)
	}
	return nil
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
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
