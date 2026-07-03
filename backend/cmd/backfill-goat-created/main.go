package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	eventType     = "goat.created"
	schemaVersion = "1.0.0"
	schemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	topic         = "identity.events"
)

type goatRow struct {
	TenantID  string
	GoatID    string
	FarmID    *string
	ParkID    *string
	ShedID    *string
	EntryDate *time.Time
	CreatedAt time.Time
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("backfill-goat-created", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", os.Getenv("GOATOS_TENANT_ID"), "tenant id")
	goatID := fs.String("goat-id", "", "optional goat id to backfill exactly one existing goat")
	limit := fs.Int("limit", 500, "maximum goats to backfill")
	dryRun := fs.Bool("dry-run", false, "list candidate count without writing")
	timeout := fs.Duration("timeout", 60*time.Second, "backfill timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenantID == "" {
		return fmt.Errorf("tenant-id is required")
	}
	if *limit < 1 || *limit > 5000 {
		return fmt.Errorf("limit must be between 1 and 5000")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
SELECT tenant_id::text, goat_id::text, farm_id::text, park_id::text, shed_id::text, entry_date, created_at
FROM goats g
WHERE tenant_id = $1::uuid
  AND merged_into_goat_id IS NULL
  AND lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND (NULLIF($3::text, '') IS NULL OR goat_id = NULLIF($3::text, '')::uuid)
  AND NOT EXISTS (
    SELECT 1
    FROM goat_identity_events gie
    WHERE gie.tenant_id = g.tenant_id
      AND gie.goat_id = g.goat_id
      AND gie.event_type = 'goat.created'
  )
ORDER BY created_at ASC, goat_id ASC
LIMIT $2`, *tenantID, *limit, strings.TrimSpace(*goatID))
	if err != nil {
		return err
	}
	defer rows.Close()
	candidates := []goatRow{}
	for rows.Next() {
		var row goatRow
		if err := rows.Scan(&row.TenantID, &row.GoatID, &row.FarmID, &row.ParkID, &row.ShedID, &row.EntryDate, &row.CreatedAt); err != nil {
			return err
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if *dryRun {
		fmt.Printf("goat.created backfill candidates=%d\n", len(candidates))
		return nil
	}
	applied := 0
	for _, goat := range candidates {
		ok, err := backfillOne(ctx, pool, goat)
		if err != nil {
			return err
		}
		if ok {
			applied++
		}
	}
	fmt.Printf("goat.created backfill applied=%d candidates=%d\n", applied, len(candidates))
	return nil
}

type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

func backfillOne(ctx context.Context, db txBeginner, goat goatRow) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	var exists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM goat_identity_events
  WHERE tenant_id = $1::uuid
    AND goat_id = $2::uuid
    AND event_type = 'goat.created'
)`, goat.TenantID, goat.GoatID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	eventID, err := newUUID(ctx, tx)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	idempotencyKey := "backfill:goat.created:" + goat.GoatID
	entryDate := ""
	if goat.EntryDate != nil {
		entryDate = goat.EntryDate.UTC().Format("2006-01-02")
	}
	eventPayload, err := json.Marshal(map[string]any{
		"goat_id":           goat.GoatID,
		"origin_type":       "backfill",
		"entry_date":        entryDate,
		"farm_id":           goat.FarmID,
		"park_id":           goat.ParkID,
		"shed_id":           goat.ShedID,
		"generation_status": "queued",
		"backfill":          true,
	})
	if err != nil {
		return false, err
	}
	var recordedAt time.Time
	if err := tx.QueryRow(ctx, `
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, source_system, source_record_id, payload, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'goat.created', 1,
  $4::timestamptz, $5::timestamptz, 'goat_created_backfill', $6, $7::jsonb, $8
)
RETURNING recorded_at`, eventID, goat.TenantID, goat.GoatID, goat.CreatedAt, now, idempotencyKey, eventPayload, idempotencyKey).Scan(&recordedAt); err != nil {
		return false, err
	}
	envelope, err := envelope(goat, eventID, goat.CreatedAt, recordedAt, eventPayload, idempotencyKey)
	if err != nil {
		return false, err
	}
	headers, _ := json.Marshal(map[string]any{"backfill": true})
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status
) VALUES (
  $1::uuid, $2::uuid, 'goat.created', $3, 'goat', $4::uuid,
  $5, $6::jsonb, $7::jsonb, $8, $9, 'pending'
)`, goat.TenantID, eventID, schemaVersion, goat.GoatID, topic, envelope, headers, idempotencyKey, "backfill-goat-created"); err != nil {
		return false, err
	}
	afterState, _ := json.Marshal(map[string]any{"goat_id": goat.GoatID, "event_id": eventID, "generation_status": "queued"})
	metadata, _ := json.Marshal(map[string]any{"idempotency_key": idempotencyKey, "backfill": true})
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_type, action, resource_type, resource_id, scope_type, scope_id,
  after_state, metadata, trace_id
) VALUES (
  $1::uuid, 'system_rule', 'goat.created', 'goat', $2::uuid, $3, nullif($4::text, '')::uuid,
  $5::jsonb, $6::jsonb, 'backfill-goat-created'
)`, goat.TenantID, goat.GoatID, scopeType(goat), scopeID(goat), afterState, metadata); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

func envelope(goat goatRow, eventID string, occurredAt, recordedAt time.Time, eventPayload []byte, idempotencyKey string) ([]byte, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(eventPayload, &payload); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": schemaVersion,
		"schema_ref":     schemaRef,
		"aggregate_type": "goat",
		"aggregate_id":   goat.GoatID,
		"occurred_at":    occurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    recordedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"producer": map[string]any{
			"service": "goatos-cli",
			"module":  "identity",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_id":   nil,
			"actor_ref":  "backfill-goat-created",
		},
		"subject_type": "goat",
		"subject_id":   goat.GoatID,
		"visibility_scope": map[string]any{
			"tenant_id": goat.TenantID,
			"farm_id":   goat.FarmID,
			"park_id":   goat.ParkID,
			"shed_id":   goat.ShedID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "goat",
			"evidence_id":   goat.GoatID,
		}},
		"payload":  payload,
		"trace_id": "backfill-goat-created",
	})
}

func scopeType(goat goatRow) string {
	switch {
	case goat.ShedID != nil:
		return "shed"
	case goat.ParkID != nil:
		return "park"
	case goat.FarmID != nil:
		return "farm"
	default:
		return "tenant"
	}
}

func scopeID(goat goatRow) string {
	switch {
	case goat.ShedID != nil:
		return *goat.ShedID
	case goat.ParkID != nil:
		return *goat.ParkID
	case goat.FarmID != nil:
		return *goat.FarmID
	default:
		return ""
	}
}

func newUUID(ctx context.Context, tx pgx.Tx) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id)
	return id, err
}
