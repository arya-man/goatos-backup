package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// goat.sale_allocated (maintainer decision 2026-09-07): ONE event per sale CONFIRM, in the same
// transaction that tags the animals and exits them, carrying the pen-by-pen breakdown of what left.
//
// WHY A SECOND EVENT BESIDE goat.exited. Each animal already emits goat.exited, and that is the
// event obligation cancellation, tasks and health consume -- per animal, which is their grain. The
// Feed Director's grain is the PEN: "8 animals left Castro 1, 4 left Mandela 1 - Part 2, reduce
// their feed from Tuesday". Reassembling that from up to a hundred per-goat events at consume time
// would mean a consumer that re-reads the allocation table on every one of them and relies on the
// notification queue to swallow ninety-nine duplicates. The confirm transaction already knows the
// whole batch; it says so once.
//
// The aggregate is the SALE (sales_deal_id), which the identity module holds as an opaque
// reference (000177), never a foreign key. Idempotency is keyed on the deal and the confirm's own
// allocated_at instant: a sale confirmed in two halves is two batches and two events, while an
// exact replay of one confirm never reaches this insert at all (the confirm short-circuits on its
// idempotency row before any write).
const (
	goatSaleAllocatedEventType = "goat.sale_allocated"
	saleAllocationAggregate    = "sales_deal"
	saleAllocationSubject      = "sales_deal"
)

// readSaleAllocationBatch reads THIS confirm's rows -- the deal's allocations that share the
// confirm's allocated_at -- grouped by pen, inside the confirm's own transaction.
func (r *Repository) readSaleAllocationBatch(ctx context.Context, q saleAllocationQuerier, tenantID, salesDealID string, allocatedAt time.Time) (ports.SaleAllocationBatch, error) {
	batches, err := r.listSaleAllocationBatches(ctx, q, tenantID, &salesDealID, &allocatedAt, nil)
	if err != nil {
		return ports.SaleAllocationBatch{}, err
	}
	if len(batches) != 1 {
		return ports.SaleAllocationBatch{}, fmt.Errorf("identity: sale allocation batch for %s at %s: want one batch, got %d", salesDealID, allocatedAt.Format(time.RFC3339Nano), len(batches))
	}
	return batches[0], nil
}

// ListRecentSaleAllocationBatches implements ports.SaleAllocationBatchReader: every confirm since
// the given instant, grouped by pen. Served by goat_sale_allocations_recent_tagged_idx (000274).
func (r *Repository) ListRecentSaleAllocationBatches(ctx context.Context, tenantID string, since time.Time) ([]ports.SaleAllocationBatch, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.listSaleAllocationBatches(ctx, r.pool, tenantID, nil, nil, &since)
}

var _ ports.SaleAllocationBatchReader = (*Repository)(nil)

// saleAllocationBatchesSQL is the one query behind both batch reads, hoisted to a named const so
// the scale guard and a plan test can reach it. Filters are NULL-guarded parameters: $2 deal,
// $3 exact allocated_at (the confirm-time read), $4 trailing since (the reminder's read, served
// by goat_sale_allocations_recent_tagged_idx from 000274).
const saleAllocationBatchesSQL = `
SELECT a.sales_deal_id::text,
       a.allocated_at,
       COALESCE(a.park_id::text, ''),
       COALESCE(pk.name, ''),
       COALESCE(a.shed_id::text, ''),
       COALESCE(sh.name, ''),
       COALESCE(a.partition_label, ''),
       count(*)::int
FROM goat_sale_allocations a
LEFT JOIN locations sh ON sh.tenant_id = a.tenant_id AND sh.location_id = a.shed_id
LEFT JOIN locations pk ON pk.tenant_id = a.tenant_id AND pk.location_id = a.park_id
WHERE a.tenant_id = $1::uuid
  AND a.status = 'tagged'
  AND ($2::uuid IS NULL OR a.sales_deal_id = $2::uuid)
  AND ($3::timestamptz IS NULL OR a.allocated_at = $3::timestamptz)
  AND ($4::timestamptz IS NULL OR a.allocated_at >= $4::timestamptz)
GROUP BY a.sales_deal_id, a.allocated_at, a.park_id, pk.name, a.shed_id, sh.name, a.partition_label
ORDER BY a.allocated_at, a.sales_deal_id, pk.name, sh.name, a.partition_label`

