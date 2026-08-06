package postgres

// The Colostrum day lens — Postgres integration proof against the real migration-complete schema
// (docs/decisions/colostrum-milk-module.md). Opt-in like every DB test: GOATOS_RUN_POSTGRES_TESTS=1
// (pgtest.SkipIfNoDocker); the default suite never starts a container.
//
// These tests drive the SAME repository methods the app service and the answer/complete APIs call.
// In particular the parity test writes through the production CompleteAction — the identical call
// the Birth page makes — and then reads the Colostrum lens, which is the whole claim of this
// feature: one row, two entry points.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// The shared harness births at 2026-07-27 09:30 IST. The 07:00 slot is already past its
// pre-notification cutoff, so the birth day carries 1st Colostrum + 11:00/15:00/18:30/22:00 = 5
// feeds, and the following day carries all five slots.
const (
	colostrumBirthDate = "2026-07-27"
	colostrumNextDate  = "2026-07-28"
)

func colostrumQuery(date, filter string, now time.Time) domain.ColostrumDayQuery {
	return domain.ColostrumDayQuery{
		TenantID:  wfTenant,
		Date:      date,
		TodayDate: date,
		Filter:    filter,
		PageSize:  domain.MaxWorkflowPageSize,
		Now:       now,
	}
}

func colostrumCard(t *testing.T, page domain.WorkflowListPage, workflowID string) domain.WorkflowCard {
	t.Helper()
	for _, card := range page.Items {
		if card.WorkflowID == workflowID {
			return card
		}
	}
	t.Fatalf("workflow %s not present in the colostrum page (%d cards)", workflowID, len(page.Items))
	return domain.WorkflowCard{}
}

// The defect this lens exists to prevent: a kid born on the 27th has feeds due on the 28th, but its
// workflow_instances.event_date is the 27th. A birth-style event_date filter would show an EMPTY
// colostrum page on the 28th while five feeds were actually due.
func TestColostrumDayListsAKidOnEveryDateItsFeedsAreDue(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	birthDay, err := repo.ListColostrumDay(ctx, colostrumQuery(colostrumBirthDate, "", wfEventAt))
	if err != nil {
		t.Fatalf("list birth day: %v", err)
	}
	birthCard := colostrumCard(t, birthDay, workflowID)
	if birthCard.ActionsTotal != 5 {
		t.Fatalf("birth-day total = %d, want 5 (1st Colostrum + the four slots after the 09:30 birth)",
			birthCard.ActionsTotal)
	}

	nextDayAt := time.Date(2026, 7, 28, 12, 0, 0, 0, biztime.DefaultLocation())
	nextDay, err := repo.ListColostrumDay(ctx, colostrumQuery(colostrumNextDate, "", nextDayAt))
	if err != nil {
		t.Fatalf("list next day: %v", err)
	}
	nextCard := colostrumCard(t, nextDay, workflowID)
	if nextCard.ActionsTotal != 5 {
		t.Fatalf("next-day total = %d, want the five scheduled slots", nextCard.ActionsTotal)
	}
	if nextCard.EventDate != colostrumBirthDate {
		t.Fatalf("event_date = %s, want the BIRTH date %s — the card is selected by feed due date, "+
			"not by birth date", nextCard.EventDate, colostrumBirthDate)
	}
	// The card is day-scoped, so its module and state describe the DAY, not the kid's whole workflow.
	if nextCard.Module != domain.ModuleColostrum {
		t.Fatalf("module = %q, want colostrum", nextCard.Module)
	}
	if nextCard.State != domain.WorkflowStateOpen {
		t.Fatalf("state = %q, want open while feeds remain", nextCard.State)
	}
	if nextCard.AwaitingVerification {
		t.Fatal("the colostrum lens has no awaiting-verification bucket; it must always read false")
	}
}

