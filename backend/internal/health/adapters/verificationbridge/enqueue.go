// Package verificationbridge holds the composition-layer adapter that connects the health module
// to the verification module's service API without either module importing the other's storage.
package verificationbridge

import (
	"context"
	"time"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	healthdomain "github.com/vgoats/goatos/backend/internal/health/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// TreatmentEnqueuer routes one completed treatment session's proof video into the health_adults /
// health_kids category for the session's cohort, so the verifier's Health tab pages match the
// operator worklists (Adults / Kids).
type TreatmentEnqueuer struct{ verification verificationCreator }

func New(v verificationCreator) *TreatmentEnqueuer { return &TreatmentEnqueuer{verification: v} }

var _ healthapp.TreatmentVerificationEnqueuer = (*TreatmentEnqueuer)(nil)

func (e *TreatmentEnqueuer) EnqueueTreatmentVerification(ctx context.Context, in healthapp.TreatmentVerificationEnqueueRequest) error {
	capturedAt := in.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     healthdomain.VerificationVerticalHealth,
		Module:       healthdomain.VerificationModuleHealth,
		Category:     healthdomain.VerificationCategoryForAgeBand(in.AgeBand),
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		Source: verificationdomain.SourceRef{
			Module:  healthdomain.VerificationModuleHealth,
			RefType: healthdomain.VerificationRefTypeTreatmentSession,
			RefID:   in.SessionID,
		},
		MediaRefs:      in.MediaRefs,
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     capturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
