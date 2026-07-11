package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

func vaccinationRuleDSLWithCapacity(maxPerDay, bufferDays int, scope, overflow string) string {
	base := validVaccinationMatrixRuleDSL()
	block := fmt.Sprintf(
		`,"capacity":{"max_per_day":%d,"max_buffer_days":%d,"capacity_scope":%q,"overflow_policy":%q}}`,
		maxPerDay, bufferDays, scope, overflow,
	)
	return strings.TrimSuffix(base, "}") + block
}

// TestPublishVersionSyncsVersionedCapacity proves publishing a vaccination version syncs its
// rule_dsl.capacity into the operational read model with the authored values.
func TestPublishVersionSyncsVersionedCapacity(t *testing.T) {
	version := validPublishVersion("draft")
	version.RuleDsl = []byte(vaccinationRuleDSLWithCapacity(137, 7, "tenant", "split_within_safe_window_then_mark_needs_review"))
	repo := &fakeProtocolRepo{version: version}
	service := NewService(repo)
	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil, "publish-key"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if repo.capacitySyncWant == nil {
		t.Fatalf("publish must sync versioned capacity into the operational read model")
	}
	got := *repo.capacitySyncWant
	want := domain.PublishedCapacity{MaxPerDay: 137, MaxBufferDays: 7, CapacityScope: "tenant", OverflowPolicy: "split_within_safe_window_then_mark_needs_review"}
	if got != want {
		t.Fatalf("synced capacity = %+v, want %+v", got, want)
	}
}

// TestPublishVersionCapacityParityMismatchFails proves the post-publish parity check fails the publish
// when the stored operational capacity does not match the versioned rule_dsl.capacity.
func TestPublishVersionCapacityParityMismatchFails(t *testing.T) {
	version := validPublishVersion("draft")
	version.RuleDsl = []byte(vaccinationRuleDSLWithCapacity(137, 7, "tenant", "split_within_safe_window_then_mark_needs_review"))
	drift := domain.PublishedCapacity{MaxPerDay: 100, MaxBufferDays: 3, CapacityScope: "tenant", OverflowPolicy: "split_within_safe_window_then_mark_needs_review"}
	repo := &fakeProtocolRepo{version: version, capacitySyncReturn: &drift}
	service := NewService(repo)
	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil, "publish-key")
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("capacity parity mismatch must fail publish with ErrNotPublishable, got %v", err)
	}
}

// TestParseVersionedCapacityDefaults proves an absent block yields ok=false and a present block applies
// the business default max_buffer_days=7 when omitted.
func TestParseVersionedCapacityDefaults(t *testing.T) {
	if _, ok, err := parseVersionedCapacity(nil); ok || err != nil {
		t.Fatalf("absent capacity: want ok=false err=nil, got ok=%v err=%v", ok, err)
	}
	got, ok, err := parseVersionedCapacity([]byte(`{"max_per_day":150}`))
	if err != nil || !ok {
		t.Fatalf("present capacity: ok=%v err=%v", ok, err)
	}
	if got.MaxPerDay != 150 || got.MaxBufferDays != 7 || got.CapacityScope != "tenant" {
		t.Fatalf("defaults not applied: %+v", got)
	}
}

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
	if !repo.genericPublishCalled {
		t.Fatalf("non-matrix published vaccination retry must use generic publish idempotency path")
	}
	if repo.publishedMatrixReplayCalled {
		t.Fatalf("non-matrix published vaccination retry must not use vaccination.matrix replay path")
	}
	if repo.createRuleCalled {
		t.Fatalf("already-published retry must not create protocol rules")
	}
}

func TestPublishVersionAlreadyPublishedVaccinationMatrixRetryUsesMatrixFingerprint(t *testing.T) {
	version := validPublishVersion("published")
	version.RuleDsl = []byte(validVaccinationMatrixRulesetDSL())
	repo := &fakeProtocolRepo{version: version}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil, "publish-key")
	if err != nil {
		t.Fatalf("publish already-published matrix retry: %v", err)
	}
	if !repo.publishedMatrixReplayCalled {
		t.Fatalf("published vaccination.matrix replay must use replay path")
	}
	if repo.genericPublishCalled {
		t.Fatalf("published vaccination.matrix replay must not fall through to generic publish fingerprint")
	}
	if repo.publishWithDerivedCalled {
		t.Fatalf("published vaccination.matrix replay must not rebuild derived rows")
	}
	if repo.createRuleCalled {
		t.Fatalf("already-published matrix retry must not create protocol rules")
	}
}

