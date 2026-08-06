package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestListUnbatchedDueShedNameMatchesOplocDisplay pins the SQL shed_name CASE expression in
// ListUnbatchedDueForVersion (obligation/adapters/postgres/sqlc/query.sql) to agree BYTE-FOR-BYTE
// with the Go primitive backend/internal/platform/oploc.OperationalLocation.Display() for every
// display shape it defines.
//
// This is a regression test for a real drift: the hand-rolled SQL CASE previously rendered the
// numeric convention as "Castro - Part 2" while oploc.Display() (and AGENTS.md's explicit rule)
// render it "Castro 2". Two copies of the same display rule -- one in Go, one duplicated across
// six SQL sites -- disagreed for the SAME animal in the SAME partition. There is no compile-time
// link between a SQL string literal and a Go function, so this must be caught by a live-DB
// assertion, not by inspection.
func TestListUnbatchedDueShedNameMatchesOplocDisplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		shedNumeric  = "00000000-0000-4000-8000-00000000f101" // Castro-style: partition "2"
		shedWorded   = "00000000-0000-4000-8000-00000000f102" // Godel-style: partition "Part 3"
		shedWhole    = "00000000-0000-4000-8000-00000000f103" // Yashoda-style: no partition
		goatNumeric  = "10000000-0000-4000-8000-00000000f101"
		goatWorded   = "10000000-0000-4000-8000-00000000f102"
		goatWhole    = "10000000-0000-4000-8000-00000000f103"
		goatMovedOld = "10000000-0000-4000-8000-00000000f104"
	)

	seedParkConsolidationShed(t, ctx, pool, shedNumeric, "Castro")
	seedParkConsolidationShed(t, ctx, pool, shedWorded, "Godel 1")
	seedParkConsolidationShed(t, ctx, pool, shedWhole, "Yashoda")

	seedShedScopedGoat(t, ctx, pool, goatNumeric, shedNumeric)
	seedShedScopedGoat(t, ctx, pool, goatWorded, shedWorded)
	seedShedScopedGoat(t, ctx, pool, goatWhole, shedWhole)
	seedShedScopedGoat(t, ctx, pool, goatMovedOld, shedNumeric)

	setGoatPartition(t, ctx, pool, goatNumeric, shedNumeric, "2")
	setGoatPartition(t, ctx, pool, goatWorded, shedWorded, "Part 3")
	// goatWhole gets no goat_shed_partitions row at all -- the NULL/no-row shape.
	setGoatPartition(t, ctx, pool, goatMovedOld, shedNumeric, "1")

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.partitionparity", Name: "Partition Parity", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	obNumeric := insertPartitionParityObligation(t, ctx, repo, versionID, ruleID, goatNumeric, shedNumeric, due, "obl-parity-numeric")
	obWorded := insertPartitionParityObligation(t, ctx, repo, versionID, ruleID, goatWorded, shedWorded, due, "obl-parity-worded")
	obWhole := insertPartitionParityObligation(t, ctx, repo, versionID, ruleID, goatWhole, shedWhole, due, "obl-parity-whole")
	obMoved := insertPartitionParityObligation(t, ctx, repo, versionID, ruleID, goatMovedOld, shedNumeric, due, "obl-parity-moved")

	rows, err := repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, due.Add(24*time.Hour), 100)
	if err != nil {
		t.Fatalf("list unbatched due: %v", err)
	}
	byOb := map[string]domain.UnbatchedDue{}
	for _, r := range rows {
		byOb[r.ObligationID] = r
	}

	cases := []struct {
		name        string
		obligation  string
		shedName    string
		partition   string
		wantDisplay string
	}{
		{"numeric convention", obNumeric, "Castro", "2", "Castro 2"},
		{"worded convention", obWorded, "Godel 1", "Part 3", "Godel 1 - Part 3"},
		{"non-partitioned, never 'whole'", obWhole, "Yashoda", "", "Yashoda"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, ok := byOb[tc.obligation]
			if !ok {
				t.Fatalf("obligation %s missing from unbatched-due list", tc.obligation)
			}
			// The Go primitive is the single source of truth for the display string.
			want := oploc.OperationalLocation{ShedName: tc.shedName, PartitionLabel: tc.partition}.Display()
			if want != tc.wantDisplay {
				t.Fatalf("test bug: oploc.Display() = %q, want fixture expectation %q", want, tc.wantDisplay)
			}
			// Compose the display from the raw SQL values using the shared primitive.
			got := oploc.OperationalLocation{ShedName: row.ShedName, PartitionLabel: row.PartitionLabel}.Display()
			if got != want {
				t.Fatalf("composed from SQL shed_name=%q partition_label=%q gives %q, want %q (oploc.Display() expected)", row.ShedName, row.PartitionLabel, got, want)
			}
			if row.PartitionLabel == "whole" {
				t.Fatalf("SQL partition_label leaked the matching sentinel 'whole': %q", row.PartitionLabel)
			}
		})
	}

	// Move-tracking: the animal moves from partition "1" to partition "2" WITHIN the same shed.
	// The obligation row (obMoved) is never touched -- if the display were denormalised onto
	// obligation_instances it would still read "Castro 1" here. Because the SQL reads
	// goat_shed_partitions live at query time, the obligation-derived label must follow the move.
	setGoatPartition(t, ctx, pool, goatMovedOld, shedNumeric, "2")

	rowsAfterMove, err := repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, due.Add(24*time.Hour), 100)
	if err != nil {
		t.Fatalf("list unbatched due after move: %v", err)
	}
	var afterMove *domain.UnbatchedDue
	for i := range rowsAfterMove {
		if rowsAfterMove[i].ObligationID == obMoved {
			afterMove = &rowsAfterMove[i]
		}
	}
	if afterMove == nil {
		t.Fatalf("obligation %s missing after move", obMoved)
	}
	wantAfterMove := oploc.OperationalLocation{ShedName: "Castro", PartitionLabel: "2"}.Display()
	gotAfterMove := oploc.OperationalLocation{ShedName: afterMove.ShedName, PartitionLabel: afterMove.PartitionLabel}.Display()
	if gotAfterMove != wantAfterMove {
		t.Fatalf("after partition move: composed from SQL shed_name=%q partition_label=%q gives %q, want %q (current partition, not the stale 'Castro 1')", afterMove.ShedName, afterMove.PartitionLabel, gotAfterMove, wantAfterMove)
	}
}

func seedShedScopedGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, shed_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, $5)`,
		goatID, tenantID, meshaParty, shedID, cbePark); err != nil {
		t.Fatalf("seed shed-scoped goat %s: %v", goatID, err)
	}
}

func setGoatPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID, partitionLabel string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, $4, $4)
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		tenantID, goatID, shedID, partitionLabel); err != nil {
		t.Fatalf("set goat partition for %s: %v", goatID, err)
	}
}

func insertPartitionParityObligation(t *testing.T, ctx context.Context, repo *Repository, versionID, ruleID, goatID, shedID string, due time.Time, idempotencyKey string) string {
	t.Helper()
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: due, Status: "scheduled", IdempotencyKey: idempotencyKey, Sequence: 1,
	})
	if err != nil || !applied || id == "" {
		t.Fatalf("insert obligation for goat %s: id=%q applied=%v err=%v", goatID, id, applied, err)
	}
	return id
}
