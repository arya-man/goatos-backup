package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ApprovalService tests -- the AUTHORITY layer.
//
// These cover the decisions made before anything touches the database: who may decide what, whether
// a reject carries a reason, and whether an approve prepares its effect through the owning module's
// guarded command. The transactional/atomicity proofs live in the Postgres integration tests.

const (
	svcTenant   = "11111111-1111-4111-8111-111111111111"
	svcApprover = "22222222-2222-4222-8222-222222222222"
	svcOperator = "33333333-3333-4333-8333-333333333333"
	svcGoat     = "44444444-4444-4444-8444-444444444444"
	svcPark     = "55555555-5555-4555-8555-555555555555"
	svcShed     = "66666666-6666-4666-8666-666666666666"
)

// shiftingApproverRole is a role that holds shifting-approval authority under the maintainer
// decision 2026-07-21 (approvals moved to the four org tiers + admin + ceo_internal). A manager
// grant is the direct analog of the retired park_head shifting approver, and when scoped to a park
// it exercises the same P0-2 park-scope path the park_head tests used to cover.
var shiftingApproverRole = permissions.RoleKey(permissions.TierManager, permissions.VerticalPreventiveCare)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeApprovalRepo records what the service asked the repository to do, so a test can assert that a
// rejection carried NO effect and that an approve carried the right one.
type fakeApprovalRepo struct {
	ports.Repository

	request     domain.ApprovalRequest
	getErr      error
	decisions   []domain.ApprovalDecision
	decideErr   error
	listQuery   domain.ApprovalRequestQuery
	listResult  domain.ApprovalRequestPage
	subjectPark string
}

func (f *fakeApprovalRepo) ApprovalSubjectPark(_ context.Context, _, _ string) (string, error) {
	return f.subjectPark, nil
}

func (f *fakeApprovalRepo) GetApprovalRequest(context.Context, string, string) (domain.ApprovalRequest, error) {
	if f.getErr != nil {
		return domain.ApprovalRequest{}, f.getErr
	}
	return f.request, nil
}

func (f *fakeApprovalRepo) DecideApprovalRequest(_ context.Context, in domain.ApprovalDecision) (domain.ApprovalRequest, bool, error) {
	f.decisions = append(f.decisions, in)
	if f.decideErr != nil {
		return domain.ApprovalRequest{}, false, f.decideErr
	}
	out := f.request
	out.Status = in.Status
	return out, false, nil
}

func (f *fakeApprovalRepo) ListApprovalRequests(_ context.Context, q domain.ApprovalRequestQuery) (domain.ApprovalRequestPage, error) {
	f.listQuery = q
	return f.listResult, nil
}

func (f *fakeApprovalRepo) CreateApprovalRequest(_ context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error) {
	return domain.ApprovalRequest{
		ApprovalRequestID: "request-1",
		TenantID:          in.TenantID,
		RequestType:       in.RequestType,
		Status:            domain.ApprovalStatusPending,
		RaisedAt:          in.RaisedAt,
	}, false, nil
}

// fakePreparer stands in for identity/app.Service's Prepare* seam. It records the commands it was
// asked to build so a test can prove the approval reached the GUARDED command rather than some
// weaker construction.
type fakePreparer struct {
	createCalls int
	exitCalls   int
	lastExit    identityapp.ExitGoatInput
	lastCreate  identityapp.CreateAdminGoatInput
	err         error
}

func (f *fakePreparer) PrepareCreateAdminGoat(_ context.Context, in identityapp.CreateAdminGoatInput) (identityports.CreateAdminGoatCommand, error) {
	f.createCalls++
	f.lastCreate = in
	if f.err != nil {
		return identityports.CreateAdminGoatCommand{}, f.err
	}
	return identityports.CreateAdminGoatCommand{TenantID: in.TenantID}, nil
}

func (f *fakePreparer) PrepareCriticalDeathExit(_ context.Context, in identityapp.ExitGoatInput) (identityports.ExitGoatCommand, error) {
	f.exitCalls++
	f.lastExit = in
	if f.err != nil {
		return identityports.ExitGoatCommand{}, f.err
	}
	// GuardrailApproved is what identity's adapter checks; the real PrepareCriticalDeathExit sets
	// it only after validateCriticalDeathExit passes.
	return identityports.ExitGoatCommand{
		TenantID: in.TenantID, GoatID: in.GoatID,
		LifecycleStatus: "dead", ExitReason: "died", GuardrailApproved: true,
	}, nil
}

