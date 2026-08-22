package app

import (
	"context"
	"errors"
	"testing"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	testTenantID = "9d9c2f66-8c1a-4a63-9a53-9f4f4b1f0a01"
	testActorID  = "9d9c2f66-8c1a-4a63-9a53-9f4f4b1f0a02"
	testParkID   = "9d9c2f66-8c1a-4a63-9a53-9f4f4b1f0a03"
	testIssuer   = "https://securetoken.google.com/goatos-test"
)

type fakePeopleRepo struct {
	created  *ports.CreatePersonCommand
	listErr  error
	people   []domain.PersonSummary
	catalog  domain.PeopleCatalog
	createFn func(ports.CreatePersonCommand) (domain.PersonSummary, error)
}

func (f *fakePeopleRepo) ListPeople(_ context.Context, _ ports.ListPeopleParams) ([]domain.PersonSummary, string, error) {
	return f.people, "", f.listErr
}

func (f *fakePeopleRepo) PeopleCatalog(_ context.Context, _ string) (domain.PeopleCatalog, error) {
	return f.catalog, nil
}

func (f *fakePeopleRepo) CreatePerson(_ context.Context, cmd ports.CreatePersonCommand) (domain.PersonSummary, error) {
	f.created = &cmd
	if f.createFn != nil {
		return f.createFn(cmd)
	}
	return domain.PersonSummary{PersonID: "person-1", DisplayName: cmd.DisplayName}, nil
}

type fakeIdentity struct {
	uid      string
	existed  bool
	err      error
	email    string
	password string
}

func (f *fakeIdentity) EnsureEmailUser(_ context.Context, email, _, password string) (ports.EnsuredUser, error) {
	f.email = email
	f.password = password
	if f.err != nil {
		return ports.EnsuredUser{}, f.err
	}
	return ports.EnsuredUser{UID: f.uid, Existed: f.existed}, nil
}

func validCreateRequest() domain.CreatePersonRequest {
	return domain.CreatePersonRequest{
		FirstName: "amit",
		LastName:  "Kumar",
		Email:     " Amit.Kumar@Mesha.SG ",
		Role:      "operator",
		ParkID:    testParkID,
	}
}

func TestCreatePersonMintsAccountAndDerivesStableUserID(t *testing.T) {
	repo := &fakePeopleRepo{}
	identity := &fakeIdentity{uid: "firebase-uid-1"}
	svc := NewPeopleService(repo, identity, testIssuer)

	resp, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-1", validCreateRequest(), "trace")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if identity.email != "amit.kumar@mesha.sg" {
		t.Fatalf("identity got email %q, want normalized", identity.email)
	}
	// The maintainer's standing password convention: FirstName (capitalized) @2026.
	if identity.password != "Amit@2026" {
		t.Fatalf("password = %q, want Amit@2026", identity.password)
	}
	if repo.created == nil {
		t.Fatalf("repo.CreatePerson not called")
	}
	wantUserID := platformauth.StableSubjectID(testIssuer, "firebase-uid-1")
	if repo.created.UserID != wantUserID {
		t.Fatalf("UserID = %q, want the StableSubjectID derivation %q", repo.created.UserID, wantUserID)
	}
	if repo.created.ScopeType != "park" || repo.created.ScopeID != testParkID {
		t.Fatalf("operator grant must be PARK-scoped, got %s/%s", repo.created.ScopeType, repo.created.ScopeID)
	}
	if repo.created.DisplayName != "amit Kumar" {
		t.Fatalf("display name = %q", repo.created.DisplayName)
	}
	if resp.Login.AccountStatus != "created" {
		t.Fatalf("account status = %q, want created", resp.Login.AccountStatus)
	}
}

func TestCreatePersonExistingAccountIsReportedAndPasswordNotClaimed(t *testing.T) {
	repo := &fakePeopleRepo{}
	identity := &fakeIdentity{uid: "firebase-uid-2", existed: true}
	svc := NewPeopleService(repo, identity, testIssuer)

	resp, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-2", validCreateRequest(), "trace")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if resp.Login.AccountStatus != "existing" {
		t.Fatalf("account status = %q, want existing (existing accounts keep their own password)", resp.Login.AccountStatus)
	}
}

