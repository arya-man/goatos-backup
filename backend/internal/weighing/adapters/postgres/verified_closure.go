package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// THE NORMAL COMPLETION PATH FOR A WEIGHING TASK.
//
// Before this file the chain stopped dead one step short of an answer:
//
//	operator submits    -> bucket status 'completed'   (submitted, nothing more)
//	verifier approves   -> observation 'verified'      (and NOTHING else happened)
//	close               -> a separate manual act, WeighingMonitor-only, and
//	                       reason-REQUIRED
//
// Because the only door to a terminal state demanded a reason, closing was
// modelled exclusively as "a leader is ending this early and explaining why" —
// an EXCEPTION. A task whose every animal had been weighed, submitted and
// verified was indistinguishable on every surface from one still waiting on the
// verifier: both sat at 'completed'. The work finished and the record never said
// so.
//
// AUTOMATIC, NOT A ONE-TAP LEADERSHIP ACTION. The alternative was to surface a
// "close now" button on a bucket that is provably settled. It was rejected on
// evidence, not taste:
//
//  1. That affordance ALREADY EXISTED and is exactly what failed. `ready_to_close`
//     (domain.CampaignShed) has computed "submitted, and nothing unverified" since
//     migration 000058, and `pending_verification_count` has been on the bucket
//     read the whole time. Nothing consumed either as a completion. Adding a
//     second button on the same fact would rebuild the thing that did not work.
//  2. There is nothing left for a human to look at. The final human look IS the
//     verifier's approval of the last piece of evidence — that is what
//     verification is. A leader tapping afterwards re-decides nothing; they can
//     only agree. A gate whose only legal answer is "yes" is a queue, and a queue
//     nobody must clear is how a finished task sits open for a week.
//  3. Free-flow makes the predicate exact. Closure is "every SUBMITTED item is
//     verified" — a fact about evidence that exists, never a comparison against an
//     expected animal count (there is none). So the machine can decide it without
//     inventing a denominator, which is precisely the case where automation is
//     safe.
//  4. Nothing is lost. 'completed' already made the bucket immutable to new
//     capture, so auto-closing takes no ability away from the operator, and
//     ReopenScope remains the leader's way back in if they disagree.
//
// WHAT STAYS UNCHANGED. CloseScope and AbandonScope keep their mandatory reason
// and their WeighingMonitor permission, and a leader ending live work early is
// still a distinct act — recorded as closure_kind 'early' / 'abandoned' against
// this path's 'verified', audited under its own action, and emitted as its own
// event. This path never sets those kinds and those paths never set this one.
//
// IDEMPOTENCE. Everything here runs inside ApplyVerificationVerdict's single
// transaction, which is already keyed on the verification EVENT ID: an
// at-least-once redelivery short-circuits on the idempotency record and never
// reaches this code. The state changes are additionally guarded on their own
// preconditions (`WHERE status='completed'` for the bucket, "no non-terminal
// bucket left" for the task), so even a verdict that somehow arrived twice
// through different event ids can only close once and can only emit once.
const (
	// eventTypeScopeVerifiedClosed is DELIBERATELY NOT weighing.shed.closed.
	// Downstream must be able to tell "finished properly, every video approved"
	// from "a leader ended this early" without parsing a reason string —
	// notifications, Calendar and Control Tower say opposite things about the
	// two. That is the same reasoning that made abandon its own event type.
	eventTypeScopeVerifiedClosed    = "weighing.shed.verified_closed"
	eventTypeCampaignVerifiedClosed = "weighing.campaign.verified_closed"
)

// weighingVerifiedClosurePayload is the closure event body for BOTH grains.
// CampaignShedID/ShedID/ShedLabel are empty on the task-grain event.
//
// It carries no reason and no closed_by: nobody ended this work, so inventing an
// actor for the record would be a lie the audit trail keeps forever. The actor
// that can be named is the verifier whose approval was the last one, and that is
// what SettledBy means — attribution for the decision, not for a close.
type weighingVerifiedClosurePayload struct {
	TenantID       string `json:"tenant_id"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id,omitempty"`
	ParkID         string `json:"park_id"`
	ShedID         string `json:"shed_id,omitempty"`
	ShedLabel      string `json:"shed_label,omitempty"`
	OperatorID     string `json:"operator_id,omitempty"`
	SettledBy      string `json:"settled_by,omitempty"`
	ClosureKind    string `json:"closure_kind"`
	// VerifiedCount is the count of SUBMITTED items in the closed scope that a
	// verifier accepted. It is the evidence the closure rests on, so it travels
	// with the event rather than making every consumer re-query for it. Never a
	// numerator — there is no expected-animal denominator in weighing.
	VerifiedCount int       `json:"verified_count"`
	ClosedAt      time.Time `json:"closed_at"`
}

