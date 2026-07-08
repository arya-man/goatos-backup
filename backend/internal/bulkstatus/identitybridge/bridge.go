// Package identitybridge adapts the bulk status kernel's GoatTransitionApplier
// to the identity module's per-goat transitions. Every bulk row is applied
// through the SAME ReproductiveGoat / HealthGoat / ExitGoat path used by the
// single-goat admin APIs, so the proper domain event, decision record, outbox
// message, vocabulary validation and guardrails all fire. The bridge translates
// the identity app error taxonomy into a bulk RowOutcome the worker persists.
package identitybridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
)

// IdentityTransitions is the subset of the identity app.Service the bridge needs.
// *identityapp.Service satisfies it.
type IdentityTransitions interface {
	ReproductiveGoat(ctx context.Context, input identityapp.ReproductiveGoatInput) (*identitydomain.AdminGoatResponse, error)
	HealthGoat(ctx context.Context, input identityapp.HealthGoatInput) (*identitydomain.AdminGoatResponse, error)
	ExitGoat(ctx context.Context, input identityapp.ExitGoatInput) (*identitydomain.AdminGoatResponse, error)
}

// Bridge implements bulkapp.GoatTransitionApplier.
type Bridge struct {
	transitions IdentityTransitions
}

func New(transitions IdentityTransitions) *Bridge {
	return &Bridge{transitions: transitions}
}

var _ bulkapp.GoatTransitionApplier = (*Bridge)(nil)

// exitReasonByLifecycle mirrors identity's lifecycle->exit_reason pairing for the
// non-critical bulk exit targets. "dead" is intentionally absent: death exits
// require the critical-action guardrail path and are rejected by the transition.
var exitReasonByLifecycle = map[string]string{
	"sold":        "sold",
	"culled":      "culled",
	"transferred": "transferred",
	"lost":        "lost",
}

func (b *Bridge) ApplyBulkStatusRow(ctx context.Context, req bulkapp.ApplyRowRequest) (bulkapp.ApplyRowResult, error) {
	reason := strings.TrimSpace(req.Reason)
	if len(reason) < 3 {
		reason = fmt.Sprintf("Bulk %s status update (job %s)", req.Axis, req.JobID)
	}
	evidence := []identitydomain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "bulk_status_job:" + req.JobID,
		SourceSystem: strPtr("bulk_status"),
	}}
	idempotencyKey := fmt.Sprintf("bulkstatus:%s:%s:%s:%d", req.JobID, req.Axis, req.Target, req.ExpectedRowVersion)

	var resp *identitydomain.AdminGoatResponse
	var err error
	switch req.Axis {
	case bulkapp.AxisReproductive:
		body, mErr := json.Marshal(identitydomain.ReproductiveGoatRequest{
			ReproductiveStatus: req.Target,
			Reason:             reason,
			EvidenceRefs:       evidence,
			RowVersion:         req.ExpectedRowVersion,
		})
		if mErr != nil {
			return bulkapp.ApplyRowResult{}, mErr
		}
		resp, err = b.transitions.ReproductiveGoat(ctx, identityapp.ReproductiveGoatInput{
			TenantID: req.TenantID, ActorID: req.ActorID, IdempotencyKey: idempotencyKey,
			TraceID: req.TraceID, GoatID: req.GoatID, RawBody: body,
		})
	case bulkapp.AxisHealth:
		body, mErr := json.Marshal(identitydomain.HealthGoatRequest{
			HealthStatus: req.Target,
			Reason:       reason,
			EvidenceRefs: evidence,
			RowVersion:   req.ExpectedRowVersion,
		})
		if mErr != nil {
			return bulkapp.ApplyRowResult{}, mErr
		}
		resp, err = b.transitions.HealthGoat(ctx, identityapp.HealthGoatInput{
			TenantID: req.TenantID, ActorID: req.ActorID, IdempotencyKey: idempotencyKey,
			TraceID: req.TraceID, GoatID: req.GoatID, RawBody: body,
		})
	case bulkapp.AxisExit:
		exitReason, ok := exitReasonByLifecycle[req.Target]
		if !ok {
			// Unsupported/critical lifecycle target for the bulk path.
			return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeError, Reason: "unsupported_bulk_exit_target:" + req.Target}, nil
		}
		body, mErr := json.Marshal(identitydomain.ExitGoatRequest{
			LifecycleStatus: req.Target,
			ExitReason:      exitReason,
			Reason:          reason,
			EvidenceRefs:    evidence,
			RowVersion:      req.ExpectedRowVersion,
		})
		if mErr != nil {
			return bulkapp.ApplyRowResult{}, mErr
		}
		resp, err = b.transitions.ExitGoat(ctx, identityapp.ExitGoatInput{
			TenantID: req.TenantID, ActorID: req.ActorID, IdempotencyKey: idempotencyKey,
			TraceID: req.TraceID, GoatID: req.GoatID, RawBody: body,
		})
	default:
		return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeError, Reason: "unknown_axis:" + req.Axis}, nil
	}

	return classify(resp, err)
}

// classify maps an identity transition result/error to a bulk RowOutcome.
//   - success (incl. idempotent replay) -> applied, with the event id.
//   - write_conflict / not_found        -> skipped (no-clobber: newer live write,
//     goat already at target, or goat gone/exited/out of scope).
//   - idempotency_pending / 5xx / retryable -> retry (transient).
//   - guardrail / invalid / 4xx         -> error (permanent, needs operator).
//
// A returned (non-nil) error is only for a truly unexpected transport failure the
// worker should retry.
func classify(resp *identitydomain.AdminGoatResponse, err error) (bulkapp.ApplyRowResult, error) {
	if err == nil {
		eventID := ""
		if resp != nil && len(resp.Events) > 0 {
			eventID = resp.Events[0].EventID
		}
		return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeApplied, EventID: eventID}, nil
	}
	var appErr *identityapp.Error
	if !errors.As(err, &appErr) {
		// Unknown/transport error: let the worker retry.
		return bulkapp.ApplyRowResult{}, err
	}
	switch appErr.Code {
	case "write_conflict":
		return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeSkipped, Reason: "row_version_mismatch_or_noop"}, nil
	case "not_found_or_not_allowed":
		return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeSkipped, Reason: "goat_not_found_or_out_of_scope"}, nil
	case "idempotency_pending":
		return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeRetry, Reason: "idempotency_pending"}, nil
	}
	if appErr.Retryable || appErr.HTTPStatus >= 500 {
		return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeRetry, Reason: appErr.Code}, nil
	}
	// Guardrail-required, invalid_reference, bad request, idempotency_conflict:
	// permanent for the bulk path.
	return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeError, Reason: appErr.Code}, nil
}

func strPtr(s string) *string { return &s }