func TestPublishVersionAlreadyPublishedVaccinationMatrixRetrySkipsNewExecutionValidation(t *testing.T) {
	version := validPublishVersion("published")
	version.RuleDsl = []byte(strings.Replace(validVaccinationMatrixRulesetDSL(), `"required_proofs":["administration_video"]`, `"required_proofs":[]`, 1))
	version.ProofPolicy = []byte(`{"required_proofs":[]}`)
	repo := &fakeProtocolRepo{version: version}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil, "publish-key")
	if err != nil {
		t.Fatalf("published matrix replay must not re-run today's execution validator: %v", err)
	}
	if !repo.publishedMatrixReplayCalled {
		t.Fatalf("published vaccination.matrix replay must use replay path")
	}
	if repo.publishWithDerivedCalled || repo.createRuleCalled {
		t.Fatalf("published replay must not rebuild rules, publishWithDerived=%v create=%v", repo.publishWithDerivedCalled, repo.createRuleCalled)
	}
}

func TestPublishVersionAlreadyPublishedVaccinationMatrixRetrySkipsInvalidOldDSL(t *testing.T) {
	version := validPublishVersion("published")
	version.RuleDsl = []byte(`{"category":"vaccination","ruleset_family":"vaccination.matrix","matrix_rows":[`)
	repo := &fakeProtocolRepo{version: version}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil, "publish-key")
	if err != nil {
		t.Fatalf("published matrix replay must not re-validate old invalid DSL: %v", err)
	}
	if !repo.publishedMatrixReplayCalled {
		t.Fatalf("published vaccination.matrix replay must use replay path")
	}
	if repo.publishWithDerivedCalled || repo.createRuleCalled {
		t.Fatalf("published replay must not rebuild rules, publishWithDerived=%v create=%v", repo.publishWithDerivedCalled, repo.createRuleCalled)
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
	if len(repo.dimensions) != 6 {
		t.Fatalf("compiled dimensions = %#v, want 3 stages x 2 sexes", repo.dimensions)
	}
	first := repo.dimensions[0]
	if first.Category != "vaccination" || first.RulesetFamily != "vaccination.matrix" || first.MatrixRowID != "adult-sheep-second-wave" {
		t.Fatalf("compiled dimension identity = %#v", first)
	}
	if first.Species != "sheep" || first.VaccineCode != "SHEEP_POX" || first.VaccineType != "live" || first.MaxDelayDays != 7 {
		t.Fatalf("compiled dimension selector/vaccine = %#v", first)
	}
	for _, dim := range repo.dimensions {
		if dim.Sex == "unknown" {
			t.Fatalf("compiled dimensions must never contain unknown sex: %#v", repo.dimensions)
		}
	}
}

func TestPublishVersionRejectsMatrixUnknownSexSelector(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	repo.version.RuleDsl = []byte(strings.Replace(validVaccinationMatrixRulesetDSL(), `"sex":["female","male"]`, `"sex":["unknown"]`, 1))
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish matrix unknown sex err=%v, want ErrNotPublishable", err)
	}
	if repo.publishCalled || len(repo.dimensions) > 0 {
		t.Fatalf("unknown sex must not publish or compile dimensions, publish=%v dimensions=%#v", repo.publishCalled, repo.dimensions)
	}
}

func TestPublishVersionRejectsMatrixCellMissingFromTopLevelSchedule(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(validVaccinationMatrixRulesetDSL()), &dsl); err != nil {
		t.Fatalf("valid matrix dsl: %v", err)
	}
	dsl["schedule"] = []any{}
	raw, err := json.Marshal(dsl)
	if err != nil {
		t.Fatalf("marshal partial matrix dsl: %v", err)
	}
	repo.version.RuleDsl = raw
	service := NewService(repo)

	err = service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish partial matrix err=%v, want ErrNotPublishable", err)
	}
	if repo.publishCalled || repo.createRuleCalled || len(repo.dimensions) > 0 {
		t.Fatalf("partial matrix must not publish/create/compile, publish=%v create=%v dimensions=%#v", repo.publishCalled, repo.createRuleCalled, repo.dimensions)
	}
}

