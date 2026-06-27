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
	CreateTaskForBatch(ctx context.Context, tenantID, sopVersionID, taskType, title, scopeType, scopeID string) (taskID string, err error)
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
			batchID, n, err := s.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
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
			if s.tasks != nil && cfg.SOPVersionID != "" {
				taskID, err := s.tasks.CreateTaskForBatch(ctx, tenantID, cfg.SOPVersionID, "vaccination", "Vaccination drive "+g.scopeID, g.scopeType, g.scopeID)
				if err != nil {
					return res, err
				}
				if err := s.repo.SetBatchSOPTask(ctx, tenantID, batchID, taskID); err != nil {
					return res, err
				}
			}
			if s.reserver != nil && cfg.VaccineItemID != "" {
				dosesPer := cfg.DosesPerGoat
				if dosesPer < 1 {
					dosesPer = 1
				}
				qty := n * int64(dosesPer)
				if err := s.reserver.ReserveForBatch(ctx, tenantID, batchID, g.scopeID, cfg.VaccineItemID, qty); err != nil {
					_ = s.repo.MarkBatchStockBlocked(ctx, tenantID, batchID, cfg.VaccineItemID, qty, err.Error())
					return res, err
				}
			}
			res.Batches++
			res.Obligations += int(n)
			progressed += n
		}
		if progressed == 0 || int32(len(rows)) < s.page {
			break
		}
	}
	return res, nil
}
