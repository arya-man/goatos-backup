package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

const (
	alertsDirectorUser = "00000000-0000-4000-8000-000000000401"
	alertsOtherPark    = "00000000-0000-4000-8000-000000003002"
)

// seedAlertsMember creates one active workforce member for a user id and returns
// the canonical workforce_member_id -- the identity the notification consumers
// stamp on every row, and the identity listAlertsSQL resolves the caller to.
func seedAlertsMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, code, name, roleHint string) string {
	t.Helper()
	var memberID string
	if err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', $5)
ON CONFLICT (tenant_id, display_code) DO UPDATE SET status='active'
RETURNING workforce_member_id::text`, repoTenant, userID, code, name, roleHint).Scan(&memberID); err != nil {
		t.Fatalf("seed workforce member %s: %v", code, err)
	}
	return memberID
}

// seedAlertNotification writes one notification_requests row exactly the way
// QueueRoleNotifications does -- one row per recipient DEVICE, with the audience
// already resolved into context.member_id.
func seedAlertNotification(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	memberID, role, messageKey, kind, parkID, title, body, deviceID, eventKey string,
	requestedAt time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel,
  recipient_ref, title, body, status, idempotency_key, request_fingerprint,
  context, requested_at
) VALUES (
  $1::uuid, 'weighing:test', 'weighing_campaign', 'advance_notice', 'push_fcm',
  $2, $3, $4, 'queued', $5, $5,
  jsonb_build_object(
    'member_id', $6::text,
    'role', $7::text,
    'message_key', $8::text,
    'type', $9::text,
    'park_id', $10::text,
    'target', '/weighing',
    'priority', 'normal',
    'event_key', $11::text
  ),
  $12::timestamptz
)`, repoTenant, deviceID, title, body,
		eventKey+":device:"+deviceID, memberID, role, messageKey, kind, parkID, eventKey, requestedAt); err != nil {
		t.Fatalf("seed alert notification %s: %v", eventKey, err)
	}
}

