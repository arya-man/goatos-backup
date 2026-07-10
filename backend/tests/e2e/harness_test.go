package e2e

// Run the whole kernel-story suite with:
//
//	go test ./backend/tests/e2e/... -run TestKernelStor -v
//
// or via the helper script `backend/tests/e2e/run.sh` (same command, plus it prints the HTML
// report path on success). Each story boots its own throwaway Postgres container via
// backend/internal/platform/pgtest (Docker required; tests call pgtest.SkipIfNoDocker and skip
// cleanly when Docker is unavailable) and renders its outcome into
// backend/tests/e2e/report/index.html via TestMain in main_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	pipg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	proofpg "github.com/vgoats/goatos/backend/internal/proof/adapters/postgres"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
)

// Baseline fixture ids: every migrated database already has these rows (see migration
// 000001_phase_1_identity_foundation.sql), the same way the rest of the backend's integration
// tests reuse them (e.g. internal/obligation/adapters/postgres/cancel_integration_test.go's
// tenantID/meshaParty/cbePark constants). Reusing them means the harness never has to create a
// tenant/party/park from scratch.
const (
	fxTenant = "00000000-0000-4000-8000-000000000001" // baseline tenant
	fxParty  = "00000000-0000-4000-8000-000000001001" // baseline custodian party (Mesha)
	fxPark   = "00000000-0000-4000-8000-000000003001" // baseline park location (CBE, Coimbatore)
)

// Fixture bundles an ephemeral pool with the same repository/service constructors the rest of the
// backend's integration tests use, plus small seed helpers shared by all three kernel stories.
type Fixture struct {
	T    *testing.T
	Ctx  context.Context
	Pool *pgxpool.Pool

	Proto    *protopg.Repository
	Obl      *oblpg.Repository
	Vacc     *vaccpg.Repository
	VaccExec *vaccexecpg.Repository
	Proof    *proofpg.Repository
	PI       *pipg.Repository
	Inv      *invapp.Service
}

// NewFixture boots a fresh throwaway Postgres container (all committed migrations applied) and
// wires the repositories/services each story needs. The container is removed via t.Cleanup.
func NewFixture(t *testing.T) *Fixture {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)

	const timeout = 5 * time.Second
	return &Fixture{
		T:    t,
		Ctx:  ctx,
		Pool: pool,

		Proto:    protopg.NewRepository(pool, timeout),
		Obl:      oblpg.NewRepository(pool, timeout),
		Vacc:     vaccpg.NewRepository(pool, timeout),
		VaccExec: vaccexecpg.NewRepository(pool, timeout),
		Proof:    proofpg.NewRepository(pool, timeout),
		PI:       pipg.NewRepository(pool, timeout),
		Inv:      invapp.NewService(invpg.NewRepository(pool, timeout)),
	}
}

func (f *Fixture) exec(label, sql string, args ...any) {
	f.T.Helper()
	if _, err := f.Pool.Exec(f.Ctx, sql, args...); err != nil {
		f.T.Fatalf("seed %s: %v", label, err)
	}
}

func (f *Fixture) scanText(sql string, args ...any) string {
	f.T.Helper()
	var v string
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&v); err != nil {
		f.T.Fatalf("scan text (%s): %v", sql, err)
	}
	return v
}

func (f *Fixture) countRows(sql string, args ...any) int {
	f.T.Helper()
	var n int
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&n); err != nil {
		f.T.Fatalf("count rows (%s): %v", sql, err)
	}
	return n
}

func (f *Fixture) scanTime(sql string, args ...any) time.Time {
	f.T.Helper()
	var v time.Time
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&v); err != nil {
		f.T.Fatalf("scan time (%s): %v", sql, err)
	}
	return v
}

// SeedShed creates a shed location under the baseline CBE park plus the animal-stage lookup, shed
// profile, and operational attributes (usable for vaccination, not ICU/quarantine) that the
// vaccination-execution and process-integrity read models need to resolve a park/shed row --
// mirroring internal/processintegrity/adapters/postgres/repository_integration_test.go's
// seedProcessIntegrityProjection and internal/vaccinationexecution/adapters/postgres/
// repository_integration_test.go's seedVaccinationExecutionProjection.
func (f *Fixture) SeedShed(shedID, shedCode, stageID string) {
	f.T.Helper()
	f.exec("shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')`,
		shedID, fxTenant, shedCode, fxPark)
	f.exec("animal stage lookup",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1', 'K1 kids', 'active')
		 ON CONFLICT (animal_stage_id) DO NOTHING`,
		stageID, fxTenant)
	f.exec("shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		shedID, fxTenant, stageID)
	f.exec("shed operational attributes",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedID)
}

// SeedWorkforce seeds one operator (at the shed), one park head, and one verifier (at the park) --
// the minimal workforce roster the process-integrity/execution read models join against for
// owner/verifier display and the "owner_missing" work-state gate.
func (f *Fixture) SeedWorkforce(operatorID, parkHeadID, verifierID, shedID string) {
	f.T.Helper()
	f.exec("operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-E2E', 'E2E Operator', 'active', 'operator', $3)`,
		operatorID, fxTenant, shedID)
	f.exec("park head",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'PH-E2E', 'E2E Park Head', 'active', 'park_head', $3)`,
		parkHeadID, fxTenant, fxPark)
	f.exec("verifier",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'VER-E2E', 'E2E Verifier', 'active', 'verifier', $3)`,
		verifierID, fxTenant, fxPark)
}

