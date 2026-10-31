package agent

import (
	"errors"
	"reflect"
	"testing"
)

func TestDuplicateSchemaFieldRejected(t *testing.T) {

	// Build dynamically so go vet can still reject accidental duplicate tags
	// in real source. This deliberately malformed schema must fail.
	typeOf := reflect.StructOf([]reflect.StructField{
		{Name: "First", Type: reflect.TypeFor[string](), Tag: `json:"v"`},
		{Name: "Second", Type: reflect.TypeFor[string](), Tag: `json:"v"`},
	})

	_, _, err := buildSchema(typeOf, make(map[reflect.Type]bool), 0)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("duplicate schema accepted: %v", err)
	}
}
