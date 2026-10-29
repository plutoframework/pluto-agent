package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strings"
	"unicode/utf8"
)

const maxJSONDepth = 32

// Schema describes the deliberately small supported Go-type subset. All
// exported struct fields require explicit json names and are required/non-null.
// A string field may declare enum:"value1,value2" and description:"...".
type Schema struct {
	typeOf reflect.Type
	root   *schemaNode
	json   json.RawMessage
}

type schemaNode struct {
	kind   reflect.Kind
	fields map[string]*schemaNode
	item   *schemaNode
	enum   []string
	bits   int
}

func SchemaFor[T any]() (*Schema, error) {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: root must be a struct", ErrConfig)
	}

	node, doc, err := buildSchema(t, make(map[reflect.Type]bool), 0)
	if err != nil {
		return nil, err
	}

	raw, err := json.Marshal(doc)
	if err != nil || len(raw) > 64<<10 {
		return nil, fmt.Errorf("%w: schema too large", ErrConfig)
	}

	return &Schema{typeOf: t, root: node, json: raw}, nil
}

func (s *Schema) JSON() json.RawMessage {
	return bytes.Clone(s.json)
}

func buildSchema(
	t reflect.Type,
	seen map[reflect.Type]bool,
	depth int,
) (*schemaNode, map[string]any, error) {
	if depth > 16 || seen[t] || hasJSONMethods(t) {
		return nil, nil, fmt.Errorf(
			"%w: recursive, deep, or custom JSON type %s", ErrConfig, t,
		)
	}

	n := &schemaNode{kind: t.Kind(), bits: 64}
	doc := make(map[string]any)

	switch t.Kind() {
	case reflect.Struct:
		seen[t] = true
		defer delete(seen, t)

		n.fields = make(map[string]*schemaNode)
		props := make(map[string]any)

		required := make([]string, 0, t.NumField())
		for i := range t.NumField() {
			field := t.Field(i)

			name := field.Tag.Get("json")
			if field.Anonymous || !field.IsExported() || name == "" ||
				name == "-" || strings.Contains(name, ",") ||
				!utf8.ValidString(name) {
				return nil, nil, fmt.Errorf(
					"%w: field %s needs an explicit plain json name",
					ErrConfig, field.Name,
				)
			}

			if _, exists := n.fields[name]; exists {
				return nil, nil, fmt.Errorf("%w: duplicate field", ErrConfig)
			}

			child, prop, err := buildSchema(field.Type, seen, depth+1)
			if err != nil {
				return nil, nil, err
			}

			if enum, ok := field.Tag.Lookup("enum"); ok {
				if child.kind != reflect.String || enum == "" {
					return nil, nil, fmt.Errorf(
						"%w: string enum only",
						ErrConfig,
					)
				}

				child.enum = strings.Split(enum, ",")

				unique := make(map[string]bool)
				for _, value := range child.enum {
					if value == "" || unique[value] {
						return nil, nil, fmt.Errorf(
							"%w: invalid enum",
							ErrConfig,
						)
					}

					unique[value] = true
				}

				prop["enum"] = child.enum
			}

			if description := field.Tag.Get("description"); description != "" {
				prop["description"] = description
			}

			n.fields[name] = child
			props[name] = prop
			required = append(required, name)
		}

		doc["type"] = "object"
		doc["properties"] = props
		doc["required"] = required
		doc["additionalProperties"] = false
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil, nil, fmt.Errorf(
				"%w: byte slices unsupported",
				ErrConfig,
			)
		}

		child, item, err := buildSchema(t.Elem(), seen, depth+1)
		if err != nil {
			return nil, nil, err
		}

		n.item = child
		doc["type"] = "array"
		doc["items"] = item
	case reflect.String:
		doc["type"] = "string"
	case reflect.Bool:
		doc["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n.bits = t.Bits()
		doc["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		doc["type"] = "number"
	default:
		return nil, nil, fmt.Errorf("%w: unsupported type %s", ErrConfig, t)
	}

	return n, doc, nil
}

func hasJSONMethods(t reflect.Type) bool {
	marshal := reflect.TypeFor[json.Marshaler]()
	unmarshal := reflect.TypeFor[json.Unmarshaler]()
	textMarshal := reflect.TypeFor[interface{ MarshalText() ([]byte, error) }]()

	textUnmarshal := reflect.TypeFor[interface{ UnmarshalText([]byte) error }]()
	for _, candidate := range []reflect.Type{t, reflect.PointerTo(t)} {
		if candidate.Implements(marshal) || candidate.Implements(unmarshal) ||
			candidate.Implements(
				textMarshal,
			) || candidate.Implements(textUnmarshal) {
			return true
		}
	}

	return false
}

// Decode validates exact field names, required fields, nulls, types, enums,
// integer ranges, duplicate keys, depth, and size before decoding into dst.
func (s *Schema) Decode(raw []byte, maxBytes int, dst any) error {
	if s == nil || maxBytes <= 0 || len(raw) > maxBytes ||
		!utf8.Valid(raw) || reflect.TypeOf(dst) != reflect.PointerTo(s.typeOf) {
		return fmt.Errorf("%w: invalid decode boundary", ErrValidation)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	value, err := readJSON(decoder, 0)
	if err != nil {
		return fmt.Errorf("%w: invalid JSON", ErrValidation)
	}

	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON", ErrValidation)
	}

	if err := s.root.validate(value); err != nil {
		return err
	}

	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%w: value outside Go type range", ErrValidation)
	}

	return nil
}

