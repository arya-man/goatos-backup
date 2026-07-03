package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

func TestValidatePublishable(t *testing.T) {
	cases := []struct {
		name    string
		dsl     string
		wantErr bool
	}{
		{
			name:    "empty dsl can proceed to executable checks",
			dsl:     ``,
			wantErr: false,
		},
		{
			name:    "source-less dsl can proceed to executable checks",
			dsl:     `{"category":"vaccination"}`,
			wantErr: false,
		},
		{
			name:    "legacy source metadata is tolerated but ignored by publishability",
			dsl:     `{"source":{"source_system":"manual_admin","source_ref":"x","review_status":"approved","approved_by":"x","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: false,
		},
		{
			name:    "invalid json is not publishable",
			dsl:     `{not json`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePublishable([]byte(tc.dsl))
			if tc.wantErr && err == nil {
				t.Fatalf("expected not-publishable, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected publishable, got %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrNotPublishable) {
				t.Fatalf("expected ErrNotPublishable, got %v", err)
			}
		})
	}
}

func TestValidateExecutionContract(t *testing.T) {
	valid := domain.Version{
		SopVersionID: "62000000-0000-4000-8000-000000000001",
		ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
		RuleDsl:      []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age"}]}`),
	}
	if err := ValidateExecutionContract(valid); err != nil {
		t.Fatalf("valid execution contract rejected: %v", err)
	}

	missingSOP := valid
	missingSOP.SopVersionID = ""
	if err := ValidateExecutionContract(missingSOP); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("missing SOP should be not publishable, got %v", err)
	}

	missingProof := valid
	missingProof.ProofPolicy = []byte(`{}`)
	if err := ValidateExecutionContract(missingProof); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("missing proof policy should be not publishable, got %v", err)
	}

	// {"required_proofs":[]} is a non-empty JSON object but carries zero real proof requirements:
	// it must NOT pass the execution-contract gate (the old non-empty-object check let it through).
	emptyRequiredProofs := valid
	emptyRequiredProofs.ProofPolicy = []byte(`{"required_proofs":[]}`)
	if err := ValidateExecutionContract(emptyRequiredProofs); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("empty required_proofs should be not publishable, got %v", err)
	}

	// A real version-level proof requirement (object with a recognized array key) passes.
	realProof := valid
	realProof.ProofPolicy = []byte(`{"required_proofs":["administration_video"]}`)
	if err := ValidateExecutionContract(realProof); err != nil {
		t.Fatalf("non-empty required_proofs rejected: %v", err)
	}

	// Version-level proof_policy is OBJECT-shaped. Every value below must be rejected: a bare array
	// is the wrong shape at the version level, blank tokens are not requirements, and metadata-only
	// or scalar objects carry no proof array.
	for _, badProof := range []string{
		`["video"]`,                   // array shape is invalid at the version level
		`{"required_proofs":[""]}`,    // blank token
		`{"required_proofs":["   "]}`, // whitespace-only token
		`{"subject_scope":"batch"}`,   // metadata, no recognized proof array
		`{"required":true}`,           // recognized key but scalar, not an array of tokens
		`[]`,                          // empty array
	} {
		bad := valid
		bad.ProofPolicy = []byte(badProof)
		if err := ValidateExecutionContract(bad); !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("version-level proof %q should be not publishable, got %v", badProof, err)
		}
	}

	// Row-level proof is exercised through rule_dsl schedule[].proof_policy, NOT by putting an array
	// in the version-level ProofPolicy. The version-level proof stays a valid object; the schedule
	// row carries its own proof.
	rowArrayProof := valid
	rowArrayProof.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age","proof_policy":["administration_video"]}]}`)
	if err := ValidateExecutionContract(rowArrayProof); err != nil {
		t.Fatalf("row-level proof array rejected: %v", err)
	}

	// A row that supplies a blank proof array is rejected — its own blank proof does not fall back to
	// the (valid) version-level proof.
	rowBlankProof := valid
	rowBlankProof.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age","proof_policy":[""]}]}`)
	if err := ValidateExecutionContract(rowBlankProof); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("row-level blank proof array should be not publishable, got %v", err)
	}

	// A row that OMITS proof_policy inherits the validated version-level proof and still publishes.
	rowOmittedProof := valid
	rowOmittedProof.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age"}]}`)
	if err := ValidateExecutionContract(rowOmittedProof); err != nil {
		t.Fatalf("row omitting proof should inherit version proof, got %v", err)
	}

	unsupportedRepeat := valid
	unsupportedRepeat.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age","repeat":"until_age"}]}`)
	if err := ValidateExecutionContract(unsupportedRepeat); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("unsupported repeat policy should be not publishable, got %v", err)
	} else if !errors.Is(err, ErrUnsupportedRepeatPolicy) {
		t.Fatalf("unsupported repeat policy should preserve sentinel, got %v", err)
	}

	everyNDays := valid
	everyNDays.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age","repeat":"every_n_days","min_gap_days":30}]}`)
	if err := ValidateExecutionContract(everyNDays); err != nil {
		t.Fatalf("every_n_days with min_gap_days should publish, got %v", err)
	}

	yearly := valid
	yearly.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age","repeat":"yearly"}]}`)
	if err := ValidateExecutionContract(yearly); err != nil {
		t.Fatalf("yearly repeat should publish, got %v", err)
	}

	everyNDaysMissingGap := valid
	everyNDaysMissingGap.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"birth_age","repeat":"every_n_days","offset_days":30}]}`)
	if err := ValidateExecutionContract(everyNDaysMissingGap); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("every_n_days without min_gap_days should be not publishable, got %v", err)
	} else if !errors.Is(err, ErrUnsupportedRepeatPolicy) {
		t.Fatalf("every_n_days without min_gap_days should preserve sentinel, got %v", err)
	}

	unknownTrigger := valid
	unknownTrigger.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","trigger_type":"typo"}]}`)
	if err := ValidateExecutionContract(unknownTrigger); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("unknown trigger_type should be not publishable, got %v", err)
	}

	missingTrigger := valid
	missingTrigger.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary"}]}`)
	if err := ValidateExecutionContract(missingTrigger); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("missing trigger_type should be not publishable, got %v", err)
	}

	vaccination := valid
	vaccination.Category = "vaccination"
	vaccination.RuleDsl = []byte(validVaccinationMatrixRuleDSL())
	if err := ValidateExecutionContract(vaccination); err != nil {
		t.Fatalf("vaccination matrix execution contract rejected: %v", err)
	}

	missingMatrix := valid
	missingMatrix.Category = "vaccination"
	missingMatrix.RuleDsl = []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},"schedule":[{"dose_code":"primary","trigger_type":"birth_age","dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","due_window_days":7,"max_delay_days":7,"course_lapse_policy":"pc_review"}]}`)
	if err := ValidateExecutionContract(missingMatrix); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("vaccination without vaccine matrix object should be not publishable, got %v", err)
	}

	missingDoseAmount := valid
	missingDoseAmount.Category = "vaccination"
	missingDoseAmount.RuleDsl = []byte(`{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},"schedule":[{"dose_code":"primary","trigger_type":"birth_age","dose_unit":"ml","route_site":"subcutaneous","due_window_days":7,"max_delay_days":7,"course_lapse_policy":"pc_review"}]}`)
	if err := ValidateExecutionContract(missingDoseAmount); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("vaccination without dose amount should be not publishable, got %v", err)
	}

	missingReproductiveExclusions := valid
	missingReproductiveExclusions.Category = "vaccination"
	missingReproductiveExclusions.RuleDsl = []byte(`{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},"schedule":[{"dose_code":"primary","trigger_type":"birth_age","dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","due_window_days":7,"max_delay_days":7,"course_lapse_policy":"pc_review"}]}`)
	if err := ValidateExecutionContract(missingReproductiveExclusions); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("vaccination without reproductive exclusions should be not publishable, got %v", err)
	}

	missingMaxDelay := valid
	missingMaxDelay.Category = "vaccination"
	missingMaxDelay.RuleDsl = []byte(`{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":[]},"schedule":[{"dose_code":"primary","trigger_type":"birth_age","dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","course_lapse_policy":"pc_review"}]}`)
	if err := ValidateExecutionContract(missingMaxDelay); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("vaccination without max delay should be not publishable, got %v", err)
	}
}

