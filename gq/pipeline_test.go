package gq_test

import (
	"bytes"
	"encoding/csv"
	"encoding/gob"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/gq"
	"github.com/codepuke/gobspect/query"
	"github.com/codepuke/gobspect/sortval"
	"github.com/codepuke/gobspect/tabular"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type person struct {
	Name  string
	Score int
}

type animal struct {
	Species string
	Legs    int
}

// encodeValues gob-encodes vals into a fresh buffer.
func encodeValues(t *testing.T, vals ...any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range vals {
		require.NoError(t, enc.Encode(v))
	}
	return &buf
}

// streamOf returns a fresh Stream over the encoded vals.
func streamOf(t *testing.T, vals ...any) *gobspect.Stream {
	t.Helper()
	return gobspect.New().Stream(encodeValues(t, vals...))
}

// mustPath parses a query expression.
func mustPath(t *testing.T, expr string) query.Path {
	t.Helper()
	p, err := query.Parse(query.NormalizeQuery(expr))
	require.NoError(t, err)
	return p
}

// collectSink returns a Sink appending .Name strings (or the value's kind)
// into out.
func namesSink(t *testing.T, out *[]string) gq.Sink {
	t.Helper()
	return func(v gobspect.Value) error {
		if sv, ok := gobspect.Unwrap(v).(gobspect.StringValue); ok {
			*out = append(*out, sv.V)
			return nil
		}
		if sv, ok := gobspect.Unwrap(v).(gobspect.StructValue); ok {
			for _, f := range sv.Fields {
				if f.Name == "Name" {
					*out = append(*out, f.Value.(gobspect.StringValue).V)
					return nil
				}
			}
		}
		*out = append(*out, gobspect.ValueKind(v))
		return nil
	}
}

func TestPipelineRun(t *testing.T) {
	people := []any{
		person{Name: "Eve", Score: 5},
		person{Name: "Bob", Score: 2},
		person{Name: "Dave", Score: 4},
		person{Name: "Alice", Score: 1},
		person{Name: "Charlie", Score: 3},
	}

	tests := []struct {
		name        string
		pipeline    gq.Pipeline
		queryExpr   string
		wantNames   []string
		wantMatched bool
	}{
		{
			name:        "identity all values stream order",
			pipeline:    gq.Pipeline{Index: gq.IndexAll},
			wantNames:   []string{"Eve", "Bob", "Dave", "Alice", "Charlie"},
			wantMatched: true,
		},
		{
			name:        "query projects a field",
			pipeline:    gq.Pipeline{Index: gq.IndexAll},
			queryExpr:   ".Name",
			wantNames:   []string{"Eve", "Bob", "Dave", "Alice", "Charlie"},
			wantMatched: true,
		},
		{
			name:        "offset skips",
			pipeline:    gq.Pipeline{Index: gq.IndexAll, Offset: 3},
			wantNames:   []string{"Alice", "Charlie"},
			wantMatched: true,
		},
		{
			name:        "limit stops",
			pipeline:    gq.Pipeline{Index: gq.IndexAll, Limit: 2},
			wantNames:   []string{"Eve", "Bob"},
			wantMatched: true,
		},
		{
			name:        "offset and limit compose",
			pipeline:    gq.Pipeline{Index: gq.IndexAll, Offset: 1, Limit: 2},
			wantNames:   []string{"Bob", "Dave"},
			wantMatched: true,
		},
		{
			name:        "index selects the Nth top-level value",
			pipeline:    gq.Pipeline{Index: 2},
			wantNames:   []string{"Dave"},
			wantMatched: true,
		},
		{
			name:        "index zero is the first value, not all",
			pipeline:    gq.Pipeline{},
			wantNames:   []string{"Eve"},
			wantMatched: true,
		},
		{
			name:        "index past the end matches nothing",
			pipeline:    gq.Pipeline{Index: 99},
			wantNames:   nil,
			wantMatched: false,
		},
		{
			name: "sort orders before offset and limit",
			pipeline: gq.Pipeline{
				Index:  gq.IndexAll,
				Offset: 1,
				Limit:  2,
				Sort:   sortSpec(t, "Name", false),
			},
			// Sorted: Alice, Bob, Charlie, Dave, Eve → offset 1, limit 2.
			wantNames:   []string{"Bob", "Charlie"},
			wantMatched: true,
		},
		{
			name: "sort descending",
			pipeline: gq.Pipeline{
				Index: gq.IndexAll,
				Sort:  sortSpec(t, "Score", true),
			},
			wantNames:   []string{"Eve", "Dave", "Charlie", "Bob", "Alice"},
			wantMatched: true,
		},
		{
			name: "sort with index restricts first",
			pipeline: gq.Pipeline{
				Index: 1,
				Sort:  sortSpec(t, "Name", false),
			},
			wantNames:   []string{"Bob"},
			wantMatched: true,
		},
		{
			name:        "matched true even when offset consumes everything",
			pipeline:    gq.Pipeline{Index: gq.IndexAll, Offset: 99},
			wantNames:   nil,
			wantMatched: true,
		},
		{
			name:        "no match on missing path",
			pipeline:    gq.Pipeline{Index: gq.IndexAll},
			queryExpr:   ".Nope",
			wantNames:   nil,
			wantMatched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.pipeline
			if tt.queryExpr != "" {
				p.Path = mustPath(t, tt.queryExpr)
			}
			var got []string
			matched, err := p.Run(streamOf(t, people...), namesSink(t, &got))
			require.NoError(t, err)
			assert.Equal(t, tt.wantMatched, matched)
			assert.Equal(t, tt.wantNames, got)
		})
	}
}

