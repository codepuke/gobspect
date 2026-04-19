package main

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
)

func strStruct(name string) gobspect.Value {
	return gobspect.StructValue{
		Fields: []gobspect.Field{
			{Name: "Name", Value: gobspect.StringValue{V: name}},
		},
	}
}

func TestSortMatchesSingleKeyAscending(t *testing.T) {
	input := []gobspect.Value{
		strStruct("Charlie"),
		strStruct("Alice"),
		strStruct("Eve"),
		strStruct("Bob"),
		strStruct("Dave"),
	}
	spec := SortSpec{Keys: []string{"Name"}}

	got := sortMatches(seqOf(input), spec)

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
	spec := SortSpec{Keys: []string{"Name"}, Desc: true}

	got := sortMatches(seqOf(input), spec)

	assert.Len(t, got, 5)
	assert.Equal(t, "Eve", got[0].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Dave", got[1].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Charlie", got[2].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Bob", got[3].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
	assert.Equal(t, "Alice", got[4].(gobspect.StructValue).Fields[0].Value.(gobspect.StringValue).V)
}

func TestSortMatchesOffsetAndLimit(t *testing.T) {
	// 10 values A-J in shuffled order
	input := []gobspect.Value{
		strStruct("F"),
		strStruct("B"),
		strStruct("H"),
		strStruct("A"),
		strStruct("J"),
		strStruct("D"),
		strStruct("G"),
		strStruct("C"),
		strStruct("I"),
		strStruct("E"),
	}
	spec := SortSpec{Keys: []string{"Name"}}

	sorted := sortMatches(seqOf(input), spec)
	// Apply [2:5] slice to the sorted result
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
	spec := SortSpec{Keys: []string{"Name"}, DropMissing: true}

	got := sortMatches(seqOf(input), spec)

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
	spec := SortSpec{Keys: []string{"Name"}, DropMissing: false}

	got := sortMatches(seqOf(input), spec)

	assert.Len(t, got, 4)
	// Missing row sorts first (NilValue < StringValue)
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
	spec := SortSpec{Keys: []string{"Name"}}
	got := sortMatches(seqOf(nil), spec)
	assert.Empty(t, got)
}
