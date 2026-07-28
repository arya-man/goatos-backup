package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type birthProvisionalPrefixRepository interface {
	BirthProvisionalPrefix(ctx context.Context, tenantID, parkID string) (string, error)
}

// BirthProvisionalPrefix returns the canonical park code used for a newborn's temporary identity
// (CBE/CPT today). It is resolved server-side from the validated placement, never trusted from a
// client label.
func (s *Service) BirthProvisionalPrefix(ctx context.Context, tenantID, parkID string) (string, error) {
	repo, ok := s.repo.(birthProvisionalPrefixRepository)
	if !ok {
		return "", NotImplemented("birth_provisional_prefix_unavailable", "birth provisional identifier prefix is not configured")
	}
	prefix, err := repo.BirthProvisionalPrefix(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(parkID))
	if err != nil {
		return "", mapRepoErr(err)
	}
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if prefix != "CBE" && prefix != "CPT" {
		return "", BadRequest("unsupported_birth_park", "birth placement must resolve to CBE or CPT")
	}
	return prefix, nil
}

// Prepare seam for approval workflows.
//
// A module that holds a write PENDING until it is approved has to do the work in two halves at two
// different times:
//
//	SUBMIT   -- validate the operator's payload NOW, so a malformed dob is reported to the phone
//	            immediately instead of surfacing days later in an approver's queue, but apply NOTHING.
//	APPROVE  -- apply the already-validated payload, atomically with the approval's status flip.
//
// The existing CreateAdminGoat / CriticalDeathExit do both halves in one call, so neither half is
// reachable on its own. These Prepare* methods expose the FIRST half: they run the identical decode,
// validation, normalization, canonical-hash, and (for create) the ID-resolution pre-flight, and
// return the resulting repository command WITHOUT executing it.
//
// Two properties matter for callers:
//
//   - Preparing has NO side effects. It never claims an idempotency key, never writes a row, and
//     never emits an event. Calling it at submit time to validate and throwing the result away is
//     safe.
//   - Validation is NOT duplicated. Every rule is the same code the direct path runs -- notably the
//     critical-death dead+died guardrail, which PrepareCriticalDeathExit enforces through the same
//     validateCriticalDeathExit validator. A caller therefore cannot construct a weaker death than
//     the direct route allows.

// PrepareCreateAdminGoat validates and normalizes a goat-create request and returns the repository
// command it would execute, without creating anything.
func (s *Service) PrepareCreateAdminGoat(ctx context.Context, input CreateAdminGoatInput) (ports.CreateAdminGoatCommand, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return ports.CreateAdminGoatCommand{}, err
	}
	body, err := decodeCreateAdminGoat(input.RawBody)
	if err != nil {
		return ports.CreateAdminGoatCommand{}, err
	}
	repo, err := s.adminGoatRepository()
	if err != nil {
		return ports.CreateAdminGoatCommand{}, err
	}
	normalized, cmd, fieldErrors, err := s.normalizeAdminGoatCreate(ctx, tenantID, actorID, clientKey, input.TraceID, body)
	if err != nil {
		return ports.CreateAdminGoatCommand{}, err
	}
	if len(fieldErrors) > 0 {
		return ports.CreateAdminGoatCommand{}, BadRequest("invalid_goat_create", fieldErrors[0].Message)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ports.CreateAdminGoatCommand{}, Internal("goat create request normalization failed")
	}
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, createAdminGoatCommand, "/admin/goats", "", raw)
	if err != nil {
		return ports.CreateAdminGoatCommand{}, BadRequest("invalid_json", "request body must be valid JSON")
	}
	cmd.RequestHash = requestHash
	cmd.StoredIdempotencyKey = fmt.Sprintf("%s:%s:%s", tenantID, createAdminGoatCommand, clientKey)
	cmd.IdempotencyScope = createAdminGoatCommand

	// Resolves custodian/farm/park/shed and checks identifier lifetime conflicts. Read-only.
	fieldErrors, _, err = validateAdminGoatCreate(ctx, repo, normalized, &cmd)
	if err != nil {
		return ports.CreateAdminGoatCommand{}, mapRepoErr(err)
	}
	if len(fieldErrors) > 0 {
		return ports.CreateAdminGoatCommand{}, BadRequest("invalid_goat_create", fieldErrors[0].Message)
	}
	return cmd, nil
}

// PrepareCriticalDeathExit validates a critical-death exit and returns the repository command it
// would execute, without exiting anything.
//
// The dead+died guardrail runs here, at SUBMIT time, exactly as validateCriticalDeathExit enforces
// it on the direct route -- so an operator recording a death learns immediately that the pairing is
// wrong, and a stored request can never carry a payload that the guarded path would have rejected.
// The adapter re-checks the guardrail independently at apply time as well.
func (s *Service) PrepareCriticalDeathExit(ctx context.Context, input ExitGoatInput) (ports.ExitGoatCommand, error) {
	return s.prepareExitGoat(input, criticalDeathGoatCommand, "/admin/goats/{goat_id}/critical-death-exit", validateCriticalDeathExit, true)
}

func (s *Service) prepareExitGoat(input ExitGoatInput, commandName, route string, validate func(*domain.ExitGoatRequest) error, guardrailApproved bool) (ports.ExitGoatCommand, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return ports.ExitGoatCommand{}, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return ports.ExitGoatCommand{}, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeExitGoat(input.RawBody)
	if err != nil {
		return ports.ExitGoatCommand{}, err
	}
	if err := validate(body); err != nil {
		return ports.ExitGoatCommand{}, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ports.ExitGoatCommand{}, Internal("exit goat request normalization failed")
	}
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, commandName, route, goatID, raw)
	if err != nil {
		return ports.ExitGoatCommand{}, BadRequest("invalid_json", "request body must be valid JSON")
	}
	// P2-DEATH: an UNDATED death is stamped at APPLY time, not submit time. In the approval flow this
	// method is re-run by the approval service at approve time (prepareEffect), so reading the
	// business clock here reads the moment the death is applied -- days after submit, if the request
	// sat in a queue. An EXPLICITLY supplied date is kept verbatim: a back-dated death states when the
	// animal actually died, which the applied instant must not overwrite.
	occurredAt := s.businessNow()
	if body.OccurredAt != nil {
		occurredAt = body.OccurredAt.UTC()
	}
	return ports.ExitGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, commandName, goatID, clientKey),
		IdempotencyScope:     commandName,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		LifecycleStatus:      strings.TrimSpace(body.LifecycleStatus),
		ExitReason:           strings.TrimSpace(body.ExitReason),
		Reason:               strings.TrimSpace(body.Reason),
		OccurredAt:           occurredAt,
		EvidenceRefs:         body.EvidenceRefs,
		RowVersion:           body.RowVersion,
		GuardrailApproved:    guardrailApproved,
	}, nil
}

// AdminGoatResponseFrom adapts a repository mutation result into the public response shape. It lets
// a caller that applied a prepared command through the *InTx seam return the same response body the
// direct route returns.
func AdminGoatResponseFrom(result *ports.AdminGoatMutationResult, clientIdempotencyKey, traceID string) *domain.AdminGoatResponse {
	return adminGoatResponse(result, clientIdempotencyKey, traceID)
}

// MapRepositoryError exposes the identity repo-error -> app-error taxonomy so a module applying a
// prepared command reports the same errors as the direct route.
func MapRepositoryError(err error) error { return mapRepoErr(err) }
