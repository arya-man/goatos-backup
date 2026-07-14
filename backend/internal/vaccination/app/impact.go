package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ErrImpactPreviewUnsupported is returned when the wired repository cannot serve the aggregate impact
// preview read model. Every ports.Repository satisfies impactReads in practice; this guards a mis-wired
// or partial repository.
var ErrImpactPreviewUnsupported = errors.New("vaccination: impact preview read model not available")

// impactReads is the NARROW read surface the config impact preview is allowed to touch. It deliberately
// excludes the live goat-count methods (CountEligibleGoats/...): the preview must be aggregate-only and
// read the vaccination_eligibility_rollups read model, never scan goats on the UI request path. The
// compile-time interface is the guard — a future edit that reaches for a goat scan won't satisfy it.
type impactReads interface {
	SumEligibilityRollup(ctx context.Context, f domain.ImpactFilter) (domain.EligibilityRollupAggregate, error)
	CapacityMaxPerDay(ctx context.Context, tenantID string) (int64, error)
	SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (string, *time.Time, error)
}

// ImpactPreview computes the aggregate config impact preview for a vaccination rule/version from the
// precomputed eligibility rollup: eligible animals, vaccination cells, affected sheds, and estimated
// days at the configured daily cap. It reads ONLY the read model (plus a cheap optional stock lookup) —
// no live goats scan — so the "Preview impact" button stays cheap across the current 5,000-50,000-animal
// release envelope (up to the ~500k obligation-row upper bound; 1-5M is the future certification bar —
// see docs/decisions/operational-kernel-5k-50k-scale-envelope.md). Per-animal, workflow, session-plan,
// and manager/backup detail belong after publish/planner execution, not here.
func (s *Service) ImpactPreview(ctx context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error) {
	reads, ok := s.repo.(impactReads)
	if !ok {
		return domain.ImpactPreview{}, ErrImpactPreviewUnsupported
	}

	filter := req.Filter
	if filter.AsOf.IsZero() {
		filter.AsOf = req.AsOf
	}
	if filter.AsOf.IsZero() {
		filter.AsOf = time.Now().In(biztime.DefaultLocation())
	}

	agg, err := reads.SumEligibilityRollup(ctx, filter)
	if err != nil {
		return domain.ImpactPreview{}, err
	}

	doseRows := req.DoseRows
	if doseRows < 1 {
		doseRows = 1
	}
	// Draft cap (authored in the rule editor) wins for pre-publish preview; fall back to the published/
	// operational cap when the request omits it.
	cap := req.DailyCap
	if cap < 1 {
		cap, err = reads.CapacityMaxPerDay(ctx, filter.TenantID)
		if err != nil {
			return domain.ImpactPreview{}, err
		}
	}
	if cap < 1 {
		cap = 1
	}

	cells := agg.EligibleAnimals * int64(doseRows)
	estimatedDays := ceilDiv(cells, cap)
	out := domain.ImpactPreview{
		EligibleAnimals:  agg.EligibleAnimals,
		VaccinationCells: cells,
		AffectedSheds:    agg.AffectedSheds,
		EstimatedDays:    estimatedDays,
		DailyCap:         cap,
		CapacityStatus:   classifyCapacity(cells, estimatedDays, req.MaxBufferDays),
		PlannedSessions:  planImpactSessions(cells, cap, estimatedDays, req.MaxBufferDays, filter.AsOf),
		SourceRevision:   agg.SourceRevision,
		RecomputedAt:     agg.RecomputedAt,
	}
	if cells > 0 && estimatedDays > maxImpactPlannedSessions {
		out.Warnings = append(out.Warnings,
			"planned session preview truncated at "+strconv.FormatInt(maxImpactPlannedSessions, 10)+" days; estimated_days="+strconv.FormatInt(estimatedDays, 10))
	}
	if agg.SourceRevision == 0 {
		out.Warnings = append(out.Warnings,
			"eligibility rollup has no data for this scope yet — run vaccination-eligibility-rollup-recompute after seed/import")
	}

	if req.VaccineItemID != nil && *req.VaccineItemID != "" {
		available, earliest, err := reads.SumAvailableStock(ctx, filter.TenantID, *req.VaccineItemID, req.LocationID)
		if err != nil {
			return domain.ImpactPreview{}, err
		}
		out.DosesAvailable = available
		out.EarliestExpiry = earliest
		if availNum, ok := parseNumeric(available); ok && float64(out.VaccinationCells) > availNum {
			out.Warnings = append(out.Warnings,
				"stock shortage: required "+strconv.FormatInt(out.VaccinationCells, 10)+" > available "+available)
		}
		if earliest != nil && req.HorizonDays > 0 {
			if earliest.Before(filter.AsOf.AddDate(0, 0, req.HorizonDays)) {
				out.Warnings = append(out.Warnings, "stock expiry before horizon: earliest "+earliest.Format("2006-01-02"))
			}
		}
	} else {
		out.Warnings = append(out.Warnings, "no vaccine item set — stock not evaluated")
	}

	return out, nil
}

