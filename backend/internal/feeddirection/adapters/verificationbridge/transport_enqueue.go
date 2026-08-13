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
	// NO subject label: a transport item is one shed's daily task, and the card already renders that
	// shed from its own location fields. Composing "Feed transport · <shed>" here printed the shed a
	// SECOND time on every card, beside the category chip that already says Feed Transport.
	label := ""
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{TenantID: in.TenantID, Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed, Category: feeddirectiondomain.VerificationCategoryTransport, SubjectLabel: ptrIfSet(label), Source: verificationdomain.SourceRef{Module: feeddirectiondomain.VerificationModuleFeed, RefType: feeddirectiondomain.VerificationRefTypeTransport, RefID: in.AttemptID}, MediaRefs: []string{in.ProofRef}, OperatorID: ptrIfSet(in.OperatorID), ShedID: ptrIfSet(in.ShedID), PartitionLabel: ptrIfSet(in.PartitionLabel), ParkID: ptrIfSet(in.ParkID), CapturedAt: in.CapturedAt, IdempotencyKey: in.IdempotencyKey})
	return err
}
