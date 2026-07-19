// Package app orchestrates feed-direction generation.
//
// The orchestration is deliberately thin and always the same three bounded reads, in this order:
//
//  1. read the FULL FILTERED SHED SCOPE (unpaged, bounded by the park's active shed catalog);
//  2. batch-read the authored config snapshot for the park (one call, set-based queries);
//  3. batch-read the projected grains for EXACTLY those sheds (one call, shed-set filter).
//
// Then ONE pure in-memory generation over the whole scope, from which the requested page is sliced.
// There is no per-shed read, no per-grain read, and no loop containing a ctx-taking call to an
// injected dependency -- the N+1 fan-out shape the scale rules ban, which a raw-driver check cannot
// see because the query sits an adapter layer down. The read count is CONSTANT: three, whatever the
// page size and whatever the park.
//
// The scope is read whole rather than paged because the summary must cover every row matching the
// filters, and a feed quantity cannot be aggregated in SQL -- see domain.PreviewSummary for the
// full argument. Both scope reads fail closed rather than truncate.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	// DefaultShedPageLimit is the shed page size. Sized to a screen of sheds, not to a park.
	DefaultShedPageLimit = int32(25)
	// MaxShedPageLimit bounds one page.
	MaxShedPageLimit = int32(100)
	// MaxShedPageOffset bounds the OFFSET walk over the park's shed catalog.
	//
	// Bounded LIMIT/OFFSET rather than keyset is defensible HERE and would not be on an event
	// stream: the paged set is the park's shed catalog, a small, stable configuration list (63
	// sheds in the largest live park), not a growing feed. The offset cannot grow with the herd,
	// and a caller past this bound is not reading a screen.
	MaxShedPageOffset = int32(5000)
)

// Service generates feed direction and the packing worklist.
type Service struct {
	config   ports.ConfigRepository
	counts   ports.ShedCountsReader
	rounding domain.RoundingPolicy
	planners domain.PlannerSet
}

func NewService(config ports.ConfigRepository, counts ports.ShedCountsReader) *Service {
	return &Service{
		config:   config,
		counts:   counts,
		rounding: domain.StandardRoundingPolicy(),
		planners: domain.NewPlannerSet(),
	}
}

// WithRoundingPolicy overrides the rounding policy. Present so a test can pin a policy explicitly
// and so the baking-soda seam can be wired without touching the pipeline; production uses
// StandardRoundingPolicy.
func (s *Service) WithRoundingPolicy(policy domain.RoundingPolicy) *Service {
	s.rounding = policy
	return s
}

// Preview generates one page of feed direction rows.
func (s *Service) Preview(ctx context.Context, q domain.PreviewQuery) (domain.PreviewPage, error) {
	normalized, err := normalizePreviewQuery(q)
	if err != nil {
		return domain.PreviewPage{}, err
	}

	result, err := s.generate(ctx, generateRequest{
		tenantID:   normalized.TenantID,
		parkID:     normalized.ParkID,
		targetDate: normalized.TargetDate,
		shedID:     normalized.ShedID,
		sessionNo:  normalized.SessionNo,
		limit:      normalized.Limit,
		offset:     normalized.Offset,
	})
	if err != nil {
		return domain.PreviewPage{}, err
	}

	return domain.PreviewPage{
		Items: result.pageRows,
		// Summarized over scopeRows -- every row matching the filters -- NOT over pageRows. The two
		// come from the same generation, so the totals and the visible rows cannot disagree.
		//
		// Column order follows the park's authored packing SLOTS, not the tenant catalog, so the
		// summary's columns read in the order a packer fills bags. Items seen in the rows but not
		// declared (an experiment shed's hand-entered feeds) are still appended by SummarizeScope
		// rather than dropped.
		Summary:    domain.SummarizeScope(result.scopeRows, result.config.PlannedFeedItems()),
		TargetDate: biztime.BusinessDate(normalized.TargetDate),
		Limit:      result.limit,
		Offset:     result.offset,
		HasMore:    result.hasMore,
	}, nil
}

