package gobspect_test

// Tests that validate the code examples used in README.md.
// Each test corresponds to a snippet in the README.

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	gobspect "github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Example 1: Collect a struct and print formatted output ------------------

type readmePoint struct {
	X, Y int
}

func TestREADME_DecodeAndFormat(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(readmePoint{X: 3, Y: 7}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0])
	assert.Contains(t, out, "X")
	assert.Contains(t, out, "Y")
	assert.Contains(t, out, "3")
	assert.Contains(t, out, "7")
}

// --- Example 2: Inspect type definitions without decoding values -------------

func TestREADME_DecodeTypes(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(readmePoint{X: 1, Y: 2}))

	ins := gobspect.New()
	s := ins.Stream(&buf)
	_, err := s.Collect()
	require.NoError(t, err)
	types := s.Types()
	require.Len(t, types, 1)

	ti := types[0]
	assert.Equal(t, gobspect.KindStruct, ti.Kind)
	assert.Equal(t, "readmePoint", ti.Name)
	require.Len(t, ti.Fields, 2)
	assert.Equal(t, "X", ti.Fields[0].Name)
	assert.Equal(t, "Y", ti.Fields[1].Name)
}

// --- Example 3: Stream returns types and values together ----------------------

func TestREADME_Stream(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(readmePoint{X: 5, Y: 10}))
	require.NoError(t, enc.Encode(readmePoint{X: 0, Y: 0}))

	ins := gobspect.New()
	s := ins.Stream(&buf)
	values, err := s.Collect()
	require.NoError(t, err)

	assert.Len(t, values, 2)
	assert.NotEmpty(t, s.Types())
}

// --- Example 4: Multi-value stream ------------------------------------------

func TestREADME_MultiValueStream(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(readmePoint{1, 2}))
	require.NoError(t, enc.Encode(readmePoint{3, 4}))
	require.NoError(t, enc.Encode(readmePoint{5, 6}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	assert.Len(t, values, 3)
}

// --- Example 5: Built-in opaque decoders (time.Time and big.Int) ------------

func TestREADME_BuiltinOpaques(t *testing.T) {
	ts := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(ts))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0])
	// The formatted output should be a recognisable date string.
	assert.Contains(t, out, "2024-01-15")
}

func TestREADME_BigInt(t *testing.T) {
	n := big.NewInt(123456789)
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(n))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0])
	assert.Contains(t, out, "123456789")
}

// --- Example 6: Register a custom opaque decoder ----------------------------

type customToken struct{ payload []byte }

func (c customToken) GobEncode() ([]byte, error) { return c.payload, nil }
func (c *customToken) GobDecode(b []byte) error {
	c.payload = append([]byte(nil), b...)
	return nil
}

// Wrap in a struct with an interface field so the type name is transmitted.
type tokenHolder struct{ Token any }

func init() { gob.Register(customToken{}) }

func TestREADME_CustomOpaqueDecoder(t *testing.T) {
	holder := tokenHolder{Token: customToken{payload: []byte("secret")}}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(holder))

	ins := gobspect.New()
	// The key is the CommonType.Name from the gob wire format (short name).
	ins.RegisterDecoder("customToken", func(data []byte) (any, error) {
		return "decoded:" + string(data), nil
	})

	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0])
	assert.Contains(t, out, "decoded:secret")
}

// --- Example 7: Format options ----------------------------------------------

func TestREADME_FormatOptions(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(readmePoint{X: 1, Y: 2}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	// Custom indent.
	out := gobspect.Format(values[0], gobspect.WithIndent("\t"))
	assert.Contains(t, out, "\t")
}

// --- Example 8: JSON output -------------------------------------------------

func TestREADME_ToJSON(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(readmePoint{X: 3, Y: 7}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	b, err := gobspect.ToJSON(values[0])
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "struct", m["kind"])
}

func TestREADME_ToJSONIndent(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(readmePoint{X: 3, Y: 7}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	b, err := gobspect.ToJSONIndent(values[0], "", "  ")
	require.NoError(t, err)

	// Must be valid JSON with indentation.
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "struct", m["kind"])
	assert.Contains(t, string(b), "\n  ")

	// Fields must carry the encoded values.
	fields := m["fields"].([]any)
	require.Len(t, fields, 2)
	xField := fields[0].(map[string]any)
	assert.Equal(t, "X", xField["name"])
	xVal := xField["value"].(map[string]any)
	assert.Equal(t, "int", xVal["kind"])
	assert.Equal(t, float64(3), xVal["v"])
}

// --- Example 9: WithTimeFormat ----------------------------------------------

func TestREADME_WithTimeFormat(t *testing.T) {
	ts := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(ts))

	ins := gobspect.New(gobspect.WithTimeFormat("2006-01-02"))
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0])
	assert.Equal(t, "2024-06-15", out)
}

// --- Example 10: WithRedactKeys ---------------------------------------------

type readmeCredentials struct {
	Username string
	Password string
}

func TestREADME_WithRedactKeys(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(readmeCredentials{
		Username: "alice",
		Password: "super-secret",
	}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0],
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}, Char: '*'}))

	assert.Contains(t, out, "alice")
	assert.NotContains(t, out, "super-secret")
	assert.Contains(t, out, "Password: ")
}

// --- Example 11: WithRedactTypes -------------------------------------------

type readmeSensitive struct{ V string }
type readmeHolder struct{ Data readmeSensitive }

func TestREADME_WithRedactTypes(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(readmeHolder{Data: readmeSensitive{V: "topsecret"}}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0], gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"readmeSensitive"}}))
	assert.NotContains(t, out, "topsecret")
}

// --- Example 12: WithBytesFormat -------------------------------------------

func TestREADME_WithBytesFormat(t *testing.T) {
	type wrapBytes struct{ V []byte }
	var buf bytes.Buffer
	require.NoError(t, gob.NewEncoder(&buf).Encode(wrapBytes{V: []byte{0xde, 0xad}}))

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, values, 1)

	out := gobspect.Format(values[0], gobspect.WithBytesFormat(gobspect.BytesBase64))
	assert.Contains(t, out, "3q0=")
}

// ensure big is used (imported for TestREADME_BigInt)
var _ = big.NewInt
