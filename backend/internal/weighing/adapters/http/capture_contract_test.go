package http

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// THE WEIGH CAPTURES ARE AUTHORED (2026-09-16): the wire structs are checked field by field
// against the OpenAPI schemas they claim, both ways (the feeddirection analytics shape). A slot
// map the backend emits that no client can see, or a schema field nothing fills, is the kind of
// drift that compiles everywhere and shows up as a blank verifier drawer.
func TestWeighingCaptureDTOsMatchTheirOpenAPISchemas(t *testing.T) {
	for _, tc := range []struct {
		schema string
		dto    any
	}{
		{"WeighingObservation", domain.Observation{}},
		{"WeighingProofMedia", domain.ProofMedia{}},
		{"WeighingCountedProofSlot", domain.CountedProofSlot{}},
		{"WeighingRemovalProofSlot", domain.RemovalProofSlot{}},
		{"RecordWeighingAnimalObservationRequest", animalObservationRequest{}},
		{"RecordWeighingShedObservationRequest", shedObservationRequest{}},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			required, properties := weighingOpenAPISchemaFields(t, tc.schema)
			emitted := weighingEmittedJSONKeys(t, tc.dto)
			for _, field := range required {
				if !emitted[field] {
					t.Errorf("%s declares %q required, but %T never emits it", tc.schema, field, tc.dto)
				}
			}
			for field := range emitted {
				if !properties[field] {
					t.Errorf("%T emits %q, which %s does not declare -- no client can see it", tc.dto, field, tc.schema)
				}
			}
		})
	}
	// The two capture sections are DISTINCT in the contract: per-animal slots are the
	// required-flag shape, whole-pen slots the counted shape, and both are declared explicitly.
	_, ind := weighingOpenAPISchemaFields(t, "WeighingCaptureProofRefs")
	_, lump := weighingOpenAPISchemaFields(t, "WeighingCountedProofRefs")
	if len(ind) == 0 || len(lump) == 0 {
		t.Fatal("both capture ref shapes must be declared")
	}
}

func weighingEmittedJSONKeys(t *testing.T, dto any) map[string]bool {
	t.Helper()
	value := reflect.New(reflect.TypeOf(dto)).Elem()
	weighingFillNonZero(value)
	raw, err := json.Marshal(value.Interface())
	if err != nil {
		t.Fatalf("marshal %T: %v", dto, err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal %T: %v", dto, err)
	}
	keys := map[string]bool{}
	for key := range decoded {
		keys[key] = true
	}
	return keys
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func weighingFillNonZero(v reflect.Value) {
	if v.Type() == rawMessageType {
		v.SetBytes([]byte(`"x"`))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		weighingFillNonZero(s.Index(0))
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		val := reflect.New(v.Type().Elem()).Elem()
		weighingFillNonZero(val)
		m.SetMapIndex(reflect.ValueOf("x").Convert(v.Type().Key()), val)
		v.Set(m)
	case reflect.Ptr:
		p := reflect.New(v.Type().Elem())
		weighingFillNonZero(p.Elem())
		v.Set(p)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				weighingFillNonZero(v.Field(i))
			}
		}
	}
}

// weighingOpenAPISchemaFields scans one components.schemas entry as text (the backend carries
// no YAML dependency), returning its required list and property names.
func weighingOpenAPISchemaFields(t *testing.T, schema string) ([]string, map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile("../../../../../contracts/openapi/app-api.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if line == "    "+schema+":" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("schema %s not found in contracts/openapi/app-api.yaml", schema)
	}
	var required []string
	properties := map[string]bool{}
	section := ""
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "      ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "required:" && strings.HasPrefix(line, "      required:"):
			// The SCHEMA's required list sits at six spaces; a PROPERTY named `required`
			// (WeighingRemovalProofSlot.required) sits at eight and is a property.
			section = "required"
			continue
		case strings.HasPrefix(trimmed, "required: ["):
			for _, part := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(trimmed, "required: ["), "]"), ",") {
				if name := strings.TrimSpace(part); name != "" {
					required = append(required, name)
				}
			}
			section = ""
			continue
		case trimmed == "properties:":
			section = "properties"
			continue
		case trimmed == "additionalProperties:" || strings.HasPrefix(trimmed, "additionalProperties:"):
			if section == "required" {
				section = ""
			}
			// A map-shaped schema: its "properties" are open; record the marker so callers can
			// tell "declared" from "missing".
			if !strings.HasPrefix(trimmed, "additionalProperties: false") {
				properties["*"] = true
			}
		}
		switch section {
		case "required":
			if !strings.HasPrefix(trimmed, "- ") {
				section = ""
				continue
			}
			required = append(required, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		case "properties":
			if !strings.HasPrefix(line, "        ") || strings.HasPrefix(line, "         ") {
				continue
			}
			name := strings.TrimSuffix(trimmed, ":")
			if name != trimmed {
				properties[name] = true
			}
		}
	}
	if len(properties) == 0 {
		t.Fatalf("schema %s parsed with no properties -- the scanner has drifted from the spec's shape", schema)
	}
	return required, properties
}