func newDecisionInput(requestID string, approve bool, decidable []string) DecisionInput {
	return DecisionInput{
		TenantID:           svcTenant,
		ApprovalRequestID:  requestID,
		Approve:            approve,
		DecidedByUserID:    svcApprover,
		IdempotencyKey:     "decision-key-0001",
		RequestFingerprint: "decision-fp-0001",
		DecidableTypes:     decidable,
	}
}

func pendingRequest(requestType string) domain.ApprovalRequest {
	req := domain.ApprovalRequest{
		ApprovalRequestID: "request-1",
		TenantID:          svcTenant,
		RequestType:       requestType,
		Status:            domain.ApprovalStatusPending,
		RaisedByUserID:    svcOperator,
		RaisedAt:          time.Now().In(biztime.DefaultLocation()),
	}
	switch requestType {
	case domain.ApprovalRequestTypeDeath:
		goatID := svcGoat
		req.SubjectGoatID = &goatID
		req.Payload = json.RawMessage(`{"goat_id":"` + svcGoat + `","lifecycle_status":"dead","exit_reason":"died"}`)
	case domain.ApprovalRequestTypeShifting:
		eventID := "shifting-event-1"
		req.ShiftingEventID = &eventID
		req.Payload = json.RawMessage(`{"destination_park_id":"` + svcPark + `","destination_shed_id":"` + svcShed + `","goat_ids":["` + svcGoat + `"]}`)
	default:
		req.Payload = json.RawMessage(`{"species":"goat","sex":"female","dob":"2026-07-01"}`)
	}
	return req
}

// ---------------------------------------------------------------------------
// Authority
// ---------------------------------------------------------------------------

// A park_head holds NO approval authority (maintainer decision 2026-07-21). Reaching a birth
// request by its id must be refused before anything is prepared or applied -- the route-level
// permission cannot catch this, because the request type lives in the row, not the URL. With an
// empty decidable set the decision fails closed.
func TestParkHeadCannotApproveABirth(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeBirth)}
	preparer := &fakePreparer{}
	svc := NewApprovalService(repo, preparer, nil)

	decidable := permissions.DecidableApprovalRequestTypes([]string{permissions.RoleParkHead})
	_, _, err := svc.Decide(context.Background(), newDecisionInput("request-1", true, decidable))
	if !errors.Is(err, ErrApprovalForbiddenType) {
		t.Fatalf("err=%v, want ErrApprovalForbiddenType", err)
	}
	if len(repo.decisions) != 0 {
		t.Fatalf("repo decisions=%d, want 0 — an unauthorized decision must never reach the repository", len(repo.decisions))
	}
	if preparer.createCalls != 0 {
		t.Fatalf("prepare calls=%d, want 0 — nothing may be prepared for a caller who cannot decide", preparer.createCalls)
	}
}

// The mirror case: shifting authority and lifecycle authority are independent grants, so a role
// holding only lifecycle approval must not be able to authorize a movement. This is what stops the
// two permissions collapsing into one in practice.
func TestLifecycleOnlyApproverCannotApproveAShifting(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeShifting)}
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	// A caller holding CountsApproveLifecycle but NOT CountsApproveShifting.
	_, _, err := svc.Decide(context.Background(), newDecisionInput("request-1", true, []string{"birth", "death"}))
	if !errors.Is(err, ErrApprovalForbiddenType) {
		t.Fatalf("err=%v, want ErrApprovalForbiddenType", err)
	}
	if len(repo.decisions) != 0 {
		t.Fatalf("repo decisions=%d, want 0", len(repo.decisions))
	}
}

