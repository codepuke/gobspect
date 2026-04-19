package main

import "github.com/codepuke/gobspect"

// extractSortKey returns the value of the field named field from v.
// v is unwrapped if it is an InterfaceValue.
// For StructValue: returns the first field whose Name == field, ok=true.
// For non-struct or missing field: returns (NilValue{}, false).
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
