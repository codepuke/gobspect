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
//	gq -f data.gob.gz           # .gz files (and gzipped stdin) are decompressed automatically
//	cat data.gob | gq .Items.*
//	gq -schema -f data.gob
//	gq -format json -f data.gob .Name
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/diff"
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
	printUsage := func(w io.Writer) {
		fmt.Fprintln(w, "usage: gq [flags] [query]")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "flags:")
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(stderr)
	}
	// Usage is printed manually after Parse so that explicit -h/-help goes to
	// stdout with exit 0, while parse errors go to stderr with exit 2.
	fs.Usage = func() {}

	fileFlag := fs.String("file", "", "input file (default: stdin)")
	fs.StringVar(fileFlag, "f", "", "input file (shorthand for -file)")
	formatFlag := fs.String("format", "pretty", "output format: pretty, json, jsonl, csv, or tsv")
	schemaFlag := fs.Bool("schema", false, "print type schema and exit")
	schemaFormatFlag := fs.String("schema-format", "go", "schema output format: go or json")
	typesFlag := fs.Bool("types", false, "print raw type definitions as JSON and exit")
	statsFlag := fs.Bool("stats", false, "print stream-level statistics and exit")
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
	sortFlag := fs.String("sort", "", "comma-separated column names to sort by (each may take a :asc or :desc suffix)")
	sortDescFlag := fs.Bool("sort-desc", false, "default direction for sort keys without an explicit suffix")
	sortFoldFlag := fs.Bool("sort-fold", false, "case-insensitive string comparison in sort")
	sortDropFlag := fs.Bool("sort-drop-missing", false, "exclude rows missing all sort keys")
	skipErrorsFlag := fs.Bool("skip-errors", false, "skip value messages that fail to decode and continue")
	diffFlag := fs.String("diff", "", "path to another .gob file; emit a structural diff against the input, aligned by index")
	countFlag := fs.Bool("count", false, "after filtering, print the number of matches and exit")
	sumFlag := fs.String("sum", "", "path to a numeric field to sum over the matches and exit")
	minFlag := fs.String("min", "", "path to a numeric field to take the minimum over and exit")
	maxFlag := fs.String("max", "", "path to a numeric field to take the maximum over and exit")
	avgFlag := fs.String("avg", "", "path to a numeric field to average over the matches and exit")
	nonfiniteFlag := fs.String("nonfinite", "strings", "JSON rendering of NaN/±Inf floats: strings or null")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return 0
		}
		printUsage(stderr)
		return 2
	}

	// Record which flags were set explicitly: conflict validation must not
	// mistake a default value for a user choice.
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if setFlags["f"] {
		setFlags["file"] = true
		delete(setFlags, "f")
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
		printUsage(stderr)
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
	if *maxBytesFlag < 0 {
		fmt.Fprintln(stderr, "gq: -max-bytes must be non-negative")
		return 2
	}
	if *indexFlag < -1 {
		fmt.Fprintln(stderr, "gq: -index must be non-negative (or -1 for all)")
		return 2
	}
	if *nonfiniteFlag != "strings" && *nonfiniteFlag != "null" {
		fmt.Fprintf(stderr, "gq: unknown -nonfinite value %q; use strings or null\n", *nonfiniteFlag)
		return 2
	}

	if err := validateFlags(setFlags, queryExpr, *formatFlag); err != nil {
		fmt.Fprintf(stderr, "gq: %v\n", err)
		return 2
	}

	path, err := query.Parse(queryExpr)
	if err != nil {
		fmt.Fprintf(stderr, "gq: invalid query expression %q: %v\n", queryExpr, err)
		return 2
	}

	var r io.Reader
	if inputPath == "" {
		r, err = maybeGzip(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "gq: opening gzip stream: %v\n", err)
			return 1
		}
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		defer f.Close()
		r, err = maybeGzip(f)
		if err != nil {
			fmt.Fprintf(stderr, "gq: opening gzip stream: %v\n", err)
			return 1
		}
	}

	var inspOpts []gobspect.Option
	if *timeFormatFlag != "" {
		inspOpts = append(inspOpts, gobspect.WithTimeFormat(*timeFormatFlag))
	}
	if *skipErrorsFlag {
		inspOpts = append(inspOpts, gobspect.WithSkipCorruptValues(true))
	}
	ins := gobspect.New(inspOpts...)

	bytesFormat, ok := gobspect.ParseBytesFormat(*bytesFlag)
	if !ok {
		fmt.Fprintf(stderr, "gq: unknown -bytes value %q; use hex, base64, or literal\n", *bytesFlag)
		return 2
	}

	switch *formatFlag {
	case "pretty", "json", "jsonl", "csv", "tsv":
	default:
		fmt.Fprintf(stderr, "gq: unknown -format value %q; use pretty, json, jsonl, csv, or tsv\n", *formatFlag)
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

	if *diffFlag != "" {
		other, err := os.Open(*diffFlag)
		if err != nil {
			fmt.Fprintf(stderr, "gq: opening diff target: %v\n", err)
			return 1
		}
		defer other.Close()
		otherReader, err := maybeGzip(other)
		if err != nil {
			fmt.Fprintf(stderr, "gq: opening diff target gzip stream: %v\n", err)
			return 1
		}
		leftVals, err := ins.Stream(r).Collect()
		if err != nil {
			fmt.Fprintf(stderr, "gq: decoding input: %v\n", err)
			return 1
		}
		rightVals, err := ins.Stream(otherReader).Collect()
		if err != nil {
			fmt.Fprintf(stderr, "gq: decoding diff target: %v\n", err)
			return 1
		}
		sd := diff.DiffStreams(leftVals, rightVals)
		if *formatFlag == "json" || *formatFlag == "jsonl" {
			out, jerr := diff.StreamToJSONIndent(sd, "", "  ")
			if jerr != nil {
				fmt.Fprintf(stderr, "gq: marshaling diff: %v\n", jerr)
				return 1
			}
			if werr := emit(stdout, out); werr != nil {
				return reportWrite(werr, stderr)
			}
		} else {
			var diffOpts []diff.FormatOption
			if useColor {
				diffOpts = append(diffOpts, diff.WithColor(diff.ANSIColorScheme))
			}
			s := diff.FormatStream(sd, diffOpts...)
			if s == "" {
				// Still emit a visible marker so callers can tell "no change"
				// from "program didn't run".
				s = "(no differences)\n"
			}
			if _, werr := io.WriteString(stdout, s); werr != nil {
				return reportWrite(werr, stderr)
			}
		}
		if diff.StreamHasChanges(sd) {
			return 1 // diff-style exit code when there are changes
		}
		return 0
	}

	if *schemaFlag {
		schema, err := ins.Stream(r).Schema()
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		switch strings.ToLower(*schemaFormatFlag) {
		case "", "go":
			var s string
			if useColor {
				s = schema.Format(gobspect.SchemaWithColor(gobspect.ANSIColorScheme))
			} else {
				s = schema.String()
			}
			if werr := emit(stdout, []byte(s)); werr != nil {
				return reportWrite(werr, stderr)
			}
		case "json":
			out, jerr := schema.JSONIndent("", "  ")
			if jerr != nil {
				fmt.Fprintf(stderr, "gq: marshaling schema: %v\n", jerr)
				return 1
			}
			if werr := emit(stdout, out); werr != nil {
				return reportWrite(werr, stderr)
			}
		default:
			fmt.Fprintf(stderr, "gq: unknown -schema-format value %q; use go or json\n", *schemaFormatFlag)
			return 2
		}
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
		if werr := emit(stdout, out); werr != nil {
			return reportWrite(werr, stderr)
		}
		return 0
	}

	if *statsFlag {
		s := ins.Stream(r)
		stats, err := s.Stats()
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		if *formatFlag == "json" {
			out, jerr := stats.JSONIndent("", "  ")
			if jerr != nil {
				fmt.Fprintf(stderr, "gq: marshaling stats: %v\n", jerr)
				return 1
			}
			if werr := emit(stdout, out); werr != nil {
				return reportWrite(werr, stderr)
			}
			return 0
		}
		if err := stats.Format(stdout); err != nil {
			return reportWrite(err, stderr)
		}
		return 0
	}

	fmtOpts := []gobspect.FormatOption{
		gobspect.WithBytesFormat(bytesFormat),
		gobspect.WithMaxBytes(*maxBytesFlag),
	}
	var jsonOpts []gobspect.JSONOption
	if *nonfiniteFlag == "null" {
		jsonOpts = append(jsonOpts, gobspect.WithNonFiniteAsNull(true))
	}

	// Aggregation mode: if -count, -sum, -min, -max, or -avg is set, drain
	// the stream, run the query, reduce the matches, and print a single
	// result. Aggregation is mutually exclusive with ordinary per-value
	// output to keep the semantics unambiguous.
	aggMode := ""
	aggPath := ""
	switch {
	case *countFlag:
		aggMode = "count"
	case *sumFlag != "":
		aggMode, aggPath = "sum", *sumFlag
	case *minFlag != "":
		aggMode, aggPath = "min", *minFlag
	case *maxFlag != "":
		aggMode, aggPath = "max", *maxFlag
	case *avgFlag != "":
		aggMode, aggPath = "avg", *avgFlag
	}
	if aggMode != "" {
		stream := ins.Stream(r)
		return runAggregate(aggMode, aggPath, stream, path, stdout, stderr)
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

		var sorted []gobspect.Value
		if tp != nil && heteroMode == tabular.HeterogeneousPartition {
			// Per-partition sort: bucket results by struct type in arrival
			// order, sort within each bucket, then concat.
			sorted = sortPerPartition(allResults, sortSpec)
		} else {
			sorted = sortval.SortMatches(sortval.SeqOf(allResults), sortSpec)
		}

		for pos, result := range sorted {
			if pos < *offsetFlag {
				continue
			}
			var writeErr error
			if tp != nil {
				writeErr = tp.WriteValue(result)
			} else {
				writeErr = printValue(result, stdout, *formatFlag, *rawFlag, *compactFlag, useColor, fmtOpts, jsonOpts)
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
					writeErr = printValue(result, stdout, *formatFlag, *rawFlag, *compactFlag, useColor, fmtOpts, jsonOpts)
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
func printValue(v gobspect.Value, w io.Writer, format string, raw, compact, color bool, fmtOpts []gobspect.FormatOption, jsonOpts []gobspect.JSONOption) error {
	switch format {
	case "json":
		var out []byte
		var err error
		if compact {
			out, err = gobspect.ToJSON(v, jsonOpts...)
		} else {
			out, err = gobspect.ToJSONIndent(v, "", "  ", jsonOpts...)
		}
		if err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w)
		return err

	case "jsonl":
		// One compact JSON object per line, always — no multi-line indentation.
		out, err := gobspect.ToJSON(v, jsonOpts...)
		if err != nil {
			return fmt.Errorf("encoding JSONL: %w", err)
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

// maybeGzip sniffs the gzip magic bytes and transparently decompresses when
// present. Detection is content-based for files and stdin alike — a gzipped
// file works regardless of its extension. The gzip layer needs no explicit
// Close: it wraps a reader whose lifetime the caller already manages.
func maybeGzip(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	if head, _ := br.Peek(2); len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
		return gzip.NewReader(br)
	}
	return br, nil
}

// emit writes out followed by a newline to w.
func emit(w io.Writer, out []byte) error {
	if _, err := w.Write(out); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

// reportWrite converts a write error into an exit code: EPIPE is a normal
// downstream close (exit 0), anything else is reported on stderr (exit 1).
func reportWrite(err error, stderr io.Writer) int {
	if errors.Is(err, syscall.EPIPE) {
		return 0
	}
	fmt.Fprintf(stderr, "gq: %v\n", err)
	return 1
}

// runAggregate drains the stream, applies path to each top-level value,
// collects matches, reduces them according to mode, and prints a single
// result to stdout. mode is one of "count", "sum", "min", "max", "avg". For
// numeric modes, aggPath is parsed as an extra path expression to apply to
// each match before numeric extraction (empty = use the match itself).
// Exit codes: 0 on success, 1 on decode error, 2 on bad numeric input.
func runAggregate(mode string, aggPath string, stream *gobspect.Stream, matchPath query.Path, stdout, stderr io.Writer) int {
	var numericPath query.Path
	if aggPath != "" {
		var err error
		numericPath, err = query.Parse(query.NormalizeQuery(aggPath))
		if err != nil {
			fmt.Fprintf(stderr, "gq: invalid aggregation path %q: %v\n", aggPath, err)
			return 2
		}
	}

	var acc numAcc
	var matchCount int64

	pushNumeric := func(v gobspect.Value) error {
		i, f, isInt, ok := toNumeric(v)
		if !ok {
			return fmt.Errorf("non-numeric value for aggregation: %s", gobspect.ValueKind(v))
		}
		acc.push(i, f, isInt)
		return nil
	}

	for v, err := range stream.Values() {
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		for result := range query.AllPathSeq(v, matchPath) {
			matchCount++
			if mode == "count" {
				continue
			}
			target := result
			if aggPath != "" {
				// Apply the numeric path to the result; take only the first
				// resolution so arithmetic stays well-defined.
				r, ok := query.GetPath(result, numericPath)
				if !ok {
					continue
				}
				target = r
			}
			if err := pushNumeric(target); err != nil {
				fmt.Fprintf(stderr, "gq: -%s %q: %v\n", mode, aggPath, err)
				return 2
			}
		}
	}

	switch mode {
	case "count":
		fmt.Fprintln(stdout, matchCount)
	case "sum":
		fmt.Fprintln(stdout, acc.sumString())
	case "min":
		if !acc.sawAny {
			fmt.Fprintln(stdout, "null")
			return 0
		}
		fmt.Fprintln(stdout, acc.minString())
	case "max":
		if !acc.sawAny {
			fmt.Fprintln(stdout, "null")
			return 0
		}
		fmt.Fprintln(stdout, acc.maxString())
	case "avg":
		if acc.count == 0 {
			fmt.Fprintln(stdout, "null")
			return 0
		}
		fmt.Fprintln(stdout, formatFloat(acc.sumFloat/float64(acc.count)))
	}
	return 0
}

// numAcc accumulates numeric aggregation state. Integer inputs are tracked
// exactly in int64 alongside the float64 mirror; sum and min/max degrade to
// the float path only when a float value appears, an integer doesn't fit in
// int64, or the running sum overflows — so e.g. summing IDs above 2^53 stays
// exact instead of silently rounding.
type numAcc struct {
	count  int64
	sawAny bool

	intMode bool // all values so far were int64-exact and sumInt hasn't overflowed
	sumInt  int64
	minInt  int64
	maxInt  int64

	sumFloat float64
	minFloat float64
	maxFloat float64
}

// push records one value. For integer-exact inputs isInt is true and i holds
// the value; f always holds the float64 mirror.
func (a *numAcc) push(i int64, f float64, isInt bool) {
	if !a.sawAny {
		a.sawAny = true
		a.intMode = isInt
		a.sumInt, a.minInt, a.maxInt = i, i, i
		a.sumFloat, a.minFloat, a.maxFloat = f, f, f
		a.count = 1
		return
	}
	a.count++
	a.sumFloat += f
	a.minFloat = min(a.minFloat, f)
	a.maxFloat = max(a.maxFloat, f)
	if a.intMode {
		if !isInt {
			a.intMode = false
			return
		}
		if s, ok := addInt64(a.sumInt, i); ok {
			a.sumInt = s
		} else {
			a.intMode = false
			return
		}
		a.minInt = min(a.minInt, i)
		a.maxInt = max(a.maxInt, i)
	}
}

func (a *numAcc) sumString() string {
	if a.intMode {
		return strconv.FormatInt(a.sumInt, 10)
	}
	return formatFloat(a.sumFloat)
}

func (a *numAcc) minString() string {
	if a.intMode {
		return strconv.FormatInt(a.minInt, 10)
	}
	return formatFloat(a.minFloat)
}

func (a *numAcc) maxString() string {
	if a.intMode {
		return strconv.FormatInt(a.maxInt, 10)
	}
	return formatFloat(a.maxFloat)
}

// addInt64 adds two int64s, reporting false on overflow.
func addInt64(a, b int64) (int64, bool) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, false
	}
	return s, true
}

// toNumeric extracts a numeric value from v. isInt reports that i holds the
// exact value; f always holds the float64 form. Returns ok=false for
// non-numeric kinds (including strings and opaques).
func toNumeric(v gobspect.Value) (i int64, f float64, isInt, ok bool) {
	if iv, isIface := v.(gobspect.InterfaceValue); isIface {
		v = iv.Value
	}
	switch n := v.(type) {
	case gobspect.IntValue:
		return n.V, float64(n.V), true, true
	case gobspect.UintValue:
		if n.V <= math.MaxInt64 {
			return int64(n.V), float64(n.V), true, true
		}
		return 0, float64(n.V), false, true
	case gobspect.FloatValue:
		return 0, n.V, false, true
	}
	return 0, 0, false, false
}

// formatFloat renders a numeric accumulator compactly. Integer-valued floats
// drop the trailing ".0" so e.g. a count-like sum of 10 reads as "10" instead
// of "10.000000". The bounds check keeps the int64 conversion in range: out of
// range it is implementation-defined (arm64 saturates, making exactly 2^63
// print as MaxInt64).
func formatFloat(f float64) string {
	if f >= math.MinInt64 && f < math.MaxInt64 && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// sortPerPartition buckets results by struct type (GobTypeID) in their first-
// arrival order, sorts within each bucket, and returns the concatenation.
// Values with GobTypeID == 0 (scalars, projections) form a single leading bucket.
// This matches the tabular partition printer's notion of a "partition".
func sortPerPartition(results []gobspect.Value, spec sortval.SortSpec) []gobspect.Value {
	type bucket struct {
		id   int
		vals []gobspect.Value
	}
	order := []int{}          // bucket IDs in first-arrival order
	byID := map[int]*bucket{} // ID → bucket
	for _, v := range results {
		id := partitionID(v)
		b, ok := byID[id]
		if !ok {
			b = &bucket{id: id}
			byID[id] = b
			order = append(order, id)
		}
		b.vals = append(b.vals, v)
	}
	out := make([]gobspect.Value, 0, len(results))
	for _, id := range order {
		sorted := sortval.SortMatches(sortval.SeqOf(byID[id].vals), spec)
		out = append(out, sorted...)
	}
	return out
}

// partitionID returns the struct GobTypeID of v, unwrapping InterfaceValue.
// Non-struct values and structs with no type ID return 0.
func partitionID(v gobspect.Value) int {
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		v = iv.Value
	}
	if sv, ok := v.(gobspect.StructValue); ok {
		return sv.GobTypeID
	}
	return 0
}

// allFlagNames lists every gq flag in a stable order for deterministic
// validation error messages. Shorthand "f" is normalized to "file" before
// validation.
var allFlagNames = []string{
	"file", "format", "schema", "schema-format", "types", "stats", "index",
	"bytes", "max-bytes", "color", "no-color", "r", "compact", "null-on-miss",
	"time-format", "no-headers", "hetero", "limit", "offset", "sort",
	"sort-desc", "sort-fold", "sort-drop-missing", "skip-errors", "diff",
	"count", "sum", "min", "max", "avg", "nonfinite",
}

// aggFlagNames are the mutually exclusive aggregation flags; any one of them
// selects aggregate mode.
var aggFlagNames = []string{"count", "sum", "min", "max", "avg"}

// modeFlags are extra flags meaningful in each non-normal mode, beyond the
// universal set. A set flag not listed here (or in universalFlags) is an
// error for that mode.
var modeFlags = map[string]map[string]bool{
	"diff":      {"format": true, "time-format": true},
	"schema":    {"schema-format": true},
	"types":     {},
	"stats":     {"format": true},
	"aggregate": {"time-format": true},
}

// universalFlags are meaningful regardless of mode: input selection, decode
// behavior, color control, and the mode selectors themselves.
var universalFlags = map[string]bool{
	"file": true, "color": true, "no-color": true, "skip-errors": true,
	"diff": true, "schema": true, "types": true, "stats": true,
	"count": true, "sum": true, "min": true, "max": true, "avg": true,
}

// validateFlags rejects flag combinations where a flag would be silently
// ignored or contradicts the selected mode. set holds the names of flags the
// user passed explicitly, so defaults never trigger false conflicts.
func validateFlags(set map[string]bool, queryExpr string, format string) error {
	if set["color"] && set["no-color"] {
		return fmt.Errorf("cannot use -color and -no-color together")
	}

	var aggSet []string
	for _, f := range aggFlagNames {
		if set[f] {
			aggSet = append(aggSet, "-"+f)
		}
	}
	if len(aggSet) > 1 {
		return fmt.Errorf("aggregation flags %s are mutually exclusive", strings.Join(aggSet, ", "))
	}

	mode, modeFlag := "normal", ""
	var modes []string
	for _, m := range []string{"diff", "schema", "types", "stats"} {
		if set[m] {
			modes = append(modes, "-"+m)
			mode, modeFlag = m, "-"+m
		}
	}
	if len(aggSet) == 1 {
		modes = append(modes, aggSet[0])
		mode, modeFlag = "aggregate", aggSet[0]
	}
	if len(modes) > 1 {
		return fmt.Errorf("%s select conflicting modes; use one", strings.Join(modes, ", "))
	}

	if mode == "normal" {
		if set["schema-format"] {
			return fmt.Errorf("-schema-format has no effect without -schema")
		}
		if set["compact"] && format != "json" {
			if format == "jsonl" {
				return fmt.Errorf("-compact has no effect with -format jsonl (jsonl is always compact)")
			}
			return fmt.Errorf("-compact has no effect with -format %s", format)
		}
		if set["r"] && format != "pretty" {
			return fmt.Errorf("-r has no effect with -format %s", format)
		}
		if set["nonfinite"] && format != "json" && format != "jsonl" {
			return fmt.Errorf("-nonfinite has no effect with -format %s", format)
		}
		if !set["sort"] {
			for _, f := range []string{"sort-desc", "sort-fold", "sort-drop-missing"} {
				if set[f] {
					return fmt.Errorf("-%s has no effect without -sort", f)
				}
			}
		}
		return nil
	}

	for _, name := range allFlagNames {
		if !set[name] || universalFlags[name] || modeFlags[mode][name] {
			continue
		}
		return fmt.Errorf("-%s has no effect with %s", name, modeFlag)
	}
	if queryExpr != "" && mode != "aggregate" {
		return fmt.Errorf("query expression has no effect with %s", modeFlag)
	}

	// Per-mode output format restrictions.
	switch mode {
	case "diff":
		if set["format"] && format != "pretty" && format != "json" && format != "jsonl" {
			return fmt.Errorf("-format %s is not supported with -diff; use pretty, json, or jsonl", format)
		}
	case "stats":
		if set["format"] && format != "pretty" && format != "json" {
			return fmt.Errorf("-format %s is not supported with -stats; use pretty or json", format)
		}
	}
	return nil
}
