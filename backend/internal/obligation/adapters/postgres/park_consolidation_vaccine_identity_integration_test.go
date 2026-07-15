package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	riShedA = "00000000-0000-4000-8000-00000000d901"
	riShedB = "00000000-0000-4000-8000-00000000d902"
)

// TestParkConsolidationTieUsesRealPerRuleVaccineIdentityNotWrapper is the R2-05(b) guard: creates
// one protocol version with two REAL matrix rules and seeds two goats -- goat1 (alone in riShedA)
// due for BOTH rules on the same day, and goat2 (alone in riShedB) due for ruleFMD only -- across
// the same park (cbePark). Both sheds are single-goat, so layer 1 (shed batching) defers every
// group to the park-consolidation pass (MinShedDriveTargets=2 default); the whole 3-row set then
// satisfies MinParkMergeTargets(2)/MinParkMergeSheds(2) and merges into ONE park-consolidation
// candidate group, where goat1's two same-target, same-date obligations (ruleFMD vs ruleHS) are
// the ones actually competing for the MaxShotsPerAnimalPerDrive=1 cap.
func TestParkConsolidationTieUsesRealPerRuleVaccineIdentityNotWrapper(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedParkConsolidationShed(t, ctx, pool, riShedA, "RULEID-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, riShedB, "RULEID-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.park.ruleidentity.tie", Name: "ParkTieRealIdentity",
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
	ruleFMD, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"fmd"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule fmd: %v", err)
	}
	ruleHS, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster_1", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"hs"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule hs: %v", err)
	}

	const goat1 = "10000000-0000-4000-8000-00000000d901"
	const goat2 = "10000000-0000-4000-8000-00000000d902"
	seedReserveGoats(t, ctx, pool, riShedA, cbePark, goat1)
	seedReserveGoats(t, ctx, pool, riShedB, cbePark, goat2)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := due.Add(7 * 24 * time.Hour)
	insert := func(ruleID, goatID, shedID, key string) {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
	}
	// goat1 is due for BOTH vaccines on the same day -- these two rows are the ones that actually
	// compete for the single shot-cap slot.
	insert(ruleFMD, goat1, riShedA, "park-tie-goat1-fmd")
	insert(ruleHS, goat1, riShedA, "park-tie-goat1-hs")
	// goat2 (a different shed) only needs ruleFMD; it exists purely so the group crosses 2 sheds
	// and satisfies MinParkMergeSheds/MinParkMergeTargets -- it never competes for goat1's cap.
	insert(ruleFMD, goat2, riShedB, "park-tie-goat2-fmd")

	// Real per-rule identities (as buildSweepConfig would populate from eligibility_json): "fmd" and
	// "hs" resolve to the SAME VaccineMatrixPriority (5) but are DIFFERENT vaccines -- a genuine
	// same-priority tie. cfg.VaccineCode is left BLANK deliberately: before the R2-05(b) fix, park
	// consolidation used this single wrapper code/priority for every candidate regardless of its
	// real rule, so goat1's two rows would have compared as "the same vaccine" (identical blank
	// code) and the second would have been silently dropped as an ordinary self-overflow -- no tie
	// ever raised, and the drop order would depend on arrival order. With the fix, each row resolves
	// its OWN rule's real identity and the genuine tie surfaces.
	cfg := oblapp.SweepConfig{
		DrivePlanner:      domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
		RuleVaccineIDs: map[string]oblapp.RuleVaccineIdentity{
			ruleFMD: {VaccineCode: "fmd", VaccinePriority: oblapp.VaccineMatrixPriority("fmd")},
			ruleHS:  {VaccineCode: "hs", VaccinePriority: oblapp.VaccineMatrixPriority("hs")},
		},
	}

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	_, err = sweep.SweepVersion(ctx, tenantID, versionID, cfg, due.Add(3*24*time.Hour))

	var tieErr *oblapp.ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError (goat1's fmd/hs doses tie on the shared visit)", err)
	}
	if tieErr.TargetID != goat1 {
		t.Fatalf("tie error target = %q, want %s", tieErr.TargetID, goat1)
	}
	gotVaccines := map[string]bool{tieErr.VaccineA: true, tieErr.VaccineB: true}
	if !gotVaccines["fmd"] || !gotVaccines["hs"] {
		t.Fatalf("tie error vaccines = %q/%q, want the two REAL rule vaccines fmd/hs (not a shared wrapper code)", tieErr.VaccineA, tieErr.VaccineB)
	}
	if tieErr.Priority != 5 {
		t.Fatalf("tie error priority = %d, want 5 (fmd/hs matrix priority)", tieErr.Priority)
	}
}

