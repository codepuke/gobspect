// Package sortval provides sorting utilities for sequences of gobspect.Value nodes.
package sortval

import (
	"fmt"
	"iter"
	"sort"
	"strings"

	"github.com/codepuke/gobspect"
)

// SortKey names one field to sort by, with its own direction.
type SortKey struct {
	Field string
	Desc  bool
}

// SortSpec describes how to sort a sequence of [gobspect.Value] nodes by struct
// field keys. Each key carries its own direction; the optional Fold and
// DropMissing flags apply to the spec as a whole.
type SortSpec struct {
	Keys        []SortKey
	Fold        bool // case-insensitive string comparison
	DropMissing bool // exclude rows missing ALL sort keys when true
}

// ParseSortSpec parses a comma-separated list of field names into a SortSpec.
// Each entry may optionally include a direction suffix of ":asc" or ":desc"
// (case-insensitive). Entries without a suffix inherit defaultDesc.
//
// Examples:
//
//	ParseSortSpec("Name", false, …)              → ascending on Name
//	ParseSortSpec("Name,Score:desc", false, …)   → Name asc, Score desc
//	ParseSortSpec("Name,Score", true, …)         → Name desc, Score desc
//	ParseSortSpec("Name:asc,Score:desc", …, …)   → Name asc, Score desc
//
// Returns an error if keysFlag is empty or contains empty/invalid field names.
func ParseSortSpec(keysFlag string, defaultDesc, fold, dropMissing bool) (SortSpec, error) {
	if keysFlag == "" {
		return SortSpec{}, fmt.Errorf("sort keys must not be empty")
	}
	parts := strings.Split(keysFlag, ",")
	keys := make([]SortKey, 0, len(parts))
	for _, p := range parts {
		raw := strings.TrimSpace(p)
		if raw == "" {
			return SortSpec{}, fmt.Errorf("sort keys must not contain empty field names")
		}
		field := raw
		desc := defaultDesc
		if colon := strings.LastIndex(raw, ":"); colon >= 0 {
			suffix := strings.ToLower(strings.TrimSpace(raw[colon+1:]))
			switch suffix {
			case "asc":
				desc = false
				field = strings.TrimSpace(raw[:colon])
			case "desc":
				desc = true
				field = strings.TrimSpace(raw[:colon])
			default:
				return SortSpec{}, fmt.Errorf("sort key %q: unknown direction %q (want asc or desc)", raw, raw[colon+1:])
			}
			if field == "" {
				return SortSpec{}, fmt.Errorf("sort key %q: field name before direction is empty", raw)
			}
		}
		keys = append(keys, SortKey{Field: field, Desc: desc})
	}
	return SortSpec{
		Keys:        keys,
		Fold:        fold,
		DropMissing: dropMissing,
	}, nil
}

// Compare compares two [gobspect.Value] nodes by the spec's keys.
// Returns -1, 0, or +1. Each key's direction is applied independently.
// Missing fields produce NilValue{} for comparison (regardless of DropMissing).
func (s SortSpec) Compare(a, b gobspect.Value) int {
	cmpFn := gobspect.CompareValues
	if s.Fold {
		cmpFn = gobspect.CompareValuesFold
	}
	for _, key := range s.Keys {
		av, _ := extractSortKey(a, key.Field)
		bv, _ := extractSortKey(b, key.Field)
		r := cmpFn(av, bv)
		if r != 0 {
			if key.Desc {
				return -r
			}
			return r
		}
	}
	return 0
}

// SortMatches drains matches into a slice, optionally filters rows missing all
// sort keys (when spec.DropMissing is true), then sorts stably by spec.Compare.
func SortMatches(matches iter.Seq[gobspect.Value], spec SortSpec) []gobspect.Value {
	var buf []gobspect.Value
	for v := range matches {
		buf = append(buf, v)
	}

	if spec.DropMissing {
		kept := buf[:0]
		for _, row := range buf {
			for _, key := range spec.Keys {
				if _, ok := extractSortKey(row, key.Field); ok {
					kept = append(kept, row)
					break
				}
			}
		}
		buf = kept
	}

	sort.SliceStable(buf, func(i, j int) bool {
		return spec.Compare(buf[i], buf[j]) < 0
	})

	return buf
}

// SeqOf converts a []gobspect.Value slice into an iter.Seq[gobspect.Value].
func SeqOf(vals []gobspect.Value) iter.Seq[gobspect.Value] {
	return func(yield func(gobspect.Value) bool) {
		for _, v := range vals {
			if !yield(v) {
				return
			}
		}
	}
}

// extractSortKey returns the value of the named field from v.
// v is unwrapped if it is an InterfaceValue.
// Returns (NilValue{}, false) for non-structs or missing fields.
func extractSortKey(v gobspect.Value, field string) (gobspect.Value, bool) {
	if iv, ok := v.(gobspect.InterfaceValue); ok {
		v = iv.Value
	}
	sv, ok := v.(gobspect.StructValue)
	if !ok {
		return gobspect.NilValue{}, false
	}
	for _, f := range sv.Fields {
		if f.Name == field {
			return f.Value, true
		}
	}
	return gobspect.NilValue{}, false
}
