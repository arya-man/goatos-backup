package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/browserpush"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The shared-browser-profile hijack, closed.
//
// WHAT THESE TESTS ARE ABOUT. browser_install_id is minted once into a Chrome profile's
// localStorage (apps/admin-web/lib/web-push-state.ts) and SURVIVES SIGN-OUT, so it names a
// BROWSER and never a person: every colleague who signs in on one office desktop presents the
// same id. The upsert's ON CONFLICT had no predicate tying the conflicting row to the calling
// member, so the second person to sign in silently refreshed the FIRST person's registration --
// the row stayed attributed to the first person, went back to 'active', and every push for them
// (a leadership-task mention carries the task title and a note excerpt in its body) was delivered
// to the browser the second person was sitting at, while the second person got no registration of
// her own and a dashboard showing zero browsers.
//
// Every test below drives the PRODUCTION repository against real SQL. A hand-written fixture row
// would prove nothing here, because the defect and the fix are both entirely in one ON CONFLICT
// clause.
const (
	bpTenant = "00000000-0000-4000-8000-00000000c001"
	// Two DIFFERENT people who share one office desktop.
	bpUserBob   = "00000000-0000-4000-8000-00000000c0b1"
	bpUserAlice = "00000000-0000-4000-8000-00000000c0a1"
	// Someone who is authenticated but is not a member of this tenant at all.
	bpUserOutsider = "00000000-0000-4000-8000-00000000c0f1"

	// ONE browser profile, shared. This is the whole premise.
	bpSharedInstall = "web-shared-office-desktop"
	// An FCM web registration token belongs to the browser PROFILE, not to the signed-in user, so
	// the token Bob's session read and the token Alice's session reads on that same desktop are
	// the same string. That is why presenting it is proof of sitting at that browser.
	bpProfileToken = "fcm-web-shared-profile-token"
)

func seedBrowserPushMembers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Browser Push Test', 'active') ON CONFLICT DO NOTHING`, bpTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	people := []struct{ userID, code, name string }{
		{bpUserBob, "BOB", "Bob"},
		{bpUserAlice, "ALI", "Alice"},
	}
	for _, p := range people {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active')`, bpTenant, p.userID, p.code, p.name); err != nil {
			t.Fatalf("seed member %s: %v", p.name, err)
		}
	}
}

func newBrowserPushRepo(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedBrowserPushMembers(t, ctx, pool)
	return NewRepository(pool, 10*time.Second), ctx
}

func register(t *testing.T, repo *Repository, ctx context.Context, userID, installID, token string, now time.Time) (browserpush.Registration, bool, error) {
	t.Helper()
	return repo.Upsert(ctx, bpTenant, userID, browserpush.RegisterRequest{
		BrowserInstallID: installID,
		Token:            token,
		BrowserLabel:     "Google Chrome",
		UserAgent:        "test-agent",
	}, now)
}

