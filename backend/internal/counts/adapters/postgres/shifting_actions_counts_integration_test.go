package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestShiftingActionsTabCountsEqualWhatEachTabLists (2026-09-25, live on the throwaway clone):
// GET /app/counts/shifting-events/pending-execution?status=pending with NO date returned 20 rows
// beside status_counts {all:0, pending:0, ...}. The counts were computed only when a business date
// was selected, so an undated read -- which lists every date -- served a zeroed summary. Each tab's
// count must equal what that tab lists, dated or not.
func TestShiftingActionsTabCountsEqualWhatEachTabLists(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	goats := []string{"00000000-0000-4000-8000-00000000e701", "00000000-0000-4000-8000-00000000e702", "00000000-0000-4000-8000-00000000e703"}
	for i, g := range goats {
		seedApprovalGoat(t, ctx, pool, g, countsShedA)
		submitShiftingApproval(t, ctx, repo, "tab-counts-"+string(rune('a'+i)), []string{g})
	}
	now := time.Now().In(biztime.DefaultLocation())
	day := biztime.BusinessDayStart(now).AddDate(0, 0, -2)
	dayEnd := day.AddDate(0, 0, 1)

	for _, dated := range []bool{false, true} {
		for _, status := range []string{"all", "pending", "authorized", "rework", "completed"} {
			q := domain.ShiftingExecutionQuery{TenantID: countsTenant, Status: status, PageSize: 50, Now: now}
			if dated {
				q.RaisedFrom, q.RaisedBefore = &day, &dayEnd
			}
			page, err := repo.ListShiftingEventsPendingExecution(ctx, q)
			if err != nil {
				t.Fatalf("dated=%t %s: %v", dated, status, err)
			}
			c := page.StatusCounts
			want := map[string]int{"all": c.All, "pending": c.Pending, "authorized": c.Authorized, "rework": c.Rework, "completed": c.Completed}[status]
			if len(page.Items) != want {
				t.Fatalf("dated=%t: the %s tab lists %d rows but its count is %d (counts %+v)", dated, status, len(page.Items), want, c)
			}
		}
	}
}
