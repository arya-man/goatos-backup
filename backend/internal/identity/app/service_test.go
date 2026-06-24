package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
	testActor    = "90000000-0000-4000-8000-000000000001"
	testTrace    = "trace-test"
	goatA        = "10000000-0000-4000-8000-000000000001"
	goatB        = "10000000-0000-4000-8000-000000000002"
	mergedGoat   = "10000000-0000-4000-8000-000000000003"
	survivorGoat = "10000000-0000-4000-8000-000000000004"
	conflictID   = "20000000-0000-4000-8000-000000000001"
)

func TestResolveIdentifierStateMachine(t *testing.T) {
	now := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		matches      []domain.IdentifierMatch
		wantState    string
		wantGoat     bool
		wantConflict bool
	}{
		{
			name: "single_match",
			matches: []domain.IdentifierMatch{{
				Identifier: identifier("old_tag", "1900", "park:CBE", "active", now),
				Goat:       summary(goatA, "G-000001", "clean"),
			}},
			wantState: domain.ResolutionSingleMatch,
			wantGoat:  true,
		},
		{
			name: "multiple_matches_with_conflict",
			matches: []domain.IdentifierMatch{
				{Identifier: identifier("old_tag", "1900", "park:CBE", "active", now), Goat: summary(goatA, "G-000001", "clean")},
				{Identifier: identifier("old_tag", "1900", "park:CBE", "active", now), Goat: summary(goatB, "G-000002", "clean")},
			},
			wantState:    domain.ResolutionMultipleMatch,
			wantConflict: true,
		},
		{
			name: "cross_scope_multiple_matches_without_conflict",
			matches: []domain.IdentifierMatch{
				{Identifier: identifier("old_tag", "1900", "park:CBE", "active", now), Goat: summary(goatA, "G-000001", "clean")},
				{Identifier: identifier("old_tag", "1900", "park:CPT", "active", now), Goat: summary(goatB, "G-000002", "clean")},
			},
			wantState: domain.ResolutionMultipleMatch,
		},
		{
			name:      "no_match",
			matches:   nil,
			wantState: domain.ResolutionNoMatch,
		},
		{
			name: "needs_review_for_history_only",
			matches: []domain.IdentifierMatch{{
				Identifier: identifier("old_tag", "1900", "park:CBE", "retired", now),
				Goat:       summary(goatA, "G-000001", "clean"),
			}},
			wantState: domain.ResolutionNeedsReview,
		},
		{
			name: "merged_redirect",
			matches: []domain.IdentifierMatch{{
				Identifier: identifier("old_tag", "1900", "park:CBE", "active", now),
				Goat:       summary(mergedGoat, "G-000003", "merged"),
			}},
			wantState: domain.ResolutionMergedRedirect,
			wantGoat:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				matches:    tt.matches,
				conflictID: conflictForTest(tt.name),
				goats: map[string]*domain.GoatPassport{
					mergedGoat:   passport(mergedGoat, "G-000003", "merged", &[]string{survivorGoat}[0]),
					survivorGoat: passport(survivorGoat, "G-000004", "clean", nil),
				},
			}
			svc := NewService(repo)
			result, err := svc.ResolveIdentifier(context.Background(), ports.ResolveIdentifierParams{
				TenantID:        testTenant,
				IdentifierType:  "old_tag",
				NormalizedValue: "1900",
				ScopeKey:        strPtr("park:CBE"),
			}, testTrace)
			if err != nil {
				t.Fatalf("ResolveIdentifier error = %v", err)
			}
			if result.ResolutionState != tt.wantState {
				t.Fatalf("state = %s, want %s", result.ResolutionState, tt.wantState)
			}
			if tt.wantGoat && result.GoatSummary == nil {
				t.Fatal("expected goat summary")
			}
			if tt.wantConflict && result.ConflictID == nil {
				t.Fatal("expected conflict id")
			}
		})
	}
}