// TestListAlertsScopesToTheCallersOwnRoutedFeed is the audience proof, run
// against the REAL SQL rather than a fake.
//
// It seeds the maintainer's four named transitions as they are actually
// produced -- assignment down to the operator, submission up to the director,
// rework back down to the operator, close up to the director -- plus a SECOND
// operator's assignment and a VACCINATION row for the same person, then asserts
// each caller reads exactly their own weighing feed.
func TestListAlertsScopesToTheCallersOwnRoutedFeed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	pramod := seedAlertsMember(t, ctx, pool, repoOperator, "OP-PRAMOD", "Pramod", "operator")
	other := seedAlertsMember(t, ctx, pool, repoOtherOp, "OP-OTHER", "Other Operator", "operator")
	director := seedAlertsMember(t, ctx, pool, alertsDirectorUser, "GD-1", "Growth Director", "growth_director")

	now := time.Now().UTC()
	// DOWNSTREAM to Pramod: work assigned.
	seedAlertNotification(t, ctx, pool, pramod, "operator",
		"weighing.assigned_operator", "weighing_campaign_published", repoPark,
		"New weighing work", "Weighing work is assigned to you: Shed 1.",
		"device-pramod", "published:c1:pramod", now.Add(-4*time.Hour))
	// DOWNSTREAM to Pramod: proof bounced back.
	seedAlertNotification(t, ctx, pool, pramod, "operator",
		"weighing.verdict.rework", "weighing_rework", repoPark,
		"Weighing proof needs redo", "Shed 1 weighing proof was sent back.",
		"device-pramod", "rework:o1", now.Add(-1*time.Hour))
	// UPSTREAM to the director: shed submitted for verification.
	seedAlertNotification(t, ctx, pool, director, "growth_director",
		"weighing.shed_submitted", "weighing_shed_submitted", repoPark,
		"Weighing shed submitted", "Shed 1 weighing was submitted.",
		"device-gd", "submitted:s1", now.Add(-3*time.Hour))
	// UPSTREAM to the director: work closed.
	seedAlertNotification(t, ctx, pool, director, "growth_director",
		"weighing.campaign_closed", "weighing_campaign_closed", repoPark,
		"Weighing task closed", "A weighing task was closed.",
		"device-gd", "closed:c1", now.Add(-2*time.Hour))
	// The OTHER operator's assignment. Pramod must never see this.
	seedAlertNotification(t, ctx, pool, other, "operator",
		"weighing.assigned_operator", "weighing_campaign_published", repoPark,
		"New weighing work", "Weighing work is assigned to you: Shed 9.",
		"device-other", "published:c1:other", now.Add(-5*time.Hour))
	// A VACCINATION alert addressed to Pramod. The weighing feed must not carry
	// it: weighing is fully isolated from vaccination.
	seedAlertNotification(t, ctx, pool, pramod, "operator",
		"vaccination.proof.rework", "verification_rework", repoPark,
		"Vaccination proof needs redo", "A vaccination proof was sent back.",
		"device-pramod", "vax-rework:o9", now.Add(-30*time.Minute))
	// Pramod's SECOND phone, same transition. One alert, not two.
	seedAlertNotification(t, ctx, pool, pramod, "operator",
		"weighing.verdict.rework", "weighing_rework", repoPark,
		"Weighing proof needs redo", "Shed 1 weighing proof was sent back.",
		"device-pramod-2", "rework:o1", now.Add(-1*time.Hour))

	repo := NewRepository(pool, 5*time.Second)

	t.Run("operator sees only their own weighing alerts", func(t *testing.T) {
		page, err := repo.ListAlerts(ctx, repoTenant, repoOperator, false, []string{repoPark}, "", domain.AlertPageSize)
		if err != nil {
			t.Fatalf("ListAlerts: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("operator alerts = %d %v, want exactly 2 (assignment + rework); "+
				"a third means another operator's row, a vaccination row, or a duplicate device row leaked",
				len(page.Items), alertKinds(page.Items))
		}
		for _, item := range page.Items {
			if item.Body == "Weighing work is assigned to you: Shed 9." {
				t.Fatal("operator can read ANOTHER operator's assignment alert")
			}
			if item.Kind == "verification_rework" {
				t.Fatal("a vaccination alert leaked into the weighing feed")
			}
			if item.Direction != domain.AlertDirectionDownstream {
				t.Fatalf("operator row %q direction = %q, want downstream", item.Kind, item.Direction)
			}
			if item.Target == "" {
				t.Fatalf("row %q has no tap target", item.Kind)
			}
		}
		// Newest first: the rework (1h ago) precedes the assignment (4h ago).
		if page.Items[0].Kind != "weighing_rework" {
			t.Fatalf("first row = %q, want the newest transition (weighing_rework)", page.Items[0].Kind)
		}
	})

	t.Run("director sees the upstream submissions and closes", func(t *testing.T) {
		page, err := repo.ListAlerts(ctx, repoTenant, alertsDirectorUser, true, nil, "", domain.AlertPageSize)
		if err != nil {
			t.Fatalf("ListAlerts: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("director alerts = %d %v, want 2 (submitted + closed)", len(page.Items), alertKinds(page.Items))
		}
		for _, item := range page.Items {
			if item.Direction != domain.AlertDirectionUpstream {
				t.Fatalf("director row %q direction = %q, want upstream", item.Kind, item.Direction)
			}
		}
		if page.Items[0].Kind != "weighing_campaign_closed" {
			t.Fatalf("first director row = %q, want newest (weighing_campaign_closed)", page.Items[0].Kind)
		}
	})

	t.Run("park-scoped caller is narrowed to their own park", func(t *testing.T) {
		page, err := repo.ListAlerts(ctx, repoTenant, repoOperator, false, []string{alertsOtherPark}, "", domain.AlertPageSize)
		if err != nil {
			t.Fatalf("ListAlerts: %v", err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("caller scoped to a different park read %d rows %v, want 0",
				len(page.Items), alertKinds(page.Items))
		}
	})

	t.Run("a caller with no routed alerts gets an honest empty page", func(t *testing.T) {
		page, err := repo.ListAlerts(ctx, repoTenant, repoOtherOp, false, []string{repoPark}, "", domain.AlertPageSize)
		if err != nil {
			t.Fatalf("ListAlerts: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("other operator alerts = %d, want their own single assignment", len(page.Items))
		}
		if page.Title == "" || page.EmptyMessage == "" {
			t.Fatal("backend-owned title/empty copy missing from the page")
		}
	})

	t.Run("keyset paging walks the feed without repeating a row", func(t *testing.T) {
		first, err := repo.ListAlerts(ctx, repoTenant, repoOperator, false, []string{repoPark}, "", 1)
		if err != nil {
			t.Fatalf("ListAlerts page 1: %v", err)
		}
		if len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatalf("page 1 = %d rows, cursor %q; want 1 row and a cursor", len(first.Items), first.NextCursor)
		}
		second, err := repo.ListAlerts(ctx, repoTenant, repoOperator, false, []string{repoPark}, first.NextCursor, 1)
		if err != nil {
			t.Fatalf("ListAlerts page 2: %v", err)
		}
		if len(second.Items) != 1 {
			t.Fatalf("page 2 = %d rows, want 1", len(second.Items))
		}
		if second.Items[0].AlertID == first.Items[0].AlertID {
			t.Fatal("page 2 repeated page 1's row; the keyset is not advancing")
		}
		if second.NextCursor != "" {
			t.Fatalf("page 2 cursor = %q, want empty (feed exhausted)", second.NextCursor)
		}
	})
}

func alertKinds(items []domain.Alert) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Kind)
	}
	return out
}
