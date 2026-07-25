package postgres

import (
	"context"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	tsShedA = "00000000-0000-4000-8000-00000000da01"
	tsShedB = "00000000-0000-4000-8000-00000000da02"
)

// TestParkConsolidationKeepsPerGoatVisitShotCapAtMemberGrain is the CPT 08-08
// guard for the hard medical cap: a goat with three distinct-priority vaccines
// due in the same window must not carry more than two obligation rules on one
// planned drive date.
//
// Important grain note: vaccination_drive_assignments.vaccine_rule_ids is an
// assignment-row vaccine lane/union, not per-goat truth. The invariant must be
// proved through vaccination_drive_assignment_members.obligation_id ->
// obligation_instances.rule_id; otherwise one mixed assignment row can make each
// member goat look like it received every vaccine in the row.
func TestParkConsolidationKeepsPerGoatVisitShotCapAtMemberGrain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, tsShedA, "RULEID-TRIM-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, tsShedB, "RULEID-TRIM-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.park.threeshot.trim", Name: "ThreeShotTrim",
		Category: "vaccination", Status: "draft",
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
	mkRule := func(dose string, seq int32, vaccine string) string {
		id, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: dose, Sequence: seq,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 30,
			EligibilityJSON: []byte(`{"vaccine":{"code":"` + vaccine + `"}}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", vaccine, err)
		}
		return id
	}
	rulePPR := mkRule("primary", 1, "ppr")
	ruleBT := mkRule("booster_1", 2, "blue tongue")
	ruleFMD := mkRule("booster_2", 3, "fmd")

	const goat1 = "10000000-0000-4000-8000-00000000da01"
	const goat2 = "10000000-0000-4000-8000-00000000da02"
	seedReserveGoats(t, ctx, pool, tsShedA, cbePark, goat1)
	seedReserveGoats(t, ctx, pool, tsShedB, cbePark, goat2)

	due := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	windowEnd := due.Add(30 * 24 * time.Hour)
	insert := func(ruleID, goatID, shedID, key string) {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
	}
	// goat1: THREE distinct-priority vaccines due the same day -- the over-cap composition.
	insert(rulePPR, goat1, tsShedA, "trim-goat1-ppr")
	insert(ruleBT, goat1, tsShedA, "trim-goat1-bt")
	insert(ruleFMD, goat1, tsShedA, "trim-goat1-fmd")
	// goat2 (second shed) needs one vaccine so the group crosses 2 sheds for park consolidation.
	insert(rulePPR, goat2, tsShedB, "trim-goat2-ppr")

	cfg := oblapp.SweepConfig{
		DrivePlanner:      domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
		RuleVaccineIDs: map[string]oblapp.RuleVaccineIdentity{
			rulePPR: {VaccineCode: "ppr", VaccinePriority: oblapp.VaccineMatrixPriority("ppr")},
			ruleBT:  {VaccineCode: "blue tongue", VaccinePriority: oblapp.VaccineMatrixPriority("blue tongue")},
			ruleFMD: {VaccineCode: "fmd", VaccinePriority: oblapp.VaccineMatrixPriority("fmd")},
		},
	}

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, due.Add(3*24*time.Hour)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Hard invariant: no goat may carry more than 2 distinct vaccine shots on any single planned
	// drive date across ALL planned batches, counted at exact obligation-member grain.
	rows, err := pool.Query(ctx, `
		SELECT m.goat_id, vda.planned_date, COUNT(DISTINCT oi.rule_id) AS shots
		FROM vaccination_drive_assignments vda
		JOIN obligation_batches b
		  ON b.tenant_id = vda.tenant_id
		 AND b.batch_id = vda.batch_id
		 AND b.status = 'planned'
		JOIN vaccination_drive_assignment_members m
		  ON m.tenant_id = vda.tenant_id
		 AND m.assignment_id = vda.assignment_id
		JOIN obligation_instances oi
		  ON oi.tenant_id = m.tenant_id
		 AND oi.obligation_id = m.obligation_id
		WHERE vda.tenant_id = $1
		GROUP BY m.goat_id, vda.planned_date
		ORDER BY shots DESC`, tenantID)
	if err != nil {
		t.Fatalf("query shots: %v", err)
	}
	defer rows.Close()
	worst := int32(0)
	var worstGoat string
	var worstDate time.Time
	for rows.Next() {
		var goatID string
		var d time.Time
		var shots int32
		if err := rows.Scan(&goatID, &d, &shots); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if shots > worst {
			worst = shots
			worstGoat = goatID
			worstDate = d
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if worst > 2 {
		t.Fatalf("goat %s carries %d shots on %s -- exceeds max 2 shots/animal/visit (third shot must be deferred, not clubbed)",
			worstGoat, worst, worstDate.Format("2006-01-02"))
	}
}
