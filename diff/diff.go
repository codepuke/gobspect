// Package diff computes structural differences between two gobspect.Value
// trees or two streams of gobspect.Values.
//
// Diff produces a Delta AST that mirrors the structure of the inputs: struct
// field-by-field deltas, map entry-by-key deltas, slice/array element-by-
// position deltas. Equality for leaves uses [gobspect.Equal] (strict, no
// cross-kind numeric coercion).
//
// DiffStreams pairs two slices of top-level Values by index and runs Diff on
// each pair, yielding a per-position StreamDelta suitable for regression
// detection and archival comparison.
package diff

import (
	"github.com/codepuke/gobspect"
)

// Delta describes the difference between two Values. Concrete types:
//
//   - [Added]: the Value is present only in the "after" side.
//   - [Removed]: the Value is present only in the "before" side.
//   - [Changed]: leaf value differs between the two sides.
//   - [StructDelta]: a struct with per-field deltas.
//   - [MapDelta]: a map with per-key deltas.
//   - [SliceDelta]: a slice with per-position deltas.
//   - [ArrayDelta]: an array with per-position deltas.
//
// When the two sides are equal under [gobspect.Equal], Diff returns nil.
type Delta interface {
	delta()
}

// Added indicates the value exists only in the "after" side.
type Added struct{ Value gobspect.Value }

// Removed indicates the value exists only in the "before" side.
type Removed struct{ Value gobspect.Value }

// Changed indicates two scalar or otherwise non-walkable values differ.
type Changed struct{ Before, After gobspect.Value }

// StructDelta holds the per-field changes for two structs whose TypeName
// matches. Only changed fields appear in Fields (deterministically ordered
// by their appearance in "before"; new fields in "after" follow).
type StructDelta struct {
	TypeName string
	Fields   []FieldDelta
}

// FieldDelta describes the change for a single struct field.
type FieldDelta struct {
	Name  string
	Delta Delta
}

// MapDelta holds the per-key changes between two maps.
type MapDelta struct {
	Entries []MapEntryDelta
}

// MapEntryDelta describes the change for a single map entry.
type MapEntryDelta struct {
	Key   gobspect.Value
	Delta Delta
}

// SliceDelta holds position-aligned deltas. Elements present in only one side
// appear as Added or Removed at the corresponding index.
type SliceDelta struct {
	Elems []ElemDelta
}

// ArrayDelta mirrors SliceDelta for gobspect.ArrayValue.
type ArrayDelta struct {
	Elems []ElemDelta
}

// ElemDelta pairs an index with the delta at that position.
type ElemDelta struct {
	Index int
	Delta Delta
}

func (Added) delta()       {}
func (Removed) delta()     {}
func (Changed) delta()     {}
func (StructDelta) delta() {}
func (MapDelta) delta()    {}
func (SliceDelta) delta()  {}
func (ArrayDelta) delta()  {}

// Diff computes the structural difference between a and b. Returns nil when
// the two sides are equal. InterfaceValue wrappers are peeled transparently.
//
// Kind mismatches (e.g. IntValue vs StringValue) surface as Changed even when
// the two sides would compare equal under permissive numeric coercion — this
// is a structural diff, not a value-equivalence check.
func Diff(a, b gobspect.Value) Delta {
	a = unwrap(a)
	b = unwrap(b)

	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		return Added{Value: b}
	}
	if b == nil {
		return Removed{Value: a}
	}
	if gobspect.Equal(a, b) {
		return nil
	}

	// Both present and not structurally equal.
	switch av := a.(type) {
	case gobspect.StructValue:
		bv, ok := b.(gobspect.StructValue)
		if !ok || av.TypeName != bv.TypeName {
			return Changed{Before: a, After: b}
		}
		return diffStruct(av, bv)

	case gobspect.MapValue:
		bv, ok := b.(gobspect.MapValue)
		if !ok {
			return Changed{Before: a, After: b}
		}
		return diffMap(av, bv)

	case gobspect.SliceValue:
		bv, ok := b.(gobspect.SliceValue)
		if !ok {
			return Changed{Before: a, After: b}
		}
		return SliceDelta{Elems: diffElems(av.Elems, bv.Elems)}

	case gobspect.ArrayValue:
		bv, ok := b.(gobspect.ArrayValue)
		if !ok {
			return Changed{Before: a, After: b}
		}
		return ArrayDelta{Elems: diffElems(av.Elems, bv.Elems)}
	}
	return Changed{Before: a, After: b}
}

