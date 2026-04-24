// Package sortval provides sorting utilities for sequences of gobspect.Value nodes.
package sortval

import (
	"fmt"
	"iter"
	"sort"
	"strings"

	"github.com/codepuke/gobspect"
)

// SortSpec describes how to sort a sequence of [gobspect.Value] nodes by struct field keys.
type SortSpec struct {
	Keys        []string // field names in priority order
	Desc        bool     // applies to all keys
	Fold        bool     // case-insensitive string comparison
	DropMissing bool     // exclude rows missing ALL sort keys when true
}

// ParseSortSpec parses a comma-separated list of field names and modifiers into a SortSpec.
// Returns an error if keysFlag is empty or contains empty field names.
func ParseSortSpec(keysFlag string, desc, fold, dropMissing bool) (SortSpec, error) {
	if keysFlag == "" {
		return SortSpec{}, fmt.Errorf("sort keys must not be empty")
	}
	parts := strings.Split(keysFlag, ",")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		k := strings.TrimSpace(p)
		if k == "" {
			return SortSpec{}, fmt.Errorf("sort keys must not contain empty field names")
		}
		keys = append(keys, k)
	}
	return SortSpec{
		Keys:        keys,
		Desc:        desc,
		Fold:        fold,
		DropMissing: dropMissing,
	}, nil
}

// Compare compares two [gobspect.Value] nodes by the spec's keys.
// Returns -1, 0, or +1. If Desc is true, the result is negated.
// Missing fields produce NilValue{} for comparison (regardless of DropMissing).
func (s SortSpec) Compare(a, b gobspect.Value) int {
	cmpFn := gobspect.CompareValues
	if s.Fold {
		cmpFn = gobspect.CompareValuesFold
	}
	for _, key := range s.Keys {
		av, _ := extractSortKey(a, key)
		bv, _ := extractSortKey(b, key)
		r := cmpFn(av, bv)
		if r != 0 {
			if s.Desc {
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
				if _, ok := extractSortKey(row, key); ok {
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