// THE HEADLINE TEST. Completing a feed through the production write path — the exact call the Birth
// screen makes — must move the Colostrum card, because they are the same row. If this ever fails,
// the feature has silently grown a second copy of the state.
func TestCompletingAFeedFromBirthMovesTheColostrumCardAndViceVersa(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}

	before := colostrumCard(t, mustListColostrum(t, repo, ctx, colostrumBirthDate, wfEventAt), workflowID)
	if before.ActionsDone != 0 {
		t.Fatalf("done = %d before any write, want 0", before.ActionsDone)
	}

	// 1st Colostrum sits behind the four earlier main-section steps, so clear them exactly as the
	// Birth screen would. This also proves the lens respects the real sequence rather than
	// side-stepping it.
	for _, action := range detail.Actions {
		if action.Seq > 4 {
			continue
		}
		key := "colostrum-parity-prereq-" + action.ActionKey
		if action.ActionType == domain.ActionTypeQuestion {
			_, err = repo.AnswerAction(ctx, domain.AnswerActionCommand{
				TenantID: wfTenant, WorkflowID: workflowID, ActionID: action.ActionID,
				AnswerValue: "yes", ProofRef: "proof-" + action.ActionKey,
				AnsweredBy: wfCustodian, AnsweredAt: wfEventAt.UTC(),
				IdempotencyKey: key, RequestFingerprint: key,
			})
		} else {
			_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{
				TenantID: wfTenant, WorkflowID: workflowID, ActionID: action.ActionID,
				ProofRef: "proof-" + action.ActionKey, CompletedBy: wfCustodian,
				CompletedAt: wfEventAt.UTC(), IdempotencyKey: key, RequestFingerprint: key,
			})
		}
		if err != nil {
			t.Fatalf("prerequisite %s: %v", action.ActionKey, err)
		}
	}

	// The write the Birth page performs. Nothing colostrum-specific about it.
	if _, err := repo.CompleteAction(ctx, domain.CompleteActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID: actionIDByKey(t, detail, domain.ActionKeyFirstColostrum),
		ProofRef: "proof-first-colostrum", CompletedBy: wfCustodian,
		CompletedAt:    wfEventAt.UTC(),
		IdempotencyKey: "colostrum-parity-first", RequestFingerprint: "colostrum-parity-first",
	}); err != nil {
		t.Fatalf("complete 1st Colostrum: %v", err)
	}

	after := colostrumCard(t, mustListColostrum(t, repo, ctx, colostrumBirthDate, wfEventAt), workflowID)
	if after.ActionsDone != 1 {
		t.Fatalf("colostrum done = %d after the Birth-path write, want 1 — the two surfaces are "+
			"supposed to be reading the SAME workflow_actions row", after.ActionsDone)
	}
	if after.ActionsTotal != 5 {
		t.Fatalf("colostrum total = %d, want 5 (unchanged by a completion)", after.ActionsTotal)
	}
	if after.NextAction == nil || after.NextAction.Key == domain.ActionKeyFirstColostrum {
		t.Fatalf("next = %+v, want the following feed once 1st Colostrum is done", after.NextAction)
	}

	// And the reverse direction: the Birth card sees the same completion. Birth counts EVERY
	// operator action, so its numbers are legitimately different — 5 done (four prerequisites +
	// the feed) against a much larger total — but it must include this feed.
	birthPage, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{
		TenantID: wfTenant, Module: domain.ModuleBirth, EventDate: colostrumBirthDate,
		TodayDate: colostrumBirthDate, PageSize: domain.MaxWorkflowPageSize, Now: wfEventAt,
	})
	if err != nil {
		t.Fatalf("birth list: %v", err)
	}
	birthCard := colostrumCard(t, birthPage, workflowID)
	if birthCard.ActionsDone != 5 {
		t.Fatalf("birth done = %d, want 5 (four prerequisites + the colostrum feed)", birthCard.ActionsDone)
	}
	if birthCard.ActionsTotal == after.ActionsTotal {
		t.Fatal("birth and colostrum totals must NOT be the same number: birth counts every operator " +
			"action, colostrum counts one day's feeds")
	}
}

