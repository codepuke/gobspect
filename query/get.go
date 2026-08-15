package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/codepuke/gobspect"
)

// GetPath resolves p against root and returns the first matching value in
// document order — the same value AllPath would return first. Evaluation is
// lazy and stops at the first match.
// Returns (root, true) for an empty path (unwrapped of any interface wrapper).
// Returns (nil, false) if the path matches nothing.
func GetPath(root gobspect.Value, p Path) (gobspect.Value, bool) {
	for v := range AllPathSeq(root, p) {
		return v, true
	}
	return nil, false
}

// Get resolves expr against root. Panics if expr is syntactically invalid.
// Returns (nil, false) if the path matches nothing.
func Get(root gobspect.Value, expr string) (gobspect.Value, bool) {
	p, err := Parse(expr)
	if err != nil {
		panic(fmt.Sprintf("query.Get: invalid path expression %q: %v", expr, err))
	}
	return GetPath(root, p)
}

// MustGet is like Get but also panics if the path does not resolve.
// Intended for tests and scripts where the data shape is known.
// The panic message includes the full expression and the failing segment.
func MustGet(root gobspect.Value, expr string) gobspect.Value {
	p, err := Parse(expr)
	if err != nil {
		panic(fmt.Sprintf("query.MustGet: invalid path expression %q: %v", expr, err))
	}
	if v, ok := GetPath(root, p); ok {
		return v
	}
	// Re-walk eagerly, purely to identify the segment to blame in the message.
	_, _, failIdx := walkGetPath(root, p.segs)
	failSeg := "?"
	if failIdx >= 0 && failIdx < len(p.segs) {
		failSeg = segString(p.segs[failIdx])
	}
	panic(fmt.Sprintf("query.MustGet: path %q did not resolve at segment %q", expr, failSeg))
}

// walkGetPath resolves segs against root eagerly, committing to the first
// element at each fan-out segment. It is kept only as a diagnostic for
// [MustGet]'s panic message: it returns the matched value, success, and the
// index of the first segment that failed to resolve (-1 if all resolved).
// Path resolution itself goes through [AllPathSeq].
func walkGetPath(root gobspect.Value, segs []segment) (gobspect.Value, bool, int) {
	cur := root
	for i, seg := range segs {
		cur = unwrapInterface(cur)
		if cur == nil {
			return nil, false, i
		}

		switch seg.kind {
		case segField:
			next, ok := stepField(cur, seg.name)
			if !ok {
				return nil, false, i
			}
			cur = next

		case segIndex:
			next, ok := stepIndex(cur, seg.index)
			if !ok {
				return nil, false, i
			}
			cur = next

		case segFilter:
			// A filter narrows the current collection — it does not advance
			// into a child node. For Get we return the first surviving element.
			elems, ok := collectFiltered(cur, seg)
			if !ok || len(elems) == 0 {
				return nil, false, i
			}
			cur = elems[0]

		case segWildcard:
			// Wildcard in Get: collect all elements and take the first.
			elems := collectAll(cur)
			if len(elems) == 0 {
				return nil, false, i
			}
			cur = elems[0]

		case segDescend:
			// Named recursive descent: delegate to allWalk for full traversal,
			// and return the first result.
			results := allWalk(cur, segs[i:])
			if len(results) == 0 {
				return nil, false, i
			}
			return results[0], true, -1

		case segProject:
			projected := buildProjection(cur, seg.projectFields)
			if projected == nil {
				return nil, false, i
			}
			cur = projected

		default:
			return nil, false, i
		}
	}
	return cur, true, -1
}

// segString formats a segment as a human-readable string for error messages.
func segString(s segment) string {
	switch s.kind {
	case segField:
		return s.name
	case segIndex:
		return strconv.Itoa(s.index)
	case segWildcard:
		return "*"
	case segFilter:
		if len(s.orAlts) > 0 {
			return Path{segs: []segment{s}}.String()
		}
		switch s.filterOp {
		case filterOpExist:
			return "[" + s.name + "!]"
		case filterOpContains:
			return "[" + s.name + "~" + s.filterPattern + "]"
		default:
			return "[" + s.name + "=" + s.filterPattern + "]"
		}
	case segDescend:
		return ".." + s.name
	case segProject:
		return strings.Join(s.projectFields, ",")
	default:
		return "?"
	}
}