// TestApproveShiftingRejectsStoredPayloadNamingNoAnimals covers the SECOND enforcement layer for
// the 2026-07-19 "a shifting event must name the animals it moves" rule.
//
// The first layer is submit-time validation (missing_goat_ids). This one exists because approving
// is where the damage would happen: a stored payload whose animal set went missing would otherwise
// flip the event to authorized while relocating nobody, leaving the herd register and the
// shed-scoped vaccination obligations disagreeing with the count that was just approved. The rule
// is therefore re-checked against the persisted row rather than trusted from the writer -- the same
// both-layers posture the mandatory clinical defer states use.
//
// Crucially this blocks APPROVE only. Decide short-circuits a rejection before prepareEffect, so a
// corrupt request stays REJECTABLE and never becomes an undecidable row stuck in the queue; the
// second sub-test pins that, because a fail-closed check that strands work is its own outage.
func TestApproveShiftingRejectsStoredPayloadNamingNoAnimals(t *testing.T) {
	storedWithoutGoats := func() domain.ApprovalRequest {
		req := pendingRequest(domain.ApprovalRequestTypeShifting)
		req.Payload = json.RawMessage(
			`{"destination_park_id":"` + svcPark + `","destination_shed_id":"` + svcShed + `","goat_ids":[]}`)
		return req
	}
	decidable := permissions.DecidableApprovalRequestTypes([]string{shiftingApproverRole})

	t.Run("approve is refused", func(t *testing.T) {
		repo := &fakeApprovalRepo{request: storedWithoutGoats()}
		svc := NewApprovalService(repo, &fakePreparer{}, nil)

		_, _, err := svc.Decide(context.Background(), newDecisionInput("request-1", true, decidable))
		if !errors.Is(err, ErrApprovalInvalidStoredPayload) {
			t.Fatalf("err=%v, want ErrApprovalInvalidStoredPayload", err)
		}
		if len(repo.decisions) != 0 {
			t.Fatalf("repo decisions=%d, want 0 — a movement that names nobody must not be authorized", len(repo.decisions))
		}
	})

	t.Run("reject still works", func(t *testing.T) {
		repo := &fakeApprovalRepo{request: storedWithoutGoats()}
		svc := NewApprovalService(repo, &fakePreparer{}, nil)

		in := newDecisionInput("request-1", false, decidable)
		in.Reason = "submitted without naming the animals that moved"
		if _, _, err := svc.Decide(context.Background(), in); err != nil {
			t.Fatalf("reject err=%v, want nil — a corrupt request must stay decidable, not strand in the queue", err)
		}
		if len(repo.decisions) != 1 {
			t.Fatalf("repo decisions=%d, want 1 (the rejection must reach the repository)", len(repo.decisions))
		}
		if repo.decisions[0].Status != domain.ApprovalStatusRejected {
			t.Fatalf("status=%q, want %q", repo.decisions[0].Status, domain.ApprovalStatusRejected)
		}
	})
}

