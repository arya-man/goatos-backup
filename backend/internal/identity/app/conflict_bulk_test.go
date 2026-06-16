package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const bulkConflictID = "30000000-0000-4000-8000-000000000001"

func bulkInput(rawBody string) BulkResolveConflictsInput {
	return BulkResolveConflictsInput{
		TenantID: testTenant,
		ActorID:  testActor,
		TraceID:  testTrace,
		RawBody:  []byte(rawBody),
	}
}

func TestBulkResolveConflictsHappyPathNormalizesCommand(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	body := fmt.Sprintf(`{"decision_type":"keep_passport_value","reason":"reviewed evidence","conflicts":[{"conflict_id":%q,"row_version":3}]}`, bulkConflictID)

	result, err := svc.BulkResolveConflicts(context.Background(), bulkInput(body))
	if err != nil {
		t.Fatalf("BulkResolveConflicts: %v", err)
	}
	if result.TraceID != testTrace || result.BulkRequestID != testTrace {
		t.Fatalf("trace/bulk_request not set from input: %#v", result)
	}
	if len(result.ResolvedConflictIDs) != 1 || result.ResolvedConflictIDs[0] != bulkConflictID {
		t.Fatalf("resolved ids = %#v", result.ResolvedConflictIDs)
	}
	if repo.lastBulkCmd.DecisionType != "keep_passport_value" || repo.lastBulkCmd.Reason != "reviewed evidence" {
		t.Fatalf("command not normalized: %#v", repo.lastBulkCmd)
	}
	if repo.lastBulkCmd.RowVersions[bulkConflictID] != 3 || repo.lastBulkCmd.BulkRequestID != testTrace {
		t.Fatalf("row_version/bulk_request not threaded: %#v", repo.lastBulkCmd)
	}
}

func TestBulkResolveConflictsValidationRejects(t *testing.T) {
	svc := NewService(&fakeRepo{})
	cases := []struct {
		name string
		body string
		code string
	}{
		{"invalid decision_type", `{"decision_type":"delete_everything","reason":"x","conflicts":[{"conflict_id":"` + bulkConflictID + `","row_version":1}]}`, "invalid_decision_type"},
		{"empty conflicts", `{"decision_type":"keep_passport_value","reason":"x","conflicts":[]}`, "missing_conflicts"},
		{"empty reason", `{"decision_type":"keep_passport_value","reason":"  ","conflicts":[{"conflict_id":"` + bulkConflictID + `","row_version":1}]}`, "invalid_reason"},
		{"invalid conflict_id", `{"decision_type":"keep_passport_value","reason":"x","conflicts":[{"conflict_id":"not-a-uuid","row_version":1}]}`, "invalid_conflict_id"},
		{"duplicate conflict_id", `{"decision_type":"keep_passport_value","reason":"x","conflicts":[{"conflict_id":"` + bulkConflictID + `","row_version":1},{"conflict_id":"` + bulkConflictID + `","row_version":2}]}`, "duplicate_conflict_id"},
		{"missing row_version", `{"decision_type":"keep_passport_value","reason":"x","conflicts":[{"conflict_id":"` + bulkConflictID + `"}]}`, "invalid_row_version"},
		{"zero row_version", `{"decision_type":"keep_passport_value","reason":"x","conflicts":[{"conflict_id":"` + bulkConflictID + `","row_version":0}]}`, "invalid_row_version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.BulkResolveConflicts(context.Background(), bulkInput(tc.body))
			var appErr *Error
			if !errors.As(err, &appErr) || appErr.HTTPStatus != 400 || appErr.Code != tc.code {
				t.Fatalf("want 400/%s, got %v", tc.code, err)
			}
		})
	}
}

func TestBulkResolveConflictsRejectsOverLimitAndBadActor(t *testing.T) {
	svc := NewService(&fakeRepo{})

	items := make([]string, 0, maxBulkResolveConflicts+1)
	for i := 0; i <= maxBulkResolveConflicts; i++ {
		items = append(items, fmt.Sprintf(`{"conflict_id":"30000000-0000-4000-8000-%012d","row_version":1}`, i))
	}
	overLimit := `{"decision_type":"keep_passport_value","reason":"x","conflicts":[` + strings.Join(items, ",") + `]}`
	_, err := svc.BulkResolveConflicts(context.Background(), bulkInput(overLimit))
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 400 || appErr.Code != "too_many_conflicts" {
		t.Fatalf("want 400/too_many_conflicts, got %v", err)
	}

	badActor := bulkInput(`{"decision_type":"keep_passport_value","reason":"x","conflicts":[{"conflict_id":"` + bulkConflictID + `","row_version":1}]}`)
	badActor.ActorID = "not-a-uuid"
	_, err = svc.BulkResolveConflicts(context.Background(), badActor)
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 400 || appErr.Code != "invalid_actor" {
		t.Fatalf("want 400/invalid_actor, got %v", err)
	}
}

func TestBulkResolveConflictsMapsRepoErrors(t *testing.T) {
	body := `{"decision_type":"use_legacy_value","reason":"x","conflicts":[{"conflict_id":"` + bulkConflictID + `","row_version":1}]}`
	cases := []struct {
		name   string
		repoErr error
		status int
		code   string
	}{
		{"not applicable", ports.ErrBulkDecisionNotApplicable, 400, "bulk_decision_not_applicable"},
		{"breed not canonical", ports.ErrBulkBreedNotCanonical, 400, "legacy_breed_not_canonical"},
		{"stale row version", ports.ErrWriteConflict, 409, "write_conflict"},
		{"missing conflict", ports.ErrNotFound, 404, "not_found_or_not_allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&fakeRepo{bulkErr: tc.repoErr})
			_, err := svc.BulkResolveConflicts(context.Background(), bulkInput(body))
			var appErr *Error
			if !errors.As(err, &appErr) || appErr.HTTPStatus != tc.status || appErr.Code != tc.code {
				t.Fatalf("want %d/%s, got %v", tc.status, tc.code, err)
			}
		})
	}
}
