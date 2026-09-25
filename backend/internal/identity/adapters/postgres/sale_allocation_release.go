package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// A FAILED SALE GIVES ITS ANIMALS BACK (maintainer decision 2026-09-25, docs/decisions/sales-sop.md
// -> "A failed sale"). The tagging confirm (RecordSaleAllocations) did exactly three things to each
// animal: it wrote a goat_sale_allocations row, and through the canonical exit it set
// goats.lifecycle_status = 'sold', exit_reason = 'sold' and exited_at. It did NOT move the animal:
// goats.shed_id / park_id / current_location_id and goat_shed_partitions are untouched by an exit.
// The release therefore undoes exactly those, and nothing else:
//
//   - goats: lifecycle_status 'sold' -> 'alive', exit_reason and exited_at cleared, row_version
//     bumped. The pen is the one the animal never left.
//   - goat_sale_allocations: status 'tagged' -> 'released' with released_at / released_by /
//     release_reason. Never deleted: the row is the history of the sale that fell through, and every
//     sold-animal read (Sold page counts and weight bands, Load wise, Farm born, the Feed Director's
//     07:00 reminder) already reads status = 'tagged' only, so they stop counting the animal.
//   - per animal: an identity decision, a goat_identity_events row, an audit row and a
//     `goat.reinstated` outbox event, through the SAME finishGoatLifecycleMutation every lifecycle
//     write uses. Vaccination consumes that event with its ordinary per-goat recheck, so the kernel
//     -- never a hand insert -- re-evaluates the animal and reschedules the work the exit cancelled.
//
// Counts, the herd register and the feed projection read live goats (the herd register through
// its own trigger), so the animal is back in its pen's head count the moment this commits.
const (
	saleReleaseScope          = "sale_release"
	goatReinstatedEventType   = "goat.reinstated"
	goatReinstatedDecisionRes = "goat_reinstated"
	saleReleaseDefaultReason  = "Sale failed; animal returned to the herd"
)

// selectTaggedAllocationsForReleaseSQL locks the deal's live allocations (the
// goat_sale_allocations_deal_idx (tenant_id, sales_deal_id) index). A deal's animals are bounded by
// the sale, so the set is small; released rows are history and are not read.
const selectTaggedAllocationsForReleaseSQL = `
SELECT a.allocation_id::text, a.goat_id::text, COALESCE(a.allocated_by::text, '')
FROM goat_sale_allocations a
WHERE a.tenant_id = $1::uuid AND a.sales_deal_id = $2::uuid AND a.status = 'tagged'
ORDER BY a.allocation_id
FOR UPDATE`

// reinstateSoldGoatSQL flips one sold animal back to alive, undoing exactly what the exit set.
const reinstateSoldGoatSQL = `
UPDATE goats
SET lifecycle_status = 'alive',
    exit_reason = NULL,
    exited_at = NULL,
    updated_at = $3::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND lifecycle_status = 'sold'`

// releaseAllocationsSQL marks the allocations released in ONE set-based statement.
const releaseAllocationsSQL = `
UPDATE goat_sale_allocations
SET status = 'released',
    released_at = $3::timestamptz,
    released_by = COALESCE(NULLIF($4, '')::uuid, allocated_by),
    release_reason = $5,
    row_version = row_version + 1,
    updated_at = $3::timestamptz
WHERE tenant_id = $1::uuid AND allocation_id = ANY($2::uuid[]) AND status = 'tagged'`

