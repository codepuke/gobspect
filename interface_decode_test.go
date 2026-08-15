package gobspect_test

import (
	"bytes"
	"encoding/gob"
	"io"
	"reflect"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gobIfaceName returns the fully-qualified type name that encoding/gob uses
// when writing a concrete value into an interface field.  Gob uses
// pkgPath + "." + typeName, which differs from reflect.Type.String().
func gobIfaceName(v any) string {
	rt := reflect.TypeOf(v)
	if pkg := rt.PkgPath(); pkg != "" {
		return pkg + "." + rt.Name()
	}
	return rt.Name()
}

var (
	houndGobName    = gobIfaceName(Hound{})
	felineGobName   = gobIfaceName(Feline{})
	labelledGobName = gobIfaceName(Labelled{})
)

// ── Types ────────────────────────────────────────────────────────────────────

// Creature is an interface whose concrete values are stored through gob interfaces.
type Creature any

type MyInt int

// Hound and Feline are two distinct concrete types used as interface values.
type Hound struct {
	Name  string
	Breed string
}

type Feline struct {
	Name   string
	Indoor bool
}

// Label is a sub-element of Labelled.  It is a new composite type that the
// decoder has never seen before when it first appears through an interface.
type Label struct {
	Key   string
	Value string
}

// Labelled is a composite concrete type: a struct containing a named slice of
// a new struct.  Encoding it through an interface exercises the continuation
// loop because the decoder must process multiple type-def outer messages
// (one each for Labelled, []Label, and Label) before it can read the positive
// concrete-type ID.
type Labelled struct {
	Tags  []Label
	Score int
}

// CreatureHolder wraps a single Creature interface field.
type CreatureHolder struct {
	Pet Creature
}

func init() {
	gob.Register(Hound{})
	gob.Register(Feline{})
	gob.Register(Labelled{})
	gob.Register(MyInt(0))
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// fieldByName returns the first field with the given name from sv.
func fieldByName(sv gobspect.StructValue, name string) (gobspect.Value, bool) {
	for _, f := range sv.Fields {
		if f.Name == name {
			return f.Value, true
		}
	}
	return nil, false
}

// gobUintEncode encodes v using gob's variable-length unsigned integer format.
// This mirrors decodeUint in wire.go so that splitGobMessages can parse the
// stream without importing internal symbols.
func gobUintEncode(v uint64) []byte {
	if v <= 0x7F {
		return []byte{byte(v)}
	}
	var tmp [8]byte
	n := 0
	for t := v; t > 0; t >>= 8 {
		tmp[7-n] = byte(t)
		n++
	}
	out := make([]byte, n+1)
	out[0] = byte(-n) // negated byte count
	copy(out[1:], tmp[8-n:])
	return out
}

// splitGobMessages splits a gob stream into individual message bodies
// (without their length prefixes).
func splitGobMessages(tb testing.TB, data []byte) [][]byte {
	tb.Helper()
	r := bytes.NewReader(data)
	var msgs [][]byte
	for r.Len() > 0 {
		b, err := r.ReadByte()
		require.NoError(tb, err, "reading length first byte")
		var n uint64
		if b <= 0x7F {
			n = uint64(b)
		} else {
			count := -int(int8(b))
			for range count {
				nb, err := r.ReadByte()
				require.NoError(tb, err, "reading length byte")
				n = n<<8 | uint64(nb)
			}
		}
		body := make([]byte, n)
		_, err = io.ReadFull(r, body)
		require.NoError(tb, err, "reading message body of length %d", n)
		msgs = append(msgs, body)
	}
	return msgs
}

// reassembleStream prepends a gob uint length to each message body and
// concatenates them into a complete stream.
func reassembleStream(msgs [][]byte) []byte {
	var buf bytes.Buffer
	for _, m := range msgs {
		buf.Write(gobUintEncode(uint64(len(m))))
		buf.Write(m)
	}
	return buf.Bytes()
}

// isTypeDefMsg returns true when the message body starts with a negative
// (zig-zag-encoded) type ID, which marks a type definition message.
func isTypeDefMsg(msg []byte) bool {
	if len(msg) == 0 {
		return false
	}
	b := msg[0]
	var u uint64
	if b <= 0x7F {
		u = uint64(b)
	} else {
		count := -int(int8(b))
		if len(msg) < 1+count {
			return false
		}
		for i := 1; i <= count; i++ {
			u = u<<8 | uint64(msg[i])
		}
	}
	return u&1 != 0 // zig-zag: odd ↔ negative ↔ type definition
}

// ── Integration tests (round-trip with encoding/gob) ─────────────────────────

// TestDecodeInterface_MultipleConcreteTypes encodes three interface values in a
// row — two distinct concrete types, with the first type repeated last — and
// verifies that each decodes to the correct concrete struct with the right field
// values.  The third value exercises the no-new-type-defs path (type already
// registered in the session).
func TestDecodeInterface_MultipleConcreteTypes(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Hound{Name: "Rex", Breed: "Shepherd"}}))
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Feline{Name: "Whiskers", Indoor: true}}))
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Hound{Name: "Spot", Breed: "Dalmatian"}}))

	ins := gobspect.New()
	vals, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 3)

	// vals[0] — Hound "Rex"
	sv0, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "CreatureHolder", sv0.TypeName)

	petRaw0, ok := fieldByName(sv0, "Pet")
	require.True(t, ok, "expected Pet field in vals[0]")
	iv0, ok := petRaw0.(gobspect.InterfaceValue)
	require.True(t, ok, "expected InterfaceValue for Pet in vals[0]")
	assert.Equal(t, houndGobName, iv0.TypeName)

	hound0, ok := iv0.Value.(gobspect.StructValue)
	require.True(t, ok, "expected StructValue inside InterfaceValue for vals[0]")
	nameVal0, ok := fieldByName(hound0, "Name")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "Rex"}, nameVal0)
	breedVal0, ok := fieldByName(hound0, "Breed")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "Shepherd"}, breedVal0)

	// vals[1] — Feline "Whiskers"
	sv1, ok := vals[1].(gobspect.StructValue)
	require.True(t, ok)
	petRaw1, ok := fieldByName(sv1, "Pet")
	require.True(t, ok)
	iv1, ok := petRaw1.(gobspect.InterfaceValue)
	require.True(t, ok)
	assert.Equal(t, felineGobName, iv1.TypeName)

	feline, ok := iv1.Value.(gobspect.StructValue)
	require.True(t, ok)
	nameVal1, ok := fieldByName(feline, "Name")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "Whiskers"}, nameVal1)
	indoorVal, ok := fieldByName(feline, "Indoor")
	require.True(t, ok)
	assert.Equal(t, gobspect.BoolValue{V: true}, indoorVal)

	// vals[2] — Hound "Spot" (type already registered, no new type-def messages)
	sv2, ok := vals[2].(gobspect.StructValue)
	require.True(t, ok)
	petRaw2, ok := fieldByName(sv2, "Pet")
	require.True(t, ok)
	iv2, ok := petRaw2.(gobspect.InterfaceValue)
	require.True(t, ok)
	assert.Equal(t, houndGobName, iv2.TypeName)

	hound2, ok := iv2.Value.(gobspect.StructValue)
	require.True(t, ok)
	nameVal2, ok := fieldByName(hound2, "Name")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "Spot"}, nameVal2)
}

