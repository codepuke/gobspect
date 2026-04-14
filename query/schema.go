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
					idx := strings.Index(currentExpr, "]")
					if idx > 0 {
						currentExpr = currentExpr[idx+1:]
						break
					}
				}
				return "", fmt.Errorf("schema lookup: type %q not found or not a struct/map", currentExpr)
			}

			if decl.Kind == gobspect.KindMap {
				// Extract map value type
				target := decl.TargetType
				currentExpr = target[strings.Index(target, "]")+1:]
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
		}
	}
	return currentExpr, nil
}

func extractElemType(schema *gobspect.Schema, expr string) (string, error) {
	if strings.HasPrefix(expr, "[]") {
		return expr[2:], nil
	}
	if strings.HasPrefix(expr, "[") { // Array [N]T
		idx := strings.Index(expr, "]")
		if idx > 0 {
			return expr[idx+1:], nil
		}
	}
	if strings.HasPrefix(expr, "map[") {
		idx := strings.Index(expr, "]")
		if idx > 0 {
			return expr[idx+1:], nil
		}
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
		return target[strings.Index(target, "]")+1:], nil
	case gobspect.KindMap:
		target := decl.TargetType
		return target[strings.Index(target, "]")+1:], nil
	}

	return "", fmt.Errorf("type %q is not a collection", expr)
}
