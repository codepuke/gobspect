package gobspect

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"
)

// JSONOption configures [ToJSON] and [ToJSONIndent].
type JSONOption func(*jsonConfig)

type jsonConfig struct {
	nonFiniteAsNull bool
}

// WithNonFiniteAsNull controls how non-finite floats (NaN, +Inf, -Inf) are
// serialized, including the real/imaginary parts of complex values. JSON has
// no representation for them as numbers, so by default they are emitted as
// the strings "NaN", "+Inf", and "-Inf". Pass true to emit JSON null instead,
// for consumers that expect the "v" field to never hold a string.
func WithNonFiniteAsNull(b bool) JSONOption {
	return func(c *jsonConfig) { c.nonFiniteAsNull = b }
}

// ToJSON serializes a Value as a discriminated-union JSON object. Every node
// carries a "kind" field with the concrete type name in lowercase snake_case.
// See the package documentation for the full field mapping per kind.
func ToJSON(v Value, opts ...JSONOption) ([]byte, error) {
	m, err := valueToJSONMap(v, newJSONConfig(opts))
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// ToJSONIndent is like [ToJSON] but applies indentation. prefix and indent
// follow the same semantics as [encoding/json.MarshalIndent].
func ToJSONIndent(v Value, prefix, indent string, opts ...JSONOption) ([]byte, error) {
	m, err := valueToJSONMap(v, newJSONConfig(opts))
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, prefix, indent)
}

func newJSONConfig(opts []JSONOption) *jsonConfig {
	cfg := &jsonConfig{}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// jsonFloat returns f itself when finite. encoding/json rejects NaN and ±Inf,
// which would fail the whole document over one leaf; non-finite values become
// the strings "NaN"/"+Inf"/"-Inf", or JSON null under [WithNonFiniteAsNull].
func jsonFloat(f float64, cfg *jsonConfig) any {
	switch {
	case !math.IsNaN(f) && !math.IsInf(f, 0):
		return f
	case cfg.nonFiniteAsNull:
		return nil
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	default:
		return "NaN"
	}
}

// valueToJSONMap converts a Value to a map[string]any suitable for JSON encoding.
func valueToJSONMap(v Value, cfg *jsonConfig) (map[string]any, error) {
	switch v := v.(type) {
	case BoolValue:
		return map[string]any{"kind": "bool", "v": v.V}, nil

	case IntValue:
		return map[string]any{"kind": "int", "v": v.V}, nil

	case UintValue:
		return map[string]any{"kind": "uint", "v": v.V}, nil

	case FloatValue:
		return map[string]any{"kind": "float", "v": jsonFloat(v.V, cfg)}, nil

	case ComplexValue:
		return map[string]any{"kind": "complex", "real": jsonFloat(v.Real, cfg), "imag": jsonFloat(v.Imag, cfg)}, nil

	case StringValue:
		// Gob strings are arbitrary byte sequences. encoding/json silently
		// replaces invalid UTF-8 with U+FFFD, so emit base64 instead — the
		// same treatment BytesValue gets — to keep the output lossless.
		if !utf8.ValidString(v.V) {
			return map[string]any{
				"kind":     "string",
				"v":        base64.StdEncoding.EncodeToString([]byte(v.V)),
				"encoding": "base64",
			}, nil
		}
		return map[string]any{"kind": "string", "v": v.V}, nil

	case BytesValue:
		return map[string]any{
			"kind":     "bytes",
			"v":        base64.StdEncoding.EncodeToString(v.V),
			"encoding": "base64",
		}, nil

	case NilValue:
		return map[string]any{"kind": "nil"}, nil

	case InterfaceValue:
		inner, err := valueToJSONMap(v.Value, cfg)
		if err != nil {
			return nil, fmt.Errorf("interface value: %w", err)
		}
		return map[string]any{
			"kind":     "interface",
			"typeName": v.TypeName,
			"value":    inner,
		}, nil

	case OpaqueValue:
		return map[string]any{
			"kind":     "opaque",
			"typeName": v.TypeName,
			"typeId":   v.GobTypeID,
			"encoding": v.Encoding,
			"raw":      base64.StdEncoding.EncodeToString(v.Raw),
			"decoded":  normalizeOpaqueDecoded(v.Decoded),
		}, nil

	case StructValue:
		fields := make([]map[string]any, 0, len(v.Fields))
		for _, f := range v.Fields {
			fv, err := valueToJSONMap(f.Value, cfg)
			if err != nil {
				return nil, fmt.Errorf("struct field %q: %w", f.Name, err)
			}
			fields = append(fields, map[string]any{"name": f.Name, "value": fv})
		}
		return map[string]any{
			"kind":     "struct",
			"typeName": v.TypeName,
			"typeId":   v.GobTypeID,
			"fields":   fields,
		}, nil

	case MapValue:
		entries := make([]map[string]any, 0, len(v.Entries))
		for _, e := range v.Entries {
			k, err := valueToJSONMap(e.Key, cfg)
			if err != nil {
				return nil, fmt.Errorf("map key: %w", err)
			}
			val, err := valueToJSONMap(e.Value, cfg)
			if err != nil {
				return nil, fmt.Errorf("map value: %w", err)
			}
			entries = append(entries, map[string]any{"key": k, "value": val})
		}
		return map[string]any{
			"kind":     "map",
			"typeName": v.TypeName,
			"typeId":   v.GobTypeID,
			"keyType":  v.KeyType,
			"elemType": v.ElemType,
			"entries":  entries,
		}, nil

	case SliceValue:
		elems, err := valuesToJSONMaps(v.Elems, cfg)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":     "slice",
			"typeName": v.TypeName,
			"typeId":   v.GobTypeID,
			"elemType": v.ElemType,
			"elems":    elems,
		}, nil

	case ArrayValue:
		elems, err := valuesToJSONMaps(v.Elems, cfg)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":     "array",
			"typeName": v.TypeName,
			"typeId":   v.GobTypeID,
			"elemType": v.ElemType,
			"len":      v.Len,
			"elems":    elems,
		}, nil

	default:
		return nil, fmt.Errorf("unknown Value type %T", v)
	}
}

func valuesToJSONMaps(vals []Value, cfg *jsonConfig) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(vals))
	for _, v := range vals {
		m, err := valueToJSONMap(v, cfg)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, nil
}

// normalizeOpaqueDecoded ensures d is a JSON-safe value. If d cannot be
// marshaled by encoding/json (e.g., a *big.Int that slipped past a decoder),
// it is converted to its fmt.Sprint string form.
func normalizeOpaqueDecoded(d any) any {
	switch v := d.(type) {
	case nil:
		return nil
	case string:
		// The overwhelmingly common decoder output; skip the probe marshal,
		// but keep base64 fallback for invalid UTF-8 as with StringValue.
		if !utf8.ValidString(v) {
			return base64.StdEncoding.EncodeToString([]byte(v))
		}
		return v
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return d
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Sprint(v)
		}
		return v
	}
	if _, err := json.Marshal(d); err != nil {
		return fmt.Sprint(d)
	}
	return d
}