func TestGetGoatPassportRedirectsMergedGoat(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{
		mergedGoat:   passport(mergedGoat, "G-000003", "merged", &[]string{survivorGoat}[0]),
		survivorGoat: passport(survivorGoat, "G-000004", "clean", nil),
	}}
	svc := NewService(repo)

	result, err := svc.GetGoatPassport(context.Background(), testTenant, mergedGoat, testTrace)
	if err != nil {
		t.Fatalf("GetGoatPassport error = %v", err)
	}
	if result.Goat.GoatID != survivorGoat {
		t.Fatalf("goat id = %s, want survivor %s", result.Goat.GoatID, survivorGoat)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != "merged_redirect" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestGetGoatPassportFollowsMergeRedirectChain(t *testing.T) {
	midSurvivor := "10000000-0000-4000-8000-000000000005"
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{
		mergedGoat:   passport(mergedGoat, "G-000003", "merged", &midSurvivor),
		midSurvivor:  passport(midSurvivor, "G-000005", "merged", &[]string{survivorGoat}[0]),
		survivorGoat: passport(survivorGoat, "G-000004", "clean", nil),
	}}
	svc := NewService(repo)

	result, err := svc.GetGoatPassport(context.Background(), testTenant, mergedGoat, testTrace)
	if err != nil {
		t.Fatalf("GetGoatPassport error = %v", err)
	}
	if result.Goat.GoatID != survivorGoat {
		t.Fatalf("goat id = %s, want final survivor %s", result.Goat.GoatID, survivorGoat)
	}
	if result.Goat.IdentityState == "merged" {
		t.Fatalf("returned merged goat: %#v", result.Goat)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want two redirect hops", result.Warnings)
	}
}

func TestGetGoatPassportDetectsMergeRedirectCycle(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{
		mergedGoat:   passport(mergedGoat, "G-000003", "merged", &[]string{survivorGoat}[0]),
		survivorGoat: passport(survivorGoat, "G-000004", "merged", &[]string{mergedGoat}[0]),
	}}
	svc := NewService(repo)

	if _, err := svc.GetGoatPassport(context.Background(), testTenant, mergedGoat, testTrace); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestCanonicalRequestHashIsStableAndScoped(t *testing.T) {
	bodyA := []byte(`{"identifier_type":"rfid","identifier_value":"RFID-SYNTHETIC-001","scope_key":"global:rfid","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}],"row_version":1}`)
	bodyB := []byte(`{"row_version":1,"evidence_refs":[{"evidence_id":"synthetic-row-1","evidence_type":"source_record"}],"scope_key":"global:rfid","identifier_value":"RFID-SYNTHETIC-001","identifier_type":"rfid"}`)
	route := "/admin/goats/" + goatA + "/identifiers"
	hashA, err := CanonicalRequestHashWithSubject(testTenant, addGoatIdentifierCommand, route, goatA, bodyA)
	if err != nil {
		t.Fatalf("hash A: %v", err)
	}
	hashB, err := CanonicalRequestHashWithSubject(testTenant, addGoatIdentifierCommand, route, goatA, bodyB)
	if err != nil {
		t.Fatalf("hash B: %v", err)
	}
	if hashA != hashB {
		t.Fatalf("hash should be stable across JSON key order: %s != %s", hashA, hashB)
	}
	otherTenantHash, err := CanonicalRequestHashWithSubject("00000000-0000-4000-8000-000000000002", addGoatIdentifierCommand, route, goatA, bodyA)
	if err != nil {
		t.Fatalf("hash other tenant: %v", err)
	}
	if otherTenantHash == hashA {
		t.Fatal("hash must include tenant scope")
	}
	otherRouteHash, err := CanonicalRequestHashWithSubject(testTenant, addGoatIdentifierCommand, "/other", goatA, bodyA)
	if err != nil {
		t.Fatalf("hash other route: %v", err)
	}
	if otherRouteHash == hashA {
		t.Fatal("hash must include route identity")
	}
	subjectHash, err := CanonicalRequestHashWithSubject(testTenant, addGoatIdentifierCommand, route, goatA, bodyA)
	if err != nil {
		t.Fatalf("hash with subject: %v", err)
	}
	otherSubjectHash, err := CanonicalRequestHashWithSubject(testTenant, addGoatIdentifierCommand, route, goatB, bodyA)
	if err != nil {
		t.Fatalf("hash with other subject: %v", err)
	}
	if subjectHash == otherSubjectHash {
		t.Fatal("hash must include subject id")
	}
}