func sortSpec(t *testing.T, keys string, desc bool) sortval.SortSpec {
	t.Helper()
	spec, err := sortval.ParseSortSpec(keys, desc, false, false)
	require.NoError(t, err)
	return spec
}

// TestPipelineRunStreamsWithoutSort proves the unsorted branch delivers
// results incrementally: values decoded before a mid-stream corruption reach
// the sink even though Run ultimately returns the decode error.
func TestPipelineRunStreamsWithoutSort(t *testing.T) {
	buf := encodeValues(t,
		person{Name: "Alice", Score: 1},
		person{Name: "Bob", Score: 2},
	)
	// Truncate inside the last message so the second value fails to decode.
	raw := buf.Bytes()
	truncated := raw[:len(raw)-3]

	stream := gobspect.New().Stream(bytes.NewReader(truncated))
	var got []string
	matched, err := gq.Pipeline{Index: gq.IndexAll}.Run(stream, namesSink(t, &got))

	require.Error(t, err)
	var sinkErr *gq.SinkError
	assert.False(t, errors.As(err, &sinkErr), "decode errors must not be wrapped in SinkError")
	assert.True(t, matched)
	assert.Equal(t, []string{"Alice"}, got, "first value must reach the sink before the error")
}

func TestPipelineRunSinkError(t *testing.T) {
	sentinel := errors.New("downstream closed")
	stream := streamOf(t, person{Name: "Alice", Score: 1})

	matched, err := gq.Pipeline{Index: gq.IndexAll}.Run(stream, func(gobspect.Value) error {
		return sentinel
	})

	require.Error(t, err)
	var sinkErr *gq.SinkError
	require.True(t, errors.As(err, &sinkErr), "sink errors must wrap in *SinkError")
	assert.True(t, errors.Is(err, sentinel), "errors.Is must reach the sink's error")
	assert.Equal(t, sentinel.Error(), err.Error(), "SinkError must not alter the message")
	assert.True(t, matched)
}

func TestPipelineRunSinkErrorSorted(t *testing.T) {
	sentinel := errors.New("boom")
	stream := streamOf(t, person{Name: "B"}, person{Name: "A"})

	p := gq.Pipeline{Index: gq.IndexAll, Sort: sortSpec(t, "Name", false)}
	_, err := p.Run(stream, func(gobspect.Value) error { return sentinel })

	var sinkErr *gq.SinkError
	require.True(t, errors.As(err, &sinkErr))
	assert.True(t, errors.Is(err, sentinel))
}

func TestPipelineRunDecodeErrorPassthrough(t *testing.T) {
	stream := gobspect.New().Stream(strings.NewReader("\x07this is not gob"))
	matched, err := gq.Pipeline{Index: gq.IndexAll}.Run(stream, func(gobspect.Value) error {
		t.Fatal("sink must not be called")
		return nil
	})
	require.Error(t, err)
	assert.False(t, matched)
}