// GoatSpec is the minimal, realistic goat fixture every story needs. Zero-value fields fall back
// to sensible defaults (alive/healthy/K1/goat).
type GoatSpec struct {
	GoatID             string
	ShedID             string // optional; when set, current_location_id/shed_id both point here
	Lifecycle          string // default "alive"
	Health             string // default "healthy"
	Stage              string // default "K1"
	Species            string // default "goat"
	ReproductiveStatus string
	BreedingDate       *time.Time
	OriginType         string
	EntryDate          *time.Time
	DOB                *time.Time
	NoDOB              bool // when true, dob is stored NULL (missing-DOB defer stories)
	NoEntryDate        bool // when true, entry_date is NULL (missing-entry-date defer stories; use procured origin)
}

// SeedGoat inserts one goat row directly (goats are owned by the identity module, exactly like the
// rest of the backend's integration tests seed them -- see e.g. generation_integration_test.go's
// seedGenGoat). current_location_id follows the shed when present, else the park.
func (f *Fixture) SeedGoat(spec GoatSpec) {
	f.T.Helper()
	lifecycle := spec.Lifecycle
	if lifecycle == "" {
		lifecycle = "alive"
	}
	health := spec.Health
	if health == "" {
		health = "healthy"
	}
	stage := spec.Stage
	if stage == "" {
		stage = "K1"
	}
	species := spec.Species
	if species == "" {
		species = "goat"
	}
	var shedID *string
	if spec.ShedID != "" {
		shedID = &spec.ShedID
	}
	var repro *string
	if spec.ReproductiveStatus != "" {
		repro = &spec.ReproductiveStatus
	}
	if spec.NoDOB {
		f.exec("goat "+spec.GoatID,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
			    current_location_id, park_id, shed_id, management_stage, dob, reproductive_status, breeding_date, origin_type, entry_date)
			 VALUES ($1, $2, $3, $4, $5, $6, 'female', COALESCE($7::uuid, $8::uuid), $8, $7, $9, NULL, $10, $11::date, $12, $13::date)`,
			spec.GoatID, fxTenant, lifecycle, health, species, fxParty, shedID, fxPark, stage,
			repro, spec.BreedingDate, nullIfEmpty(spec.OriginType), spec.EntryDate)
		return
	}
	if spec.NoEntryDate {
		origin := spec.OriginType
		if origin == "" {
			origin = "procured"
		}
		f.exec("goat "+spec.GoatID,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
			    current_location_id, park_id, shed_id, management_stage, dob, reproductive_status, breeding_date, origin_type, entry_date)
			 VALUES ($1, $2, $3, $4, $5, $6, 'female', COALESCE($7::uuid, $8::uuid), $8, $7, $9, $10::date, $11, $12::date, $13, NULL)`,
			spec.GoatID, fxTenant, lifecycle, health, species, fxParty, shedID, fxPark, stage, spec.DOB,
			repro, spec.BreedingDate, origin)
		return
	}
	f.exec("goat "+spec.GoatID,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, reproductive_status, breeding_date, origin_type, entry_date)
		 VALUES ($1, $2, $3, $4, $5, $6, 'female', COALESCE($7::uuid, $8::uuid), $8, $7, $9, $10::date, $11, $12::date, $13, $14::date)`,
		spec.GoatID, fxTenant, lifecycle, health, species, fxParty, shedID, fxPark, stage, spec.DOB,
		repro, spec.BreedingDate, nullIfEmpty(spec.OriginType), spec.EntryDate)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SeedICUShed creates a shed marked ICU (location unusable for vaccination until the goat leaves).
func (f *Fixture) SeedICUShed(shedID, shedCode, stageID string) {
	f.T.Helper()
	f.SeedShed(shedID, shedCode, stageID)
	f.exec("icu shed attributes",
		`UPDATE location_operational_attributes
		    SET is_icu = true, usable_for_vaccination = false
		  WHERE tenant_id = $1 AND location_id = $2`,
		fxTenant, shedID)
}

// PublishSimpleProtocol creates and publishes a one-rule vaccination protocol version (a single
// birth-age primary dose), mirroring the rule shape used throughout
// internal/vaccination/adapters/postgres/generation_integration_test.go. When deferStates is
// non-empty it is threaded into the rule_dsl's top-level eligibility.defer_states, exactly like
// TestGoatRecheckDefersExistingScheduledObligation, so a goat whose health_status/lifecycle_status
// matches one of those states gets its obligation deferred (and reopened on recovery) by the real
// generation service.
func (f *Fixture) PublishSimpleProtocol(code string, offsetDays, dueWindowDays int32, deferStates []string) (versionID, ruleID string) {
	f.T.Helper()
	protoID, err := f.Proto.CreateDefinition(f.Ctx, protodomain.NewDefinition{
		TenantID: fxTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		f.T.Fatalf("create protocol definition %s: %v", code, err)
	}

	ruleDSL := []byte(`{}`)
	if len(deferStates) > 0 {
		states, mErr := json.Marshal(deferStates)
		if mErr != nil {
			f.T.Fatalf("marshal defer states: %v", mErr)
		}
		ruleDSL = []byte(fmt.Sprintf(`{"eligibility":{"defer_states":%s}}`, states))
	}
	versionID, err = f.Proto.CreateVersion(f.Ctx, protodomain.NewVersion{
		TenantID: fxTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create protocol version %s: %v", code, err)
	}
	ruleID, err = f.Proto.CreateRule(f.Ctx, protodomain.NewRule{
		TenantID: fxTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: offsetDays, DueWindowDays: dueWindowDays,
		Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create protocol rule %s: %v", code, err)
	}
	if err := f.Proto.PublishVersion(f.Ctx, fxTenant, versionID, nil); err != nil {
		f.T.Fatalf("publish protocol version %s: %v", code, err)
	}
	return versionID, ruleID
}

// SeedAcceptedCompletion inserts a completed obligation plus an accepted+verified vaccination
// completion row, mirroring generation_integration_test.go's seedGoatOSCompletion helper. Used by
// cross-vaccine-gap and trusted-history suppression stories.
func (f *Fixture) SeedAcceptedCompletion(versionID, ruleID, goatID, idempotencySuffix string, administered time.Time) {
	f.T.Helper()
	verifiedAt := administered.Add(2 * time.Hour)
	obID, applied, err := f.Obl.InsertObligation(f.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: fxTenant,
		DueAt: administered.Add(48 * time.Hour), Status: "completed",
		IdempotencyKey: "prior:" + idempotencySuffix, Sequence: 1,
	})
	if err != nil || !applied {
		f.T.Fatalf("seed prior obligation %s: applied=%v err=%v", idempotencySuffix, applied, err)
	}
	f.exec("accepted completion "+idempotencySuffix, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, doses, administered_at, status, verified_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, $4::timestamptz, 'accepted', $5::timestamptz, $6)`,
		fxTenant, obID, goatID, administered, verifiedAt, "compl:"+idempotencySuffix)
}