// Yesterday's completions must not leak into today's numerator or denominator (maintainer decision
// 2026-08-06: show that day's colostrum only).
func TestColostrumDayCountersAreIndependentPerDate(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	// Complete every BIRTH-DAY feed directly, bypassing the sequence gate: this test is about the
	// date partition of the counters, not about the ordering rule (which its own test covers).
	if _, err := pool.Exec(ctx, `
UPDATE workflow_actions
SET status = 'completed', completed_at = now(), proof_ref = 'seeded'
WHERE workflow_id = $1::uuid
  AND (section = 'colostrum_session' OR action_key = 'first_colostrum')
  AND (due_at AT TIME ZONE 'Asia/Kolkata')::date = DATE '2026-07-27'`, workflowID); err != nil {
		t.Fatalf("complete birth-day feeds: %v", err)
	}

	birthDay := colostrumCard(t, mustListColostrum(t, repo, ctx, colostrumBirthDate, wfEventAt), workflowID)
	if birthDay.ActionsDone != 5 || birthDay.ActionsTotal != 5 {
		t.Fatalf("birth day = %d/%d, want 5/5", birthDay.ActionsDone, birthDay.ActionsTotal)
	}
	if birthDay.State != domain.WorkflowStateCompleted {
		t.Fatalf("state = %q, want completed for a fully fed day", birthDay.State)
	}

	nextDayAt := time.Date(2026, 7, 28, 12, 0, 0, 0, biztime.DefaultLocation())
	nextDay := colostrumCard(t, mustListColostrum(t, repo, ctx, colostrumNextDate, nextDayAt), workflowID)
	if nextDay.ActionsDone != 0 {
		t.Fatalf("next-day done = %d, want 0 — yesterday's five completions must not carry over",
			nextDay.ActionsDone)
	}
	if nextDay.ActionsTotal != 5 {
		t.Fatalf("next-day total = %d, want 5", nextDay.ActionsTotal)
	}
	if nextDay.State != domain.WorkflowStateOpen {
		t.Fatalf("next-day state = %q, want open", nextDay.State)
	}
}

// Chips are whole-day aggregates and mutually exclusive: overdue + due + completed = all, with page
// size never changing the totals.
func TestColostrumChipsAreDisjointAndCoverTheWholeDay(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	// Mid-afternoon on the birth day: 1st Colostrum (09:30) and the 11:00 slot have passed, so the
	// card is overdue.
	at := time.Date(2026, 7, 27, 15, 30, 0, 0, biztime.DefaultLocation())
	page := mustListColostrum(t, repo, ctx, colostrumBirthDate, at)

	chips := page.Chips
	if chips.All != chips.Overdue+chips.Due+chips.Completed {
		t.Fatalf("chips do not partition the day: all=%d overdue=%d due=%d completed=%d",
			chips.All, chips.Overdue, chips.Due, chips.Completed)
	}
	if chips.AwaitingVideo != 0 {
		t.Fatalf("awaiting_video = %d, want 0 — the lens has no such bucket", chips.AwaitingVideo)
	}
	if chips.Overdue != 1 {
		t.Fatalf("overdue = %d, want 1 (feeds are past their time and unfed)", chips.Overdue)
	}

	// A one-row page must report the SAME chips: summaries are whole-filter aggregates.
	small := colostrumQuery(colostrumBirthDate, "", at)
	small.PageSize = 1
	smallPage, err := repo.ListColostrumDay(ctx, small)
	if err != nil {
		t.Fatalf("small page: %v", err)
	}
	if smallPage.Chips != chips {
		t.Fatalf("page size changed the chips: %+v vs %+v", smallPage.Chips, chips)
	}

	// The overdue FILTER must select exactly the cards the overdue CHIP counted.
	overdue, err := repo.ListColostrumDay(ctx, colostrumQuery(colostrumBirthDate, domain.FilterOverdue, at))
	if err != nil {
		t.Fatalf("overdue filter: %v", err)
	}
	if len(overdue.Items) != chips.Overdue {
		t.Fatalf("overdue filter returned %d cards but the chip said %d", len(overdue.Items), chips.Overdue)
	}
}