func TestPipelineRunRender(t *testing.T) {
	var out bytes.Buffer
	stream := streamOf(t, person{Name: "Alice", Score: 1})

	matched, err := gq.Pipeline{Index: gq.IndexAll}.RunRender(stream, &out, gq.RenderOptions{Format: gq.FormatJSONL})
	require.NoError(t, err)
	assert.True(t, matched)
	// One compact structural-JSON object per line, exactly as gq -format jsonl.
	line := strings.TrimSuffix(out.String(), "\n")
	assert.NotContains(t, line, "\n", "jsonl output must be a single line")
	assert.Contains(t, line, `"typeName":"person"`)
	assert.Contains(t, line, `"v":"Alice"`)
}

// TestPipelineRunTabularPartitionSort is the regression guard for the
// partition-sort divergence: with hetero=partition and a sort spec, rows must
// sort within each struct-type partition (in first-arrival order), not
// globally across partitions.
func TestPipelineRunTabularPartitionSort(t *testing.T) {
	vals := []any{
		person{Name: "Zed", Score: 3},
		animal{Species: "ant", Legs: 6},
		person{Name: "Amy", Score: 1},
		animal{Species: "zebra", Legs: 4},
		person{Name: "Mel", Score: 2},
	}

	stream := streamOf(t, vals...)
	var out bytes.Buffer
	tp := tabular.NewPrinter(&out,
		tabular.WithDelimiter(','),
		tabular.WithStream(stream),
		tabular.WithHeterogeneousMode(tabular.HeterogeneousPartition),
	)

	p := gq.Pipeline{Index: gq.IndexAll, Sort: sortSpec(t, "Name,Species", false)}
	matched, err := p.RunTabular(stream, tp)
	require.NoError(t, err)
	require.NoError(t, tp.Flush())
	assert.True(t, matched)

	// The person partition arrives first and must be sorted internally
	// (Amy, Mel, Zed), followed by the animal partition sorted internally
	// (ant, zebra). A global sort would interleave the two types.
	output := out.String()
	order := []string{"Amy", "Mel", "Zed", "ant", "zebra"}
	last := -1
	for _, name := range order {
		pos := strings.Index(output, name)
		require.NotEqual(t, -1, pos, "missing %q in output:\n%s", name, output)
		assert.Greater(t, pos, last, "%q out of order in output:\n%s", name, output)
		last = pos
	}
}

// TestPipelineRunTabularGlobalSort verifies non-partition modes keep the
// plain global sort.
func TestPipelineRunTabularGlobalSort(t *testing.T) {
	vals := []any{
		person{Name: "Zed", Score: 3},
		person{Name: "Amy", Score: 1},
		person{Name: "Mel", Score: 2},
	}
	stream := streamOf(t, vals...)
	var out bytes.Buffer
	tp := tabular.NewPrinter(&out, tabular.WithStream(stream))

	p := gq.Pipeline{Index: gq.IndexAll, Sort: sortSpec(t, "Name", false)}
	_, err := p.RunTabular(stream, tp)
	require.NoError(t, err)
	require.NoError(t, tp.Flush())

	r := csv.NewReader(strings.NewReader(out.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 4, "header + 3 rows")
	assert.Equal(t, "Amy", rows[1][0])
	assert.Equal(t, "Mel", rows[2][0])
	assert.Equal(t, "Zed", rows[3][0])
}

// TestPipelineRunTabularLimitOffset mirrors gq's tabular offset/limit
// behavior through the shared pipeline.
func TestPipelineRunTabularLimitOffset(t *testing.T) {
	vals := []any{
		person{Name: "A", Score: 1},
		person{Name: "B", Score: 2},
		person{Name: "C", Score: 3},
		person{Name: "D", Score: 4},
	}
	stream := streamOf(t, vals...)
	var out bytes.Buffer
	tp := tabular.NewPrinter(&out, tabular.WithStream(stream))

	p := gq.Pipeline{Index: gq.IndexAll, Offset: 1, Limit: 2}
	_, err := p.RunTabular(stream, tp)
	require.NoError(t, err)
	require.NoError(t, tp.Flush())

	r := csv.NewReader(strings.NewReader(out.String()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + 2 rows")
	assert.Equal(t, "B", rows[1][0])
	assert.Equal(t, "C", rows[2][0])
}

// TestPipelineRunEmptyStream verifies EOF-at-start yields no results, no
// match, and no error.
func TestPipelineRunEmptyStream(t *testing.T) {
	stream := gobspect.New().Stream(io.LimitReader(strings.NewReader(""), 0))
	matched, err := gq.Pipeline{Index: gq.IndexAll}.Run(stream, func(gobspect.Value) error {
		t.Fatal("sink must not be called")
		return nil
	})
	require.NoError(t, err)
	assert.False(t, matched)
}
