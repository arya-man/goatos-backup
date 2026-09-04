package app

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// personRoleSpec declares, per grantable RBAC role, the scope shape the grant
// takes and the primary_role_hint stamped on the member row. This IS the rule
// that keeps the operator scope invariant: an operator (and a park head) is
// park-scoped, never tenant; director/verifier roles are tenant-scoped.
type personRoleSpec struct {
	ScopeType string
	RoleHint  string
}

// grantablePersonRoles is the closed set the Add Person form may grant.
// ceo_internal is deliberately absent: the platform-owner cohort is seed-owned
// (founder/builder visibility invariant), never created from a form.
var grantablePersonRoles = map[string]personRoleSpec{
	permissions.RoleOperator:       {ScopeType: "park", RoleHint: "operator"},
	permissions.RoleParkHead:       {ScopeType: "park", RoleHint: "park_head"},
	permissions.RoleVerifier:       {ScopeType: "tenant", RoleHint: "verifier"},
	permissions.RolePCDirector:     {ScopeType: "tenant", RoleHint: "pc_director"},
	permissions.RoleGrowthDirector: {ScopeType: "tenant", RoleHint: "growth_director"},
	permissions.RoleFeedDirector:   {ScopeType: "tenant", RoleHint: "feed_director"},
	permissions.RoleHealthDirector: {ScopeType: "tenant", RoleHint: "health_director"},
	// Breeding Director (maintainer decision 2026-09-04): a director desk, tenant-scoped like
	// the others -- hoof / hair trimming is planned across both parks.
	permissions.RoleBreedingDirector: {ScopeType: "tenant", RoleHint: "breeding_director"},
}

var validDesignationGrades = map[string]struct{}{
	"cxo": {}, "director": {}, "manager": {}, "assistant_manager": {},
}

// PeopleService owns the People/HRMS directory reads and the create-person
// onboarding write (Firebase account + grant + allowlist + member, see
// CreatePerson).
type PeopleService struct {
	repo     ports.PeopleRepository
	identity ports.IdentityProvider
	issuer   string
}

// NewPeopleService wires the directory service. identity may be nil in
// environments with no Firebase project (local dev-headers mode); creating a
// person then fails closed with a clear "account service unavailable" error
// rather than writing rows for a login that cannot exist.
func NewPeopleService(repo ports.PeopleRepository, identity ports.IdentityProvider, issuer string) *PeopleService {
	return &PeopleService{repo: repo, identity: identity, issuer: strings.TrimSpace(issuer)}
}

func (s *PeopleService) ListPeople(ctx context.Context, params ports.ListPeopleParams, traceID string) (*domain.PeopleListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.ParkID = strings.TrimSpace(params.ParkID)
	params.DepartmentID = strings.TrimSpace(params.DepartmentID)
	params.Status = strings.TrimSpace(params.Status)
	params.Search = strings.TrimSpace(params.Search)
	if params.ParkID != "" && !uuidutil.IsUUIDString(params.ParkID) {
		return nil, BadRequest("invalid_park_id", "park_id must be a UUID")
	}
	if params.DepartmentID != "" && !uuidutil.IsUUIDString(params.DepartmentID) {
		return nil, BadRequest("invalid_department_id", "department_id must be a UUID")
	}
	params.Limit = boundedLimit(params.Limit, 100)

	items, next, err := s.repo.ListPeople(ctx, params)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidFilter) {
			return nil, BadRequest("invalid_cursor", "cursor is not a valid page cursor")
		}
		return nil, mapRepoErr(err)
	}
	catalog, err := s.repo.PeopleCatalog(ctx, params.TenantID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.PeopleListResponse{Items: items, NextCursor: next, Catalog: catalog, TraceID: traceID}, nil
}

