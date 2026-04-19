package main

import (
	"fmt"
	"strings"

	"github.com/codepuke/gobspect"
)

// SortSpec describes how to sort rows from a tabular gob stream.
type SortSpec struct {
	Keys        []string // field names in priority order
	Desc        bool     // applies to all keys
	Fold        bool     // case-insensitive string comparison
	DropMissing bool     // if true, rows missing ALL sort keys are excluded
}

// ParseSortSpec parses the -sort flag value and modifiers into a SortSpec.
// keysFlag is the comma-separated list of field names.
// Returns error if keysFlag is empty or contains empty field names.
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

// Compare compares two gobspect.Value rows by the sort spec's keys.
// Returns -1, 0, or +1. If Desc is true, the result is negated.
// Missing fields produce NilValue{} for comparison (regardless of DropMissing).
func (s SortSpec) Compare(a, b gobspect.Value) int {
	cmpFn := compareValues
	if s.Fold {
		cmpFn = compareValuesFold
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