func memberIDOf(t *testing.T, repo *Repository, ctx context.Context, userID string) string {
	t.Helper()
	var memberID string
	if err := repo.pool.QueryRow(ctx, `
SELECT workforce_member_id::text FROM workforce_members
 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, bpTenant, userID).Scan(&memberID); err != nil {
		t.Fatalf("resolve member for %s: %v", userID, err)
	}
	return memberID
}

// TestSecondMemberCannotSilentlyTakeOverASharedBrowserProfile is the regression for the leak.
//
// It asserts BOTH halves of the defect, because the fix is only real if both close: Bob's
// registration is not rewritten (no delivery of Bob's notifications to Alice's screen), AND Alice
// is not left silently unreachable -- she is told, recoverably, so her client can mint its own
// install id, which the next test exercises.
func TestSecondMemberCannotSilentlyTakeOverASharedBrowserProfile(t *testing.T) {
	repo, ctx := newBrowserPushRepo(t)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	bobMember := memberIDOf(t, repo, ctx, bpUserBob)
	aliceMember := memberIDOf(t, repo, ctx, bpUserAlice)

	bob, created, err := register(t, repo, ctx, bpUserBob, bpSharedInstall, bpProfileToken, now)
	if err != nil || !created {
		t.Fatalf("Bob's first registration: created=%v err=%v", created, err)
	}
	if bob.WorkforceMemberID != bobMember {
		t.Fatalf("Bob's registration is attributed to %s, want %s", bob.WorkforceMemberID, bobMember)
	}

	// Alice signs in on the same Chrome profile AFTER the token rotated, so she holds a different
	// token and has no proof of being at Bob's browser. This is the hijack attempt, and it is
	// also exactly what any authenticated tenant user who merely LEARNED Bob's install id can do.
	_, _, err = register(t, repo, ctx, bpUserAlice, bpSharedInstall, "fcm-web-alice-rotated", now.Add(time.Hour))
	if !errors.Is(err, browserpush.ErrBrowserInstallConflict) {
		t.Fatalf("Alice's register on Bob's live browser profile: err=%v, want ErrBrowserInstallConflict", err)
	}
	// NOT the member-not-found answer: Alice is a perfectly good member, and telling her she is
	// not would send her to support for a conflict her own client can resolve.
	if errors.Is(err, browserpush.ErrRegistrationNotFound) {
		t.Fatalf("a browser conflict must not be reported as an authorization failure: %v", err)
	}

	// Bob's row is untouched: same owner, same token, still active, same row_version. row_version
	// is the sharpest assertion here -- the pre-fix code bumped it, because it really did rewrite
	// the row.
	bobRows, err := repo.ListForMember(ctx, bpTenant, bpUserBob)
	if err != nil {
		t.Fatalf("list Bob: %v", err)
	}
	if len(bobRows) != 1 {
		t.Fatalf("Bob has %d registrations, want 1", len(bobRows))
	}
	if got := bobRows[0]; got.WorkforceMemberID != bobMember || got.Status != browserpush.StatusActive || got.RowVersion != 1 {
		t.Fatalf("Bob's row was rewritten by Alice's register: %+v", got)
	}
	var storedToken string
	if err := repo.pool.QueryRow(ctx, `
SELECT fcm_token FROM workforce_member_browser_push_registrations
 WHERE tenant_id = $1::uuid AND browser_install_id = $2`, bpTenant, bpSharedInstall).Scan(&storedToken); err != nil {
		t.Fatalf("read stored token: %v", err)
	}
	if storedToken != bpProfileToken {
		t.Fatalf("Bob's stored token was refreshed to Alice's: %q", storedToken)
	}

	// And nothing was attributed to Alice, in either direction.
	aliceRows, err := repo.ListForMember(ctx, bpTenant, bpUserAlice)
	if err != nil {
		t.Fatalf("list Alice: %v", err)
	}
	if len(aliceRows) != 0 {
		t.Fatalf("Alice has %d registrations after a refused register, want 0", len(aliceRows))
	}
	if aliceMember == bobMember {
		t.Fatalf("fixture is wrong: the two people resolved to one member")
	}
}

// TestBothMembersOfOneDesktopReceiveTheirOwnPushes is the not-user-hostile half.
//
// A refusal that left the second person permanently unable to register would be a different bug
// with the same shape (she silently receives nothing). The client answers 409
// browser_push_install_conflict by minting a fresh install id -- localStorage is the only thing
// that id ever meant -- and this proves that once both are registered, the fan-out resolves each
// person to their OWN browser and token, with no crossover in either direction.
func TestBothMembersOfOneDesktopReceiveTheirOwnPushes(t *testing.T) {
	repo, ctx := newBrowserPushRepo(t)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	bobMember := memberIDOf(t, repo, ctx, bpUserBob)
	aliceMember := memberIDOf(t, repo, ctx, bpUserAlice)

	if _, _, err := register(t, repo, ctx, bpUserBob, bpSharedInstall, bpProfileToken, now); err != nil {
		t.Fatalf("Bob register: %v", err)
	}
	if _, _, err := register(t, repo, ctx, bpUserAlice, bpSharedInstall, "fcm-web-alice-rotated", now); !errors.Is(err, browserpush.ErrBrowserInstallConflict) {
		t.Fatalf("Alice register on Bob's profile: %v", err)
	}
	// The client's recovery: a fresh install id for the same physical browser.
	aliceReg, created, err := register(t, repo, ctx, bpUserAlice, "web-shared-office-desktop-alice", "fcm-web-alice-rotated", now.Add(time.Minute))
	if err != nil || !created {
		t.Fatalf("Alice register after minting a new install id: created=%v err=%v", created, err)
	}
	if aliceReg.WorkforceMemberID != aliceMember {
		t.Fatalf("Alice's own registration is attributed to %s, want %s", aliceReg.WorkforceMemberID, aliceMember)
	}

	bobRecipients, err := repo.ResolveMemberRecipients(ctx, bpTenant, bpUserBob)
	if err != nil {
		t.Fatalf("resolve Bob: %v", err)
	}
	if len(bobRecipients) != 1 || bobRecipients[0].Token != bpProfileToken || bobRecipients[0].WorkforceMemberID != bobMember {
		t.Fatalf("Bob's push fan-out is wrong: %+v", bobRecipients)
	}
	aliceRecipients, err := repo.ResolveMemberRecipients(ctx, bpTenant, bpUserAlice)
	if err != nil {
		t.Fatalf("resolve Alice: %v", err)
	}
	if len(aliceRecipients) != 1 || aliceRecipients[0].Token != "fcm-web-alice-rotated" || aliceRecipients[0].WorkforceMemberID != aliceMember {
		t.Fatalf("Alice's push fan-out is wrong: %+v", aliceRecipients)
	}
	// The crossover assertion, stated directly: neither person's address reaches the other.
	if bobRecipients[0].Token == aliceRecipients[0].Token {
		t.Fatalf("both people resolve to one push address, which is the leak")
	}
}

// TestHandOverOnProofOfTheBrowserTransfersTheRow is the case a plain member predicate alone would
// have broken: the desk really did change hands.
//
// Alice presents the token the stored row already holds. Only a session at that physical browser
// can, because an FCM web token is issued to the profile and does not change when the signed-in
// user does. The row is TRANSFERRED rather than duplicated -- see the grain note on migration
// 000347: two live rows for one profile would both carry that one browser-scoped token, so Bob's
// notification would still arrive on Alice's screen.
func TestHandOverOnProofOfTheBrowserTransfersTheRow(t *testing.T) {
	repo, ctx := newBrowserPushRepo(t)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	bobMember := memberIDOf(t, repo, ctx, bpUserBob)
	aliceMember := memberIDOf(t, repo, ctx, bpUserAlice)

	if _, _, err := register(t, repo, ctx, bpUserBob, bpSharedInstall, bpProfileToken, now); err != nil {
		t.Fatalf("Bob register: %v", err)
	}
	handOver, _, err := register(t, repo, ctx, bpUserAlice, bpSharedInstall, bpProfileToken, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Alice's hand-over register with the profile's own token: %v", err)
	}
	if handOver.WorkforceMemberID != aliceMember {
		t.Fatalf("the handed-over row is attributed to %s, want Alice %s", handOver.WorkforceMemberID, aliceMember)
	}
	// created_at moves with the row: the registration is Alice's from now on, and dating it from
	// Bob's first sign-in would misreport whose address it is and since when.
	if !handOver.CreatedAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("handed-over created_at = %v, want the hand-over instant", handOver.CreatedAt)
	}
	var registeredBy string
	if err := repo.pool.QueryRow(ctx, `
SELECT registered_by::text FROM workforce_member_browser_push_registrations
 WHERE tenant_id = $1::uuid AND browser_install_id = $2`, bpTenant, bpSharedInstall).Scan(&registeredBy); err != nil {
		t.Fatalf("read registered_by: %v", err)
	}
	if registeredBy != aliceMember {
		t.Fatalf("registered_by = %s after hand-over, want Alice %s", registeredBy, aliceMember)
	}

	// Bob is no longer reachable at a browser he has signed out of, which is the correct outcome
	// and the reason the row moves instead of multiplying.
	bobRows, err := repo.ListForMember(ctx, bpTenant, bpUserBob)
	if err != nil {
		t.Fatalf("list Bob: %v", err)
	}
	if len(bobRows) != 0 {
		t.Fatalf("Bob still holds %d registrations for a browser he handed over: %+v", len(bobRows), bobRows)
	}
	bobRecipients, err := repo.ResolveMemberRecipients(ctx, bpTenant, bpUserBob)
	if err != nil {
		t.Fatalf("resolve Bob: %v", err)
	}
	if len(bobRecipients) != 0 {
		t.Fatalf("Bob's notifications would still reach the handed-over browser: %+v", bobRecipients)
	}
	if bobMember == aliceMember {
		t.Fatalf("fixture is wrong: the two people resolved to one member")
	}

	// Exactly one row for the profile. A second would carry the same token and re-open the leak.
	var rows int
	if err := repo.pool.QueryRow(ctx, `
SELECT count(*) FROM workforce_member_browser_push_registrations
 WHERE tenant_id = $1::uuid AND browser_install_id = $2`, bpTenant, bpSharedInstall).Scan(&rows); err != nil {
		t.Fatalf("count profile rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the shared profile holds %d registrations, want exactly 1", rows)
	}
}

// TestARetiredRegistrationLeavesTheBrowserFreeToClaim: an 'unsubscribed' or 'stale' row is an
// address nobody is relying on, so the next person may claim the profile even with a new token.
// Without this branch a colleague who had switched notifications off would block the desk's next
// occupant forever.
func TestARetiredRegistrationLeavesTheBrowserFreeToClaim(t *testing.T) {
	repo, ctx := newBrowserPushRepo(t)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	aliceMember := memberIDOf(t, repo, ctx, bpUserAlice)

	if _, _, err := register(t, repo, ctx, bpUserBob, bpSharedInstall, bpProfileToken, now); err != nil {
		t.Fatalf("Bob register: %v", err)
	}
	removed, err := repo.MarkUnsubscribed(ctx, bpTenant, bpUserBob, bpSharedInstall, now.Add(time.Minute))
	if err != nil || !removed {
		t.Fatalf("Bob unsubscribe: removed=%v err=%v", removed, err)
	}
	claimed, _, err := register(t, repo, ctx, bpUserAlice, bpSharedInstall, "fcm-web-alice-rotated", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Alice claiming a retired browser profile: %v", err)
	}
	if claimed.WorkforceMemberID != aliceMember || claimed.Status != browserpush.StatusActive {
		t.Fatalf("claimed row: %+v", claimed)
	}
	if claimed.StaleAt != nil || claimed.StaleReason != "" {
		t.Fatalf("a revived row must clear stale_at/stale_reason (the table's CHECK requires it): %+v", claimed)
	}
}

// TestOwnBrowserRefreshIsStillTheOrdinaryCase guards against over-correcting: the same member
// re-registering a rotated token on their own browser is the overwhelming majority of calls to
// this endpoint (admin-web re-reads the token on every dashboard load), and it must stay one row.
func TestOwnBrowserRefreshIsStillTheOrdinaryCase(t *testing.T) {
	repo, ctx := newBrowserPushRepo(t)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)

	first, created, err := register(t, repo, ctx, bpUserBob, bpSharedInstall, bpProfileToken, now)
	if err != nil || !created {
		t.Fatalf("first register: created=%v err=%v", created, err)
	}
	second, created, err := register(t, repo, ctx, bpUserBob, bpSharedInstall, "fcm-web-bob-rotated", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("refresh with a rotated token: %v", err)
	}
	if created {
		t.Fatalf("a refresh must not report itself as a new subscription")
	}
	if second.BrowserRegistrationID != first.BrowserRegistrationID {
		t.Fatalf("refresh created a second row: %s != %s", second.BrowserRegistrationID, first.BrowserRegistrationID)
	}
	// created_at is NOT touched on an own-browser refresh: it is the same registration.
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("own-browser refresh moved created_at from %v to %v", first.CreatedAt, second.CreatedAt)
	}
	if second.RowVersion != first.RowVersion+1 {
		t.Fatalf("row_version = %d after refresh, want %d", second.RowVersion, first.RowVersion+1)
	}
	recipients, err := repo.ResolveMemberRecipients(ctx, bpTenant, bpUserBob)
	if err != nil {
		t.Fatalf("resolve Bob: %v", err)
	}
	if len(recipients) != 1 || recipients[0].Token != "fcm-web-bob-rotated" {
		t.Fatalf("the fan-out did not pick up the rotated token: %+v", recipients)
	}
}

// TestNonMemberStillGetsTheAuthorizationAnswer: the conflict sentinel must not swallow the
// pre-existing authorization gap. A caller who is authenticated but is not an active workforce
// member of this tenant resolves to nothing, writes nothing, and is still told exactly that.
func TestNonMemberStillGetsTheAuthorizationAnswer(t *testing.T) {
	repo, ctx := newBrowserPushRepo(t)
	now := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)

	_, _, err := register(t, repo, ctx, bpUserOutsider, "web-outsider", "fcm-web-outsider", now)
	if !errors.Is(err, browserpush.ErrRegistrationNotFound) {
		t.Fatalf("non-member register: err=%v, want ErrRegistrationNotFound", err)
	}
	if errors.Is(err, browserpush.ErrBrowserInstallConflict) {
		t.Fatalf("an authorization gap must not be reported as a browser conflict: %v", err)
	}
}
