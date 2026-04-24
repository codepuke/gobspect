package sortval_test

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/sortval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeStruct(fields ...gobspect.Field) gobspect.Value {
	return gobspect.StructValue{Fields: fields}
}

func strStruct(name string) gobspect.Value {
	return gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: name}},
		},
	}
}

// — ParseSortSpec tests ——————————————————————————————————————————————————————

func TestParseSortSpec(t *testing.T) {
	tests := []struct {
		name        string
		keysFlag    string
		desc        bool
		fold        bool
		dropMissing bool
		want        sortval.SortSpec
		wantErr     bool
	}{
		{
			name:     "single key",
			keysFlag: "Name",
			want:     sortval.SortSpec{Keys: []string{"Name"}},
		},
		{
			name:     "multiple keys with whitespace",
			keysFlag: " Name , Date ",
			want:     sortval.SortSpec{Keys: []string{"Name", "Date"}},
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
			want:        sortval.SortSpec{Keys: []string{"Name"}, Desc: true, Fold: true, DropMissing: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sortval.ParseSortSpec(tc.keysFlag, tc.desc, tc.fold, tc.dropMissing)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// — SortSpec.Compare tests ——————————————————————————————————————————————————

func TestSortSpecCompare(t *testing.T) {
	tests := []struct {
		name string
		spec sortval.SortSpec
		a    gobspect.Value
		b    gobspect.Value
		want int
	}{
		{
			name: "single key ascending",
			spec: sortval.SortSpec{Keys: []string{"Name"}},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Bob"}}),
			want: -1,
		},
		{
			name: "single key descending",
			spec: sortval.SortSpec{Keys: []string{"Name"}, Desc: true},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Bob"}}),
			want: 1,
		},
		{
			name: "multi-key tie on first resolved by second",
			spec: sortval.SortSpec{Keys: []string{"Status", "Date"}},
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
			spec: sortval.SortSpec{Keys: []string{"Name"}, Fold: false},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Banana"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "apple"}}),
			want: -1,
		},
		{
			name: "fold banana after apple",
			spec: sortval.SortSpec{Keys: []string{"Name"}, Fold: true},
			a:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Banana"}}),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "apple"}}),
			want: 1,
		},
		{
			name: "missing field defaults to NilValue sorts first",
			spec: sortval.SortSpec{Keys: []string{"Name"}},
			a:    makeStruct(),
			b:    makeStruct(gobspect.Field{Name: "Name", Value: gobspect.StringValue{V: "Alice"}}),
			want: -1,
		},
		{
			name: "DropMissing does not change Compare behavior",
			spec: sortval.SortSpec{Keys: []string{"Name"}, DropMissing: true},
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

// — extractSortKey tests ——————————————————————————————————————————————————————

func TestExtractSortKey(t *testing.T) {
	theStruct := gobspect.StructValue{
		TypeName: "T",
		Fields: []gobspect.Field{
			{Name: "A", Value: gobspect.IntValue{V: 1}},
			{Name: "B", Value: gobspect.StringValue{V: "hello"}},
		},
	}

	tests := []struct {
		name      string
		v         gobspect.Value
		field     string
		wantValue gobspect.Value
		wantOK    bool
	}{
		{
			name:      "field present",
			v:         theStruct,
			field:     "B",
			wantValue: gobspect.StringValue{V: "hello"},
			wantOK:    true,
		},
		{
			name:      "field absent",
			v:         theStruct,
			field:     "C",
			wantValue: gobspect.NilValue{},
			wantOK:    false,
		},
		{
			name:      "InterfaceValue wrapping a struct",
			v:         gobspect.InterfaceValue{TypeName: "T", Value: theStruct},
			field:     "A",
			wantValue: gobspect.IntValue{V: 1},
			wantOK:    true,
		},
		{
			name:      "non-struct input",
			v:         gobspect.StringValue{V: "hello"},
			field:     "Name",
			wantValue: gobspect.NilValue{},
			wantOK:    false,
		},
		{
			name:      "NilValue input",
			v:         gobspect.NilValue{},
			field:     "X",
			wantValue: gobspect.NilValue{},
			wantOK:    false,
		},
		{
			name: "projected struct integration",
			v: gobspect.StructValue{
				Fields: []gobspect.Field{
					{Name: "Zip", Value: gobspect.StringValue{V: "10001"}},
				},
			},
			field:     "Zip",
			wantValue: gobspect.StringValue{V: "10001"},
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sortval.ExtractSortKey(tt.v, tt.field)
			assert.Equal(t, tt.wantValue, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

// — SortMatches tests —————————————————————————————————————————————————————————

func TestSortMatchesSingleKeyAscending(t *testing.T) {
	input := []gobspect.Value{
		strStruct("Charlie"),
		strStruct("Alice"),
		strStruct("Eve"),
		strStruct("Bob"),
		strStruct("Dave"),
	}
	spec := sortval.SortSpec{Keys: []string{"Name"}}

	got := sortval.SortMatches(sortval.SeqOf(input), spec)

	assert.Len(t, got, 5)
	assert.Equal(t, "Alice", got[0].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Bob", got[1].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Charlie", got[2].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Dave", got[3].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Eve", got[4].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
}

func TestSortMatchesSingleKeyDescending(t *testing.T) {
	input := []gobspect.Value{
		strStruct("Charlie"),
		strStruct("Alice"),
		strStruct("Eve"),
		strStruct("Bob"),
		strStruct("Dave"),
	}
	spec := sortval.SortSpec{Keys: []string{"Name"}, Desc: true}

	got := sortval.SortMatches(sortval.SeqOf(input), spec)

	assert.Len(t, got, 5)
	assert.Equal(t, "Eve", got[0].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Dave", got[1].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Charlie", got[2].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Bob", got[3].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Alice", got[4].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
}

func TestSortMatchesOffsetAndLimit(t *testing.T) {
	input := []gobspect.Value{
		strStruct("F"), strStruct("B"), strStruct("H"), strStruct("A"),
		strStruct("J"), strStruct("D"), strStruct("G"), strStruct("C"),
		strStruct("I"), strStruct("E"),
	}
	spec := sortval.SortSpec{Keys: []string{"Name"}}

	sorted := sortval.SortMatches(sortval.SeqOf(input), spec)
	sliced := sorted[2:5]

	assert.Equal(t, "C", sliced[0].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "D", sliced[1].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "E", sliced[2].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
}

func TestSortMatchesDropMissingTrue(t *testing.T) {
	withName := strStruct("Alice")
	withName2 := strStruct("Bob")
	withName3 := strStruct("Charlie")
	noName := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Age", Value: gobspect.IntValue{V: 30}},
		},
	}

	input := []gobspect.Value{withName, noName, withName2, withName3}
	spec := sortval.SortSpec{Keys: []string{"Name"}, DropMissing: true}

	got := sortval.SortMatches(sortval.SeqOf(input), spec)

	assert.Len(t, got, 3)
	for _, v := range got {
		sv := v.(gobspect.StructValue)
		found := false
		for _, f := range sv.Fields {
			if f.Name == "Name" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected row to have Name field")
	}
}

func TestSortMatchesDropMissingFalse(t *testing.T) {
	withName := strStruct("Alice")
	withName2 := strStruct("Bob")
	withName3 := strStruct("Charlie")
	noName := gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Age", Value: gobspect.IntValue{V: 30}},
		},
	}

	input := []gobspect.Value{withName, noName, withName2, withName3}
	spec := sortval.SortSpec{Keys: []string{"Name"}, DropMissing: false}

	got := sortval.SortMatches(sortval.SeqOf(input), spec)

	assert.Len(t, got, 4)
	sv := got[0].(gobspect.StructValue)
	hasName := false
	for _, f := range sv.Fields {
		if f.Name == "Name" {
			hasName = true
			break
		}
	}
	assert.False(t, hasName, "expected missing-key row to sort first")
}

func TestSortMatchesEmptyInput(t *testing.T) {
	spec := sortval.SortSpec{Keys: []string{"Name"}}
	got := sortval.SortMatches(sortval.SeqOf(nil), spec)
	assert.Empty(t, got)
}