func TestValidateRuleDSLRejectsUnknownKeys(t *testing.T) {
	valid := []byte(`{"category":"vaccination","scope":{"type":"tenant","id":null},"vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid"},"eligibility":{"animal_stage":"K1","defer_states":["icu"]},"schedule":[{"dose_code":"primary","sop_label":"display","dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review"}]}`)
	if err := ValidateRuleDSL(valid); err != nil {
		t.Fatalf("valid rule_dsl rejected: %v", err)
	}
	validFeed := []byte(`{"category":"feed_direction","scope":{"type":"tenant","id":null},"eligibility":{"animal_stage":"all","animal_stage_source":"shed_profiles.animal_stage_id -> animal_stage_lookup","breed_class":"all_reviewed_cohorts"},"parameter_template":{"source_tables":["feed_validation_tables"],"parameter_families":["feed_vectors"],"dimension_keys":["park_shed_breed_horizon"],"ratio_policy":"source_row_variable"},"ration":{"mode":"reviewed_template_rows","feed_item":"reviewed_template_rows","quantity":0,"unit":"kg_as_fed","quantity_semantics":"source-row variable by approved dimensions; examples are not global defaults"},"session_timing":[{"session_order":1,"session_time":"09:00","split_weight":"50","packing_proof_policy":["pack_qty"],"execution_proof_policy":["distribution_video"]}],"inventory_policy":{"mode":"reserve_consume_release","reserve":"reserve stock before packing","consume":"consume verified quantity","release":"release unused reserved quantity"},"validation_policy":{"checks":["resolver_coverage"],"calculation_outputs":["session_split_quantities"],"fail_closed":true,"preview_required":true}}`)
	if err := ValidateRuleDSL(validFeed); err != nil {
		t.Fatalf("valid feed rule_dsl rejected: %v", err)
	}
	for _, bad := range []string{
		`{"eligibilty":{"animal_stage":"K1"}}`,
		`{"vaccine":{"code":"ET","vaccine_type":"toxoid"}}`,
		`{"eligibility":{"animal_stage":"K1","defer_state":["icu"]}}`,
		`{"schedule":[{"dose_code":"primary","sop_version_id":"display-only"}]}`,
		`{"source":{"source_system":"pc","approvedby":"x"}}`,
		`{"parameter_template":{"source_table":["feed_validation_tables"]}}`,
		`{"validation_policy":{"calculation_output":["session_split_quantities"]}}`,
	} {
		if err := ValidateRuleDSL([]byte(bad)); !errors.Is(err, ErrInvalidRuleDSL) {
			t.Fatalf("rule_dsl %s err=%v, want ErrInvalidRuleDSL", bad, err)
		}
	}
}

