package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Opt-in, disposable database only. Run with GOATOS_RUN_POSTGRES_TESTS=1,
// GOATOS_NOTIFICATION_SCALE_TESTS=1 and the pgtest Docker/admin-DSN harness.
// Baseline is the production query before PR376, including the same unread count.
const sqlListNotificationsBefore376 = sqlTargetMemberCTE + `,
mine AS (
  SELECT DISTINCT ON (` + dedupeKeyExpr + `)
    nr.notification_request_id,
    nr.notification_type,
    nr.title,
    nr.body,
    nr.status,
    nr.requested_at,
    nr.read_at,
    nr.requested_by,
    nr.context
  FROM notification_requests nr, target_member tm
  WHERE nr.tenant_id = $1::uuid
    AND tm.workforce_member_id IS NOT NULL
    AND nr.context->>'member_id' = tm.workforce_member_id::text
  ORDER BY ` + dedupeKeyExpr + `, nr.requested_at DESC, nr.notification_request_id DESC
)
SELECT
  mine.notification_request_id::text,
  mine.notification_type,
  mine.title,
  mine.body,
  mine.status,
  mine.requested_at,
  mine.read_at,
  COALESCE(NULLIF(mine.context->>'actor_name', ''), actor.display_name, '') AS actor_name,
  COALESCE(mine.context->>'task_id', ''),
  COALESCE(mine.context->>'task_no', ''),
  COALESCE(mine.context->>'screen', ''),
  COALESCE(mine.context->>'group_key', ''),
  COALESCE(mine.context->>'priority', ''),
  COALESCE(mine.context->>'message_key', ''),
  COALESCE(mine.context->>'target', ''),
  COALESCE(mine.context->>'status', '')
FROM mine
LEFT JOIN workforce_members actor
  ON actor.tenant_id = $1::uuid
 AND actor.user_id = mine.requested_by
 AND actor.status = 'active'
WHERE $3::timestamptz IS NULL
   OR (mine.requested_at, mine.notification_request_id) < ($3::timestamptz, $4::uuid)
ORDER BY mine.requested_at DESC, mine.notification_request_id DESC
LIMIT $5`

