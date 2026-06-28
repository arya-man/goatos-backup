package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	testTenant        = "00000000-0000-4000-8000-000000000001"
	testActor         = "90000000-0000-4000-8000-000000000001"
	testTrace         = "trace-test"
	testBulkHash      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testOtherBulkHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	goatA             = "10000000-0000-4000-8000-000000000001"
	goatB             = "10000000-0000-4000-8000-000000000002"
	mergedGoat        = "10000000-0000-4000-8000-000000000003"
	survivorGoat      = "10000000-0000-4000-8000-000000000004"
	conflictID        = "20000000-0000-4000-8000-000000000001"
	testPark          = "00000000-0000-4000-8000-000000003001"
	testShed          = "00000000-0000-4000-8000-000000004001"
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

func TestCreateAdminGoatPassesIdempotencyIntoValidation(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	_, err := svc.CreateAdminGoat(context.Background(), CreateAdminGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-create-0001",
		TraceID:        testTrace,
		RawBody:        validAdminGoatCreateRaw("RFID-CREATE-001"),
	})
	if err != nil {
		t.Fatalf("CreateAdminGoat: %v", err)
	}
	if len(repo.validateAdminGoatCreateCmds) != 1 {
		t.Fatalf("validate calls = %d, want 1", len(repo.validateAdminGoatCreateCmds))
	}
	got := repo.validateAdminGoatCreateCmds[0]
	wantKey := testTenant + ":" + createAdminGoatCommand + ":idem-create-0001"
	if got.StoredIdempotencyKey != wantKey {
		t.Fatalf("validation idempotency key = %q, want %q", got.StoredIdempotencyKey, wantKey)
	}
	if got.RequestHash == "" {
		t.Fatal("validation request hash must be set before business conflict checks")
	}
	if len(repo.createAdminGoatCmds) != 1 {
		t.Fatalf("create calls = %d, want 1", len(repo.createAdminGoatCmds))
	}
}

func TestCommitAdminGoatBulkUsesStableRowIdempotencyKey(t *testing.T) {
	seenHashByKey := map[string]string{}
	repo := &fakeRepo{
		validateAdminGoatCreateFunc: func(cmd ports.ValidateAdminGoatCreateCommand) (ports.AdminGoatCreateValidation, error) {
			if previous, ok := seenHashByKey[cmd.StoredIdempotencyKey]; ok && previous != cmd.RequestHash {
				return ports.AdminGoatCreateValidation{}, ports.ErrIdempotencyConflict
			}
			seenHashByKey[cmd.StoredIdempotencyKey] = cmd.RequestHash
			return defaultAdminGoatCreateValidation(cmd), nil
		},
	}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	input := CommitAdminGoatBulkInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "bulk-idem-0001",
		TraceID:        testTrace,
		RawBody:        validAdminGoatBulkCommitRaw("RFID-BULK-001"),
	}
	first, err := svc.CommitAdminGoatBulkImport(context.Background(), input)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if first.Summary.Created != 1 || len(repo.createAdminGoatCmds) != 1 {
		t.Fatalf("first commit summary=%#v createCalls=%d", first.Summary, len(repo.createAdminGoatCmds))
	}
	wantRowKey := "bulk-idem-0001:row:1"
	if repo.createAdminGoatCmds[0].ClientIdempotencyKey != wantRowKey {
		t.Fatalf("row key = %q, want %q", repo.createAdminGoatCmds[0].ClientIdempotencyKey, wantRowKey)
	}

	input.RawBody = validAdminGoatBulkCommitRaw("RFID-BULK-CHANGED")
	second, err := svc.CommitAdminGoatBulkImport(context.Background(), input)
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if second.Summary.Failed != 1 || second.Summary.Created != 0 {
		t.Fatalf("second summary = %#v, want one failed row", second.Summary)
	}
	if len(repo.createAdminGoatCmds) != 1 {
		t.Fatalf("changed replay should not create another goat, createCalls=%d", len(repo.createAdminGoatCmds))
	}
	if len(second.Rows) != 1 || len(second.Rows[0].Errors) != 1 || second.Rows[0].Errors[0].Code != "commit_failed" {
		t.Fatalf("second row errors = %#v", second.Rows)
	}
}

