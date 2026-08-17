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
		{"ID.0", "", true}, // cannot index int
		// descent collects every reachable Items field; Order.Tags is a
		// map[string]string, so its value type "string" also enters the union
		// because any string key could in principle be "Items" at runtime.
		{"..Items", "[]LineItem|string", false},
		{"SKU,Price", "struct", false}, // projection returns anonymous struct
		{"Items.Price", "", true},      // cannot navigate field on slice
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

// TestSchemaAt_Descent verifies that recursive descent collects the union of
// reachable types and joins multiple results with "|" in sorted order.
func TestSchemaAt_Descent(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Order",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "ID", Type: "int"},
					{Name: "Customer", Type: "Person"},
					{Name: "Items", Type: "[]LineItem"},
				},
			},
			{
				Name: "Person",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Name", Type: "string"},
					{Name: "Address", Type: "Address"},
				},
			},
			{
				Name: "Address",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Street", Type: "string"},
					{Name: "City", Type: "string"},
				},
			},
			{
				Name: "LineItem",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "SKU", Type: "string"},
					{Name: "Price", Type: "float"},
					{Name: "Supplier", Type: "Person"}, // shares Person with Order.Customer
				},
			},
		},
	}

	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "descent_to_single_field_string",
			expr: "..Name",
			want: "string",
		},
		{
			name: "descent_to_field_with_multiple_reachable_struct_parents",
			// Person.Address points to Address, reachable via both Customer and Supplier.
			// Result: struct type "Address" (deduped).
			expr: "..Address",
			want: "Address",
		},
		{
			name: "descent_to_items_slice",
			expr: "..Items",
			want: "[]LineItem",
		},
		{
			name: "descent_then_index_produces_element",
			expr: "..Items.0",
			want: "LineItem",
		},
		{
			name: "descent_collects_heterogeneous_field_types",
			// "ID" exists only on Order; we descend from Order so it's still reachable.
			expr: "..ID",
			want: "int",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)
			got, err := query.SchemaAt(schema, "Order", p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSchemaAt_DescentUnion verifies the pipe-joined union form when two
// distinct field types with the same name are reachable.
func TestSchemaAt_DescentUnion(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Root",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "A", Type: "Alpha"},
					{Name: "B", Type: "Beta"},
				},
			},
			{
				Name: "Alpha",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Value", Type: "string"},
				},
			},
			{
				Name: "Beta",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Value", Type: "int"},
				},
			},
		},
	}
	p, err := query.Parse("..Value")
	require.NoError(t, err)
	got, err := query.SchemaAt(schema, "Root", p)
	require.NoError(t, err)
	assert.Equal(t, "int|string", got, "union should be sorted and pipe-joined")
}

// TestSchemaAt_DescentUnreachable verifies an error when no reachable type has
// the requested field.
func TestSchemaAt_DescentUnreachable(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Root",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "A", Type: "int"},
				},
			},
		},
	}
	p, err := query.Parse("..Missing")
	require.NoError(t, err)
	_, err = query.SchemaAt(schema, "Root", p)
	require.Error(t, err)
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
					{Name: "Items", Type: "[]int"},
				},
			},
			{
				Name:       "NamedSlice",
				Kind:       gobspect.KindSlice,
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
			name:     "named_map_int_key",
			rootType: "IntMap",
			expr:     "xyz",
			wantErr:  "cannot navigate field",
		},
		{
			name:     "inline_map_int_key",
			rootType: "Wrapper",
			expr:     "Data.xyz",
			wantErr:  "cannot navigate field",
		},
		{
			name:     "unnamed_slice_field",
			rootType: "Wrapper",
			expr:     "Items.Price",
			wantErr:  "use an index or wildcard first",
		},
		{
			name:     "named_slice_field",
			rootType: "NamedSlice",
			expr:     "SomeField",
			wantErr:  "use an index or wildcard first",
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
		assert.Contains(t, err.Error(), "cannot navigate field")
	})
}

// TestSchemaAt_WildcardDescent verifies that filters following a wildcard
// descend ("..[Filter]") act as type-preserving predicates: the result is the
// type of the nodes that could pass the filter, not their element type.
func TestSchemaAt_WildcardDescent(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Root",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Resources", Type: "[]Resource"},
				},
			},
			{
				Name: "Resource",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Status", Type: "string"},
					{Name: "Tags", Type: "[]string"},
				},
			},
		},
	}

	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{name: "filter_after_descend", expr: "..[Status=active]", want: "Resource"},
		{name: "wildcard_then_descend_filter", expr: "Resources.*..[Status=active]", want: "Resource"},
		{name: "chained_predicate_filters", expr: "..[Tags~devops][Status=active]", want: "Resource"},
		{name: "or_group_predicate", expr: "..[Status=active]|[Missing!]", want: "Resource"},
		{name: "predicate_then_field", expr: "..[Status=active].Status", want: "string"},
		{name: "numeric_predicate", expr: "..[Status==5]", want: "Resource"},
		{name: "not_exist_keeps_all_reachable", expr: "..[Nope!!]", want: "Resource|Root|[]Resource|[]string|string"},
		{name: "no_type_can_match", expr: "..[Nope=x]", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)
			got, err := query.SchemaAt(schema, "Root", p)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSchemaAt_WildcardDescentMapCandidate verifies that string-keyed map
// types survive a predicate filter (any runtime key could match the field
// name), while non-string-keyed maps do not.
func TestSchemaAt_WildcardDescentMapCandidate(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Root",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "Meta", Type: "map[string]string"},
					{Name: "Counts", Type: "map[int]int"},
				},
			},
		},
	}
	p, err := query.Parse("..[Whatever=x]")
	require.NoError(t, err)
	got, err := query.SchemaAt(schema, "Root", p)
	require.NoError(t, err)
	assert.Equal(t, "map[string]string", got)
}

// TestSchemaAt_FilterAsPredicate verifies a filter on a non-collection type
// mirrors the runtime's predicate fallback: the type is unchanged when the
// filter could match, and an error is returned when it cannot.
func TestSchemaAt_FilterAsPredicate(t *testing.T) {
	schema := &gobspect.Schema{
		Types: []gobspect.TypeDecl{
			{
				Name: "Order",
				Kind: gobspect.KindStruct,
				Fields: []gobspect.FieldDecl{
					{Name: "ID", Type: "int"},
				},
			},
		},
	}

	tests := []struct {
		name    string
		root    string
		expr    string
		want    string
		wantErr bool
	}{
		{name: "exist_on_struct", root: "Order", expr: "[ID!]", want: "Order"},
		{name: "predicate_then_field", root: "Order", expr: "[ID!].ID", want: "int"},
		{name: "numeric_on_struct", root: "Order", expr: "[ID==5]", want: "Order"},
		{name: "not_exist_on_scalar", root: "int", expr: "[X!!]", want: "int"},
		{name: "missing_field_errors", root: "Order", expr: "[Missing!]", wantErr: true},
		{name: "exist_on_scalar_errors", root: "int", expr: "[X!]", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := query.Parse(tt.expr)
			require.NoError(t, err)
			got, err := query.SchemaAt(schema, tt.root, p)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
