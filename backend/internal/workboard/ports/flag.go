package ports

import (
	"context"
	"errors"

	ltdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
)

// A FLAG is the director's phone call made visible (the Work Board Build Plan, phase 5):
// raised from a board row, owned by the park head, carried as a Leadership Task so it has a
// number, a status ladder, notes, and a push -- none of which the board has to build. The
// board composes the brief; the Leadership Tasks module records it.

// FlagRaiser is the Leadership Tasks service's Raise, behind a port so the board never
// imports the module's app package.
type FlagRaiser interface {
	Raise(ctx context.Context, p ltports.RaiseParams) (ltdomain.Task, error)
}

// ParkHeadResolver names the park head a flag is addressed to.
type ParkHeadResolver interface {
	// ParkHead returns the user id and display name of the park's head, or
	// ErrParkHeadMissing when the park has none.
	ParkHead(ctx context.Context, tenantID, parkID string) (ParkHead, error)
}

// ParkHead is one park's head.
type ParkHead struct {
	UserID string
	Name   string
}

// ErrParkHeadMissing means no active park_head grant covers the park. The flag is refused
// rather than sent to a fallback; an unowned flag is exactly the silent drop the kernel bans.
var ErrParkHeadMissing = errors.New("workboard: park has no park head")

// FlagParams is what the director's screen sends. The row fields are the backend-owned
// strings the board served that row with; the note is the director's own words.
type FlagParams struct {
	TenantID         string
	ActorID          string
	ActorDesignation string
	ParkID           string
	RowKey           string
	RowTitle         string
	RowSubtitle      string
	PenDisplay       string
	ClockLabel       string
	Note             string
	IdempotencyKey   string
}

// FlagResult is what the screen shows back.
type FlagResult struct {
	TaskID       string
	TaskNo       int64
	AssigneeName string
}