func TestCommitAdminGoatBulkPreservesPreviewRowNumber(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	input := CommitAdminGoatBulkInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "bulk-idem-0002",
		TraceID:        testTrace,
		RawBody:        validAdminGoatBulkCommitRowRaw(5, "RFID-BULK-ROW-005"),
	}
	resp, err := svc.CommitAdminGoatBulkImport(context.Background(), input)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if resp.Summary.Created != 1 || len(resp.Rows) != 1 || resp.Rows[0].RowNumber != 5 {
		t.Fatalf("response=%#v, want one created row with source row number 5", resp)
	}
	wantRowKey := "bulk-idem-0002:row:5"
	if repo.createAdminGoatCmds[0].ClientIdempotencyKey != wantRowKey {
		t.Fatalf("row key = %q, want %q", repo.createAdminGoatCmds[0].ClientIdempotencyKey, wantRowKey)
	}
}

func TestCommitAdminGoatBulkRejectsMissingPreviewToken(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	_, err := svc.CommitAdminGoatBulkImport(context.Background(), CommitAdminGoatBulkInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "bulk-no-preview-token",
		TraceID:        testTrace,
		RawBody:        []byte(fmt.Sprintf(`{"rows":[%s],"file_hash":%q}`, validAdminGoatCreateRaw("RFID-NO-TOKEN"), testBulkHash)),
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_preview_token" {
		t.Fatalf("err = %v, want invalid_preview_token", err)
	}
	if len(repo.createAdminGoatCmds) != 0 {
		t.Fatalf("missing preview token must not create goats, calls=%d", len(repo.createAdminGoatCmds))
	}
}

func TestCommitAdminGoatBulkRejectsRowsAlteredAfterPreview(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	token := validAdminGoatBulkPreviewToken("RFID-PREVIEWED")
	_, err := svc.CommitAdminGoatBulkImport(context.Background(), CommitAdminGoatBulkInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "bulk-altered-preview",
		TraceID:        testTrace,
		RawBody:        []byte(fmt.Sprintf(`{"rows":[%s],"file_hash":%q,"preview_token":%q}`, validAdminGoatCreateRaw("RFID-ALTERED"), testBulkHash, token)),
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_preview_token" {
		t.Fatalf("err = %v, want invalid_preview_token", err)
	}
	if len(repo.createAdminGoatCmds) != 0 {
		t.Fatalf("altered preview rows must not create goats, calls=%d", len(repo.createAdminGoatCmds))
	}
}

func TestCommitAdminGoatBulkRejectsUnrelatedValidFileHash(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	token := validAdminGoatBulkPreviewToken("RFID-FILE-HASH")
	_, err := svc.CommitAdminGoatBulkImport(context.Background(), CommitAdminGoatBulkInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "bulk-wrong-file-hash",
		TraceID:        testTrace,
		RawBody:        []byte(fmt.Sprintf(`{"rows":[%s],"file_hash":%q,"preview_token":%q}`, validAdminGoatCreateRaw("RFID-FILE-HASH"), testOtherBulkHash, token)),
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_preview_token" {
		t.Fatalf("err = %v, want invalid_preview_token", err)
	}
	if len(repo.createAdminGoatCmds) != 0 {
		t.Fatalf("unrelated file hash must not create goats, calls=%d", len(repo.createAdminGoatCmds))
	}
}

func TestPreviewAdminGoatBulkParsesTempFieldIDAndEntryDate(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	csv := "Farm,Temp field ID,Park,Shed,Sex,Origin,Management stage,Entry date\nMain Farm,TMP-KID-001,CBE,K1,female,birth,K1,2026-06-15\n"
	resp, err := svc.PreviewAdminGoatBulkImport(context.Background(), PreviewAdminGoatBulkInput{
		TenantID: testTenant,
		TraceID:  testTrace,
		RawBody:  []byte(fmt.Sprintf(`{"csv":%q,"file_hash":%q}`, csv, testBulkHash)),
	})
	if err != nil {
		t.Fatalf("PreviewAdminGoatBulkImport: %v", err)
	}
	if resp.PreviewToken == "" {
		t.Fatalf("preview token must be returned for commit binding")
	}
	if resp.Summary.CreateReady != 1 || len(resp.Rows) != 1 {
		t.Fatalf("summary=%#v rows=%d", resp.Summary, len(resp.Rows))
	}
	row := resp.Rows[0]
	if row.Normalized == nil || row.Normalized.TempFieldID == nil || *row.Normalized.TempFieldID != "TMP-KID-001" {
		t.Fatalf("normalized temp_field_id = %#v", row.Normalized)
	}
	if row.Normalized.EntryDate != "2026-06-15" {
		t.Fatalf("entry_date = %q", row.Normalized.EntryDate)
	}
	if row.Normalized.ManagementStage == nil || *row.Normalized.ManagementStage != "K1" {
		t.Fatalf("management_stage = %#v", row.Normalized.ManagementStage)
	}
	if len(repo.validateAdminGoatCreateCmds) != 1 || repo.validateAdminGoatCreateCmds[0].ParkCode == nil || *repo.validateAdminGoatCreateCmds[0].ParkCode != "CBE" {
		t.Fatalf("validation command did not receive park code: %#v", repo.validateAdminGoatCreateCmds)
	}
}

