package query

import (
	"fmt"
	"iter"
	"slices"

	"github.com/codepuke/gobspect"
)

// AllPathSeq returns an iterator over all values matched by p against root,
// expanding wildcard segments lazily. The caller may break early; the iterator
// will stop producing values as soon as yield returns false.
func AllPathSeq(root gobspect.Value, p Path) iter.Seq[gobspect.Value] {
	return func(yield func(gobspect.Value) bool) {
		allWalkSeq(root, p.segs, yield)
	}
}

// AllSeq returns an iterator over all values matched by expr against root,
// expanding * segments lazily. Panics if expr is syntactically invalid
// (matching All's behaviour).
func AllSeq(root gobspect.Value, expr string) iter.Seq[gobspect.Value] {
	p, err := Parse(expr)
	if err != nil {
		panic(fmt.Sprintf("query.AllSeq: invalid path expression %q: %v", expr, err))
	}
	return AllPathSeq(root, p)
}

// AllPath returns all values matched by p against root, expanding wildcard
// segments. Returns nil (not an empty slice) when nothing matches.
func AllPath(root gobspect.Value, p Path) []gobspect.Value {
	results := slices.Collect(AllPathSeq(root, p))
	if len(results) == 0 {
		return nil
	}
	return results
}

// All returns all values matched by expr against root, expanding * segments.
// Panics if expr is syntactically invalid. Returns nil if nothing matches.
func All(root gobspect.Value, expr string) []gobspect.Value {
	p, err := Parse(expr)
	if err != nil {
		panic(fmt.Sprintf("query.All: invalid path expression %q: %v", expr, err))
	}
	return AllPath(root, p)
}

// allWalkSeq is the lazy counterpart to allWalk. It calls yield for each
// matching value and returns false as soon as yield returns false (early
// termination). Returns false if the caller requested an early stop.
func allWalkSeq(cur gobspect.Value, segs []segment, yield func(gobspect.Value) bool) bool {
	cur = unwrapInterface(cur)
	if cur == nil {
		return true
	}

	if len(segs) == 0 {
		return yield(cur)
	}

	seg := segs[0]
	rest := segs[1:]

	switch seg.kind {
	case segField:
		next, ok := stepField(cur, seg.name)
		if !ok {
			return true
		}
		return allWalkSeq(next, rest, yield)

	case segIndex:
		next, ok := stepIndex(cur, seg.index)
		if !ok {
			return true
		}
		return allWalkSeq(next, rest, yield)

	case segWildcard:
		elems := collectAll(cur)
		for _, elem := range elems {
			if !allWalkSeq(elem, rest, yield) {
				return false
			}
		}
		return true

	case segFilter:
		elems, ok := collectFiltered(cur, seg)
		if !ok {
			return true
		}
		for _, elem := range elems {
			if !allWalkSeq(elem, rest, yield) {
				return false
			}
		}
		return true

	case segDescend:
		if seg.name == "" {
			// Wildcard descent: test cur against the leading filter(s) in rest
			// (pre-order), then recurse into each direct child with the full
			// descent+rest prepended.
			if !applyFiltersToNodeSeq(cur, rest, yield) {
				return false
			}
			for _, child := range descendAll(cur) {
				if !allWalkSeq(child, segs, yield) {
					return false
				}
			}
			return true
		}
		// Named recursive descent: pre-order depth-first traversal.
		if next, ok := stepField(cur, seg.name); ok {
			if !allWalkSeq(next, rest, yield) {
				return false
			}
		}
		for _, child := range descendAll(cur) {
			if !allWalkSeq(child, segs, yield) {
				return false
			}
		}
		return true

	case segProject:
		projected := buildProjection(cur, seg.projectFields)
		if projected == nil {
			return true
		}
		return allWalkSeq(projected, rest, yield)

	default:
		return true
	}
}

// applyFiltersToNodeSeq is the lazy counterpart to applyFiltersToNode.
// Returns false if yield returned false (early stop).
func applyFiltersToNodeSeq(v gobspect.Value, segs []segment, yield func(gobspect.Value) bool) bool {
	if len(segs) == 0 || segs[0].kind != segFilter {
		return allWalkSeq(v, segs, yield)
	}
	i := 0
	for i < len(segs) && segs[i].kind == segFilter {
		if !matchesFilter(unwrapInterface(v), segs[i]) {
			return true
		}
		i++
	}
	return allWalkSeq(v, segs[i:], yield)
}