// ---- Story narration / assertion recorder ----

// Story records one kernel story's narrative steps and assertions as a test runs, both driving
// real Go test pass/fail (via t.Errorf on a failed assertion) and building the StoryResult that
// gets rendered into the HTML report on Finish.
type Story struct {
	t      *testing.T
	result StoryResult
}

// NewStory starts recording a new story. Call Finish (typically via defer) to file it into the
// shared report.
func NewStory(t *testing.T, id, title, narrative string) *Story {
	t.Helper()
	return &Story{t: t, result: StoryResult{ID: id, Title: title, Narrative: narrative, Pass: true}}
}

// Step opens a new narrated step. Subsequent Assert calls attach to this step until the next Step.
func (s *Story) Step(name, narrative string) {
	s.t.Helper()
	s.result.Steps = append(s.result.Steps, StepResult{Name: name, Narrative: narrative})
}

// Assert records one pass/fail check against the current step (creating a default step if Step
// was never called) and fails the Go test (non-fatally, via t.Errorf) when cond is false so the
// suite still runs to completion and the report shows every check, not just the first failure.
func (s *Story) Assert(description string, cond bool, detailFormat string, args ...any) bool {
	s.t.Helper()
	if len(s.result.Steps) == 0 {
		s.result.Steps = append(s.result.Steps, StepResult{Name: "Result"})
	}
	detail := fmt.Sprintf(detailFormat, args...)
	idx := len(s.result.Steps) - 1
	s.result.Steps[idx].Assertions = append(s.result.Steps[idx].Assertions, AssertionResult{
		Description: description, Pass: cond, Detail: detail,
	})
	if !cond {
		s.result.Pass = false
		s.t.Errorf("%s / %s: %s (%s)", s.result.Steps[idx].Name, description, "FAILED", detail)
	}
	return cond
}

// Finish files the story into the shared report. Safe to call even after t.Fatalf elsewhere in the
// same goroutine (it still runs as a deferred call), so a setup failure still yields a partial,
// honest report instead of a silently missing story.
func (s *Story) Finish() {
	s.t.Helper()
	if s.t.Failed() {
		s.result.Pass = false
	}
	globalReport.Add(s.result)
}
