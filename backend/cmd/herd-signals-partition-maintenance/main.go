// Command herd-signals-partition-maintenance runs the two partition-maintenance functions
// defined in backend/migrations/postgres/000201_herd_signal_packets_partition_maintenance.sql
// against herd_signal_packets:
//
//  1. herd_signal_packets_ensure_future_partitions(days-ahead) -- creates any missing daily
//     partition from today through today+days-ahead, so ingest never hits a missing-partition
//     error.
//  2. herd_signal_packets_prune_expired_partitions(retention-days) -- drops whole daily
//     partitions entirely older than the retention cutoff via DROP TABLE.
//
// Intended to run once daily (recommended: a low-traffic hour, well ahead of the runway
// migration 000200 pre-created). This binary is deliberately thin: all the actual logic,
// including the hardcoded-to-herd_signal_packets safety guard, lives in the SQL functions so a
// direct `psql -c "select ..."` invocation from any scheduler has the exact same safety
// properties as this binary. This command exists for structured logging and for environments
// that already run Go cron jobs via this cmd/ pattern rather than raw SQL in a crontab.
//
// Scheduling this command (e.g. a Cloud Run job / cron entry) is an infra step outside the scope
// of this migration; see docs/modules/herd-signals-system-design.md Section 3 for the retention
// design this command implements.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	var (
		databaseURL   = flag.String("database-url", os.Getenv("DATABASE_URL"), "target database")
		daysAhead     = flag.Int("days-ahead", 14, "ensure daily partitions exist through today+N days")
		retentionDays = flag.Int("retention-days", 14, "drop daily partitions entirely older than N days")
		dryRun        = flag.Bool("dry-run", false, "only run herd_signal_packets_ensure_future_partitions; skip pruning")
		timeout       = flag.Duration("timeout", 2*time.Minute, "overall timeout")
	)
	flag.Parse()

	if *databaseURL == "" {
		fmt.Fprintln(os.Stderr, "database-url (or DATABASE_URL) is required")
		os.Exit(2)
	}
	if *retentionDays < 1 {
		fmt.Fprintln(os.Stderr, "retention-days must be >= 1")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	created := 0
	rows, err := pool.Query(ctx,
		`SELECT partition_name, created FROM public.herd_signal_packets_ensure_future_partitions($1)`,
		*daysAhead,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ensure future partitions: %v\n", err)
		os.Exit(1)
	}
	for rows.Next() {
		var name string
		var wasCreated bool
		if err := rows.Scan(&name, &wasCreated); err != nil {
			rows.Close()
			fmt.Fprintf(os.Stderr, "scan ensure-partitions row: %v\n", err)
			os.Exit(1)
		}
		if wasCreated {
			created++
			fmt.Printf("created partition %s\n", name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "ensure future partitions: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ensure-future-partitions: %d created (days-ahead=%d)\n", created, *daysAhead)

	if *dryRun {
		fmt.Println("dry-run: skipping herd_signal_packets_prune_expired_partitions")
		return
	}

	dropped := 0
	prows, err := pool.Query(ctx,
		`SELECT partition_name, range_start, range_end FROM public.herd_signal_packets_prune_expired_partitions($1)`,
		*retentionDays,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prune expired partitions: %v\n", err)
		os.Exit(1)
	}
	for prows.Next() {
		var name string
		var start, end time.Time
		if err := prows.Scan(&name, &start, &end); err != nil {
			prows.Close()
			fmt.Fprintf(os.Stderr, "scan prune row: %v\n", err)
			os.Exit(1)
		}
		dropped++
		fmt.Printf("dropped partition %s [%s, %s)\n", name, start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "prune expired partitions: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("prune-expired-partitions: %d dropped (retention-days=%d)\n", dropped, *retentionDays)
}