func TestPreviewAdminGoatBulkFlagsDuplicateRowsWithoutFailingFile(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	csv := "RFID,Park,Shed,Sex,Origin,Management stage,Entry date,Weight(kg)\nDUP-RFID-001,CBE,K1,female,birth,K1,2026-06-15,22.5\n\nDUP-RFID-001,CBE,K1,female,birth,K1,2026-06-15\n"
	resp, err := svc.PreviewAdminGoatBulkImport(context.Background(), PreviewAdminGoatBulkInput{
		TenantID: testTenant,
		TraceID:  testTrace,
		RawBody:  []byte(fmt.Sprintf(`{"csv":%q,"file_hash":%q}`, csv, testBulkHash)),
	})
	if err != nil {
		t.Fatalf("PreviewAdminGoatBulkImport: %v", err)
	}
	if resp.Summary.CreateReady != 1 || resp.Summary.RequiresReview != 1 || resp.Summary.Total != 2 {
		t.Fatalf("summary=%#v, want one create-ready and one review", resp.Summary)
	}
	if len(resp.Rows) != 2 || resp.Rows[1].RowNumber != 4 || len(resp.Rows[1].Errors) != 1 || resp.Rows[1].Errors[0].Code != "duplicate_in_file" {
		t.Fatalf("duplicate row result = %#v", resp.Rows)
	}
	if len(repo.validateAdminGoatCreateCmds) != 1 {
		t.Fatalf("validate calls = %d, want only the first create-ready row validated", len(repo.validateAdminGoatCreateCmds))
	}
}

func TestPreviewAdminGoatBulkWrongTemplateReturnsRowErrors(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	csv := "Wrong column,Another wrong column\nvalue,still wrong\n"
	resp, err := svc.PreviewAdminGoatBulkImport(context.Background(), PreviewAdminGoatBulkInput{
		TenantID: testTenant,
		TraceID:  testTrace,
		RawBody:  []byte(fmt.Sprintf(`{"csv":%q,"file_hash":%q}`, csv, testBulkHash)),
	})
	if err != nil {
		t.Fatalf("PreviewAdminGoatBulkImport: %v", err)
	}
	if resp.Summary.CreateReady != 0 || resp.Summary.RequiresReview != 1 || resp.Summary.Total != 1 {
		t.Fatalf("summary=%#v, want one row-level review", resp.Summary)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].RowNumber != 2 || len(resp.Rows[0].Errors) == 0 {
		t.Fatalf("wrong-template row result = %#v", resp.Rows)
	}
	if len(repo.validateAdminGoatCreateCmds) != 0 {
		t.Fatalf("wrong-template row should not reach business validation, calls=%d", len(repo.validateAdminGoatCreateCmds))
	}
}

