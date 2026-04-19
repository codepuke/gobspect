package main

import (
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
)

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
			got, ok := extractSortKey(tt.v, tt.field)
			assert.Equal(t, tt.wantValue, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}
