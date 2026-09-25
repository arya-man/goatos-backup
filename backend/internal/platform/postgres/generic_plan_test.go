package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestGenericPlanReadTxIsReadOnlyAndLocal(t *testing.T) {
	if GenericPlanReadTx.AccessMode != pgx.ReadOnly {
		t.Fatalf("GenericPlanReadTx must be read-only, got %q", GenericPlanReadTx.AccessMode)
	}
	q := GenericPlanReadTx.BeginQuery
	if !strings.HasPrefix(q, "BEGIN READ ONLY;") || !strings.Contains(q, "SET LOCAL plan_cache_mode = force_generic_plan") {
		t.Fatalf("GenericPlanReadTx.BeginQuery must open a read-only tx and SET LOCAL the plan mode, got %q", q)
	}
}

// GOATOS_PLATFORM_PG_TEST_DATABASE_URL: any Postgres (read-only is enough for the first half).
func TestGenericPlanDoesNotLeakAndSurvivesResultTypeChange(t *testing.T) {
	dsn := os.Getenv("GOATOS_PLATFORM_PG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOATOS_PLATFORM_PG_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	var inside string
	if err := WithGenericPlanReadTx(ctx, conn, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT current_setting('plan_cache_mode') WHERE $1::int = 1", 1).Scan(&inside)
	}); err != nil {
		t.Fatal(err)
	}
	if inside != "force_generic_plan" {
		t.Fatalf("inside tx plan_cache_mode = %q", inside)
	}
	var after string
	if err := conn.QueryRow(ctx, "SELECT current_setting('plan_cache_mode')").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after == "force_generic_plan" {
		t.Fatal("plan_cache_mode leaked onto the connection")
	}

	// Result-type change after a cached generic plan: the first call after the DDL may fail with
	// "cached plan must not change result type"; pgx invalidates the statement and the next call works.
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE generic_plan_probe (id int, v int)"); err != nil {
		t.Skipf("DDL not allowed on this database (%v); skipping result-type check", err)
	}
	read := func() error {
		return WithGenericPlanReadTx(ctx, conn, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT * FROM generic_plan_probe WHERE id = $1", 1)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
			}
			return rows.Err()
		})
	}
	if err := read(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "ALTER TABLE generic_plan_probe ALTER COLUMN v TYPE text"); err != nil {
		t.Fatal(err)
	}
	_ = read() // may fail once with "cached plan must not change result type"
	if err := read(); err != nil {
		t.Fatalf("second call after result-type change must re-prepare and succeed: %v", err)
	}
}