func TestMoveGoatBuildsLifecycleCommand(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{}}
	svc := NewService(repo)
	input := MoveGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-move-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"park_id":"00000000-0000-4000-8000-000000003001","shed_id":"00000000-0000-4000-8000-000000004001","reason":"shifted to target vaccination shed","evidence_refs":[{"evidence_type":"source_record","evidence_id":"move-ticket-1"}],"row_version":7}`),
	}
	result, err := svc.MoveGoat(context.Background(), input)
	if err != nil {
		t.Fatalf("MoveGoat error = %v", err)
	}
	if result.Events[0].EventType != "goat.location.changed" {
		t.Fatalf("event type = %s", result.Events[0].EventType)
	}
	if repo.lastMoveGoatCmd.ToShedID != testShed || repo.lastMoveGoatCmd.RowVersion != 7 {
		t.Fatalf("move command = %#v", repo.lastMoveGoatCmd)
	}
	if repo.lastMoveGoatCmd.StoredIdempotencyKey == "" || repo.lastMoveGoatCmd.RequestHash == "" {
		t.Fatalf("expected idempotency and request hash, got %#v", repo.lastMoveGoatCmd)
	}
}

func TestExitGoatBuildsLifecycleCommand(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{}}
	svc := NewService(repo)
	input := ExitGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-exit-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"lifecycle_status":"sold","exit_reason":"sold","reason":"sold after approved disposal","evidence_refs":[{"evidence_type":"source_record","evidence_id":"exit-ticket-1"}],"row_version":8}`),
	}
	result, err := svc.ExitGoat(context.Background(), input)
	if err != nil {
		t.Fatalf("ExitGoat error = %v", err)
	}
	if result.Events[0].EventType != "goat.exited" {
		t.Fatalf("event type = %s", result.Events[0].EventType)
	}
	if repo.lastExitGoatCmd.LifecycleStatus != "sold" || repo.lastExitGoatCmd.RowVersion != 8 {
		t.Fatalf("exit command = %#v", repo.lastExitGoatCmd)
	}
}

func TestStageGoatBuildsLifecycleCommand(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{}}
	svc := NewService(repo)
	input := StageGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-stage-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"management_stage":"weaner","reason":"stage confirmed by supervisor","evidence_refs":[{"evidence_type":"source_record","evidence_id":"stage-ticket-1"}],"row_version":9}`),
	}
	result, err := svc.StageGoat(context.Background(), input)
	if err != nil {
		t.Fatalf("StageGoat error = %v", err)
	}
	if result.Events[0].EventType != "goat.stage_changed" {
		t.Fatalf("event type = %s", result.Events[0].EventType)
	}
	if repo.lastStageGoatCmd.ManagementStage != "weaner" || repo.lastStageGoatCmd.RowVersion != 9 {
		t.Fatalf("stage command = %#v", repo.lastStageGoatCmd)
	}
	if repo.lastStageGoatCmd.StoredIdempotencyKey == "" || repo.lastStageGoatCmd.RequestHash == "" {
		t.Fatalf("expected idempotency and request hash, got %#v", repo.lastStageGoatCmd)
	}
}

func TestHealthGoatBuildsLifecycleCommand(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{}}
	svc := NewService(repo)
	input := HealthGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-health-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"health_status":"healthy","reason":"recovered after PHC treatment","evidence_refs":[{"evidence_type":"source_record","evidence_id":"health-ticket-1"}],"row_version":10}`),
	}
	result, err := svc.HealthGoat(context.Background(), input)
	if err != nil {
		t.Fatalf("HealthGoat error = %v", err)
	}
	if result.Events[0].EventType != "goat.health.changed" {
		t.Fatalf("event type = %s", result.Events[0].EventType)
	}
	if repo.lastHealthGoatCmd.HealthStatus != "healthy" || repo.lastHealthGoatCmd.RowVersion != 10 {
		t.Fatalf("health command = %#v", repo.lastHealthGoatCmd)
	}
	if repo.lastHealthGoatCmd.StoredIdempotencyKey == "" || repo.lastHealthGoatCmd.RequestHash == "" {
		t.Fatalf("expected idempotency and request hash, got %#v", repo.lastHealthGoatCmd)
	}
}

