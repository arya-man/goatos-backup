package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// Tasks whose readiness is judged by the PINNED card's compulsory keys (PC CARE SOP,
// 2026-09-22) rather than by the legacy proof columns.
const (
	tTwoKeys  = "00000000-0000-4000-8000-000000007201" // deworming, Part 6: video + dose photo
	tNoKeys   = "00000000-0000-4000-8000-000000007202" // hair trimming, Part 7: an EMPTY key list
	tKeysDone = "00000000-0000-4000-8000-000000007203" // ticks removal, Part 8: awaiting the verdict
	tKeysPark = "00000000-0000-4000-8000-000000007204" // the other park, same shape
)

// TestPCCareBoardSlotReadinessMultipleDimensionsParkScopePaginationStatusBucketsFailClosed pins
// the readiness predicate the board counts with, on the grain it counts at.
//
// MULTIPLE DIMENSIONS: three pens whose pinned cards ask for different things -- two compulsory
// captures, one, and NONE -- read in one query; an animal is done only when it carries EVERY
// compulsory key of ITS OWN task, so a card asking for two is not satisfied by one.
//
// FAIL CLOSED is the sharp one: `sop_proofs ?& '{}'` is vacuously TRUE in Postgres, so a row
// stating no requirement would report every unfilmed animal as DONE -- a pen nobody worked
// reading as finished on the CEO's board. The cardinality() test is what stops it, and removing
// it turns this red.
//
// PARK SCOPE: the other park's identically-shaped task never appears. STATUS BUCKETS: an open
// task counts its captures while a submitted one carries its own verification state, and both
// are counted by the same predicate. PAGINATION: a page of one walks every row exactly once,
// and the totals do not move with the page size.
func TestPCCareBoardSlotReadinessMultipleDimensionsParkScopePaginationStatusBucketsFailClosed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	seedSlotReadiness(t, ctx, pool)
	src := New(pool, 5*time.Second)

	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	got := byTask(rows)

	if _, leaked := got[tKeysPark]; leaked {
		t.Fatal("the other park's task leaked into this park's board")
	}

	// Two compulsory captures: the animal carrying both is done, the one carrying only the
	// video is still pending. One card, one animal each side, counted per animal.
	two, ok := got[tTwoKeys]
	if !ok {
		t.Fatal("the two-capture task is missing from the board")
	}
	if two.Counts.Done != 1 || two.Counts.Pending != 1 {
		t.Fatalf("two-capture card: done=%d pending=%d, want 1 and 1 (a card asking for two captures is not satisfied by one)",
			two.Counts.Done, two.Counts.Pending)
	}

	// FAIL CLOSED: a task stating no requirement counts NOBODY as done. Both its animals are
	// unfilmed; a vacuous has-all-keys test would report 2 done and a finished pen.
	none, ok := got[tNoKeys]
	if !ok {
		t.Fatal("the no-requirement task is missing from the board")
	}
	if none.Counts.Done != 0 || none.Counts.Pending != 2 {
		t.Fatalf("empty key list: done=%d pending=%d, want 0 and 2 (an empty requirement must fail closed, never read as done)",
			none.Counts.Done, none.Counts.Pending)
	}
	if none.WorkState != domain.WorkStateInProgress {
		t.Fatalf("empty key list: state=%s, want %s (scanned but nothing proven is work in progress)", none.WorkState, domain.WorkStateInProgress)
	}

	// A submitted task is counted by the same predicate as an open one: its single animal
	// carries the one key its card asks for.
	done, ok := got[tKeysDone]
	if !ok {
		t.Fatal("the submitted task is missing from the board")
	}
	if done.Counts.Done != 1 || done.Counts.Pending != 0 {
		t.Fatalf("submitted task: done=%d pending=%d, want 1 and 0", done.Counts.Done, done.Counts.Pending)
	}
	if done.WorkState != domain.WorkStateVerificationPending {
		t.Fatalf("submitted task: state=%s, want %s", done.WorkState, domain.WorkStateVerificationPending)
	}

	// PAGINATION: the same rows, one page at a time, with the same counts.
	seen := map[string]domain.Counts{}
	after := ""
	for i := 0; i < 40; i++ {
		q := query("")
		q.Limit = 1
		q.AfterSourceID = after
		page, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if page[0].SourceID <= after {
			t.Fatalf("keyset went backwards at %s after %s", page[0].SourceID, after)
		}
		if _, repeat := seen[page[0].SourceID]; repeat {
			t.Fatalf("keyset repeated %s", page[0].SourceID)
		}
		seen[page[0].SourceID] = page[0].Counts
		after = page[0].SourceID
	}
	if len(seen) != len(rows) {
		t.Fatalf("keyset walk saw %d rows, one page has %d", len(seen), len(rows))
	}
	for id, counts := range seen {
		if counts != got[id].Counts {
			t.Fatalf("%s counts moved with the page size: %+v paged, %+v whole", id, counts, got[id].Counts)
		}
	}
}

// seedSlotReadiness lays the four readiness tasks over the board seed: differing compulsory key
// lists, differing statuses, two pens plus the other park.
func seedSlotReadiness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	type task struct {
		id, category, park, partition, status string
		required                              string
	}
	for _, tk := range []task{
		{tTwoKeys, "deworming", bsPark, "Part 6", "open", `ARRAY['video','dose_photo']`},
		{tNoKeys, "hair_trimming", bsPark, "Part 7", "open", `ARRAY[]::text[]`},
		{tKeysDone, "ticks_removal", bsPark, "Part 8", "pending_verification", `ARRAY['video']`},
		{tKeysPark, "deworming", bsOtherPk, "Part 6", "open", `ARRAY['video','dose_photo']`},
	} {
		exec(t, ctx, pool, `
INSERT INTO pc_care_tasks (task_id, tenant_id, category, park_id, shed_id, partition_label,
                           planned_business_date, due_business_date, work_state, status,
                           idempotency_key, created_by, slot_keys, required_slot_keys)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, NULLIF($6, ''), $7::date, $7::date, 'scheduled', $8,
        'slots-' || $1, $9::uuid, ARRAY['video','dose_photo'], `+tk.required+`)
ON CONFLICT (task_id) DO NOTHING`, tk.id, bsTenant, tk.category, tk.park, bsShed, tk.partition, bsDate, tk.status, bsCEO)
		exec(t, ctx, pool, `
INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id)
VALUES ($1::uuid, $2::uuid, $3::uuid) ON CONFLICT DO NOTHING`, bsTenant, tk.id, bsOperator)
	}

	// An animal and the captures it carries, stored the way the module writes them.
	scanSlots := func(task, tag, proofs string) {
		exec(t, ctx, pool, `
INSERT INTO pc_care_task_animals (tenant_id, task_id, scanned_identifier, scanned_by, sop_proofs, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::jsonb, 'slots-' || $2 || '-' || $3) ON CONFLICT DO NOTHING`,
			bsTenant, task, tag, bsOperator, proofs)
	}
	scanSlots(tTwoKeys, "tag-two-both", `{"video":"proof/v","dose_photo":"proof/p"}`)
	scanSlots(tTwoKeys, "tag-two-half", `{"video":"proof/v"}`)
	// The no-requirement pen: two animals, NOTHING captured.
	scanSlots(tNoKeys, "tag-none-a", `{}`)
	scanSlots(tNoKeys, "tag-none-b", `{}`)
	scanSlots(tKeysDone, "tag-done", `{"video":"proof/v"}`)
	scanSlots(tKeysPark, "tag-other-park", `{"video":"proof/v","dose_photo":"proof/p"}`)
}
