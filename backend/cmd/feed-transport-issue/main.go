package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:], time.Now); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, now func() time.Time) error {
	fs := flag.NewFlagSet("feed-transport-issue", flag.ContinueOnError)
	tenant := fs.String("tenant-id", os.Getenv("GOATOS_TENANT_ID"), "tenant id")
	asOfRaw := fs.String("as-of", "", "RFC3339 instant")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenant) == "" {
		return fmt.Errorf("tenant-id is required")
	}
	asOf := now()
	if *asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, *asOfRaw)
		if err != nil {
			return fmt.Errorf("as-of must be RFC3339: %w", err)
		}
		asOf = parsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := feeddirectionpg.NewRepository(pool, cfg.QueryTimeout)
	result, err := repo.MaterializeTransportTasks(ctx, feeddirectionports.MaterializeTransportParams{TenantID: strings.TrimSpace(*tenant), AsOf: asOf.In(biztime.DefaultLocation())})
	if err != nil {
		return err
	}
	fmt.Printf("feed-transport-issue business_date=%s inserted=%d\n", result.BusinessDate, result.Inserted)
	return nil
}