// TestParkConsolidationLegacyNonMatrixRuleFallsBackToVersionLevelIdentity is the R2-05(a) guard: a
// rule with NO cached RuleVaccineIDs entry (simulating buildSweepConfig's fixed "only cache
// complete identities" contract for a legacy, non-matrix rule) must still resolve to the
// version-level VaccineCode/priority when it competes for a park-consolidation shot-cap slot,
// instead of comparing as a blank-code/zero-priority phantom vaccine. Here the legacy rule's
// fallback identity (version-level VaccineCode="fmd", priority 5) genuinely ties with a real
// matrix rule's vaccine ("hs", also priority 5), proving the fallback is a REAL, correctly
// resolved identity and not an inert placeholder that would (before the fix) either never tie or
// tie against everything.
func TestParkConsolidationLegacyNonMatrixRuleFallsBackToVersionLevelIdentity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const shedA = "00000000-0000-4000-8000-00000000d903"
	const shedB = "00000000-0000-4000-8000-00000000d904"
	seedParkConsolidationShed(t, ctx, pool, shedA, "RULEID-FALLBACK-SHED-A")
	seedParkConsolidationShed(t, ctx, pool, shedB, "RULEID-FALLBACK-SHED-B")
	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.park.ruleidentity.fallback", Name: "ParkTieFallback",
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
	// Legacy, non-matrix rule: no vaccine in eligibility_json. Per the R2-05(a) fix,
	// buildSweepConfig would leave this rule OUT of RuleVaccineIDs entirely -- simulated here by
	// simply never adding an entry for it below.
	ruleLegacy, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule legacy: %v", err)
	}
	ruleHS, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster_1", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"hs"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule hs: %v", err)
	}

	const goat1 = "10000000-0000-4000-8000-00000000d903"
	const goat2 = "10000000-0000-4000-8000-00000000d904"
	seedReserveGoats(t, ctx, pool, shedA, cbePark, goat1)
	seedReserveGoats(t, ctx, pool, shedB, cbePark, goat2)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := due.Add(7 * 24 * time.Hour)
	insert := func(ruleID, goatID, shedID, key string) {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
	}
	insert(ruleLegacy, goat1, shedA, "park-fallback-goat1-legacy")
	insert(ruleHS, goat1, shedA, "park-fallback-goat1-hs")
	insert(ruleLegacy, goat2, shedB, "park-fallback-goat2-legacy")

	// cfg.VaccineCode = "fmd" is the version-level fallback identity the legacy rule must resolve
	// to (priority 5, same as "hs"): RuleVaccineIDs deliberately has NO entry for ruleLegacy.
	cfg := oblapp.SweepConfig{
		VaccineCode:       "fmd",
		DrivePlanner:      domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 1},
		ParkConsolidation: domain.DefaultParkConsolidationSettings(),
		RuleVaccineIDs: map[string]oblapp.RuleVaccineIdentity{
			ruleHS: {VaccineCode: "hs", VaccinePriority: oblapp.VaccineMatrixPriority("hs")},
		},
	}

	sweep := oblapp.NewSweeperService(repo, nil, nil)
	_, err = sweep.SweepVersion(ctx, tenantID, versionID, cfg, due.Add(3*24*time.Hour))

	var tieErr *oblapp.ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError (legacy rule's fallback identity ties with hs)", err)
	}
	if tieErr.TargetID != goat1 {
		t.Fatalf("tie error target = %q, want %s", tieErr.TargetID, goat1)
	}
	gotVaccines := map[string]bool{tieErr.VaccineA: true, tieErr.VaccineB: true}
	if !gotVaccines["fmd"] || !gotVaccines["hs"] {
		t.Fatalf("tie error vaccines = %q/%q, want fmd (legacy rule's version-level fallback) and hs", tieErr.VaccineA, tieErr.VaccineB)
	}
	if tieErr.Priority != 5 {
		t.Fatalf("tie error priority = %d, want 5", tieErr.Priority)
	}
}
