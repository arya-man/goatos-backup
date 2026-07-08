package identitybridge

import (
	"context"
	"encoding/json"
	"testing"

	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
)

type fakeTransitions struct {
	reproBody []byte
	healthCmd *identityapp.HealthGoatInput
	exitCmd   *identityapp.ExitGoatInput
	resp      *identitydomain.AdminGoatResponse
	err       error
}

func (f *fakeTransitions) ReproductiveGoat(_ context.Context, in identityapp.ReproductiveGoatInput) (*identitydomain.AdminGoatResponse, error) {
	f.reproBody = in.RawBody
	return f.resp, f.err
}

func (f *fakeTransitions) HealthGoat(_ context.Context, in identityapp.HealthGoatInput) (*identitydomain.AdminGoatResponse, error) {
	f.healthCmd = &in
	return f.resp, f.err
}

func (f *fakeTransitions) ExitGoat(_ context.Context, in identityapp.ExitGoatInput) (*identitydomain.AdminGoatResponse, error) {
	f.exitCmd = &in
	return f.resp, f.err
}

const goatA = "10000000-0000-4000-8000-000000000001"

func baseReq(axis, target string) bulkapp.ApplyRowRequest {
	return bulkapp.ApplyRowRequest{
		TenantID: "t1", ActorID: goatA, JobID: "job-1", GoatID: goatA,
		Axis: axis, Target: target, ExpectedRowVersion: 4, TraceID: "trace",
	}
}

func TestBridgeAppliedCarriesEventID(t *testing.T) {
	ft := &fakeTransitions{resp: &identitydomain.AdminGoatResponse{Events: []identitydomain.EventSummary{{EventID: "evt-1", EventType: "goat.reproductive.changed"}}}}
	res, err := New(ft).ApplyBulkStatusRow(context.Background(), baseReq(bulkapp.AxisReproductive, "pregnant"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Outcome != bulkapp.OutcomeApplied || res.EventID != "evt-1" {
		t.Fatalf("expected applied with event id, got %+v", res)
	}
	// The reproductive body must carry the captured expected_row_version as row_version.
	var body identitydomain.ReproductiveGoatRequest
	if err := json.Unmarshal(ft.reproBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.RowVersion != 4 || body.ReproductiveStatus != "pregnant" {
		t.Fatalf("unexpected reproductive body: %+v", body)
	}
	if len(body.EvidenceRefs) != 1 || body.EvidenceRefs[0].EvidenceType != "source_record" {
		t.Fatalf("expected a source_record evidence ref, got %+v", body.EvidenceRefs)
	}
}

func TestBridgeClassifiesErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bulkapp.RowOutcome
	}{
		{"write_conflict -> skipped", identityapp.Conflict("write_conflict", "x"), bulkapp.OutcomeSkipped},
		{"not_found -> skipped", identityapp.NotFound("x"), bulkapp.OutcomeSkipped},
		{"idempotency_pending -> retry", identityapp.Conflict("idempotency_pending", "x"), bulkapp.OutcomeRetry},
		{"guardrail -> error", identityapp.GuardrailRequired("critical_health_transition_requires_guardrail", "x"), bulkapp.OutcomeError},
		{"internal -> retry", identityapp.Internal("boom"), bulkapp.OutcomeRetry},
		{"bad request -> error", identityapp.BadRequest("invalid_reference", "x"), bulkapp.OutcomeError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTransitions{err: tc.err}
			res, err := New(ft).ApplyBulkStatusRow(context.Background(), baseReq(bulkapp.AxisHealth, "sick"))
			if err != nil {
				t.Fatalf("apply returned transport error: %v", err)
			}
			if res.Outcome != tc.want {
				t.Fatalf("got %q want %q", res.Outcome, tc.want)
			}
		})
	}
}

func TestBridgeUnknownTransportErrorBubblesForRetry(t *testing.T) {
	ft := &fakeTransitions{err: context.DeadlineExceeded}
	_, err := New(ft).ApplyBulkStatusRow(context.Background(), baseReq(bulkapp.AxisHealth, "sick"))
	if err == nil {
		t.Fatal("expected non-app transport error to bubble up so the worker retries")
	}
}

func TestBridgeExitDerivesReason(t *testing.T) {
	ft := &fakeTransitions{resp: &identitydomain.AdminGoatResponse{Events: []identitydomain.EventSummary{{EventID: "evt-9"}}}}
	if _, err := New(ft).ApplyBulkStatusRow(context.Background(), baseReq(bulkapp.AxisExit, "sold")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ft.exitCmd == nil {
		t.Fatal("expected exit transition to be called")
	}
	var body identitydomain.ExitGoatRequest
	if err := json.Unmarshal(ft.exitCmd.RawBody, &body); err != nil {
		t.Fatalf("unmarshal exit body: %v", err)
	}
	if body.LifecycleStatus != "sold" || body.ExitReason != "sold" {
		t.Fatalf("expected sold/sold, got %+v", body)
	}
}

func TestBridgeRejectsDeathExitTarget(t *testing.T) {
	ft := &fakeTransitions{}
	res, err := New(ft).ApplyBulkStatusRow(context.Background(), baseReq(bulkapp.AxisExit, "dead"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Outcome != bulkapp.OutcomeError {
		t.Fatalf("expected death exit target to be rejected as error, got %+v", res)
	}
	if ft.exitCmd != nil {
		t.Fatal("death exit must not reach the transition on the bulk path")
	}
}