func TestPublishVersionRejectsExtraTopLevelMatrixScheduleAlias(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(validVaccinationMatrixRulesetDSL()), &dsl); err != nil {
		t.Fatalf("valid matrix dsl: %v", err)
	}
	schedule, ok := dsl["schedule"].([]any)
	if !ok || len(schedule) == 0 {
		t.Fatalf("valid matrix dsl missing schedule: %#v", dsl["schedule"])
	}
	extra, ok := schedule[0].(map[string]any)
	if !ok {
		t.Fatalf("valid matrix schedule row has wrong shape: %#v", schedule[0])
	}
	extra = cloneAnyMap(extra)
	extra["dose_code"] = "sheep_pox_duplicate_alias"
	extra["source_dose_code"] = "sheep_pox_adult_second_wave"
	dsl["schedule"] = append(schedule, extra)
	raw, err := json.Marshal(dsl)
	if err != nil {
		t.Fatalf("marshal aliased matrix dsl: %v", err)
	}
	repo.version.RuleDsl = raw
	service := NewService(repo)

	err = service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish aliased matrix err=%v, want ErrNotPublishable", err)
	}
	if repo.publishCalled || repo.createRuleCalled || len(repo.dimensions) > 0 {
		t.Fatalf("aliased matrix must not publish/create/compile, publish=%v create=%v dimensions=%#v", repo.publishCalled, repo.createRuleCalled, repo.dimensions)
	}
}

func TestAddRuleRejectsUnknownSexEligibility(t *testing.T) {
	repo := &fakeProtocolRepo{version: validPublishVersion("draft")}
	service := NewService(repo)

	_, err := service.AddRule(context.Background(), domain.NewRule{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		DoseCode:          "bad-sex",
		Sequence:          1,
		TriggerType:       "birth_age",
		Repeat:            "none",
		CatchUp:           "immediate",
		EligibilityJSON:   []byte(`{"eligibility":{"sex":["female","unknown"]}}`),
	})
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("add rule unknown sex err=%v, want ErrNotPublishable", err)
	}
	if repo.createRuleCalled {
		t.Fatalf("unknown sex rule must not be stored")
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

func TestPublishVersionRegeneratesMatrixRulesFromRuleDSL(t *testing.T) {
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

	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil); err != nil {
		t.Fatalf("publish with stale existing matrix rule: %v", err)
	}
	if !repo.publishWithDerivedCalled || !repo.publishCalled {
		t.Fatalf("matrix publish should use atomic derived path, derived=%v publish=%v", repo.publishWithDerivedCalled, repo.publishCalled)
	}
	if len(repo.createdRules) != 1 {
		t.Fatalf("created rules=%#v, want regenerated single matrix row", repo.createdRules)
	}
	var meta struct {
		MatrixRowID string `json:"matrix_row_id"`
		Eligibility struct {
			Species []string `json:"species"`
		} `json:"eligibility"`
		Vaccine struct {
			Code string `json:"code"`
		} `json:"vaccine"`
	}
	if err := json.Unmarshal(repo.createdRules[0].EligibilityJSON, &meta); err != nil {
		t.Fatalf("regenerated metadata should be matrix wrapper: %v", err)
	}
	if meta.MatrixRowID != "adult-sheep-second-wave" || meta.Vaccine.Code != "SHEEP_POX" || len(meta.Eligibility.Species) != 1 || meta.Eligibility.Species[0] != "sheep" {
		t.Fatalf("regenerated matrix metadata = %#v", meta)
	}
}

