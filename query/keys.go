package query

import (
	"fmt"
	"strconv"

	"github.com/codepuke/gobspect"
)

// KeysPath returns the navigable keys at the node resolved by p against root.
//   - StructValue: field names in declaration order.
//   - SliceValue/ArrayValue: index strings ("0", "1", …).
//   - MapValue: string-coerced keys (StringValue keys, unwrapping any
//     InterfaceValue wrapper first). Returns (nil, false) if the map is
//     non-empty but has no string-typed keys.
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
// Returns (nil, false) if the path does not resolve, or if the node is a type that cannot be navigated by key (e.g. a scalar or a map with no string keys).
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
		if len(n.Entries) == 0 {
			return []string{}, true
		}
		var keys []string
		for _, e := range n.Entries {
			if k, ok := keyString(e.Key); ok {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			return nil, false
		}
		return keys, true
	}

	// Scalar, OpaqueValue, NilValue, nil — no keys.
	return nil, false
}
