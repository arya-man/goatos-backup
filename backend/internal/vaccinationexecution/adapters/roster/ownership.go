// Package roster adapts the workforce RosterService to the vaccination-execution ShedOwnershipReader
// port, so the shed-wise table can attach a shed's Manager/Backup without the vaccination module knowing
// about workforce tables. Manager = shed-scoped Position holder; Backup = the center Backup Manager slot
// (resolved by the roster service). A missing seat maps to a nil owner (seed/config gap), never invented.
package roster

import (
	"context"
	"time"

	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	workforced "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// RosterReader is the narrow slice of the workforce RosterService this adapter needs. *workforce/app.
// RosterService satisfies it structurally.
type RosterReader interface {
	ShedManager(ctx context.Context, tenantID, shedID string, at time.Time) (*workforced.Position, error)
	ShedBackup(ctx context.Context, tenantID, shedID, centerID string, at time.Time) (*workforced.Position, error)
}

type OwnershipAdapter struct {
	roster RosterReader
}

func NewOwnershipAdapter(roster RosterReader) *OwnershipAdapter {
	return &OwnershipAdapter{roster: roster}
}

// ShedOwnership resolves the shed's Manager (shed-scoped position holder) and Backup (center Backup
// Manager slot; centerID = parkID). Either may be nil (unassigned seat = seed/config gap).
func (a *OwnershipAdapter) ShedOwnership(ctx context.Context, tenantID, shedID, parkID string, at time.Time) (*vaccexecd.ShedOwner, *vaccexecd.ShedOwner, error) {
	manager, err := a.roster.ShedManager(ctx, tenantID, shedID, at)
	if err != nil {
		return nil, nil, err
	}
	backup, err := a.roster.ShedBackup(ctx, tenantID, shedID, parkID, at)
	if err != nil {
		return nil, nil, err
	}
	return ownerFromPosition(manager), ownerFromPosition(backup), nil
}

func ownerFromPosition(p *workforced.Position) *vaccexecd.ShedOwner {
	if p == nil {
		return nil
	}
	name := ""
	if p.PersonDisplayName != nil {
		name = *p.PersonDisplayName
	}
	return &vaccexecd.ShedOwner{WorkforceMemberID: p.WorkforceMemberID, DisplayName: name}
}