func unwrap(v gobspect.Value) gobspect.Value {
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		return iv.Value
	}
	return v
}

func diffStruct(a, b gobspect.StructValue) Delta {
	var out StructDelta
	out.TypeName = a.TypeName

	// Index b fields by name for O(1) lookup; preserve a's order for output.
	bFields := make(map[string]gobspect.Value, len(b.Fields))
	for _, f := range b.Fields {
		bFields[f.Name] = f.Value
	}
	seenInA := make(map[string]struct{}, len(a.Fields))

	for _, af := range a.Fields {
		seenInA[af.Name] = struct{}{}
		if bv, ok := bFields[af.Name]; ok {
			if d := Diff(af.Value, bv); d != nil {
				out.Fields = append(out.Fields, FieldDelta{Name: af.Name, Delta: d})
			}
		} else {
			out.Fields = append(out.Fields, FieldDelta{Name: af.Name, Delta: Removed{Value: af.Value}})
		}
	}
	for _, bf := range b.Fields {
		if _, ok := seenInA[bf.Name]; !ok {
			out.Fields = append(out.Fields, FieldDelta{Name: bf.Name, Delta: Added{Value: bf.Value}})
		}
	}
	if len(out.Fields) == 0 {
		return nil
	}
	return out
}

func diffMap(a, b gobspect.MapValue) Delta {
	var out MapDelta
	// Build a keyed lookup for b.
	type bIndex struct {
		val gobspect.Value
		key gobspect.Value
	}
	bByFormat := make(map[string]bIndex, len(b.Entries))
	for _, e := range b.Entries {
		bByFormat[gobspect.Format(e.Key)] = bIndex{val: e.Value, key: e.Key}
	}
	seen := make(map[string]struct{}, len(a.Entries))

	for _, ae := range a.Entries {
		k := gobspect.Format(ae.Key)
		seen[k] = struct{}{}
		if bi, ok := bByFormat[k]; ok {
			if d := Diff(ae.Value, bi.val); d != nil {
				out.Entries = append(out.Entries, MapEntryDelta{Key: ae.Key, Delta: d})
			}
		} else {
			out.Entries = append(out.Entries, MapEntryDelta{Key: ae.Key, Delta: Removed{Value: ae.Value}})
		}
	}
	for _, be := range b.Entries {
		if _, ok := seen[gobspect.Format(be.Key)]; !ok {
			out.Entries = append(out.Entries, MapEntryDelta{Key: be.Key, Delta: Added{Value: be.Value}})
		}
	}
	if len(out.Entries) == 0 {
		return nil
	}
	return out
}

func diffElems(a, b []gobspect.Value) []ElemDelta {
	var out []ElemDelta
	n := max(len(a), len(b))
	for i := range n {
		var av, bv gobspect.Value
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av == nil && bv == nil {
			continue
		}
		if av == nil {
			out = append(out, ElemDelta{Index: i, Delta: Added{Value: bv}})
			continue
		}
		if bv == nil {
			out = append(out, ElemDelta{Index: i, Delta: Removed{Value: av}})
			continue
		}
		if d := Diff(av, bv); d != nil {
			out = append(out, ElemDelta{Index: i, Delta: d})
		}
	}
	return out
}

// StreamDelta is the result of DiffStreams: one entry per aligned position.
// If len(a) != len(b), the shorter side contributes Added/Removed at the
// trailing positions.
type StreamDelta struct {
	Entries []ElemDelta
}

// DiffStreams pairs the two slices by index and returns a StreamDelta. Only
// positions that actually differ are included; an unchanged pair contributes
// nothing.
func DiffStreams(a, b []gobspect.Value) StreamDelta {
	return StreamDelta{Entries: diffElems(a, b)}
}

// HasChanges reports whether a Delta represents any actual change. Nil delta
// means equal; non-nil always means some difference.
func HasChanges(d Delta) bool { return d != nil }

// StreamHasChanges reports whether any top-level pair differs.
func StreamHasChanges(sd StreamDelta) bool { return len(sd.Entries) > 0 }
