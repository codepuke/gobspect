package query_test

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaAt(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Order",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "ID", Type: "int"},
					{Name: "Items", Type: "[]LineItem"},
					{Name: "Tags", Type: "map[string]string"},
				},
			},
			{
				Name: "LineItem",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Price", Type: "float"},
					{Name: "SKU", Type: "string"},
				},
			},
		},
	}

	tests := []struct {
		expr    string
		want    string
		wantErr bool
	}{
		{"ID", "int", false},
		{"Items", "[]LineItem", false},
		{"Items.0", "LineItem", false},
		{"Items[Price>30]", "LineItem", false},
		{"Items.*.SKU", "string", false},
		{"Tags.promo", "string", false}, // Tags is map[string]string
		{"Missing", "", true},
		{"Items.0.Missing", "", true},
		{"ID.0", "", true},       // cannot index int
		{"..Items", "", true},    // descent not supported statically
		{"SKU,Price", "struct", false}, // projection returns anonymous struct
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)

			got, err := query.SchemaAt(schema, "Order", p)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestSchemaAt_CompositeMapKeys verifies that SchemaAt and extractElemType
// correctly handle map types whose keys contain brackets (e.g. map[[4]byte]string).
// Before the fix, strings.Index(_, "]") matched the first ']' inside the key,
// producing garbage value types like "]string".
func TestSchemaAt_CompositeMapKeys(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name:       "ByteIndex",
				Kind:       gobspect.KindMap,
				TargetType: "map[[4]byte]string",
			},
			{
				Name:       "NestedIndex",
				Kind:       gobspect.KindMap,
				TargetType: "map[map[string]int]Foo",
			},
			{
				Name: "Foo",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "X", Type: "int"},
				},
			},
		},
	}

	tests := []struct {
		name     string
		rootType string
		expr     string
		want     string
	}{
		{
			name:     "map_with_array_key_wildcard",
			rootType: "ByteIndex",
			expr:     "*",
			want:     "string",
		},
		{
			name:     "map_with_nested_map_key_wildcard",
			rootType: "NestedIndex",
			expr:     "*",
			want:     "Foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)

			got, err := query.SchemaAt(schema, tt.rootType, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSchemaAt_NonStringMapKeyRejectsField verifies that SchemaAt rejects
// segField navigation on maps whose key type is not string. Before the fix,
// SchemaAt(schema, "map[int]Foo", ...{segField: "xyz"}) silently succeeded.
func TestSchemaAt_NonStringMapKeyRejectsField(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name:       "IntMap",
				Kind:       gobspect.KindMap,
				TargetType: "map[int]Foo",
			},
			{
				Name: "Foo",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "X", Type: "int"},
				},
			},
			{
				Name: "Wrapper",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Data", Type: "map[int]string"},
				},
			},
		},
	}

	tests := []struct {
		name     string
		rootType string
		expr     string
		wantErr  string
	}{
		{
			name:     "named_map_int_key",
			rootType: "IntMap",
			expr:     "xyz",
			wantErr:  "only map[string]T is field-navigable",
		},
		{
			name:     "inline_map_int_key",
			rootType: "Wrapper",
			expr:     "Data.xyz",
			wantErr:  "only map[string]T is field-navigable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)

			_, err = query.SchemaAt(schema, tt.rootType, p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestSchemaAt_MapProjection verifies that field projection works correctly
// on struct values inside maps — including maps with composite keys.
func TestSchemaAt_MapProjection(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name:       "ArrayKeyMap",
				Kind:       gobspect.KindMap,
				TargetType: "map[[4]byte]Item",
			},
			{
				Name:       "NestedKeyMap",
				Kind:       gobspect.KindMap,
				TargetType: "map[map[string]int]Item",
			},
			{
				Name:       "StringKeyMap",
				Kind:       gobspect.KindMap,
				TargetType: "map[string]Item",
			},
			{
				Name:       "IntKeyMap",
				Kind:       gobspect.KindMap,
				TargetType: "map[int]Item",
			},
			{
				Name: "Item",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Name", Type: "string"},
					{Name: "Qty", Type: "int"},
					{Name: "Price", Type: "float"},
				},
			},
		},
	}

	t.Run("wildcard_then_project_array_key_map", func(t *testing.T) {
		p, err := query.Parse("*.Name,Qty")
		require.NoError(t, err)
		got, err := query.SchemaAt(schema, "ArrayKeyMap", p)
		require.NoError(t, err)
		assert.Equal(t, "struct", got)
	})

	t.Run("wildcard_then_project_nested_key_map", func(t *testing.T) {
		p, err := query.Parse("*.Name,Price")
		require.NoError(t, err)
		got, err := query.SchemaAt(schema, "NestedKeyMap", p)
		require.NoError(t, err)
		assert.Equal(t, "struct", got)
	})

	t.Run("wildcard_then_field_array_key_map", func(t *testing.T) {
		p, err := query.Parse("*.Name")
		require.NoError(t, err)
		got, err := query.SchemaAt(schema, "ArrayKeyMap", p)
		require.NoError(t, err)
		assert.Equal(t, "string", got)
	})

	t.Run("field_then_project_string_key_map", func(t *testing.T) {
		p, err := query.Parse("somekey.Name,Qty")
		require.NoError(t, err)
		got, err := query.SchemaAt(schema, "StringKeyMap", p)
		require.NoError(t, err)
		assert.Equal(t, "struct", got)
	})

	t.Run("field_then_project_int_key_map_rejected", func(t *testing.T) {
		p, err := query.Parse("somekey.Name,Qty")
		require.NoError(t, err)
		_, err = query.SchemaAt(schema, "IntKeyMap", p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only map[string]T is field-navigable")
	})
}
