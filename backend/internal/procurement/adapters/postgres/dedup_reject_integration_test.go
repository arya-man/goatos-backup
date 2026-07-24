package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	procapp "github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// These regression tests cover PROC-001: the batched arrival-review upsert conflicts on
// (tenant_id, review_id, item_key) and the accept-intake pc_handoffs upsert conflicts on
// (tenant_id, load_id, goat_id). A single request carrying the same animal twice would fail
// with PostgreSQL "ON CONFLICT DO UPDATE command cannot affect row a second time", and the
// batched array_position state mapping would apply the first row's state to every repeat.
// The service must reject duplicates with a 400 before the batch write runs.

const (
	dedupGoatA = "10000000-0000-4000-8000-0000000000e1"
	dedupLoad  = "00000000-0000-4000-8000-0000000000f1"
	dedupShed  = "00000000-0000-4000-8000-0000000000f2"
)

// assertBadRequest extracts a *procapp.Error and asserts code + 400 status.
func assertBadRequest(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", wantCode)
	}
	var appErr *procapp.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not *app.Error: %v", err)
	}
	if appErr.Code != wantCode {
		t.Fatalf("error code = %q, want %q", appErr.Code, wantCode)
	}
	if appErr.HTTPStatus != 400 {
		t.Fatalf("error HTTPStatus = %d, want 400", appErr.HTTPStatus)
	}
}

func TestServiceRecordArrivalReviewRejectsDuplicateGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	svc := procapp.NewService(NewRepository(pool, 5*time.Second))
	gid := dedupGoatA
	_, err := svc.RecordArrivalReview(ctx, ports.ArrivalReview{
		TenantID:       testTenant,
		LoadID:         dedupLoad,
		ParkLocationID: testPark,
		ReviewedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ReviewedAtSet:  true,
		IdempotencyKey: "dedup-arrival-1",
		Goats: []ports.ArrivalGoat{
			{GoatID: &gid, ArrivalState: "matched"},
			{GoatID: &gid, ArrivalState: "matched"},
		},
	})
	assertBadRequest(t, err, "duplicate_arrival_item")

	// Rejected before any write: no arrival-review rows persisted.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM arrival_intake_review_goats WHERE tenant_id=$1`, testTenant).Scan(&n); err != nil {
		t.Fatalf("count arrival rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("arrival_intake_review_goats rows = %d, want 0 (rejected before write)", n)
	}
}

func TestServiceRecordArrivalReviewRejectsDuplicateIdentifier(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	svc := procapp.NewService(NewRepository(pool, 5*time.Second))
	// Same animal identifier with divergent casing/whitespace normalizes to one key.
	id1 := "TAG-77"
	id2 := "  tag-77 "
	_, err := svc.RecordArrivalReview(ctx, ports.ArrivalReview{
		TenantID:       testTenant,
		LoadID:         dedupLoad,
		ParkLocationID: testPark,
		ReviewedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ReviewedAtSet:  true,
		IdempotencyKey: "dedup-arrival-2",
		Goats: []ports.ArrivalGoat{
			{AnimalIdentifier1: &id1, ArrivalState: "missing"},
			{AnimalIdentifier1: &id2, ArrivalState: "missing"},
		},
	})
	assertBadRequest(t, err, "duplicate_arrival_item")
}

func TestServiceAcceptIntakeRejectsDuplicateGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	svc := procapp.NewService(NewRepository(pool, 5*time.Second))
	gid := dedupGoatA
	_, err := svc.AcceptIntake(ctx, ports.AcceptIntake{
		TenantID:       testTenant,
		LoadID:         dedupLoad,
		ParkLocationID: testPark,
		ShedLocationID: dedupShed,
		GoatIDs:        []string{gid, gid},
		EntryDate:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		IdempotencyKey: "dedup-intake-1",
	})
	assertBadRequest(t, err, "duplicate_goat_id")

	// Rejected before any write: no PC handoffs produced.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM procurement_pc_handoffs WHERE tenant_id=$1`, testTenant).Scan(&n); err != nil {
		t.Fatalf("count handoffs: %v", err)
	}
	if n != 0 {
		t.Fatalf("procurement_pc_handoffs rows = %d, want 0 (rejected before write)", n)
	}
}