// PackingWorklist generates one page of the per-shed packing view.
//
// It is built from the SAME generated rows as the preview -- not from a second, independently
// rounded computation -- so the bag a packer fills always matches the sheet the direction printed.
// Read-only: no proof capture, no video, no packing status is recorded anywhere. The status field
// is derived from the generation result.
func (s *Service) PackingWorklist(ctx context.Context, q domain.PackingQuery) (domain.PackingPage, error) {
	normalized, err := normalizePackingQuery(q)
	if err != nil {
		return domain.PackingPage{}, err
	}

	result, err := s.generate(ctx, generateRequest{
		tenantID:   normalized.TenantID,
		parkID:     normalized.ParkID,
		targetDate: normalized.TargetDate,
		limit:      normalized.Limit,
		offset:     normalized.Offset,
	})
	if err != nil {
		return domain.PackingPage{}, err
	}

	items := result.config.PlannedFeedItems()
	return domain.PackingPage{
		Items: domain.BuildPackingRows(result.pageRows, items),
		// Built from scopeRows, so the store draw covers the whole worklist rather than the visible
		// page. BuildPackingRows is deliberately run twice over two different row sets rather than
		// once and then filtered: a shed's grains must be collapsed into its bag using only that
		// shed's rows, which both calls satisfy, and a line's total is identical in both because the
		// page rows for a shed are exactly that shed's scope rows -- paging is by shed.
		Summary:    domain.SummarizePacking(domain.BuildPackingRows(result.scopeRows, items), items),
		TargetDate: biztime.BusinessDate(normalized.TargetDate),
		Limit:      result.limit,
		Offset:     result.offset,
		HasMore:    result.hasMore,
	}, nil
}

type generateRequest struct {
	tenantID   string
	parkID     string
	targetDate time.Time
	shedID     string
	sessionNo  int32
	limit      int32
	offset     int32
}

// generateResult carries one generation run: the whole filtered scope, and the page sliced out of
// it.
//
// scopeRows and pageRows come from the SAME GenerateDirection call, which is the property that
// makes the summary trustworthy: the totals are the sum of rows the operator can page through and
// verify, not a parallel computation that could drift from them.
type generateResult struct {
	scopeRows []domain.DirectionRow
	pageRows  []domain.DirectionRow
	config    domain.ConfigSnapshot
	limit     int32
	offset    int32
	hasMore   bool
}

// generate runs the three bounded reads and the pure generation shared by both surfaces.
//
// # WHY THE SCOPE IS GENERATED, NOT JUST THE PAGE
//
// The summary must cover every row matching the filters (see domain.PreviewSummary), and a feed
// quantity is not reconstructable in SQL -- the rounding step is per-cell and non-linear, so the
// only honest park total is the sum of the cells this generator actually produced. So the scope is
// generated once and the page is sliced from it, rather than the page being generated and the
// summary being guessed from it.
//
// THE READ COUNT IS STILL CONSTANT: one shed-scope read, one config snapshot, one batched grain
// read, whatever the page size. Nothing here scales with sheds or grains, and no loop below issues
// I/O -- they only collect ids and slice already-materialized rows.
//
// PAGING BY SHED IS PRESERVED EXACTLY. The page is sliced out of the ordered SHED SCOPE and every
// row of each selected shed travels with it, so a shed's grains still cannot straddle a page
// boundary -- which would present two partial session totals as if each were complete. Slicing
// sheds rather than rows is what keeps that invariant; slicing rows would break it.
func (s *Service) generate(ctx context.Context, req generateRequest) (generateResult, error) {
	// READ 1 -- the FULL filtered shed scope, unpaged. Bounded by the park's active shed catalog
	// (physical infrastructure), not by herd size, and it fails closed rather than truncating.
	scope, err := s.config.ListShedScope(ctx, ports.ShedScopeQuery{
		TenantID: req.tenantID,
		ParkID:   req.parkID,
		ShedID:   req.shedID,
	})
	if err != nil {
		return generateResult{}, err
	}

	// READ 2 -- the authored config for the park, once. Not per shed, not per grain.
	config, err := s.config.LoadConfigSnapshot(ctx, req.tenantID, req.parkID, req.targetDate)
	if err != nil {
		return generateResult{}, err
	}

	pageSheds, hasMore := sliceShedPage(scope.Items, req.limit, req.offset)
	if len(scope.Items) == 0 {
		return generateResult{
			scopeRows: []domain.DirectionRow{},
			pageRows:  []domain.DirectionRow{},
			config:    config,
			limit:     req.limit,
			offset:    req.offset,
		}, nil
	}

	scopeShedIDs := make([]string, 0, len(scope.Items))
	for _, shed := range scope.Items {
		scopeShedIDs = append(scopeShedIDs, shed.ShedID)
	}

	// READ 3 -- every projected grain for the WHOLE scope, in ONE batched call. The loop above only
	// collects ids; it issues no I/O.
	grains, err := s.counts.ProjectedGrainsForSheds(ctx, ports.ProjectedGrainsRequest{
		TenantID:   req.tenantID,
		ParkID:     req.parkID,
		TargetDate: req.targetDate,
		ShedIDs:    scopeShedIDs,
	})
	if err != nil {
		return generateResult{}, err
	}

	sheds := make([]domain.ShedInput, 0, len(scope.Items))
	for _, shed := range scope.Items {
		sheds = append(sheds, domain.ShedInput{
			ShedID:    shed.ShedID,
			ShedLabel: shed.Label,
			Grains:    grains[shed.ShedID],
		})
	}

	scopeRows := domain.GenerateDirection(domain.GenerateInput{
		Config:    config,
		Sheds:     sheds,
		SessionNo: req.sessionNo,
		Rounding:  s.rounding,
		Planners:  s.planners,
	})

	return generateResult{
		scopeRows: scopeRows,
		pageRows:  rowsForSheds(scopeRows, pageSheds),
		config:    config,
		limit:     req.limit,
		offset:    req.offset,
		hasMore:   hasMore,
	}, nil
}

