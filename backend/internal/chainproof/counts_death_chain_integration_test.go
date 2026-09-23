package chainproof

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/chaintest"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestDeathReportedThenRejectedChain proves TWO registered chains end to end, in the order the
// farm produces them:
//
//	counts.death.reported -> tasks/app.CountsDeathReportedHandler -> the staged death workflow
//	counts.death.rejected -> tasks/app.CountsDeathRejectedHandler -> that workflow canceled
//
// The rejected chain had no producer proof at all. Its registered one,
// counts/adapters/postgres.TestRejectAppliesNothing, rejects a BIRTH -- birthSubmission(...) --
// and a birth rejection emits no death event, so deleting the death-rejected emission left it
// green. The consumer side was covered only by tasks/app tests that hand-build the event.
//
// Which assertion catches which mutation:
//
//	drop the reported emission  -> DrainExpecting finds no counts.death.reported
//	no-op the reported handler  -> no workflow is found for the reported death
//	drop the rejected emission  -> DrainExpecting finds no counts.death.rejected
//	no-op the rejected handler  -> the workflow is still open after the rejection
//
// Fixtures seed only external inputs: the tenant, its custodian, a pen, and one LIVE animal. The
// approval rows, the outbox envelopes and the workflow are produced by production code.
func TestDeathReportedThenRejectedChain(t *testing.T) {
	pool, ctx := newChainDB(t)
	const goatID = "7c0f1a2b-0000-4000-8000-00000000d001"
	seedChainGoat(t, ctx, pool, goatID, "alive")

	bus := workflowBus(t, pool)
	counts := countspg.NewRepository(pool, 10*time.Second).
		WithDeathEvidenceTxGate(chainDeathGateReady{})

	// PRODUCER 1: the operator reports the death. Real command, real transaction.
	raised := countsdomain.ApprovalRequestSubmission{
		TenantID:    chainTenant,
		RequestType: countsdomain.ApprovalRequestTypeDeath,
		Payload: json.RawMessage(`{"goat_id":"` + goatID +
			`","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID:      ptr(goatID),
		RaisedByUserID:     chainCustodian,
		RaisedAt:           chainEventAt,
		IdempotencyKey:     "chain-death-report",
		RequestFingerprint: "chain-death-report-fp",
	}
	req, replay, err := counts.CreateApprovalRequest(ctx, raised)
	if err != nil || replay {
		t.Fatalf("report the death: replay=%v err=%v", replay, err)
	}

	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "counts.death.reported")

	var workflowID string
	if err := pool.QueryRow(ctx, `
SELECT workflow_id::text FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_goat_id = $2::uuid AND template_key = $3`,
		chainTenant, goatID, tasksdomain.TemplateKeyDeath).Scan(&workflowID); err != nil {
		t.Fatalf("the reported death opened no staged workflow: %v", err)
	}
	if state := workflowState(t, ctx, pool, workflowID); state == "canceled" {
		t.Fatalf("the workflow opened already canceled")
	}

	// PRODUCER 2: the approver REJECTS it. A rejection applies nothing -- the animal stays alive --
	// so the staged evidence work must be withdrawn by the event, not by the write.
	if _, _, err := counts.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID:           chainTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             countsdomain.ApprovalStatusRejected,
		DecidedByUserID:    chainCustodian,
		DecidedAt:          chainEventAt,
		Reason:             "duplicate report",
		IdempotencyKey:     "chain-death-reject",
		RequestFingerprint: "chain-death-reject-fp",
	}); err != nil {
		t.Fatalf("reject the death: %v", err)
	}
	// The animal is still alive, which is the precondition the cancel is allowed to act on.
	if got := goatLifecycle(t, ctx, pool, goatID); got != "alive" {
		t.Fatalf("a rejection must not kill the animal; lifecycle=%q", got)
	}

	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "counts.death.rejected")

	// CONSUMER state: the staged work is withdrawn.
	if state := workflowState(t, ctx, pool, workflowID); state != "canceled" {
		t.Fatalf("the rejected death left its workflow in state %q; want canceled", state)
	}
	var open int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM workflow_actions WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid AND status <> 'canceled'`,
		chainTenant, workflowID).Scan(&open); err != nil {
		t.Fatalf("count open steps: %v", err)
	}
	if open != 0 {
		t.Fatalf("%d steps of the rejected death are still open; a rejection withdraws all of them", open)
	}
}

func ptr(s string) *string { return &s }

// chainDeathGateReady stands in for the death-evidence readiness gate, which is an inbound policy
// seam rather than part of either chain under test.
type chainDeathGateReady struct{}

func (chainDeathGateReady) PrepareDeathEvidenceForApprovalInTx(_ context.Context, _ pgx.Tx, _, _ string) (bool, error) {
	return true, nil
}
