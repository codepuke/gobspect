package gobspect

import (
	"fmt"
	"sort"
	"strings"
)

// Schema represents a complete parsed Gob schema comprising multiple type declarations.
type Schema struct {
	Types  []TypeDecl
	Indent string
}

// TypeDecl represents a top-level type declaration in the schema.
type TypeDecl struct {
	Name       string
	Kind       TypeKind
	Fields     []FieldDecl // Populated if Kind is KindStruct
	TargetType string      // Target expression for slices, maps, arrays
	Annotation string      // Opaque type annotations (GobEncoder, etc.)
}

// FieldDecl represents a field within a struct declaration.
type FieldDecl struct {
	Name       string
	Type       string
	Annotation string
}

// FormatSchema converts a []TypeInfo slice into an AST-based *Schema, covering
// all top-level named types. Named types with mechanically generated names
// (containing brackets, e.g. "[]int") are safely excluded from the top-level
// schema output. Anonymous (unnamed) types appear only inline within other
// declarations.
//
// The only FormatOption currently respected is [WithIndent] when formatted; all
// others are silently ignored.
func FormatSchema(types []TypeInfo, opts ...FormatOption) *Schema {
	cfg := &formatConfig{indent: "  "}
	for _, o := range opts {
		o(cfg)
	}

	// Build a lookup map keyed by type ID for resolving references.
	byID := make(map[int]TypeInfo, len(types))
	for _, ti := range types {
		byID[ti.ID] = ti
	}

	// Collect only named types; anonymous types appear inline only.
	// We also skip mechanically-generated names: anonymous slices ([]T), arrays
	// ([N]T), maps (map[K]V), and pointers (*T) all begin with one of those
	// prefixes. User-defined generic instantiations like "Pair[int,string]" do
	// NOT start with those prefixes, so they are kept.
	var named []TypeInfo
	for _, ti := range types {
		if ti.Name != "" && !isMechanicalTypeName(ti.Name) {
			named = append(named, ti)
		}
	}

	// Sort alphabetically for deterministic, readable output.
	sort.Slice(named, func(i, j int) bool {
		return named[i].Name < named[j].Name
	})

	schema := &Schema{Indent: cfg.indent}
	for _, ti := range named {
		schema.Types = append(schema.Types, buildTypeDecl(ti, byID))
	}
	return schema
}

// String renders the Schema as a human-readable Go-style type declaration block,
// matching the original plain-text formatting exactly.
func (s *Schema) String() string {
	return s.FormatString(s.Indent)
}

// TypeByName locates a top-level type declaration by its name.
func (s *Schema) TypeByName(name string) (*TypeDecl, bool) {
	for i := range s.Types {
		if s.Types[i].Name == name {
			return &s.Types[i], true
		}
	}
	return nil, false
}

// FormatString respects custom indentation.
func (s *Schema) FormatString(indent string) string {
	var sb strings.Builder
	for i, t := range s.Types {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		
		switch t.Kind {
		case KindStruct:
			if len(t.Fields) == 0 {
				sb.WriteString("type " + t.Name + " struct{}")
				continue
			}

			// Determine max len to align all field types.
			maxLen := 0
			for _, f := range t.Fields {
				if len(f.Name) > maxLen {
					maxLen = len(f.Name)
				}
			}

			sb.WriteString("type ")
			sb.WriteString(t.Name)
			sb.WriteString(" struct {\n")
			for _, f := range t.Fields {
				sb.WriteString(indent)
				sb.WriteString(f.Name)
				// Pad so all type expressions start at the same column (maxLen + 2 spaces).
				sb.WriteString(strings.Repeat(" ", maxLen-len(f.Name)+2))
				sb.WriteString(f.Type)
				if f.Annotation != "" {
					sb.WriteString("  // ")
					sb.WriteString(f.Annotation)
				}
				sb.WriteString("\n")
			}
			sb.WriteString("}")

		case KindMap, KindSlice, KindArray:
			sb.WriteString(fmt.Sprintf("type %s %s", t.Name, t.TargetType))
		case KindGobEncoder, KindBinaryMarshaler, KindTextMarshaler:
			sb.WriteString(fmt.Sprintf("type %s // %s", t.Name, t.Annotation))
		default:
			sb.WriteString("// type " + t.Name)
		}
	}
	return sb.String()
}

