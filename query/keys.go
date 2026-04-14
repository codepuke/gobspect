package query

import (
	"fmt"
	"strconv"

	"github.com/codepuke/gobspect"
)

// KeysPath returns the navigable keys at the node resolved by p against root.
//   - StructValue: field names in declaration order.
//   - SliceValue/ArrayValue: index strings ("0", "1", …).
//   - MapValue: string-coerced keys (only StringValue keys are included).
//   - InterfaceValue: unwrapped before dispatch.
//   - Scalar, OpaqueValue, NilValue: returns (nil, false).
//
// Returns (nil, false) if the path does not resolve.
func KeysPath(root gobspect.Value, p Path) ([]string, bool) {
	v, ok := GetPath(root, p)
	if !ok {
		return nil, false
	}
	return keysOf(v)
}

// Keys returns the navigable keys at the node resolved by expr against root.
// Panics if expr is syntactically invalid.
// Returns (nil, false) if the path does not resolve or the node has no keys.
func Keys(root gobspect.Value, expr string) ([]string, bool) {
	p, err := Parse(expr)
	if err != nil {
		panic(fmt.Sprintf("query.Keys: invalid path expression %q: %v", expr, err))
	}
	return KeysPath(root, p)
}

// keysOf returns the navigable keys of v, unwrapping InterfaceValue first.
func keysOf(v gobspect.Value) ([]string, bool) {
	v = unwrapInterface(v)
	switch n := v.(type) {
	case gobspect.StructValue:
		if len(n.Fields) == 0 {
			return []string{}, true
		}
		keys := make([]string, len(n.Fields))
		for i, f := range n.Fields {
			keys[i] = f.Name
		}
		return keys, true

	case gobspect.SliceValue:
		if len(n.Elems) == 0 {
			return []string{}, true
		}
		keys := make([]string, len(n.Elems))
		for i := range n.Elems {
			keys[i] = strconv.Itoa(i)
		}
		return keys, true

	case gobspect.ArrayValue:
		if len(n.Elems) == 0 {
			return []string{}, true
		}
		keys := make([]string, len(n.Elems))
		for i := range n.Elems {
			keys[i] = strconv.Itoa(i)
		}
		return keys, true

	case gobspect.MapValue:
		var keys []string
		for _, e := range n.Entries {
			if sv, ok := e.Key.(gobspect.StringValue); ok {
				keys = append(keys, sv.V)
			}
		}
		if keys == nil {
			keys = []string{}
		}
		return keys, true
	}

	// Scalar, OpaqueValue, NilValue, nil — no keys.
	return nil, false
}
