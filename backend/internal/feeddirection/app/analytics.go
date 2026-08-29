package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Feed Analytics: the windowed DIRECTED rollup. Serving-only — no generation,
// no lifecycle writes, no completion overlay. See the grain contract on
// domain.DirectedAnalytics and the SQL notes in adapters/postgres/analytics.go.

// DirectedAnalyticsInput is one authorized analytics read.
type DirectedAnalyticsInput struct {
	TenantID string
	// ParkID is the selected park; empty means every authorized park.
	ParkID string
	// AuthorizedParkIDs is the caller's park capability set; empty means
	// tenant-wide. Resolved by the HTTP layer's park-scope middleware, applied
	// here so a park-scoped caller can never widen the rollup past their grant.
	AuthorizedParkIDs []string
	DateFrom          time.Time
	DateTo            time.Time
	// Sections narrows the EXECUTION read to the arms the caller renders; empty means all.
	Sections []domain.ExecutionSection
	// PackingVarianceLimit / PackingVarianceOffset page the mismatch list; zero limit takes the
	// contract default.
	PackingVarianceLimit       int
	PackingVarianceOffset      int
	PackingVarianceParkLabel   string
	PackingVarianceFeedItemKey string
	// WastageDay is the single business day the experiment read's per-pen
	// wastage table describes; zero lets the adapter default it. Ignored by
	// the directed/execution/stock reads.
	WastageDay time.Time
	// CompletionDay is the single business day the execution read's per-pen-session completion table
	// describes; zero lets the adapter default it (yesterday, IST). CompletionLimit/Offset page that
	// table and CompletionParkID/ShedID/Status narrow it. All ignored by the other reads.
	CompletionDay    time.Time
	CompletionLimit  int
	CompletionOffset int
	CompletionParkID string
	CompletionShedID string
	CompletionStatus string
}

// WithAnalyticsReader wires the directed-analytics rollup read. Optional: a pure
// generation unit test never touches it.
func (s *Service) WithAnalyticsReader(reader ports.DirectedAnalyticsReader) *Service {
	s.analytics = reader
	return s
}

// DirectedAnalytics serves the day and per-item directed series for one window.
func (s *Service) DirectedAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.DirectedAnalytics, error) {
	if s.analytics == nil {
		return domain.DirectedAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.DirectedAnalytics{}, err
	}
	return s.analytics.DirectedAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs:  parkIDs,
		DateFrom: in.DateFrom,
		DateTo:   in.DateTo,
	})
}

// analyticsParkFilter narrows to the selected park when one is chosen, otherwise
// to the caller's authorized set. A malformed id is rejected, never ignored — an
// ignored filter would silently widen the read.
func analyticsParkFilter(selected string, authorized []string) ([]uuid.UUID, error) {
	if selected != "" {
		id, err := uuid.Parse(selected)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ports.ErrParkNotFound, err)
		}
		return []uuid.UUID{id}, nil
	}
	out := make([]uuid.UUID, 0, len(authorized))
	for _, raw := range authorized {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ports.ErrParkNotFound, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// ExecutionAnalytics serves the per-day completion-status counts and verify
// latency for one window.
func (s *Service) ExecutionAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.ExecutionAnalytics, error) {
	if s.analytics == nil {
		return domain.ExecutionAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.ExecutionAnalytics{}, err
	}
	result, err := s.analytics.ExecutionAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs: parkIDs, DateFrom: in.DateFrom, DateTo: in.DateTo, Sections: in.Sections,
		PackingVarianceLimit: in.PackingVarianceLimit, PackingVarianceOffset: in.PackingVarianceOffset,
		PackingVarianceParkLabel: in.PackingVarianceParkLabel, PackingVarianceFeedItemKey: in.PackingVarianceFeedItemKey,
		CompletionDay: in.CompletionDay, CompletionLimit: in.CompletionLimit, CompletionOffset: in.CompletionOffset,
		CompletionParkID: in.CompletionParkID, CompletionShedID: in.CompletionShedID,
		CompletionStatus: in.CompletionStatus,
	})
	if err != nil {
		return domain.ExecutionAnalytics{}, err
	}
	s.describeDistributionProofs(ctx, in.TenantID, result.DistributionCompletions)
	return result, nil
}

// ExperimentAnalytics serves the trial arms' authored absolute-kg series.
func (s *Service) ExperimentAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.ExperimentAnalytics, error) {
	if s.analytics == nil {
		return domain.ExperimentAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.ExperimentAnalytics{}, err
	}
	return s.analytics.ExperimentAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs: parkIDs, DateFrom: in.DateFrom, DateTo: in.DateTo, WastageDay: in.WastageDay,
	})
}

// StockAnalytics serves the purchase-ledger stock cards and expenditure series.
func (s *Service) StockAnalytics(ctx context.Context, in DirectedAnalyticsInput) (domain.StockAnalytics, error) {
	if s.analytics == nil {
		return domain.StockAnalytics{}, fmt.Errorf("feeddirection: analytics reader is not wired")
	}
	parkIDs, err := analyticsParkFilter(in.ParkID, in.AuthorizedParkIDs)
	if err != nil {
		return domain.StockAnalytics{}, err
	}
	return s.analytics.StockAnalytics(ctx, in.TenantID, domain.DirectedAnalyticsQuery{
		ParkIDs: parkIDs, DateFrom: in.DateFrom, DateTo: in.DateTo,
	})
}

// describeDistributionProofs fills in WHO uploaded each of a pen-session's three proofs and WHEN,
// for every row of the completion table at once.
//
// ONE batched call for the whole PAGE, never one per row: the reference set is collected across
// every row first and resolved in a single DescribeProofUploads, so a page of ten pen-sessions costs
// one round trip rather than ten (the n-plus-one-fanout rule -- the real query sits one adapter
// layer down, which is exactly the shape that hides from the raw-driver check).
//
// Best-effort by design: a proof module that is not wired, or a lookup that fails, leaves the slots
// carrying their reference with no provenance. The table's own answer -- which pen was fed, which
// was not -- comes from the completion rows and stays correct either way, so a provenance failure
// must not take the screen down.
func (s *Service) describeDistributionProofs(ctx context.Context, tenantID string, rows []domain.DistributionCompletionRow) {
	if s.proofUploads == nil || len(rows) == 0 {
		return
	}
	refs := make([]string, 0, len(rows)*len(domain.DistributionSlotOrder))
	for _, row := range rows {
		for _, slot := range row.Proofs {
			if slot.ProofRef != "" {
				refs = append(refs, slot.ProofRef)
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	uploads, err := s.proofUploads.DescribeProofUploads(ctx, tenantID, refs)
	if err != nil {
		return
	}
	for i := range rows {
		for j := range rows[i].Proofs {
			slot := &rows[i].Proofs[j]
			upload, ok := uploads[slot.ProofRef]
			if !ok {
				continue
			}
			uploadedAt := upload.UploadedAt
			slot.UploadedAt = &uploadedAt
			slot.UploadedByName = upload.UploadedByName
			if slot.MimeType == "" {
				slot.MimeType = upload.MimeType
			}
		}
	}
}