// CreatePerson creates the person AND their working login in one flow:
//
//  1. validate + normalize the request,
//  2. ensure the Firebase email/password account (idempotent lookup-first;
//     the convention password applies only to a NEWLY created account),
//  3. derive the internal user id (platformauth.StableSubjectID — the same
//     derivation the auth middleware applies at request time),
//  4. one DB transaction: member + active scope grant + allowlist + audit.
//
// Failure between 2 and 4 is safe: an account with no grants 403s at the
// middleware, and a retry with the same Idempotency-Key finds the same UID and
// completes the transaction.
func (s *PeopleService) CreatePerson(ctx context.Context, tenantID, actorID, idempotencyKey string, body domain.CreatePersonRequest, traceID string) (*domain.PersonResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, BadRequest("idempotency_key_required", "Idempotency-Key header is required")
	}

	firstName := collapseSpaces(body.FirstName)
	lastName := collapseSpaces(body.LastName)
	if firstName == "" {
		return nil, BadRequest("first_name_required", "first name is required")
	}
	email := authallow.NormalizeEmail(body.Email)
	if !validPersonEmail(email) {
		return nil, BadRequest("invalid_email", "a valid email address is required")
	}

	role := strings.TrimSpace(body.Role)
	spec, ok := grantablePersonRoles[role]
	if !ok {
		return nil, BadRequest("invalid_role", "role is not grantable from this form")
	}
	parkID := strings.TrimSpace(body.ParkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return nil, BadRequest("invalid_park_id", "park_id must be a UUID")
	}
	scopeID := tenantID
	if spec.ScopeType == "park" {
		// The operator scope invariant: a park-scoped role REQUIRES its park.
		if parkID == "" {
			return nil, BadRequest("park_required", "this role works inside one park; choose the park")
		}
		scopeID = parkID
	}
	departmentID := strings.TrimSpace(body.DepartmentID)
	if departmentID != "" && !uuidutil.IsUUIDString(departmentID) {
		return nil, BadRequest("invalid_department_id", "department_id must be a UUID")
	}
	grade := strings.TrimSpace(body.DesignationGrade)
	if grade != "" {
		if _, ok := validDesignationGrades[grade]; !ok {
			return nil, BadRequest("invalid_designation_grade", "designation grade is not recognized")
		}
	}

	displayName := firstName
	if lastName != "" {
		displayName = firstName + " " + lastName
	}

	preflight, err := s.repo.PreflightCreatePerson(ctx, ports.PreflightCreatePersonCommand{
		TenantID:         tenantID,
		IdempotencyKey:   idempotencyKey,
		NormalizedEmail:  email,
		FirstName:        firstName,
		LastName:         lastName,
		Role:             role,
		ScopeType:        spec.ScopeType,
		ScopeID:          scopeID,
		DepartmentID:     departmentID,
		DesignationGrade: grade,
	})
	if err != nil {
		if errors.Is(err, ports.ErrDuplicateEmail) {
			return nil, &Error{Code: "duplicate_email", Message: "a person with this email already exists", HTTPStatus: 409, Retryable: false}
		}
		if errors.Is(err, ports.ErrIdempotencyConflict) {
			return nil, &Error{Code: "idempotency_conflict", Message: "this request key was already used with different details", HTTPStatus: 409, Retryable: false}
		}
		if errors.Is(err, ports.ErrIdempotencyInFlight) {
			return nil, &Error{Code: "idempotency_in_flight", Message: "this request is already being processed; retry shortly", HTTPStatus: 409, Retryable: true}
		}
		return nil, mapRepoErr(err)
	}
	if preflight.Replay != nil {
		return &domain.PersonResponse{
			Person:  *preflight.Replay,
			Login:   domain.PersonLogin{Email: email, AccountStatus: "existing"},
			TraceID: traceID,
		}, nil
	}

	if s.identity == nil {
		return nil, &Error{Code: "identity_unavailable", Message: "login account service is not configured in this environment", HTTPStatus: 503, Retryable: false}
	}
	ensured, err := s.identity.EnsureEmailUser(ctx, email, displayName, ConventionPassword(firstName))
	if err != nil {
		if errors.Is(err, ports.ErrIdentityUnavailable) {
			return nil, &Error{Code: "identity_unavailable", Message: "login account service is unavailable; nothing was created — try again", HTTPStatus: 502, Retryable: true}
		}
		return nil, err
	}
	if s.issuer == "" {
		return nil, &Error{Code: "identity_unavailable", Message: "auth issuer is not configured", HTTPStatus: 503, Retryable: false}
	}
	userID := platformauth.StableSubjectID(s.issuer, ensured.UID)

	person, err := s.repo.CreatePerson(ctx, ports.CreatePersonCommand{
		TenantID:         tenantID,
		ActorID:          actorID,
		IdempotencyKey:   idempotencyKey,
		UserID:           userID,
		FirstName:        firstName,
		LastName:         lastName,
		DisplayName:      displayName,
		Email:            email,
		NormalizedEmail:  email,
		Role:             role,
		ScopeType:        spec.ScopeType,
		ScopeID:          scopeID,
		RoleHint:         spec.RoleHint,
		DesignationGrade: grade,
		ParkID:           parkID,
		DepartmentID:     departmentID,
	})
	if err != nil {
		if errors.Is(err, ports.ErrDuplicateEmail) {
			return nil, &Error{Code: "duplicate_email", Message: "a person with this email already exists", HTTPStatus: 409, Retryable: false}
		}
		if errors.Is(err, ports.ErrIdempotencyConflict) {
			return nil, &Error{Code: "idempotency_conflict", Message: "this request key was already used with different details", HTTPStatus: 409, Retryable: false}
		}
		if errors.Is(err, ports.ErrIdempotencyInFlight) {
			return nil, &Error{Code: "idempotency_in_flight", Message: "this request is already being processed; retry shortly", HTTPStatus: 409, Retryable: true}
		}
		return nil, mapRepoErr(err)
	}

	accountStatus := "created"
	if ensured.Existed {
		accountStatus = "existing"
	}
	return &domain.PersonResponse{
		Person:  person,
		Login:   domain.PersonLogin{Email: email, AccountStatus: accountStatus},
		TraceID: traceID,
	}, nil
}

// ConventionPassword builds the farm's standing `<FirstName>@2026` password
// convention (maintainer decision 2026-08-22) for a NEWLY created account. An
// account that already existed keeps its own password untouched.
func ConventionPassword(firstName string) string {
	name := collapseSpaces(firstName)
	if name == "" {
		return ""
	}
	runes := []rune(name)
	runes[0] = unicode.ToUpper(runes[0])
	// The convention uses one word; a two-word first name keeps only its first
	// word so the password matches what the maintainer would type by habit.
	word := strings.Fields(string(runes))[0]
	return word + "@2026"
}

func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func validPersonEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n,") {
		return false
	}
	local, domainPart, ok := strings.Cut(email, "@")
	return ok && local != "" && domainPart != "" && strings.Contains(domainPart, ".") && !strings.Contains(domainPart, "@")
}
