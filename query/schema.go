package query

import (
	"fmt"
	"strings"

	"github.com/codepuke/gobspect"
)

// SchemaAt evaluates a path expression statically against a schema to determine
// the type expression of the resulting values. 
// rootTypeExpr is the name of the type representing the root value of the query
// (e.g., "Order", "[]main.LineItem").
// Note: recursive descent (..) cannot be resolved statically.
func SchemaAt(schema *gobspect.Schema, rootTypeExpr string, p Path) (string, error) {
	currentExpr := rootTypeExpr

	for _, seg := range p.segs {
		switch seg.kind {
		case segField:
			decl, ok := schema.TypeByName(currentExpr)
			if !ok {
				// Try unnamed map.
				if strings.HasPrefix(currentExpr, "map[") {
					valType, keyType, err := mapValueAndKeyType(currentExpr)
					if err != nil {
						return "", fmt.Errorf("schema lookup: malformed map type %q: %v", currentExpr, err)
					}
					if keyType != "string" {
						return "", fmt.Errorf("schema lookup: field %q cannot navigate map with key type %q (only map[string]T is field-navigable)", seg.name, keyType)
					}
					currentExpr = valType
					break
				}
				return "", fmt.Errorf("schema lookup: type %q not found or not a struct/map", currentExpr)
			}

			if decl.Kind == gobspect.KindMap {
				// Extract map value and key type from the target expression.
				valType, keyType, err := mapValueAndKeyType(decl.TargetType)
				if err != nil {
					return "", fmt.Errorf("schema lookup: malformed map type %q: %v", decl.TargetType, err)
				}
				if keyType != "string" {
					return "", fmt.Errorf("schema lookup: field %q cannot navigate map with key type %q (only map[string]T is field-navigable)", seg.name, keyType)
				}
				currentExpr = valType
				break
			}

			if decl.Kind != gobspect.KindStruct {
				return "", fmt.Errorf("schema lookup: type %q is not a struct or map", currentExpr)
			}

			found := false
			for _, f := range decl.Fields {
				if f.Name == seg.name {
					currentExpr = f.Type
					found = true
					break
				}
			}
			if !found {
				return "", fmt.Errorf("schema lookup: field %q not found in struct %q", seg.name, currentExpr)
			}

		case segIndex, segFilter, segWildcard:
			// Resolve element type of collection.
			elem, err := extractElemType(schema, currentExpr)
			if err != nil {
				return "", fmt.Errorf("schema lookup on collection: %v", err)
			}
			currentExpr = elem

		case segDescend:
			return "", fmt.Errorf("schema lookup: recursive descent (..) is not supported statically")

		case segProject:
			// Projection produces an anonymous struct; no schema entry exists.
			currentExpr = "struct"
		}
	}
	return currentExpr, nil
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
