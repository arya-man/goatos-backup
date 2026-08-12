package identityhttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestGetGoatPassportContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-contract")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["goat"] == nil || body["warnings"] == nil || body["trace_id"] != "req-contract" {
		t.Fatalf("unexpected contract shape: %#v", body)
	}
}

func TestSearchGoatsForwardsTableFilters(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=25&q=G-000001&goat_id=10000000-0000-4000-8000-000000000001&identifier_type=animal_identifier_1&scope_key=global&breed=Sojat&sex=male&farm_id=20000000-0000-4000-8000-000000000001&park_id=30000000-0000-4000-8000-000000000001&location_id=40000000-0000-4000-8000-000000000001&status=alive", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.searchParams == nil {
		t.Fatal("search params were not captured")
	}
	assertPtr := func(name string, got *string, want string) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s = %v, want %q", name, got, want)
		}
	}
	if repo.searchParams.Limit != 25 || repo.searchParams.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected limit/tenant params: %#v", repo.searchParams)
	}
	assertPtr("q", repo.searchParams.Query, "G-000001")
	assertPtr("goat_id", repo.searchParams.GoatID, "10000000-0000-4000-8000-000000000001")
	assertPtr("identifier_type", repo.searchParams.IdentifierType, "animal_identifier_1")
	assertPtr("scope_key", repo.searchParams.ScopeKey, "global")
	assertPtr("breed", repo.searchParams.Breed, "Sojat")
	assertPtr("sex", repo.searchParams.Sex, "male")
	assertPtr("farm_id", repo.searchParams.FarmID, "20000000-0000-4000-8000-000000000001")
	assertPtr("park_id", repo.searchParams.ParkID, "30000000-0000-4000-8000-000000000001")
	assertPtr("location_id", repo.searchParams.LocationID, "40000000-0000-4000-8000-000000000001")
	assertPtr("status", repo.searchParams.Status, "alive")
}

func TestSearchGoatsEnforcesGoatReadParkScope(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		parkA    = "30000000-0000-4000-8000-00000000000a"
		parkB    = "30000000-0000-4000-8000-00000000000b"
	)
	parkAGrant := permissions.ActiveGrant{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}

	request := func(repo *handlerRepo, rawQuery string, grants []permissions.ActiveGrant) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=20&"+rawQuery, nil)
		ctx := httpmiddleware.WithTenantID(req.Context(), tenantID)
		ctx = httpmiddleware.WithAuthGrants(ctx, grants)
		rec := httptest.NewRecorder()
		NewHandler(app.NewService(repo)).SearchGoats(rec, req.WithContext(ctx))
		return rec
	}

	t.Run("omitted park defaults to the operator's authorized park", func(t *testing.T) {
		repo := &handlerRepo{}
		rec := request(repo, "q=TAG-1", []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if repo.searchParams == nil || repo.searchParams.ParkID == nil || *repo.searchParams.ParkID != parkA {
			t.Fatalf("park_id=%v, want authorized park %q", repo.searchParams, parkA)
		}
	})

	t.Run("foreign park is forbidden before the repository read", func(t *testing.T) {
		repo := &handlerRepo{}
		rec := request(repo, "q=TAG-1&park_id="+parkB, []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
		if repo.searchParams != nil {
			t.Fatal("repository must not be queried for a forbidden park")
		}
	})

	t.Run("unrelated tenant grant does not bypass goat scope", func(t *testing.T) {
		repo := &handlerRepo{}
		grants := []permissions.ActiveGrant{
			parkAGrant,
			{Role: "unrelated_role", ScopeType: "tenant", ScopeID: tenantID},
		}
		rec := request(repo, "q=TAG-1&park_id="+parkB, grants)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("CEO can search any park", func(t *testing.T) {
		repo := &handlerRepo{}
		grants := []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenantID}}
		rec := request(repo, "q=TAG-1&park_id="+parkB, grants)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if repo.searchParams == nil || repo.searchParams.ParkID == nil || *repo.searchParams.ParkID != parkB {
			t.Fatalf("park_id=%v, want CEO-requested park %q", repo.searchParams, parkB)
		}
	})
}

// TestSearchGoatsRejectsUnknownQueryParameters is the regression for the most dangerous shape of
// this bug: ?query=... instead of ?q=... used to be silently DROPPED, so a search for a tag that
// matches nothing returned the first N animals of the herd, unfiltered, presented as search results.
// A caller cannot tell that answer apart from a real match. The repository must not be reached.
func TestSearchGoatsRejectsUnknownQueryParameters(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=3&query=NONSENSE-NO-SUCH-TAG", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search-unknown-param")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.searchParams != nil {
		t.Fatal("repository must not be queried when an unknown parameter is present: that is exactly how unfiltered herd data was returned as a search result")
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Code != "unknown_query_parameter" {
		t.Fatalf("code = %q, want unknown_query_parameter", envelope.Code)
	}
	if len(envelope.FieldErrors) != 1 || envelope.FieldErrors[0].Field != "query" {
		t.Fatalf("field errors = %#v, want a single error naming %q", envelope.FieldErrors, "query")
	}
}

// TestSearchGoatsUnknownParameterIsReportedBeforeMissingLimit pins the ordering. A request with a
// typo'd filter AND no limit must report the typo, not missing_limit, or the caller is sent looking
// in the wrong place.
func TestSearchGoatsUnknownParameterIsReportedBeforeMissingLimit(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?query=X", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search-unknown-before-limit")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Code != "unknown_query_parameter" {
		t.Fatalf("code = %q, want unknown_query_parameter", envelope.Code)
	}
}

