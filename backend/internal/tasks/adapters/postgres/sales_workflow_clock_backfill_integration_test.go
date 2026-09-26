package postgres

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestRepair000451AnchorsPlannedSalesOpenedBeforeTheAnchor (docs/decisions/sales-sop.md -> "A
// planned sale's work is due on the sale day" -> "Existing data"). A sale workflow opened before
// 000449 was anchored on its RECORDING instant with a NULL clock_anchor_at, so a sale planned five
// days out stayed Overdue even after the opener was fixed. Migration 000451 anchors it on the sale
// day and recomputes each unfinished step exactly as the opener would -- an "immediately" step at
// 00:00 of the sale day, an authored "at_fixed_time" step at its wall-clock time ON the sale day
// (a blind instant delta would put it at 10:30, not 10:00) -- leaves finished steps and a sale dated
// on its recording day alone, is a no-op the second time, and a later close then moves the steps
// once.
func TestRepair000451AnchorsPlannedSalesOpenedBeforeTheAnchor(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	publishV1SaleSOPAndApply000443(t, repo)
	// An authored version whose payment question is due at a fixed 10:00 on the anchor's day.
	if _, err := pool.Exec(ctx, `
UPDATE sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{follow_up,tracks}', (
      SELECT jsonb_agg(jsonb_set(tr, '{steps}', (
               SELECT jsonb_agg(CASE WHEN st->>'key' = 'full_payment'
                                     THEN jsonb_set(st, '{schedule}', '{"kind": "at_fixed_time", "time": "10:00"}'::jsonb)
                                     ELSE st END ORDER BY so)
               FROM jsonb_array_elements(tr->'steps') WITH ORDINALITY s(st, so))) ORDER BY o)
      FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') WITH ORDINALITY t(tr, o)))
FROM sop_definitions sd
WHERE sd.tenant_id = v.tenant_id AND sd.sop_id = v.sop_id AND sd.code = 'sales.deal' AND v.tenant_id = $1::uuid`, wfTenant); err != nil {
		t.Fatalf("author a fixed-time payment step: %v", err)
	}

	recordedAt := wfEventAt // 27/07/2026 09:30 IST
	plannedDay := biztime.BusinessDayStart(recordedAt).AddDate(0, 0, 5)
	const plannedDeal, todayDeal = "5a1e5a1e-0000-4000-8000-00000000d001", "5a1e5a1e-0000-4000-8000-00000000d002"
	open := func(dealID string, saleDay time.Time) string {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, $3::date, 'CBE', 'Planned buyer', 'Goat', 'Any', 3, 1000, 'Advance Paid')`,
			dealID, wfTenant, biztime.BusinessDate(saleDay)); err != nil {
			t.Fatalf("seed deal: %v", err)
		}
		ref := dealID
		// The OLD opener: no clock anchor, every step due from the recording instant.
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal,
			EventAt: recordedAt, SubjectRefID: &ref}); err != nil {
			t.Fatalf("open %s: %v", dealID, err)
		}
		id, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, dealID)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	plannedWF := open(plannedDeal, plannedDay)
	todayWF := open(todayDeal, recordedAt)
	// A step already finished on the planned sale: history, never moved.
	if err := repo.CompleteSaleTagStep(ctx, wfTenant, plannedDeal, recordedAt.UTC()); err != nil {
		t.Fatal(err)
	}

	type stepClock struct {
		due    time.Time
		status string
	}
	snapshot := func(workflowID string) (map[string]stepClock, string) {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT action_key, due_at, status FROM workflow_actions WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, workflowID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]stepClock{}
		for rows.Next() {
			var key, status string
			var due *time.Time
			if err := rows.Scan(&key, &due, &status); err != nil {
				t.Fatal(err)
			}
			c := stepClock{status: status}
			if due != nil {
				c.due = *due
			}
			out[key] = c
		}
		var card string
		if err := pool.QueryRow(ctx, `SELECT coalesce(clock_anchor_at::text, 'NULL') || ' next=' || coalesce(next_action_key, '') || '@' || coalesce(next_due_at::text, '') || ' v' || row_version
FROM workflow_instances WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, workflowID).Scan(&card); err != nil {
			t.Fatal(err)
		}
		return out, card
	}
	at := func(day time.Time, hour, minute int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, biztime.DefaultLocation())
	}
	expectDue := func(workflowID string, want map[string]time.Time) {
		t.Helper()
		got, card := snapshot(workflowID)
		for key, w := range want {
			if !got[key].due.Equal(w) {
				t.Fatalf("%s step %s due %s, want %s (card %s)", workflowID, key, got[key].due.In(biztime.DefaultLocation()), w, card)
			}
		}
	}

	// RED before the repair: the planned sale is Overdue a minute after it was recorded.
	detail, err := repo.GetWorkflow(ctx, wfTenant, plannedWF, recordedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.NextAction == nil || !detail.Card.NextAction.Overdue {
		t.Fatalf("precondition: the old-way planned sale reads Overdue, got %+v", detail.Card.NextAction)
	}
	todayBefore, todayCardBefore := snapshot(todayWF)
	tagBefore, _ := snapshot(plannedWF)

	apply000451(t, pool)
	plannedAfterFirst, plannedCardFirst := snapshot(plannedWF)
	apply000451(t, pool) // a second run changes nothing
	plannedAfterSecond, plannedCardSecond := snapshot(plannedWF)
	if plannedCardFirst != plannedCardSecond || fmt.Sprint(plannedAfterFirst) != fmt.Sprint(plannedAfterSecond) {
		t.Fatalf("000451 is not idempotent: %s -> %s", plannedCardFirst, plannedCardSecond)
	}

	// The planned sale: unfinished steps on the sale day, the fixed-time one at its own 10:00.
	expectDue(plannedWF, map[string]time.Time{
		"loading_video": plannedDay,
		"dispatch_note": plannedDay,
		"full_payment":  at(plannedDay, 10, 0),
		"tag_animals":   tagBefore["tag_animals"].due, // finished: untouched
	})
	if !strings.HasPrefix(plannedCardFirst, plannedDay.UTC().Format("2006-01-02 15:04:05")) {
		t.Fatalf("anchor must be 00:00 IST of the sale day, card %s", plannedCardFirst)
	}
	detail, err = repo.GetWorkflow(ctx, wfTenant, plannedWF, recordedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.NextAction == nil || detail.Card.NextAction.Overdue || detail.Card.NextAction.DueAt == nil || !detail.Card.NextAction.DueAt.Equal(plannedDay) {
		t.Fatalf("after 000451 the planned sale's next step is due on the sale day and not overdue, got %+v", detail.Card.NextAction)
	}
	// A sale dated on its recording day is untouched.
	todayAfter, todayCardAfter := snapshot(todayWF)
	if todayCardAfter != todayCardBefore || fmt.Sprint(todayAfter) != fmt.Sprint(todayBefore) {
		t.Fatalf("a sale dated on its recording day must not change: %s -> %s", todayCardBefore, todayCardAfter)
	}

	// Closing the planned sale early (+2 days) moves its unfinished steps once, redelivery or not.
	closeDay := biztime.BusinessDayStart(recordedAt).AddDate(0, 0, 2)
	for i := 0; i < 2; i++ {
		if err := repo.ReanchorSaleWorkflow(ctx, wfTenant, plannedDeal, biztime.BusinessDate(closeDay)); err != nil {
			t.Fatalf("reanchor pass %d: %v", i+1, err)
		}
	}
	expectDue(plannedWF, map[string]time.Time{
		"loading_video": closeDay,
		"full_payment":  at(closeDay, 10, 0),
		"tag_animals":   tagBefore["tag_animals"].due,
	})
}

// apply000451 runs migration 000451's own Up SQL.
func apply000451(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "postgres", "000451_sales_workflow_clock_anchor_backfill.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(raw), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]
	if _, err := pool.Exec(t.Context(), up); err != nil {
		t.Fatalf("apply 000451: %v", err)
	}
}
