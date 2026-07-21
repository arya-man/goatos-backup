package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

func TestStgCalendarMonthFilterOptionsRepro(t *testing.T) {
	raw := os.Getenv("GOATOS_STG_DATABASE_URL")
	if raw == "" {
		t.Skip("GOATOS_STG_DATABASE_URL not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if socketDir := os.Getenv("GOATOS_STG_CLOUDSQL_SOCKET"); socketDir != "" {
		config.ConnConfig.Host = socketDir
		config.ConnConfig.Port = 5432
		config.ConnConfig.TLSConfig = nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewRepository(pool, 20*time.Second)
	loc, _ := time.LoadLocation("Asia/Kolkata")
	base := domain.Query{
		TenantID:             "00000000-0000-4000-8000-000000000001",
		DateFrom:             time.Date(2026, 7, 1, 0, 0, 0, 0, loc),
		DateTo:               time.Date(2026, 7, 31, 0, 0, 0, 0, loc),
		Limit:                20,
		Scope:                domain.ScopeFilter{TenantWide: true},
	}
	for name, mutate := range map[string]func(*domain.Query){
		"list_only": func(q *domain.Query) {},
		"with_filters": func(q *domain.Query) {
			q.IncludeFilterOptions = true
		},
		"markers_only": func(q *domain.Query) {
			q.MarkersOnly = true
			q.IncludeDateMarkers = true
		},
	} {
		q := base
		mutate(&q)
		start := time.Now()
		resp, err := repo.ListEvents(ctx, q)
		t.Logf("%s duration=%s items=%d filter_options=%t markers=%d", name, time.Since(start), len(resp.Items), resp.FilterOptions != nil, len(resp.DateMarkers))
		if err != nil {
			t.Fatalf("%s ListEvents: %v", name, err)
		}
		if len(strings.TrimSpace(resp.Source)) == 0 {
			t.Fatalf("%s response missing source: %#v", name, resp)
		}
	}
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) "+calendarCanonicalListSQL,
		"00000000-0000-4000-8000-000000000001",
		time.Date(2026, 7, 1, 0, 0, 0, 0, loc),
		time.Date(2026, 8, 1, 0, 0, 0, 0, loc),
		"", "", "", "", nil, "", 21, true, []string{}, []string{}, "")
	if err != nil {
		t.Fatalf("explain canonical: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("plan:\n%s", strings.Join(plan, "\n"))
}
