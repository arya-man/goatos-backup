package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
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

type fakeRepo struct {
	goats      map[string]*domain.GoatPassport
	matches    []domain.IdentifierMatch
	conflictID string
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

func (f *fakeRepo) ListConflicts(context.Context, ports.ListConflictsParams) ([]domain.ConflictSummary, *string, error) {
	return nil, nil, nil
}

func (f *fakeRepo) GetConflict(context.Context, string, string) (*domain.ConflictDetailResult, error) {
	return nil, ports.ErrNotFound
}

func (f *fakeRepo) ListIdentityCounts(context.Context, ports.CountParams) ([]domain.IdentityCount, domain.Freshness, error) {
	return nil, domain.Freshness{}, nil
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
