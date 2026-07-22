package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	ceoobs "github.com/vgoats/goatos/backend/internal/ceoai/adapters/observability"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// newTraceRequest builds a request to /api/ceo-ai/admin/trace/{request_id} with
// a session-scoped actor context, exactly as the auth middleware would install.
func newTraceRequest(tenantID, actorID, requestID string, grants []permissions.ActiveGrant) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/ceo-ai/admin/trace/"+requestID, nil)
	r.SetPathValue("request_id", requestID)
	ctx := r.Context()
	if tenantID != "" {
		ctx = httpmiddleware.WithTenantID(ctx, tenantID)
	}
	if actorID != "" {
		ctx = httpmiddleware.WithActorID(ctx, actorID)
	}
	ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	return r.WithContext(ctx)
}

func adminGrants() []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant"}}
}

func nonAdminGrants() []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "shed", ScopeID: "s1"}}
}

func seededStore(t *testing.T, tenantID, requestID string) *ceoobs.MemoryTraceStore {
	t.Helper()
	store := ceoobs.NewMemoryTraceStore(8)
	if err := store.RecordTrace(context.Background(), ceoobs.TraceRecord{
		TenantID:         tenantID,
		RequestID:        requestID,
		ActorRole:        permissions.RoleCEOInternal,
		RouteTier:        "cube",
		ToolCalled:       "vaccination_overdue",
		QuestionRedacted: "which sheds overdue? rfid=982000123456789",
		Steps: []ceoobs.StepTrace{
			{SubQuestion: "overdue by shed", Route: "cube", ToolName: "vaccination_overdue", RowCount: 3, DurationMS: 12},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return store
}

func TestAdminTrace_NonAdminForbidden(t *testing.T) {
	h := NewAdminTraceHandler(seededStore(t, "tA", "req-1"), slog.Default())
	rec := httptest.NewRecorder()
	// Non-admin, even with a real request id in their own tenant, must get 403 —
	// and must NOT be able to distinguish 403 from 404 (no existence probe).
	h.Trace(rec, newTraceRequest("tA", "user-op", "req-1", nonAdminGrants()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin want 403, got %d", rec.Code)
	}
}

func TestAdminTrace_UnauthenticatedRejected(t *testing.T) {
	h := NewAdminTraceHandler(seededStore(t, "tA", "req-1"), slog.Default())
	rec := httptest.NewRecorder()
	h.Trace(rec, newTraceRequest("", "", "req-1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated want 401, got %d", rec.Code)
	}
}

func TestAdminTrace_AdminGetsTrace(t *testing.T) {
	h := NewAdminTraceHandler(seededStore(t, "tA", "req-1"), slog.Default())
	rec := httptest.NewRecorder()
	h.Trace(rec, newTraceRequest("tA", "user-ceo", "req-1", adminGrants()))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got ceoobs.TraceRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RouteTier != "cube" || got.ToolCalled != "vaccination_overdue" {
		t.Fatalf("unexpected trace body: %+v", got)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(got.Steps))
	}
	// Goat RFID must survive (not PII); tenant id must never serialize.
	if got.TenantID != "" {
		t.Fatalf("tenant_id must not be serialized to client, got %q", got.TenantID)
	}
	if ceoobs.HasSecretLeak(got) {
		t.Fatalf("trace body leaked a secret: %+v", got)
	}
}

func TestAdminTrace_AdminCrossTenantNotFound(t *testing.T) {
	h := NewAdminTraceHandler(seededStore(t, "tA", "req-1"), slog.Default())
	rec := httptest.NewRecorder()
	// Admin in tenant B may not read tenant A's trace.
	h.Trace(rec, newTraceRequest("tB", "user-ceo", "req-1", adminGrants()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant want 404, got %d", rec.Code)
	}
}

func TestAdminTrace_MissingRequestID(t *testing.T) {
	h := NewAdminTraceHandler(seededStore(t, "tA", "req-1"), slog.Default())
	rec := httptest.NewRecorder()
	h.Trace(rec, newTraceRequest("tA", "user-ceo", "", adminGrants()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty request_id want 400, got %d", rec.Code)
	}
}
