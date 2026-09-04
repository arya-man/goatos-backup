// qa-fasting-sweep is a THROWAWAY QA runner (not wired into any build or
// deploy): it fires one weighing kernel sweep and one pc_care roll-forward
// sweep against the database in DATABASE_URL, at the instant in QA_AS_OF
// (RFC3339, default now), and prints what moved. Used by the fasting
// precondition phone rehearsal to force the midnight gate without waiting for
// midnight. Delete freely.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func main() {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL required")
		os.Exit(1)
	}
	asOf := time.Now()
	if raw := os.Getenv("QA_AS_OF"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad QA_AS_OF:", err)
			os.Exit(1)
		}
		asOf = parsed
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()

	var tenantID string
	if err := pool.QueryRow(ctx, `SELECT tenant_id::text FROM tenants LIMIT 1`).Scan(&tenantID); err != nil {
		fmt.Fprintln(os.Stderr, "resolve tenant:", err)
		os.Exit(1)
	}

	wrepo := weighingpg.NewRepository(pool, 30*time.Second)
	wres, err := wrepo.SweepWorkItems(ctx, weighingdomain.KernelSweepParams{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		fmt.Fprintln(os.Stderr, "weighing sweep:", err)
		os.Exit(1)
	}
	raw, _ := json.Marshal(wres)
	fmt.Println("weighing:", string(raw))

	prepo := pccarepg.NewRepository(pool, 30*time.Second)
	rolled, held, err := sweepPCCare(ctx, prepo, tenantID, asOf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pccare sweep:", err)
		os.Exit(1)
	}
	fmt.Printf("pccare: rolled=%d held_for_removal=%d asOf=%s\n", rolled, held, asOf.Format(time.RFC3339))
}

func sweepPCCare(ctx context.Context, repo *pccarepg.Repository, tenantID string, asOf time.Time) (int, int, error) {
	res, err := repo.SweepTaskRollForward(ctx, tenantID, asOf, 200, 50)
	if err != nil {
		return 0, 0, err
	}
	return res.RolledForward, res.HeldForRemoval, nil
}
