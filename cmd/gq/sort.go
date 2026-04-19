package main

import (
	"iter"
	"sort"

	"github.com/codepuke/gobspect"
)

// seqOf converts a []gobspect.Value slice into an iter.Seq[gobspect.Value].
func seqOf(vals []gobspect.Value) iter.Seq[gobspect.Value] {
	return func(yield func(gobspect.Value) bool) {
		for _, v := range vals {
			if !yield(v) {
				return
			}
		}
	}
}

// sortMatches drains matches into a slice, optionally filters rows missing all
// sort keys (when spec.DropMissing is true), then sorts stably by spec.Compare.
func sortMatches(matches iter.Seq[gobspect.Value], spec SortSpec) []gobspect.Value {
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
