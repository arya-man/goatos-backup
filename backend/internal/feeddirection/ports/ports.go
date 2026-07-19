// Package ports defines the feed-direction generation boundaries.
//
// This module OWNS NO TABLES. It is a read-only generator over two other modules' data, so its
// ports are deliberately narrow reads and there is no write path, no idempotency ledger, and no
// outbox here. Anything that needs to mutate feed configuration goes through feedconfig; anything
// that needs to record what was actually delivered goes through feed.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

var (
	// ErrParkRequired is returned when a request omits the park. The ration grid, the session
	// split, and the dispatch clock are ALL park-scoped, so a tenant-wide generation would mix two
	// parks' rations into one document. Failing closed is the only safe answer.
	ErrParkRequired = errors.New("feeddirection: park_id is required")
	// ErrParkNotFound is returned when the addressed park does not resolve to a locations row in
	// the caller's tenant.
	ErrParkNotFound = errors.New("feeddirection: park not found")
	// ErrInvalidTargetDate is returned for a missing or unparseable feed day.
	ErrInvalidTargetDate = errors.New("feeddirection: target_date is required and must be a business date (YYYY-MM-DD)")
	// ErrInvalidPaging is returned for a present-but-out-of-range limit/offset. A bad paging value
	// is rejected rather than clamped to a default the caller never asked for.
	ErrInvalidPaging = errors.New("feeddirection: limit/offset out of range")
	// ErrScopeTooLarge is returned when the FULL filtered scope -- the set the summary must cover --
	// exceeds what this module will read in one request.
	//
	// It exists so the summary can never be silently partial. A truncated scope produces a park
	// total that is quietly too small, which is indistinguishable from a park that genuinely needs
	// less feed: the operator packs short and nothing anywhere reports an error. That is the exact
	// defect class the whole-scope summary was built to remove, so hitting the bound must FAIL
	// rather than under-report. See the summary contract on domain.PreviewSummary.
	ErrScopeTooLarge = errors.New("feeddirection: filtered scope is too large to summarize in one request")
)

// Shed is one shed in the scope.
type Shed struct {
	ShedID string
	Label  string
}

// ShedScopeQuery selects EVERY shed matching the request filters, unpaged.
//
// It carries the same tenant/park/shed predicates as ShedPageQuery and deliberately no limit or
// offset: this is the set the summary must cover, and a summary that moved with the page is the
// defect this query exists to fix.
type ShedScopeQuery struct {
	TenantID string
	ParkID   string
	// ShedID optionally narrows to a single shed, on exactly the same terms as ShedPageQuery. The
	// scope must honour every filter the page honours, or the summary would describe a different
	// population than the rows beneath it.
	ShedID string
}

// ShedScope is the complete filtered shed set.
//
// There is no HasMore and no Truncated flag, on purpose. A partial scope has no honest
// representation here: the reader either returns the whole set or fails with ErrScopeTooLarge.
// A boolean saying "this total is incomplete" would still be rendered as a number an operator
// packs against.
type ShedScope struct {
	Items []Shed
}

// ConfigRepository reads the authored configuration a park's generation resolves against.
type ConfigRepository interface {
	// LoadConfigSnapshot reads the ENTIRE authored configuration for one park in a bounded batch of
	// set-based queries -- the tag vocabulary, the breed map, the item catalog, the currently-open
	// ration grid, the shed factors, the session split, and the experiment sheds.
	//
	// It is loaded ONCE per request, not per shed and never per grain. Resolving a rate through a
	// per-row lookup would be the N+1 fan-out the scale rules ban; the whole live grid is ~1,442
	// rows for the tenant and ~721 per park, so reading it whole is both bounded and cheaper than
	// the round trips it replaces.
	//
	// asOf selects the effective-dated rows in force on the target business date.
	LoadConfigSnapshot(ctx context.Context, tenantID, parkID string, asOf time.Time) (domain.ConfigSnapshot, error)

	// ListShedScope returns EVERY shed matching the request filters, so the summary can be computed
	// over the whole filtered set rather than over the visible page.
	//
	// THE PAGE UNIT IS STILL THE SHED: the service slices its page out of this ordered scope, so a
	// shed's ration grains never straddle a page boundary -- two partial session totals would each
	// look like a complete instruction for that shed. Paging moved into the service (it is a slice
	// of an already-bounded list) rather than staying a second, separately-predicated query that
	// could select a different population than the summary describes.
	//
	// BOUNDED BY PHYSICAL INFRASTRUCTURE, NOT BY HERD SIZE. The set is a park's active shed catalog
	// (78 in the largest live park), which grows when the farm builds sheds, not when it buys
	// animals. That is what makes an unpaged read defensible here and would not make it defensible
	// on an animal-, obligation-, or event-scoped set.
	//
	// It FAILS with ErrScopeTooLarge rather than truncating -- see that error's contract.
	ListShedScope(ctx context.Context, q ShedScopeQuery) (ShedScope, error)
}

// ShedCountsReader is the projected-count input, expressed in this module's terms.
//
// The implementation adapts backend/internal/counts, which owns the projection (live herd plus the
// movements that are approved but not yet executed). It is a port rather than a direct dependency
// so this module's generation logic stays testable without the counts service, and so the counts
// module keeps sole ownership of the census SQL.
type ShedCountsReader interface {
	// ProjectedGrainsForSheds returns every projected grain for the named sheds on the target date.
	//
	// It takes a SET of shed ids, not one id, deliberately: calling a single-shed read once per
	// shed in a loop is the N+1 fan-out the scale rules ban, and it would be invisible to a raw
	// driver check because the query sits an adapter layer down.
	//
	// COMPLETE OR ERROR, NEVER PARTIAL. The implementation drains the underlying projection until
	// the whole set is in hand and fails with ErrScopeTooLarge if it cannot. A silently truncated
	// grain read would drop sheds out of the summary and out of the sheet, which reads as "that
	// shed needs no feed" rather than as an error.
	ProjectedGrainsForSheds(ctx context.Context, req ProjectedGrainsRequest) (map[string][]domain.ShedGrain, error)
}

// ProjectedGrainsRequest names the sheds and the feed day.
type ProjectedGrainsRequest struct {
	TenantID string
	ParkID   string
	// TargetDate is the feed day, in the Goat OS business calendar.
	TargetDate time.Time
	// ShedIDs is the FILTERED SHED SCOPE -- every shed the request's filters select, which for an
	// unfiltered request is the park's active shed catalog.
	//
	// It was the visible page until the whole-scope summary landed. It had to widen: the summary
	// must cover every row matching the filters, and a grain read scoped to the page can only ever
	// produce a page-scoped total. Bounded by shed count, not herd size -- see ListShedScope.
	ShedIDs []string
}
