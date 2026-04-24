package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/codepuke/gobspect"
)

// SchemaAt evaluates a path expression statically against a schema to determine
// the type expression of the resulting values. rootTypeExpr is the name of the
// type representing the root value of the query (e.g., "Order", "[]main.LineItem").
//
// When the path contains a recursive-descent segment (".."), resolution is
// widened to consider every type reachable from the current type. If more than
// one distinct result type is possible, SchemaAt returns a pipe-joined union
// (e.g. "string|int") with the alternatives in sorted order.
func SchemaAt(schema *gobspect.Schema, rootTypeExpr string, p Path) (string, error) {
	hasDescend := false
	for _, seg := range p.segs {
		if seg.kind == segDescend {
			hasDescend = true
			break
		}
	}
	if !hasDescend {
		return schemaAtSingle(schema, rootTypeExpr, p.segs)
	}

	candidates := []string{rootTypeExpr}
	for _, seg := range p.segs {
		if seg.kind == segDescend {
			next := descendResolve(schema, candidates, seg.name)
			if len(next) == 0 {
				return "", fmt.Errorf("schema lookup: recursive descent %q produced no reachable types", segString(seg))
			}
			candidates = next
			continue
		}
		next := make([]string, 0, len(candidates))
		seen := make(map[string]struct{}, len(candidates))
		for _, c := range candidates {
			r, err := advanceSegment(schema, c, seg)
			if err != nil {
				continue
			}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			next = append(next, r)
		}
		if len(next) == 0 {
			return "", fmt.Errorf("schema lookup: no candidate types match segment after recursive descent")
		}
		candidates = next
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("schema lookup: recursive descent produced no reachable types")
	}
	candidates = dedupeSortedStrings(candidates)
	return strings.Join(candidates, "|"), nil
}

// descendResolve executes one segDescend segment over the candidate set.
// With a non-empty name, it returns the union of field types named `name`
// reachable from any candidate. With an empty name (wildcard descent, for
// "..[filter]"), it returns the union of collection-element types reachable
// from any candidate, to be further filtered by the following segment.
func descendResolve(schema *gobspect.Schema, candidates []string, name string) []string {
	reachable := expandDescent(schema, candidates)
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	if name == "" {
		// Wildcard descent: every reachable collection contributes its element type.
		for _, expr := range reachable {
			if elem, err := extractElemType(schema, expr); err == nil {
				add(elem)
			}
		}
		return dedupeSortedStrings(out)
	}

	for _, expr := range reachable {
		// Struct field match: named type lookup.
		if decl, ok := schema.TypeByName(expr); ok {
			switch decl.Kind {
			case gobspect.KindStruct:
				for _, f := range decl.Fields {
					if f.Name == name {
						add(f.Type)
					}
				}
			case gobspect.KindMap:
				// "..name" on a map with string keys may match a key at runtime;
				// the value type is a candidate.
				val, keyType, err := mapValueAndKeyType(decl.TargetType)
				if err == nil && keyType == "string" {
					add(val)
				}
			}
			continue
		}
		// Inline map[string]T may also match "..name" on a string key.
		if strings.HasPrefix(expr, "map[") {
			val, keyType, err := mapValueAndKeyType(expr)
			if err == nil && keyType == "string" {
				add(val)
			}
		}
	}
	return dedupeSortedStrings(out)
}

// schemaAtSingle is the original linear resolver for paths with no recursive
// descent. Errors are propagated.
func schemaAtSingle(schema *gobspect.Schema, rootTypeExpr string, segs []segment) (string, error) {
	currentExpr := rootTypeExpr
	for _, seg := range segs {
		next, err := advanceSegment(schema, currentExpr, seg)
		if err != nil {
			return "", err
		}
		currentExpr = next
	}
	return currentExpr, nil
}

// advanceSegment resolves one segment against the current type expression,
// returning the resulting type expression or an error.
func advanceSegment(schema *gobspect.Schema, currentExpr string, seg segment) (string, error) {
	switch seg.kind {
	case segField:
		decl, ok := schema.TypeByName(currentExpr)
		if !ok {
			if strings.HasPrefix(currentExpr, "map[") {
				valType, keyType, err := mapValueAndKeyType(currentExpr)
				if err != nil {
					return "", fmt.Errorf("schema lookup: malformed map type %q: %v", currentExpr, err)
				}
				if keyType != "string" {
					return "", fmt.Errorf("schema lookup: cannot navigate field %q on map with non-string keys", seg.name)
				}
				return valType, nil
			}
			if strings.HasPrefix(currentExpr, "[") {
				return "", fmt.Errorf("schema lookup: cannot navigate field %q on slice/array type %q — use an index or wildcard first", seg.name, currentExpr)
			}
			return "", fmt.Errorf("schema lookup: type %q not found or not a struct/map", currentExpr)
		}

		if decl.Kind == gobspect.KindMap {
			valType, keyType, err := mapValueAndKeyType(decl.TargetType)
			if err != nil {
				return "", fmt.Errorf("schema lookup: malformed map type %q: %v", decl.TargetType, err)
			}
			if keyType != "string" {
				return "", fmt.Errorf("schema lookup: cannot navigate field %q on map with non-string keys", seg.name)
			}
			return valType, nil
		}

		if decl.Kind == gobspect.KindSlice || decl.Kind == gobspect.KindArray {
			return "", fmt.Errorf("schema lookup: cannot navigate field %q on slice/array type %q — use an index or wildcard first", seg.name, currentExpr)
		}

		if decl.Kind != gobspect.KindStruct {
			return "", fmt.Errorf("schema lookup: type %q is not a struct or map", currentExpr)
		}

		for _, f := range decl.Fields {
			if f.Name == seg.name {
				return f.Type, nil
			}
		}
		return "", fmt.Errorf("schema lookup: field %q not found in struct %q", seg.name, currentExpr)

	case segIndex, segFilter, segWildcard:
		elem, err := extractElemType(schema, currentExpr)
		if err != nil {
			return "", fmt.Errorf("schema lookup on collection: %v", err)
		}
		return elem, nil

	case segDescend:
		return "", fmt.Errorf("schema lookup: recursive descent (..) must be handled by SchemaAt")

	case segProject:
		return "struct", nil
	}
	return "", fmt.Errorf("schema lookup: unknown segment kind")
}