func TestAddGoatIdentifierValidationAndCommand(t *testing.T) {
	identifierID := "30000000-0000-4000-8000-000000000001"
	repo := &fakeRepo{
		addIdentifierResult: &ports.AdminGoatMutationResult{
			Goat:        summary(goatA, "G-000001", "clean"),
			Identifiers: []domain.GoatIdentifier{identifier("rfid", " rfid-synthetic-001 ", "global:rfid", "active", time.Now().UTC())},
			Decision: domain.DecisionRecordSummary{
				DecisionID:     "50000000-0000-4000-8000-000000000101",
				DecisionType:   "attach_identifier",
				DecisionResult: "identifier_attached",
				DecisionState:  "approved",
				PolicyVersion:  "phase1-identifier-v1",
				CreatedAt:      time.Now().UTC(),
			},
			Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000101", EventType: "goat.identifier.added"}},
		},
	}
	repo.addIdentifierResult.Identifiers[0].IdentifierID = identifierID
	svc := NewService(repo)

	response, err := svc.AddGoatIdentifier(context.Background(), validAddIdentifierInput())
	if err != nil {
		t.Fatalf("AddGoatIdentifier: %v", err)
	}
	if response.Decision.DecisionType != "attach_identifier" || len(response.Events) != 1 || response.Events[0].EventType != "goat.identifier.added" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if repo.lastAddIdentifierCmd.NormalizedValue != "RFID-SYNTHETIC-001" {
		t.Fatalf("normalized value = %q", repo.lastAddIdentifierCmd.NormalizedValue)
	}
	wantKey := testTenant + ":" + addGoatIdentifierCommand + ":" + goatA + ":idem-add-0001"
	if repo.lastAddIdentifierCmd.StoredIdempotencyKey != wantKey {
		t.Fatalf("stored idempotency key = %q, want %q", repo.lastAddIdentifierCmd.StoredIdempotencyKey, wantKey)
	}
	if repo.lastAddIdentifierCmd.RequestHash == "" || repo.lastAddIdentifierCmd.RowVersion != 1 || repo.lastAddIdentifierCmd.ScopeKey != "global:rfid" {
		t.Fatalf("command not normalized: %#v", repo.lastAddIdentifierCmd)
	}
	if repo.lastAddIdentifierCmd.IsPrimaryForGoat {
		t.Fatalf("is_primary_for_goat default = true")
	}
	if response.Idempotency.IdempotencyKey != "idem-add-0001" {
		t.Fatalf("client idempotency key not returned: %#v", response.Idempotency)
	}
}

func TestAddGoatIdentifierRejectsOldEvidenceIDsAndMissingScope(t *testing.T) {
	svc := NewService(&fakeRepo{})
	input := validAddIdentifierInput()
	input.RawBody = []byte(`{"identifier_type":"rfid","identifier_value":"RFID-SYNTHETIC-001","scope_key":"global:rfid","evidence_ids":["synthetic-row-1"],"row_version":1}`)
	_, err := svc.AddGoatIdentifier(context.Background(), input)
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 400 || appErr.Code != "invalid_json" {
		t.Fatalf("expected invalid_json for old evidence_ids, got %v", err)
	}

	input = validAddIdentifierInput()
	input.RawBody = []byte(`{"identifier_type":"rfid","identifier_value":"RFID-SYNTHETIC-001","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}],"row_version":1}`)
	_, err = svc.AddGoatIdentifier(context.Background(), input)
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 400 || appErr.Code != "invalid_scope_key" {
		t.Fatalf("expected invalid_scope_key, got %v", err)
	}
}

