package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestWeighingCalendarMigrationPreservesAuthoredAndHistoricalSettings(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_, sql := onlyMigrationWithSuffix(t, "weighing_calendar_earliest_date")
	up := strings.Split(sql, "-- +goose Down")[0]
	old := `{"default_from_mode":"fixed_date","default_from_date":"2026-08-03","earliest_date":"2026-08-01"}`
	cases := []struct{ name, status, block, want string }{
		{"seeded", "published", old, "2026-07-05"},
		{"custom", "published", `{"default_from_mode":"fixed_date","default_from_date":"2026-08-04","earliest_date":"2026-08-01"}`, "2026-08-01"},
		{"rolling", "published", `{"default_from_mode":"rolling_days","default_from_days":42,"earliest_date":"2026-08-01"}`, "2026-08-01"},
		{"draft", "draft", old, "2026-08-01"},
		{"historical", "retired", old, "2026-08-01"},
		{"missing", "published", "", "2026-07-05"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := fmt.Sprintf("7f100000-0000-4000-8000-%012d", i+1)
			if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id,name,status) VALUES ($1::uuid,'Calendar test','active')`, tenant); err != nil {
				t.Fatal(err)
			}
			var sop string
			if err := pool.QueryRow(ctx, `INSERT INTO sop_definitions (tenant_id,code,name,status) VALUES ($1::uuid,'weighing.session','Calendar test','active') RETURNING sop_id::text`, tenant).Scan(&sop); err != nil {
				t.Fatal(err)
			}
			weighing := map[string]any{"planning": map[string]any{"default_cap_per_day": 77}}
			if tc.block != "" {
				var block any
				if err := json.Unmarshal([]byte(tc.block), &block); err != nil {
					t.Fatal(err)
				}
				weighing["weights_pages"] = block
			}
			doc, _ := json.Marshal(map[string]any{"weighing": weighing})
			if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id,sop_id,version,version_label,status,form_dsl,proof_policy,compatibility,validation_report) VALUES ($1::uuid,$2::uuid,7,'v7',$3,$4::jsonb,'{}','{}','{}')`, tenant, sop, tc.status, doc); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := pool.Exec(ctx, up); err != nil {
					t.Fatal(err)
				}
			}
			var earliest, cap string
			var version int
			if err := pool.QueryRow(ctx, `SELECT form_dsl #>> '{weighing,weights_pages,earliest_date}', form_dsl #>> '{weighing,planning,default_cap_per_day}', version FROM sop_versions WHERE tenant_id=$1::uuid AND sop_id=$2::uuid`, tenant, sop).Scan(&earliest, &cap, &version); err != nil {
				t.Fatal(err)
			}
			if earliest != tc.want || cap != "77" || version != 7 {
				t.Fatalf("earliest=%s cap=%s version=%d", earliest, cap, version)
			}
		})
	}
}
