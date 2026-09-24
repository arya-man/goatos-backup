package app

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

const (
	// riskBatchSize bounds one classifier transaction (rows locked, enrichment, UNNEST write).
	riskBatchSize = 500
	// riskMaxQueueBatchesPerTick caps one tick's queue work per tenant; leftovers wait a tick.
	riskMaxQueueBatchesPerTick = 40
	// riskAgingPerTick is the hourly backstop's per-tick share (oldest evaluations > 1h).
	riskAgingPerTick = 1500
	// riskPenMedianEvery: pen baselines are re-aggregated at most this often per tenant.
	riskPenMedianEvery = 5 * time.Minute
	// A pen is re-scored only when its baseline moves by more than these.
	riskPenMotionRelThreshold = 0.10
	riskPenMotionAbsThreshold = 1.0
	riskPenTempThresholdC     = 0.2
)

// RiskPassStats reports one tenant's classifier work.
type RiskPassStats struct {
	Processed   int
	Changed     int
	PensMoved   int
	TagsQueued  int
	Locked      bool
	MediansRead bool
}

// RecomputeRisk runs one change-driven classifier pass for a tenant: refresh pen baselines when
// due (queueing only pens whose median moved past the threshold), drain the change queue in
// bounded batches, then one aging-backstop share. Each batch is its own short transaction
// under the tenant's advisory xact lock, so no connection or lock is held across the pass.
// It returns the number of tags whose visible classification changed.
func (s *Service) RecomputeRisk(ctx context.Context, tenantID string) (int, error) {
	st, err := s.riskPass(ctx, tenantID, true, riskMaxQueueBatchesPerTick*1000)
	return st.Changed, err
}

// RiskTick is one scheduled (non-forced) classifier pass for a tenant -- what each
// RunRiskClassifier tick does per tenant.
func (s *Service) RiskTick(ctx context.Context, tenantID string) (RiskPassStats, error) {
	return s.riskPass(ctx, tenantID, false, riskMaxQueueBatchesPerTick)
}

func (s *Service) riskPass(ctx context.Context, tenantID string, forceMedians bool, maxQueueBatches int) (RiskPassStats, error) {
	st := RiskPassStats{Locked: true}
	stats, err := s.refreshPenBaselines(ctx, tenantID, forceMedians, &st)
	if err != nil || !st.Locked {
		return st, err
	}
	classify := func(ctx context.Context, tags []domain.TagLatest) ([]ports.TagRisk, error) {
		items := s.enrichTagsBatch(ctx, tenantID, tags, nil, false)
		applyRiskSignals(items, stats)
		return riskRowsFromItems(items), nil
	}
	for i := 0; i < maxQueueBatches; i++ {
		// scale-guard:ignore: bounded batch drain owner=herd-signals issue=perf/herd-live reason=each iteration is one short FOR UPDATE SKIP LOCKED batch of riskBatchSize rows whose write removes them from the queue; capped per tick expiry=2027-06-30
		res, err := s.repo.ClassifyRiskBatch(ctx, tenantID, ports.RiskBatchQueue, riskBatchSize, classify)
		if err != nil {
			return st, err
		}
		if !res.Locked {
			st.Locked = false
			return st, nil
		}
		st.Processed += res.Processed
		st.Changed += res.Changed
		if res.Processed < riskBatchSize {
			break
		}
	}
	for aged := 0; aged < riskAgingPerTick; aged += riskBatchSize {
		// scale-guard:ignore: bounded aging share owner=herd-signals issue=perf/herd-live reason=at most riskAgingPerTick/riskBatchSize short batches per tick, each advancing risk_evaluated_at past the aging predicate expiry=2027-06-30
		res, err := s.repo.ClassifyRiskBatch(ctx, tenantID, ports.RiskBatchAging, riskBatchSize, classify)
		if err != nil {
			return st, err
		}
		st.Processed += res.Processed
		st.Changed += res.Changed
		if !res.Locked || res.Processed < riskBatchSize {
			break
		}
	}
	return st, nil
}

