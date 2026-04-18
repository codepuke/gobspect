package query

import (
	"strings"

	"github.com/codepuke/gobspect"
)

// ProjectionTypeName is the TypeName sentinel set on StructValues produced by
// buildProjection. The tabular printer uses this to bypass heterogeneous-type
// checking, since projections intentionally unify fields from disparate source
// types into a uniform synthetic struct.
const ProjectionTypeName = "gobspect:projection"

// buildProjection constructs a synthetic StructValue from the requested field
// names. It works on StructValue (lookup by field name) and MapValue (lookup
// by string key). Missing fields are filled with NilValue to guarantee a
// uniform shape for downstream formatters.
//
// Field names may contain "/" to navigate nested structs: "Address/Zip"
// walks to the Address field then the Zip field within it. The column name
// in the output is the last "/"-component (e.g. "Zip").
//
// The returned StructValue carries TypeName == ProjectionTypeName so that
// downstream consumers (e.g. the tabular printer) can identify it as a
// projection and bypass normal heterogeneous-type restrictions.
//
// Returns nil if v is not a StructValue or MapValue.
func buildProjection(v gobspect.Value, fields []string) gobspect.Value {
	if _, ok := v.(gobspect.StructValue); !ok {
		if _, ok := v.(gobspect.MapValue); !ok {
			return nil
		}
	}
	out := make([]gobspect.Field, len(fields))
	for i, name := range fields {
		colName, val := resolveProjectionField(v, name)
		out[i] = gobspect.Field{Name: colName, Value: val}
	}
	return gobspect.StructValue{TypeName: ProjectionTypeName, Fields: out}
}

// resolveProjectionField looks up a single projection field name, which may
// contain "/" separators for nested navigation. Returns the column name (last
// component) and the resolved value (NilValue if any step misses).
func resolveProjectionField(v gobspect.Value, name string) (string, gobspect.Value) {
	components := strings.Split(name, "/")
	cur := v
	for _, c := range components {
		next, ok := stepField(cur, c)
		if !ok {
			return components[len(components)-1], gobspect.NilValue{}
		}
		cur = next
	}
	return components[len(components)-1], cur
}