func TestPublishVersionMatrixInheritsVersionEligibilityAndCanonicalizesAny(t *testing.T) {
	repo := &fakeProtocolRepo{version: validPublishVersion("draft")}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(validVaccinationMatrixRulesetDSL()), &dsl); err != nil {
		t.Fatalf("valid matrix dsl: %v", err)
	}
	dsl["eligibility"] = map[string]any{
		"animal_stage":                []any{"any"},
		"species":                     []any{"sheep"},
		"sex":                         []any{"any"},
		"breed":                       []any{"*"},
		"lifecycle":                   []any{"alive"},
		"health":                      []any{"*"},
		"reproductive":                []any{"any"},
		"exclude_reproductive_states": []any{"pregnant_late"},
		"defer_states":                []any{"icu", "quarantine"},
	}
	matrixRows := dsl["matrix_rows"].([]any)
	firstRow := cloneAnyMap(matrixRows[0].(map[string]any))
	firstRow["eligibility"] = map[string]any{
		"lifecycle":                   []any{"alive"},
		"reproductive":                []any{"any"},
		"exclude_reproductive_states": []any{"pregnant_late"},
		"defer_states":                []any{"icu", "quarantine"},
	}
	dsl["matrix_rows"] = []any{firstRow}
	raw, err := json.Marshal(dsl)
	if err != nil {
		t.Fatalf("marshal inherited matrix dsl: %v", err)
	}
	repo.version.RuleDsl = raw
	service := NewService(repo)

	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil); err != nil {
		t.Fatalf("publish inherited matrix: %v", err)
	}
	if len(repo.dimensions) != 1 {
		t.Fatalf("dimensions=%#v, want 1 wildcard sex row with inherited sheep/all/all", repo.dimensions)
	}
	for _, dim := range repo.dimensions {
		if dim.Species != "sheep" || dim.AnimalStage != "all" || dim.Sex != "all" || dim.Breed != "all" {
			t.Fatalf("dimension did not inherit/canonicalize selectors: %#v", dim)
		}
	}
	var payload struct {
		Eligibility map[string]json.RawMessage `json:"eligibility"`
	}
	if err := json.Unmarshal(repo.createdRules[0].EligibilityJSON, &payload); err != nil {
		t.Fatalf("derived eligibility json: %v", err)
	}
	for field, want := range map[string]string{
		"species":      "sheep",
		"animal_stage": "all",
		"sex":          "all",
		"breed":        "all",
		"health":       "all",
		"reproductive": "all",
	} {
		values, err := rawSelectorValues(payload.Eligibility[field])
		if err != nil || len(values) != 1 || values[0] != want {
			t.Fatalf("derived eligibility %s=%v err=%v, want [%s]", field, values, err, want)
		}
	}
}

func TestPublishVersionRejectsDuplicateMatrixRowIDAndSourceDoseAlias(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "duplicate row id",
			mutate: func(dsl map[string]any) {
				rows := dsl["matrix_rows"].([]any)
				dsl["matrix_rows"] = append(rows, cloneAnyMap(rows[0].(map[string]any)))
			},
		},
		{
			name: "duplicate source dose alias",
			mutate: func(dsl map[string]any) {
				rows := dsl["matrix_rows"].([]any)
				row := cloneAnyMap(rows[0].(map[string]any))
				row["row_id"] = "adult-sheep-second-wave-copy"
				cells := row["schedule"].([]any)
				cell := cloneAnyMap(cells[0].(map[string]any))
				cell["dose_code"] = "sheep_pox_alias_copy"
				cell["source_dose_code"] = "sheep_pox_adult_second_wave"
				row["schedule"] = []any{cell}
				rows = append(rows, row)
				dsl["matrix_rows"] = rows
				top := dsl["schedule"].([]any)
				topCell := cloneAnyMap(top[0].(map[string]any))
				topCell["dose_code"] = "sheep_pox_alias_copy"
				topCell["source_dose_code"] = "sheep_pox_adult_second_wave"
				dsl["schedule"] = append(top, topCell)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeProtocolRepo{version: validPublishVersion("draft")}
			var dsl map[string]any
			if err := json.Unmarshal([]byte(validVaccinationMatrixRulesetDSL()), &dsl); err != nil {
				t.Fatalf("valid matrix dsl: %v", err)
			}
			tc.mutate(dsl)
			raw, err := json.Marshal(dsl)
			if err != nil {
				t.Fatalf("marshal bad matrix dsl: %v", err)
			}
			repo.version.RuleDsl = raw
			err = NewService(repo).PublishVersion(context.Background(), "tenant-1", "version-1", nil)
			if !errors.Is(err, ErrNotPublishable) {
				t.Fatalf("publish err=%v, want ErrNotPublishable", err)
			}
			if repo.publishCalled || repo.createRuleCalled {
				t.Fatalf("invalid matrix must not publish/create derived rows")
			}
		})
	}
}