// The permission table is the contract these tests rely on; assert it directly so a future edit
// that quietly changes who may approve fails here.
//
// Maintainer decision 2026-07-21: approve/reject moved off the park head onto the four org tiers
// (director/head/manager/am, each across every vertical) plus ceo_internal, on the admin-web
// Approvals page. All four tiers decide every type; park_head now decides nothing. (The flat
// `admin` role was removed from main, so it is no longer part of the approver set.)
func TestDecidableApprovalRequestTypesPerRole(t *testing.T) {
	all := []string{"birth", "death", "shifting"}
	cases := []struct {
		role string
		want []string
	}{
		// The four org tiers each approve everything (any vertical composes the same tier caps).
		{permissions.RoleKey(permissions.TierDirector, permissions.VerticalPreventiveCare), all},
		{permissions.RoleKey(permissions.TierHead, permissions.VerticalFeed), all},
		{permissions.RoleKey(permissions.TierManager, permissions.VerticalHealth), all},
		{permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalBreeding), all},
		{permissions.RoleCEOInternal, all},
		// Park head no longer holds ANY approval permission.
		{permissions.RoleParkHead, nil},
		// Operators capture; capture is not approval.
		{permissions.RoleOperator, nil},
		// Separation of duty: the verifier reviews media and must not gain lifecycle authority.
		{permissions.RoleVerifier, nil},
		{permissions.RolePCDirector, nil},
	}
	for _, tc := range cases {
		got := permissions.DecidableApprovalRequestTypes([]string{tc.role})
		if len(got) != len(tc.want) {
			t.Fatalf("role %s: decidable=%v, want %v", tc.role, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("role %s: decidable=%v, want %v", tc.role, got, tc.want)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Reject
// ---------------------------------------------------------------------------

// A rejection must carry a reason: the operator who reported the event has to know why it was
// refused, otherwise they re-submit the same thing.
func TestRejectRequiresAReason(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeBirth)}
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	in := newDecisionInput("request-1", false, []string{"birth", "death"})
	if _, _, err := svc.Decide(context.Background(), in); !errors.Is(err, ErrApprovalReasonRequired) {
		t.Fatalf("err=%v, want ErrApprovalReasonRequired", err)
	}
	if len(repo.decisions) != 0 {
		t.Fatalf("repo decisions=%d, want 0", len(repo.decisions))
	}
}

// A rejection must reach the repository with NO effect attached. If a reason-carrying reject ever
// arrived with a prepared command, the repository could apply it.
func TestRejectCarriesNoEffect(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeBirth)}
	preparer := &fakePreparer{}
	svc := NewApprovalService(repo, preparer, nil)

	in := newDecisionInput("request-1", false, []string{"birth", "death"})
	in.Reason = "duplicate report"
	if _, _, err := svc.Decide(context.Background(), in); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if len(repo.decisions) != 1 {
		t.Fatalf("repo decisions=%d, want 1", len(repo.decisions))
	}
	decision := repo.decisions[0]
	if decision.Status != domain.ApprovalStatusRejected {
		t.Fatalf("status=%q, want rejected", decision.Status)
	}
	if decision.Effect != nil {
		t.Fatal("a rejection must carry no effect — an effect on the decision is applicable state")
	}
	if preparer.createCalls != 0 {
		t.Fatalf("prepare calls=%d, want 0 on a rejection", preparer.createCalls)
	}
}

// ---------------------------------------------------------------------------
// Approve prepares the guarded command
// ---------------------------------------------------------------------------

// Approving a death must go through PrepareCriticalDeathExit -- the guarded command -- and the
// resulting command must carry GuardrailApproved. Reaching identity by any other route would be a
// way around the dead+died rule.
func TestApproveDeathPreparesTheGuardedCommand(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeDeath)}
	preparer := &fakePreparer{}
	svc := NewApprovalService(repo, preparer, nil)

	if _, _, err := svc.Decide(context.Background(), newDecisionInput("request-1", true, []string{"birth", "death"})); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if preparer.exitCalls != 1 {
		t.Fatalf("PrepareCriticalDeathExit calls=%d, want 1", preparer.exitCalls)
	}
	// goat_id must have been split out of the stored payload: identity's ExitGoatRequest decodes
	// strictly and would reject the body if it were still inline.
	if preparer.lastExit.GoatID != svcGoat {
		t.Fatalf("prepared goat_id=%q, want %q", preparer.lastExit.GoatID, svcGoat)
	}
	var body map[string]any
	if err := json.Unmarshal(preparer.lastExit.RawBody, &body); err != nil {
		t.Fatalf("decode forwarded body: %v", err)
	}
	if _, present := body["goat_id"]; present {
		t.Fatal("goat_id must be stripped from the body forwarded to identity")
	}
	decision := repo.decisions[0]
	if decision.Effect == nil || decision.Effect.ExitGoat == nil {
		t.Fatal("an approved death must carry the prepared exit command")
	}
	cmd, ok := decision.Effect.ExitGoat.(identityports.ExitGoatCommand)
	if !ok {
		t.Fatalf("effect is %T, want identityports.ExitGoatCommand", decision.Effect.ExitGoat)
	}
	if cmd.LifecycleStatus != "dead" || cmd.ExitReason != "died" || !cmd.GuardrailApproved {
		t.Fatalf("prepared command=%+v, want the guardrail-approved dead+died pairing", cmd)
	}
}

