package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	leaveTenant    = "00000000-0000-4000-8000-000000000001"
	leavePark      = "7a000000-0000-4000-8000-000000000101"
	leaveMember    = "97000000-0000-4000-8000-000000000301"
	leaveUser      = "91000000-0000-4000-8000-000000000301"
	leaveHeadMbr   = "97000000-0000-4000-8000-000000000302"
	leaveHeadUser  = "91000000-0000-4000-8000-000000000302"
	leaveHRMbr     = "97000000-0000-4000-8000-000000000303"
	leaveHRUser    = "91000000-0000-4000-8000-000000000303"
	leaveOtherPark = "7a000000-0000-4000-8000-000000000102"
)

// TestLeaveRequestWritePathWithDockerPostgres proves the whole leave workflow on the real
// database (maintainer decisions 2026-09-10): raise (+ exact replay, + same-key/different-payload
// conflict, + overlap refusal), the approver queue per slot, withdraw, BOTH slots approving
// mirrors one workforce_absences row in the same transaction, EITHER slot rejecting ends the
// request, and every write leaves its outbox event behind.
func TestLeaveRequestWritePathWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Leave Test Tenant', 'active') ON CONFLICT (tenant_id) DO NOTHING`, leaveTenant)
	for _, p := range [][2]string{{leavePark, "Coimbatore"}, {leaveOtherPark, "Channapatna"}} {
		mustExec(`INSERT INTO locations (location_id, tenant_id, location_type, name, status) VALUES ($1::uuid, $2::uuid, 'park', $3, 'active')`, p[0], leaveTenant, p[1])
	}
	for _, m := range [][4]string{
		{leaveMember, leaveUser, "Ramesh", "operator"},
		{leaveHeadMbr, leaveHeadUser, "Karthik", "park_head"},
		{leaveHRMbr, leaveHRUser, "Priya", "other"},
	} {
		mustExec(`
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $4, 'active', $5, $6::uuid)`, m[0], leaveTenant, m[1], m[2], m[3], leavePark)
	}
	// The park head is a park-scoped ROLE GRANT (most hold no seat); HR is a tenant grant.
	mustExec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'park', $3::uuid, 'active', now() - interval '1 day')`, leaveTenant, leaveHeadUser, leavePark)
	mustExec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'hr', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, leaveTenant, leaveHRUser)

	repo := NewRepository(pool, 5*time.Second)

	// Approver resolution: the park head of THIS park and every HR holder.
	approvers, err := repo.ResolveLeaveApproverMembers(ctx, leaveTenant, leavePark)
	if err != nil {
		t.Fatalf("resolve approvers: %v", err)
	}
	if len(approvers.ParkHead) != 1 || approvers.ParkHead[0] != leaveHeadMbr || len(approvers.HR) != 1 || approvers.HR[0] != leaveHRMbr {
		t.Fatalf("approvers = %+v", approvers)
	}
	other, err := repo.ResolveLeaveApproverMembers(ctx, leaveTenant, leaveOtherPark)
	if err != nil || len(other.ParkHead) != 0 {
		t.Fatalf("another park must resolve no park head: %+v %v", other, err)
	}

	raise := func(key, starts, ends string) (ports.LeaveRequestWrite, error) {
		return repo.CreateLeaveRequest(ctx, ports.CreateLeaveRequestCommand{
			TenantID: leaveTenant, WorkforceMemberID: leaveMember, ActorUserID: leaveUser,
			StartsOn: starts, EndsOn: ends, Reason: "Sister's wedding", IdempotencyKey: key,
			ParkHeadRequired: true, HRRequired: true, BusinessDate: "2026-09-10",
		})
	}

	// 1. Raise.
	first, err := raise("req-1", "2026-09-12", "2026-09-14")
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if first.Replayed || first.Row.Status != domain.LeaveRequestStatusPending || first.Row.ParkID != leavePark || first.Row.ParkLabel != "Coimbatore" || first.Row.PersonName != "Ramesh" {
		t.Fatalf("raise row = %+v", first.Row)
	}
	// Exact replay returns the ORIGINAL and writes nothing.
	replay, err := raise("req-1", "2026-09-12", "2026-09-14")
	if err != nil || !replay.Replayed || replay.Row.LeaveRequestID != first.Row.LeaveRequestID {
		t.Fatalf("replay = %+v err=%v", replay, err)
	}
	// Same key, different payload is refused.
	if _, err := raise("req-1", "2026-09-12", "2026-09-15"); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different payload: want ErrIdempotencyConflict, got %v", err)
	}
	// An overlapping window is refused while the first is pending.
	if _, err := raise("req-2", "2026-09-14", "2026-09-16"); !errors.Is(err, ports.ErrLeaveOverlap) {
		t.Fatalf("overlap: want ErrLeaveOverlap, got %v", err)
	}
	var requests, events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workforce_leave_requests WHERE tenant_id=$1`, leaveTenant).Scan(&requests); err != nil || requests != 1 {
		t.Fatalf("requests=%d err=%v (replay/conflict/overlap must write nothing)", requests, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='workforce.leave.requested'`, leaveTenant).Scan(&events); err != nil || events != 1 {
		t.Fatalf("requested events=%d err=%v", events, err)
	}

	// 2. Queue per slot: the park head of THIS park sees it; another park's head does not;
	//    HR sees it; the CEO floor sees it.
	page, err := repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{TenantID: leaveTenant, ParkHeadParks: []string{leavePark}})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("park head queue: rows=%d err=%v", len(page.Rows), err)
	}
	page, err = repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{TenantID: leaveTenant, ParkHeadParks: []string{leaveOtherPark}})
	if err != nil || len(page.Rows) != 0 {
		t.Fatalf("other park head queue: rows=%d err=%v", len(page.Rows), err)
	}
	page, err = repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{TenantID: leaveTenant, HR: true})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("hr queue: rows=%d err=%v", len(page.Rows), err)
	}
	page, err = repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{TenantID: leaveTenant, Any: true})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("ceo queue: rows=%d err=%v", len(page.Rows), err)
	}

	// 3. Withdraw: only the requester, only while pending.
	if _, err := repo.WithdrawLeaveRequest(ctx, ports.WithdrawLeaveRequestCommand{
		TenantID: leaveTenant, LeaveRequestID: first.Row.LeaveRequestID, WorkforceMemberID: leaveHeadMbr, ActorUserID: leaveHeadUser, IdempotencyKey: "wd-x", BusinessDate: "2026-09-10",
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("someone else withdrawing: want ErrNotFound, got %v", err)
	}
	withdrawn, err := repo.WithdrawLeaveRequest(ctx, ports.WithdrawLeaveRequestCommand{
		TenantID: leaveTenant, LeaveRequestID: first.Row.LeaveRequestID, WorkforceMemberID: leaveMember, ActorUserID: leaveUser, IdempotencyKey: "wd-1", BusinessDate: "2026-09-10",
	})
	if err != nil || withdrawn.Row.Status != domain.LeaveRequestStatusWithdrawn || withdrawn.Row.DecidedAt == nil {
		t.Fatalf("withdraw = %+v err=%v", withdrawn.Row, err)
	}
	if _, err := repo.DecideLeaveRequest(ctx, ports.DecideLeaveRequestCommand{
		TenantID: leaveTenant, LeaveRequestID: first.Row.LeaveRequestID, Slot: domain.LeaveSlotHR, Decision: domain.LeaveDecisionApproved, ActorUserID: leaveHRUser, IdempotencyKey: "dec-x", BusinessDate: "2026-09-10",
	}); !errors.Is(err, ports.ErrLeaveNotPending) {
		t.Fatalf("deciding a withdrawn request: want ErrLeaveNotPending, got %v", err)
	}
	// The withdrawn window no longer blocks a new request.
	second, err := raise("req-3", "2026-09-12", "2026-09-14")
	if err != nil {
		t.Fatalf("raise after withdraw: %v", err)
	}

	// 4. Park head approves: still pending, waiting for HR; a second park-head decision is
	//    refused; the HR slot cannot be signed as park_head.
	decide := func(id, slot, decision, key string) (ports.LeaveRequestWrite, error) {
		actor := leaveHeadUser
		if slot == domain.LeaveSlotHR {
			actor = leaveHRUser
		}
		return repo.DecideLeaveRequest(ctx, ports.DecideLeaveRequestCommand{
			TenantID: leaveTenant, LeaveRequestID: id, Slot: slot, Decision: decision, Note: "ok", ActorUserID: actor, IdempotencyKey: key, BusinessDate: "2026-09-10",
		})
	}
	afterHead, err := decide(second.Row.LeaveRequestID, domain.LeaveSlotParkHead, domain.LeaveDecisionApproved, "dec-1")
	if err != nil {
		t.Fatalf("park head approve: %v", err)
	}
	if afterHead.Row.Status != domain.LeaveRequestStatusPending || afterHead.Row.ParkHeadDecision != "approved" || afterHead.Row.ParkHeadDeciderNm != "Karthik" || afterHead.Row.HRDecision != "" {
		t.Fatalf("after park head = %+v", afterHead.Row)
	}
	if _, err := decide(second.Row.LeaveRequestID, domain.LeaveSlotParkHead, domain.LeaveDecisionApproved, "dec-1b"); !errors.Is(err, ports.ErrLeaveSlotDecided) {
		t.Fatalf("second park head decision: want ErrLeaveSlotDecided, got %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type IN ('workforce.leave.approved','workforce.leave.rejected')`, leaveTenant).Scan(&events); err != nil || events != 0 {
		t.Fatalf("one slot approving must push nobody; events=%d err=%v", events, err)
	}
	// The park head's queue no longer lists it; HR's still does.
	page, _ = repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{TenantID: leaveTenant, ParkHeadParks: []string{leavePark}})
	if len(page.Rows) != 0 {
		t.Fatalf("park head queue after signing: rows=%d", len(page.Rows))
	}
	page, _ = repo.ListLeaveQueue(ctx, ports.LeaveQueueParams{TenantID: leaveTenant, HR: true})
	if len(page.Rows) != 1 {
		t.Fatalf("hr queue after park head signed: rows=%d", len(page.Rows))
	}

	// 5. HR approves: approved, and ONE workforce_absences row mirrored in the same transaction.
	approved, err := decide(second.Row.LeaveRequestID, domain.LeaveSlotHR, domain.LeaveDecisionApproved, "dec-2")
	if err != nil {
		t.Fatalf("hr approve: %v", err)
	}
	if approved.Row.Status != domain.LeaveRequestStatusApproved || approved.Row.AbsenceID == "" || approved.Row.DecidedAt == nil {
		t.Fatalf("approved = %+v", approved.Row)
	}
	var absStatus, absReason, startsAt, endsAt string
	if err := pool.QueryRow(ctx, `
SELECT status, reason_code, (starts_at AT TIME ZONE 'Asia/Kolkata')::text, (ends_at AT TIME ZONE 'Asia/Kolkata')::text
FROM workforce_absences WHERE tenant_id=$1 AND absence_id=$2::uuid`, leaveTenant, approved.Row.AbsenceID).Scan(&absStatus, &absReason, &startsAt, &endsAt); err != nil {
		t.Fatalf("mirror absence: %v", err)
	}
	if absStatus != "approved" || absReason != "planned_leave" || startsAt != "2026-09-12 00:00:00" || endsAt != "2026-09-15 00:00:00" {
		t.Fatalf("absence mirror = %s %s %s..%s (ends_at is exclusive: the day after ends_on)", absStatus, absReason, startsAt, endsAt)
	}
	today, err := repo.LeaveTodayForMember(ctx, leaveTenant, leaveMember, "2026-09-13")
	if err != nil || today == nil || today.LeaveRequestID != second.Row.LeaveRequestID {
		t.Fatalf("leave today: %+v err=%v", today, err)
	}
	if off, _ := repo.LeaveTodayForMember(ctx, leaveTenant, leaveMember, "2026-09-15"); off != nil {
		t.Fatalf("the day after ends_on must not read as leave: %+v", off)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='workforce.leave.approved'`, leaveTenant).Scan(&events); err != nil || events != 1 {
		t.Fatalf("approved events=%d err=%v", events, err)
	}

	// 6. Either slot rejecting ends a request at once, mirrors NO absence.
	third, err := raise("req-4", "2026-10-01", "2026-10-01")
	if err != nil {
		t.Fatalf("raise third: %v", err)
	}
	rejected, err := decide(third.Row.LeaveRequestID, domain.LeaveSlotHR, domain.LeaveDecisionRejected, "dec-3")
	if err != nil {
		t.Fatalf("hr reject: %v", err)
	}
	if rejected.Row.Status != domain.LeaveRequestStatusRejected || rejected.Row.AbsenceID != "" || rejected.Row.HRNote != "ok" {
		t.Fatalf("rejected = %+v", rejected.Row)
	}
	if _, err := decide(third.Row.LeaveRequestID, domain.LeaveSlotParkHead, domain.LeaveDecisionApproved, "dec-4"); !errors.Is(err, ports.ErrLeaveNotPending) {
		t.Fatalf("park head after HR rejected: want ErrLeaveNotPending, got %v", err)
	}
	var absences int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workforce_absences WHERE tenant_id=$1 AND workforce_member_id=$2`, leaveTenant, leaveMember).Scan(&absences); err != nil || absences != 1 {
		t.Fatalf("absences=%d err=%v (a rejection mirrors nothing)", absences, err)
	}

	// 7. Own history lists every request, newest window first; admin list filters by status.
	mine, err := repo.ListLeaveRequestsForMember(ctx, leaveTenant, leaveMember, "2026-09-10", 20)
	if err != nil || len(mine) != 3 || mine[0].LeaveRequestID != third.Row.LeaveRequestID {
		t.Fatalf("own history: n=%d err=%v first=%+v", len(mine), err, mine)
	}
	admin, err := repo.ListLeaveRequestsAdmin(ctx, ports.LeaveAdminListParams{TenantID: leaveTenant, Status: domain.LeaveRequestStatusApproved})
	if err != nil || len(admin.Rows) != 1 || admin.Rows[0].LeaveRequestID != second.Row.LeaveRequestID {
		t.Fatalf("admin approved list: %+v err=%v", admin.Rows, err)
	}
	// Keyset paging: page size 1 walks all three without repeats.
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 5; i++ {
		p, err := repo.ListLeaveRequestsAdmin(ctx, ports.LeaveAdminListParams{TenantID: leaveTenant, Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		for _, r := range p.Rows {
			if seen[r.LeaveRequestID] {
				t.Fatalf("row %s repeated across pages", r.LeaveRequestID)
			}
			seen[r.LeaveRequestID] = true
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("paged %d rows, want 3", len(seen))
	}

	// 8. Routing config: absent reads as both required; optimistic upsert.
	cfg, err := repo.GetLeaveApprovalConfig(ctx, leaveTenant)
	if err != nil || cfg.Stored || !cfg.ParkHeadRequired || !cfg.HRRequired {
		t.Fatalf("default config = %+v err=%v", cfg, err)
	}
	saved, err := repo.SetLeaveApprovalConfig(ctx, leaveTenant, leaveHRUser, domain.LeaveApprovalConfigUpdate{ParkHeadRequired: false, HRRequired: true, RowVersion: 0})
	if err != nil || !saved.Stored || saved.ParkHeadRequired || saved.RowVersion != 1 || saved.UpdatedByName != "Priya" {
		t.Fatalf("saved config = %+v err=%v", saved, err)
	}
	if _, err := repo.SetLeaveApprovalConfig(ctx, leaveTenant, leaveHRUser, domain.LeaveApprovalConfigUpdate{ParkHeadRequired: true, HRRequired: true, RowVersion: 0}); !errors.Is(err, ports.ErrLeaveConfigConflict) {
		t.Fatalf("stale row_version: want ErrLeaveConfigConflict, got %v", err)
	}
}
