package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

type supersedingRepo struct {
	fakeRepo
	superseded []string
}

func (r *supersedingRepo) ReopenScope(context.Context, string, string, string, string, string, string) ([]string, error) {
	return r.superseded, nil
}

type captureWithdrawer struct {
	tenantID string
	refType  string
	refIDs   []string
	calls    int
	err      error
}

func (w *captureWithdrawer) WithdrawWeighingVerification(_ context.Context, tenantID, refType string, observationIDs []string) error {
	w.calls++
	w.tenantID = tenantID
	w.refType = refType
	w.refIDs = append([]string(nil), observationIDs...)
	return w.err
}

func reopenActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
}

// A reopen withdraws the bucket's lump-sum submission. The verification item raised
// for that submission points at the same observation id, so it must be retired --
// otherwise a verifier approves a submission the bucket no longer counts and the UI
// reports that non-decision as success.
func TestReopenScopeRetiresVerificationForSupersededSubmissions(t *testing.T) {
	observationID := "00000000-0000-4000-8000-000000000901"
	withdrawer := &captureWithdrawer{}
	service := NewService(&supersedingRepo{superseded: []string{observationID}}).
		WithVerificationWithdrawer(withdrawer)

	if err := service.ReopenScope(context.Background(), reopenActor(),
		"00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801",
		"reopen:withdraw", "video unusable"); err != nil {
		t.Fatalf("ReopenScope: %v", err)
	}
	if withdrawer.calls != 1 {
		t.Fatalf("verification withdrawals=%d, want 1", withdrawer.calls)
	}
	if withdrawer.refType != domain.VerificationRefTypeShed {
		t.Fatalf("ref_type=%q, want %q", withdrawer.refType, domain.VerificationRefTypeShed)
	}
	if withdrawer.tenantID != testTenant {
		t.Fatalf("tenant=%q, want %q", withdrawer.tenantID, testTenant)
	}
	if len(withdrawer.refIDs) != 1 || withdrawer.refIDs[0] != observationID {
		t.Fatalf("withdrawn ref ids=%v, want [%s]", withdrawer.refIDs, observationID)
	}
}

// Reopening an INDIVIDUAL bucket supersedes no lump-sum submission, so verification
// must not be called at all.
func TestReopenScopeSkipsVerificationWhenNothingSuperseded(t *testing.T) {
	withdrawer := &captureWithdrawer{}
	service := NewService(&supersedingRepo{}).WithVerificationWithdrawer(withdrawer)

	if err := service.ReopenScope(context.Background(), reopenActor(),
		"00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801",
		"reopen:none", "recheck"); err != nil {
		t.Fatalf("ReopenScope: %v", err)
	}
	if withdrawer.calls != 0 {
		t.Fatalf("verification withdrawals=%d, want 0", withdrawer.calls)
	}
}

// A failed withdrawal must SURFACE. Swallowing it leaves the item decidable with
// nobody aware, which is the exact silent-success shape this lane exists to remove.
func TestReopenScopeSurfacesVerificationWithdrawalFailure(t *testing.T) {
	boom := errors.New("verification unavailable")
	withdrawer := &captureWithdrawer{err: boom}
	service := NewService(&supersedingRepo{superseded: []string{"00000000-0000-4000-8000-000000000901"}}).
		WithVerificationWithdrawer(withdrawer)

	err := service.ReopenScope(context.Background(), reopenActor(),
		"00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801",
		"reopen:boom", "video unusable")
	if !errors.Is(err, boom) {
		t.Fatalf("ReopenScope error=%v, want the withdrawal failure", err)
	}
}