// The apply-time idempotency key must be a function of the APPROVAL REQUEST, not of the approver's
// client key. Otherwise two approvers (or one approver retrying with a fresh key) would each get a
// distinct identity write and the herd would gain two kids from one birth.
func TestApproveEffectIdempotencyKeyIsDerivedFromTheRequest(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeBirth)}
	preparer := &fakePreparer{}
	svc := NewApprovalService(repo, preparer, nil)

	first := newDecisionInput("request-1", true, []string{"birth", "death"})
	if _, _, err := svc.Decide(context.Background(), first); err != nil {
		t.Fatalf("approve: %v", err)
	}
	keyFromFirst := preparer.lastCreate.IdempotencyKey

	second := newDecisionInput("request-1", true, []string{"birth", "death"})
	second.IdempotencyKey = "a-totally-different-client-key"
	second.RequestFingerprint = "a-different-fingerprint"
	if _, _, err := svc.Decide(context.Background(), second); err != nil {
		t.Fatalf("second approve: %v", err)
	}
	if preparer.lastCreate.IdempotencyKey != keyFromFirst {
		t.Fatalf("effect idempotency key changed between approvals (%q -> %q); a retry with a new "+
			"client key must still resolve to the SAME identity write",
			keyFromFirst, preparer.lastCreate.IdempotencyKey)
	}
	if keyFromFirst == "" {
		t.Fatal("effect idempotency key must not be empty")
	}
}

// An already-decided request must not have its effect re-prepared. Preparing would burn the
// idempotency key of a command that will never run, and for a birth it is the step that resolves
// identifiers -- doing it again on a decided request is pure waste and a source of spurious errors.
func TestApproveDoesNotPrepareAnAlreadyDecidedRequest(t *testing.T) {
	decided := pendingRequest(domain.ApprovalRequestTypeBirth)
	decided.Status = domain.ApprovalStatusApproved
	repo := &fakeApprovalRepo{request: decided}
	preparer := &fakePreparer{}
	svc := NewApprovalService(repo, preparer, nil)

	if _, _, err := svc.Decide(context.Background(), newDecisionInput("request-1", true, []string{"birth", "death"})); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if preparer.createCalls != 0 {
		t.Fatalf("prepare calls=%d, want 0 for an already-decided request", preparer.createCalls)
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// The list's type filter must come from the caller's authority. If it were client-supplied, a
// caller could ask for types they cannot act on and page through work outside their authority.
func TestListPendingPassesCallerAuthorityAsTheTypeFilter(t *testing.T) {
	repo := &fakeApprovalRepo{}
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	// A caller whose authority is shifting-only must get a shifting-only list filter. Passed
	// explicitly rather than derived from a role, because every current approver role decides all
	// three types -- the contract under test is that ListPending forwards the caller's authority verbatim.
	decidable := []string{"shifting"}
	if _, err := svc.ListPending(context.Background(), svcTenant, "", decidable, "", 20, ""); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(repo.listQuery.RequestTypes) != 1 || repo.listQuery.RequestTypes[0] != domain.ApprovalRequestTypeShifting {
		t.Fatalf("request types=%v, want only shifting", repo.listQuery.RequestTypes)
	}
	// An absent status must default to pending, which is the queue an approver opens.
	if repo.listQuery.Status != domain.ApprovalStatusPending {
		t.Fatalf("status=%q, want pending by default", repo.listQuery.Status)
	}
}

// The page size is a mobile constraint, not a suggestion: a client asking for the whole backlog
// must receive one screen of work.
func TestListPendingCapsPageSize(t *testing.T) {
	repo := &fakeApprovalRepo{}
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	if _, err := svc.ListPending(context.Background(), svcTenant, "", []string{"shifting"}, "", 500, ""); err != nil {
		t.Fatalf("list: %v", err)
	}
	// The service forwards the request; the repository clamps. Assert the domain cap is what the
	// repository will clamp to, so the two cannot drift.
	if domain.MaxApprovalPageSize > 20 {
		t.Fatalf("MaxApprovalPageSize=%d, want <= 20 (mobile page-size rule)", domain.MaxApprovalPageSize)
	}
}

// A malformed cursor must be rejected rather than silently ignored, which would restart the
// approver at page one and make them re-decide work they already passed.
func TestListPendingRejectsAMalformedCursor(t *testing.T) {
	svc := NewApprovalService(&fakeApprovalRepo{}, &fakePreparer{}, nil)
	if _, err := svc.ListPending(context.Background(), svcTenant, "", []string{"shifting"}, "", 20, "!!!not-base64!!!"); err == nil {
		t.Fatal("a malformed cursor must be rejected")
	}
}

// P0-2 scope escalation: a park-scoped approver may decide a shifting request ONLY inside the park
// they manage. The route-level permission checks the request TYPE, not the park SCOPE, so without
// this the same approver could authorize a movement in a park they do not manage. The park lives in
// the stored payload (destination_park_id, which equals the source park by P0-1), never in the URL.
func TestParkScopedApproverCannotApproveShiftingOutsideTheirPark(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeShifting)} // dest park = svcPark
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	decidable := permissions.DecidableApprovalRequestTypes([]string{shiftingApproverRole})
	in := newDecisionInput("request-1", true, decidable)
	in.CallerParkID = "77777777-7777-4777-8777-777777777777" // a DIFFERENT park than the request's

	_, _, err := svc.Decide(context.Background(), in)
	if !errors.Is(err, ErrApprovalForbiddenScope) {
		t.Fatalf("err=%v, want ErrApprovalForbiddenScope", err)
	}
	if len(repo.decisions) != 0 {
		t.Fatalf("repo decisions=%d, want 0 — an out-of-scope decision must never reach the repository", len(repo.decisions))
	}
}