// allWalk recursively evaluates segs against cur, collecting all matching
// values. Wildcards and filters fan out; other segments narrow to one branch.
func allWalk(cur gobspect.Value, segs []segment) []gobspect.Value {
	cur = unwrapInterface(cur)
	if cur == nil {
		return nil
	}

	if len(segs) == 0 {
		return []gobspect.Value{cur}
	}

	seg := segs[0]
	rest := segs[1:]

	switch seg.kind {
	case segField:
		next, ok := stepField(cur, seg.name)
		if !ok {
			return nil
		}
		return allWalk(next, rest)

	case segIndex:
		next, ok := stepIndex(cur, seg.index)
		if !ok {
			return nil
		}
		return allWalk(next, rest)

	case segWildcard:
		elems := collectAll(cur)
		if len(elems) == 0 {
			return nil
		}
		var out []gobspect.Value
		for _, elem := range elems {
			out = append(out, allWalk(elem, rest)...)
		}
		return out

	case segFilter:
		elems, ok := collectFiltered(cur, seg)
		if !ok || len(elems) == 0 {
			return nil
		}
		var out []gobspect.Value
		for _, elem := range elems {
			out = append(out, allWalk(elem, rest)...)
		}
		return out

	case segDescend:
		if seg.name == "" {
			// Wildcard descent: test cur against the leading filter(s) in rest
			// (pre-order), then recurse into each direct child with the full
			// descent+rest prepended.
			var out []gobspect.Value
			out = append(out, applyFiltersToNode(cur, rest)...)
			for _, child := range descendAll(cur) {
				out = append(out, allWalk(child, segs)...)
			}
			return out
		}
		// Named recursive descent: pre-order depth-first traversal.
		// 1. If the current node has a field with seg.name, collect allWalk on that.
		// 2. Then recurse into each direct child with the full descent segment.
		var out []gobspect.Value
		if next, ok := stepField(cur, seg.name); ok {
			out = append(out, allWalk(next, rest)...)
		}
		for _, child := range descendAll(cur) {
			out = append(out, allWalk(child, segs)...)
		}
		return out

	case segProject:
		projected := buildProjection(cur, seg.projectFields)
		if projected == nil {
			return nil
		}
		return allWalk(projected, rest)

	default:
		return nil
	}
}

// applyFiltersToNode tests v directly against the leading segFilter segments in
// segs.  This is used by wildcard descent (..[Filter…]) where each visited node
// must be tested as a candidate, not treated as a collection to iterate over.
//
// Behaviour:
//   - Consume all leading segFilter segments.
//   - If v passes every one of them, call allWalk(v, remaining) and return the
//     results.
//   - If v fails any filter, return nil.
//   - If segs is empty or its first segment is not a segFilter, fall back to
//     allWalk(v, segs) so that non-filter tails work normally.
func applyFiltersToNode(v gobspect.Value, segs []segment) []gobspect.Value {
	if len(segs) == 0 || segs[0].kind != segFilter {
		return allWalk(v, segs)
	}
	// Walk through consecutive leading filter segments.
	i := 0
	for i < len(segs) && segs[i].kind == segFilter {
		if !matchesFilter(unwrapInterface(v), segs[i]) {
			return nil
		}
		i++
	}
	// v passed all leading filters; continue with the remaining segments.
	return allWalk(v, segs[i:])
}

// descendAll returns the direct child values of v one level deep, used by
// segDescend to enumerate nodes to recurse into.
//
// StructValue → field values; SliceValue/ArrayValue → elements;
// MapValue → entry values. InterfaceValue is unwrapped before dispatch.
// Returns nil for scalars, OpaqueValue, NilValue, and nil input.
func descendAll(v gobspect.Value) []gobspect.Value {
	v = unwrapInterface(v)
	if v == nil {
		return nil
	}
	switch n := v.(type) {
	case gobspect.StructValue:
		if len(n.Fields) == 0 {
			return nil
		}
		out := make([]gobspect.Value, len(n.Fields))
		for i, f := range n.Fields {
			out[i] = f.Value
		}
		return out
	case gobspect.SliceValue:
		if len(n.Elems) == 0 {
			return nil
		}
		out := make([]gobspect.Value, len(n.Elems))
		copy(out, n.Elems)
		return out
	case gobspect.ArrayValue:
		if len(n.Elems) == 0 {
			return nil
		}
		out := make([]gobspect.Value, len(n.Elems))
		copy(out, n.Elems)
		return out
	case gobspect.MapValue:
		if len(n.Entries) == 0 {
			return nil
		}
		out := make([]gobspect.Value, len(n.Entries))
		for i, e := range n.Entries {
			out[i] = e.Value
		}
		return out
	}
	return nil
}