func readJSON(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, ErrValidation
	}

	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}

	switch delim {
	case '{':
		object := make(map[string]any)

		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}

			name, ok := key.(string)
			if !ok {
				return nil, ErrValidation
			}

			if _, exists := object[name]; exists {
				return nil, ErrValidation
			}

			value, err := readJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}

			object[name] = value
		}

		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrValidation
		}

		return object, nil
	case '[':
		array := make([]any, 0)

		for decoder.More() {
			value, err := readJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}

			array = append(array, value)
		}

		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrValidation
		}

		return array, nil
	default:
		return nil, ErrValidation
	}
}

func (n *schemaNode) validate(value any) error {
	valid := false

	switch n.kind {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok || len(object) != len(n.fields) {
			break
		}

		for name, child := range n.fields {
			v, exists := object[name]
			if !exists || child.validate(v) != nil {
				return fmt.Errorf("%w: object field", ErrValidation)
			}
		}

		valid = true
	case reflect.Slice:
		array, ok := value.([]any)
		if !ok {
			break
		}

		for _, item := range array {
			if err := n.item.validate(item); err != nil {
				return err
			}
		}

		valid = true
	case reflect.String:
		v, ok := value.(string)

		valid = ok && len(n.enum) == 0
		for _, allowed := range n.enum {
			valid = valid || (ok && v == allowed)
		}
	case reflect.Bool:
		_, valid = value.(bool)
	case reflect.Float32, reflect.Float64:
		_, valid = value.(json.Number)
	default:
		number, ok := value.(json.Number)
		if !ok {
			break
		}

		integer, ok := new(big.Int).SetString(string(number), 10)
		if !ok {
			break
		}

		signed := n.kind >= reflect.Int && n.kind <= reflect.Int64
		if signed {
			valid = integer.IsInt64() &&
				integer.BitLen() < n.bits
			if integer.Sign() < 0 {
				minValue := new(big.Int).Lsh(big.NewInt(1), uint(n.bits-1))
				valid = integer.Cmp(minValue.Neg(minValue)) >= 0
			}
		} else {
			valid = integer.Sign() >= 0 && integer.BitLen() <= n.bits
		}
	}

	if !valid {
		return fmt.Errorf("%w: required type or enum", ErrValidation)
	}

	return nil
}