// settleVerifiedClosure is called from ApplyVerificationVerdict, in that
// transaction, ONLY on a 'verified' verdict. It answers one question at each
// grain — "is anything still outstanding here?" — and closes the scope when the
// answer is no.
//
// LOCK ORDER IS CAMPAIGN THEN BUCKET, matching CloseCampaign. Taking them the
// other way round would give two writers opposite orders and deadlock a verdict
// against a leadership close. Both locks are FOR NO KEY UPDATE, not FOR UPDATE:
// a plain FOR UPDATE on these parent rows conflicts with the FOR KEY SHARE that
// a concurrent observation insert takes for its foreign key, which is the exact
// deadlock CloseCampaign documents.
func (r *Repository) settleVerifiedClosure(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	scope observationScope,
) (shedClosed bool, campaignClosed bool, err error) {
	if scope.CampaignShedID == "" || scope.CampaignID == "" {
		// A free-flow individual observation may carry no bucket. There is no
		// scope to settle and nothing to invent one from.
		return false, false, nil
	}

	var campaignStatus string
	if err := tx.QueryRow(ctx, `
SELECT status FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid
FOR NO KEY UPDATE`, verdict.TenantID, scope.CampaignID).Scan(&campaignStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}

	var shedStatus string
	if err := tx.QueryRow(ctx, `
SELECT status FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid
FOR NO KEY UPDATE`, verdict.TenantID, scope.CampaignShedID).Scan(&shedStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	// Only a SUBMITTED bucket can complete. 'pending'/'in_progress' means the
	// operator has not filed the work yet — a verified stray observation inside
	// live work is not a finished shed. 'closed'/'canceled' are already terminal
	// and this path never rewrites a leader's decision.
	if shedStatus != domain.StatusCompleted {
		return false, false, nil
	}

	// The closure predicate, read under the bucket lock taken above so a submit
	// landing concurrently cannot slip between the count and the status flip.
	// SUBMITTED evidence only; no roster, no expected count, no denominator.
	submitted, pending, err := r.pendingVerificationCount(ctx, tx, verdict.TenantID, scope.CampaignShedID)
	if err != nil {
		return false, false, err
	}
	if submitted == 0 || pending > 0 {
		return false, false, nil
	}

	var closedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE weighing_campaign_sheds
SET status='closed',
  closed_at=now(),
  close_reason=NULL,
  closure_kind='verified',
  closed_not_accepted_count=0,
  updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND status='completed'
RETURNING closed_at`, verdict.TenantID, scope.CampaignShedID).Scan(&closedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost the race to another writer; that writer owns the closure.
			return false, false, nil
		}
		return false, false, err
	}
	// closed_by is left NULL on purpose. Every other close names the leader who
	// ended the work; this one had no such person, and writing the verifier there
	// would read forever as "the verifier closed the shed".

	shedPayload := weighingVerifiedClosurePayload{
		TenantID:       verdict.TenantID,
		CampaignID:     scope.CampaignID,
		CampaignShedID: scope.CampaignShedID,
		ParkID:         scope.ParkID,
		ShedID:         scope.ShedID,
		ShedLabel:      scope.ShedLabel,
		OperatorID:     scope.OperatorID,
		SettledBy:      verdict.VerifiedBy,
		ClosureKind:    domain.ClosureKindVerified,
		VerifiedCount:  submitted,
		ClosedAt:       closedAt,
	}
	if err := r.auditVerifiedClosure(ctx, tx, verdict, scope, "weighing.scope_verified_closed",
		"weighing_campaign_shed", scope.CampaignShedID, shedPayload); err != nil {
		return false, false, err
	}
	if err := r.enqueue(ctx, tx, verdict.TenantID, eventTypeScopeVerifiedClosed, scope.CampaignID,
		eventTypeScopeVerifiedClosed+":"+scope.CampaignShedID+":"+verdict.EventID, "", shedPayload); err != nil {
		return false, false, err
	}

	campaignClosed, err = r.settleCampaignVerifiedClosure(ctx, tx, verdict, scope, campaignStatus)
	if err != nil {
		return false, false, err
	}
	return true, campaignClosed, nil
}

// settleCampaignVerifiedClosure is the CASCADE: the task ends when its last
// bucket does.
//
// The predicate is "no bucket of this task is still non-terminal". 'canceled'
// buckets are retracted work and never hold the task open; every other terminal
// bucket ('closed', by any kind) is settled. A bucket still at
// 'pending'/'in_progress'/'completed' means real outstanding work — including a
// 'completed' bucket whose evidence is still queued, which is exactly the case
// the old campaign close gate existed to catch.
//
// The task is only closed 'verified' when it ends this way. A task a leader ends
// early still goes through CloseCampaign and is still recorded 'early'.
func (r *Repository) settleCampaignVerifiedClosure(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	scope observationScope,
	campaignStatus string,
) (bool, error) {
	if campaignStatus == domain.StatusClosed || campaignStatus == "canceled" {
		return false, nil
	}
	var outstanding int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('closed','canceled')`, verdict.TenantID, scope.CampaignID).Scan(&outstanding); err != nil {
		return false, err
	}
	if outstanding > 0 {
		return false, nil
	}
	// A task with no buckets at all never "finished"; it was never work.
	var settled int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status='closed'
  AND closure_kind='verified'`, verdict.TenantID, scope.CampaignID).Scan(&settled); err != nil {
		return false, err
	}
	if settled == 0 {
		return false, nil
	}

	var closedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE weighing_campaigns
SET status='closed',
  completed_at=COALESCE(completed_at, now()),
  closed_at=now(),
  close_reason=NULL,
  closure_kind='verified',
  closed_not_accepted_count=0,
  updated_at=now(),
  row_version=row_version+1
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('closed','canceled')
RETURNING closed_at`, verdict.TenantID, scope.CampaignID).Scan(&closedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	payload := weighingVerifiedClosurePayload{
		TenantID:      verdict.TenantID,
		CampaignID:    scope.CampaignID,
		ParkID:        scope.ParkID,
		SettledBy:     verdict.VerifiedBy,
		ClosureKind:   domain.ClosureKindVerified,
		VerifiedCount: settled,
		ClosedAt:      closedAt,
	}
	if err := r.auditVerifiedClosure(ctx, tx, verdict, scope, "weighing.campaign_verified_closed",
		"weighing_campaign", scope.CampaignID, payload); err != nil {
		return false, err
	}
	if err := r.enqueue(ctx, tx, verdict.TenantID, eventTypeCampaignVerifiedClosed, scope.CampaignID,
		eventTypeCampaignVerifiedClosed+":"+scope.CampaignID+":"+verdict.EventID, "", payload); err != nil {
		return false, err
	}
	return true, nil
}

// auditVerifiedClosure records the closure under its OWN action, never the
// action a leadership close uses, so "this finished" and "someone ended this"
// stay separable in the audit trail forever.
//
// ActorType is system_rule: no person performed the close. The verifier whose
// approval settled it is recorded in the metadata as settled_by.
func (r *Repository) auditVerifiedClosure(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	scope observationScope,
	action, resourceType, resourceID string,
	payload weighingVerifiedClosurePayload,
) error {
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     verdict.TenantID,
		ActorID:      verdict.VerifiedBy,
		ActorType:    "system_rule",
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ScopeType:    "weighing.campaign",
		ScopeID:      scope.CampaignID,
		AfterState:   payload,
		Metadata: map[string]any{
			"campaign_id":           scope.CampaignID,
			"campaign_shed_id":      scope.CampaignShedID,
			"closure_kind":          domain.ClosureKindVerified,
			"settled_by":            verdict.VerifiedBy,
			"verified_count":        payload.VerifiedCount,
			"verification_event_id": verdict.EventID,
		},
	})
}