func TestRetireGoatIdentifierValidationAndCommand(t *testing.T) {
	identifierID := "30000000-0000-4000-8000-000000000001"
	repo := &fakeRepo{
		retireIdentifierResult: &ports.AdminGoatMutationResult{
			Goat:        summary(goatA, "G-000001", "clean"),
			Identifiers: []domain.GoatIdentifier{identifier("old_tag", "1900", "park:CBE", "retired", time.Now().UTC())},
			Decision: domain.DecisionRecordSummary{
				DecisionID:     "50000000-0000-4000-8000-000000000102",
				DecisionType:   "retire_identifier",
				DecisionResult: "identifier_retired",
				DecisionState:  "approved",
				PolicyVersion:  "phase1-identifier-v1",
				CreatedAt:      time.Now().UTC(),
			},
			Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000102", EventType: "goat.identifier.retired"}},
		},
	}
	repo.retireIdentifierResult.Identifiers[0].IdentifierID = identifierID
	svc := NewService(repo)

	response, err := svc.RetireGoatIdentifier(context.Background(), validRetireIdentifierInput())
	if err != nil {
		t.Fatalf("RetireGoatIdentifier: %v", err)
	}
	if response.Decision.DecisionType != "retire_identifier" || len(response.Events) != 1 || response.Events[0].EventType != "goat.identifier.retired" {
		t.Fatalf("unexpected response: %#v", response)
	}
	wantKey := testTenant + ":" + retireGoatIdentifierCommand + ":" + goatA + ":" + identifierID + ":idem-retire-0001"
	if repo.lastRetireIdentifierCmd.StoredIdempotencyKey != wantKey {
		t.Fatalf("stored idempotency key = %q, want %q", repo.lastRetireIdentifierCmd.StoredIdempotencyKey, wantKey)
	}
	if repo.lastRetireIdentifierCmd.RequestHash == "" || repo.lastRetireIdentifierCmd.RowVersion != 2 || repo.lastRetireIdentifierCmd.Reason != "synthetic retire reason" {
		t.Fatalf("command not normalized: %#v", repo.lastRetireIdentifierCmd)
	}
}

func TestRetireGoatIdentifierIdempotencyConflict(t *testing.T) {
	repo := &fakeRepo{retireIdentifierErr: ports.ErrIdempotencyConflict}
	svc := NewService(repo)
	_, err := svc.RetireGoatIdentifier(context.Background(), validRetireIdentifierInput())
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 || appErr.Code != "idempotency_conflict" {
		t.Fatalf("expected idempotency conflict app error, got %v", err)
	}
}

func validAddIdentifierInput() AddGoatIdentifierInput {
	return AddGoatIdentifierInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-add-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"identifier_type":"rfid","identifier_value":" rfid-synthetic-001 ","scope_key":"global:rfid","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":1}`),
	}
}

func validRetireIdentifierInput() RetireGoatIdentifierInput {
	return RetireGoatIdentifierInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-retire-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		IdentifierID:   "30000000-0000-4000-8000-000000000001",
		RawBody:        []byte(`{"reason":"synthetic retire reason","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}],"row_version":2}`),
	}
}

type fakeRepo struct {
	goats                   map[string]*domain.GoatPassport
	matches                 []domain.IdentifierMatch
	conflictID              string
	addIdentifierResult     *ports.AdminGoatMutationResult
	addIdentifierErr        error
	retireIdentifierResult  *ports.AdminGoatMutationResult
	retireIdentifierErr     error
	lastAddIdentifierCmd    ports.AddGoatIdentifierCommand
	lastRetireIdentifierCmd ports.RetireGoatIdentifierCommand
}

func (f *fakeRepo) GetGoatByID(_ context.Context, _ string, goatID string) (*domain.GoatPassport, error) {
	goat, ok := f.goats[goatID]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return goat, nil
}

func (f *fakeRepo) GetGoatByDisplayID(_ context.Context, _ string, displayID string) (*domain.GoatPassport, error) {
	for _, goat := range f.goats {
		if goat.DisplayID == displayID {
			return goat, nil
		}
	}
	return nil, ports.ErrNotFound
}

func (f *fakeRepo) SearchGoats(context.Context, ports.SearchGoatsParams) ([]domain.GoatSummary, *string, error) {
	return nil, nil, nil
}

func (f *fakeRepo) FindIdentifierMatches(context.Context, ports.ResolveIdentifierParams) ([]domain.IdentifierMatch, error) {
	return f.matches, nil
}

func (f *fakeRepo) FindOpenConflictForIdentifier(context.Context, string, string, string, string) (*string, error) {
	if f.conflictID == "" {
		return nil, nil
	}
	return &f.conflictID, nil
}

