package app

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestReviewWindowTurnsBusinessDatesIntoInstants(t *testing.T) {
	from, to, err := reviewWindow("2026-09-14", "2026-09-14")
	if err != nil {
		t.Fatalf("one-day window: %v", err)
	}
	// Inclusive IST business dates: 14 Sep 00:00 IST up to (not including) 15 Sep 00:00 IST.
	if from.Format(time.RFC3339) != "2026-09-14T00:00:00+05:30" || to.Format(time.RFC3339) != "2026-09-15T00:00:00+05:30" {
		t.Fatalf("window = %s .. %s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	if f, tt, err := reviewWindow("", ""); err != nil || !f.IsZero() || !tt.IsZero() {
		t.Fatalf("open window: %v %v %v", err, f, tt)
	}
	if _, _, err := reviewWindow("2026-09-15", "2026-09-14"); err == nil {
		t.Fatal("an end before the start must be refused")
	}
	if _, _, err := reviewWindow("14/09/2026", ""); err == nil {
		t.Fatal("a non-ISO date must be refused")
	}
}