func TestHealthGoatRejectsCriticalTargetBeforeRepository(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{}}
	svc := NewService(repo)

	_, err := svc.HealthGoat(context.Background(), HealthGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-health-critical-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"health_status":"quarantine","reason":"suspected contagious disease","evidence_refs":[{"evidence_type":"source_record","evidence_id":"health-ticket-2"}],"row_version":10}`),
	})
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("HealthGoat error = %v, want app error", err)
	}
	if appErr.Code != "critical_health_transition_requires_guardrail" || appErr.HTTPStatus != 409 {
		t.Fatalf("app error = %#v, want guardrail-required 409", appErr)
	}
	if repo.lastHealthGoatCmd.GoatID != "" {
		t.Fatalf("repository should not be called for critical target, got %#v", repo.lastHealthGoatCmd)
	}
}

func TestExitGoatRejectsDeathBeforeRepository(t *testing.T) {
	repo := &fakeRepo{goats: map[string]*domain.GoatPassport{}}
	svc := NewService(repo)

	_, err := svc.ExitGoat(context.Background(), ExitGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-exit-death-0001",
		TraceID:        testTrace,
		GoatID:         goatA,
		RawBody:        []byte(`{"lifecycle_status":"dead","exit_reason":"died","reason":"death certificate reported by PHC supervisor","evidence_refs":[{"evidence_type":"source_record","evidence_id":"death-ticket-1"}],"row_version":10}`),
	})
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("ExitGoat error = %v, want app error", err)
	}
	if appErr.Code != "critical_death_transition_requires_guardrail" || appErr.HTTPStatus != 409 {
		t.Fatalf("app error = %#v, want death guardrail-required 409", appErr)
	}
	if repo.lastExitGoatCmd.GoatID != "" {
		t.Fatalf("repository should not be called for death exit, got %#v", repo.lastExitGoatCmd)
	}
}

func TestMapRepoErrReturnsDeathGuardrailMessage(t *testing.T) {
	err := mapRepoErr(ports.ErrCriticalDeathGuardrailRequired)
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("mapRepoErr error = %v, want app error", err)
	}
	if appErr.Code != "critical_death_transition_requires_guardrail" || appErr.HTTPStatus != 409 {
		t.Fatalf("app error = %#v, want death guardrail-required 409", appErr)
	}
	if !errors.Is(ports.ErrCriticalDeathGuardrailRequired, ports.ErrGuardrailRequired) {
		t.Fatal("death guardrail error should preserve generic guardrail sentinel")
	}
}

func TestMoveExitStageAndHealthRequireEvidence(t *testing.T) {
	svc := NewService(&fakeRepo{goats: map[string]*domain.GoatPassport{}})
	_, err := svc.MoveGoat(context.Background(), MoveGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-move-0002",
		GoatID:         goatA,
		RawBody:        []byte(`{"park_id":"00000000-0000-4000-8000-000000003001","shed_id":"00000000-0000-4000-8000-000000004001","reason":"missing evidence","row_version":1}`),
	})
	if err == nil {
		t.Fatal("MoveGoat expected evidence error")
	}
	_, err = svc.ExitGoat(context.Background(), ExitGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-exit-0002",
		GoatID:         goatA,
		RawBody:        []byte(`{"lifecycle_status":"sold","exit_reason":"sold","reason":"missing evidence","row_version":1}`),
	})
	if err == nil {
		t.Fatal("ExitGoat expected evidence error")
	}
	_, err = svc.StageGoat(context.Background(), StageGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-stage-0002",
		GoatID:         goatA,
		RawBody:        []byte(`{"management_stage":"yearling","reason":"missing evidence","row_version":1}`),
	})
	if err == nil {
		t.Fatal("StageGoat expected evidence error")
	}
	_, err = svc.HealthGoat(context.Background(), HealthGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-health-0002",
		GoatID:         goatA,
		RawBody:        []byte(`{"health_status":"healthy","reason":"missing evidence","row_version":1}`),
	})
	if err == nil {
		t.Fatal("HealthGoat expected evidence error")
	}
}

func TestExitGoatRejectsMergeOrMismatchedReason(t *testing.T) {
	svc := NewService(&fakeRepo{goats: map[string]*domain.GoatPassport{}})
	_, err := svc.ExitGoat(context.Background(), ExitGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-exit-merged",
		GoatID:         goatA,
		RawBody:        []byte(`{"lifecycle_status":"merged","exit_reason":"lost","reason":"bad merge through exit","evidence_refs":[{"evidence_type":"source_record","evidence_id":"exit-ticket-merged"}],"row_version":1}`),
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_lifecycle_status" {
		t.Fatalf("merged exit err = %v, want invalid_lifecycle_status", err)
	}
	_, err = svc.ExitGoat(context.Background(), ExitGoatInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "idem-exit-mismatch",
		GoatID:         goatA,
		RawBody:        []byte(`{"lifecycle_status":"dead","exit_reason":"sold","reason":"bad lifecycle reason pair","evidence_refs":[{"evidence_type":"source_record","evidence_id":"exit-ticket-mismatch"}],"row_version":1}`),
	})
	if !errors.As(err, &appErr) || appErr.Code != "invalid_exit_reason" {
		t.Fatalf("mismatched exit err = %v, want invalid_exit_reason", err)
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

func validAdminGoatCreateRaw(rfid string) []byte {
	return []byte(fmt.Sprintf(`{"rfid":%q,"park_id":%q,"shed_id":%q,"sex":"female","origin_type":"procured","entry_date":"2026-06-01","evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1"}]}`, rfid, testPark, testShed))
}

func validAdminGoatBulkCommitRaw(rfid string) []byte {
	return []byte(fmt.Sprintf(`{"rows":[%s],"file_hash":%q,"preview_token":%q}`, validAdminGoatCreateRaw(rfid), testBulkHash, validAdminGoatBulkPreviewToken(rfid)))
}

func validAdminGoatBulkCommitRowRaw(rowNumber int, rfid string) []byte {
	return []byte(fmt.Sprintf(`{"rows":[{"row_number":%d,"normalized":%s}],"file_hash":%q,"preview_token":%q}`, rowNumber, validAdminGoatCreateRaw(rfid), testBulkHash, validAdminGoatBulkPreviewTokenForRow(rowNumber, rfid)))
}

func validAdminGoatBulkPreviewToken(rfid string) string {
	return validAdminGoatBulkPreviewTokenForRow(1, rfid)
}

func validAdminGoatBulkPreviewTokenForRow(rowNumber int, rfid string) string {
	token, err := signAdminGoatBulkPreviewWithKey(testTenant, testBulkHash, []domain.AdminGoatBulkCommitRow{{
		RowNumber:  rowNumber,
		Normalized: validAdminGoatCreateRequest(rfid),
	}}, defaultAdminGoatBulkPreviewSigningKey)
	if err != nil {
		panic(err)
	}
	return token
}

func validAdminGoatCreateRequest(rfid string) *domain.AdminGoatCreateRequest {
	parkID := testPark
	shedID := testShed
	estimated := true
	return &domain.AdminGoatCreateRequest{
		RFID:         &rfid,
		ParkID:       &parkID,
		ShedID:       &shedID,
		Sex:          "female",
		OriginType:   "procured",
		EntryDate:    "2026-06-01",
		DOBEstimated: &estimated,
		EvidenceRefs: []domain.EvidenceRef{{
			EvidenceType: "source_record",
			EvidenceID:   "synthetic-row-1",
		}},
	}
}

type fakeRepo struct {
	goats                       map[string]*domain.GoatPassport
	matches                     []domain.IdentifierMatch
	conflictID                  string
	addIdentifierResult         *ports.AdminGoatMutationResult
	addIdentifierErr            error
	retireIdentifierResult      *ports.AdminGoatMutationResult
	retireIdentifierErr         error
	lastAddIdentifierCmd        ports.AddGoatIdentifierCommand
	lastRetireIdentifierCmd     ports.RetireGoatIdentifierCommand
	lastMoveGoatCmd             ports.MoveGoatCommand
	lastExitGoatCmd             ports.ExitGoatCommand
	lastStageGoatCmd            ports.StageGoatCommand
	lastHealthGoatCmd           ports.HealthGoatCommand
	validateAdminGoatCreateFunc func(ports.ValidateAdminGoatCreateCommand) (ports.AdminGoatCreateValidation, error)
	validateAdminGoatCreateCmds []ports.ValidateAdminGoatCreateCommand
	createAdminGoatResult       *ports.AdminGoatMutationResult
	createAdminGoatErr          error
	createAdminGoatCmds         []ports.CreateAdminGoatCommand
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

func (f *fakeRepo) MoveGoat(_ context.Context, cmd ports.MoveGoatCommand) (*ports.AdminGoatMutationResult, error) {
	f.lastMoveGoatCmd = cmd
	return &ports.AdminGoatMutationResult{
		Goat:        summary(cmd.GoatID, "G-000001", "clean"),
		Identifiers: []domain.GoatIdentifier{},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000201",
			DecisionType:   "move_goat",
			DecisionResult: "goat_moved",
			DecisionState:  "approved",
			PolicyVersion:  "goat-lifecycle-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000201", EventType: "goat.location.changed"}},
	}, nil
}

func (f *fakeRepo) ExitGoat(_ context.Context, cmd ports.ExitGoatCommand) (*ports.AdminGoatMutationResult, error) {
	f.lastExitGoatCmd = cmd
	out := summary(cmd.GoatID, "G-000001", "clean")
	out.LifecycleStatus = cmd.LifecycleStatus
	return &ports.AdminGoatMutationResult{
		Goat:        out,
		Identifiers: []domain.GoatIdentifier{},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000202",
			DecisionType:   "exit_goat",
			DecisionResult: "goat_exited",
			DecisionState:  "approved",
			PolicyVersion:  "goat-lifecycle-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000202", EventType: "goat.exited"}},
	}, nil
}

func (f *fakeRepo) StageGoat(_ context.Context, cmd ports.StageGoatCommand) (*ports.AdminGoatMutationResult, error) {
	f.lastStageGoatCmd = cmd
	out := summary(cmd.GoatID, "G-000001", "clean")
	out.ManagementStage = strPtr(cmd.ManagementStage)
	return &ports.AdminGoatMutationResult{
		Goat:        out,
		Identifiers: []domain.GoatIdentifier{},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000203",
			DecisionType:   "stage_goat",
			DecisionResult: "goat_stage_changed",
			DecisionState:  "approved",
			PolicyVersion:  "goat-lifecycle-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000203", EventType: "goat.stage_changed"}},
	}, nil
}

func (f *fakeRepo) HealthGoat(_ context.Context, cmd ports.HealthGoatCommand) (*ports.AdminGoatMutationResult, error) {
	f.lastHealthGoatCmd = cmd
	out := summary(cmd.GoatID, "G-000001", "clean")
	out.HealthStatus = strPtr(cmd.HealthStatus)
	return &ports.AdminGoatMutationResult{
		Goat:        out,
		Identifiers: []domain.GoatIdentifier{},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000204",
			DecisionType:   "health_goat",
			DecisionResult: "goat_health_changed",
			DecisionState:  "approved",
			PolicyVersion:  "goat-lifecycle-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events: []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000204", EventType: "goat.health.changed"}},
	}, nil
}

func (f *fakeRepo) ValidateAdminGoatCreate(_ context.Context, cmd ports.ValidateAdminGoatCreateCommand) (ports.AdminGoatCreateValidation, error) {
	f.validateAdminGoatCreateCmds = append(f.validateAdminGoatCreateCmds, cmd)
	if f.validateAdminGoatCreateFunc != nil {
		return f.validateAdminGoatCreateFunc(cmd)
	}
	return defaultAdminGoatCreateValidation(cmd), nil
}

func (f *fakeRepo) CreateAdminGoat(_ context.Context, cmd ports.CreateAdminGoatCommand) (*ports.AdminGoatMutationResult, error) {
	f.createAdminGoatCmds = append(f.createAdminGoatCmds, cmd)
	if f.createAdminGoatErr != nil {
		return nil, f.createAdminGoatErr
	}
	if f.createAdminGoatResult != nil {
		return f.createAdminGoatResult, nil
	}
	return &ports.AdminGoatMutationResult{
		Goat:        summary("10000000-0000-4000-8000-000000000099", "G-000099", "clean"),
		Identifiers: []domain.GoatIdentifier{identifier(cmd.Identifiers[0].IdentifierType, cmd.Identifiers[0].IdentifierValue, cmd.Identifiers[0].ScopeKey, "active", time.Now().UTC())},
		Decision: domain.DecisionRecordSummary{
			DecisionID:     "50000000-0000-4000-8000-000000000199",
			DecisionType:   "create_goat",
			DecisionResult: "goat_created",
			DecisionState:  "approved",
			PolicyVersion:  "admin-goat-create-v1",
			CreatedAt:      time.Now().UTC(),
		},
		Events:           []domain.EventSummary{{EventID: "60000000-0000-4000-8000-000000000199", EventType: "goat.created"}},
		GenerationStatus: "queued",
	}, nil
}

func defaultAdminGoatCreateValidation(cmd ports.ValidateAdminGoatCreateCommand) ports.AdminGoatCreateValidation {
	parkID := testPark
	if cmd.ParkID != nil {
		parkID = *cmd.ParkID
	}
	shedID := testShed
	if cmd.ShedID != nil {
		shedID = *cmd.ShedID
	}
	return ports.AdminGoatCreateValidation{
		CustodianPartyID: "70000000-0000-4000-8000-000000000001",
		FarmID:           cmd.FarmID,
		ParkID:           parkID,
		ShedID:           shedID,
	}
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
