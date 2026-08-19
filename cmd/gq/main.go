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
//	gq -f data.gob.gz           # compressed input (gzip, zstd, xz, bzip2, zip) is
//	                            # detected by content and decompressed automatically
//	cat data.gob | gq .Items.*
//	gq -schema -f data.gob
//	gq -format json -f data.gob .Name
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
	"github.com/codepuke/gobspect/decompress"
	"github.com/codepuke/gobspect/diff"
	"github.com/codepuke/gobspect/gq"
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
	readLimitFlag := fs.Int64("read-limit", 0, "max decompressed bytes to read from the input (0 = no limit)")

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
	if *readLimitFlag < 0 {
		fmt.Fprintln(stderr, "gq: -read-limit must be non-negative")
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
		r, err = decompress.Reader(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
	} else {
		f, err := os.Open(inputPath)
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
			return 1
		}
		defer f.Close()
		r, err = decompress.Reader(f)
		if err != nil {
			fmt.Fprintf(stderr, "gq: %v\n", err)
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
	if *readLimitFlag > 0 {
		inspOpts = append(inspOpts, gobspect.WithReadLimit(*readLimitFlag))
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
		otherReader, err := decompress.Reader(other)
		if err != nil {
			fmt.Fprintf(stderr, "gq: opening diff target: %v\n", err)
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

	renderFormat, _ := gq.ParseFormat(*formatFlag) // csv/tsv route through tp instead
	renderOpts := gq.RenderOptions{
		Format:        renderFormat,
		Raw:           *rawFlag,
		Compact:       *compactFlag,
		Color:         useColor,
		FormatOptions: fmtOpts,
		JSONOptions:   jsonOpts,
	}

	pipeline := gq.Pipeline{
		Path:   path,
		Index:  *indexFlag,
		Offset: *offsetFlag,
		Limit:  *limitFlag,
		Sort:   sortSpec,
	}

	exitCode := 0
	var anyMatch bool
	if tp != nil {
		anyMatch, err = pipeline.RunTabular(stream, tp)
	} else {
		anyMatch, err = pipeline.RunRender(stream, stdout, renderOpts)
	}
	if err != nil {
		var sinkErr *gq.SinkError
		if errors.As(err, &sinkErr) && errors.Is(err, syscall.EPIPE) {
			// Downstream closed the pipe; normal termination.
			return 0
		}
		fmt.Fprintf(stderr, "gq: %v\n", err)
		return 1
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

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
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
// reduces the matches according to mode via [gq.Aggregate], and prints a
// single result to stdout. mode is one of "count", "sum", "min", "max",
// "avg". For numeric modes, aggPath is parsed as an extra path expression to
// apply to each match before numeric extraction (empty = use the match
// itself). Exit codes: 0 on success, 1 on decode error, 2 on bad numeric
// input.
func runAggregate(mode string, aggPath string, stream *gobspect.Stream, matchPath query.Path, stdout, stderr io.Writer) int {
	op, ok := gq.ParseAggregateOp(mode)
	if !ok {
		fmt.Fprintf(stderr, "gq: unknown aggregation mode %q\n", mode)
		return 2
	}

	var numericPath query.Path
	if aggPath != "" {
		var err error
		numericPath, err = query.Parse(query.NormalizeQuery(aggPath))
		if err != nil {
			fmt.Fprintf(stderr, "gq: invalid aggregation path %q: %v\n", aggPath, err)
			return 2
		}
	}

	res, err := gq.Aggregate(stream, matchPath, op, numericPath)
	if err != nil {
		if errors.Is(err, gq.ErrNonNumeric) {
			fmt.Fprintf(stderr, "gq: -%s %q: %v\n", mode, aggPath, err)
			return 2
		}
		fmt.Fprintf(stderr, "gq: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, res.String())
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
	"count", "sum", "min", "max", "avg", "nonfinite", "read-limit",
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
	"read-limit": true,
	"diff":       true, "schema": true, "types": true, "stats": true,
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
