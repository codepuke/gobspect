package query_test

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaAt_MisleadingErrors(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Order",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "ID", Type: "int"},
					{Name: "Items", Type: "[]LineItem"},
					{Name: "IntMap", Type: "map[int]string"},
				},
			},
			{
				Name: "LineItem",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Price", Type: "float"},
				},
			},
			{
				Name: "NamedSlice",
				Kind: gobspect.KindSlice,
				TargetType: "[]int",
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
			name:     "navigate_field_on_unnamed_slice",
			rootType: "Order",
			expr:     "Items.Price", // Items is []LineItem, can't navigate Price directly
			wantErr:  "use an index or wildcard first",
		},
		{
			name:     "navigate_field_on_named_slice",
			rootType: "NamedSlice",
			expr:     "SomeField",
			wantErr:  "use an index or wildcard first",
		},
		{
			name:     "navigate_field_on_non_string_map",
			rootType: "Order",
			expr:     "IntMap.SomeField",
			wantErr:  "cannot navigate field \"SomeField\" on map with non-string keys",
		},
		{
			name:     "missing_type_still_original_error",
			rootType: "Order",
			expr:     "ID.MissingField", // ID is int, int is not in schema.
			wantErr:  "type \"int\" not found or not a struct/map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)

			_, err = query.SchemaAt(schema, tt.rootType, p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr, "Error message should be helpful, got: %v", err)
		})
	}
}