// expandDescent returns the transitive closure of types reachable from each
// candidate, including the candidate itself. The closure walks struct fields,
// map keys and values, and slice/array elements. Builtin scalar types
// ("int", "string", etc.) are included only when directly reached; they
// produce no further descendants.
func expandDescent(schema *gobspect.Schema, candidates []string) []string {
	visited := make(map[string]struct{})
	var out []string
	var dfs func(expr string)
	dfs = func(expr string) {
		if expr == "" {
			return
		}
		if _, ok := visited[expr]; ok {
			return
		}
		visited[expr] = struct{}{}
		out = append(out, expr)

		// Inline collections: look inside.
		if strings.HasPrefix(expr, "[]") {
			dfs(expr[2:])
			return
		}
		if strings.HasPrefix(expr, "[") {
			if idx := findMatchingBracket(expr, 0); idx > 0 {
				dfs(expr[idx+1:])
			}
			return
		}
		if strings.HasPrefix(expr, "map[") {
			val, key, err := mapValueAndKeyType(expr)
			if err == nil {
				dfs(key)
				dfs(val)
			}
			return
		}
		// Pointers: unwrap.
		if strings.HasPrefix(expr, "*") {
			dfs(expr[1:])
			return
		}

		decl, ok := schema.TypeByName(expr)
		if !ok {
			return
		}
		switch decl.Kind {
		case gobspect.KindStruct:
			for _, f := range decl.Fields {
				dfs(f.Type)
			}
		case gobspect.KindSlice, gobspect.KindArray:
			if inner, err := extractElemType(schema, expr); err == nil {
				dfs(inner)
			}
		case gobspect.KindMap:
			val, key, err := mapValueAndKeyType(decl.TargetType)
			if err == nil {
				dfs(key)
				dfs(val)
			}
		}
	}
	for _, c := range candidates {
		dfs(c)
	}
	return out
}

func dedupeSortedStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func extractElemType(schema *gobspect.Schema, expr string) (string, error) {
	if strings.HasPrefix(expr, "[]") {
		return expr[2:], nil
	}
	if strings.HasPrefix(expr, "[") { // Array [N]T
		idx := findMatchingBracket(expr, 0)
		if idx > 0 {
			return expr[idx+1:], nil
		}
	}
	if strings.HasPrefix(expr, "map[") {
		valType, _, err := mapValueAndKeyType(expr)
		if err != nil {
			return "", fmt.Errorf("malformed map type %q: %v", expr, err)
		}
		return valType, nil
	}

	decl, ok := schema.TypeByName(expr)
	if !ok {
		return "", fmt.Errorf("type %q is not a recognized collection", expr)
	}

	switch decl.Kind {
	case gobspect.KindSlice:
		return strings.TrimPrefix(decl.TargetType, "[]"), nil
	case gobspect.KindArray:
		target := decl.TargetType
		idx := findMatchingBracket(target, 0)
		if idx < 0 {
			return "", fmt.Errorf("malformed array type %q", target)
		}
		return target[idx+1:], nil
	case gobspect.KindMap:
		valType, _, err := mapValueAndKeyType(decl.TargetType)
		if err != nil {
			return "", fmt.Errorf("malformed map type %q: %v", decl.TargetType, err)
		}
		return valType, nil
	}

	return "", fmt.Errorf("type %q is not a collection", expr)
}

// findMatchingBracket returns the index of the ']' that matches the '[' at
// position openPos in s, correctly handling nested brackets. Returns -1 if no
// matching bracket is found.
func findMatchingBracket(s string, openPos int) int {
	if openPos >= len(s) || s[openPos] != '[' {
		return -1
	}
	depth := 0
	for i := openPos; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// mapValueAndKeyType parses a map type expression like "map[K]V" and returns
// the value type and key type. It uses bracket-depth matching to correctly
// handle composite keys like "map[[4]byte]string" or "map[map[string]int]Foo".
func mapValueAndKeyType(expr string) (valType, keyType string, err error) {
	if !strings.HasPrefix(expr, "map[") {
		return "", "", fmt.Errorf("not a map type: %q", expr)
	}
	// The opening '[' is at index 3.
	closeIdx := findMatchingBracket(expr, 3)
	if closeIdx < 0 {
		return "", "", fmt.Errorf("no matching ']' in %q", expr)
	}
	keyType = expr[4:closeIdx]
	valType = expr[closeIdx+1:]
	return valType, keyType, nil
}
