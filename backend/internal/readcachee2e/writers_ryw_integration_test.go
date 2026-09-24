package readcachee2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
	"github.com/vgoats/goatos/backend/internal/platform/readcache/readcachetest"
)

// The identity goat writers (lifecycle move / exit / stage, census correction, shed stage) and the
// batch-transaction publish the feed-purchase importer uses: each write makes the writing
// instance's next cached read fresh at once and a sibling instance's within one second. The page
// level proof for relocate, partition move, feed purchase, location, assumptions and weighing is
// TestReadYourWritesAcrossTwoInstances; the other module writers have the same check in their own
// package (procurement receive, feed-direction issues, herd-signals mapping, configuration
// registers, weighing park move and kernel claims).
func TestIdentityAndImporterWritesEvictBothInstances(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFixture(t, ctx, pool)
	execT(t, ctx, pool, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status)
SELECT $1::uuid, c, c, 'kid', 'active' FROM unnest(ARRAY['kid','weaner','K2']) AS c
WHERE NOT EXISTS (SELECT 1 FROM animal_stage_lookup s WHERE s.tenant_id = $1::uuid AND s.stage_code = c)`, tenant)
	execT(t, ctx, pool, `
INSERT INTO breeds (species, canonical_name, status) VALUES ('goat', 'Beetal', 'active'), ('goat', 'Sirohi', 'active')
ON CONFLICT DO NOTHING`)
	pair := readcachetest.NewPair(t, ctx, pool)
	repo := identitypg.NewRepository(pool, 30*time.Second).WithReadCacheInvalidator(pair.Writer)
	parks := []string{park}

	lumpGoats := func(n int) []string {
		rows, err := pool.Query(ctx, `SELECT goat_id::text FROM goats WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND lifecycle_status = 'alive' ORDER BY display_id LIMIT $3`, tenant, shedLump, n)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(ids) < n {
			t.Fatalf("lump goats: %v %v", ids, err)
		}
		return ids
	}
	rowVersion := func(goatID string) int {
		var rv int
		if err := pool.QueryRow(ctx, `SELECT row_version FROM goats WHERE goat_id = $1::uuid`, goatID).Scan(&rv); err != nil {
			t.Fatal(err)
		}
		return rv
	}
	src := "readcachee2e"
	ev := []identitydomain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "ryw", SourceSystem: &src}}
	at := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	g := lumpGoats(3)

	pair.Check(t, ctx, "identity MoveGoat", tenant, parks, true, func(t *testing.T) {
		if _, err := repo.MoveGoat(ctx, identityports.MoveGoatCommand{
			TenantID: tenant, ActorID: operator, ClientIdempotencyKey: "ryw-move", StoredIdempotencyKey: tenant + ":moveGoat:ryw-move",
			IdempotencyScope: "moveGoat", RequestHash: "ryw-move", TraceID: "ryw-move", GoatID: g[0], ToParkID: park, ToShedID: shedOther,
			Reason: "e2e", OccurredAt: at, EvidenceRefs: ev, RowVersion: rowVersion(g[0]), GuardrailApproved: true,
		}); err != nil {
			t.Fatalf("MoveGoat: %v", err)
		}
	})
	pair.Check(t, ctx, "identity StageGoat", tenant, parks, true, func(t *testing.T) {
		if _, err := repo.StageGoat(ctx, identityports.StageGoatCommand{
			TenantID: tenant, ActorID: operator, ClientIdempotencyKey: "ryw-stage", StoredIdempotencyKey: tenant + ":stageGoat:ryw-stage",
			IdempotencyScope: "stageGoat", RequestHash: "ryw-stage", TraceID: "ryw-stage", GoatID: g[1], ManagementStage: "weaner",
			Reason: "e2e", OccurredAt: at, EvidenceRefs: ev, RowVersion: rowVersion(g[1]),
		}); err != nil {
			t.Fatalf("StageGoat: %v", err)
		}
	})
	pair.Check(t, ctx, "identity ExitGoat", tenant, parks, true, func(t *testing.T) {
		if _, err := repo.ExitGoat(ctx, identityports.ExitGoatCommand{
			TenantID: tenant, ActorID: operator, ClientIdempotencyKey: "ryw-exit", StoredIdempotencyKey: tenant + ":exitGoat:ryw-exit",
			IdempotencyScope: "exitGoat", RequestHash: "ryw-exit", TraceID: "ryw-exit", GoatID: g[2], LifecycleStatus: "sold", ExitReason: "sold",
			Reason: "e2e", OccurredAt: at, EvidenceRefs: ev, RowVersion: rowVersion(g[2]),
		}); err != nil {
			t.Fatalf("ExitGoat: %v", err)
		}
	})
	pair.Check(t, ctx, "identity CorrectCensusSlice", tenant, parks, true, func(t *testing.T) {
		if _, err := repo.CorrectCensusSlice(ctx, identityports.CorrectCensusSliceCommand{
			TenantID: tenant, ActorID: operator, ClientIdempotencyKey: "ryw-census", StoredIdempotencyKey: "ryw-census",
			IdempotencyScope: "correctCensusSlice", RequestHash: "ryw-census", TraceID: "ryw-census",
			ShedID: shedLump, ManagementStage: "kid", Breed: "Beetal", Sex: "male", Field: "breed", Value: "Sirohi",
			Reason: "e2e", OccurredAt: at,
		}); err != nil {
			t.Fatalf("CorrectCensusSlice: %v", err)
		}
	})
	pair.Check(t, ctx, "identity ReclassifyShedStage", tenant, parks, true, func(t *testing.T) {
		if _, err := repo.ReclassifyShedStage(ctx, identityports.ReclassifyShedStageCommand{
			TenantID: tenant, ActorID: operator, ClientIdempotencyKey: "ryw-shedstage", StoredIdempotencyKey: "ryw-shedstage",
			IdempotencyScope: "reclassifyShedStage", RequestHash: "ryw-shedstage", TraceID: "ryw-shedstage",
			ShedID: shedLump, ManagementStage: "K2", Reason: "e2e", OccurredAt: at,
		}); err != nil {
			t.Fatalf("ReclassifyShedStage: %v", err)
		}
	})
	// cmd/import-feed-purchases publishes with readcache.QueueNotify inside its one-transaction
	// upsert batch (it has no cache of its own, so both API instances hear it by NOTIFY).
	pair.Check(t, ctx, "import-feed-purchases batch (QueueNotify)", tenant, parks, false, func(t *testing.T) {
		b := &pgx.Batch{}
		b.Queue(`UPDATE feed_purchases SET total_cost = total_cost + 1 WHERE tenant_id = $1::uuid`, tenant)
		readcache.QueueNotify(b, tenant)
		if err := pool.SendBatch(ctx, b).Close(); err != nil {
			t.Fatalf("batch: %v", err)
		}
	})
}
