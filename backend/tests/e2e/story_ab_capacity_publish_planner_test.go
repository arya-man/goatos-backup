package e2e

import (
	"fmt"
	"testing"
	"time"

	protoapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// matrixRuleDSLWithCapacity is the real Preventive Care vaccination.matrix ruleset (ruleset_family
// "vaccination.matrix" + matrix_rows) — the shape the admin publishes — with an appended versioned
// rule_dsl.capacity block. The capacity block is the AUTHORING source of truth; publishing through the
// protocol Service syncs it into the vaccination_capacity_config operational read model. Kept in this
// file (not shared with the unit-test package helpers) so the kernel suite stays self-contained.
func matrixRuleDSLWithCapacity(maxPerDay, bufferDays int) string {
	const base = `{"category":"vaccination","ruleset_family":"vaccination.matrix","vaccine":{"code":"vaccination.matrix","name":"Preventive Care vaccination matrix","type":"matrix"},"eligibility":{"animal_stage":"all","species":["goat","sheep"],"sex":["female","male"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"exclude_reproductive_states":["pregnant_late"],"defer_states":["sick","under_treatment","icu","quarantine"]},"missed_dose_policy":"immediate","compatibility_policy":{"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14,"live_to_live_gap_days":28,"kid_booster_min_gap_days":21,"bacterial_viral_same_day_allowed":true,"live_killed_viral_same_day_allowed":true,"max_vaccines_per_combo_session":2},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_prior_vaccination_allowed":true,"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox"],"sheep_second_wave":["Sheep Pox"]},"matrix_rows":[{"row_id":"adult-sheep-second-wave","vaccine":{"code":"SHEEP_POX","name":"Sheep Pox","type":"live","pathogen_class":"viral","compatibility_group":"POX","course_type":"single"},"eligibility":{"species":["sheep"],"animal_stage":["DOE","MOTHER","BUCK"],"sex":["female","male"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"exclude_reproductive_states":["pregnant_late"],"defer_states":["sick","under_treatment","icu","quarantine"]},"schedule":[{"dose_code":"sheep_pox_adult_second_wave","source_dose_code":"sheep_pox_adult_second_wave","sequence":1,"trigger_type":"post_arrival","offset_days":28,"due_window_days":7,"dose_amount":1,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"preventive_care_review","repeat":"yearly","catch_up":"immediate"}]}],"schedule":[{"dose_code":"sheep_pox_adult_second_wave","source_dose_code":"sheep_pox_adult_second_wave","sequence":1,"trigger_type":"post_arrival","offset_days":28,"due_window_days":7,"dose_amount":1,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"preventive_care_review","repeat":"yearly","catch_up":"immediate"}]}`
	// Append the versioned capacity block just inside the closing brace (mirror of the unit-test helper
	// vaccinationRuleDSLWithCapacity). overflow_policy is the only accepted split policy today.
	block := fmt.Sprintf(
		`,"capacity":{"max_per_day":%d,"max_buffer_days":%d,"capacity_scope":"tenant","overflow_policy":"split_within_safe_window_then_mark_needs_review"}}`,
		maxPerDay, bufferDays,
	)
	return base[:len(base)-1] + block
}

// TestKernelStoryAB_CapacityPublishPlannerParity proves the REAL product publish path for daily
// vaccination capacity end to end, over a live Postgres:
//
//	rule_dsl.capacity  --(Service.PublishVersion, vaccination.matrix)-->  vaccination_capacity_config
//	                   --(VaccExec.CapacityConfig read)-->  PlanSessions (split / within-cap / needs-review)
//
// The existing kernel stories publish through f.Proto.PublishVersion (the *protopg.Repository method),
// which bypasses the Service entirely and never touches capacity — they are capacity-agnostic by
// construction. This story is the one that drives the Service so the publish-time capacity sync + the
// post-publish parity check actually run, then feeds the SYNCED config into the planner to prove the
// classification matches the published rule (max_buffer_days = 7 => an 8-day safe window).
func TestKernelStoryAB_CapacityPublishPlannerParity(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ab", "Publishing vaccination.matrix syncs daily capacity into the planner",
		"The admin authors a daily vaccination cap inside the versioned rule (rule_dsl.capacity) and "+
			"publishes the vaccination.matrix through the protocol Service. Publish must persist that cap "+
			"into the vaccination_capacity_config read model (max_buffer_days = 7), and the session-splitting "+
			"planner must read the SYNCED cap — a shed within the cap runs in one day, an over-cap load splits "+
			"across the safe window, and a load past the window is flagged Needs review.")
	defer story.Finish()
	story.Certify("backend kernel")

	protoSvc := protoapp.NewService(fx.Proto)

	// Pre-state: force the operational read model OFF the target so a post-publish match proves the sync
	// wrote (not that the row happened to already hold the published values). Migration 000155 seeds the
	// baseline tenant and 000157 bumps buffer to 7, so without this the buffer would already read 7.
	story.Step("Seed a deliberately-wrong pre-publish capacity",
		"Set the tenant cap to 100/day with a 2-day buffer. Neither value matches what the version will "+
			"publish, so the post-publish read is unambiguous evidence the publish overwrote it.")
	fx.exec("wrong pre-publish capacity",
		`UPDATE vaccination_capacity_config
		   SET max_per_day = 100, max_buffer_days = 2, capacity_scope = 'tenant',
		       overflow_policy = 'split_within_safe_window_then_mark_needs_review',
		       row_version = row_version + 1, updated_at = now()
		 WHERE tenant_id = $1`, fxTenant)
	preMaxPerDay := fx.countRows(`SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id=$1`, fxTenant)
	preBuffer := fx.countRows(`SELECT max_buffer_days FROM vaccination_capacity_config WHERE tenant_id=$1`, fxTenant)
	story.Assert("pre-publish cap is the wrong 100/2", preMaxPerDay == 100 && preBuffer == 2,
		"max_per_day=%d max_buffer_days=%d", preMaxPerDay, preBuffer)

	// Author a draft vaccination.matrix version carrying capacity{max_per_day:10, max_buffer_days:7}. A cap
	// of 10 (distinct from the seeded 100) makes the small cell counts below straddle the window cleanly.
	protoID, err := fx.Proto.CreateDefinition(fx.Ctx, protodomain.NewDefinition{
		TenantID: fxTenant, Code: "vaccination.e2e.story_ab", Name: "E2E Story AB capacity matrix",
		Category: "vaccination", Status: "draft",
	})
	story.Assert("protocol definition created", err == nil, "err=%v", err)

	// The vaccination execution contract requires a real SOP version (composite FK) and a proof policy
	// with recognized content. Migration 000075 seeds this SOP version for the baseline tenant.
	sopVersionID := "b0000000-0000-4000-8000-000000000002"
	versionID, err := fx.Proto.CreateVersion(fx.Ctx, protodomain.NewVersion{
		TenantID: fxTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(matrixRuleDSLWithCapacity(10, 7)),
		ProofPolicy:   []byte(`{"required_proofs":["administration_video"]}`),
		SopVersionID:  &sopVersionID,
	})
	story.Assert("draft matrix version created", err == nil, "err=%v", err)

	story.Step("Publish the vaccination.matrix through the protocol Service",
		"Service.PublishVersion recognizes the vaccination.matrix ruleset, writes the derived rules, then "+
			"syncs rule_dsl.capacity into vaccination_capacity_config and parity-checks the stored row.")
	publishErr := protoSvc.PublishVersion(fx.Ctx, fxTenant, versionID, nil)
	story.Assert("Service publish succeeded (sync + parity check passed)", publishErr == nil, "err=%v", publishErr)

	// The stored read model must now hold the AUTHORED capacity, overwriting the wrong pre-state.
	gotMaxPerDay := fx.countRows(`SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id=$1`, fxTenant)
	gotBuffer := fx.countRows(`SELECT max_buffer_days FROM vaccination_capacity_config WHERE tenant_id=$1`, fxTenant)
	gotScope := fx.scanText(`SELECT capacity_scope FROM vaccination_capacity_config WHERE tenant_id=$1`, fxTenant)
	story.Assert("publish persisted max_per_day = 10", gotMaxPerDay == 10, "max_per_day=%d", gotMaxPerDay)
	story.Assert("publish persisted max_buffer_days = 7", gotBuffer == 7, "max_buffer_days=%d", gotBuffer)
	story.Assert("publish persisted capacity_scope = tenant", gotScope == "tenant", "scope=%q", gotScope)

	story.Step("Planner reads the SYNCED capacity",
		"The session-splitting planner reads vaccination_capacity_config through the vaccination-execution "+
			"read path — the same values publish just wrote, not a code default.")
	cfg, err := fx.VaccExec.CapacityConfig(fx.Ctx, fxTenant)
	story.Assert("capacity read for planner succeeded", err == nil, "err=%v", err)
	story.Assert("planner sees synced cap 10/day", cfg.MaxPerDay == 10, "MaxPerDay=%d", cfg.MaxPerDay)
	story.Assert("planner sees synced buffer 7 (8-day window)", cfg.MaxBufferDays == 7, "MaxBufferDays=%d", cfg.MaxBufferDays)

	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	// within_cap: 10 cells at cap 10 = 1 session (fits in a single day).
	story.Step("Within cap → one day",
		"A shed with 10 open vaccination cells fits in a single day at the 10/day cap: within_cap.")
	nWithin, statusWithin, _, errWithin := vaccexecapp.PlanSessions(10, cfg, start)
	story.Assert("within-cap planner returns no error", errWithin == nil, "err=%v", errWithin)
	story.Assert("within-cap load is one session", nWithin == 1, "sessions=%d", nWithin)
	story.Assert("within-cap classified within_cap", statusWithin == vaccexecdomain.CapacityWithinCap, "status=%q", statusWithin)

	// over_cap (split): 80 cells at cap 10 = 8 sessions == exactly the 8-day safe window (buffer 7 + 1).
	story.Step("Over cap but inside the window → split",
		"80 cells split into 8 daily sessions — exactly the safe window for buffer 7 (7 + 1). This boundary "+
			"is bound to the PUBLISHED buffer: a smaller buffer would push this same load to Needs review.")
	nSplit, statusSplit, sessionsSplit, errSplit := vaccexecapp.PlanSessions(80, cfg, start)
	story.Assert("over-cap planner returns no error", errSplit == nil, "err=%v", errSplit)
	story.Assert("over-cap load splits into 8 sessions", nSplit == 8, "sessions=%d", nSplit)
	story.Assert("over-cap classified over_cap (Split)", statusSplit == vaccexecdomain.CapacityOverCap, "status=%q", statusSplit)
	story.Assert("every split day stays within the safe window", allWithinWindow(sessionsSplit),
		"sessions=%+v", sessionsSplit)

	// capacity_breach (needs review): 90 cells at cap 10 = 9 sessions > 8-day window.
	story.Step("Past the window → Needs review",
		"90 cells need 9 daily sessions — one past the 8-day safe window — so the planner flags capacity_breach "+
			"(Needs review). Same cap, one dose more than Split: the window edge is exactly buffer 7.")
	nBreach, statusBreach, _, errBreach := vaccexecapp.PlanSessions(90, cfg, start)
	story.Assert("breach planner returns no error", errBreach == nil, "err=%v", errBreach)
	story.Assert("past-window load needs 9 sessions", nBreach == 9, "sessions=%d", nBreach)
	story.Assert("past-window classified capacity_breach (Needs review)", statusBreach == vaccexecdomain.CapacityBreach, "status=%q", statusBreach)
}

// allWithinWindow reports whether every planned session sits inside the safe window (no per-day breach).
// A split load must keep all its days within_cap — capacity_breach is a shed-level headline, never a
// per-session state for a load that fits the window.
func allWithinWindow(sessions []vaccexecdomain.PlannedSession) bool {
	for _, s := range sessions {
		if s.Capacity != vaccexecdomain.CapacityWithinCap {
			return false
		}
	}
	return true
}
