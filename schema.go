package gobspect

import (
	"fmt"
	"io"
	"slices"
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
	slices.SortFunc(named, func(a, b TypeInfo) int {
		return strings.Compare(a.Name, b.Name)
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
	return s.Format()
}

// Format renders the Schema as a string, applying any provided FormatOptions.
// Currently only [WithColor] and [WithIndent] are respected; other options are
// silently ignored. A zero-valued [ColorScheme] (the default) produces plain
// text identical to [Schema.String].
func (s *Schema) Format(opts ...FormatOption) string {
	var sb strings.Builder
	_ = s.FormatTo(&sb, opts...)
	return sb.String()
}

// FormatTo renders the Schema to w. The first write error aborts rendering and
// is returned. Currently only [WithColor] and [WithIndent] are respected.
func (s *Schema) FormatTo(w io.Writer, opts ...FormatOption) error {
	cfg := &formatConfig{indent: s.Indent}
	if cfg.indent == "" {
		cfg.indent = "  "
	}
	for _, o := range opts {
		o(cfg)
	}
	return schemaFormatTo(w, s, cfg)
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

// FormatString renders the schema with the given indentation string.
// Deprecated: prefer [Schema.Format] with [WithIndent].
func (s *Schema) FormatString(indent string) string {
	if indent == "" {
		indent = "  "
	}
	return s.Format(WithIndent(indent))
}

// schemaFormatTo writes the Schema to w using the given formatConfig for indent
// and color options.
func schemaFormatTo(w io.Writer, s *Schema, cfg *formatConfig) error {
	indent := cfg.indent
	clr := cfg.color

	// keyword wraps a keyword token (e.g. "type") in the TypeHeader style.
	// In the schema, we use TypeHeader for the "type" keyword and for the
	// type name. We reuse OpaquePrefix (dim) for comments/annotations.

	for i, t := range s.Types {
		if i > 0 {
			if _, err := io.WriteString(w, "\n\n"); err != nil {
				return err
			}
		}

		switch t.Kind {
		case KindStruct:
			if len(t.Fields) == 0 {
				_, err := io.WriteString(w,
					clr.Number.apply("type")+" "+clr.TypeHeader.apply(t.Name)+" struct{}")
				if err != nil {
					return err
				}
				continue
			}

			// Determine max len to align all field types.
			maxLen := 0
			for _, f := range t.Fields {
				if len(f.Name) > maxLen {
					maxLen = len(f.Name)
				}
			}

			line := clr.Number.apply("type") + " " + clr.TypeHeader.apply(t.Name) + " struct {\n"
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
			for _, f := range t.Fields {
				fieldLine := indent +
					clr.FieldName.apply(f.Name) +
					strings.Repeat(" ", maxLen-len(f.Name)+2) +
					f.Type
				if f.Annotation != "" {
					fieldLine += "  " + clr.OpaquePrefix.apply("// "+f.Annotation)
				}
				fieldLine += "\n"
				if _, err := io.WriteString(w, fieldLine); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, cfg.color.CloseBrace.apply("}")); err != nil {
				return err
			}

		case KindMap, KindSlice, KindArray:
			line := clr.Number.apply("type") + " " + clr.TypeHeader.apply(t.Name) + " " + t.TargetType
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		case KindGobEncoder, KindBinaryMarshaler, KindTextMarshaler:
			line := clr.Number.apply("type") + " " + clr.TypeHeader.apply(t.Name) +
				" " + clr.OpaquePrefix.apply("// "+t.Annotation)
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		default:
			line := clr.OpaquePrefix.apply("// type " + t.Name)
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
	}
	return nil
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