// TestSearchGoatsReportsEveryUnknownParameterSorted keeps a hand-built query string to one
// round trip of fixes instead of one round trip per typo.
func TestSearchGoatsReportsEveryUnknownParameterSorted(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=5&zebra=1&alpha=2", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search-unknown-many")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if len(envelope.FieldErrors) != 2 ||
		envelope.FieldErrors[0].Field != "alpha" || envelope.FieldErrors[1].Field != "zebra" {
		t.Fatalf("field errors = %#v, want alpha then zebra", envelope.FieldErrors)
	}
}

// TestSearchGoatsAcceptsEveryContractParameter is the other half of the guard: the allow-list must
// not drift narrower than the documented contract. TestSearchGoatsForwardsTableFilters already
// sends all 12; this asserts none of them is rejected as unknown.
func TestSearchGoatsAcceptsEveryContractParameter(t *testing.T) {
	for _, name := range []string{
		"limit", "cursor", "q", "goat_id", "identifier_type", "scope_key",
		"breed", "sex", "farm_id", "park_id", "location_id", "status",
	} {
		if !searchGoatsAllowedParams[name] {
			t.Errorf("contract parameter %q is missing from searchGoatsAllowedParams", name)
		}
	}
	if len(searchGoatsAllowedParams) != 12 {
		t.Errorf("allow-list has %d entries, want 12: add the new parameter to the OpenAPI contract too", len(searchGoatsAllowedParams))
	}
}

func TestSearchGoatsRejectsInvalidUUIDFilters(t *testing.T) {
	repo := &handlerRepo{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=25&goat_id=not-a-uuid", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-search-invalid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.searchParams != nil {
		t.Fatal("repository should not be called for invalid goat_id")
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Code != "invalid_goat_id" {
		t.Fatalf("code = %q, want invalid_goat_id", envelope.Code)
	}
}

func TestMissingTenantReturnsErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("X-Request-ID", "req-missing-tenant")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_tenant_scope" || envelope.TraceID != "req-missing-tenant" || envelope.FieldErrors == nil {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestGetGoatTimelineContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001/timeline?limit=10", nil)
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-Request-ID", "req-timeline")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.GoatTimelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].EventType != "goat.created" || response.NextCursor == nil {
		t.Fatalf("unexpected timeline response: %#v", response)
	}
	if response.TraceID != "req-timeline" {
		t.Fatalf("unexpected trace id: %s", response.TraceID)
	}
}

func TestStageGoatContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	body := `{"management_stage":"weaner","reason":"stage confirmed by PC manager","evidence_refs":[{"evidence_type":"source_record","evidence_id":"stage-ticket-1"}],"row_version":4}`
	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/stage", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", "00000000-0000-4000-8000-000000000002")
	req.Header.Set("Idempotency-Key", "idem-stage-handler-0001")
	req.Header.Set("X-Request-ID", "req-stage")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.AdminGoatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Events) != 1 || response.Events[0].EventType != "goat.stage_changed" {
		t.Fatalf("unexpected stage response: %#v", response)
	}
	if response.Goat.ManagementStage == nil || *response.Goat.ManagementStage != "weaner" {
		t.Fatalf("management stage = %#v", response.Goat.ManagementStage)
	}
}

func TestHealthGoatContractShape(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	body := `{"health_status":"healthy","reason":"recovered after PC treatment","evidence_refs":[{"evidence_type":"source_record","evidence_id":"health-ticket-1"}],"row_version":5}`
	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/health", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", "00000000-0000-4000-8000-000000000002")
	req.Header.Set("Idempotency-Key", "idem-health-handler-0001")
	req.Header.Set("X-Request-ID", "req-health")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.AdminGoatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(response.Events) != 1 || response.Events[0].EventType != "goat.health.changed" {
		t.Fatalf("unexpected health response: %#v", response)
	}
	if response.Goat.HealthStatus == nil || *response.Goat.HealthStatus != "healthy" {
		t.Fatalf("health status = %#v", response.Goat.HealthStatus)
	}
}

