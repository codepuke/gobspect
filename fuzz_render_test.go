// Fuzzing baseline: 2026-08-17. Ran 3h, 1206 corpus entries. Found the Equal
// reflexivity defect on nested InterfaceValue (44 restarts on that one bug).
package gobspect_test

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/codepuke/gobspect"
)

// renderCase is one named bundle of formatting options.
type renderCase struct {
	name string
	opts []gobspect.FormatOption
}

func renderCases() []renderCase {
	return []renderCase{
		{"default", nil},
		{"color", []gobspect.FormatOption{gobspect.WithColor(gobspect.ANSIColorScheme)}},
		{"hex", []gobspect.FormatOption{gobspect.WithBytesFormat(gobspect.BytesHex), gobspect.WithMaxBytes(8)}},
		{"base64", []gobspect.FormatOption{gobspect.WithBytesFormat(gobspect.BytesBase64), gobspect.WithMaxBytes(0)}},
		{"literal", []gobspect.FormatOption{gobspect.WithBytesFormat(gobspect.BytesLiteral), gobspect.WithMaxBytes(3)}},
		{"insertion order", []gobspect.FormatOption{gobspect.WithMapOrder(gobspect.MapOrderInsertion)}},
		{"raw opaques", []gobspect.FormatOption{gobspect.WithRawOpaques(true)}},
		{"narrow inline", []gobspect.FormatOption{gobspect.WithInlineWidth(1), gobspect.WithIndent("\t")}},
		{"wide inline", []gobspect.FormatOption{gobspect.WithInlineWidth(1 << 20), gobspect.WithIndent("")}},
		{"redact keys", []gobspect.FormatOption{gobspect.WithRedactKeys(gobspect.RedactConfig{
			Keys: []string{"Name", "V", "X", "foo", ""}, Char: '█',
		})}},
		{"redact keys fixed width", []gobspect.FormatOption{gobspect.WithRedactKeys(gobspect.RedactConfig{
			Keys: []string{"Name", "Pt"}, TextLength: 4,
		})}},
		{"redact types", []gobspect.FormatOption{gobspect.WithRedactTypes(gobspect.RedactTypesConfig{
			Types: []string{"Point", "time.Time", "string", ""}, Char: '#',
		})}},
	}
}

// FuzzRender drives everything downstream of decoding: a Value tree decoded
// from a hostile stream is itself untrusted data, and it flows straight into
// the formatters and JSON encoders.
//
// Beyond "does not panic", two real properties are checked. Format must be
// deterministic — the same value and options must render identically twice, or
// map ordering and truncation are reading uninitialised state. And ToJSON must
// emit syntactically valid JSON for every value it accepts, since callers pipe
// its output straight into a JSON consumer.
func FuzzRender(f *testing.F) {
	for _, seed := range fuzzSeedCorpus(f, "testdata") {
		f.Add(seed)
	}
	f.Add(multiValueSeed(f))

	ins := gobspect.New(gobspect.WithSkipCorruptValues(true), gobspect.WithReadLimit(1<<20))
	cases := renderCases()

	f.Fuzz(func(t *testing.T, data []byte) {
		vals, _ := ins.Stream(bytes.NewReader(data)).Collect()

		for _, v := range vals {
			_ = gobspect.ValueKind(v)

			// Reflexivity: a value must equal, and compare equal to, itself.
			if !gobspect.Equal(v, v) {
				t.Fatalf("Equal(v, v) = false for %s", gobspect.ValueKind(v))
			}
			if c := gobspect.CompareValues(v, v); c != 0 {
				t.Fatalf("CompareValues(v, v) = %d, want 0 for %s", c, gobspect.ValueKind(v))
			}
			if c := gobspect.CompareValuesFold(v, v); c != 0 {
				t.Fatalf("CompareValuesFold(v, v) = %d, want 0 for %s", c, gobspect.ValueKind(v))
			}

			for _, rc := range cases {
				first := gobspect.Format(v, rc.opts...)
				if second := gobspect.Format(v, rc.opts...); first != second {
					t.Fatalf("Format not deterministic under %q:\nfirst:  %q\nsecond: %q", rc.name, first, second)
				}

				// FormatTo must agree with Format for the same options.
				var buf bytes.Buffer
				if err := gobspect.FormatTo(&buf, v, rc.opts...); err != nil {
					t.Fatalf("FormatTo failed under %q: %v", rc.name, err)
				}
				if buf.String() != first {
					t.Fatalf("FormatTo disagrees with Format under %q:\nFormat:   %q\nFormatTo: %q", rc.name, first, buf.String())
				}
			}

			assertValidJSON(t, "ToJSON", func() ([]byte, error) { return gobspect.ToJSON(v) })
			assertValidJSON(t, "ToJSON nonfinite null", func() ([]byte, error) {
				return gobspect.ToJSON(v, gobspect.WithNonFiniteAsNull(true))
			})
			assertValidJSON(t, "ToJSONIndent", func() ([]byte, error) {
				return gobspect.ToJSONIndent(v, "", "  ")
			})
		}

		// Schema rendering over the same stream.
		if schema, err := ins.Stream(bytes.NewReader(data)).Schema(); err == nil && schema != nil {
			_ = schema.String()
			_ = schema.Format()
			_ = schema.Format(gobspect.SchemaWithIndent("\t"))
			schema.FormatTo(io.Discard, gobspect.SchemaWithColor(gobspect.ANSIColorScheme)) //nolint:errcheck
			for _, td := range schema.Types {
				schema.TypeByName(td.Name) //nolint:errcheck
			}
			schema.TypeByName("") //nolint:errcheck
			assertValidJSON(t, "Schema.JSON", schema.JSON)
			assertValidJSON(t, "Schema.JSONIndent", func() ([]byte, error) { return schema.JSONIndent("", "  ") })
		}

		if stats, err := ins.Stream(bytes.NewReader(data)).Stats(); err == nil && stats != nil {
			stats.Format(io.Discard) //nolint:errcheck
			assertValidJSON(t, "Stats.JSON", stats.JSON)
			assertValidJSON(t, "Stats.JSONIndent", func() ([]byte, error) { return stats.JSONIndent("", "  ") })
		}

		// FormatBytes over the raw input, which is arbitrary by construction.
		for _, bf := range []gobspect.BytesFormat{gobspect.BytesHex, gobspect.BytesBase64, gobspect.BytesLiteral} {
			for _, max := range []int{0, 1, 7, len(data)} {
				_ = gobspect.FormatBytes(data, bf, max)
			}
		}
	})
}

// assertValidJSON fails the test if fn succeeds but returns bytes that are not
// valid JSON. An error return is acceptable — silently emitting malformed JSON
// is not.
func assertValidJSON(t *testing.T, label string, fn func() ([]byte, error)) {
	t.Helper()
	out, err := fn()
	if err != nil {
		return
	}
	if !json.Valid(out) {
		t.Fatalf("%s produced invalid JSON: %q", label, out)
	}
}