// ReleaseSaleAllocations releases every animal still tagged to the failed deal in ONE transaction.
//
// scale-guard:ignore: bounded per-goat canonical lifecycle transition inside one transaction (the deal's own tagged animals); a set-based UPDATE would skip the per-animal event, decision and audit that vaccination re-evaluation depends on -- the same shape as RecordSaleAllocations.
func (r *Repository) ReleaseSaleAllocations(ctx context.Context, cmd ports.ReleaseSaleAllocationsCommand) (int, error) {
	if strings.TrimSpace(cmd.TenantID) == "" || strings.TrimSpace(cmd.SalesDealID) == "" || cmd.OccurredAt.IsZero() {
		return 0, fmt.Errorf("identity: release sale allocations: tenant, deal and time are required")
	}
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return 0, err
	}
	reason := strings.TrimSpace(cmd.Reason)
	if reason == "" {
		reason = saleReleaseDefaultReason
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("identity: release sale allocations: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)
	// The same per-sale lock the tagging confirm takes, so a release and a confirm never interleave.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, cmd.TenantID+":"+cmd.SalesDealID); err != nil {
		return 0, fmt.Errorf("identity: release sale allocations: lock sale: %w", err)
	}

	type allocation struct{ id, goatID, allocatedBy string }
	rows, err := tx.Query(ctx, selectTaggedAllocationsForReleaseSQL, cmd.TenantID, cmd.SalesDealID)
	if err != nil {
		return 0, fmt.Errorf("identity: release sale allocations: read: %w", err)
	}
	var allocations []allocation
	for rows.Next() {
		var a allocation
		if err := rows.Scan(&a.id, &a.goatID, &a.allocatedBy); err != nil {
			rows.Close()
			return 0, err
		}
		allocations = append(allocations, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(allocations) == 0 {
		// Nothing tagged (or an exact replay): nothing to release.
		return 0, tx.Commit(ctx)
	}

	// The pen-by-pen breakdown of what is coming back, read BEFORE the rows stop being 'tagged':
	// the Feed Director's grain is the pen (goat.sale_released, the mirror of goat.sale_allocated).
	batches, err := r.listSaleAllocationBatches(ctx, tx, cmd.TenantID, &cmd.SalesDealID, nil, nil)
	if err != nil {
		return 0, err
	}
	released := mergeReleasedPens(cmd.SalesDealID, cmd.OccurredAt, batches)

	ids := make([]string, 0, len(allocations))
	for _, a := range allocations {
		ids = append(ids, a.id)
		actor := strings.TrimSpace(cmd.ActorID)
		if actor == "" {
			actor = a.allocatedBy
		}
		if err := r.reinstateSoldGoatInTx(ctx, tx, qtx, tenantUUID, cmd, a.goatID, actor, reason); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, releaseAllocationsSQL, cmd.TenantID, ids, cmd.OccurredAt, strings.TrimSpace(cmd.ActorID), reason); err != nil {
		return 0, fmt.Errorf("identity: release sale allocations: mark released: %w", err)
	}
	var buyer string
	if err := tx.QueryRow(ctx, saleReleaseBuyerSQL, cmd.TenantID, cmd.SalesDealID).Scan(&buyer); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("identity: release sale allocations: read buyer: %w", err)
	}
	if err := r.insertSaleReleasedOutbox(ctx, qtx, tenantUUID, cmd, strings.TrimSpace(buyer), released); err != nil {
		return 0, err
	}
	if err := r.commitHerdWrite(ctx, tx, cmd.TenantID); err != nil {
		return 0, err
	}
	return len(allocations), nil
}

// reinstateSoldGoatInTx returns one sold animal to alive and records it as a lifecycle mutation
// (decision, identity event, audit, `goat.reinstated` outbox row) inside the caller's transaction.
// An animal that is no longer 'sold' (already reinstated) is skipped: its allocation is still
// released, but nothing about the animal is rewritten.
func (r *Repository) reinstateSoldGoatInTx(ctx context.Context, tx pgx.Tx, qtx *identitydb.Queries, tenantUUID pgtype.UUID, cmd ports.ReleaseSaleAllocationsCommand, goatID, actorID, reason string) error {
	goatUUID, err := uuidParam(goatID)
	if err != nil {
		return err
	}
	actorUUID, err := uuidParam(actorID)
	if err != nil {
		return fmt.Errorf("identity: release sale allocations: no person to record the release against: %w", err)
	}
	state, err := lockGoatForLifecycleMutation(ctx, tx, cmd.TenantID, goatID)
	if err != nil {
		return err
	}
	if state.LifecycleStatus != "sold" || state.MergedIntoGoatID != nil {
		return nil
	}
	key := fmt.Sprintf("%s:%s:%s:%s", cmd.TenantID, saleReleaseScope, cmd.SalesDealID, goatID)
	sum := sha256.Sum256([]byte(cmd.SalesDealID + ":" + goatID + ":release"))
	if _, err := qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: key,
		TenantID:       tenantUUID,
		Scope:          saleReleaseScope,
		RequestHash:    hex.EncodeToString(sum[:]),
	}); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, reinstateSoldGoatSQL, cmd.TenantID, goatID, cmd.OccurredAt); err != nil {
		return err
	}
	source := "sales"
	payload := map[string]any{
		"goat_id":             goatID,
		"previous_lifecycle":  state.LifecycleStatus,
		"lifecycle_status":    "alive",
		"reason":              reason,
		"sales_deal_id":       cmd.SalesDealID,
		"row_version_from":    state.RowVersion,
		"current_location_id": stringValue(state.CurrentLocation),
		"current_park_id":     stringValue(state.ParkID),
		"current_shed_id":     stringValue(state.ShedID),
		"partition_label":     stringValue(state.PartitionLabel),
		"scope_type":          "goat",
		"scope_id":            goatID,
	}
	_, err = r.finishGoatLifecycleMutation(ctx, tx, qtx, new(bool), goatLifecycleFinish{
		TenantUUID:    tenantUUID,
		ActorUUID:     actorUUID,
		GoatUUID:      goatUUID,
		AggregateUUID: goatUUID,
		Command: goatLifecycleCommand{
			TenantID:             cmd.TenantID,
			ActorID:              actorID,
			ClientIdempotencyKey: saleReleaseScope + ":" + cmd.SalesDealID + ":" + goatID,
			StoredIdempotencyKey: key,
			IdempotencyScope:     saleReleaseScope,
			TraceID:              cmd.TraceID,
			GoatID:               goatID,
			Reason:               reason,
			EvidenceRefs: []domain.EvidenceRef{{
				EvidenceType: "source_record",
				EvidenceID:   "sales_deal:" + cmd.SalesDealID,
				SourceSystem: &source,
			}},
		},
		// A failed sale CORRECTS the animal's lifecycle fact: it never left. Recorded under the
		// existing lifecycle-correction decision type rather than a new one.
		DecisionType:   goatIdentityChangedDecisionType,
		DecisionResult: goatReinstatedDecisionRes,
		EventType:      goatReinstatedEventType,
		OccurredAt:     cmd.OccurredAt,
		Payload:        payload,
		Scope:          domain.LocationScope{FarmID: state.FarmID, ParkID: state.ParkID, ShedID: state.ShedID},
		AggregateType:  goatLifecycleAggregate,
		SubjectType:    goatLifecycleSubject,
		SubjectID:      goatID,
		DeferCommit:    true,
	})
	return err
}