// TestDecodeInterface_CompositeConcreteType encodes a struct that itself
// contains a slice of another new struct, all sent through an interface.
// This forces the continuation loop to consume multiple type-def outer
// messages (for Labelled, []Label, and Label) before it can read the positive
// concrete-type ID.
func TestDecodeInterface_CompositeConcreteType(t *testing.T) {
	val := CreatureHolder{
		Pet: Labelled{
			Tags:  []Label{{Key: "env", Value: "prod"}, {Key: "region", Value: "us-east"}},
			Score: 99,
		},
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(val))

	ins := gobspect.New()
	vals, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)

	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "CreatureHolder", sv.TypeName)

	petRaw, ok := fieldByName(sv, "Pet")
	require.True(t, ok, "expected Pet field")
	iv, ok := petRaw.(gobspect.InterfaceValue)
	require.True(t, ok, "expected InterfaceValue")
	assert.Equal(t, labelledGobName, iv.TypeName)

	labelled, ok := iv.Value.(gobspect.StructValue)
	require.True(t, ok, "expected StructValue inside InterfaceValue")
	assert.Equal(t, "Labelled", labelled.TypeName) // CommonType.Name is the short name

	tagsRaw, ok := fieldByName(labelled, "Tags")
	require.True(t, ok, "expected Tags field in Labelled")
	sliceVal, ok := tagsRaw.(gobspect.SliceValue)
	require.True(t, ok, "expected SliceValue for Tags")
	require.Len(t, sliceVal.Elems, 2)

	tag0, ok := sliceVal.Elems[0].(gobspect.StructValue)
	require.True(t, ok, "Tags[0] should be a StructValue")
	key0, ok := fieldByName(tag0, "Key")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "env"}, key0)
	value0, ok := fieldByName(tag0, "Value")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "prod"}, value0)

	tag1, ok := sliceVal.Elems[1].(gobspect.StructValue)
	require.True(t, ok, "Tags[1] should be a StructValue")
	key1, ok := fieldByName(tag1, "Key")
	require.True(t, ok)
	assert.Equal(t, gobspect.StringValue{V: "region"}, key1)

	scoreRaw, ok := fieldByName(labelled, "Score")
	require.True(t, ok, "expected Score field in Labelled")
	assert.Equal(t, gobspect.IntValue{V: 99}, scoreRaw)
}

