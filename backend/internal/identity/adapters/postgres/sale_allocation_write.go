package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// saleAllocationScope labels the confirm's idempotency row. It is its own scope rather
// than reusing the single-goat exit scope: the result of this command is a SALE, not one
// animal, and a replay must not be mistaken for a single-animal lifecycle mutation.
const saleAllocationScope = "sale_allocation"

// RecordSaleAllocations confirms a picked set onto a sale: it records which animals the
// sale is made of AND exits each one as sold, in ONE transaction.
//
// WHY BOTH HALVES SHARE A TRANSACTION. The allocation row and the animal's exit are one
// business fact stated twice. If the allocation committed and the exit did not, the sale
// would list animals the herd still calls alive; if the exit committed and the allocation
// did not, animals would be gone with no record of which sale took them. This is the
// atomic-transition rule in AGENTS.md, and there is no best-effort second write here.
//
// WHY THE EXIT GOES THROUGH ExitGoatInTx AND NOT A SET-BASED UPDATE. ExitGoatInTx is the
// CANONICAL per-goat exit -- the same transition the single-goat admin route uses -- so
// each animal gets its proper goat.exited outbox message (which cancels its open
// vaccination obligations), its decision record and its audit row. A hand-written bulk
// UPDATE would flip lifecycle_status and silently skip all three, leaving sold animals
// with live vaccination work scheduled against them.
//
// The per-goat loop is therefore deliberate, bounded by
// ports.MaxSaleAllocationGoatsPerCommand, and inside ONE transaction.
// scale-guard:ignore: bounded (<=100) canonical per-goat transition inside one transaction; a set-based UPDATE would skip the per-animal event, decision and audit that obligation cancellation depends on.
func (r *Repository) RecordSaleAllocations(ctx context.Context, cmd ports.RecordSaleAllocationsCommand) (*ports.SaleAllocationResult, error) {
	if len(cmd.Rows) == 0 {
		return nil, ports.ErrSaleAllocationEmpty
	}
	if len(cmd.Rows) > ports.MaxSaleAllocationGoatsPerCommand {
		return nil, ports.ErrSaleAllocationScopeTooLarge
	}
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	dealUUID, err := uuidParam(cmd.SalesDealID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: record sale allocations: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	if _, err := qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          saleAllocationScope,
		RequestHash:    cmd.RequestHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		// EXACT REPLAY. The key is already held, so the animals were already tagged and
		// exited by the first call. Read the sale back rather than re-running any of it:
		// re-applying would try to exit animals that are already sold and fail noisily on
		// a request the caller is entitled to retry.
		groups, readErr := r.listSaleAllocationsInTx(ctx, tx, cmd.TenantID, cmd.SalesDealID)
		if readErr != nil {
			return nil, readErr
		}
		return saleAllocationResult(cmd.SalesDealID, groups), nil
	} else if err != nil {
		return nil, fmt.Errorf("identity: record sale allocations: reserve idempotency: %w", err)
	}

	if err := r.lockAndCheckSaleAllocationCapacity(ctx, tx, cmd); err != nil {
		return nil, err
	}

	// The allocation SNAPSHOT is written BEFORE the exits, while the animals still carry
	// their location. Once ExitGoatInTx has run, reading park/shed/tag back off the goat
	// would describe an animal that has already left.
	if err := r.insertSaleAllocations(ctx, tx, cmd, tenantUUID, dealUUID); err != nil {
		return nil, err
	}

	for _, row := range cmd.Rows {
		// The per-animal idempotency key is derived from the SALE and the animal, so a
		// retry of this confirm resolves to the same key per animal rather than minting a
		// new one and attempting a second exit.
		exitKey := fmt.Sprintf("sale_allocation:%s:%s", cmd.SalesDealID, row.GoatID)
		requestHash, hashErr := saleExitRequestHash(cmd, row)
		if hashErr != nil {
			return nil, hashErr
		}
		if _, err := r.ExitGoatInTx(ctx, tx, ports.ExitGoatCommand{
			TenantID:             cmd.TenantID,
			ActorID:              cmd.ActorID,
			ClientIdempotencyKey: exitKey,
			StoredIdempotencyKey: fmt.Sprintf("%s:%s", cmd.TenantID, exitKey),
			IdempotencyScope:     saleAllocationScope,
			RequestHash:          requestHash,
			TraceID:              cmd.TraceID,
			GoatID:               row.GoatID,
			LifecycleStatus:      "sold",
			ExitReason:           "sold",
			Reason:               cmd.Reason,
			OccurredAt:           cmd.OccurredAt,
			// The sale itself is the evidence, so the animal's timeline points back at
			// the deal it left on rather than saying only "sold".
			EvidenceRefs: saleEvidence(cmd.SalesDealID),
			RowVersion:   row.RowVersion,
			// Not a death. GuardrailApproved stays false so the critical-death guardrail
			// still refuses a 'dead' exit if this command is ever widened by mistake.
			GuardrailApproved: false,
		}); err != nil {
			// A stale row_version means the animal changed after the picker listed it.
			// The whole confirm fails: see the fail-closed rule in the app layer.
			return nil, err
		}
	}

	groups, err := r.listSaleAllocationsInTx(ctx, tx, cmd.TenantID, cmd.SalesDealID)
	if err != nil {
		return nil, err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(saleAllocationScope),
		ResultID:       dealUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, fmt.Errorf("identity: record sale allocations: complete idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: record sale allocations: commit: %w", err)
	}
	return saleAllocationResult(cmd.SalesDealID, groups), nil
}

