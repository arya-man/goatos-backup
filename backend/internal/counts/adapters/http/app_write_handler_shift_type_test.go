package http

import (
	"encoding/json"
	"net/http"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// Typed-raise tests: THE SHIFT TYPE DECIDES THE TAG (maintainer decisions 2026-08-20,
// docs/features/shifting/shifting-rewrite-tag-rules.md). These drive the REAL raise path -- HTTP
// handler -> catalog + goat facts -> domain rulebook -> stored event + approval payload -- so what
// is asserted is what the park head approves and what the apply transaction will stamp.

const (
	typedGoatB    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa02"
	typedGoatC    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa03"
	typedDestShed = "99999999-9999-4999-8999-999999999901"
)

// typedRepo builds a catalog carrying BOTH the destination pen and the animals' SOURCE pen (the
// spacing rule reads the source pen's authored tag and live head count from the same catalog the
// form renders), plus per-animal facts with stage and sex.
func typedRepo(destConfigured string, destHead int, srcConfigured string, srcHead int, facts map[string]domain.GoatShiftingFact) *fakeShiftingRepo {
	repo := newFakeShiftingRepo()
	vocabulary := []string{
		"K0", "K1", "K2", "K3", "F2", "F2-Male", "F2-Female", "Buck",
		"Mother", "Milking", "M0", "Warmup", "Pregnant", "Non-Pregnant",
		"ICU", "Quarantine", "ICU-Kid", "Flushing",
	}
	repo.destinations = domain.ShiftingDestinationCatalog{
		Parks: []domain.ShiftingDestinationPark{{
			ParkID: testParkID,
			Name:   "Channapatna",
			Sheds: []domain.ShiftingDestinationShed{
				{ShedID: typedDestShed, Name: "Gandhi 2", ConfiguredStage: destConfigured, HeadCount: destHead},
				{ShedID: testSourceShedID, Name: "Gandhi 1", ConfiguredStage: srcConfigured, HeadCount: srcHead},
			},
		}},
		// The picker list stays clinical-stripped; the rulebook reads the complete vocabulary.
		ManagementStages:    []string{"K0", "K1", "K2"},
		AllManagementStages: vocabulary,
	}
	repo.goatFacts = facts
	return repo
}

func typedFact(goatID, stage, sex string) domain.GoatShiftingFact {
	return domain.GoatShiftingFact{
		GoatID: goatID, BreedKey: "sirohi", BreedLabel: "Sirohi",
		StageTag: strPtrTest(stage), Sex: strPtrTest(sex),
		ParkID: strPtrTest(testParkID), ShedID: strPtrTest(testSourceShedID),
	}
}

func typedBody(category string, goatIDs ...string) map[string]any {
	return map[string]any{
		"destination_park_id": testParkID,
		"destination_shed_id": typedDestShed,
		"category":            category,
		"goat_ids":            goatIDs,
	}
}

type typedApprovalPayload struct {
	ManagementStageMode   string `json:"management_stage_mode"`
	TargetManagementStage string `json:"target_management_stage"`
	AdoptPenTag           string `json:"adopt_pen_tag"`
}

func decodeTypedPayload(t *testing.T, approvals *fakeApprovalWorkflow) typedApprovalPayload {
	t.Helper()
	var payload typedApprovalPayload
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &payload); err != nil {
		t.Fatalf("decode approval payload: %v", err)
	}
	return payload
}

// A HEALTH raise into the ICU pen stamps the clinical state -- the one type allowed to. The stored
// snapshot and the approval payload both carry it, so the park head approves the same clinical
// stamp the apply will write (and the apply path lifts identity's clinical refusal from the stored
// category, never from anything the client sent).
func TestTypedRaiseHealthIntoICUPenStampsClinicalState(t *testing.T) {
	repo := typedRepo("ICU", 3, "K1", 1, map[string]domain.GoatShiftingFact{
		testGoatID: typedFact(testGoatID, "K1", "female"),
	})
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	res := post(t, mux, appShiftingEventRoute, "typed-health-icu", typedBody("health", testGoatID))
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	if repo.lastEvent.TargetManagementStage != "ICU" || repo.lastEvent.ManagementStageMode != "destination_stage" {
		t.Fatalf("stored mode=%q target=%q, want destination_stage/ICU",
			repo.lastEvent.ManagementStageMode, repo.lastEvent.TargetManagementStage)
	}
	if payload := decodeTypedPayload(t, approvals); payload.TargetManagementStage != "ICU" {
		t.Fatalf("approval payload target=%q, want ICU", payload.TargetManagementStage)
	}
}

// The health RETURN leg takes the return pen's tag outright -- a K1 animal that was in ICU and
// returns to a K3 pen becomes K3 (maintainer confirmed: no memory of the pre-ICU tag, and the
// forward-only rule does not govern the return).
func TestTypedRaiseHealthReturnTakesReturnPensTag(t *testing.T) {
	repo := typedRepo("K3", 5, "ICU", 1, map[string]domain.GoatShiftingFact{
		testGoatID: typedFact(testGoatID, "ICU", "female"),
	})
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())

	res := post(t, mux, appShiftingEventRoute, "typed-health-return", typedBody("health", testGoatID))
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	if repo.lastEvent.TargetManagementStage != "K3" {
		t.Fatalf("stored target=%q, want K3", repo.lastEvent.TargetManagementStage)
	}
}