func TestCreateVersionRejectsInvalidRuleDSLBeforeRepository(t *testing.T) {
	repo := &fakeProtocolRepo{}
	service := NewService(repo)

	_, err := service.CreateVersion(context.Background(), domain.NewVersion{
		RuleDsl: []byte(`{"eligibilty":{"animal_stage":"K1"}}`),
	})
	if !errors.Is(err, ErrInvalidRuleDSL) {
		t.Fatalf("CreateVersion invalid rule_dsl err=%v, want ErrInvalidRuleDSL", err)
	}
	if repo.createVersionCalled {
		t.Fatalf("repo must not be called with invalid rule_dsl")
	}
}

func TestAddRuleRejectsUnsupportedRepeatPolicy(t *testing.T) {
	repo := &fakeProtocolRepo{}
	service := NewService(repo)

	_, err := service.AddRule(context.Background(), domain.NewRule{Repeat: "after_age"})
	if !errors.Is(err, ErrUnsupportedRepeatPolicy) {
		t.Fatalf("unsupported repeat err=%v, want ErrUnsupportedRepeatPolicy", err)
	}
	if repo.createRuleCalled {
		t.Fatalf("repo must not be called for unsupported repeat policies")
	}
}

func TestAddRuleRejectsEveryNDaysWithoutMinGap(t *testing.T) {
	repo := &fakeProtocolRepo{}
	service := NewService(repo)

	_, err := service.AddRule(context.Background(), domain.NewRule{Repeat: "every_n_days", OffsetDays: 30})
	if !errors.Is(err, ErrUnsupportedRepeatPolicy) {
		t.Fatalf("every_n_days err=%v, want ErrUnsupportedRepeatPolicy", err)
	}
	if repo.createRuleCalled {
		t.Fatalf("repo must not be called for unsafe every_n_days policies")
	}
}

