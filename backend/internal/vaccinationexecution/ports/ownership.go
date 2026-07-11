package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// ShedOwnershipReader is the cross-module read boundary the vaccination-execution service uses to attach
// a shed's Manager and Backup to shed-wise rows. It is implemented by an adapter over the workforce
// RosterService (Manager = shed-scoped Position holder; Backup = the center Backup Manager slot),
// keeping shed ownership as WORKFORCE-owned data read through a port — never a vaccination-owned table.
//
// A nil manager/backup means the assignment is genuinely absent (a seed/config gap surfaced in staging
// preflight), not an error and never invented. Errors are reserved for real infrastructure faults.
type ShedOwnershipReader interface {
	ShedOwnership(ctx context.Context, tenantID, shedID, parkID string, at time.Time) (manager, backup *domain.ShedOwner, err error)
	ShedOwnerships(ctx context.Context, tenantID string, sheds []domain.ShedOwnershipScope, at time.Time) (map[string]domain.ShedOwnership, error)
}

// NoopShedOwnership is a ShedOwnershipReader that always reports "no assignment", used where ownership
// resolution is not wired (tests, or a deployment without the roster module). Every shed then reads as a
// manager/backup seed gap — the honest default, never a fabricated owner.
type NoopShedOwnership struct{}

func (NoopShedOwnership) ShedOwnership(context.Context, string, string, string, time.Time) (*domain.ShedOwner, *domain.ShedOwner, error) {
	return nil, nil, nil
}

func (NoopShedOwnership) ShedOwnerships(context.Context, string, []domain.ShedOwnershipScope, time.Time) (map[string]domain.ShedOwnership, error) {
	return map[string]domain.ShedOwnership{}, nil
}
