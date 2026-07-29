package verificationbridge

import (
	"context"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type TransportEnqueuer struct{ verification verificationCreator }

func NewTransport(v verificationCreator) *TransportEnqueuer {
	return &TransportEnqueuer{verification: v}
}

var _ feeddirectionapp.FeedTransportVerificationEnqueuer = (*TransportEnqueuer)(nil)

func (e *TransportEnqueuer) EnqueueFeedTransportVerification(ctx context.Context, in feeddirectionapp.FeedTransportVerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{TenantID: in.TenantID, Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed, Category: feeddirectiondomain.VerificationCategoryTransport, SubjectLabel: ptrIfSet("Feed transport"), Source: verificationdomain.SourceRef{Module: feeddirectiondomain.VerificationModuleFeed, RefType: feeddirectiondomain.VerificationRefTypeTransport, RefID: in.AttemptID}, MediaRefs: []string{in.ProofRef}, OperatorID: ptrIfSet(in.OperatorID), ShedID: ptrIfSet(in.ShedID), ParkID: ptrIfSet(in.ParkID), CapturedAt: in.CapturedAt, IdempotencyKey: in.IdempotencyKey})
	return err
}
