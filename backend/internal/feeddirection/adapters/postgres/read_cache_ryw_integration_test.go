package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/readcache/readcachetest"
)

// Feed issue / amend / lock change feed_direction_issue_rows, which Growth Director's FCR reads
// through the shared analytics cache. The writer must evict its OWN instance after commit (not
// wait for its listener) and every sibling within a second.
func TestIssueWritesEvictTheSharedReadCacheOnBothInstances(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	pair := readcachetest.NewPair(t, ctx, pool)
	repo = repo.WithReadCacheInvalidator(pair.Writer)
	parks := []string{fdiPark}

	pair.Check(t, ctx, "feeddirection PersistIssue", fdiTenant, parks, true, func(t *testing.T) {
		if _, err := repo.PersistIssue(ctx, issueCmd(sampleCells(), "fp-ryw", time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation()))); err != nil {
			t.Fatalf("PersistIssue: %v", err)
		}
	})
	changed := sampleCells()
	for i := range changed {
		if changed[i].ShedID == fdiShedB && changed[i].FeedItemLabel == "Concentrate" {
			changed[i].QuantityKg = kg("3.000")
			changed[i].SessionTotalKg = "3.000"
		}
	}
	pair.Check(t, ctx, "feeddirection AmendIssue", fdiTenant, parks, true, func(t *testing.T) {
		if _, err := repo.AmendIssue(ctx, ports.AmendIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
			AmendedAt: time.Date(2026, 7, 29, 14, 0, 0, 0, biztime.DefaultLocation()), Fingerprint: "fp-ryw-amended", Cells: changed,
		}); err != nil {
			t.Fatalf("AmendIssue: %v", err)
		}
	})
	pair.Check(t, ctx, "feeddirection LockIssue", fdiTenant, parks, true, func(t *testing.T) {
		if _, err := repo.LockIssue(ctx, ports.LockIssueCommand{TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
			LockedAt: time.Date(2026, 7, 29, 15, 45, 0, 0, biztime.DefaultLocation())}); err != nil {
			t.Fatalf("LockIssue: %v", err)
		}
	})
}
