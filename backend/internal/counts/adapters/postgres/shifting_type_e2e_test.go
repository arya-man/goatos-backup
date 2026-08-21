package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countshttp "github.com/vgoats/goatos/backend/internal/counts/adapters/http"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// TYPED SHIFTING, END TO END ON THE PRODUCTION PATH (maintainer decisions 2026-08-20,
// docs/features/shifting/shifting-rewrite-tag-rules.md).
//
// The sibling suites each prove one half against a seam: the rulebook tests drive the real HTTP
// handler over a FAKE repository, and the apply tests drive the real database from a HAND-SEEDED
// event row. Neither proves the JOINT, which is where the rewrite can actually be wrong: does the
// tag decision the rulebook makes at raise survive into the stored row, through the park head's
// approval, and come back out of the apply transaction as the thing that is really written to the
// animal and the pen?
//
// So nothing derived is seeded here. The only seeded facts are inputs a farm genuinely has before
// a movement exists (tenant, park, sheds, animals, stage vocabulary). The event row, its category,
// its target stage, its adopt_pen_tag, the approval request, the completion and every write the
// apply performs are produced by the same handler and services the API composes in
// bootstrap/api.go.

// typedE2EStack composes the counts write handler exactly as bootstrap/api.go does -- real
// repository (carrying the identity transaction seam the relocation runs through), real service,
// real approval workflow -- so a raise here travels the production path.
func typedE2EStack(t *testing.T, pool *pgxpool.Pool) (*http.ServeMux, *Repository) {
	t.Helper()
	repo := newRealIdentityApprovalRepo(t, pool)
	mux := http.NewServeMux()
	handler := countshttp.NewAppWriteHandler(countsapp.NewService(repo), nil).
		WithApprovalWorkflow(countsapp.NewApprovalService(repo, nil, nil), nil)
	countshttp.RegisterAppWrites(mux, handler)
	return mux, repo
}

// raiseTypedShifting POSTs a typed raise through the real route and returns the recorder.
func raiseTypedShifting(
	t *testing.T, mux *http.ServeMux, idempotencyKey, category string, goatIDs []string,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"destination_park_id": countsPark,
		"destination_shed_id": countsShedB,
		"category":            category,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal raise body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/app/counts/shifting-events", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", idempotencyKey)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), countsTenant), countsOperator)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// storedShiftingSnapshot reads back the three columns the rulebook is responsible for writing at
// raise -- the row the park head approves and the apply transaction later re-reads under its lock.
func storedShiftingSnapshot(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, shiftingEventID string,
) (category, targetStage, adoptPenTag string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(category, ''), COALESCE(target_management_stage, ''), COALESCE(adopt_pen_tag, '')
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		countsTenant, shiftingEventID).Scan(&category, &targetStage, &adoptPenTag); err != nil {
		t.Fatalf("read stored shifting snapshot: %v", err)
	}
	return category, targetStage, adoptPenTag
}

// pendingApprovalForShifting finds the approval request the raise created, so the E2E approves the
// real request rather than fabricating one.
func pendingApprovalForShifting(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, shiftingEventID string,
) string {
	t.Helper()
	var approvalRequestID string
	if err := pool.QueryRow(ctx, `
SELECT approval_request_id::text
FROM counts_approval_requests
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND status = 'pending'`,
		countsTenant, shiftingEventID).Scan(&approvalRequestID); err != nil {
		t.Fatalf("read pending approval request: %v", err)
	}
	return approvalRequestID
}

// A SPACING raise, end to end. The farm facts are: one animal tagged K2 standing alone in its pen,
// and an empty destination nobody has configured. Nothing tells the system what tag to carry or
// what to do with the destination -- the RULEBOOK derives both at raise ("the tag travels; an empty
// destination adopts it"), and this test follows that derivation all the way to the animal's row
// and the pen's configuration.
func TestTypedSpacingEndToEndFromRaiseToAppliedPenTag(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	goatID := "00000000-0000-4000-8000-00000000e2e1"
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K2")
	seedStageVocabulary(t, ctx, pool, "K2")
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "" {
		t.Fatalf("destination pre-configured %q, want an unconfigured empty pen for this scenario", got)
	}

	res := raiseTypedShifting(t, mux, "e2e-spacing", domain.ShiftTypeSpacing, []string{goatID})
	if res.Code != http.StatusOK {
		t.Fatalf("raise status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	var raised struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &raised); err != nil {
		t.Fatalf("decode raise response %q: %v", res.Body.String(), err)
	}
	if raised.ShiftingEventID == "" {
		t.Fatalf("raise response carried no shifting_event_id: %s", res.Body.String())
	}

	// THE JOINT, HALF ONE: what the rulebook decided is what the database stores. The tag travels
	// (no target stage restamps the animal) and the empty destination is marked to adopt K2 -- and
	// K2 was never sent by the client, it was derived from the animal the raise names.
	category, targetStage, adoptPenTag := storedShiftingSnapshot(t, ctx, pool, raised.ShiftingEventID)
	if category != domain.ShiftTypeSpacing {
		t.Fatalf("stored category=%q, want spacing", category)
	}
	if targetStage != "" {
		t.Fatalf("stored target stage=%q, want empty -- spacing never restamps the animals", targetStage)
	}
	if adoptPenTag != "K2" {
		t.Fatalf("stored adopt_pen_tag=%q, want K2 derived from the moving group", adoptPenTag)
	}

	// The park head approves the real request the raise created. Approval moves NOTHING.
	approvalRequestID := pendingApprovalForShifting(t, ctx, pool, raised.ShiftingEventID)
	if _, _, err := approveShifting(
		repo, ctx, "e2e-spacing", approvalRequestID, raised.ShiftingEventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat shed=%s after approval alone, want still at source %s", got, countsShedA)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "" {
		t.Fatalf("destination configured %q after approval alone, want unconfigured until apply", got)
	}

	// The operator completes with a video; the second gate applies everything atomically.
	completed, _, err := submitShiftingForVerification(repo, ctx, "e2e-spacing", raised.ShiftingEventID, "")
	if err != nil {
		t.Fatalf("operator completion: %v", err)
	}
	if completed.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("event status=%q after completion, want applied", completed.EventStatus)
	}

	// THE JOINT, HALF TWO: the decision made at raise is what actually landed on the farm's rows.
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s, want destination %s", got, countsShedB)
	}
	if got := goatStage(t, ctx, pool, goatID); got != "K2" {
		t.Fatalf("goat stage=%q, want K2 carried unchanged through a spacing move", got)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "K2" {
		t.Fatalf("destination configured stage=%q, want the K2 the raise promised the approver", got)
	}
}