func TestAddRuleAcceptsMaterializedRepeatPolicies(t *testing.T) {
	repo := &fakeProtocolRepo{}
	service := NewService(repo)

	if _, err := service.AddRule(context.Background(), domain.NewRule{Repeat: "every_n_days", MinGapDays: 30}); err != nil {
		t.Fatalf("every_n_days with min_gap_days should be accepted: %v", err)
	}
	if !repo.createRuleCalled || repo.createdRule.Repeat != "every_n_days" {
		t.Fatalf("repo called=%v repeat=%q, want every_n_days stored", repo.createRuleCalled, repo.createdRule.Repeat)
	}

	repo = &fakeProtocolRepo{}
	service = NewService(repo)
	if _, err := service.AddRule(context.Background(), domain.NewRule{Repeat: "yearly"}); err != nil {
		t.Fatalf("yearly repeat should be accepted: %v", err)
	}
	if !repo.createRuleCalled || repo.createdRule.Repeat != "yearly" {
		t.Fatalf("repo called=%v repeat=%q, want yearly stored", repo.createRuleCalled, repo.createdRule.Repeat)
	}
}

func TestAddRuleDefaultsBlankRepeatToNone(t *testing.T) {
	repo := &fakeProtocolRepo{}
	service := NewService(repo)

	if _, err := service.AddRule(context.Background(), domain.NewRule{}); err != nil {
		t.Fatalf("blank repeat should default to none: %v", err)
	}
	if !repo.createRuleCalled {
		t.Fatalf("repo should be called")
	}
	if repo.createdRule.Repeat != "none" {
		t.Fatalf("repeat=%q, want none", repo.createdRule.Repeat)
	}
}

func TestPublishVersionAlreadyPublishedRetryIsNoop(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("published"),
	}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil, "publish-key")
	if err != nil {
		t.Fatalf("publish already-published retry: %v", err)
	}
	if !repo.publishCalled {
		t.Fatalf("repo publish must run so idempotency is still enforced for already-published retries")
	}
	if repo.createRuleCalled {
		t.Fatalf("already-published retry must not create protocol rules")
	}
}

func TestPublishVersionRejectsRetiredVersion(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("retired"),
	}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ports.ErrVersionNotDraft) {
		t.Fatalf("publish retired err=%v, want ErrVersionNotDraft", err)
	}
}

func TestPublishVersionDelegatesDraftPublishToRepository(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	service := NewService(repo)

	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !repo.publishCalled {
		t.Fatalf("repo publish was not called")
	}
	if !repo.createRuleCalled {
		t.Fatalf("publish should materialize schedule[] into protocol_rules before publishing")
	}
	if len(repo.createdRules) != 2 {
		t.Fatalf("created rules = %#v, want ET+TT 4-week and 7-week rows", repo.createdRules)
	}
	if got := repo.createdRules[0]; got.DoseCode != "et_tt_4w" || got.TriggerType != "birth_age" || got.OffsetDays != 28 || got.CatchUp != "pc_approval" || got.Sequence != 1 {
		t.Fatalf("created rule[0] = %#v, want ET+TT 4-week birth_age pc_approval sequence 1", got)
	}
	if got := repo.createdRules[1]; got.DoseCode != "et_tt_7w" || got.TriggerType != "birth_age" || got.OffsetDays != 49 || got.MinGapDays != 21 || got.Sequence != 2 {
		t.Fatalf("created rule[1] = %#v, want ET+TT 7-week source row with 21-day min gap", got)
	}
	if string(repo.createdRule.EligibilityJSON) != `{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["icu","quarantine"]}` {
		t.Fatalf("eligibility json = %s", repo.createdRule.EligibilityJSON)
	}
	if repo.createdRules[0].IdempotencyKey != "protocol-schedule-rule:version-1:et_tt_4w" {
		t.Fatalf("idempotency key = %q", repo.createdRules[0].IdempotencyKey)
	}
	if repo.version.Status != "published" {
		t.Fatalf("repo status = %s, want published", repo.version.Status)
	}
}

