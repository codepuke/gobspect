package query

import "github.com/codepuke/gobspect"

// buildProjection constructs a synthetic anonymous StructValue from the
// requested field names. It works on StructValue (lookup by field name) and
// MapValue (lookup by string key). Missing fields are filled with NilValue to
// guarantee a uniform shape for downstream formatters.
//
// Returns nil if v is not a StructValue or MapValue.
func buildProjection(v gobspect.Value, fields []string) gobspect.Value {
	switch n := v.(type) {
	case gobspect.StructValue:
		out := make([]gobspect.Field, len(fields))
		for i, name := range fields {
			found := false
			for _, f := range n.Fields {
				if f.Name == name {
					out[i] = f
					found = true
					break
				}
			}
			if !found {
				out[i] = gobspect.Field{Name: name, Value: gobspect.NilValue{}}
			}
		}
		return gobspect.StructValue{TypeName: "", Fields: out}

	case gobspect.MapValue:
		out := make([]gobspect.Field, len(fields))
		for i, name := range fields {
			found := false
			for _, e := range n.Entries {
				if sv, ok := e.Key.(gobspect.StringValue); ok && sv.V == name {
					out[i] = gobspect.Field{Name: name, Value: e.Value}
					found = true
					break
				}
			}
			if !found {
				out[i] = gobspect.Field{Name: name, Value: gobspect.NilValue{}}
			}
		}
		return gobspect.StructValue{TypeName: "", Fields: out}

	default:
		return nil
	}
}
