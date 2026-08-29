package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// TestShiftingEventStatusDispositionMatrix is the recurrence control for the 2026-08-29 defect
// class: a status that EXISTS in the vocabulary but that some layer never handles. The reject
// defect was exactly that — 'rejected' was declared in the DB check constraint and the domain
// constants, no write path produced it, and no read predicate had decided what the Pending tab
// should do with it, so a refused movement sat in 'pending' forever.
//
// Three properties, each of which fails loudly when a future change half-lands a status:
//
//  1. VOCABULARY PARITY. The statuses in the live shifting_events_status_check constraint and the
//     domain's ValidShiftingEventStatus must agree exactly. A migration widening the CHECK without
//     the Go constant (or the reverse) fails here, before any behavior ships on the new status.
//  2. DECIDED DISPOSITION. Every status in that vocabulary must appear in the matrix below with an
//     explicit answer to "which Actions tabs list a row in this state?" — including the explicit
//     empty set for dropped states. A new status with no row fails with instructions, forcing the
//     author to DECIDE its read-side treatment instead of inheriting whatever the predicates
//     happen to do.
//  3. LIST/COUNT AGREEMENT. For every state actually seeded, each tab's StatusCounts number equals
//     the rows that tab lists — a tab badge must never advertise a row its tab hides.
//
// Rows are seeded through RecordShiftingEvent and then UPDATEd into the target state; this is a
// read-model disposition test in the owning package, not an E2E (the E2E lifecycle proof is
// TestKernelStory_ShiftingRejectLifecycle).
func TestShiftingEventStatusDispositionMatrix(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	// -- Property 1: DB constraint <-> domain vocabulary parity ---------------------------------
	var constraintDef string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(oid)
FROM pg_constraint
WHERE conname = 'shifting_events_status_check' AND conrelid = 'shifting_events'::regclass`).
		Scan(&constraintDef); err != nil {
		t.Fatalf("read shifting_events_status_check: %v", err)
	}
	statusRe := regexp.MustCompile(`'([a-z_]+)'::text`)
	dbStatuses := map[string]bool{}
	for _, m := range statusRe.FindAllStringSubmatch(constraintDef, -1) {
		dbStatuses[m[1]] = true
	}
	if len(dbStatuses) == 0 {
		t.Fatalf("parsed no statuses from constraint %q — the parser needs updating", constraintDef)
	}
	for s := range dbStatuses {
		if !domain.ValidShiftingEventStatus(s) {
			t.Errorf("event_status %q is allowed by shifting_events_status_check but unknown to "+
				"domain.ValidShiftingEventStatus — add the domain constant in the same change as the migration", s)
		}
	}
	for _, s := range []string{
		domain.ShiftingEventStatusPending, domain.ShiftingEventStatusAuthorized,
		domain.ShiftingEventStatusPendingVerification, domain.ShiftingEventStatusApplied,
		domain.ShiftingEventStatusRejected, domain.ShiftingEventStatusCanceled, "unresolved",
	} {
		if !dbStatuses[s] {
			t.Errorf("domain status %q is not allowed by shifting_events_status_check — ship the "+
				"constraint widening in the same change as the constant", s)
		}
	}

	// -- Property 2: every status has a DECIDED disposition -------------------------------------
	//
	// verificationState lets the matrix also pin the evidence-rework overlay where it applies.
	// wantTabs is the exact set of tabs (of pending/all/authorized/rework/completed) that must
	// list a row in this state; an empty set means the state is DROPPED from the Actions queue
	// entirely (canceled: retired paperwork; rejected: the approver refused it — 2026-08-29 fix).
	type disposition struct {
		status            string
		verificationState string
		wantTabs          []string
	}
	matrix := []disposition{
		{status: "pending", verificationState: "unverified", wantTabs: []string{"pending"}},
		{status: "authorized", verificationState: "unverified", wantTabs: []string{"all", "authorized"}},
		{status: "authorized", verificationState: "rejected", wantTabs: []string{"all", "rework"}},
		// Legacy rollout state (completed before approval, pre-000049); visible as history in the
		// work list, not actionable through any status tab of its own.
		{status: "pending_verification", verificationState: "unverified", wantTabs: []string{"all"}},
		{status: "applied", verificationState: "unverified", wantTabs: []string{"all", "completed"}},
		{status: "applied", verificationState: "verified", wantTabs: []string{"all", "completed"}},
		{status: "applied", verificationState: "rejected", wantTabs: []string{"all", "rework"}},
		{status: "rejected", verificationState: "unverified", wantTabs: nil},
		{status: "canceled", verificationState: "unverified", wantTabs: nil},
		// Unreachable by any current write path; kept visible in the work list rather than
		// silently dropped, so a row that somehow lands here is seen and questioned.
		{status: "unresolved", verificationState: "unverified", wantTabs: []string{"all"}},
	}
	covered := map[string]bool{}
	for _, d := range matrix {
		covered[d.status] = true
	}
	for s := range dbStatuses {
		if !covered[s] {
			t.Fatalf("event_status %q exists in shifting_events_status_check but has NO decided "+
				"disposition in this matrix. Decide which Actions tab(s) list it — or that it is "+
				"dropped — add the row here, and make the read SQL in shifting_execution.go match. "+
				"Do not let a new status inherit whatever the predicates happen to do (that is how "+
				"'rejected' rows sat on the Pending tab forever until 2026-08-29).", s)
		}
	}

	// -- Seed one movement per matrix row through the production raise/decide paths --------------
	//
	// States beyond 'pending'/'unresolved' are reached through the REAL decision path: approve and
	// reject are production writes, and the executable-source predicate
	// (shiftingExecutableSourceCurrentSQL) lists an outstanding row only when its APPROVED
	// request's animals are still standing at the approved source shed — so every decided case
	// carries a real goat at the source and a real approved/rejected request, exactly like a live
	// movement. Only stamps a single decision cannot reach (completion, cancel, legacy statuses)
	// are placed directly afterwards.
	eventForCase := make([]string, len(matrix))
	for i, d := range matrix {
		key := fmt.Sprintf("disposition-%d", i)
		eventID, _, err := repo.RecordShiftingEvent(ctx, shiftingEventForApproval(key))
		if err != nil {
			t.Fatalf("record movement for case %d (%s/%s): %v", i, d.status, d.verificationState, err)
		}
		eventForCase[i] = eventID

		if d.status != "pending" && d.status != "unresolved" {
			goatID := fmt.Sprintf("00000000-0000-4000-8000-0000000dd%03d", i)
			seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
			payload, err := json.Marshal(map[string]any{
				"shifting_event_id":   eventID,
				"destination_park_id": countsPark,
				"destination_shed_id": countsShedB,
				"goat_ids":            []string{goatID},
			})
			if err != nil {
				t.Fatalf("marshal payload for case %d: %v", i, err)
			}
			req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
				TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeShifting,
				Payload: payload, ShiftingEventID: &eventID,
				RaisedByUserID: countsOperator, RaisedAt: time.Now().AddDate(0, 0, -2),
				IdempotencyKey: "submit-" + key, RequestFingerprint: "submit-fp-" + key,
			})
			if err != nil {
				t.Fatalf("submit approval for case %d: %v", i, err)
			}
			decision := domain.ApprovalDecision{
				TenantID: countsTenant, ApprovalRequestID: req.ApprovalRequestID,
				Status: domain.ApprovalStatusApproved, DecidedByUserID: countsApprover,
				DecidedAt:      time.Now(),
				IdempotencyKey: "decide-" + key, RequestFingerprint: "decide-fp-" + key,
				Effect: &domain.ApprovalEffect{Shifting: &domain.ShiftingApprovalEffect{
					ShiftingEventID: eventID, DestinationParkID: countsPark,
					DestinationShedID: countsShedB, GoatIDs: []string{goatID},
				}},
			}
			if d.status == "rejected" {
				decision.Status = domain.ApprovalStatusRejected
				decision.Reason = "disposition matrix"
				decision.Effect = nil
			}
			if _, _, err := repo.DecideApprovalRequest(ctx, decision); err != nil {
				t.Fatalf("decide case %d (%s/%s): %v", i, d.status, d.verificationState, err)
			}
		}

		set := `verification_state = $3`
		args := []any{countsTenant, eventID, d.verificationState}
		switch d.status {
		case "pending", "authorized", "rejected":
			// Fully produced by the raise/decide path above; only the verification overlay remains.
		case "pending_verification":
			set += `, event_status = 'pending_verification',
			         proof_ref = 'proof-disposition', completed_at = now(), completed_by = $4::uuid`
			args = append(args, countsApprover)
		case "applied":
			set += `, event_status = 'applied',
			         proof_ref = 'proof-disposition', completed_at = now(), completed_by = $4::uuid,
			         applied_at = now()`
			args = append(args, countsApprover)
		case "canceled":
			set += `, event_status = 'canceled',
			         canceled_at = now(), canceled_by = $4::uuid, cancel_reason = 'disposition matrix'`
			args = append(args, countsApprover)
		case "unresolved":
			set += `, event_status = 'unresolved'`
		}
		query := `UPDATE shifting_events SET ` + set + ` WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("place case %d into %s/%s: %v", i, d.status, d.verificationState, err)
		}
	}

	// -- Read every tab once; assert membership per case and count/list agreement ---------------
	// The shared fixture raises two days in the past, so the Actions lead time hides nothing.
	now := time.Now()
	tabs := []string{"pending", "all", "authorized", "rework", "completed"}
	listed := map[string]map[string]bool{} // tab -> eventID -> listed
	tabPages := map[string]domain.ShiftingExecutionPage{}
	for _, tab := range tabs {
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant, Status: tab, Now: now,
		})
		if err != nil {
			t.Fatalf("list %s tab: %v", tab, err)
		}
		tabPages[tab] = page
		listed[tab] = map[string]bool{}
		for _, row := range page.Items {
			listed[tab][row.ShiftingEventID] = true
		}
	}

	for i, d := range matrix {
		want := map[string]bool{}
		for _, tab := range d.wantTabs {
			want[tab] = true
		}
		for _, tab := range tabs {
			got := listed[tab][eventForCase[i]]
			if got != want[tab] {
				t.Errorf("state %s/%s: %s tab listed=%v, want %v — the disposition matrix and the "+
					"read SQL in shifting_execution.go disagree; change BOTH deliberately or neither",
					d.status, d.verificationState, tab, got, want[tab])
			}
		}
	}

	// Count/list agreement, using a window covering the shared fixture's raise date.
	windowFrom := now.AddDate(0, 0, -7)
	windowTo := now.AddDate(0, 0, 1)
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, Status: "all", Now: now,
		RaisedFrom: &windowFrom, RaisedBefore: &windowTo,
	})
	if err != nil {
		t.Fatalf("list all tab with window: %v", err)
	}
	countFor := map[string]int{
		"all":        int(page.StatusCounts.All),
		"pending":    int(page.StatusCounts.Pending),
		"authorized": int(page.StatusCounts.Authorized),
		"rework":     int(page.StatusCounts.Rework),
		"completed":  int(page.StatusCounts.Completed),
	}
	for _, tab := range tabs {
		if got, want := countFor[tab], len(listed[tab]); got != want {
			t.Errorf("%s tab count=%d but the tab lists %d rows — a badge must never advertise a "+
				"row its tab hides (or hide one it advertises)", tab, got, want)
		}
	}

	// Stable summary for the reader: which states are visible where.
	var names []string
	for s := range dbStatuses {
		names = append(names, s)
	}
	sort.Strings(names)
	t.Logf("disposition matrix verified for statuses: %v", names)
}