func TestPublishVersionUsesExistingProtocolRuleRows(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
		rules:   []domain.Rule{{DoseCode: "et_tt_4w", Sequence: 1, TriggerType: "birth_age"}},
	}
	service := NewService(repo)

	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil); err != nil {
		t.Fatalf("publish with existing rule: %v", err)
	}
	if len(repo.createdRules) != 1 || repo.createdRules[0].DoseCode != "et_tt_7w" {
		t.Fatalf("created rules=%#v, want only missing ET+TT 7-week row", repo.createdRules)
	}
	if !repo.publishCalled {
		t.Fatalf("repo publish was not called")
	}
}

func TestPublishVersionMaterializesMatrixRowMetadata(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	repo.version.RuleDsl = []byte(validVaccinationMatrixRulesetDSL())
	service := NewService(repo)

	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil); err != nil {
		t.Fatalf("publish matrix: %v", err)
	}
	if len(repo.createdRules) != 1 {
		t.Fatalf("created rules = %#v, want one Sheep Pox row", repo.createdRules)
	}
	var meta struct {
		MatrixRowID    string `json:"matrix_row_id"`
		SourceDoseCode string `json:"source_dose_code"`
		Eligibility    struct {
			Species     []string `json:"species"`
			AnimalStage []string `json:"animal_stage"`
		} `json:"eligibility"`
		Vaccine struct {
			Code          string `json:"code"`
			PathogenClass string `json:"pathogen_class"`
		} `json:"vaccine"`
	}
	if err := json.Unmarshal(repo.createdRules[0].EligibilityJSON, &meta); err != nil {
		t.Fatalf("eligibility metadata should be matrix wrapper, got %s: %v", repo.createdRules[0].EligibilityJSON, err)
	}
	if meta.MatrixRowID != "adult-sheep-second-wave" || meta.SourceDoseCode != "sheep_pox_adult_second_wave" {
		t.Fatalf("matrix identity = %#v", meta)
	}
	if len(meta.Eligibility.Species) != 1 || meta.Eligibility.Species[0] != "sheep" {
		t.Fatalf("matrix eligibility species = %#v", meta.Eligibility.Species)
	}
	if meta.Vaccine.Code != "SHEEP_POX" || meta.Vaccine.PathogenClass != "live" {
		t.Fatalf("matrix vaccine metadata = %#v", meta.Vaccine)
	}
}

func TestPublishVersionRejectsMatrixScheduleWithoutRowMetadata(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	repo.version.RuleDsl = []byte(strings.Replace(validVaccinationMatrixRulesetDSL(), `"matrix_rows":[{"row_id":"adult-sheep-second-wave","vaccine":{"code":"SHEEP_POX","name":"Sheep Pox","type":"live","pathogen_class":"live","compatibility_group":"POX","course_type":"single"},"eligibility":{"species":["sheep"],"animal_stage":["DOE","MOTHER","BUCK"],"sex":["female","male"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"exclude_reproductive_states":["pregnant_late"],"defer_states":["icu","quarantine"]},"schedule":[{"dose_code":"sheep_pox_adult_second_wave","source_dose_code":"sheep_pox_adult_second_wave","sequence":1,"trigger_type":"post_arrival","offset_days":28,"due_window_days":7,"dose_amount":1,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"preventive_care_review","repeat":"yearly","catch_up":"immediate"}]}],`, ``, 1))
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish matrix missing row metadata err=%v, want ErrNotPublishable", err)
	}
	if repo.createRuleCalled || repo.publishCalled {
		t.Fatalf("matrix without row metadata must not create rules or publish")
	}
}