// A GROWTH raise into the next rung stamps it; a backward raise is refused at raise time with the
// farm-worded copy, and NOTHING is recorded -- no movement row, no approval request.
func TestTypedRaiseGrowthForwardOnly(t *testing.T) {
	t.Run("forward advances", func(t *testing.T) {
		repo := typedRepo("K2", 8, "K1", 1, map[string]domain.GoatShiftingFact{
			testGoatID: typedFact(testGoatID, "K1", "male"),
		})
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-growth-fwd", typedBody("growth", testGoatID))
		if res.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
		}
		if repo.lastEvent.TargetManagementStage != "K2" {
			t.Fatalf("stored target=%q, want K2", repo.lastEvent.TargetManagementStage)
		}
	})
	t.Run("backward is refused and records nothing", func(t *testing.T) {
		repo := typedRepo("K1", 8, "K3", 1, map[string]domain.GoatShiftingFact{
			testGoatID: typedFact(testGoatID, "K3", "male"),
		})
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-growth-back", typedBody("growth", testGoatID))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", res.Code, res.Body.String())
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &body)
		if body.Code != "growth_not_next_stage" {
			t.Fatalf("code=%q body=%s, want growth_not_next_stage", body.Code, res.Body.String())
		}
		if repo.inserts != 0 || approvals.submits != 0 {
			t.Fatalf("refused raise wrote inserts=%d submits=%d, want 0/0", repo.inserts, approvals.submits)
		}
	})
}

// A typed raise IGNORES the legacy stage_mode toggle: the type decides, so a growth raise sent with
// stage_mode=keep_current still stamps the next rung. (The toggle governs only category-less raises
// from clients predating the rewrite.)
func TestTypedRaiseIgnoresTheLegacyToggle(t *testing.T) {
	repo := typedRepo("K2", 8, "K1", 1, map[string]domain.GoatShiftingFact{
		testGoatID: typedFact(testGoatID, "K1", "male"),
	})
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
	body := typedBody("growth", testGoatID)
	body["stage_mode"] = "keep_current"
	res := post(t, mux, appShiftingEventRoute, "typed-growth-toggle", body)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	if repo.lastEvent.TargetManagementStage != "K2" {
		t.Fatalf("stored target=%q, want K2 -- the type decides, not the toggle", repo.lastEvent.TargetManagementStage)
	}
}

// SPACING moves the WHOLE pen: a raise naming fewer animals than the source pen holds is refused
// ("half-half is not an option"), and a full-pen raise into an EMPTY pen adopts the source tag
// onto the destination -- stored on the event AND the approval payload, so the park head approves
// the pen configuration too.
func TestTypedRaiseSpacingWholePenIntoEmptyPen(t *testing.T) {
	facts := map[string]domain.GoatShiftingFact{
		testGoatID: typedFact(testGoatID, "K2", "male"),
		typedGoatB: typedFact(typedGoatB, "K2", "female"),
		typedGoatC: typedFact(typedGoatC, "K2", "male"),
	}
	t.Run("partial group refused", func(t *testing.T) {
		repo := typedRepo("", 0, "K2", 3, facts)
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-spacing-partial", typedBody("spacing", testGoatID, typedGoatB))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", res.Code, res.Body.String())
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &body)
		if body.Code != "spacing_partial_group" {
			t.Fatalf("code=%q, want spacing_partial_group", body.Code)
		}
	})
	t.Run("whole pen into empty pen adopts the source tag", func(t *testing.T) {
		repo := typedRepo("", 0, "K2", 3, facts)
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		// Explicit impacts: a mixed-sex group cannot be auto-derived into one cohort row (the
		// pre-existing DeriveShiftingImpacts contract), and a mixed-sex pen is exactly the common
		// spacing case -- the phone states the cohort split, as it does today.
		body := typedBody("spacing", testGoatID, typedGoatB, typedGoatC)
		body["impacts"] = []map[string]any{
			{"breed_key": "sirohi", "breed_label": "Sirohi", "head_count": 3},
		}
		res := post(t, mux, appShiftingEventRoute, "typed-spacing-whole", body)
		if res.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
		}
		if repo.lastEvent.ManagementStageMode != "keep_current" || repo.lastEvent.TargetManagementStage != "" {
			t.Fatalf("stored mode=%q target=%q, want keep_current/'' -- spacing never restamps the animals",
				repo.lastEvent.ManagementStageMode, repo.lastEvent.TargetManagementStage)
		}
		if repo.lastEvent.AdoptPenTag != "K2" {
			t.Fatalf("stored adopt_pen_tag=%q, want K2", repo.lastEvent.AdoptPenTag)
		}
		if payload := decodeTypedPayload(t, approvals); payload.AdoptPenTag != "K2" {
			t.Fatalf("approval payload adopt_pen_tag=%q, want K2 -- the park head approves the pen configuration too", payload.AdoptPenTag)
		}
	})
	t.Run("differently tagged destination refused", func(t *testing.T) {
		repo := typedRepo("K3", 4, "K2", 3, facts)
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-spacing-mismatch", typedBody("spacing", testGoatID, typedGoatB, typedGoatC))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", res.Code, res.Body.String())
		}
	})
}

