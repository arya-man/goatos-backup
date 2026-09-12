package ports

import (
	"context"
	"errors"

	ltdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
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

// FlagParams is what the director's screen sends: WHICH row (its key), on WHICH board
// (the same bounds the read used), and the director's own words. The row's copy is never
// taken from the client: the service looks the row up on the caller's board, so a flag can
// only name work the caller can actually see, with the title the board really served.
type FlagParams struct {
	TenantID         string
	ActorID          string
	ActorDesignation string
	// Board is the caller's own read bounds: tenant, park, business date and the modules
	// the caller may see. The flagged row must be on it.
	Board          domain.Query
	RowKey         string
	Note           string
	IdempotencyKey string
}

// RowFinder is the board's own lookup of one row by key inside a caller's bounds.
type RowFinder interface {
	// FindRow returns the row the key names when it is on the board the query describes,
	// or found=false when it is not there (absent, another park or day, or a module the
	// caller cannot see). An unparseable key is domain.ErrInvalidRowKey.
	FindRow(ctx context.Context, q domain.Query, rowKey string) (row domain.Row, found bool, err error)
}

// FlagResult is what the screen shows back.
type FlagResult struct {
	TaskID       string
	TaskNo       int64
	AssigneeName string
}