func (r *Repository) lockAndCheckSaleAllocationCapacity(ctx context.Context, tx pgx.Tx, cmd ports.RecordSaleAllocationsCommand) error {
	if cmd.DeclaredAnimalCount <= 0 {
		return ports.ErrSaleDealNoAnimalCount
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, cmd.TenantID+":"+cmd.SalesDealID); err != nil {
		return fmt.Errorf("identity: record sale allocations: lock sale: %w", err)
	}
	var alreadyTagged int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM goat_sale_allocations
WHERE tenant_id = $1::uuid AND sales_deal_id = $2::uuid AND status = 'tagged'`,
		cmd.TenantID, cmd.SalesDealID).Scan(&alreadyTagged); err != nil {
		return fmt.Errorf("identity: record sale allocations: count sale allocations: %w", err)
	}
	if alreadyTagged+len(cmd.Rows) != cmd.DeclaredAnimalCount {
		return ports.ErrSaleAllocationCountChanged
	}
	return nil
}

// insertSaleAllocations writes every allocation row in ONE set-based statement, snapshotting
// each animal's location and identifier as they stand right now.
func (r *Repository) insertSaleAllocations(ctx context.Context, tx pgx.Tx, cmd ports.RecordSaleAllocationsCommand, tenantUUID, dealUUID any) error {
	goatIDs := make([]string, 0, len(cmd.Rows))
	for _, row := range cmd.Rows {
		goatIDs = append(goatIDs, row.GoatID)
	}
	// scale-guard:ignore: bounded set-based transactional write over <=100 named ids, not a compute-on-read god-CTE.
	_, err := tx.Exec(ctx, `
INSERT INTO goat_sale_allocations (
    tenant_id, goat_id, sales_deal_id, park_id, shed_id, partition_label, tag_number,
    status, allocated_at, allocated_by, idempotency_key
)
SELECT g.tenant_id, g.goat_id, $2::uuid, g.park_id, g.shed_id,
       NULLIF(COALESCE(NULLIF(gsp.partition_label, 'whole'), ''), ''),
       tag.identifier_value,
       'tagged', $5::timestamptz, NULLIF($4, '')::uuid,
       'sale_allocation:' || $2::text || ':' || g.goat_id::text
FROM goats g
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
    AND gi.status = 'active' AND gi.valid_to IS NULL
  -- Same priority as the picker's read, by the REAL type vocabulary the CHECK constraint
  -- admits, so the tag SNAPSHOTTED onto the allocation is the one the operator was shown.
  ORDER BY CASE gi.identifier_type
             WHEN 'animal_identifier_1' THEN 0
             WHEN 'animal_identifier_2' THEN 1
             WHEN 'temporary_tag' THEN 2
             ELSE 3
           END,
           gi.is_primary_for_goat DESC, gi.valid_from DESC, gi.identifier_id
  LIMIT 1
) tag ON TRUE
WHERE g.tenant_id = $1::uuid AND g.goat_id = ANY($3::uuid[])
-- An exact replay of the same confirm must not insert a second row for the same animal.
-- The live-goat partial unique index is what refuses a tagging onto a DIFFERENT sale;
-- this clause is only about this command's own retry.
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		tenantUUID, dealUUID, goatIDs, cmd.ActorID, cmd.OccurredAt)
	if err != nil {
		return fmt.Errorf("identity: insert sale allocations: %w", err)
	}
	return nil
}

func saleAllocationResult(dealID string, groups []ports.SaleAllocationShedGroup) *ports.SaleAllocationResult {
	total := 0
	for _, g := range groups {
		total += g.Animals
	}
	return &ports.SaleAllocationResult{SalesDealID: dealID, Allocated: total, ShedGroups: groups}
}

var _ ports.SaleAllocationWriter = (*Repository)(nil)

// saleExitRequestHash is the per-animal exit's request fingerprint.
//
// It covers the deal, the animal and the row_version the picker captured, which is what
// makes a same-key-different-payload replay detectable: retrying the same confirm produces
// the same hash, while a confirm that names the same animal at a DIFFERENT version is a
// different request and is refused rather than silently applied.
func saleExitRequestHash(cmd ports.RecordSaleAllocationsCommand, row ports.SaleAllocationRow) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"sales_deal_id": cmd.SalesDealID,
		"goat_id":       row.GoatID,
		"row_version":   row.RowVersion,
		"lifecycle":     "sold",
		"exit_reason":   "sold",
	})
	if err != nil {
		return "", fmt.Errorf("identity: sale exit request hash: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// saleEvidence points the animal's timeline at the deal it left on, so a later reader of
// the goat's history sees WHICH sale took it rather than only that it was sold.
func saleEvidence(salesDealID string) []domain.EvidenceRef {
	source := "sales"
	return []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "sales_deal:" + salesDealID,
		SourceSystem: &source,
	}}
}
