package agent_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agent "github.com/plutoframework/pluto-agent"
)

type schemaValue struct {
	Name    string    `json:"name"    enum:"a,b"`
	Enabled bool      `json:"enabled"`
	Count   int8      `json:"count"`
	Values  []float64 `json:"values"`
}

func TestSchemaIndependentContract(t *testing.T) {
	schema, err := agent.SchemaFor[schemaValue]()
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(schema.JSON(), &doc); err != nil {
		t.Fatal(err)
	}

	if doc["type"] != "object" || doc["additionalProperties"] != false {
		t.Fatalf("%s", schema.JSON())
	}

	properties := doc["properties"].(map[string]any)
	if len(doc["required"].([]any)) != 4 ||
		properties["count"].(map[string]any)["type"] != "integer" ||
		properties["values"].(map[string]any)["items"].(map[string]any)["type"] != "number" {
		t.Fatalf("unexpected schema: %s", schema.JSON())
	}

	copy := schema.JSON()
	copy[0] = '!'

	if schema.JSON()[0] != '{' {
		t.Fatal("mutable schema exposed")
	}
}

func TestStrictSchemaDecode(t *testing.T) {
	schema, err := agent.SchemaFor[schemaValue]()
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{
		`{"name":"a","enabled":true,"count":127,"values":[]}`,
		`{"name":"b","enabled":false,"count":-128,"values":[1.25,2e3]}`,
	} {
		var v schemaValue
		if err := schema.Decode([]byte(raw), 4096, &v); err != nil {
			t.Fatalf("rejected %s: %v", raw, err)
		}
	}

	for _, raw := range []string{
		`{}`, `null`, `[]`, `{"name":"a","enabled":true,"count":128,"values":[]}`,
		`{"name":"a","enabled":true,"count":-129,"values":[]}`,
		`{"name":"c","enabled":true,"count":1,"values":[]}`,
		`{"name":"a","enabled":"true","count":1,"values":[]}`,
		`{"name":"a","enabled":true,"count":"1","values":[]}`,
		`{"name":"a","enabled":true,"count":1.0,"values":[]}`,
		`{"name":"a","enabled":true,"count":1,"values":null}`,
		`{"name":"a","enabled":true,"count":1,"values":[null]}`,
		`{"name":"a","enabled":true,"count":1,"values":[1e400]}`,
		`{"name":"a","enabled":true,"count":1,"values":[],"extra":1}`,
		`{"Name":"a","enabled":true,"count":1,"values":[]}`,
		`{"name":"a","name":"b","enabled":true,"count":1,"values":[]}`,
		`{"name":"a","enabled":true,"count":1,"values":[]} {}`,
		`{"name":"a","enabled":true,"count":1,"values":[]} trailing`,
		`{"name":"a","enabled":true,"count":1,"values":[` + strings.Repeat("[", 40) + `1` + strings.Repeat("]", 40) + `]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var v schemaValue
			if err := schema.Decode(
				[]byte(raw),
				4096,
				&v,
			); !errors.Is(
				err,
				agent.ErrValidation,
			) {
				t.Fatalf("accepted invalid JSON %s: %v", raw, err)
			}
		})
	}

	var v schemaValue
	if err := schema.Decode(
		[]byte(`{"name":"a"}`),
		2,
		&v,
	); !errors.Is(
		err,
		agent.ErrValidation,
	) {
		t.Fatal("size not enforced")
	}

	if err := schema.Decode(
		[]byte("\xff"),
		4096,
		&v,
	); !errors.Is(
		err,
		agent.ErrValidation,
	) {
		t.Fatal("invalid UTF-8 accepted")
	}

	if err := schema.Decode(
		[]byte(`{}`),
		4096,
		&struct{}{},
	); !errors.Is(
		err,
		agent.ErrValidation,
	) {
		t.Fatal("wrong Go boundary accepted")
	}
}

type customJSON string

func (customJSON) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }

func unsupported[T any](t *testing.T) {
	t.Helper()

	if _, err := agent.SchemaFor[T](); !errors.Is(err, agent.ErrConfig) {
		t.Fatalf("unsupported type accepted: %v", err)
	}
}

func TestUnsupportedSchemaTypesFailAtConstruction(t *testing.T) {
	unsupported[string](t)
	unsupported[struct {
		V *int `json:"v"`
	}](t)
	unsupported[struct {
		V map[string]string `json:"v"`
	}](t)
	unsupported[struct {
		V any `json:"v"`
	}](t)
	unsupported[struct {
		V []byte `json:"v"`
	}](t)
	unsupported[struct {
		V time.Time `json:"v"`
	}](t)
	unsupported[struct {
		V customJSON `json:"v"`
	}](t)
	unsupported[struct{ V string }](t)
	unsupported[struct {
		V string `json:"v,omitempty"`
	}](t)
	unsupported[struct {
		V string `json:"-"`
	}](t)
	unsupported[struct {
		v string
	}](t)
	unsupported[struct {
		V int `json:"v" enum:"1,2"`
	}](t)
	unsupported[struct {
		V string `json:"v" enum:"a,a"`
	}](t)
	unsupported[struct {
		V [2]string `json:"v"`
	}](t)
}

func FuzzSchemaDecode(f *testing.F) {
	schema, err := agent.SchemaFor[schemaValue]()
	if err != nil {
		f.Fatal(err)
	}

	f.Add([]byte(`{"name":"a","enabled":true,"count":1,"values":[]}`))
	f.Add([]byte(`{"name":"a","name":"b"}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 8192 {
			t.Skip()
		}

		var v schemaValue
		if schema.Decode(raw, 4096, &v) == nil {
			if v.Name != "a" && v.Name != "b" {
				t.Fatal("enum bypass")
			}

			roundtrip, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}

			var back schemaValue
			if err := schema.Decode(roundtrip, 4096, &back); err != nil {
				t.Fatalf("noncanonical accepted value: %v", err)
			}
		}
	})
}
