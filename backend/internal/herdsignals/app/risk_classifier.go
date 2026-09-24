package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// riskClassifierPageSize bounds one classifier batch (read page, enrichment and UNNEST write).
const riskClassifierPageSize = 2000

// RecomputeRisk classifies every tag of one tenant and persists the result (000402), so the
// live risk_state filter is an indexed predicate instead of a per-request whole-cohort walk.
//
// It runs the SAME classifier the live page used in memory -- enrichTagsBatch (24h p75
// baselines, mapped animal's pen) then applyRiskSignals against whole-pen medians -- one
// keyset page at a time, so memory and each statement stay bounded at any herd size. Returns
// the number of rows whose classification changed.
func (s *Service) RecomputeRisk(ctx context.Context, tenantID string) (int, error) {
	medians, err := s.repo.ListLivePenMedians(ctx, tenantID, nil, nil, nil, nil, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("pen medians: %w", err)
	}
	groupStats := make(map[string]riskGroupStats, len(medians))
	for pen, m := range medians {
		groupStats[pen] = riskGroupStats{motionMedian: m.MotionMedian, tempMedian: m.TempMedian}
	}
	evaluatedAt := time.Now().UTC()
	if err := s.repo.ReplacePenMedians(ctx, tenantID, medians, evaluatedAt); err != nil {
		return 0, fmt.Errorf("persist pen medians: %w", err)
	}
	changed := 0
	cursor := ""
	for {
		// scale-guard:ignore: bounded keyset worker walk owner=herd-signals issue=perf/herd-live reason=background risk classifier, fixed 2000-row pages, cursor strictly advances by tag_id, one batched enrichment + one UNNEST write per page expiry=2027-06-30
		tags, next, err := s.repo.ListTagsLatestKeyset(ctx, tenantID, nil, nil, nil, nil, nil, nil, nil, nil, cursor, riskClassifierPageSize, domain.LiveSort{Key: "smart_tag", Dir: "asc"})
		if err != nil {
			return changed, fmt.Errorf("risk page: %w", err)
		}
		// scale-guard:ignore: one batched enrichment per bounded page owner=herd-signals issue=perf/herd-live reason=enrichTagsBatch issues a fixed number of = ANY($1) reads per 2000-row page, not per row expiry=2027-06-30
		items := s.enrichTagsBatch(ctx, tenantID, tags, nil, false)
		applyRiskSignals(items, groupStats)
		rows := riskRowsFromItems(items)
		// scale-guard:ignore: one set-based UNNEST write per bounded page owner=herd-signals issue=perf/herd-live reason=UpdateTagRisk writes the whole 2000-row page in one statement expiry=2027-06-30
		n, err := s.repo.UpdateTagRisk(ctx, tenantID, rows, evaluatedAt)
		if err != nil {
			return changed, err
		}
		changed += n
		if next == nil || *next == "" {
			return changed, nil
		}
		if *next == cursor {
			return changed, fmt.Errorf("risk classifier cursor did not advance")
		}
		cursor = *next
	}
}

func riskRowsFromItems(items []domain.LiveItem) []ports.TagRisk {
	rows := make([]ports.TagRisk, len(items))
	for i, item := range items {
		rows[i] = ports.TagRisk{TagID: item.TagID, State: item.RiskState, Score: item.RiskScore, Reasons: item.RiskReasons}
		if item.RiskState == nil {
			rows[i].Reasons = nil
		}
	}
	return rows
}

// RunRiskClassifier re-classifies every tenant every interval until ctx ends. Only the instance
// holding the cluster-wide advisory lock runs a pass; the others skip it.
func (s *Service) RunRiskClassifier(ctx context.Context, interval time.Duration) {
	run := func() {
		passCtx, cancel := context.WithTimeout(ctx, interval)
		defer cancel()
		ran, err := s.repo.WithRiskClassifierLock(passCtx, func(ctx context.Context) error {
			tenants, err := s.repo.ListRiskTenants(ctx)
			if err != nil {
				return err
			}
			for _, tenantID := range tenants {
				start := time.Now()
				// scale-guard:ignore: per-tenant background pass owner=herd-signals issue=perf/herd-live reason=the classifier is tenant-scoped by design; tenants are few and each pass is internally batched expiry=2027-06-30
				changed, err := s.RecomputeRisk(ctx, tenantID)
				if err != nil {
					s.log.Warn("herd_signals_risk_classifier_failed", "tenant_id", tenantID, "error", err.Error())
					continue
				}
				s.log.Info("herd_signals_risk_classified", "tenant_id", tenantID, "changed", changed, "duration_ms", time.Since(start).Milliseconds())
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			s.log.Warn("herd_signals_risk_classifier_pass_failed", "ran", ran, "error", err.Error())
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