func (f *fakeRepo) GetGoatTimeline(context.Context, ports.GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error) {
	return []domain.GoatTimelineEvent{}, nil, nil
}

func (f *fakeRepo) AddGoatIdentifier(_ context.Context, cmd ports.AddGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	f.lastAddIdentifierCmd = cmd
	if f.addIdentifierErr != nil {
		return nil, f.addIdentifierErr
	}
	if f.addIdentifierResult != nil {
		return f.addIdentifierResult, nil
	}
	return &ports.AdminGoatMutationResult{
		Goat: summary(cmd.GoatID, "G-000001", "clean"),
		Identifiers: []domain.GoatIdentifier{{
			IdentifierID:     "30000000-0000-4000-8000-000000000001",
			IdentifierType:   cmd.IdentifierType,
			IdentifierValue:  cmd.IdentifierValue,
			ScopeKey:         cmd.ScopeKey,
			Status:           "active",
			IsPrimaryForGoat: cmd.IsPrimaryForGoat,
			ValidFrom:        time.Now().UTC(),
		}},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000101",
			DecisionType:   "attach_identifier",
			DecisionResult: "identifier_attached",
			DecisionState:  "approved",
			PolicyVersion:  "phase1-identifier-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000101", EventType: "goat.identifier.added"}},
	}, nil
}

func (f *fakeRepo) RetireGoatIdentifier(_ context.Context, cmd ports.RetireGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
	f.lastRetireIdentifierCmd = cmd
	if f.retireIdentifierErr != nil {
		return nil, f.retireIdentifierErr
	}
	if f.retireIdentifierResult != nil {
		return f.retireIdentifierResult, nil
	}
	validTo := time.Now().UTC()
	return &ports.AdminGoatMutationResult{
		Goat: summary(cmd.GoatID, "G-000001", "clean"),
		Identifiers: []domain.GoatIdentifier{{
			IdentifierID:     cmd.IdentifierID,
			IdentifierType:   "old_tag",
			IdentifierValue:  "1900",
			ScopeKey:         "park:CBE",
			Status:           "retired",
			IsPrimaryForGoat: true,
			ValidFrom:        validTo.Add(-time.Hour),
			ValidTo:          &validTo,
		}},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000102",
			DecisionType:   "retire_identifier",
			DecisionResult: "identifier_retired",
			DecisionState:  "approved",
			PolicyVersion:  "phase1-identifier-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000102", EventType: "goat.identifier.retired"}},
	}, nil
}

func (f *fakeRepo) Ping(context.Context) error { return nil }

func passport(goatID, displayID, identityState string, mergedInto *string) *domain.GoatPassport {
	s := summary(goatID, displayID, identityState)
	return &domain.GoatPassport{
		GoatID:           goatID,
		DisplayID:        displayID,
		Species:          "goat",
		IdentityState:    identityState,
		Summary:          s,
		Identifiers:      []domain.GoatIdentifier{},
		EvidenceRefs:     []domain.EvidenceRef{},
		FamilyRefs:       []domain.FamilyRef{},
		MergedIntoGoatID: mergedInto,
		RowVersion:       1,
	}
}

func summary(goatID, displayID, identityState string) domain.GoatSummary {
	return domain.GoatSummary{
		GoatID:          goatID,
		DisplayID:       displayID,
		LifecycleStatus: "alive",
		IdentityState:   identityState,
		LocationPath:    domain.LocationPath{Display: "Synthetic CBE"},
		Warnings:        []domain.Warning{},
	}
}

func identifier(identifierType, value, scope, status string, validFrom time.Time) domain.GoatIdentifier {
	return domain.GoatIdentifier{
		IdentifierID:     "30000000-0000-4000-8000-000000000001",
		IdentifierType:   identifierType,
		IdentifierValue:  value,
		ScopeKey:         scope,
		Status:           status,
		IsPrimaryForGoat: status == "active",
		ValidFrom:        validFrom,
	}
}

func strPtr(value string) *string {
	return &value
}

func conflictForTest(name string) string {
	if name == "multiple_matches_with_conflict" {
		return conflictID
	}
	return ""
}