func TestPublishVersionRejectsExistingMatrixRuleWithoutRowMetadata(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
		rules: []domain.Rule{{
			RuleID:          "rule-existing",
			DoseCode:        "sheep_pox_adult_second_wave",
			Sequence:        1,
			TriggerType:     "post_arrival",
			EligibilityJSON: []byte(`{"species":["sheep"]}`),
		}},
	}
	repo.version.RuleDsl = []byte(validVaccinationMatrixRulesetDSL())
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish existing bad matrix rule err=%v, want ErrNotPublishable", err)
	}
	if repo.publishCalled {
		t.Fatalf("matrix with existing bad protocol_rule must not publish")
	}
}

func TestPublishVersionRejectsDraftWithNoExecutableRows(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	repo.version.Category = "deworming"
	repo.version.RuleDsl = []byte(`{"category":"deworming"}`)
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish no executable rows err=%v, want ErrNotPublishable", err)
	}
	if repo.publishCalled {
		t.Fatalf("repo publish must not run when no schedule/rules exist")
	}
}

func TestPublishVersionRejectsInvalidScheduleRowBeforePublish(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	repo.version.RuleDsl = []byte(`{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":[]},"schedule":[{"dose_code":"primary","dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","due_window_days":7,"max_delay_days":7,"course_lapse_policy":"pc_review"}]}`)
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish invalid schedule err=%v, want ErrNotPublishable", err)
	}
	if repo.createRuleCalled || repo.publishCalled {
		t.Fatalf("invalid schedule should not create rules or publish")
	}
}

func TestPublishVersionRejectsProcurementComboOverTwoVaccines(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	dsl := validVaccinationMatrixRuleDSL()
	dsl = strings.Replace(dsl, `"first_wave":["ET+TT","PPR"]`, `"first_wave":["ET+TT","PPR","FMD"]`, 1)
	repo.version.RuleDsl = []byte(dsl)
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish 3-vaccine combo err=%v, want ErrNotPublishable", err)
	}
}

func validPublishVersion(status string) domain.Version {
	return domain.Version{
		ProtocolVersionID: "version-1",
		ProtocolID:        "protocol-1",
		Category:          "vaccination",
		Status:            status,
		SopVersionID:      "62000000-0000-4000-8000-000000000001",
		ProofPolicy:       []byte(`{"required_proofs":["administration_video"]}`),
		RuleDsl:           []byte(validVaccinationMatrixRuleDSL()),
	}
}

func validVaccinationMatrixRuleDSL() string {
	return `{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid","pathogen_class":"bacterial","course_type":"booster","inventory_item_id":"item-et","manufacturer":"tracked-matrix","disease":"Enterotoxaemia + Tetanus","compatibility_group":"ET+TT"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["icu","quarantine"]},"missed_dose_policy":"pc_approval","compatibility_policy":{"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14,"live_to_live_gap_days":28,"kid_booster_min_gap_days":21,"bacterial_viral_same_day_allowed":true,"live_killed_viral_same_day_allowed":true},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_prior_vaccination_allowed":true,"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox","ET+TT booster"],"sheep_second_wave":["ET+TT booster","Sheep Pox"]},"pregnancy_policy":{"allow_until_pregnancy_month":3,"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5,"post_delivery_catch_up_days":14},"schedule":[{"dose_code":"et_tt_4w","sequence":1,"trigger_type":"birth_age","offset_days":28,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","vial_doses":100,"revaccination_interval_days":182,"schedule_note":"approved kid timing: 4 weeks and 7 weeks","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review","repeat":"none","catch_up":"pc_approval"},{"dose_code":"et_tt_7w","sequence":2,"trigger_type":"birth_age","offset_days":49,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","vial_doses":100,"revaccination_interval_days":182,"schedule_note":"approved kid timing: 4 weeks and 7 weeks; booster gap 3 weeks","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review","min_gap_days":21,"repeat":"none","repeat_until_after_age":"-","catch_up":"pc_approval"}]}`
}

