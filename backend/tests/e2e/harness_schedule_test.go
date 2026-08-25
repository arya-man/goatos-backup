package e2e

import (
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// sameDay reports whether two instants fall on the same Goat OS business day (Asia/Kolkata). The
// generation engine stores due_at/window_end as business-day-start instants in the India business
// calendar (see biztime.BusinessDayStart), so schedule assertions must compare in that calendar, not
// raw UTC, or a 05:30 IST offset would make a same-date dose look like the previous day.
func sameDay(a, b time.Time) bool {
	loc := biztime.DefaultLocation()
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

// RuleSpec is one schedule row authored into a multi-rule protocol version by
// PublishScheduleProtocol. It mirrors the fields protodomain.NewRule needs for a
// birth_age or post_arrival SM-1 trigger.
type RuleSpec struct {
	DoseCode        string
	Sequence        int32
	TriggerType     string // "birth_age" | "post_arrival"
	OffsetDays      int32
	DueWindowDays   int32
	MinGapDays      int32
	Repeat          string
	CatchUp         string
	EligibilityJSON string // optional; default `{}`
}

func defaultParkSweepConfig() oblapp.SweepConfig {
	return oblapp.SweepConfig{ParkConsolidation: obldomain.DefaultParkConsolidationSettings()}
}

// PublishScheduleProtocol creates and publishes a vaccination protocol version carrying an
// arbitrary set of schedule rules (the kid dose bundle, the adult course, etc.) plus an optional
// version-level rule_dsl (e.g. a procurement warm-up policy). It reuses the same protocol
// repository entrypoints (CreateDefinition/CreateVersion/CreateRule/PublishVersion) the real
// admin publish flow uses, so the stories drive the genuine generation engine over a real
// multi-row published version -- not a hand-built obligation set.
//
// ruleDSL is the version rule_dsl JSON ("{}" for none). Returns the version id plus a
// dose_code -> rule_id map so a story can join obligations back to the rule that produced them.
func (f *Fixture) PublishScheduleProtocol(code, ruleDSL string, rules []RuleSpec) (versionID string, ruleIDs map[string]string) {
	f.T.Helper()
	protoID, err := f.Proto.CreateDefinition(f.Ctx, protodomain.NewDefinition{
		TenantID: fxTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		f.T.Fatalf("create protocol definition %s: %v", code, err)
	}
	if ruleDSL == "" {
		ruleDSL = "{}"
	}
	versionID, err = f.Proto.CreateVersion(f.Ctx, protodomain.NewVersion{
		TenantID: fxTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(ruleDSL), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create protocol version %s: %v", code, err)
	}
	ruleIDs = make(map[string]string, len(rules))
	for _, r := range rules {
		trigger := r.TriggerType
		if trigger == "" {
			trigger = "birth_age"
		}
		eligibility := r.EligibilityJSON
		if eligibility == "" {
			eligibility = `{}`
		}
		repeat := r.Repeat
		if repeat == "" {
			repeat = "none"
		}
		catchUp := r.CatchUp
		if catchUp == "" {
			catchUp = "pc_approval"
		}
		rid, err := f.Proto.CreateRule(f.Ctx, protodomain.NewRule{
			TenantID: fxTenant, ProtocolVersionID: versionID, DoseCode: r.DoseCode, Sequence: r.Sequence,
			TriggerType: trigger, OffsetDays: r.OffsetDays, DueWindowDays: r.DueWindowDays,
			MinGapDays: r.MinGapDays, Repeat: repeat, CatchUp: catchUp,
			EligibilityJSON: []byte(eligibility), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			f.T.Fatalf("create protocol rule %s/%s: %v", code, r.DoseCode, err)
		}
		ruleIDs[r.DoseCode] = rid
	}
	if err := f.Proto.PublishVersion(f.Ctx, fxTenant, versionID, nil); err != nil {
		f.T.Fatalf("publish protocol version %s: %v", code, err)
	}
	return versionID, ruleIDs
}

// SeedAdultShed creates a shed whose profile carries an ADULT animal-stage code (not the kid 'K1'
// SeedShed hardcodes). The generation engine resolves a goat's stage from its shed profile
// (COALESCE(asl.stage_code, ...) in GetGoatForGeneration), and schedulePathForGoat routes any
// K-prefixed stage to the kid schedule -- so an adult goat must live in an adult-stage shed for the
// adult_procurement path (and its post_arrival rows) to fire. Used by the adult and procurement stories.
func (f *Fixture) SeedAdultShed(shedID, shedCode, stageID, stageCode string) {
	f.T.Helper()
	f.exec("adult shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')`,
		shedID, fxTenant, shedCode, fxPark)
	f.exec("adult stage lookup",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, $3, $3, 'active')`,
		stageID, fxTenant, stageCode)
	f.exec("adult shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		shedID, fxTenant, stageID)
	f.exec("adult shed operational attributes",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedID)
}

// SeedProcurementGoat inserts an adult goat that arrived via procurement: origin_type='procured'
// with a farm entry_date (the warm-up anchor GetGoatForGeneration reads for the post_arrival
// trigger, and the signal that puts the goat on the adult_procurement schedule path). DOB is set
// well in the past so the goat is not on the kid schedule. Used by the adult and procurement stories.
func (f *Fixture) SeedProcurementGoat(goatID, shedID string, entryDate time.Time, stage string, dob time.Time) {
	f.T.Helper()
	if stage == "" {
		stage = "A1"
	}
	var shed *string
	if shedID != "" {
		shed = &shedID
	}
	f.exec("procurement goat "+goatID,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, entry_date, origin_type)
		 VALUES ($1, $2, 'alive', 'healthy', 'goat', $3, 'female', COALESCE($4::uuid, $5::uuid), $5, $4, $6, $7::date, $8::date, 'procured')`,
		goatID, fxTenant, fxParty, shed, fxPark, stage, dob, entryDate)
}

func (f *Fixture) SeedProcurementPurpose(goatID, purpose string, entryDate time.Time) {
	f.T.Helper()
	loadID := "ec000000-0000-4000-8000-" + goatID[len(goatID)-12:]
	f.exec("procurement load "+goatID,
		`INSERT INTO procurement_loads (
				load_id, tenant_id, source_party_id, expected_count, purchase_date, status, idempotency_key
			 ) VALUES (
				$1::uuid, $2, $3, 1, $4::date, 'accepted_intake', $5
			 ) ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		loadID, fxTenant, fxParty, entryDate, "e2e-proc-purpose-"+goatID)
	f.exec("procurement load goat purpose "+goatID,
		`INSERT INTO procurement_load_goats (
			tenant_id, load_id, goat_id, purpose, selection_state, current_state, source_entry_state,
			ownership_state, health_state, warmup_started_at, intake_accepted_at
		 ) VALUES (
			$1, $2::uuid, $3::uuid, $4, 'accepted', 'accepted_herd_intake', 'accepted',
			'mesha_owned', 'passed', $5::timestamptz, $5::timestamptz
		 ) ON CONFLICT (tenant_id, load_id, goat_id) DO UPDATE
		   SET purpose = EXCLUDED.purpose,
		       current_state = EXCLUDED.current_state,
		       intake_accepted_at = EXCLUDED.intake_accepted_at`,
		fxTenant, loadID, goatID, purpose, entryDate)
}