// The in-scope case must still succeed: a park-scoped approver deciding a shifting request in their
// OWN park passes the scope check and the decision is recorded.
func TestParkScopedApproverCanApproveShiftingInTheirPark(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeShifting)} // dest park = svcPark
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	decidable := permissions.DecidableApprovalRequestTypes([]string{shiftingApproverRole})
	in := newDecisionInput("request-1", true, decidable)
	in.CallerParkID = svcPark // the SAME park as the request

	if _, _, err := svc.Decide(context.Background(), in); errors.Is(err, ErrApprovalForbiddenScope) {
		t.Fatalf("in-scope park head was wrongly denied: %v", err)
	}
	if len(repo.decisions) != 1 {
		t.Fatalf("repo decisions=%d, want 1 — an in-scope decision must be recorded", len(repo.decisions))
	}
}

func TestParkScopedManagerCanApproveDeathOnlyInGoatsPark(t *testing.T) {
	for _, tc := range []struct {
		name, subjectPark string
		wantForbidden     bool
	}{
		{name: "same park", subjectPark: svcPark},
		{name: "different park", subjectPark: "77777777-7777-4777-8777-777777777777", wantForbidden: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeDeath), subjectPark: tc.subjectPark}
			svc := NewApprovalService(repo, &fakePreparer{}, nil)
			in := newDecisionInput("request-1", true,
				permissions.DecidableApprovalRequestTypes([]string{shiftingApproverRole}))
			in.CallerParkID = svcPark
			_, _, err := svc.Decide(context.Background(), in)
			if got := errors.Is(err, ErrApprovalForbiddenScope); got != tc.wantForbidden {
				t.Fatalf("forbidden=%v err=%v, want %v", got, err, tc.wantForbidden)
			}
		})
	}
}

// A no-scope caller (CEO/internal, empty CallerParkID) is not restricted by park scope and may
// decide a request in any park.
func TestNoScopeCallerBypassesParkScopeCheck(t *testing.T) {
	repo := &fakeApprovalRepo{request: pendingRequest(domain.ApprovalRequestTypeShifting)}
	svc := NewApprovalService(repo, &fakePreparer{}, nil)

	decidable := permissions.DecidableApprovalRequestTypes([]string{shiftingApproverRole})
	in := newDecisionInput("request-1", true, decidable)
	in.CallerParkID = "" // no park scope

	if _, _, err := svc.Decide(context.Background(), in); errors.Is(err, ErrApprovalForbiddenScope) {
		t.Fatalf("no-scope caller was wrongly denied: %v", err)
	}
	if len(repo.decisions) != 1 {
		t.Fatalf("repo decisions=%d, want 1", len(repo.decisions))
	}
}