func validVaccinationMatrixRulesetDSL() string {
	return `{"category":"vaccination","ruleset_family":"vaccination.matrix","vaccine":{"code":"vaccination.matrix","name":"Preventive Care vaccination matrix","type":"matrix"},"eligibility":{"animal_stage":"all","species":["goat","sheep"],"sex":["female","male"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"exclude_reproductive_states":["pregnant_late"],"defer_states":["icu","quarantine"]},"missed_dose_policy":"immediate","compatibility_policy":{"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14,"live_to_live_gap_days":28,"kid_booster_min_gap_days":21,"bacterial_viral_same_day_allowed":true,"live_killed_viral_same_day_allowed":true,"max_vaccines_per_combo_session":2},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_prior_vaccination_allowed":true,"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox","ET+TT booster"],"sheep_second_wave":["ET+TT booster","Sheep Pox"]},"matrix_rows":[{"row_id":"adult-sheep-second-wave","vaccine":{"code":"SHEEP_POX","name":"Sheep Pox","type":"live","pathogen_class":"live","compatibility_group":"POX","course_type":"single"},"eligibility":{"species":["sheep"],"animal_stage":["DOE","MOTHER","BUCK"],"sex":["female","male"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"exclude_reproductive_states":["pregnant_late"],"defer_states":["icu","quarantine"]},"schedule":[{"dose_code":"sheep_pox_adult_second_wave","source_dose_code":"sheep_pox_adult_second_wave","sequence":1,"trigger_type":"post_arrival","offset_days":28,"due_window_days":7,"dose_amount":1,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"preventive_care_review","repeat":"yearly","catch_up":"immediate"}]}],"schedule":[{"dose_code":"sheep_pox_adult_second_wave","source_dose_code":"sheep_pox_adult_second_wave","sequence":1,"trigger_type":"post_arrival","offset_days":28,"due_window_days":7,"dose_amount":1,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"preventive_care_review","repeat":"yearly","catch_up":"immediate"}]}`
}

type fakeProtocolRepo struct {
	version             domain.Version
	publishCalled       bool
	publishCalls        int
	createVersionCalled bool
	createRuleCalled    bool
	createdRule         domain.NewRule
	createdRules        []domain.NewRule
	rules               []domain.Rule
}

func (f *fakeProtocolRepo) Ping(context.Context) error { return nil }
func (f *fakeProtocolRepo) CreateDefinition(context.Context, domain.NewDefinition) (string, error) {
	return "", nil
}
func (f *fakeProtocolRepo) GetDefinitionByCode(context.Context, string, string) (domain.Definition, error) {
	return domain.Definition{}, nil
}
func (f *fakeProtocolRepo) CreateVersion(context.Context, domain.NewVersion) (string, error) {
	f.createVersionCalled = true
	return "", nil
}
func (f *fakeProtocolRepo) GetVersion(context.Context, string, string) (domain.Version, error) {
	return f.version, nil
}
func (f *fakeProtocolRepo) ListPublishedVersions(context.Context, string, string) ([]domain.Version, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) ListConfigs(context.Context, string, string) ([]domain.ConfigListItem, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) PublishVersion(context.Context, string, string, *string, ...string) error {
	f.publishCalled = true
	f.publishCalls++
	f.version.Status = "published"
	return nil
}
func (f *fakeProtocolRepo) CreateRule(_ context.Context, in domain.NewRule) (string, error) {
	f.createRuleCalled = true
	f.createdRule = in
	f.createdRules = append(f.createdRules, in)
	f.rules = append(f.rules, domain.Rule{
		RuleID:        "rule-1",
		DoseCode:      in.DoseCode,
		Sequence:      in.Sequence,
		TriggerType:   in.TriggerType,
		OffsetDays:    in.OffsetDays,
		DueWindowDays: in.DueWindowDays,
		MinGapDays:    in.MinGapDays,
		Repeat:        in.Repeat,
		CatchUp:       in.CatchUp,
		SortOrder:     in.SortOrder,
	})
	return "rule-1", nil
}
func (f *fakeProtocolRepo) ListRules(context.Context, string, string) ([]domain.Rule, error) {
	return f.rules, nil
}
func (f *fakeProtocolRepo) ListActiveAnimalStages(context.Context, string) ([]domain.AnimalStage, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) CreateTrigger(context.Context, domain.NewTrigger) (string, error) {
	return "", nil
}
