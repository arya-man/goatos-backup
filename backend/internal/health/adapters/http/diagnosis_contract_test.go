package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"gopkg.in/yaml.v3"
)

// A schema that does not match the wire is worse than no schema: a client
// generated from it compiles, ships, and then reads fields the server never
// sends. These tests compare the DECLARED contract against what the handler
// actually emits, rather than trusting that the two were kept in step by hand.

const openAPIPath = "../../../../../contracts/openapi/app-api.yaml"

// The contract is parsed generically rather than into a typed mirror: a typed
// struct would need to model every shape the whole document uses (path-level
// parameter lists, $ref items, mixed response bodies) and would fail to load
// for reasons that have nothing to do with what these tests check.
type openAPIDoc struct {
	schemas map[string]any
	paths   map[string]any
}

func (d openAPIDoc) schemaProperties(t *testing.T, name string) map[string]any {
	t.Helper()
	schema, ok := d.schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("schema %s is not declared in the contract", name)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema %s declares no properties", name)
	}
	return props
}

// operationID returns the operationId declared for one method on one path, or ""
// when the path or method is absent.
func (d openAPIDoc) operationID(path, method string) string {
	item, ok := d.paths[path].(map[string]any)
	if !ok {
		return ""
	}
	op, ok := item[method].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := op["operationId"].(string)
	return id
}

func loadOpenAPI(t *testing.T) openAPIDoc {
	t.Helper()
	raw, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Skipf("openapi contract unavailable: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	paths, _ := doc["paths"].(map[string]any)
	if len(schemas) == 0 || len(paths) == 0 {
		t.Fatalf("openapi document has no schemas or paths")
	}
	return openAPIDoc{schemas: schemas, paths: paths}
}

// Every field the handler actually serialises must be declared. The schemas use
// additionalProperties:false, so an undeclared field makes the document a lie
// about its own strictness.
func TestResponseFieldsAreDeclaredInOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)

	cases := []struct {
		schema string
		value  any
	}{
		{"HealthDiagnosisProposalResponse", domain.SubmitObservationResult{}},
		{"ConfirmHealthDiagnosisResponse", domain.ConfirmDiagnosisResult{}},
		{"HealthDiagnosisRun", domain.DiagnosisRun{}},
		{"HealthConfirmableProblem", domain.ConfirmableProblem{}},
		{"HealthOpenedCase", domain.OpenedCase{}},
		{"HealthDiagnosisProposal", diagnosis.Proposal{}},
		{"HealthHousingDirective", diagnosis.Housing{}},
	}

	for _, tc := range cases {
		t.Run(tc.schema, func(t *testing.T) {
			props := doc.schemaProperties(t, tc.schema)
			encoded, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for field := range wire {
				if _, declared := props[field]; !declared {
					t.Errorf("handler emits %q but %s does not declare it", field, tc.schema)
				}
			}
		})
	}
}

// The form schema must accept every finding the engine reads. A finding the
// contract omits is one a client will never learn to send, and the engine will
// silently diagnose without it.
func TestEveryFindingIsDeclaredInOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	props := doc.schemaProperties(t, "HealthObservationFindings")

	// Marshalling the zero value omits nothing only if every field is non-omitempty;
	// Findings has no omitempty tags, so this yields the full field set.
	encoded, err := json.Marshal(diagnosis.Findings{})
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal findings: %v", err)
	}
	for field := range wire {
		if _, declared := props[field]; !declared {
			t.Errorf("the engine reads finding %q but the contract does not declare it", field)
		}
	}
}

// The follow-up context the contract declares must be one the server actually
// decodes -- and it must NOT declare `open`, which is resolved server-side from
// the animal's active courses. A client able to assert its own open problems
// could suppress a reconcile.
func TestObservationContextDoesNotAcceptClientOpenProblems(t *testing.T) {
	doc := loadOpenAPI(t)
	props := doc.schemaProperties(t, "HealthObservationContext")
	if _, declared := props["open"]; declared {
		t.Error("the contract must not invite a client to supply `open`: it is resolved server-side")
	}

	// And the server rejects it on the wire, not merely in prose.
	h := NewDiagnosisHandler(&fakeDiagnosisService{}, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/health/observations",
		strings.NewReader(`{"goat_id":"30000000-0000-4000-8000-000000000001","context":{"open":["FEVER"]}}`))
	req.Header.Set("Idempotency-Key", "obs-open")
	h.SubmitObservation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a client-supplied `open` must be rejected, got %d: %s", w.Code, w.Body.String())
	}
}

// The three routes are declared with the operation ids the rest of the stack
// expects, so a generated client exposes the names the app is written against.
func TestDiagnosisPathsAreDeclared(t *testing.T) {
	doc := loadOpenAPI(t)
	want := map[string]struct{ method, operationID string }{
		"/app/health/observations":                                   {"post", "submitHealthObservation"},
		"/app/health/observations/{health_diagnosis_run_id}":         {"get", "getHealthObservation"},
		"/app/health/observations/{health_diagnosis_run_id}/confirm": {"post", "confirmHealthDiagnosis"},
	}
	for path, expect := range want {
		if got := doc.operationID(path, expect.method); got != expect.operationID {
			t.Errorf("%s %s operationId = %q, want %q", expect.method, path, got, expect.operationID)
		}
	}
}