// TestDecodeInterface_NilInterface verifies that a nil interface field is
// decoded without error.  Gob omits zero-valued fields, so the Pet field will
// not appear in the decoded StructValue.
func TestDecodeInterface_NilInterface(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(CreatureHolder{Pet: nil}))

	ins := gobspect.New()
	vals, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 1)

	sv, ok := vals[0].(gobspect.StructValue)
	require.True(t, ok)
	assert.Equal(t, "CreatureHolder", sv.TypeName)
	// nil interface → field omitted by encoder → no Pet field in decoded output
	_, hasPet := fieldByName(sv, "Pet")
	assert.False(t, hasPet, "nil interface field should be absent from decoded output")
}

// TestDecodeInterface_MixedSequence encodes four interface values in a row,
// mixing concrete types including: a composite type seen for the first time, a
// simple type, the composite type a second time (no new type defs), and a third
// simple type.  This exercises both the multi-type-def continuation path and the
// no-type-def-needed path within a single stream.
func TestDecodeInterface_MixedSequence(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	// 1. Labelled — first occurrence, decoder must process multiple type-def continuation messages
	require.NoError(t, enc.Encode(CreatureHolder{
		Pet: Labelled{Tags: []Label{{Key: "x", Value: "1"}}, Score: 10},
	}))
	// 2. Hound — simple struct, new type
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Hound{Name: "Buddy", Breed: "Lab"}}))
	// 3. Labelled again — types already registered, continuation has only positive typeID
	require.NoError(t, enc.Encode(CreatureHolder{
		Pet: Labelled{Tags: []Label{{Key: "y", Value: "2"}}, Score: 20},
	}))
	// 4. Feline — new type not yet seen
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Feline{Name: "Mittens", Indoor: false}}))

	ins := gobspect.New()
	vals, err := ins.Stream(&buf).Collect()
	require.NoError(t, err)
	require.Len(t, vals, 4, "expected 4 decoded values")

	assert.Equal(t, labelledGobName, concreteTypeName(vals[0]))
	assert.Equal(t, houndGobName, concreteTypeName(vals[1]))
	assert.Equal(t, labelledGobName, concreteTypeName(vals[2]))
	assert.Equal(t, felineGobName, concreteTypeName(vals[3]))

	// Verify field values for the second Labelled to ensure it was fully decoded.
	sv2, ok := vals[2].(gobspect.StructValue)
	require.True(t, ok)
	petRaw2, ok := fieldByName(sv2, "Pet")
	require.True(t, ok)
	iv2, ok := petRaw2.(gobspect.InterfaceValue)
	require.True(t, ok)
	labelled2, ok := iv2.Value.(gobspect.StructValue)
	require.True(t, ok)
	scoreRaw, ok := fieldByName(labelled2, "Score")
	require.True(t, ok)
	assert.Equal(t, gobspect.IntValue{V: 20}, scoreRaw)
}

// ── Stream manipulation test ──────────────────────────────────────────────────

