package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
)

type refusingRepo struct{ ports.Repository }

func (refusingRepo) Decide(context.Context, ports.DecideParams) (domain.Candidate, error) {
	panic("the repository must not be reached with a key the envelope schema would refuse")
}

// A decision commits BEFORE the relay validates its outbox envelope, whose idempotency_key has
// minLength 8. A shorter key therefore lands the decision and silently drops the push (found on
// the edge-case pass with a two-character key). The service refuses it up front.
func TestDecideRefusesAnIdempotencyKeyTheEnvelopeSchemaWouldReject(t *testing.T) {
	s := NewService(refusingRepo{}, nil, nil)
	for _, key := range []string{"", "  ", "d8", "1234567"} {
		_, err := s.Decide(context.Background(), ports.DecideParams{TenantID: "t", CandidateID: "c", IdempotencyKey: key,
			Write: domain.DecisionWrite{Decision: "accept", RowVersion: 1}})
		if !errors.Is(err, ErrIdempotencyKeyRequired) {
			t.Fatalf("key %q: want ErrIdempotencyKeyRequired, got %v", key, err)
		}
	}
	if HTTPError(ErrIdempotencyKeyRequired).HTTPStatus != 400 {
		t.Fatal("a refused key must be a 400 the client can act on")
	}
}