// unwrapInterface peels off a single InterfaceValue wrapper, returning the
// inner value. Returns v unchanged for any other type.
func unwrapInterface(v gobspect.Value) gobspect.Value {
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		return iv.Value
	}
	return v
}

// keyString extracts the string form of a map key, unwrapping a single
// InterfaceValue wrapper first (interface-keyed maps such as map[any]T
// deliver their keys wrapped). Returns ("", false) for non-string keys.
func keyString(k gobspect.Value) (string, bool) {
	if sv, ok := unwrapInterface(k).(gobspect.StringValue); ok {
		return sv.V, true
	}
	return "", false
}

// stepField navigates a segField segment: looks up name in a StructValue's
// Fields slice or in a MapValue whose keys are StringValues.
func stepField(v gobspect.Value, name string) (gobspect.Value, bool) {
	switch n := v.(type) {
	case gobspect.StructValue:
		for _, f := range n.Fields {
			if f.Name == name {
				return f.Value, true
			}
		}
	case gobspect.MapValue:
		for _, e := range n.Entries {
			if k, ok := keyString(e.Key); ok && k == name {
				return e.Value, true
			}
		}
	}
	return nil, false
}

// stepIndex navigates a segIndex segment: resolves a positive or negative
// integer index into a SliceValue or ArrayValue.
func stepIndex(v gobspect.Value, index int) (gobspect.Value, bool) {
	switch n := v.(type) {
	case gobspect.MapValue:
		// Fallback for numeric-looking keys in map[string]T.
		// Negative indices have no meaningful map fallback.
		if index < 0 {
			return nil, false
		}
		return stepField(n, strconv.Itoa(index))

	case gobspect.SliceValue:
		return stepSliceIndex(n.Elems, index)
	case gobspect.ArrayValue:
		return stepSliceIndex(n.Elems, index)
	default:
		return nil, false
	}
}

func stepSliceIndex(elems []gobspect.Value, index int) (gobspect.Value, bool) {
	length := len(elems)
	i := index
	if i < 0 {
		i = length + i
	}
	if i < 0 || i >= length {
		return nil, false
	}
	return elems[i], true
}

// collectAll returns all element values from a SliceValue, ArrayValue, or
// MapValue (map values only). Returns nil for other node types.
func collectAll(v gobspect.Value) []gobspect.Value {
	switch n := v.(type) {
	case gobspect.SliceValue:
		if len(n.Elems) == 0 {
			return nil
		}
		return n.Elems
	case gobspect.ArrayValue:
		if len(n.Elems) == 0 {
			return nil
		}
		return n.Elems
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

// collectFiltered applies filter seg to v and returns the matching values.
//
// If v is a collection (SliceValue, ArrayValue, MapValue), it iterates over the
// elements and returns those that pass the filter.
//
// If v is not a collection, the filter is applied to v itself as a predicate:
//   - If v passes the filter, returns ([v], true).
//   - If v does not pass the filter, returns (nil, true).
//
// The second return value is always true; the (nil, true) form lets callers
// distinguish "filter applied but nothing passed" from a future error path.
func collectFiltered(v gobspect.Value, seg segment) ([]gobspect.Value, bool) {
	elems := collectAll(v)
	if elems == nil {
		// Not a collection — treat filter as a predicate applied directly to v.
		if matchesFilter(unwrapInterface(v), seg) {
			return []gobspect.Value{v}, true
		}
		return nil, true
	}

	var out []gobspect.Value
	for _, elem := range elems {
		if matchesFilter(unwrapInterface(elem), seg) {
			out = append(out, elem)
		}
	}
	return out, true
}
