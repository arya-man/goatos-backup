package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
)

// SweeperService implements SM-4 batching: collect unbatched due obligations for a version, group
// them by scope (shed/park), and create one batch per scope, attaching its obligations. Idempotent:
// a re-sweep finds no unbatched rows and creates nothing. (sop_task spawn + FEFO stock reserve are
// added in the following slices.)
type SweeperService struct {
	repo ports.Repository
	page int32
}

// NewSweeperService constructs the sweeper.
func NewSweeperService(repo ports.Repository) *SweeperService {
	return &SweeperService{repo: repo, page: 1000}
}

// SweepVersion batches all currently-unbatched due obligations for a version (due_at <= dueBefore).
func (s *SweeperService) SweepVersion(ctx context.Context, tenantID, versionID string, dueBefore time.Time) (domain.SweepResult, error) {
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
			batchID, err := s.repo.CreateBatch(ctx, domain.NewBatch{
				TenantID:          tenantID,
				ProtocolVersionID: versionID,
				ScopeType:         g.scopeType,
				ScopeID:           g.scopeID,
				Status:            "planned",
				EstimatedTargets:  int32(len(g.ids)),
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