// refreshPenBaselines returns the pen medians batches score against: the persisted ones,
// refreshed from the live aggregate when due. Only pens that moved past the threshold (or
// appeared/vanished) are written and have their tags re-queued.
func (s *Service) refreshPenBaselines(ctx context.Context, tenantID string, force bool, st *RiskPassStats) (map[string]riskGroupStats, error) {
	stored, found, err := s.repo.LoadPenMedians(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load pen medians: %w", err)
	}
	if force || !found || s.penMediansDue(tenantID) {
		st.MediansRead = true
		live, err := s.repo.ListLivePenMedians(ctx, tenantID, nil, nil, nil, nil, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("pen medians: %w", err)
		}
		moved, vanished := movedPens(stored, live)
		locked, queued, err := s.repo.ApplyPenMedians(ctx, tenantID, moved, vanished, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if !locked {
			st.Locked = false
			return nil, nil
		}
		st.PensMoved, st.TagsQueued = len(moved)+len(vanished), queued
		for pen, m := range moved {
			stored[pen] = m
		}
		for _, pen := range vanished {
			delete(stored, pen)
		}
		s.markPenMediansRead(tenantID)
	}
	out := make(map[string]riskGroupStats, len(stored))
	for pen, m := range stored {
		out[pen] = riskGroupStats{motionMedian: m.MotionMedian, tempMedian: m.TempMedian}
	}
	return out, nil
}

// movedPens compares the live medians with the stored ones.
func movedPens(stored, live map[string]ports.PenMedians) (map[string]ports.PenMedians, []string) {
	moved := map[string]ports.PenMedians{}
	for pen, l := range live {
		old, ok := stored[pen]
		if !ok || motionMoved(old.MotionMedian, l.MotionMedian) || tempMoved(old.TempMedian, l.TempMedian) {
			moved[pen] = l
		}
	}
	var vanished []string
	for pen := range stored {
		if _, ok := live[pen]; !ok {
			vanished = append(vanished, pen)
		}
	}
	return moved, vanished
}

func motionMoved(old, cur *float64) bool {
	if (old == nil) != (cur == nil) {
		return true
	}
	if old == nil {
		return false
	}
	d := math.Abs(*cur - *old)
	return d > riskPenMotionAbsThreshold && d > riskPenMotionRelThreshold*math.Abs(*old)
}

func tempMoved(old, cur *float64) bool {
	if (old == nil) != (cur == nil) {
		return true
	}
	return old != nil && math.Abs(*cur-*old) > riskPenTempThresholdC
}

func (s *Service) penMediansDue(tenantID string) bool {
	s.penMedianMu.Lock()
	defer s.penMedianMu.Unlock()
	last, ok := s.penMedianAt[tenantID]
	return !ok || time.Since(last) >= riskPenMedianEvery
}

func (s *Service) markPenMediansRead(tenantID string) {
	s.penMedianMu.Lock()
	defer s.penMedianMu.Unlock()
	if s.penMedianAt == nil {
		s.penMedianAt = map[string]time.Time{}
	}
	s.penMedianAt[tenantID] = time.Now()
}

func riskRowsFromItems(items []domain.LiveItem) []ports.TagRisk {
	rows := make([]ports.TagRisk, len(items))
	for i, item := range items {
		rows[i] = ports.TagRisk{
			TagID: item.TagID, State: item.RiskState, Score: item.RiskScore, Reasons: item.RiskReasons,
			OwnMotionPct: item.OwnMotionDeltaPct, GroupMotionPct: item.GroupMotionDeltaPct, GroupTempDeltaC: item.GroupTempDeltaC,
		}
		if item.RiskState == nil {
			rows[i].Reasons, rows[i].Score = nil, 0
		}
	}
	return rows
}

// RunRiskClassifier runs a change-driven pass per tenant every interval until ctx ends. A tick
// is skipped while the previous one is still running, each pass is bounded to 3/4 of the
// interval, and per-tenant exclusivity comes from the advisory xact lock inside each batch.
func (s *Service) RunRiskClassifier(ctx context.Context, interval time.Duration) {
	var running atomic.Bool
	tick := func() {
		if !running.CompareAndSwap(false, true) {
			s.log.Info("herd_signals_risk_classifier_tick_skipped", "reason", "previous tick still running")
			return
		}
		go func() {
			defer running.Store(false)
			passCtx, cancel := context.WithTimeout(ctx, interval*3/4)
			defer cancel()
			tenants, err := s.repo.ListRiskTenants(passCtx)
			if err != nil {
				if ctx.Err() == nil {
					s.log.Warn("herd_signals_risk_classifier_tenants_failed", "error", err.Error())
				}
				return
			}
			for _, tenantID := range tenants {
				start := time.Now()
				// scale-guard:ignore: per-tenant background pass owner=herd-signals issue=perf/herd-live reason=the classifier is tenant-scoped by design; tenants are few and each pass is internally batched and bounded expiry=2027-06-30
				st, err := s.RiskTick(passCtx, tenantID)
				if err != nil {
					s.log.Warn("herd_signals_risk_classifier_failed", "tenant_id", tenantID, "error", err.Error())
					continue
				}
				if st.Processed > 0 || st.PensMoved > 0 || !st.Locked {
					s.log.Info("herd_signals_risk_classified", "tenant_id", tenantID, "processed", st.Processed,
						"changed", st.Changed, "pens_moved", st.PensMoved, "tags_queued", st.TagsQueued,
						"locked", st.Locked, "duration_ms", time.Since(start).Milliseconds())
				}
			}
		}()
	}
	tick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}
