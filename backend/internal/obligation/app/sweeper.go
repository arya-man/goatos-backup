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

// SweeperService implements SM-4 batching: collect unbatched due obligations for a version, group
// them by scope (shed/park), spawn one SOP task per batch, and create one batch per scope attaching
// its obligations. Idempotent: a re-sweep finds no unbatched rows → no new batches/tasks.
type SweeperService struct {
	repo  ports.Repository
	tasks TaskCreator
	page  int32
}

// NewSweeperService constructs the sweeper. tasks may be nil to skip SOP task spawn.
func NewSweeperService(repo ports.Repository, tasks TaskCreator) *SweeperService {
	return &SweeperService{repo: repo, tasks: tasks, page: 1000}
}

// SweepVersion batches all currently-unbatched due obligations for a version (due_at <= dueBefore).
// sopVersionID (the version's SOP) drives task spawn; "" or a nil TaskCreator skips it.
func (s *SweeperService) SweepVersion(ctx context.Context, tenantID, versionID, sopVersionID string, dueBefore time.Time) (domain.SweepResult, error) {
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
			var sopTaskID *string
			if s.tasks != nil && sopVersionID != "" {
				taskID, err := s.tasks.CreateTaskForBatch(ctx, tenantID, sopVersionID, "vaccination", "Vaccination drive "+g.scopeID, g.scopeType, g.scopeID)
				if err != nil {
					return res, err
				}
				sopTaskID = &taskID
			}
			batchID, err := s.repo.CreateBatch(ctx, domain.NewBatch{
				TenantID:          tenantID,
				ProtocolVersionID: versionID,
				ScopeType:         g.scopeType,
				ScopeID:           g.scopeID,
				Status:            "planned",
				EstimatedTargets:  int32(len(g.ids)),
				SopTaskID:         sopTaskID,
			})
			if err != nil {
				return res, err
			}
			n, err := s.repo.AttachObligationsToBatch(ctx, tenantID, batchID, g.ids)
			if err != nil {
				return res, err
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
