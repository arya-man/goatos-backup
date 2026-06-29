package app

import (
	"context"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
)

// TaskCreator spawns one SOP task per batch. Implemented by a thin adapter over the SOP module
// (sop.CreateTask) at wiring time; the sweeper stays decoupled from SOP types. nil disables spawn.
type TaskCreator interface {
	CreateTaskForBatch(ctx context.Context, tenantID, batchID, sopVersionID, taskType, title, scopeType, scopeID string) (taskID string, err error)
}

// StockReserver reserves doses for a batch (FEFO). Satisfied by the inventory app service; nil
// disables reserve. Best-effort: no stock is a no-op.
type StockReserver interface {
	ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64, validOn time.Time) error
}

// SweepConfig carries the per-version batch config resolved by the caller from the protocol version:
// the SOP to instantiate, the vaccine item to reserve, and doses per goat.
type SweepConfig struct {
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int32
	RuleConfigs   map[string]SweepRuleConfig
}

// SweepRuleConfig overrides version-level execution bindings for one protocol rule.
type SweepRuleConfig struct {
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int32
}

// SweeperService implements SM-4 batching: collect unbatched due obligations for a version, group
// them by scope/rule/due window, spawn one SOP task per batch, reserve stock, and create one batch per
// operational drive/session. Idempotent: a re-sweep finds no unbatched rows → no new work.
type SweeperService struct {
	repo     ports.Repository
	tasks    TaskCreator
	reserver StockReserver
	page     int32
}

// NewSweeperService constructs the sweeper. tasks/reserver may be nil to skip spawn/reserve.
func NewSweeperService(repo ports.Repository, tasks TaskCreator, reserver StockReserver) *SweeperService {
	return &SweeperService{repo: repo, tasks: tasks, reserver: reserver, page: 1000}
}

// SweepVersion batches all currently-unbatched due obligations for a version (due_at <= dueBefore).
func (s *SweeperService) SweepVersion(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	var res domain.SweepResult
	if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
		return res, err
	}
	touchedScopes := make(map[string]bool)
	for {
		rows, err := s.repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, dueBefore, s.page)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}

		type group struct {
			scopeType   string
			scopeID     string
			ruleID      string
			plannedDate *time.Time
			windowStart *time.Time
			windowEnd   *time.Time
			ids         []string
		}
		order := make([]string, 0)
		groups := make(map[string]*group)
		for _, r := range rows {
			plannedDate := batchPlannedDate(r.DueAt)
			k := sweepGroupKey(r, plannedDate)
			g := groups[k]
			if g == nil {
				g = &group{scopeType: r.ScopeType, scopeID: r.ScopeID, ruleID: r.RuleID, plannedDate: plannedDate, windowStart: r.WindowStart, windowEnd: r.WindowEnd}
				groups[k] = g
				order = append(order, k)
			}
			g.ids = append(g.ids, r.ObligationID)
		}

		var progressed int64
		for _, k := range order {
			g := groups[k]
			_, n, err := s.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
				TenantID:          tenantID,
				ProtocolVersionID: versionID,
				ScopeType:         g.scopeType,
				ScopeID:           g.scopeID,
				Session:           batchSession(g.ruleID),
				PlannedDate:       g.plannedDate,
				WindowStart:       g.windowStart,
				WindowEnd:         g.windowEnd,
				Status:            "planned",
				EstimatedTargets:  int32(len(g.ids)),
				PlannedQuantity:   strconv.FormatInt(int64(len(g.ids))*int64(normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)), 10),
				QuantityUnit:      "dose",
			}, g.ids)
			if err != nil {
				return res, err
			}
			if n == 0 {
				continue
			}
			if !touchedScopes[k] {
				res.Batches++
				touchedScopes[k] = true
			}
			res.Obligations += int(n)
			progressed += n
		}
		if progressed == 0 || int32(len(rows)) < s.page {
			break
		}
	}
	if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
		return res, err
	}
	return res, nil
}

func normalizedDosesPerGoat(v int32) int32 {
	if v <= 0 {
		return 1
	}
	return v
}

func (cfg SweepConfig) forRule(ruleID string) SweepRuleConfig {
	out := SweepRuleConfig{
		SOPVersionID:  cfg.SOPVersionID,
		VaccineItemID: cfg.VaccineItemID,
		DosesPerGoat:  cfg.DosesPerGoat,
	}
	if cfg.RuleConfigs == nil {
		return out
	}
	ruleCfg, ok := cfg.RuleConfigs[ruleID]
	if !ok {
		return out
	}
	if ruleCfg.SOPVersionID != "" {
		out.SOPVersionID = ruleCfg.SOPVersionID
	}
	if ruleCfg.VaccineItemID != "" {
		out.VaccineItemID = ruleCfg.VaccineItemID
	}
	if ruleCfg.DosesPerGoat > 0 {
		out.DosesPerGoat = ruleCfg.DosesPerGoat
	}
	return out
}

