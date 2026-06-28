package app

import (
	"context"
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
	ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64) error
}

// SweepConfig carries the per-version batch config resolved by the caller from the protocol version:
// the SOP to instantiate, the vaccine item to reserve, and doses per goat.
type SweepConfig struct {
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int32
}

// SweeperService implements SM-4 batching: collect unbatched due obligations for a version, group
// them by scope (shed/park), spawn one SOP task per batch, reserve stock, and create one batch per
// scope attaching its obligations. Idempotent: a re-sweep finds no unbatched rows → no new work.
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
			scopeType string
			scopeID   string
			ids       []string
		}
		order := make([]string, 0)
		groups := make(map[string]*group)
		for _, r := range rows {
			k := r.ScopeType + "|" + r.ScopeID
			g := groups[k]
			if g == nil {
				g = &group{scopeType: r.ScopeType, scopeID: r.ScopeID}
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
				Status:            "planned",
				EstimatedTargets:  int32(len(g.ids)),
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

func (s *SweeperService) finalizePlannedBatches(ctx context.Context, tenantID, versionID string, cfg SweepConfig) error {
	needsTask := s.tasks != nil && cfg.SOPVersionID != ""
	needsStock := s.reserver != nil && cfg.VaccineItemID != ""
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
			if needsTask && !b.HasSOPTask {
				taskID, err := s.tasks.CreateTaskForBatch(ctx, tenantID, b.BatchID, cfg.SOPVersionID, "vaccination", "Vaccination drive "+b.ScopeID, b.ScopeType, b.ScopeID)
				if err != nil {
					return err
				}
				if err := s.repo.SetBatchSOPTask(ctx, tenantID, b.BatchID, taskID); err != nil {
					return err
				}
			}
			if needsStock && !b.HasStockReservation && !b.StockBlocked {
				dosesPer := cfg.DosesPerGoat
				if dosesPer < 1 {
					dosesPer = 1
				}
				qty := b.AttachedObligations * int64(dosesPer)
				if qty <= 0 {
					continue
				}
				if err := s.reserver.ReserveForBatch(ctx, tenantID, b.BatchID, b.ScopeID, cfg.VaccineItemID, qty); err != nil {
					if markErr := s.repo.MarkBatchStockBlocked(ctx, tenantID, b.BatchID, cfg.VaccineItemID, qty, err.Error()); markErr != nil {
						return markErr
					}
					continue
				}
			}
		}
		if int32(len(batches)) < s.page {
			return nil
		}
	}
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