// sliceShedPage takes the requested page out of the ordered shed scope.
//
// HasMore is "are there sheds after this page", computed from the scope that is already in hand --
// not a second COUNT query. An offset past the end yields an empty page rather than an error: that
// is a caller walking off the end of a shrinking park, not a bad request.
func sliceShedPage(scope []ports.Shed, limit, offset int32) ([]ports.Shed, bool) {
	start := int(offset)
	if start > len(scope) {
		start = len(scope)
	}
	end := start + int(limit)
	if end > len(scope) {
		end = len(scope)
	}
	return scope[start:end], end < len(scope)
}

// rowsForSheds narrows the generated scope to the sheds on the page, preserving generation order.
//
// A pure slice-and-filter over rows already in memory: no I/O, and no regeneration that could round
// differently from the rows the summary counted.
func rowsForSheds(rows []domain.DirectionRow, sheds []ports.Shed) []domain.DirectionRow {
	if len(sheds) == 0 {
		return []domain.DirectionRow{}
	}
	wanted := make(map[string]struct{}, len(sheds))
	for _, shed := range sheds {
		wanted[shed.ShedID] = struct{}{}
	}
	out := make([]domain.DirectionRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := wanted[row.ShedID]; ok {
			out = append(out, row)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Query normalization
// ---------------------------------------------------------------------------

func normalizePreviewQuery(q domain.PreviewQuery) (domain.PreviewQuery, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	q.ParkID = strings.TrimSpace(q.ParkID)
	q.ShedID = strings.TrimSpace(q.ShedID)
	if q.TenantID == "" || q.ParkID == "" {
		return domain.PreviewQuery{}, ports.ErrParkRequired
	}
	if q.TargetDate.IsZero() {
		return domain.PreviewQuery{}, ports.ErrInvalidTargetDate
	}
	// Normalized to a business-day start in Asia/Kolkata AT THE BOUNDARY, so a caller that passed a
	// late-evening UTC instant cannot push the whole generation onto the wrong feed day. Per
	// AGENTS.md, UTC never defines a Goat OS business day.
	q.TargetDate = biztime.BusinessDayStart(q.TargetDate)

	if q.SessionNo < 0 {
		return domain.PreviewQuery{}, ports.ErrInvalidPaging
	}
	limit, offset, err := normalizePaging(q.Limit, q.Offset)
	if err != nil {
		return domain.PreviewQuery{}, err
	}
	q.Limit, q.Offset = limit, offset
	return q, nil
}

func normalizePackingQuery(q domain.PackingQuery) (domain.PackingQuery, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	q.ParkID = strings.TrimSpace(q.ParkID)
	if q.TenantID == "" || q.ParkID == "" {
		return domain.PackingQuery{}, ports.ErrParkRequired
	}
	if q.TargetDate.IsZero() {
		return domain.PackingQuery{}, ports.ErrInvalidTargetDate
	}
	q.TargetDate = biztime.BusinessDayStart(q.TargetDate)

	limit, offset, err := normalizePaging(q.Limit, q.Offset)
	if err != nil {
		return domain.PackingQuery{}, err
	}
	q.Limit, q.Offset = limit, offset
	return q, nil
}

// normalizePaging applies the declared default to an ABSENT value and REJECTS a present
// out-of-range one. A bad limit is never silently clamped: per AGENTS.md, a present-but-invalid
// value fails rather than being rewritten to something the caller never asked for.
func normalizePaging(limit, offset int32) (int32, int32, error) {
	if limit == 0 {
		limit = DefaultShedPageLimit
	}
	if limit < 0 || limit > MaxShedPageLimit {
		return 0, 0, fmt.Errorf("%w: limit must be between 1 and %d", ports.ErrInvalidPaging, MaxShedPageLimit)
	}
	if offset < 0 || offset > MaxShedPageOffset {
		return 0, 0, fmt.Errorf("%w: offset must be between 0 and %d", ports.ErrInvalidPaging, MaxShedPageOffset)
	}
	return limit, offset, nil
}