func (cfg SweepConfig) needsTask() bool {
	if cfg.SOPVersionID != "" {
		return true
	}
	for _, ruleCfg := range cfg.RuleConfigs {
		if ruleCfg.SOPVersionID != "" {
			return true
		}
	}
	return false
}

func (cfg SweepConfig) needsStock() bool {
	if cfg.VaccineItemID != "" {
		return true
	}
	for _, ruleCfg := range cfg.RuleConfigs {
		if ruleCfg.VaccineItemID != "" {
			return true
		}
	}
	return false
}

func (s *SweeperService) finalizePlannedBatches(ctx context.Context, tenantID, versionID string, cfg SweepConfig) error {
	needsTask := s.tasks != nil && cfg.needsTask()
	needsStock := s.reserver != nil && cfg.needsStock()
	if !needsTask && !needsStock {
		return nil
	}
	for {
		batches, err := s.repo.ListPlannedBatchesNeedingFinalization(ctx, tenantID, versionID, needsTask, needsStock, s.page)
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			return nil
		}
		for _, b := range batches {
			batchCfg := cfg.forRule(b.RuleID)
			if s.tasks != nil && batchCfg.SOPVersionID != "" && !b.HasSOPTask {
				taskID, err := s.tasks.CreateTaskForBatch(ctx, tenantID, b.BatchID, batchCfg.SOPVersionID, "vaccination", "Vaccination drive "+b.ScopeID, b.ScopeType, b.ScopeID)
				if err != nil {
					return err
				}
				if err := s.repo.SetBatchSOPTask(ctx, tenantID, b.BatchID, taskID); err != nil {
					return err
				}
			}
			if s.reserver != nil && batchCfg.VaccineItemID != "" && !b.HasStockReservation {
				dosesPer := batchCfg.DosesPerGoat
				if dosesPer < 1 {
					dosesPer = 1
				}
				qty := b.AttachedObligations * int64(dosesPer)
				if qty <= 0 {
					continue
				}
				if err := s.reserver.ReserveForBatch(ctx, tenantID, b.BatchID, b.ScopeID, batchCfg.VaccineItemID, qty, batchStockValidOn(b)); err != nil {
					if markErr := s.repo.MarkBatchStockBlocked(ctx, tenantID, b.BatchID, batchCfg.VaccineItemID, qty, err.Error()); markErr != nil {
						return markErr
					}
					continue
				}
				if b.StockBlocked {
					if err := s.repo.ClearBatchStockBlock(ctx, tenantID, b.BatchID); err != nil {
						return err
					}
				}
			}
		}
		if int32(len(batches)) < s.page {
			return nil
		}
	}
}

func sweepGroupKey(r domain.UnbatchedDue, plannedDate *time.Time) string {
	return r.ScopeType + "|" + r.ScopeID + "|" + r.RuleID + "|" + timeKey(plannedDate) + "|" + timeKey(r.WindowStart) + "|" + timeKey(r.WindowEnd)
}

func batchSession(ruleID string) string {
	if ruleID == "" {
		return ""
	}
	return "rule:" + ruleID
}

func batchPlannedDate(dueAt time.Time) *time.Time {
	if dueAt.IsZero() {
		return nil
	}
	y, m, d := dueAt.UTC().Date()
	planned := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &planned
}

func batchStockValidOn(b domain.PlannedBatchFinalization) time.Time {
	if b.PlannedDate != nil && !b.PlannedDate.IsZero() {
		return *b.PlannedDate
	}
	return time.Now().UTC()
}

func timeKey(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// MarkMissed materializes the terminal missed state for obligations whose due window/deadline has
// already crossed. This keeps the canonical obligation table aligned with Calendar/Action Center
// late-state projections.
func (s *SweeperService) MarkMissed(ctx context.Context, tenantID string, missedBefore time.Time) (int, error) {
	total := 0
	for {
		n, err := s.repo.MarkMissedBefore(ctx, tenantID, missedBefore, s.page)
		if err != nil {
			return total, err
		}
		total += n
		if int32(n) < s.page {
			return total, nil
		}
	}
}
