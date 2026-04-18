package gobspect

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ToJSON serializes a Value as a discriminated-union JSON object. Every node
// carries a "kind" field with the concrete type name in lowercase snake_case.
// See the package documentation for the full field mapping per kind.
func ToJSON(v Value) ([]byte, error) {
	m, err := valueToJSONMap(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// ToJSONIndent is like [ToJSON] but applies indentation. prefix and indent
// follow the same semantics as [encoding/json.MarshalIndent].
func ToJSONIndent(v Value, prefix, indent string) ([]byte, error) {
	m, err := valueToJSONMap(v)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, prefix, indent)
}

// valueToJSONMap converts a Value to a map[string]any suitable for JSON encoding.
func valueToJSONMap(v Value) (map[string]any, error) {
	switch v := v.(type) {
	case BoolValue:
		return map[string]any{"kind": "bool", "v": v.V}, nil

	case IntValue:
		return map[string]any{"kind": "int", "v": v.V}, nil

	case UintValue:
		return map[string]any{"kind": "uint", "v": v.V}, nil

	case FloatValue:
		return map[string]any{"kind": "float", "v": v.V}, nil

	case ComplexValue:
		return map[string]any{"kind": "complex", "real": v.Real, "imag": v.Imag}, nil

	case StringValue:
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
		inner, err := valueToJSONMap(v.Value)
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
			"encoding": v.Encoding,
			"raw":      base64.StdEncoding.EncodeToString(v.Raw),
			"decoded":  normalizeOpaqueDecoded(v.Decoded),
		}, nil

	case StructValue:
		fields := make([]map[string]any, 0, len(v.Fields))
		for _, f := range v.Fields {
			fv, err := valueToJSONMap(f.Value)
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
			k, err := valueToJSONMap(e.Key)
			if err != nil {
				return nil, fmt.Errorf("map key: %w", err)
			}
			val, err := valueToJSONMap(e.Value)
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
		elems, err := valuesToJSONMaps(v.Elems)
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
		elems, err := valuesToJSONMaps(v.Elems)
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

func valuesToJSONMaps(vals []Value) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(vals))
	for _, v := range vals {
		m, err := valueToJSONMap(v)
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
	if d == nil {
		return nil
	}
	if _, err := json.Marshal(d); err != nil {
		return fmt.Sprint(d)
	}
	return d
}