// listSaleAllocationBatches runs saleAllocationBatchesSQL and folds its (deal, instant, pen) rows
// into batches.
// projection-review: membership=one goat_sale_allocations row per (deal, goat) with status='tagged' -- one row per ANIMAL, unique on (tenant_id, idempotency_key) and one live row per goat; group_key=(sales_deal_id, allocated_at, park_id, shed_id, partition_label) from the allocation SNAPSHOT, the same key the confirm-time event and the reminder read; join_cardinality=two LEFT JOINs to locations on its primary key location_id, 1:1 each, so count(*) counts animals and never fans out; pagination=NONE -- the confirm read is one (deal, instant) bounded by MaxSaleAllocationGoatsPerCommand and the reminder read is a 4-day trailing window over an indexed (tenant_id, allocated_at) scan; scope=tenant_id always, plus sales_deal_id + allocated_at for the confirm or since for the reminder; park/shed are GROUP keys, not filters
func (r *Repository) listSaleAllocationBatches(ctx context.Context, q saleAllocationQuerier, tenantID string, salesDealID *string, allocatedAt, since *time.Time) ([]ports.SaleAllocationBatch, error) {
	rows, err := q.Query(ctx, saleAllocationBatchesSQL,
		tenantID, salesDealID, allocatedAt, since)
	if err != nil {
		return nil, fmt.Errorf("identity: list sale allocation batches: %w", err)
	}
	defer rows.Close()

	out := []ports.SaleAllocationBatch{}
	index := map[string]int{}
	for rows.Next() {
		var dealID string
		var at time.Time
		var pen ports.SaleAllocationPen
		if err := rows.Scan(&dealID, &at, &pen.ParkID, &pen.ParkName, &pen.ShedID, &pen.ShedName,
			&pen.PartitionLabel, &pen.Animals); err != nil {
			return nil, fmt.Errorf("identity: scan sale allocation batch: %w", err)
		}
		// Canonical composition, never hand-rolled: "Castro 1" for a numeric pen, "Godel 1 - Part 3"
		// for a worded one.
		pen.OperationalLocationDisplay = (oploc.OperationalLocation{
			ShedID: pen.ShedID, ShedName: pen.ShedName, PartitionLabel: pen.PartitionLabel,
		}).Display()
		key := dealID + "|" + at.UTC().Format(time.RFC3339Nano)
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, ports.SaleAllocationBatch{SalesDealID: dealID, AllocatedAt: at})
		}
		out[i].Pens = append(out[i].Pens, pen)
		out[i].Animals += pen.Animals
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list sale allocation batches: %w", err)
	}
	return out, nil
}

// saleAllocatedIdempotencyKey is one confirm's event key: the deal plus the confirm instant at the
// microsecond precision Postgres stores, so the key computed here equals the key any later reader
// derives from the stored allocated_at.
func saleAllocatedIdempotencyKey(salesDealID string, allocatedAt time.Time) string {
	return fmt.Sprintf("%s:%s:%d", goatSaleAllocatedEventType, salesDealID, allocatedAt.UnixMicro())
}

// insertSaleAllocatedOutbox writes the goat.sale_allocated envelope for one confirm. Called inside
// the confirm transaction, after the exits, so the event commits with the state it describes.
func (r *Repository) insertSaleAllocatedOutbox(ctx context.Context, qtx *identitydb.Queries, cmd ports.RecordSaleAllocationsCommand, tenantUUID, dealUUID pgtype.UUID, batch ports.SaleAllocationBatch) error {
	idempotencyKey := saleAllocatedIdempotencyKey(cmd.SalesDealID, cmd.OccurredAt)
	eventID := platformoutbox.DeterministicUUID(idempotencyKey + ":" + cmd.TenantID)
	eventUUID, err := uuidParam(eventID)
	if err != nil {
		return fmt.Errorf("identity: sale allocated event id: %w", err)
	}
	// The envelope schema requires a non-empty trace_id; a confirm always carries one, but the
	// event must not be the thing that dies if it ever does not.
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
			"shed_id":                      pen.ShedID,
			"shed_name":                    pen.ShedName,
			"partition_label":              pen.PartitionLabel,
			"operational_location_display": pen.OperationalLocationDisplay,
			"animals":                      pen.Animals,
		})
	}
	// The visibility scope names the park only when the whole batch stands in one; a batch that
	// somehow spans parks is tenant-scoped rather than mis-filed under the first pen's park.
	var scope domain.LocationScope
	if len(parkIDs) == 1 {
		for id := range parkIDs {
			parkID := id
			scope.ParkID = &parkID
		}
	}
	envelope := eventEnvelope{
		EventID:         eventID,
		EventType:       goatSaleAllocatedEventType,
		SchemaVersion:   eventSchemaVersion,
		SchemaRef:       eventSchemaRef,
		AggregateType:   saleAllocationAggregate,
		AggregateID:     cmd.SalesDealID,
		OccurredAt:      cmd.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:      cmd.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
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
			"allocated_at":  cmd.OccurredAt.UTC().Format(time.RFC3339Nano),
			"animals":       batch.Animals,
			"pens":          pens,
			"scope_type":    "sales_deal",
			"scope_id":      cmd.SalesDealID,
		},
		TraceID: traceID,
	}
	payload, err := marshalEnvelopeWithTenant(envelope, cmd.TenantID)
	if err != nil {
		return fmt.Errorf("identity: sale allocated envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"trace_id":               traceID,
	})
	if err != nil {
		return fmt.Errorf("identity: sale allocated headers: %w", err)
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		EventType:      goatSaleAllocatedEventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  saleAllocationAggregate,
		AggregateID:    dealUUID,
		Topic:          goatLifecycleTopic,
		Payload:        payload,
		Headers:        headers,
		IdempotencyKey: idempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(traceID)),
	}); err != nil {
		return fmt.Errorf("identity: insert sale allocated outbox: %w", err)
	}
	return nil
}

// marshalEnvelopeWithTenant serialises an envelope and stamps visibility_scope.tenant_id, which the
// schema requires and domain.LocationScope does not carry -- the same two-step the goat lifecycle
// envelope performs.
func marshalEnvelopeWithTenant(envelope eventEnvelope, tenantID string) ([]byte, error) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	var withTenant map[string]any
	if err := json.Unmarshal(raw, &withTenant); err != nil {
		return nil, err
	}
	visibilityScope, ok := withTenant["visibility_scope"].(map[string]any)
	if !ok {
		visibilityScope = map[string]any{}
	}
	visibilityScope["tenant_id"] = tenantID
	withTenant["visibility_scope"] = visibilityScope
	return json.Marshal(withTenant)
}