func TestPublishVersionRejectsNegativeMatrixTiming(t *testing.T) {
	repo := &fakeProtocolRepo{version: validPublishVersion("draft")}
	repo.version.RuleDsl = []byte(strings.Replace(validVaccinationMatrixRulesetDSL(), `"offset_days":28`, `"offset_days":-1`, 1))
	err := NewService(repo).PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("publish negative timing err=%v, want ErrNotPublishable", err)
	}
	if repo.publishCalled || repo.createRuleCalled {
		t.Fatalf("negative timing must not publish/create derived rows")
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

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type fakeProtocolRepo struct {
	version                     domain.Version
	publishCalled               bool
	genericPublishCalled        bool
	publishWithDerivedCalled    bool
	publishedMatrixReplayCalled bool
	publishCalls                int
	createVersionCalled         bool
	createRuleCalled            bool
	createdRule                 domain.NewRule
	createdRules                []domain.NewRule
	rules                       []domain.Rule
	dimensions                  []domain.RuleDimension
	capacitySyncWant            *domain.PublishedCapacity
	capacitySyncReturn          *domain.PublishedCapacity
}

// SyncVaccinationCapacityConfig records the versioned capacity the publish flow synced, and echoes it
// back (parity ok) unless a mismatching capacitySyncReturn is configured.
func (f *fakeProtocolRepo) SyncVaccinationCapacityConfig(_ context.Context, _ string, want domain.PublishedCapacity) (domain.PublishedCapacity, error) {
	w := want
	f.capacitySyncWant = &w
	if f.capacitySyncReturn != nil {
		return *f.capacitySyncReturn, nil
	}
	return want, nil
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
	f.genericPublishCalled = true
	f.publishCalls++
	f.version.Status = "published"
	return nil
}
func (f *fakeProtocolRepo) PublishVersionWithDerivedRules(_ context.Context, _ string, _ domain.Version, rules []domain.NewRule, dimensions []domain.RuleDimension, _ *string, _ ...string) error {
	alreadyPublished := f.version.Status == "published"
	f.publishCalled = true
	f.publishWithDerivedCalled = true
	f.publishCalls++
	f.createRuleCalled = !alreadyPublished && len(rules) > 0
	f.createdRules = append([]domain.NewRule(nil), rules...)
	if len(rules) > 0 {
		f.createdRule = rules[len(rules)-1]
	}
	f.rules = f.rules[:0]
	for _, in := range rules {
		ruleID := strings.TrimSpace(in.RuleID)
		if ruleID == "" {
			ruleID = "rule-" + in.DoseCode
		}
		f.rules = append(f.rules, domain.Rule{
			RuleID:              ruleID,
			ProtocolVersionID:   in.ProtocolVersionID,
			ProtocolID:          f.version.ProtocolID,
			DoseCode:            in.DoseCode,
			Sequence:            in.Sequence,
			TriggerType:         in.TriggerType,
			OffsetDays:          in.OffsetDays,
			DueWindowDays:       in.DueWindowDays,
			MinGapDays:          in.MinGapDays,
			Repeat:              in.Repeat,
			RepeatUntilAfterAge: in.RepeatUntilAfterAge,
			CatchUp:             in.CatchUp,
			EligibilityJSON:     in.EligibilityJSON,
			SortOrder:           in.SortOrder,
		})
	}
	f.dimensions = append([]domain.RuleDimension(nil), dimensions...)
	f.version.Status = "published"
	return nil
}
func (f *fakeProtocolRepo) PublishPublishedMatrixReplay(context.Context, string, domain.Version, *string, ...string) error {
	f.publishCalled = true
	f.publishedMatrixReplayCalled = true
	f.publishCalls++
	return nil
}
func (f *fakeProtocolRepo) CreateRule(_ context.Context, in domain.NewRule) (string, error) {
	f.createRuleCalled = true
	f.createdRule = in
	f.createdRules = append(f.createdRules, in)
	f.rules = append(f.rules, domain.Rule{
		RuleID:              "rule-" + in.DoseCode,
		ProtocolVersionID:   in.ProtocolVersionID,
		ProtocolID:          f.version.ProtocolID,
		DoseCode:            in.DoseCode,
		Sequence:            in.Sequence,
		TriggerType:         in.TriggerType,
		OffsetDays:          in.OffsetDays,
		DueWindowDays:       in.DueWindowDays,
		MinGapDays:          in.MinGapDays,
		Repeat:              in.Repeat,
		RepeatUntilAfterAge: in.RepeatUntilAfterAge,
		CatchUp:             in.CatchUp,
		EligibilityJSON:     in.EligibilityJSON,
		SortOrder:           in.SortOrder,
	})
	return "rule-" + in.DoseCode, nil
}
func (f *fakeProtocolRepo) ListRules(context.Context, string, string) ([]domain.Rule, error) {
	return f.rules, nil
}
func (f *fakeProtocolRepo) ReplaceProtocolRuleDimensions(_ context.Context, _ string, _ string, dimensions []domain.RuleDimension) error {
	f.dimensions = append([]domain.RuleDimension(nil), dimensions...)
	return nil
}
func (f *fakeProtocolRepo) ListActiveAnimalStages(context.Context, string) ([]domain.AnimalStage, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) CreateTrigger(context.Context, domain.NewTrigger) (string, error) {
	return "", nil
}
