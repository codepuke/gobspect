package gobspect_test

import (
	"testing"

	gobspect "github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
)

func TestUnwrap(t *testing.T) {
	tests := []struct {
		name string
		in   gobspect.Value
		want gobspect.Value
	}{
		{
			name: "non-interface passes through",
			in:   gobspect.IntValue{V: 42},
			want: gobspect.IntValue{V: 42},
		},
		{
			name: "single layer",
			in:   gobspect.InterfaceValue{TypeName: "main.Miles", Value: gobspect.IntValue{V: 5}},
			want: gobspect.IntValue{V: 5},
		},
		{
			name: "nested layers all stripped",
			in: gobspect.InterfaceValue{
				TypeName: "outer",
				Value: gobspect.InterfaceValue{
					TypeName: "inner",
					Value:    gobspect.StringValue{V: "deep"},
				},
			},
			want: gobspect.StringValue{V: "deep"},
		},
		{
			name: "nil inner value normalises to NilValue",
			in:   gobspect.InterfaceValue{TypeName: "any"},
			want: gobspect.NilValue{},
		},
		{
			name: "nested with nil innermost normalises",
			in: gobspect.InterfaceValue{
				Value: gobspect.InterfaceValue{TypeName: "inner"},
			},
			want: gobspect.NilValue{},
		},
		{
			name: "explicit NilValue inner",
			in:   gobspect.InterfaceValue{Value: gobspect.NilValue{}},
			want: gobspect.NilValue{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gobspect.Unwrap(tt.in)
			assert.Equal(t, tt.want, got)
			_, isIface := got.(gobspect.InterfaceValue)
			assert.False(t, isIface, "Unwrap must never return an InterfaceValue")
		})
	}
}
