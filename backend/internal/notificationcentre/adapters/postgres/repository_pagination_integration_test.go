package postgres

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Compare the optimized page walk with the original DISTINCT ON specification.
// Deliberately interleave device deliveries across page boundaries; producer-based
// coverage lives in repository_integration_test.go, while this fixture exercises
// historic rows with empty/missing keys and exact timestamp ties.
func TestNotificationCentrePagesMatchCanonicalDedupe(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedExternalFacts(t, ctx, pool)
	var memberA, memberB string
	if err := pool.QueryRow(ctx, `SELECT workforce_member_id::text FROM workforce_members WHERE tenant_id=$1 AND user_id=$2`, ncTenant, ncUserA).Scan(&memberA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT workforce_member_id::text FROM workforce_members WHERE tenant_id=$1 AND user_id=$2`, ncTenant, ncUserB).Scan(&memberB); err != nil {
		t.Fatal(err)
	}
	otherTenant := "00000000-0000-4000-8000-00000000b002"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id,name,status) VALUES ($1,'Other','active')`, otherTenant); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 36; i++ {
		id := fmt.Sprintf("00000000-0000-4000-9000-%012d", i+1)
		// Repeated keys are deliberately separated by twelve delivery rows.
		key := fmt.Sprintf("event.%d", i%12)
		tenant, member := ncTenant, memberA
		if i >= 30 {
			member = memberB
		}
		if i >= 33 {
			tenant, member = otherTenant, memberA
		}
		stamp := time.Date(2026, 9, 1, 0, 0, i/2, 0, time.UTC)
		if i == 24 {
			// Same key and timestamp as row 12: UUID must break the winner tie.
			stamp = time.Date(2026, 9, 1, 0, 0, 6, 0, time.UTC)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO notification_requests
   (notification_request_id,tenant_id,calendar_event_id,target_type,notification_type,channel,title,idempotency_key,request_fingerprint,requested_at,context)
   VALUES ($1::text::uuid,$2,'pagination-fixture','tenant','reminder','local-stub','Fixture',$1,$1,$3,
    CASE WHEN $6 THEN jsonb_build_object('member_id',$4::text)
    ELSE jsonb_build_object('member_id',$4::text,'event_key',$5::text) END)`, id, tenant, stamp, member, func() string {
			if i == 2 || i == 3 {
				return ""
			}
			return key
		}(), i == 4 || i == 5); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := pool.Query(ctx, `SELECT notification_request_id::text FROM (
  SELECT DISTINCT ON (COALESCE(NULLIF(context->>'event_key',''),notification_request_id::text)) notification_request_id,requested_at
  FROM notification_requests WHERE tenant_id=$1 AND context->>'member_id'=$2
  ORDER BY COALESCE(NULLIF(context->>'event_key',''),notification_request_id::text),requested_at DESC,notification_request_id DESC
 ) canonical ORDER BY requested_at DESC,notification_request_id DESC`, ncTenant, memberA)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	repo := NewRepository(pool, 10*time.Second)
	for _, limit := range []int{1, 2, 5, 20} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			var got []string
			cursor := ""
			for pageNo := 0; pageNo <= len(want); pageNo++ {
				page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: limit, Cursor: cursor})
				if err != nil {
					t.Fatal(err)
				}
				if page.UnreadCount != len(want) {
					t.Fatalf("whole-feed unread=%d want %d", page.UnreadCount, len(want))
				}
				for _, item := range page.Items {
					got = append(got, item.NotificationRequestID)
				}
				cursor = page.NextCursor
				if cursor == "" {
					break
				}
			}
			if cursor != "" {
				t.Fatal("pagination did not terminate")
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("page walk differs from canonical dedupe:\ngot %v\nwant %v", got, want)
			}
		})
	}
}