func TestAddGoatIdentifierRequiresIdempotencyKey(t *testing.T) {
	rec := postAddGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "", validAddIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_idempotency_key" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAddGoatIdentifierRejectsInvalidActorAndUnknownFields(t *testing.T) {
	rec := postAddGoatIdentifier(t, "not-a-uuid", "idem-add-handler-0001", validAddIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_actor_id" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}

	body := `{"identifier_type":"animal_identifier_1","identifier_value":"A1-SYNTHETIC-001","scope_key":"global","evidence_ids":["synthetic-row-1"],"row_version":1}`
	rec = postAddGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "idem-add-handler-0002", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_json" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAddGoatIdentifierReplayReturnsAdminResponse(t *testing.T) {
	resultID := "30000000-0000-4000-8000-000000000001"
	rec := postAddGoatIdentifierWithRepo(t, &handlerRepo{
		addIdentifierResult: &ports.AdminGoatMutationResult{
			Goat:          handlerPassport().Summary,
			Identifiers:   []domain.GoatIdentifier{identifierResponseFixture(resultID, "animal_identifier_1", "A1-SYNTHETIC-001", "active")},
			Decision:      identifierDecisionFixture("50000000-0000-4000-8000-000000000101", "attach_identifier", "identifier_attached"),
			Events:        []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000101", EventType: "goat.identifier.added"}},
			Replayed:      true,
			FirstResultID: &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-add-handler-0003", validAddIdentifierBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.AdminGoatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.Decision.DecisionType != "attach_identifier" || len(response.Events) != 1 || !response.Idempotency.Replayed {
		t.Fatalf("unexpected admin response: %#v", response)
	}
}

func TestRetireGoatIdentifierRequiresIdempotencyKey(t *testing.T) {
	rec := postRetireGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "", validRetireIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_idempotency_key" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestRetireGoatIdentifierRejectsInvalidActorAndMissingEvidence(t *testing.T) {
	rec := postRetireGoatIdentifier(t, "not-a-uuid", "idem-retire-handler-0001", validRetireIdentifierBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "invalid_actor_id" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}

	rec = postRetireGoatIdentifier(t, "90000000-0000-4000-8000-000000000001", "idem-retire-handler-0002", `{"reason":"synthetic retire reason","row_version":2}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if envelope.Code != "missing_evidence_refs" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestRetireGoatIdentifierReplayReturnsAdminResponse(t *testing.T) {
	resultID := "30000000-0000-4000-8000-000000000001"
	rec := postRetireGoatIdentifierWithRepo(t, &handlerRepo{
		retireIdentifierResult: &ports.AdminGoatMutationResult{
			Goat:          handlerPassport().Summary,
			Identifiers:   []domain.GoatIdentifier{identifierResponseFixture(resultID, "animal_identifier_1", "A1-1900", "retired")},
			Decision:      identifierDecisionFixture("50000000-0000-4000-8000-000000000102", "retire_identifier", "identifier_retired"),
			Events:        []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000102", EventType: "goat.identifier.retired"}},
			Replayed:      true,
			FirstResultID: &resultID,
		},
	}, "90000000-0000-4000-8000-000000000001", "idem-retire-handler-0003", validRetireIdentifierBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response domain.AdminGoatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if response.Decision.DecisionType != "retire_identifier" || len(response.Events) != 1 || !response.Idempotency.Replayed {
		t.Fatalf("unexpected admin response: %#v", response)
	}
}

func postAddGoatIdentifier(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postAddGoatIdentifierWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postAddGoatIdentifierWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-add-identifier")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postRetireGoatIdentifier(t *testing.T, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postRetireGoatIdentifierWithRepo(t, &handlerRepo{}, actorID, idempotencyKey, body)
}

func postRetireGoatIdentifierWithRepo(t *testing.T, repo ports.Repository, actorID string, idempotencyKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(repo)))
	handler := httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers/30000000-0000-4000-8000-000000000001/retire", strings.NewReader(body))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	req.Header.Set("X-GoatOS-Actor-ID", actorID)
	req.Header.Set("X-Request-ID", "req-retire-identifier")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func validAddIdentifierBody() string {
	return `{"identifier_type":"animal_identifier_1","identifier_value":"A1-SYNTHETIC-001","scope_key":"global","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":1}`
}

func validRetireIdentifierBody() string {
	return `{"reason":"synthetic retire reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":2}`
}

func validIdentityGoatBody() string {
	return `{"dob":"2026-05-01","reason":"synthetic dob correction","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":1}`
}

type handlerRepo struct {
	addIdentifierResult    *ports.AdminGoatMutationResult
	retireIdentifierResult *ports.AdminGoatMutationResult
	searchParams           *ports.SearchGoatsParams
}

func (handlerRepo) GetGoatByID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (handlerRepo) GetGoatByDisplayID(context.Context, string, string) (*domain.GoatPassport, error) {
	return handlerPassport(), nil
}

func (r *handlerRepo) SearchGoats(_ context.Context, params ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	r.searchParams = &params
	return []domain.GoatSummary{handlerPassport().Summary}, nil, nil
}

func (handlerRepo) ListTemporaryTaggedGoats(context.Context, ports.ListTemporaryTaggedGoatsParams) ([]domain.TemporaryTaggedGoat, *string, error) {
	return nil, nil, nil
}

func (handlerRepo) FindIdentifierMatches(context.Context, ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	return nil, nil
}

func (handlerRepo) FindOpenConflictForIdentifier(context.Context, string, string, string, string) (*string, error) {
	return nil, nil
}

func (handlerRepo) GetGoatTimeline(context.Context, ports.GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error) {
	next := "eyJ2ZXJzaW9uIjoxLCJvY2N1cnJlZF9hdCI6IjIwMjYtMDYtMDhUMDA6MDA6MDBaIiwiZXZlbnRfaWQiOiI2MDAwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDEifQ"
	return []domain.GoatTimelineEvent{{
		EventID:      "60000000-0000-4000-8000-000000000001",
		EventType:    "goat.created",
		OccurredAt:   time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
		RecordedAt:   time.Date(2026, 6, 8, 0, 1, 0, 0, time.UTC),
		ActorType:    "system",
		EvidenceRefs: []domain.EvidenceRef{},
	}}, &next, nil
}

func (h handlerRepo) AddGoatIdentifier(_ context.Context, cmd ports.AddGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	if h.addIdentifierResult != nil {
		return h.addIdentifierResult, nil
	}
	return &ports.AdminGoatMutationResult{
		Goat:        handlerPassport().Summary,
		Identifiers: []domain.GoatIdentifier{identifierResponseFixture("30000000-0000-4000-8000-000000000001", cmd.IdentifierType, cmd.IdentifierValue, "active")},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000101", "attach_identifier", "identifier_attached"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000101", EventType: "goat.identifier.added"}},
	}, nil
}

func (h handlerRepo) RetireGoatIdentifier(_ context.Context, cmd ports.RetireGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	if h.retireIdentifierResult != nil {
		return h.retireIdentifierResult, nil
	}
	return &ports.AdminGoatMutationResult{
		Goat:        handlerPassport().Summary,
		Identifiers: []domain.GoatIdentifier{identifierResponseFixture(cmd.IdentifierID, "animal_identifier_1", "A1-1900", "retired")},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000102", "retire_identifier", "identifier_retired"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000102", EventType: "goat.identifier.retired"}},
	}, nil
}

func (h handlerRepo) PromoteTemporaryIdentifier(_ context.Context, cmd ports.PromoteTemporaryIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	return &ports.AdminGoatMutationResult{
		Goat:        handlerPassport().Summary,
		Identifiers: []domain.GoatIdentifier{identifierResponseFixture("70000000-0000-4000-8000-000000000103", "animal_identifier_1", cmd.PermanentValue, "active")},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000103", "attach_identifier", "identifier_attached"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000103", EventType: "goat.identifier.added"}},
	}, nil
}

func (h handlerRepo) MoveGoat(_ context.Context, cmd ports.MoveGoatCommand) (*ports.AdminGoatMutationResult, error) {
	return &ports.AdminGoatMutationResult{
		Goat:        handlerPassport().Summary,
		Identifiers: []domain.GoatIdentifier{},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000201", "move_goat", "goat_moved"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000201", EventType: "goat.location.changed"}},
	}, nil
}

func (h handlerRepo) ExitGoat(_ context.Context, cmd ports.ExitGoatCommand) (*ports.AdminGoatMutationResult, error) {
	goat := handlerPassport().Summary
	goat.LifecycleStatus = cmd.LifecycleStatus
	return &ports.AdminGoatMutationResult{
		Goat:        goat,
		Identifiers: []domain.GoatIdentifier{},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000202", "exit_goat", "goat_exited"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000202", EventType: "goat.exited"}},
	}, nil
}

func (h handlerRepo) StageGoat(_ context.Context, cmd ports.StageGoatCommand) (*ports.AdminGoatMutationResult, error) {
	goat := handlerPassport().Summary
	goat.ManagementStage = strPtr(cmd.ManagementStage)
	return &ports.AdminGoatMutationResult{
		Goat:        goat,
		Identifiers: []domain.GoatIdentifier{},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000203", "stage_goat", "goat_stage_changed"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000203", EventType: "goat.stage_changed"}},
	}, nil
}

func (h handlerRepo) HealthGoat(_ context.Context, cmd ports.HealthGoatCommand) (*ports.AdminGoatMutationResult, error) {
	goat := handlerPassport().Summary
	goat.HealthStatus = strPtr(cmd.HealthStatus)
	return &ports.AdminGoatMutationResult{
		Goat:        goat,
		Identifiers: []domain.GoatIdentifier{},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000204", "health_goat", "goat_health_changed"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000204", EventType: "goat.health.changed"}},
	}, nil
}

func (h handlerRepo) ReproductiveGoat(_ context.Context, cmd ports.ReproductiveGoatCommand) (*ports.AdminGoatMutationResult, error) {
	goat := handlerPassport().Summary
	return &ports.AdminGoatMutationResult{
		Goat:        goat,
		Identifiers: []domain.GoatIdentifier{},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000205", "reproductive_goat", "goat_reproductive_changed"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000205", EventType: "goat.reproductive.changed"}},
	}, nil
}

func (h handlerRepo) PreviewReclassifyShedStage(_ context.Context, cmd ports.ReclassifyShedStageCommand) (*ports.ReclassifyShedStagePreview, error) {
	return &ports.ReclassifyShedStagePreview{ShedID: cmd.ShedID, ManagementStage: cmd.ManagementStage, TotalLive: 1, Changing: 1}, nil
}

func (h handlerRepo) ReclassifyShedStage(_ context.Context, cmd ports.ReclassifyShedStageCommand) (*ports.ReclassifyShedStageResult, error) {
	return &ports.ReclassifyShedStageResult{ShedID: cmd.ShedID, ManagementStage: cmd.ManagementStage, TotalLive: 1, Reclassified: 1}, nil
}

func (h handlerRepo) IdentityGoat(_ context.Context, cmd ports.IdentityGoatCommand) (*ports.AdminGoatMutationResult, error) {
	goat := handlerPassport().Summary
	return &ports.AdminGoatMutationResult{
		Goat:        goat,
		Identifiers: []domain.GoatIdentifier{},
		Decision:    identifierDecisionFixture("50000000-0000-4000-8000-000000000206", "identity_goat", "goat_identity_changed"),
		Events:      []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000206", EventType: "goat.identity.changed"}},
	}, nil
}

func (handlerRepo) Ping(context.Context) error { return nil }

func strPtr(value string) *string {
	return &value
}

func identifierDecisionFixture(id, decisionType, result string) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     id,
		DecisionType:   decisionType,
		DecisionResult: result,
		DecisionState:  "approved",
		PolicyVersion:  "phase1-identifier-v1",
		CreatedAt:      time.Now().In(biztime.DefaultLocation()),
	}
}

func identifierResponseFixture(id, identifierType, value, status string) domain.GoatIdentifier {
	validFrom := time.Now().UTC().Add(-time.Hour)
	identifier := domain.GoatIdentifier{
		IdentifierID:     id,
		IdentifierType:   identifierType,
		IdentifierValue:  value,
		ScopeKey:         "global",
		Status:           status,
		IsPrimaryForGoat: false,
		ValidFrom:        validFrom,
	}
	if status == "retired" {
		validTo := time.Now().UTC()
		identifier.ValidTo = &validTo
	}
	return identifier
}

func handlerPassport() *domain.GoatPassport {
	return &domain.GoatPassport{
		GoatID:    "10000000-0000-4000-8000-000000000001",
		DisplayID: "G-000001",
		Species:   "goat",
		Summary: domain.GoatSummary{
			GoatID:            "10000000-0000-4000-8000-000000000001",
			DisplayID:         "G-000001",
			AnimalIdentifier1: strPtr("A1-G-000001"),
			AnimalIdentifier2: strPtr("A2-G-000001"),
			LifecycleStatus:   "alive",
			LocationPath:      domain.LocationPath{OperationalLocationDisplay: "Synthetic CBE"},
			Warnings:          []domain.Warning{},
		},
		Identifiers:  []domain.GoatIdentifier{},
		EvidenceRefs: []domain.EvidenceRef{},
		FamilyRefs:   []domain.FamilyRef{},
		RowVersion:   1,
	}
}
