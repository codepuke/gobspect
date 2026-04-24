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
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: gq [flags] [query]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "flags:")
		fs.PrintDefaults()
	}

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

	warnings, err := validateFlags(*schemaFlag, *typesFlag, queryExpr, *formatFlag, *indexFlag, *limitFlag, *offsetFlag, *compactFlag, *rawFlag, *colorFlag, *noColorFlag, *sortFlag, *sortDescFlag, *sortFoldFlag, *sortDropFlag, *nullOnMissFlag, *timeFormatFlag, *schemaFormatFlag)
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
		br := bufio.NewReader(stdin)
		if head, _ := br.Peek(2); len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
			gz, err := gzip.NewReader(br)
			if err != nil {
				fmt.Fprintf(stderr, "gq: opening gzip stream: %v\n", err)
				return 1
			}
			defer gz.Close()
			r = gz
		} else {
			r = br
		}
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
		var otherReader io.Reader = other
		if strings.HasSuffix(*diffFlag, ".gz") {
			gz, err := gzip.NewReader(other)
			if err != nil {
				fmt.Fprintf(stderr, "gq: opening diff target gzip stream: %v\n", err)
				return 1
			}
			defer gz.Close()
			otherReader = gz
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
			stdout.Write(out)
			fmt.Fprintln(stdout)
		} else {
			var diffOpts []diff.FormatOption
			if useColor {
				diffOpts = append(diffOpts, diff.WithColor(diff.ANSIColorScheme))
			}
			s := diff.FormatStream(sd, diffOpts...)
			if s == "" {
				// Still emit a visible marker so callers can tell "no change"
				// from "program didn't run".
				fmt.Fprintln(stdout, "(no differences)")
			} else {
				fmt.Fprint(stdout, s)
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
			fmt.Fprintln(stdout, s)
		case "json":
			out, jerr := schema.JSONIndent("", "  ")
			if jerr != nil {
				fmt.Fprintf(stderr, "gq: marshaling schema: %v\n", jerr)
				return 1
			}
			stdout.Write(out)
			fmt.Fprintln(stdout)
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
		stdout.Write(out)
		fmt.Fprintln(stdout)
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
			stdout.Write(out)
			fmt.Fprintln(stdout)
			return 0
		}
		if err := stats.Format(stdout); err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		return 0
	}

	fmtOpts := []gobspect.FormatOption{
		gobspect.WithBytesFormat(bytesFormat),
		gobspect.WithMaxBytes(*maxBytesFlag),
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

	case "jsonl":
		// One compact JSON object per line, always — no multi-line indentation.
		out, err := gobspect.ToJSON(v)
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

	// Numeric accumulators; we track integer and float separately so values
	// that fit in int64/uint64 retain precision, falling back to float64
	// when a float appears.
	type numeric struct {
		asFloat float64
		count   int64
		minInit bool
		min     float64
		max     float64
		sawAny  bool
	}
	var acc numeric
	var matchCount int64

	pushNumeric := func(v gobspect.Value) error {
		f, ok := toFloat(v)
		if !ok {
			return fmt.Errorf("non-numeric value for aggregation: %s", gobspect.ValueKind(v))
		}
		acc.asFloat += f
		acc.count++
		acc.sawAny = true
		if !acc.minInit {
			acc.min = f
			acc.max = f
			acc.minInit = true
		} else {
			if f < acc.min {
				acc.min = f
			}
			if f > acc.max {
				acc.max = f
			}
		}
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
		fmt.Fprintln(stdout, formatFloat(acc.asFloat))
	case "min":
		if !acc.sawAny {
			fmt.Fprintln(stdout, "null")
			return 0
		}
		fmt.Fprintln(stdout, formatFloat(acc.min))
	case "max":
		if !acc.sawAny {
			fmt.Fprintln(stdout, "null")
			return 0
		}
		fmt.Fprintln(stdout, formatFloat(acc.max))
	case "avg":
		if acc.count == 0 {
			fmt.Fprintln(stdout, "null")
			return 0
		}
		fmt.Fprintln(stdout, formatFloat(acc.asFloat/float64(acc.count)))
	}
	return 0
}

// toFloat extracts a numeric value from v. Returns (0, false) for non-numeric
// kinds (including strings and opaques).
func toFloat(v gobspect.Value) (float64, bool) {
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		v = iv.Value
	}
	switch n := v.(type) {
	case gobspect.IntValue:
		return float64(n.V), true
	case gobspect.UintValue:
		return float64(n.V), true
	case gobspect.FloatValue:
		return n.V, true
	}
	return 0, false
}

// formatFloat renders a numeric accumulator compactly. Integer-valued floats
// drop the trailing ".0" so e.g. a count-like sum of 10 reads as "10" instead
// of "10.000000".
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
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
	order := []int{}           // bucket IDs in first-arrival order
	byID := map[int]*bucket{}  // ID → bucket
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

func validateFlags(schema, types bool, queryExpr string, format string, index, limit, offset int, compact, raw, color, noColor bool, sort string, sortDesc, sortFold, sortDrop bool, nullOnMiss bool, timeFormat string, schemaFormat string) (warnings []string, err error) {
	if color && noColor {
		return nil, fmt.Errorf("cannot use -color and -no-color together")
	}
	if !schema && schemaFormat != "" && schemaFormat != "go" {
		warnings = append(warnings, "-schema-format has no effect without -schema; ignoring")
	}
	if compact && format == "jsonl" {
		warnings = append(warnings, "-compact has no effect with -format jsonl (jsonl is always compact); ignoring")
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
	if compact && format != "json" && format != "jsonl" {
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
