package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// RecordExternalConsumption upserts one (park, feed, day) row of the
// feed_external_consumption ledger (migration 000185). The farm label is
// resolved from the park location in the same statement — the ledger's natural
// key is farm-labelled because the legacy sheet's is, and the two writers (this
// method and cmd/import-feed-external-consumption) must land on the SAME key
// or an overlap day would double-count instead of converging. An unresolvable
// park is a loud error, never a silent skip: the event redelivers until the
// org data is fixed.
func (r *Repository) RecordExternalConsumption(ctx context.Context, in ports.RecordExternalConsumptionCommand) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ParkID) == "" ||
		strings.TrimSpace(in.FeedItemLabel) == "" || strings.TrimSpace(in.FeedDay) == "" {
		return fmt.Errorf("feeddirection: external consumption command incomplete: %+v", in)
	}
	if in.QuantityKg <= 0 {
		// Validate-or-reject: a verified preparation always carries a positive
		// answer, so zero here means a producer bug, not a real zero-milk day.
		return fmt.Errorf("feeddirection: external consumption quantity must be positive, got %v (%s)", in.QuantityKg, in.SourceRef)
	}
	tag, err := r.pool.Exec(ctx, `
INSERT INTO feed_external_consumption
  (tenant_id, park_id, farm_label, feed_item_label, feed_day, quantity_kg, source_ref)
SELECT $1::uuid, $2::uuid, COALESCE(NULLIF(upper(l.location_code), ''), l.name), $3, $4::date, $5::numeric, $6
FROM locations l
WHERE l.tenant_id = $1::uuid AND l.location_id = $2::uuid AND l.location_type = 'park'
ON CONFLICT (tenant_id, farm_label, feed_item_key, feed_day) DO UPDATE SET
  quantity_kg = EXCLUDED.quantity_kg, park_id = EXCLUDED.park_id,
  source_ref = EXCLUDED.source_ref, imported_at = now()`,
		in.TenantID, in.ParkID, in.FeedItemLabel, in.FeedDay, in.QuantityKg, in.SourceRef)
	if err != nil {
		return fmt.Errorf("feeddirection: record external consumption: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("feeddirection: external consumption park %s does not resolve to a park location", in.ParkID)
	}
	return nil
}
