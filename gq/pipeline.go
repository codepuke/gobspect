package gq

import (
	"io"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"github.com/codepuke/gobspect/sortval"
	"github.com/codepuke/gobspect/tabular"
)

// Sink consumes one matched value. Returning an error stops the pipeline; the
// error is surfaced to the caller wrapped in [SinkError].
type Sink func(gobspect.Value) error

// Pipeline drives a [gobspect.Stream] through the gq result-iteration steps:
// query matching, top-level value indexing, sorting, offset, and limit, in
// that order. The zero value matches every value in stream order with no
// offset or limit.
//
// A Pipeline consumes its Stream via [gobspect.Stream.Values]; like the
// stream itself it is single-use per stream.
type Pipeline struct {
	// Path selects matches within each top-level value. The zero Path is the
	// identity: it matches the whole value.
	Path query.Path

	// Index restricts matching to the Nth top-level value (0-based).
	// Negative means all values. Note that the zero value of this field
	// selects only the first top-level value; use -1 (or [IndexAll]) for the
	// common match-everything case.
	Index int

	// Offset skips the first N results after sorting.
	Offset int

	// Limit stops after N results have been delivered past the offset.
	// Zero means no limit.
	Limit int

	// Sort orders results before offset and limit apply. The zero SortSpec
	// keeps stream order and lets results stream to the sink incrementally;
	// a non-empty spec buffers all results before delivery.
	Sort sortval.SortSpec
}

// IndexAll is the [Pipeline.Index] value that matches every top-level value.
const IndexAll = -1

// SinkError wraps an error returned by a [Sink], distinguishing output
// failures from stream decode failures. Error reports the underlying error's
// text unchanged, and Unwrap exposes it to [errors.Is] and [errors.As] — so
// e.g. a downstream pipe closure still matches errors.Is(err, syscall.EPIPE).
type SinkError struct{ Err error }

func (e *SinkError) Error() string { return e.Err.Error() }

// Unwrap returns the sink's error.
func (e *SinkError) Unwrap() error { return e.Err }

// Run drives s through the pipeline, delivering each result to sink.
//
// matched reports whether the query matched at least once, regardless of
// offset and limit; frontends use it to distinguish "no results" from "path
// not found". Decode errors from the stream are returned as-is; sink errors
// are returned wrapped in [SinkError]. Results already delivered before an
// error stand.
func (p Pipeline) Run(s *gobspect.Stream, sink Sink) (matched bool, err error) {
	return p.run(s, sink, false)
}

// RunRender is [Pipeline.Run] with a sink that renders each result to w
// using [Render] with the given options.
func (p Pipeline) RunRender(s *gobspect.Stream, w io.Writer, o RenderOptions) (matched bool, err error) {
	return p.run(s, func(v gobspect.Value) error { return Render(w, v, o) }, false)
}

// RunTabular is [Pipeline.Run] with tp.WriteValue as the sink. When tp is in
// [tabular.HeterogeneousPartition] mode and a sort is set, results are sorted
// within each struct-type partition rather than globally, so row order agrees
// with the partitioned tables tp emits. RunTabular does not call tp.Flush;
// that remains the caller's responsibility (some heterogeneous modes buffer
// rows until Flush).
func (p Pipeline) RunTabular(s *gobspect.Stream, tp *tabular.Printer) (matched bool, err error) {
	partition := tp.HeterogeneousMode() == tabular.HeterogeneousPartition
	return p.run(s, tp.WriteValue, partition)
}

func (p Pipeline) run(s *gobspect.Stream, sink Sink, partitionSort bool) (matched bool, err error) {
	idx := 0     // current top-level value index
	resultN := 0 // results counted toward offset/limit

	if len(p.Sort.Keys) > 0 {
		// Sorting requires the full result set: buffer, sort, then deliver.
		var allResults []gobspect.Value
		for v, err := range s.Values() {
			if err != nil {
				return matched, err
			}
			if p.Index >= 0 && idx != p.Index {
				idx++
				continue
			}
			for result := range query.AllPathSeq(v, p.Path) {
				matched = true
				allResults = append(allResults, result)
			}
			idx++
			if p.Index >= 0 && idx > p.Index {
				break
			}
		}

		var sorted []gobspect.Value
		if partitionSort {
			sorted = sortPerPartition(allResults, p.Sort)
		} else {
			sorted = sortval.SortMatches(sortval.SeqOf(allResults), p.Sort)
		}

		for pos, result := range sorted {
			if pos < p.Offset {
				continue
			}
			if err := sink(result); err != nil {
				return matched, &SinkError{Err: err}
			}
			resultN++
			if p.Limit > 0 && resultN >= p.Limit {
				break
			}
		}
		return matched, nil
	}

	// No sort: stream results to the sink as they decode.
	for v, err := range s.Values() {
		if err != nil {
			return matched, err
		}
		if p.Index >= 0 && idx != p.Index {
			idx++
			continue
		}
		for result := range query.AllPathSeq(v, p.Path) {
			matched = true
			pos := resultN
			resultN++
			if pos < p.Offset {
				continue
			}
			if err := sink(result); err != nil {
				return matched, &SinkError{Err: err}
			}
			if p.Limit > 0 && resultN-p.Offset >= p.Limit {
				return matched, nil
			}
		}
		idx++
		if p.Index >= 0 && idx > p.Index {
			break
		}
	}
	return matched, nil
}

// sortPerPartition buckets results by struct type (GobTypeID) in their first-
// arrival order, sorts within each bucket, and returns the concatenation.
// Values with GobTypeID == 0 (scalars, projections) form a single leading
// bucket. This matches the tabular partition printer's notion of a
// "partition", so sorted rows land inside the right table.
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

// partitionID returns the struct GobTypeID of v, unwrapping any interface
// layers. Non-struct values and structs with no type ID return 0.
func partitionID(v gobspect.Value) int {
	if sv, ok := gobspect.Unwrap(v).(gobspect.StructValue); ok {
		return sv.GobTypeID
	}
	return 0
}
