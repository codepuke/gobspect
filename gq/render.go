package gq

import (
	"fmt"
	"io"

	"github.com/codepuke/gobspect"
)

// Format selects the rendering of a value written by [Render].
type Format int

const (
	// FormatPretty is the human-readable indented rendering produced by
	// [gobspect.Format].
	FormatPretty Format = iota
	// FormatJSON is indented JSON (or compact with [RenderOptions.Compact]).
	FormatJSON
	// FormatJSONL is one compact JSON object per line, never indented.
	FormatJSONL
)

// ParseFormat parses a format name. Recognized names are "pretty", "json",
// and "jsonl"; the empty string parses as FormatPretty, mirroring the gq
// command's default.
func ParseFormat(s string) (Format, bool) {
	switch s {
	case "", "pretty":
		return FormatPretty, true
	case "json":
		return FormatJSON, true
	case "jsonl":
		return FormatJSONL, true
	default:
		return FormatPretty, false
	}
}

// RenderOptions configures [Render]. The zero value renders pretty output
// with default formatting.
type RenderOptions struct {
	Format Format

	// Raw prints string results without surrounding quotes (pretty only).
	// Interface wrappers are unwrapped — through every nested layer — before
	// the string check.
	Raw bool

	// Compact suppresses JSON indentation (FormatJSON only; FormatJSONL is
	// always compact).
	Compact bool

	// Color enables ANSI color output (pretty only).
	Color bool

	// FormatOptions are passed through to [gobspect.FormatTo] for pretty
	// output.
	FormatOptions []gobspect.FormatOption

	// JSONOptions are passed through to [gobspect.ToJSON] and
	// [gobspect.ToJSONIndent] for JSON output.
	JSONOptions []gobspect.JSONOption
}

// Render writes one value to w in the selected format, followed by a
// newline.
func Render(w io.Writer, v gobspect.Value, o RenderOptions) error {
	switch o.Format {
	case FormatJSON:
		var out []byte
		var err error
		if o.Compact {
			out, err = gobspect.ToJSON(v, o.JSONOptions...)
		} else {
			out, err = gobspect.ToJSONIndent(v, "", "  ", o.JSONOptions...)
		}
		if err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w)
		return err

	case FormatJSONL:
		// One compact JSON object per line, always — no multi-line indentation.
		out, err := gobspect.ToJSON(v, o.JSONOptions...)
		if err != nil {
			return fmt.Errorf("encoding JSONL: %w", err)
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w)
		return err

	default: // FormatPretty
		opts := o.FormatOptions
		if o.Color {
			// Full-slice expression so the append cannot scribble on spare
			// capacity in the caller's options slice across calls.
			opts = append(opts[:len(opts):len(opts)], gobspect.WithColor(gobspect.ANSIColorScheme))
		}
		if o.Raw {
			if sv, ok := gobspect.Unwrap(v).(gobspect.StringValue); ok {
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
