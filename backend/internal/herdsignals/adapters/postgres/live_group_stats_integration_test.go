package postgres

import (
	"context"
	"testing"
)

// ListLivePenMedians aggregates whole-cohort pen medians in SQL: the unmapped tag never joins a
// pen, the mapped tag lands in its animal's pen exactly once (no identifier fan-out), and the
// movement_state filter is not part of its signature at all.
func TestListLivePenMediansIntegration(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	if _, err := pool.Exec(ctx, `
		INSERT INTO herd_signal_tag_latest (tenant_id, tag_id, last_seen_at, motion_delta, tag_temperature_c, mapping_state, gap_delta)
		VALUES ($1, $2, now(), 40, 37.5, 'mapped', false),
		       ($1, $3, now(), 999, 45.0, 'unmapped', false)`, hsiTenant, hsiMappedTag, hsiUnmappedTag); err != nil {
		t.Fatal(err)
	}
	// A second active identifier row for the SAME animal must not double-count the tag.
	if _, err := pool.Exec(ctx, `
		INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable)
		VALUES ($1::uuid, $2::uuid, 'animal_identifier_2', 'SECOND-ID', 'SECOND-ID', 'global', false, 'active', now(), 'test_v1', true)`, hsiTenant, hsiGoat); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListLivePenMedians(ctx, hsiTenant, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListLivePenMedians: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("pens = %v, want only the mapped animal's pen", got)
	}
	m, ok := got[hsiShed]
	if !ok || m.MotionMedian == nil || *m.MotionMedian != 40 || m.TempMedian == nil || *m.TempMedian != 37.5 {
		t.Fatalf("pen medians = %+v, want motion 40 temp 37.5", m)
	}
}
