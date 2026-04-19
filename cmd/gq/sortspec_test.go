package main

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeStruct(fields ...gobspect.Field) gobspect.Value {
	return gobspect.StructValue{Fields: fields}
}

func TestParseSortSpec(t *testing.T) {
	tests := []struct {
		name        string
		keysFlag    string
		desc        bool
		fold        bool
		dropMissing bool
		want        SortSpec
		wantErr     bool
	}{
		{
			name:     "single key",
			keysFlag: "Name",
			want:     SortSpec{Keys: []string{"Name"}},
		},
		{
			name:     "multiple keys with whitespace",
			keysFlag: " Name , Date ",
			want:     SortSpec{Keys: []string{"Name", "Date"}},
		},
		{
			name:     "empty flag",
			keysFlag: "",
			wantErr:  true,
		},
		{
			name:     "leading comma",
			keysFlag: ",Name",
			wantErr:  true,
		},
		{
			name:     "trailing comma",
			keysFlag: "Name,",
			wantErr:  true,
		},
		{
			name:        "desc fold dropMissing flags passed through",
			keysFlag:    "Name",
			desc:        true,
			fold:        true,
			dropMissing: true,
			want:        SortSpec{Keys: []string{"Name"}, Desc: true, Fold: true, DropMissing: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSortSpec(tc.keysFlag, tc.desc, tc.fold, tc.dropMissing)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSortSpecCompare(t *testing.T) {
	tests := []struct {
		name string
		spec SortSpec
		a    gobspect.Value
		b    gobspect.Value
		want int
	}{
		{
			name: "single key ascending",
			spec: SortSpec{Keys: []string{"Name"}},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Bob"}}),
			want: -1,
		},
		{
			name: "single key descending",
			spec: SortSpec{Keys: []string{"Name"}, Desc: true},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Bob"}}),
			want: 1,
		},
		{
			name: "multi-key tie on first resolved by second",
			spec: SortSpec{Keys: []string{"Status", "Date"}},
			a: makeStruct(
				gobspect.Field{Name: "Status", Value: gobspect.StringValue{V: "open"}},
				gobspect.Field{Name: "Date", Value: gobspect.StringValue{V: "2024-01"}},
			),
			b: makeStruct(
				gobspect.Field{Name: "Status", Value: gobspect.StringValue{V: "open"}},
				gobspect.Field{Name: "Date", Value: gobspect.StringValue{V: "2024-02"}},
			),
			want: -1,
		},
		{
			name: "no-fold B before a by byte value",
			spec: SortSpec{Keys: []string{"Name"}, Fold: false},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Banana"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "apple"}}),
			want: -1,
		},
		{
			name: "fold banana after apple",
			spec: SortSpec{Keys: []string{"Name"}, Fold: true},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Banana"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "apple"}}),
			want: 1,
		},
		{
			name: "missing field defaults to NilValue sorts first",
			spec: SortSpec{Keys: []string{"Name"}},
			a:    makeStruct(),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			want: -1,
		},
		{
			name: "DropMissing does not change Compare behavior",
			spec: SortSpec{Keys: []string{"Name"}, DropMissing: true},
			a:    makeStruct(),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			want: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.spec.Compare(tc.a, tc.b)
			assert.Equal(t, tc.want, got)
		})
	}
}