// ceilDiv returns ceil(n / d) for non-negative n and positive d, using integer math (no float drift at
// million scale).
// previewDefaultMaxBufferDays is the business-rule safe-window buffer (2026-07-11) applied when a preview
// request omits the draft buffer, so the classification never silently assumes a 0-day window.
const previewDefaultMaxBufferDays = 7

// maxImpactPlannedSessions bounds the payload for high-scale previews. The preview still returns the full
// EstimatedDays headline, but avoids sending tens of thousands of day rows to the browser.
const maxImpactPlannedSessions = 366

// classifyCapacity mirrors the session-splitting planner headline (PlanSessions /
// shedSummaryCanonicalReadSQL):
// sessions = estimatedDays; within_cap fits one day, over_cap fits the safe window (buffer + 1 days),
// capacity_breach spills beyond it. Empty when there are no cells to plan. Returns the machine value; the
// UI renders the CEO label (within_cap→"Within cap", over_cap→"Split", capacity_breach→"Needs review").
func classifyCapacity(cells, estimatedDays int64, maxBufferDays *int64) string {
	if cells <= 0 {
		return ""
	}
	buffer := int64(previewDefaultMaxBufferDays)
	if maxBufferDays != nil && *maxBufferDays >= 0 {
		buffer = *maxBufferDays
	}
	switch {
	case estimatedDays <= 1:
		return "within_cap"
	case estimatedDays <= buffer+1:
		return "over_cap"
	default:
		return "capacity_breach"
	}
}

func planImpactSessions(cells, cap, estimatedDays int64, maxBufferDays *int64, start time.Time) []domain.ImpactPlannedSession {
	if cells <= 0 || cap < 1 || estimatedDays <= 0 {
		return []domain.ImpactPlannedSession{}
	}
	if start.IsZero() {
		start = time.Now().In(biztime.DefaultLocation())
	}
	allowedDays := effectiveBufferDays(maxBufferDays) + 1
	if allowedDays < 1 {
		allowedDays = 1
	}
	limit := estimatedDays
	if limit > maxImpactPlannedSessions {
		limit = maxImpactPlannedSessions
	}
	planned := make([]domain.ImpactPlannedSession, 0, limit)
	remaining := cells
	for i := int64(0); i < limit; i++ {
		vax := cap
		if remaining < cap {
			vax = remaining
		}
		remaining -= vax
		perDay := "within_cap"
		if i >= allowedDays {
			perDay = "capacity_breach"
		}
		planned = append(planned, domain.ImpactPlannedSession{
			Date:         biztime.BusinessDate(start.AddDate(0, 0, int(i))),
			Vaccinations: vax,
			DailyLimit:   cap,
			Capacity:     perDay,
		})
	}
	return planned
}

func effectiveBufferDays(maxBufferDays *int64) int64 {
	buffer := int64(previewDefaultMaxBufferDays)
	if maxBufferDays != nil && *maxBufferDays >= 0 {
		buffer = *maxBufferDays
	}
	return buffer
}

func ceilDiv(n, d int64) int64 {
	if d <= 0 {
		return 0
	}
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func parseNumeric(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