// The bell lists PREVIOUS business dates that still hold an unfed, past-due feed, counted in KID
// CARDS (not feed rows — one kid with three missed feeds is one card to go and fix).
func TestColostrumPreviousDayAttentionCountsKidsNotFeeds(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	// Stand on the 29th: both the 27th and the 28th are in the past with unfed feeds.
	at := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	page, err := repo.ListColostrumDay(ctx, domain.ColostrumDayQuery{
		TenantID: wfTenant, Date: "2026-07-29", TodayDate: "2026-07-29",
		PageSize: domain.MaxWorkflowPageSize, Now: at,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.OverdueDates) != 2 {
		t.Fatalf("attention dates = %+v, want the 27th and the 28th", page.OverdueDates)
	}
	if page.OverdueDates[0].Date != colostrumNextDate {
		t.Fatalf("dates must be most-recent first, got %+v", page.OverdueDates)
	}
	for _, item := range page.OverdueDates {
		if item.WorkflowCount != 1 {
			t.Fatalf("%s count = %d, want 1 KID (the kid has several missed feeds that day, but it "+
				"is one card)", item.Date, item.WorkflowCount)
		}
	}
	// The 29th itself holds no feeds, so the day's own page is empty while the bell still points back.
	if page.Chips.All != 0 {
		t.Fatalf("the 29th has no feeds; chips.all = %d, want 0", page.Chips.All)
	}
}

// ---------------------------------------------------------------------------
// Adversarial regressions required by the aggregate/projection contract
// (.agents/skills/goatos-code-review/references/aggregates-and-projections.md).
// ---------------------------------------------------------------------------

// FAN-OUT. A kid has MANY feeds on one date; the card grain is one row per kid per date. If the
// aggregate ever became a join (or lost its GROUP BY), the same kid would appear once per feed and
// the chips would count a single animal five times.
func TestColostrumDayOneToManyFeedsStillYieldOneCardPerKid(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	page := mustListColostrum(t, repo, ctx, colostrumBirthDate, wfEventAt)

	matches := 0
	for _, card := range page.Items {
		if card.WorkflowID == workflowID {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("kid appeared on %d cards, want exactly 1 — five feeds is one card", matches)
	}
	if page.Chips.All != 1 {
		t.Fatalf("chips.all = %d, want 1 kid (not one per feed)", page.Chips.All)
	}
	card := colostrumCard(t, page, workflowID)
	if card.ActionsTotal != 5 {
		t.Fatalf("the many feeds must live in the counter, not in extra rows: total = %d, want 5",
			card.ActionsTotal)
	}
}

// PAGINATION. Chips are whole-day aggregates, so walking the keyset must visit every card exactly
// once while the chip counts stay put — page size changes rows, never summary truth.
func TestColostrumDayPageBoundaryKeepsChipsAndVisitsEveryCard(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	first := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	second := openSecondKidWorkflow(t, repo, pool, ctx)

	full := mustListColostrum(t, repo, ctx, colostrumBirthDate, wfEventAt)
	if full.Chips.All != 2 {
		t.Fatalf("chips.all = %d, want 2 kids", full.Chips.All)
	}

	seen := map[string]int{}
	query := colostrumQuery(colostrumBirthDate, "", wfEventAt)
	query.PageSize = 1
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("keyset did not terminate — the cursor is not advancing")
		}
		page, err := repo.ListColostrumDay(ctx, query)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if page.Chips != full.Chips {
			t.Fatalf("page %d changed the chips: %+v vs %+v", pages, page.Chips, full.Chips)
		}
		for _, card := range page.Items {
			seen[card.WorkflowID]++
		}
		if page.NextCursor == nil {
			break
		}
		cursor, err := domain.DecodeWorkflowCursor(*page.NextCursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		query.Cursor = cursor
	}
	if len(seen) != 2 || seen[first] != 1 || seen[second] != 1 {
		t.Fatalf("keyset walk visited %v, want each of %s and %s exactly once", seen, first, second)
	}
}

// DATE. The feeds of ONE kid deliberately land on two different business dates, and neither date
// may borrow the other's rows. This is the difference between "due date" and "event date" that the
// whole lens exists for.
func TestColostrumDayScheduledDateSplitsFeedsAcrossBusinessDates(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	birthDay := colostrumCard(t, mustListColostrum(t, repo, ctx, colostrumBirthDate, wfEventAt), workflowID)
	nextDayAt := time.Date(2026, 7, 28, 12, 0, 0, 0, biztime.DefaultLocation())
	nextDay := colostrumCard(t, mustListColostrum(t, repo, ctx, colostrumNextDate, nextDayAt), workflowID)

	if birthDay.ActionsTotal != 5 || nextDay.ActionsTotal != 5 {
		t.Fatalf("feeds split %d / %d, want 5 / 5", birthDay.ActionsTotal, nextDay.ActionsTotal)
	}
	// Ten feeds exist in total; each belongs to exactly one date. Neither double-counted nor lost.
	var totalFeeds int
	if err := repo.pool.QueryRow(ctx, `
SELECT count(*)::int FROM workflow_actions
WHERE workflow_id = $1::uuid AND (section = 'colostrum_session' OR action_key = 'first_colostrum')`,
		workflowID).Scan(&totalFeeds); err != nil {
		t.Fatalf("count feeds: %v", err)
	}
	if birthDay.ActionsTotal+nextDay.ActionsTotal != totalFeeds {
		t.Fatalf("the two dates hold %d feeds but the kid has %d",
			birthDay.ActionsTotal+nextDay.ActionsTotal, totalFeeds)
	}
	// A date with no feeds is empty rather than falling back to any other day's rows.
	empty := mustListColostrum(t, repo, ctx, "2026-07-30",
		time.Date(2026, 7, 30, 9, 0, 0, 0, biztime.DefaultLocation()))
	if empty.Chips.All != 0 {
		t.Fatalf("a date with no scheduled feeds returned %d cards", empty.Chips.All)
	}
}

// SCOPE. This lens has no park or cohort dimension — its scope is (tenant, business date). The
// dangerous leak is therefore tenant, and it is worth asserting because the day CTE filters on
// workflow_actions.tenant_id while the card join re-filters on workflow_instances.tenant_id.
func TestColostrumDayScopeHierarchyIsTenantAndDate(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	otherTenant := "aaaaaaa1-0000-0000-0000-0000000000e1"
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Other Tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, otherTenant); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}

	page, err := repo.ListColostrumDay(ctx, domain.ColostrumDayQuery{
		TenantID: otherTenant, Date: colostrumBirthDate, TodayDate: colostrumBirthDate,
		PageSize: domain.MaxWorkflowPageSize, Now: wfEventAt,
	})
	if err != nil {
		t.Fatalf("other tenant list: %v", err)
	}
	if len(page.Items) != 0 || page.Chips.All != 0 {
		t.Fatalf("another tenant saw %d cards / chips %+v — the day window must never cross tenants",
			len(page.Items), page.Chips)
	}
	if len(page.OverdueDates) != 0 {
		t.Fatalf("another tenant saw attention dates %+v", page.OverdueDates)
	}
}

