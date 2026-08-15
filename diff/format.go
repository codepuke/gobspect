package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/codepuke/gobspect"
)

// FormatOption configures how Format, FormatTo, and FormatStream render a
// Delta. The zero-value configuration is plain text with no color.
type FormatOption func(*formatConfig)

type formatConfig struct {
	color ColorScheme
}

// WithColor wraps added, removed, header, and stream-index lines with the
// given ColorScheme. Pass [ANSIColorScheme] for terminal-ready colors.
func WithColor(cs ColorScheme) FormatOption {
	return func(cfg *formatConfig) { cfg.color = cs }
}

func buildConfig(opts []FormatOption) *formatConfig {
	cfg := &formatConfig{}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// Format renders a Delta as a multi-line text diff. Each added element is
// prefixed with "+", each removed with "-", and each changed leaf shows the
// before line then the after line. Composite deltas descend into children
// with indentation. Pass [WithColor] to enable terminal coloring.
func Format(d Delta, opts ...FormatOption) string {
	var b strings.Builder
	writeDelta(&b, d, "", buildConfig(opts))
	return b.String()
}

// FormatTo renders a Delta to w using the same form as [Format].
func FormatTo(w io.Writer, d Delta, opts ...FormatOption) error {
	_, err := io.WriteString(w, Format(d, opts...))
	return err
}

// FormatStream renders a StreamDelta as indexed entries. The per-entry "[N]"
// marker is styled with ColorScheme.Index when color is configured.
func FormatStream(sd StreamDelta, opts ...FormatOption) string {
	if len(sd.Entries) == 0 {
		return ""
	}
	cfg := buildConfig(opts)
	var b strings.Builder
	for _, e := range sd.Entries {
		b.WriteString(cfg.color.Index.Apply(fmt.Sprintf("[%d]", e.Index)))
		b.WriteByte('\n')
		writeDelta(&b, e.Delta, "  ", cfg)
	}
	return b.String()
}

func writeDelta(b *strings.Builder, d Delta, indent string, cfg *formatConfig) {
	switch dd := d.(type) {
	case nil:
		return
	case Added:
		b.WriteString(indent)
		b.WriteString(cfg.color.Added.Apply("+ " + oneLine(dd.Value)))
		b.WriteByte('\n')
	case Removed:
		b.WriteString(indent)
		b.WriteString(cfg.color.Removed.Apply("- " + oneLine(dd.Value)))
		b.WriteByte('\n')
	case Changed:
		b.WriteString(indent)
		b.WriteString(cfg.color.Removed.Apply("- " + oneLine(dd.Before)))
		b.WriteByte('\n')
		b.WriteString(indent)
		b.WriteString(cfg.color.Added.Apply("+ " + oneLine(dd.After)))
		b.WriteByte('\n')
	case StructDelta:
		header := "~ struct {"
		if dd.TypeName != "" {
			header = "~ " + dd.TypeName + " {"
		}
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply(header))
		b.WriteByte('\n')
		for _, f := range dd.Fields {
			fmt.Fprintf(b, "%s  %s:\n", indent, f.Name)
			writeDelta(b, f.Delta, indent+"    ", cfg)
		}
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("}"))
		b.WriteByte('\n')
	case MapDelta:
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("~ map {"))
		b.WriteByte('\n')
		for _, e := range dd.Entries {
			fmt.Fprintf(b, "%s  [%s]:\n", indent, oneLine(e.Key))
			writeDelta(b, e.Delta, indent+"    ", cfg)
		}
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("}"))
		b.WriteByte('\n')
	case SliceDelta:
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("~ slice {"))
		b.WriteByte('\n')
		for _, e := range dd.Elems {
			fmt.Fprintf(b, "%s  [%d]:\n", indent, e.Index)
			writeDelta(b, e.Delta, indent+"    ", cfg)
		}
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("}"))
		b.WriteByte('\n')
	case ArrayDelta:
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("~ array {"))
		b.WriteByte('\n')
		for _, e := range dd.Elems {
			fmt.Fprintf(b, "%s  [%d]:\n", indent, e.Index)
			writeDelta(b, e.Delta, indent+"    ", cfg)
		}
		b.WriteString(indent)
		b.WriteString(cfg.color.Header.Apply("}"))
		b.WriteByte('\n')
	default:
		fmt.Fprintf(b, "%s? %T\n", indent, dd)
	}
}