// TestDecodeInterface_FrontLoadedTypeDefs takes an encoding/gob-produced stream,
// splits it into individual messages, separates type-def messages from value
// messages, and rebuilds a stream with all type defs front-loaded before all
// value messages.  This is unusual (gob normally interleaves type defs with
// values) but technically valid, since every type def still precedes any value
// that references it.  The test verifies that gobspect decodes the reordered
// stream to the same concrete type names as the original.
//
// This also exercises the splitGobMessages / reassembleStream helpers and the
// isTypeDefMsg classifier, acting as a canary for the message-boundary parsing
// logic used in the manual crafting below.
func TestDecodeInterface_FrontLoadedTypeDefs(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Hound{Name: "Alpha", Breed: "Husky"}}))
	require.NoError(t, enc.Encode(CreatureHolder{Pet: Feline{Name: "Beta", Indoor: true}}))

	original := buf.Bytes()
	msgs := splitGobMessages(t, original)

	// Baseline: original stream must decode without error.
	ins := gobspect.New()
	baseline, err := ins.Stream(bytes.NewReader(original)).Collect()
	require.NoError(t, err)
	require.Len(t, baseline, 2, "baseline: expected 2 decoded values")

	// Sanity-check the classifier: there should be at least one type-def message.
	var nTypeDefs, nValues int
	for _, m := range msgs {
		if isTypeDefMsg(m) {
			nTypeDefs++
		} else {
			nValues++
		}
	}
	assert.Positive(t, nTypeDefs, "expected at least one type-def message in the stream")
	assert.Positive(t, nValues, "expected at least one value message in the stream")

	// Round-trip through split → reassemble without reordering.
	// This verifies the helpers produce a bit-identical stream.
	rebuilt := reassembleStream(msgs)
	require.Equal(t, original, rebuilt, "reassembled stream should be bit-identical to original")

	// Front-load: all type defs first, then all values (preserving relative order
	// within each group).
	var typeDefs, values [][]byte
	for _, m := range msgs {
		if isTypeDefMsg(m) {
			typeDefs = append(typeDefs, m)
		} else {
			values = append(values, m)
		}
	}
	reordered := reassembleStream(append(typeDefs, values...))

	ins2 := gobspect.New()
	got, err := ins2.Stream(bytes.NewReader(reordered)).Collect()
	require.NoError(t, err, "front-loaded stream should decode without error")
	require.Len(t, got, len(baseline), "front-loaded stream should yield the same number of values")

	for i := range got {
		assert.Equal(t, concreteTypeName(baseline[i]), concreteTypeName(got[i]),
			"vals[%d]: concrete type name mismatch after reordering", i)
	}
}

// concreteTypeName navigates a decoded CreatureHolder value and returns the
// TypeName of the concrete value stored in the Pet interface field.
func concreteTypeName(v gobspect.Value) string {
	sv, ok := v.(gobspect.StructValue)
	if !ok {
		return "<not a struct>"
	}
	petRaw, ok := fieldByName(sv, "Pet")
	if !ok {
		return "<no Pet>"
	}
	iv, ok := petRaw.(gobspect.InterfaceValue)
	if !ok {
		return "<not an interface>"
	}
	return iv.TypeName
}

func TestDecodeInterface_MalformedZeroLengthValue(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(CreatureHolder{Pet: MyInt(42)}))

	original := buf.Bytes()
	msgs := splitGobMessages(t, original)

	lastMsg := msgs[len(msgs)-1]
	// The interface value carries its concrete type name inline, so the name
	// itself is a reliable anchor: concreteTypeID follows it, then valueLen.
	idx := bytes.Index(lastMsg, []byte("MyInt"))
	require.NotEqual(t, -1, idx, "MyInt type name not found in message")
	idx += 5 // skip "MyInt"

	require.Equal(t, byte(0x04), lastMsg[idx], "expected concreteTypeID to be 0x04")
	lastMsg[idx+1] = 0x00 // zero the valueLen

	reordered := reassembleStream(msgs)

	ins := gobspect.New()
	_, err := ins.Stream(bytes.NewReader(reordered)).Collect()
	require.Error(t, err)
	require.ErrorContains(t, err, "has zero-length value body")
}