var _ ports.SaleAllocationReleaser = (*Repository)(nil)

// saleReleaseBuyerSQL reads the failed deal's buyer for the Feed Director's message, by primary
// key -- the same deal row the tagging confirm already locks and writes.
const saleReleaseBuyerSQL = `
SELECT buyer_name FROM sales_deals WHERE tenant_id = $1::uuid AND id = $2::uuid`

// goatSaleReleasedEventType (maintainer decision 2026-09-25): ONE event per released deal, in the
// release transaction, carrying the pen-by-pen breakdown of the animals that came back -- the
// mirror of goat.sale_allocated. Its consumer tells the Feed Director those pens feed as before.
// Only a release that returned animals emits it; Deal Failed is final, so a deal emits it once.
const goatSaleReleasedEventType = "goat.sale_released"

// mergeReleasedPens folds the deal's confirm batches (a sale may be tagged in several confirms)
// into one batch per pen, stamped with the release instant.
func mergeReleasedPens(dealID string, at time.Time, batches []ports.SaleAllocationBatch) ports.SaleAllocationBatch {
	out := ports.SaleAllocationBatch{SalesDealID: dealID, AllocatedAt: at}
	index := map[string]int{}
	for _, b := range batches {
		for _, pen := range b.Pens {
			key := pen.ParkID + "|" + pen.ShedID + "|" + pen.PartitionLabel
			i, ok := index[key]
			if !ok {
				i = len(out.Pens)
				index[key] = i
				out.Pens = append(out.Pens, ports.SaleAllocationPen{
					ParkID: pen.ParkID, ParkName: pen.ParkName, ParkCode: pen.ParkCode, ShedID: pen.ShedID, ShedName: pen.ShedName,
					PartitionLabel: pen.PartitionLabel, OperationalLocationDisplay: pen.OperationalLocationDisplay,
				})
			}
			out.Pens[i].Animals += pen.Animals
			out.Animals += pen.Animals
		}
	}
	return out
}

