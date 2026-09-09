package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09). A producer may answer a
// per-field approve with MeasurementConfirmationRequired. Verification's half of that: the approve
// becomes a 422 measurement_confirmation_required naming each flagged box, NOTHING is recorded (no
// verdict, no row_version bump), and the same approve re-sent with VarianceAcknowledged reaches the
// producer carrying that flag and then lands.

func TestConfirmationRefusalIsA422ThatRecordsNothing(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())
	applier.applyErr = &MeasurementConfirmationRequired{
		Message: "check the video again",
		Fields: []MeasurementFieldNotice{
			{Key: "maize", Code: "above_plan", Message: "More than 500 g above the plan."},
			{Key: "", Code: "ignored", Message: "a keyless notice cannot address a box"},
		},
	}

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-confirm-1",
		Measurement: &domain.VerdictMeasurement{Entries: []domain.MeasurementEntry{
			{Key: "maize", Value: 12.6},
			{Key: "soya", Value: 1},
		}},
	})
	appErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	if appErr.HTTPStatus != 422 || appErr.Code != "measurement_confirmation_required" {
		t.Fatalf("status/code = %d/%q, want 422 measurement_confirmation_required", appErr.HTTPStatus, appErr.Code)
	}
	if appErr.Message != "check the video again" {
		t.Errorf("message = %q, want the producer's headline verbatim", appErr.Message)
	}
	if len(appErr.FieldErrors) != 1 {
		t.Fatalf("field errors = %+v, want exactly the keyed notice", appErr.FieldErrors)
	}
	if fe := appErr.FieldErrors[0]; fe.Field != "measurement.entries.maize" || fe.Code != "above_plan" || fe.Message != "More than 500 g above the plan." {
		t.Errorf("field error = %+v, want measurement.entries.maize / above_plan / the producer's sentence", fe)
	}
	// Nothing landed: the item is still pending on the row_version she loaded, so the same approve
	// can be re-sent acknowledged without a 409.
	current, err := svc.GetItem(context.Background(), item.TenantID, item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if current.Status != domain.StatusPending || current.RowVersion != item.RowVersion {
		t.Fatalf("after refusal: status=%q row_version=%d, want pending on row_version %d", current.Status, current.RowVersion, item.RowVersion)
	}
	if len(applier.applies) != 0 {
		t.Fatalf("applier recorded %d sets after a refusal, want 0", len(applier.applies))
	}
}

func TestAcknowledgedApproveReachesTheProducerWithTheFlagAndLands(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-confirm-2",
		Measurement: &domain.VerdictMeasurement{
			Entries: []domain.MeasurementEntry{
				{Key: "maize", Value: 12.6},
				{Key: "soya", Value: 1},
			},
			VarianceAcknowledged: true,
		},
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %q, want approved", decided.Status)
	}
	if len(applier.applies) != 1 || !applier.applies[0].VarianceAcknowledged {
		t.Fatalf("applier applies = %+v, want one apply carrying VarianceAcknowledged", applier.applies)
	}
}

// The flag is meaningless on a reject and is dropped with the rest of the measurement: rejection
// sends the bag back to be packed again.
func TestAcknowledgementNeverRidesAReject(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())
	if _, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionRejected, Reason: "bag not shown",
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "reject-key-confirm-1",
		Measurement: &domain.VerdictMeasurement{
			Entries:              []domain.MeasurementEntry{{Key: "maize", Value: 12.6}, {Key: "soya", Value: 1}},
			VarianceAcknowledged: true,
		},
	}); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if len(applier.applies) != 0 {
		t.Fatalf("a reject must never reach the applier, got %d applies", len(applier.applies))
	}
}