// DELIVERY into the kidding pen (destination tagged with the newborn stage) keeps the mother's own
// tag -- she must never be stamped K0. Into an empty untagged shed (the one-day recovery shed) she
// keeps her tag and the shed adopts it.
func TestTypedRaiseDeliveryNeverStampsNewbornStage(t *testing.T) {
	t.Run("into the kidding pen keeps her tag", func(t *testing.T) {
		repo := typedRepo("K0", 9, "Pregnant", 1, map[string]domain.GoatShiftingFact{
			testGoatID: typedFact(testGoatID, "Pregnant", "female"),
		})
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-delivery-k0", typedBody("delivery", testGoatID))
		if res.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
		}
		if repo.lastEvent.TargetManagementStage != "" || repo.lastEvent.ManagementStageMode != "keep_current" {
			t.Fatalf("stored mode=%q target=%q, want keep_current/'' -- the K0 tag belongs to the kids",
				repo.lastEvent.ManagementStageMode, repo.lastEvent.TargetManagementStage)
		}
	})
	t.Run("into the empty recovery shed adopts the mother's tag", func(t *testing.T) {
		repo := typedRepo("", 0, "Pregnant", 1, map[string]domain.GoatShiftingFact{
			testGoatID: typedFact(testGoatID, "Pregnant", "female"),
		})
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-delivery-recovery", typedBody("delivery", testGoatID))
		if res.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
		}
		if repo.lastEvent.AdoptPenTag != "Pregnant" {
			t.Fatalf("stored adopt_pen_tag=%q, want Pregnant (the mother's tag only)", repo.lastEvent.AdoptPenTag)
		}
	})
}

// BREEDING never changes the tag, whatever the destination carries.
func TestTypedRaiseBreedingKeepsTheTag(t *testing.T) {
	repo := typedRepo("Non-Pregnant", 12, "Buck", 1, map[string]domain.GoatShiftingFact{
		testGoatID: typedFact(testGoatID, "Buck", "male"),
	})
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
	res := post(t, mux, appShiftingEventRoute, "typed-breeding", typedBody("breeding", testGoatID))
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}
	if repo.lastEvent.TargetManagementStage != "" || repo.lastEvent.AdoptPenTag != "" {
		t.Fatalf("stored target=%q adopt=%q, want ''/'' -- a visitor keeps his tag",
			repo.lastEvent.TargetManagementStage, repo.lastEvent.AdoptPenTag)
	}
}

// FLUSHING refuses males and requires an empty or already-flushing destination; into an empty pen
// the animal is stamped Flushing and the pen becomes a flushing pen.
func TestTypedRaiseFlushing(t *testing.T) {
	t.Run("male refused", func(t *testing.T) {
		repo := typedRepo("", 0, "Buck", 1, map[string]domain.GoatShiftingFact{
			testGoatID: typedFact(testGoatID, "Buck", "male"),
		})
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-flushing-male", typedBody("flushing", testGoatID))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", res.Code, res.Body.String())
		}
	})
	t.Run("female into empty pen stamps Flushing and tags the pen", func(t *testing.T) {
		repo := typedRepo("", 0, "Non-Pregnant", 1, map[string]domain.GoatShiftingFact{
			testGoatID: typedFact(testGoatID, "Non-Pregnant", "female"),
		})
		approvals := newFakeApprovalWorkflow()
		mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
		res := post(t, mux, appShiftingEventRoute, "typed-flushing-female", typedBody("flushing", testGoatID))
		if res.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
		}
		if repo.lastEvent.TargetManagementStage != "Flushing" || repo.lastEvent.AdoptPenTag != "Flushing" {
			t.Fatalf("stored target=%q adopt=%q, want Flushing/Flushing",
				repo.lastEvent.TargetManagementStage, repo.lastEvent.AdoptPenTag)
		}
	})
}

// The widened vocabulary is accepted end-to-end and an unknown category still refuses with the
// updated message naming all six types.
func TestTypedRaiseCategoryVocabulary(t *testing.T) {
	repo := typedRepo("", 0, "K2", 1, map[string]domain.GoatShiftingFact{
		testGoatID: typedFact(testGoatID, "K2", "male"),
	})
	approvals := newFakeApprovalWorkflow()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, newFakeGoatValidator())
	res := post(t, mux, appShiftingEventRoute, "typed-vocab-bad", typedBody("warmup", testGoatID))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", res.Code, res.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	if body.Code != "invalid_category" {
		t.Fatalf("code=%q, want invalid_category", body.Code)
	}
}