// STATUS MATRIX. Every workflow_actions status a feed can hold, on one date, at once. Each has a
// different effect on the counters, and getting any one wrong silently misreports the day.
func TestColostrumDayStatusBucketsCoverEveryActionStatus(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	// Assign one status to each of the birth day's five feeds, in due order.
	statuses := []string{"completed", "in_review", "rework", "canceled", "pending"}
	rows, err := pool.Query(ctx, `
SELECT action_id::text, action_key FROM workflow_actions
WHERE workflow_id = $1::uuid
  AND (section = 'colostrum_session' OR action_key = 'first_colostrum')
  AND (due_at AT TIME ZONE 'Asia/Kolkata')::date = DATE '2026-07-27'
ORDER BY seq`, workflowID)
	if err != nil {
		t.Fatalf("load feeds: %v", err)
	}
	var actionIDs, actionKeys []string
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		actionIDs = append(actionIDs, id)
		actionKeys = append(actionKeys, key)
	}
	rows.Close()
	if len(actionIDs) != len(statuses) {
		t.Fatalf("expected %d birth-day feeds, got %d", len(statuses), len(actionIDs))
	}
	for i, status := range statuses {
		if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET status = $2 WHERE action_id = $1::uuid`,
			actionIDs[i], status); err != nil {
			t.Fatalf("set %s: %v", status, err)
		}
	}

	at := time.Date(2026, 7, 27, 23, 0, 0, 0, biztime.DefaultLocation())
	page := mustListColostrum(t, repo, ctx, colostrumBirthDate, at)
	card := colostrumCard(t, page, workflowID)

	// canceled leaves the denominator entirely; the other four remain.
	if card.ActionsTotal != 4 {
		t.Fatalf("total = %d, want 4 (canceled excluded)", card.ActionsTotal)
	}
	// Only `completed` is done. in_review, rework and pending are all still the operator's work.
	if card.ActionsDone != 1 {
		t.Fatalf("done = %d, want 1 — only 'completed' counts", card.ActionsDone)
	}
	// The next feed is the earliest incomplete one, which is the in_review row, not the pending one.
	if card.NextAction == nil {
		t.Fatal("a day with outstanding feeds must carry a next action")
	}
	if card.NextAction.Key != actionKeys[1] {
		t.Fatalf("next feed = %q, want %q — the earliest incomplete row, which is the in_review one",
			card.NextAction.Key, actionKeys[1])
	}
	// Chips still partition the day with this mixture present.
	if page.Chips.All != page.Chips.Overdue+page.Chips.Due+page.Chips.Completed {
		t.Fatalf("chips stopped partitioning under a mixed status matrix: %+v", page.Chips)
	}
}

// Migration 000115 is the only thing keeping this lens off a sequential scan of workflow_actions.
// A migration that silently fails to apply leaves every test above green (the queries are correct
// either way at fixture scale) and the page slow in production, so assert the index EXISTS rather
// than inferring it from a passing read.
//
// This deliberately does not assert an EXPLAIN plan: at fixture row counts Postgres will rightly
// choose a sequential scan regardless, so a plan assertion here would prove nothing and would flap.
// Plan proof at realistic volume belongs to `make validate-sqlc-plans`.
func TestColostrumDayIndexIsCreatedByMigration(t *testing.T) {
	_, pool, ctx := newWorkflowRepo(t)
	var indexDef string
	if err := pool.QueryRow(ctx, `
SELECT indexdef FROM pg_indexes
WHERE schemaname = 'public' AND indexname = 'workflow_actions_colostrum_day_idx'`).Scan(&indexDef); err != nil {
		t.Fatalf("workflow_actions_colostrum_day_idx is missing — migration 000115 did not apply: %v", err)
	}
	for _, fragment := range []string{"tenant_id", "due_at", "workflow_id", "colostrum_session", "first_colostrum"} {
		if !strings.Contains(indexDef, fragment) {
			t.Fatalf("index definition %q is missing %q", indexDef, fragment)
		}
	}
}

// openSecondKidWorkflow seeds a second kid born the same moment, so the day holds two cards and the
// keyset has a real boundary to cross.
func openSecondKidWorkflow(t *testing.T, repo *Repository, pool *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	const secondKid = "aaaaaaa1-0000-0000-0000-0000000000fd"
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id,
                   origin_type, dob, entry_date, time_of_birth)
VALUES ($1::uuid, $2::uuid, 'goat', 'male', 'Boer', 'alive', $3::uuid,
        'birth', DATE '2026-07-27', DATE '2026-07-27', TIME '09:30')
ON CONFLICT (goat_id) DO NOTHING`, secondKid, wfTenant, wfCustodian); err != nil {
		t.Fatalf("seed second kid: %v", err)
	}
	return openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, secondKid)
}

func mustListColostrum(t *testing.T, repo *Repository, ctx context.Context, date string, now time.Time) domain.WorkflowListPage {
	t.Helper()
	page, err := repo.ListColostrumDay(ctx, colostrumQuery(date, "", now))
	if err != nil {
		t.Fatalf("list colostrum %s: %v", date, err)
	}
	return page
}
