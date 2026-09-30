package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

// separateAdherenceSummary is the retired second read /vaccination/adherence used to run in parallel
// with the page. It stays here as the reference the combined statement must equal.
func separateAdherenceSummary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, args []any) domain.AdherenceSummary {
	t.Helper()
	c := countQueryArgs(args)
	var s domain.AdherenceSummary
	if err := pool.QueryRow(ctx, processIntegrityCanonicalAdherenceSummarySQL, pgx.QueryExecModeExec,
		c[0], c[1], c[2], c[3], c[4], c[5], c[6], c[7], c[8], c[9], c[10], c[11], c[12], c[13], c[14], c[15]).Scan(
		&s.ExpectedCount, &s.CompletedCount, &s.OpenGapCount, &s.DeferredCount, &s.ProcessIntactCount); err != nil {
		t.Fatal(err)
	}
	if s.ExpectedCount > 0 {
		s.AdherencePercent = float64(s.CompletedCount) / float64(s.ExpectedCount) * 100
	}
	return s
}

// TestCanonicalRowsAndSummaryMatchesTheTwoReadsItReplacesWorkStateDateShift: the Protocol Adherence
// page+summary statement returns exactly the separate LIST page and adherence summary across as_of
// date shifts, page sizes, every work_state tab (including one with no rows: the Blocked tab, whose
// empty page must still carry the summary), a cursor past the last row, and the latest-drive scope.
// Before the merge the two reads ran in parallel and overran the query timeout under load (500
// internal_error on the Blocked tab).
func TestCanonicalRowsAndSummaryMatchesTheTwoReadsItReplacesWorkStateDateShift(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	nonEmpty, emptyTab := false, false
	states := []string{"", "overdue", "missed", "blocked", "completed", "due"}
	for _, asOf := range []time.Time{
		time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
	} {
		for _, state := range states {
			for _, latestDrive := range []bool{false, true} {
				for _, limit := range []int{1, 2, 10} {
					due := asOf.Add(-30 * 24 * time.Hour)
					cat := domain.CategoryVaccination
					q := domain.Query{
						TenantID:                piTenant,
						AsOf:                    asOf,
						DueAfter:                &due,
						DueBefore:               asOf.AddDate(0, 0, 7),
						Limit:                   limit,
						IncludeCompleted:        true,
						IncludeAdherenceSummary: true,
						ScopeLatestDrive:        latestDrive,
					}
					if latestDrive {
						q.Category = &cat
					}
					if state != "" {
						ws := domain.WorkState(state)
						q.WorkState = &ws
					}
					q = normalizeQuery(q)
					args := queryArgs(q)
					wantRows, wantCursor, wantExtra, err := repo.fetchCanonicalRows(ctx, q, args)
					if err != nil {
						t.Fatal(err)
					}
					wantSummary := separateAdherenceSummary(t, ctx, pool, args)
					gotRows, gotCursor, gotExtra, gotSummary, err := repo.fetchCanonicalRowsAndSummary(ctx, q, args)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(gotRows, wantRows) || !reflect.DeepEqual(gotCursor, wantCursor) || gotExtra != wantExtra {
						t.Fatalf("state=%q limit=%d page differs:\n got %+v %+v %v\nwant %+v %+v %v", state, limit, gotRows, gotCursor, gotExtra, wantRows, wantCursor, wantExtra)
					}
					if gotSummary != wantSummary {
						t.Fatalf("state=%q limit=%d summary differs: got %+v want %+v", state, limit, gotSummary, wantSummary)
					}
					if len(wantRows) > 0 {
						nonEmpty = true
					} else {
						emptyTab = true
					}
					// The service path returns the same envelope.
					res, err := repo.listRowsCanonical(ctx, q, args)
					if err != nil {
						t.Fatal(err)
					}
					if res.AdherenceSummary != wantSummary || res.TotalCount != int64(wantSummary.OpenGapCount+wantSummary.ProcessIntactCount) || !reflect.DeepEqual(res.Rows, wantRows) {
						t.Fatalf("state=%q listRowsCanonical envelope differs: %+v", state, res)
					}
					if wantCursor == nil {
						continue
					}
					past := q
					past.Cursor = &domain.Cursor{SortPriority: 1 << 30, DueAt: wantCursor.DueAt, RowID: "~"}
					emptyRows, _, _, emptySummary, err := repo.fetchCanonicalRowsAndSummary(ctx, past, queryArgs(past))
					if err != nil {
						t.Fatal(err)
					}
					if len(emptyRows) != 0 || emptySummary != wantSummary {
						t.Fatalf("empty page lost the summary: rows=%d summary=%+v want %+v", len(emptyRows), emptySummary, wantSummary)
					}
				}
			}
		}
	}
	if !nonEmpty || !emptyTab {
		t.Fatalf("seed must produce both a non-empty and an empty tab (nonEmpty=%v empty=%v)", nonEmpty, emptyTab)
	}
}