// oneLine is a compact single-line rendering of v, used when a delta wants to
// show the value inline rather than nested.
func oneLine(v gobspect.Value) string {
	if v == nil {
		return "<nil>"
	}
	s := gobspect.Format(v)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		// Back the cut up to a rune boundary: slicing mid-rune would emit
		// invalid UTF-8.
		cut := 157
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "..."
	}
	return s
}

// ToJSON serializes a Delta as a discriminated-union JSON document. Each
// node carries a "kind" field identifying which Delta variant it is.
// JSON output is always plain; color options do not apply.
func ToJSON(d Delta) ([]byte, error) { return json.Marshal(deltaToMap(d)) }

// ToJSONIndent is like ToJSON with indentation.
func ToJSONIndent(d Delta, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(deltaToMap(d), prefix, indent)
}

// StreamToJSON serializes a StreamDelta.
func StreamToJSON(sd StreamDelta) ([]byte, error) {
	entries := make([]map[string]any, 0, len(sd.Entries))
	for _, e := range sd.Entries {
		entries = append(entries, map[string]any{
			"index": e.Index,
			"delta": deltaToMap(e.Delta),
		})
	}
	return json.Marshal(map[string]any{"entries": entries})
}

// StreamToJSONIndent is like StreamToJSON with indentation.
func StreamToJSONIndent(sd StreamDelta, prefix, indent string) ([]byte, error) {
	entries := make([]map[string]any, 0, len(sd.Entries))
	for _, e := range sd.Entries {
		entries = append(entries, map[string]any{
			"index": e.Index,
			"delta": deltaToMap(e.Delta),
		})
	}
	return json.MarshalIndent(map[string]any{"entries": entries}, prefix, indent)
}

func deltaToMap(d Delta) map[string]any {
	if d == nil {
		return nil
	}
	switch dd := d.(type) {
	case Added:
		return map[string]any{"kind": "added", "value": valueToJSON(dd.Value)}
	case Removed:
		return map[string]any{"kind": "removed", "value": valueToJSON(dd.Value)}
	case Changed:
		return map[string]any{
			"kind":   "changed",
			"before": valueToJSON(dd.Before),
			"after":  valueToJSON(dd.After),
		}
	case StructDelta:
		fields := make([]map[string]any, 0, len(dd.Fields))
		for _, f := range dd.Fields {
			fields = append(fields, map[string]any{"name": f.Name, "delta": deltaToMap(f.Delta)})
		}
		return map[string]any{"kind": "struct", "typeName": dd.TypeName, "fields": fields}
	case MapDelta:
		entries := make([]map[string]any, 0, len(dd.Entries))
		for _, e := range dd.Entries {
			entries = append(entries, map[string]any{"key": valueToJSON(e.Key), "delta": deltaToMap(e.Delta)})
		}
		return map[string]any{"kind": "map", "entries": entries}
	case SliceDelta:
		return map[string]any{"kind": "slice", "elems": elemDeltasToJSON(dd.Elems)}
	case ArrayDelta:
		return map[string]any{"kind": "array", "elems": elemDeltasToJSON(dd.Elems)}
	default:
		return map[string]any{"kind": "unknown"}
	}
}

func elemDeltasToJSON(elems []ElemDelta) []map[string]any {
	out := make([]map[string]any, 0, len(elems))
	for _, e := range elems {
		out = append(out, map[string]any{"index": e.Index, "delta": deltaToMap(e.Delta)})
	}
	return out
}

func valueToJSON(v gobspect.Value) any {
	if v == nil {
		return nil
	}
	b, err := gobspect.ToJSON(v)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return out
}
