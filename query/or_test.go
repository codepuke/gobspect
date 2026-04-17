package query

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestORGroupSeedCorpus(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"[a=1]|[b=2]", false},
		{"[a=1]|[b=2]|[c=3]", false},
		{"Foo[a=1]|[b=2]", false},
		{"Foo[a=1]|[b=2].Bar", false},
		{"Foo[a=1]|[b=2].Bar[c=3]|[d=4]", false},
		{"[Name=a|b]", false},
		{"[Name=\"a|b\"]|[Other=x]", false},
		{"..[a=1]|[b=2]", false},
		{"Items.*[Status=active]|[Status=pending]", false},
		{"[a!]|[b!!]", false},
		{"[a==1]|[a>=2]", false},
		{"[a=1]|", true},
		{"|[a=1]", true},
		{"[a=1]||[b=2]", true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			p, err := Parse(tt.expr)
			if tt.wantErr {
				assert.Error(t, err, "expected error for %q", tt.expr)
				return
			}
			require.NoError(t, err, "unexpected error for %q", tt.expr)

			// Round-trip stability
			s1 := p.String()
			p2, err := Parse(s1)
			require.NoError(t, err, "re-parse failed for %q (s1=%q)", tt.expr, s1)
			
			// We use reflect.DeepEqual to check AST identity.
			// Note: if the parser or String() normalization changes the AST, this might fail
			// but for these cases it should be identical.
			if !reflect.DeepEqual(p, p2) {
				t.Errorf("AST drift for %q\nGOT:  %+v\nWANT: %+v", tt.expr, p2, p)
			}
			
			s2 := p2.String()
			assert.Equal(t, s1, s2, "String() drift for %q", tt.expr)
		})
	}
}

func TestORGroupInvariants(t *testing.T) {
	exprs := []string{
		"[a=1]|[b=2]",
		"Foo[a=1]|[b=2].Bar[c=3]|[d=4]",
		"..[a=1]|[b=2]",
	}
	for _, expr := range exprs {
		p, err := Parse(expr)
		require.NoError(t, err)
		assertORGroupInvariant(t, p)
	}
}
