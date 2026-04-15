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