func TestCreatePersonOperatorWithoutParkIsRefused(t *testing.T) {
	repo := &fakePeopleRepo{}
	svc := NewPeopleService(repo, &fakeIdentity{uid: "u"}, testIssuer)

	body := validCreateRequest()
	body.ParkID = ""
	_, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-3", body, "trace")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "park_required" {
		t.Fatalf("want park_required, got %v", err)
	}
	if repo.created != nil {
		t.Fatalf("no rows may be written for a refused request")
	}
}

func TestCreatePersonRefusesUngrantableRole(t *testing.T) {
	svc := NewPeopleService(&fakePeopleRepo{}, &fakeIdentity{uid: "u"}, testIssuer)
	body := validCreateRequest()
	body.Role = "ceo_internal"
	_, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-4", body, "trace")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_role" {
		t.Fatalf("ceo_internal must not be grantable from the form, got %v", err)
	}
}

func TestCreatePersonRequiresIdempotencyKey(t *testing.T) {
	svc := NewPeopleService(&fakePeopleRepo{}, &fakeIdentity{uid: "u"}, testIssuer)
	_, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "  ", validCreateRequest(), "trace")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "idempotency_key_required" {
		t.Fatalf("want idempotency_key_required, got %v", err)
	}
}

func TestCreatePersonIdentityUnavailableFailsClosed(t *testing.T) {
	repo := &fakePeopleRepo{}
	identity := &fakeIdentity{err: ports.ErrIdentityUnavailable}
	svc := NewPeopleService(repo, identity, testIssuer)
	_, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-5", validCreateRequest(), "trace")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "identity_unavailable" || appErr.HTTPStatus != 502 {
		t.Fatalf("want retryable identity_unavailable 502, got %v", err)
	}
	if repo.created != nil {
		t.Fatalf("no DB write may happen when the login account could not be ensured")
	}
}

func TestCreatePersonWithoutIdentityProviderIsRefused(t *testing.T) {
	svc := NewPeopleService(&fakePeopleRepo{}, nil, testIssuer)
	_, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-6", validCreateRequest(), "trace")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "identity_unavailable" || appErr.HTTPStatus != 503 {
		t.Fatalf("want identity_unavailable 503, got %v", err)
	}
}

func TestCreatePersonDuplicateEmailIs409(t *testing.T) {
	repo := &fakePeopleRepo{createFn: func(ports.CreatePersonCommand) (domain.PersonSummary, error) {
		return domain.PersonSummary{}, ports.ErrDuplicateEmail
	}}
	svc := NewPeopleService(repo, &fakeIdentity{uid: "u"}, testIssuer)
	_, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-7", validCreateRequest(), "trace")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "duplicate_email" || appErr.HTTPStatus != 409 {
		t.Fatalf("want duplicate_email 409, got %v", err)
	}
}

func TestConventionPassword(t *testing.T) {
	cases := map[string]string{
		"amit":        "Amit@2026",
		"  Darshan  ": "Darshan@2026",
		"sagar m":     "Sagar@2026",
		"":            "",
	}
	for in, want := range cases {
		if got := ConventionPassword(in); got != want {
			t.Fatalf("ConventionPassword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTenantScopedRoleIgnoresParkForScopeButKeepsLocation(t *testing.T) {
	repo := &fakePeopleRepo{}
	svc := NewPeopleService(repo, &fakeIdentity{uid: "u"}, testIssuer)
	body := validCreateRequest()
	body.Role = "verifier"
	if _, err := svc.CreatePerson(context.Background(), testTenantID, testActorID, "key-8", body, "trace"); err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if repo.created.ScopeType != "tenant" || repo.created.ScopeID != testTenantID {
		t.Fatalf("verifier must be tenant-scoped, got %s/%s", repo.created.ScopeType, repo.created.ScopeID)
	}
	if repo.created.ParkID != testParkID {
		t.Fatalf("primary location park should still be recorded, got %q", repo.created.ParkID)
	}
}