// A HEALTH raise into a pen configured for ICU, end to end. Health is the ONE type allowed to stamp
// a clinical state, and the permission travels as the stored CATEGORY: the client never says
// "clinical", it says "health", and identity's clinical refusal is lifted at apply from the row.
func TestTypedHealthEndToEndStampsClinicalStateFromTheStoredCategory(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, repo := typedE2EStack(t, pool)

	goatID := "00000000-0000-4000-8000-00000000e2e2"
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K1")
	seedStageVocabulary(t, ctx, pool, "K1")
	seedStageVocabulary(t, ctx, pool, "ICU")
	// The destination is the sick bay: a pen somebody configured for ICU.
	seedShedProfile(t, ctx, pool, countsShedB, "ICU")

	res := raiseTypedShifting(t, mux, "e2e-health", domain.ShiftTypeHealth, []string{goatID})
	if res.Code != http.StatusOK {
		t.Fatalf("raise status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	var raised struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &raised); err != nil {
		t.Fatalf("decode raise response %q: %v", res.Body.String(), err)
	}

	category, targetStage, adoptPenTag := storedShiftingSnapshot(t, ctx, pool, raised.ShiftingEventID)
	if category != domain.ShiftTypeHealth || targetStage != "ICU" {
		t.Fatalf("stored category=%q target=%q, want health/ICU", category, targetStage)
	}
	if adoptPenTag != "" {
		t.Fatalf("stored adopt_pen_tag=%q, want empty -- health never re-tags the pen", adoptPenTag)
	}

	approvalRequestID := pendingApprovalForShifting(t, ctx, pool, raised.ShiftingEventID)
	if _, _, err := approveShifting(
		repo, ctx, "e2e-health", approvalRequestID, raised.ShiftingEventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	if _, _, err := submitShiftingForVerification(repo, ctx, "e2e-health", raised.ShiftingEventID, ""); err != nil {
		t.Fatalf("operator completion: %v", err)
	}

	if got := goatStage(t, ctx, pool, goatID); got != "ICU" {
		t.Fatalf("goat stage=%q, want ICU -- a health movement IS the clinical call", got)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s, want the sick bay %s", got, countsShedB)
	}
}

// A GROWTH raise that runs BACKWARD down the ladder is refused at RAISE time, and the refusal is
// the whole point of the rewrite: it happens before the park head is asked and before the operator
// shoots a video. Nothing at all is written -- no movement row, no approval request -- and the
// operator is handed farm-worded copy rather than a code.
func TestTypedGrowthBackwardRaiseIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	mux, _ := typedE2EStack(t, pool)

	goatID := "00000000-0000-4000-8000-00000000e2e3"
	// The animal is K3; the destination pen is configured K1, which is BACKWARD.
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K3")
	seedStageVocabulary(t, ctx, pool, "K1")
	seedStageVocabulary(t, ctx, pool, "K3")
	seedShedProfile(t, ctx, pool, countsShedB, "K1")

	before := time.Now()
	res := raiseTypedShifting(t, mux, "e2e-growth-back", domain.ShiftTypeGrowth, []string{goatID})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("raise status=%d body=%s, want 400", res.Code, res.Body.String())
	}
	var failure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode refusal %q: %v", res.Body.String(), err)
	}
	if failure.Code != "growth_not_next_stage" {
		t.Fatalf("refusal code=%q, want growth_not_next_stage (body %s)", failure.Code, res.Body.String())
	}
	// Backend-owned FARM copy, rendered verbatim by the phone -- never an internal token.
	if failure.Message != "This destination's tag is not the next stage for every animal in the group" {
		t.Fatalf("refusal message=%q, want the farm-worded reason", failure.Message)
	}

	// Refused at raise means refused before ANYTHING durable exists.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM shifting_events WHERE tenant_id = $1::uuid AND created_at >= $2`,
		countsTenant, before); got != 0 {
		t.Fatalf("refused raise wrote %d shifting event(s), want 0", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM counts_approval_requests WHERE tenant_id = $1::uuid AND raised_at >= $2`,
		countsTenant, before); got != 0 {
		t.Fatalf("refused raise wrote %d approval request(s), want 0", got)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat shed=%s after a refused raise, want untouched %s", got, countsShedA)
	}
	if got := goatStage(t, ctx, pool, goatID); got != "K3" {
		t.Fatalf("goat stage=%q after a refused raise, want untouched K3", got)
	}
}