// insertSaleReleasedOutbox writes the goat.sale_released envelope inside the release transaction.
func (r *Repository) insertSaleReleasedOutbox(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, cmd ports.ReleaseSaleAllocationsCommand, buyer string, batch ports.SaleAllocationBatch) error {
	if batch.Animals == 0 {
		return nil
	}
	dealUUID, err := uuidParam(cmd.SalesDealID)
	if err != nil {
		return err
	}
	idempotencyKey := goatSaleReleasedEventType + ":" + cmd.SalesDealID
	eventID := platformoutbox.DeterministicUUID(idempotencyKey + ":" + cmd.TenantID)
	eventUUID, err := uuidParam(eventID)
	if err != nil {
		return err
	}
	traceID := strings.TrimSpace(cmd.TraceID)
	if traceID == "" {
		traceID = idempotencyKey
	}
	pens := make([]map[string]any, 0, len(batch.Pens))
	parkIDs := map[string]struct{}{}
	for _, pen := range batch.Pens {
		if pen.ParkID != "" {
			parkIDs[pen.ParkID] = struct{}{}
		}
		pens = append(pens, map[string]any{
			"park_id":                      pen.ParkID,
			"park_name":                    pen.ParkName,
			"park_code":                    pen.ParkCode,
			"shed_id":                      pen.ShedID,
			"shed_name":                    pen.ShedName,
			"partition_label":              pen.PartitionLabel,
			"operational_location_display": pen.OperationalLocationDisplay,
			"animals":                      pen.Animals,
		})
	}
	var scope domain.LocationScope
	if len(parkIDs) == 1 {
		for id := range parkIDs {
			parkID := id
			scope.ParkID = &parkID
		}
	}
	at := cmd.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	payload, err := marshalEnvelopeWithTenant(eventEnvelope{
		EventID:         eventID,
		EventType:       goatSaleReleasedEventType,
		SchemaVersion:   eventSchemaVersion,
		SchemaRef:       eventSchemaRef,
		AggregateType:   saleAllocationAggregate,
		AggregateID:     cmd.SalesDealID,
		OccurredAt:      at,
		RecordedAt:      at,
		Producer:        eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey:  idempotencyKey,
		Actor:           eventActor{ActorType: "human", ActorID: nonEmptyStringPtr(cmd.ActorID)},
		SubjectType:     saleAllocationSubject,
		SubjectID:       cmd.SalesDealID,
		VisibilityScope: scope,
		EvidenceRefs:    saleEvidence(cmd.SalesDealID),
		Payload: map[string]any{
			"tenant_id":     cmd.TenantID,
			"sales_deal_id": cmd.SalesDealID,
			"buyer_name":    buyer,
			"released_at":   cmd.OccurredAt.UTC().Format(time.RFC3339Nano),
			"animals":       batch.Animals,
			"pens":          pens,
			"scope_type":    "sales_deal",
			"scope_id":      cmd.SalesDealID,
		},
		TraceID: traceID,
	}, cmd.TenantID)
	if err != nil {
		return fmt.Errorf("identity: sale released envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{"actor_id": cmd.ActorID, "trace_id": traceID})
	if err != nil {
		return err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		EventType:      goatSaleReleasedEventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  saleAllocationAggregate,
		AggregateID:    dealUUID,
		Topic:          goatLifecycleTopic,
		Payload:        payload,
		Headers:        headers,
		IdempotencyKey: idempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(traceID)),
	}); err != nil {
		return fmt.Errorf("identity: insert sale released outbox: %w", err)
	}
	return nil
}