func buildTypeDecl(ti TypeInfo, byID map[int]TypeInfo) TypeDecl {
	decl := TypeDecl{
		Name: ti.Name,
		Kind: ti.Kind,
	}

	switch ti.Kind {
	case KindStruct:
		for _, f := range ti.Fields {
			expr, annotation := schemaFieldTypeExpr(f.TypeID, byID)
			decl.Fields = append(decl.Fields, FieldDecl{
				Name:       f.Name,
				Type:       expr,
				Annotation: annotation,
			})
		}
	case KindMap:
		decl.TargetType = fmt.Sprintf("map[%s]%s", schemaTypeExpr(ti.Key, byID), schemaTypeExpr(ti.Elem, byID))
	case KindSlice:
		decl.TargetType = fmt.Sprintf("[]%s", schemaTypeExpr(ti.Elem, byID))
	case KindArray:
		decl.TargetType = fmt.Sprintf("[%d]%s", ti.Len, schemaTypeExpr(ti.Elem, byID))
	case KindGobEncoder:
		decl.Annotation = "GobEncoder"
	case KindBinaryMarshaler:
		decl.Annotation = "BinaryMarshaler"
	case KindTextMarshaler:
		decl.Annotation = "TextMarshaler"
	}
	return decl
}

// schemaTypeExpr returns the Go type expression for the type identified by ref.
// For named types it returns the name. For anonymous composite types it
// constructs an inline expression (e.g. "[]string", "map[string]int").
func schemaTypeExpr(ref *TypeRef, byID map[int]TypeInfo) string {
	if ref == nil {
		return "?"
	}
	if name, ok := builtinTypeName(ref.ID); ok {
		return name
	}
	ti, ok := byID[ref.ID]
	if !ok {
		if ref.Name != "" {
			return ref.Name
		}
		return "?"
	}
	if ti.Name != "" {
		return ti.Name
	}
	// Anonymous composite: build inline expression.
	switch ti.Kind {
	case KindSlice:
		return "[]" + schemaTypeExpr(ti.Elem, byID)
	case KindArray:
		return fmt.Sprintf("[%d]%s", ti.Len, schemaTypeExpr(ti.Elem, byID))
	case KindMap:
		return "map[" + schemaTypeExpr(ti.Key, byID) + "]" + schemaTypeExpr(ti.Elem, byID)
	default:
		if ref.Name != "" {
			return ref.Name
		}
		return "?"
	}
}

// schemaFieldTypeExpr returns the type expression and optional opaque-kind
// annotation for a struct field whose type ID is typeID.
// The annotation is non-empty only for opaque kinds (GobEncoder, BinaryMarshaler,
// TextMarshaler) and names the encoding interface.
func schemaFieldTypeExpr(typeID int, byID map[int]TypeInfo) (expr, annotation string) {
	if name, ok := builtinTypeName(typeID); ok {
		return name, ""
	}
	ti, ok := byID[typeID]
	if !ok {
		return "?", ""
	}
	name := ti.Name
	if name == "" {
		name = "?"
	}
	switch ti.Kind {
	case KindGobEncoder:
		return name, "GobEncoder"
	case KindBinaryMarshaler:
		return name, "BinaryMarshaler"
	case KindTextMarshaler:
		return name, "TextMarshaler"
	default:
		return schemaTypeExpr(&TypeRef{ID: typeID, Name: ti.Name}, byID), ""
	}
}

// isMechanicalTypeName reports whether name is an anonymous composite type
// generated by the reflect package (slice, array, map, or pointer). These names
// start with "[", "map[", or "*". User-defined generic instantiations such as
// "Pair[int,string]" do not start with any of these prefixes and therefore
// return false, so they are preserved in schema output.
//
// Names containing a space (e.g. "struct { X int }") are also treated as
// mechanical to guard against anonymous struct literals.
func isMechanicalTypeName(name string) bool {
	return strings.HasPrefix(name, "[") ||
		strings.HasPrefix(name, "map[") ||
		strings.HasPrefix(name, "*") ||
		strings.Contains(name, " ")
}