func TestNotificationFeedScale(t *testing.T) {
	if os.Getenv("GOATOS_NOTIFICATION_SCALE_TESTS") != "1" {
		t.Skip("opt-in 237k-row proof")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedExternalFacts(t, ctx, pool)
	_, err := pool.Exec(ctx, `INSERT INTO notification_requests (
 notification_request_id, tenant_id, calendar_event_id, target_type, notification_type,
 channel,title,body,status,requested_at,idempotency_key,request_fingerprint,context)
 SELECT md5('scale-row-'||i)::uuid,$1::uuid,'scale','tenant','reminder','push_fcm',
 'Scale notification',repeat('payload ',100),'sent',
 '2026-09-23T00:00:00Z'::timestamptz - i * interval '1 second',
 'scale-'||i,'scale',jsonb_build_object('member_id',wm.workforce_member_id::text,
 'event_key','event-'|| CASE WHEN i<=102291 THEN ((i-1)/3)*2 + CASE WHEN i%3=0 THEN 1 ELSE 0 END ELSE i END)
 FROM generate_series(1,236999) i
 JOIN workforce_members wm ON wm.tenant_id=$1::uuid AND wm.user_id=CASE WHEN i<=102291 THEN $2::uuid ELSE $3::uuid END`, ncTenant, ncUserA, ncUserB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "ANALYZE notification_requests"); err != nil {
		t.Fatal(err)
	}
	var total, member, distinct int
	err = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE context->>'member_id'=wm.workforce_member_id::text),count(DISTINCT context->>'event_key') FILTER (WHERE context->>'member_id'=wm.workforce_member_id::text) FROM notification_requests nr CROSS JOIN workforce_members wm WHERE wm.tenant_id=$1::uuid AND wm.user_id=$2::uuid`, ncTenant, ncUserA).Scan(&total, &member, &distinct)
	if err != nil {
		t.Fatal(err)
	}
	if total != 236999 || member != 102291 || distinct != 68194 {
		t.Fatalf("wrong scale: %d/%d/%d", total, member, distinct)
	}
	t.Logf("fixture total=%d member=%d distinct=%d; all unread; body=800 bytes", total, member, distinct)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var cursorAt time.Time
	var cursorID string
	err = conn.QueryRow(ctx, sqlTargetMemberCTE+`, canonical AS (SELECT DISTINCT ON (`+dedupeKeyExpr+`) nr.requested_at,nr.notification_request_id FROM notification_requests nr,target_member tm WHERE nr.tenant_id=$1::uuid AND nr.context->>'member_id'=tm.workforce_member_id::text ORDER BY `+dedupeKeyExpr+`,nr.requested_at DESC,nr.notification_request_id DESC) SELECT requested_at,notification_request_id::text FROM canonical ORDER BY requested_at DESC,notification_request_id DESC OFFSET 60000 LIMIT 1`, ncTenant, ncUserA).Scan(&cursorAt, &cursorID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		if _, err = conn.Exec(ctx, "SET plan_cache_mode="+mode); err != nil {
			t.Fatal(err)
		}
		for _, depth := range []string{"first", "deep"} {
			var at, id any
			if depth == "deep" {
				at, id = cursorAt, cursorID
			}
			args := []any{ncTenant, ncUserA, at, id, 21}
			var baseline [][]any
			for _, q := range []struct{ name, sql string }{{"before", sqlListNotificationsBefore376}, {"after", sqlListNotifications}} {
				queryArgs := args
				if q.name == "after" && depth == "first" {
					q.sql = sqlListNotificationsFirst
					queryArgs = []any{ncTenant, ncUserA, 21}
				}
				var values [][]any
				var unread int
				var e error
				var samples []time.Duration
				for sample := 0; sample < 4; sample++ {
					started := time.Now()
					rows, e := conn.Query(ctx, q.sql, queryArgs...)
					if e != nil {
						t.Fatal(e)
					}
					values = nil
					for rows.Next() {
						v, e := rows.Values()
						if e != nil {
							t.Fatal(e)
						}
						values = append(values, v)
					}
					if e = rows.Err(); e != nil {
						t.Fatal(e)
					}
					rows.Close()

					if e = conn.QueryRow(ctx, sqlUnreadCount, ncTenant, ncUserA).Scan(&unread); e != nil {
						t.Fatal(e)
					}
					if sample > 0 {
						samples = append(samples, time.Since(started))
					}
				}
				sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
				elapsed := samples[len(samples)/2]

				if len(values) != 21 || unread != 68194 {
					t.Fatalf("%s payload rows=%d unread=%d", q.name, len(values), unread)
				}
				if q.name == "before" {
					baseline = values
				} else if !reflect.DeepEqual(values, baseline) {
					t.Fatal("page differs from canonical baseline")
				}
				payload, _ := json.Marshal(values)
				t.Logf("%s %s %s page+unread warm-median-3=%s rows=%d pageJSONbytes=%d unread=%d", mode, depth, q.name, elapsed, len(values), len(payload), unread)
				name := "scale_" + q.name + "_" + depth
				if _, e = conn.Conn().Prepare(ctx, name, q.sql); e != nil {
					t.Fatal(e)
				}
				cursorSQL := "NULL,NULL"
				if depth == "deep" {
					cursorSQL = fmt.Sprintf("'%s'::timestamptz,'%s'::uuid", cursorAt.Format(time.RFC3339Nano), cursorID)
				}
				explain := fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE %s('%s','%s',%s,21)", name, ncTenant, ncUserA, cursorSQL)
				if q.name == "after" && depth == "first" {
					explain = fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE %s('%s','%s',21)", name, ncTenant, ncUserA)
				}
				var plan []byte
				if e = conn.QueryRow(ctx, explain).Scan(&plan); e != nil {
					t.Fatal(e)
				}

				t.Logf("PLAN %s %s %s %s", mode, depth, q.name, plan)
				if q.name == "after" {
					assertBoundedNotificationScan(t, plan)
				}
			}
		}
	}
	repo := NewRepository(pool, 10*time.Second)
	started := time.Now()
	page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(page)
	t.Logf("Repository.ListNotifications wall=%s rows=%d JSONbytes=%d unread=%d", time.Since(started), len(page.Items), len(payload), page.UnreadCount)
	if len(page.Items) != 20 || page.UnreadCount != 68194 || page.NextCursor == "" {
		t.Fatal("bad full repository result")
	}
}

// Assert plan work rather than fragile wall-clock budgets on shared developer machines.
func assertBoundedNotificationScan(t *testing.T, raw []byte) {
	t.Helper()
	var plans []map[string]any
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		if node["Relation Name"] == "notification_requests" {
			alias, _ := node["Alias"].(string)
			seen[alias] = true
			if alias == "newer" && node["Index Name"] != "notification_requests_member_dedupe_idx" {
				t.Errorf("dedupe probe must use its index: %v", node["Node Type"])
			}
			rows, _ := node["Actual Rows"].(float64)
			removed, _ := node["Rows Removed by Filter"].(float64)
			loops, _ := node["Actual Loops"].(float64)
			if loops > 128 || (rows+removed)*loops > 128 {
				t.Errorf("page scan grows with history: rows=%v removed=%v loops=%v", rows, removed, loops)
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			walk(child.(map[string]any))
		}
	}
	walk(plans[0]["Plan"].(map[string]any))
	if !seen["nr"] || !seen["newer"] {
		t.Fatalf("missing candidate or dedupe scan: %v", seen)
	}
}
