package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	createLocationCommand         = "createLocation"
	updateLocationCommand         = "updateLocation"
	retireLocationCommand         = "retireLocation"
	deleteLocationCommand         = "deleteLocation"
	createLocationAliasCommand    = "createLocationAlias"
	updateLocationAliasCommand    = "updateLocationAlias"
	retireLocationAliasCommand    = "retireLocationAlias"
	deleteLocationAliasCommand    = "deleteLocationAlias"
	createLocationCapacityCommand = "createLocationCapacity"
	updateLocationCapacityCommand = "updateLocationCapacity"
	deleteLocationCapacityCommand = "deleteLocationCapacity"
	createLocationReviewCommand   = "createLocationReviewItem"
	resolveLocationReviewCommand  = "resolveLocationReviewItem"
	maxLocationTextLength         = 200
	maxLocationNotesLength        = 2000
)

var allowedLocationTypes = map[string]bool{
	"farm": true, "park": true, "shed": true, "cohort": true, "pen": true,
}

var allowedLocationStatuses = map[string]bool{
	"active": true, "inactive": true, "staging": true, "review": true,
}

var allowedAliasSourceContexts = map[string]bool{
	"manual":   true,
	"import":   true,
	"sheds_db": true,
}

var allowedCapacityKinds = map[string]bool{
	"goat_occupancy": true, "quarantine": true, "feed_trial": true, "other": true,
}

var allowedCapacitySources = map[string]bool{
	"manual": true, "android_sop": true, "import": true, "sheds_db": true,
}

var allowedReviewTypes = map[string]bool{
	"unknown_alias": true, "alias_conflict": true, "parent_type_conflict": true, "capacity_conflict": true, "retire_blocked": true, "usage_conflict": true,
}

type CreateLocationInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type UpdateLocationInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	RawBody        []byte
}

type RetireLocationInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	RawBody        []byte
}

type DeleteLocationInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	RawBody        []byte
}

type CreateLocationAliasInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	RawBody        []byte
}

type UpdateLocationAliasInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	AliasID        string
	RawBody        []byte
}

type RetireLocationAliasInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	AliasID        string
	RawBody        []byte
}

type DeleteLocationAliasInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	AliasID        string
	RawBody        []byte
}

type CreateLocationCapacityInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	LocationID     string
	RawBody        []byte
}

type UpdateLocationCapacityInput struct {
	TenantID         string
	ActorID          string
	IdempotencyKey   string
	TraceID          string
	LocationID       string
	CapacityRecordID string
	RawBody          []byte
}

type DeleteLocationCapacityInput struct {
	TenantID         string
	ActorID          string
	IdempotencyKey   string
	TraceID          string
	LocationID       string
	CapacityRecordID string
	RawBody          []byte
}

type CreateLocationReviewItemInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type ResolveLocationReviewItemInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	ReviewID       string
	RawBody        []byte
}

type locationBody struct {
	LocationType     string                        `json:"location_type"`
	LocationCode     *string                       `json:"location_code"`
	Name             string                        `json:"name"`
	ParentLocationID *string                       `json:"parent_location_id"`
	Status           string                        `json:"status"`
	Country          string                        `json:"country"`
	StateRegion      *string                       `json:"state_region"`
	District         *string                       `json:"district"`
	Pincode          *string                       `json:"pincode"`
	Lat              *float64                      `json:"lat"`
	Lng              *float64                      `json:"lng"`
	Timezone         string                        `json:"timezone"`
	Operational      *domain.OperationalAttributes `json:"operational"`
}

type updateLocationBody struct {
	LocationType     *string                       `json:"location_type"`
	LocationCode     *string                       `json:"location_code"`
	Name             *string                       `json:"name"`
	ParentLocationID *string                       `json:"parent_location_id"`
	ClearParent      bool                          `json:"clear_parent"`
	Status           *string                       `json:"status"`
	Country          *string                       `json:"country"`
	StateRegion      *string                       `json:"state_region"`
	District         *string                       `json:"district"`
	Pincode          *string                       `json:"pincode"`
	Lat              *float64                      `json:"lat"`
	Lng              *float64                      `json:"lng"`
	Timezone         *string                       `json:"timezone"`
	Operational      *domain.OperationalAttributes `json:"operational"`
	RowVersion       *int                          `json:"row_version"`
}

type retireLocationBody struct {
	Reason     string `json:"reason"`
	RowVersion *int   `json:"row_version"`
}

type deleteLocationBody struct {
	Reason     string `json:"reason"`
	RowVersion *int   `json:"row_version"`
}

type createAliasBody struct {
	AliasCode     string  `json:"alias_code"`
	SourceContext string  `json:"source_context"`
	Notes         *string `json:"notes"`
}

type updateAliasBody struct {
	AliasCode     string  `json:"alias_code"`
	SourceContext string  `json:"source_context"`
	Notes         *string `json:"notes"`
	RowVersion    *int    `json:"row_version"`
}

type retireAliasBody struct {
	Reason     string `json:"reason"`
	RowVersion *int   `json:"row_version"`
}

type deleteAliasBody struct {
	Reason     string `json:"reason"`
	RowVersion *int   `json:"row_version"`
}

type createCapacityBody struct {
	CapacityKind  string  `json:"capacity_kind"`
	CapacityValue int     `json:"capacity_value"`
	EffectiveFrom string  `json:"effective_from"`
	EffectiveTo   *string `json:"effective_to"`
	Source        string  `json:"source"`
	SourceRef     *string `json:"source_ref"`
	Notes         *string `json:"notes"`
}

type updateCapacityBody struct {
	CapacityKind  string  `json:"capacity_kind"`
	CapacityValue int     `json:"capacity_value"`
	EffectiveFrom string  `json:"effective_from"`
	EffectiveTo   *string `json:"effective_to"`
	Source        string  `json:"source"`
	SourceRef     *string `json:"source_ref"`
	Notes         *string `json:"notes"`
	RowVersion    *int    `json:"row_version"`
}

type deleteCapacityBody struct {
	Reason     string `json:"reason"`
	RowVersion *int   `json:"row_version"`
}

type createReviewItemBody struct {
	ReviewType            string          `json:"review_type"`
	SourceContext         *string         `json:"source_context"`
	SourceLabel           *string         `json:"source_label"`
	NormalizedSourceLabel *string         `json:"normalized_source_label"`
	CanonicalLocationID   *string         `json:"canonical_location_id"`
	CandidateLocationIDs  []string        `json:"candidate_location_ids"`
	Evidence              json.RawMessage `json:"evidence"`
	EvidenceHash          string          `json:"evidence_hash"`
}

type resolveReviewItemBody struct {
	Status              string  `json:"status"`
	CanonicalLocationID *string `json:"canonical_location_id"`
	ResolutionNotes     string  `json:"resolution_notes"`
	RowVersion          *int    `json:"row_version"`
}

func (s *Service) CreateLocation(ctx context.Context, input CreateLocationInput) (*domain.LocationMutationResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	body, err := decodeJSON[locationBody](input.RawBody, "CreateLocationRequest")
	if err != nil {
		return nil, err
	}
	cmd, err := createLocationCommandFromBody(tenantID, actorID, clientKey, input.TraceID, input.RawBody, body)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.CreateLocation(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return locationMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) UpdateLocation(ctx context.Context, input UpdateLocationInput) (*domain.LocationMutationResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	body, err := decodeJSON[updateLocationBody](input.RawBody, "UpdateLocationRequest")
	if err != nil {
		return nil, err
	}
	cmd, err := updateLocationCommandFromBody(tenantID, actorID, clientKey, input.TraceID, locationID, input.RawBody, body)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.UpdateLocation(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return locationMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) RetireLocation(ctx context.Context, input RetireLocationInput) (*domain.LocationMutationResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	body, err := decodeJSON[retireLocationBody](input.RawBody, "RetireLocationRequest")
	if err != nil {
		return nil, err
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > maxLocationNotesLength {
		return nil, BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return nil, BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	route := fmt.Sprintf("/admin/locations/%s/retire", locationID)
	hash, err := canonicalRequestHash(tenantID, retireLocationCommand, route, locationID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.RetireLocation(ctx, ports.RetireLocationCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, retireLocationCommand, locationID, clientKey),
		IdempotencyScope:     retireLocationCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, Reason: body.Reason, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return locationMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) DeleteLocation(ctx context.Context, input DeleteLocationInput) (*domain.LocationDeleteResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	body, err := decodeJSON[deleteLocationBody](input.RawBody, "DeleteLocationRequest")
	if err != nil {
		return nil, err
	}
	if err := validateReasonAndRowVersion(body.Reason, body.RowVersion); err != nil {
		return nil, err
	}
	body.Reason = strings.TrimSpace(body.Reason)
	route := fmt.Sprintf("/admin/locations/%s", locationID)
	hash, err := canonicalRequestHash(tenantID, deleteLocationCommand, route, locationID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.DeleteLocation(ctx, ports.DeleteLocationCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, deleteLocationCommand, locationID, clientKey),
		IdempotencyScope:     deleteLocationCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, Reason: body.Reason, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return deleteMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) CreateLocationAlias(ctx context.Context, input CreateLocationAliasInput) (*domain.LocationAliasResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	body, err := decodeJSON[createAliasBody](input.RawBody, "CreateLocationAliasRequest")
	if err != nil {
		return nil, err
	}
	body.AliasCode = strings.TrimSpace(body.AliasCode)
	body.SourceContext = strings.TrimSpace(body.SourceContext)
	body.Notes = trimOptional(body.Notes)
	if body.AliasCode == "" || len(body.AliasCode) > maxLocationTextLength {
		return nil, BadRequest("invalid_alias_code", "alias_code must be between 1 and 200 characters")
	}
	if !allowedAliasSourceContexts[body.SourceContext] {
		return nil, BadRequest("invalid_source_context", "source_context is not supported")
	}
	if err := validateOptionalText("notes", body.Notes, maxLocationNotesLength); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/locations/%s/aliases", locationID)
	hash, err := canonicalRequestHash(tenantID, createLocationAliasCommand, route, locationID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.CreateLocationAlias(ctx, ports.CreateLocationAliasCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, createLocationAliasCommand, locationID, clientKey),
		IdempotencyScope:     createLocationAliasCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, AliasCode: body.AliasCode, SourceContext: body.SourceContext, Notes: body.Notes,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return aliasMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) UpdateLocationAlias(ctx context.Context, input UpdateLocationAliasInput) (*domain.LocationAliasResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	aliasID := strings.TrimSpace(input.AliasID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(aliasID) {
		return nil, BadRequest("invalid_alias_id", "alias_id must be a UUID")
	}
	body, err := decodeJSON[updateAliasBody](input.RawBody, "UpdateLocationAliasRequest")
	if err != nil {
		return nil, err
	}
	if err := validateUpdateAliasBody(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/locations/%s/aliases/%s", locationID, aliasID)
	hash, err := canonicalRequestHash(tenantID, updateLocationAliasCommand, route, aliasID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.UpdateLocationAlias(ctx, ports.UpdateLocationAliasCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, updateLocationAliasCommand, locationID, aliasID, clientKey),
		IdempotencyScope:     updateLocationAliasCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, AliasID: aliasID, AliasCode: body.AliasCode,
		SourceContext: body.SourceContext, Notes: body.Notes, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return aliasMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) RetireLocationAlias(ctx context.Context, input RetireLocationAliasInput) (*domain.LocationAliasResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	aliasID := strings.TrimSpace(input.AliasID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(aliasID) {
		return nil, BadRequest("invalid_alias_id", "alias_id must be a UUID")
	}
	body, err := decodeJSON[retireAliasBody](input.RawBody, "RetireLocationAliasRequest")
	if err != nil {
		return nil, err
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > maxLocationNotesLength {
		return nil, BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return nil, BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	route := fmt.Sprintf("/admin/locations/%s/aliases/%s/retire", locationID, aliasID)
	hash, err := canonicalRequestHash(tenantID, retireLocationAliasCommand, route, aliasID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.RetireLocationAlias(ctx, ports.RetireLocationAliasCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, retireLocationAliasCommand, locationID, aliasID, clientKey),
		IdempotencyScope:     retireLocationAliasCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, AliasID: aliasID, Reason: body.Reason, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return aliasMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) DeleteLocationAlias(ctx context.Context, input DeleteLocationAliasInput) (*domain.LocationDeleteResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	aliasID := strings.TrimSpace(input.AliasID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(aliasID) {
		return nil, BadRequest("invalid_alias_id", "alias_id must be a UUID")
	}
	body, err := decodeJSON[deleteAliasBody](input.RawBody, "DeleteLocationAliasRequest")
	if err != nil {
		return nil, err
	}
	if err := validateReasonAndRowVersion(body.Reason, body.RowVersion); err != nil {
		return nil, err
	}
	body.Reason = strings.TrimSpace(body.Reason)
	route := fmt.Sprintf("/admin/locations/%s/aliases/%s", locationID, aliasID)
	hash, err := canonicalRequestHash(tenantID, deleteLocationAliasCommand, route, aliasID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.DeleteLocationAlias(ctx, ports.DeleteLocationAliasCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, deleteLocationAliasCommand, locationID, aliasID, clientKey),
		IdempotencyScope:     deleteLocationAliasCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, AliasID: aliasID, Reason: body.Reason, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return deleteMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) CreateLocationCapacity(ctx context.Context, input CreateLocationCapacityInput) (*domain.LocationCapacityResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	body, err := decodeJSON[createCapacityBody](input.RawBody, "CreateLocationCapacityRequest")
	if err != nil {
		return nil, err
	}
	if err := validateCapacityBody(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/locations/%s/capacity", locationID)
	hash, err := canonicalRequestHash(tenantID, createLocationCapacityCommand, route, locationID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.CreateLocationCapacity(ctx, ports.CreateLocationCapacityCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, createLocationCapacityCommand, locationID, clientKey),
		IdempotencyScope:     createLocationCapacityCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, CapacityKind: body.CapacityKind, CapacityValue: body.CapacityValue,
		EffectiveFrom: body.EffectiveFrom, EffectiveTo: body.EffectiveTo, Source: body.Source,
		SourceRef: body.SourceRef, Notes: body.Notes,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return capacityMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) UpdateLocationCapacity(ctx context.Context, input UpdateLocationCapacityInput) (*domain.LocationCapacityResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	capacityID := strings.TrimSpace(input.CapacityRecordID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(capacityID) {
		return nil, BadRequest("invalid_capacity_record_id", "capacity_record_id must be a UUID")
	}
	body, err := decodeJSON[updateCapacityBody](input.RawBody, "UpdateLocationCapacityRequest")
	if err != nil {
		return nil, err
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return nil, BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	capacityBody := &createCapacityBody{
		CapacityKind:  body.CapacityKind,
		CapacityValue: body.CapacityValue,
		EffectiveFrom: body.EffectiveFrom,
		EffectiveTo:   body.EffectiveTo,
		Source:        body.Source,
		SourceRef:     body.SourceRef,
		Notes:         body.Notes,
	}
	if err := validateCapacityBody(capacityBody); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/locations/%s/capacity/%s", locationID, capacityID)
	hash, err := canonicalRequestHash(tenantID, updateLocationCapacityCommand, route, capacityID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.UpdateLocationCapacity(ctx, ports.UpdateLocationCapacityCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, updateLocationCapacityCommand, locationID, capacityID, clientKey),
		IdempotencyScope:     updateLocationCapacityCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, CapacityRecordID: capacityID, CapacityKind: capacityBody.CapacityKind,
		CapacityValue: capacityBody.CapacityValue, EffectiveFrom: capacityBody.EffectiveFrom,
		EffectiveTo: capacityBody.EffectiveTo, Source: capacityBody.Source, SourceRef: capacityBody.SourceRef,
		Notes: capacityBody.Notes, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return capacityMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) DeleteLocationCapacity(ctx context.Context, input DeleteLocationCapacityInput) (*domain.LocationDeleteResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	locationID := strings.TrimSpace(input.LocationID)
	capacityID := strings.TrimSpace(input.CapacityRecordID)
	if !uuidutil.IsUUIDString(locationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(capacityID) {
		return nil, BadRequest("invalid_capacity_record_id", "capacity_record_id must be a UUID")
	}
	body, err := decodeJSON[deleteCapacityBody](input.RawBody, "DeleteLocationCapacityRequest")
	if err != nil {
		return nil, err
	}
	if err := validateReasonAndRowVersion(body.Reason, body.RowVersion); err != nil {
		return nil, err
	}
	body.Reason = strings.TrimSpace(body.Reason)
	route := fmt.Sprintf("/admin/locations/%s/capacity/%s", locationID, capacityID)
	hash, err := canonicalRequestHash(tenantID, deleteLocationCapacityCommand, route, capacityID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.DeleteLocationCapacity(ctx, ports.DeleteLocationCapacityCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, deleteLocationCapacityCommand, locationID, capacityID, clientKey),
		IdempotencyScope:     deleteLocationCapacityCommand, RequestHash: hash, TraceID: input.TraceID,
		LocationID: locationID, CapacityRecordID: capacityID, Reason: body.Reason, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return deleteMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) CreateLocationReviewItem(ctx context.Context, input CreateLocationReviewItemInput) (*domain.LocationReviewItemResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	body, err := decodeJSON[createReviewItemBody](input.RawBody, "CreateLocationReviewItemRequest")
	if err != nil {
		return nil, err
	}
	if err := validateReviewItemBody(body); err != nil {
		return nil, err
	}
	route := "/admin/location-review-items"
	hash, err := canonicalRequestHash(tenantID, createLocationReviewCommand, route, body.EvidenceHash, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.CreateLocationReviewItem(ctx, ports.CreateLocationReviewItemCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, createLocationReviewCommand, body.EvidenceHash, clientKey),
		IdempotencyScope:     createLocationReviewCommand, RequestHash: hash, TraceID: input.TraceID,
		ReviewType: body.ReviewType, SourceContext: body.SourceContext, SourceLabel: body.SourceLabel,
		NormalizedSourceLabel: body.NormalizedSourceLabel, CanonicalLocationID: body.CanonicalLocationID,
		CandidateLocationIDs: body.CandidateLocationIDs, EvidenceJSON: body.Evidence, EvidenceHash: body.EvidenceHash,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return reviewMutationResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) ResolveLocationReviewItem(ctx context.Context, input ResolveLocationReviewItemInput) (*domain.LocationReviewItemResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	reviewID := strings.TrimSpace(input.ReviewID)
	if !uuidutil.IsUUIDString(reviewID) {
		return nil, BadRequest("invalid_review_id", "review_id must be a UUID")
	}
	body, err := decodeJSON[resolveReviewItemBody](input.RawBody, "ResolveLocationReviewItemRequest")
	if err != nil {
		return nil, err
	}
	body.Status = strings.TrimSpace(body.Status)
	body.ResolutionNotes = strings.TrimSpace(body.ResolutionNotes)
	body.CanonicalLocationID = trimOptional(body.CanonicalLocationID)
	if body.Status != "resolved" && body.Status != "dismissed" {
		return nil, BadRequest("invalid_status", "status must be resolved or dismissed")
	}
	if body.Status == "resolved" && (body.CanonicalLocationID == nil || !uuidutil.IsUUIDString(*body.CanonicalLocationID)) {
		return nil, BadRequest("invalid_canonical_location_id", "canonical_location_id is required for resolved review items")
	}
	if body.ResolutionNotes == "" || len(body.ResolutionNotes) > maxLocationNotesLength {
		return nil, BadRequest("invalid_resolution_notes", "resolution_notes must be between 1 and 2000 characters")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return nil, BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	route := fmt.Sprintf("/admin/location-review-items/%s/resolve", reviewID)
	hash, err := canonicalRequestHash(tenantID, resolveLocationReviewCommand, route, reviewID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	result, err := s.repo.ResolveLocationReviewItem(ctx, ports.ResolveLocationReviewItemCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, resolveLocationReviewCommand, reviewID, clientKey),
		IdempotencyScope:     resolveLocationReviewCommand, RequestHash: hash, TraceID: input.TraceID,
		ReviewID: reviewID, Status: body.Status, CanonicalLocationID: body.CanonicalLocationID,
		ResolutionNotes: body.ResolutionNotes, RowVersion: *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return reviewMutationResponse(result, clientKey, input.TraceID), nil
}

func validateWriteHeaders(rawTenantID, rawActorID, rawClientKey string) (tenantID, actorID, clientKey string, err error) {
	tenantID = strings.TrimSpace(rawTenantID)
	if err := validateTenant(tenantID); err != nil {
		return "", "", "", err
	}
	actorID = strings.TrimSpace(rawActorID)
	if !uuidutil.IsUUIDString(actorID) {
		return "", "", "", BadRequest("invalid_actor_id", "authenticated actor must be a valid UUID")
	}
	clientKey = strings.TrimSpace(rawClientKey)
	if clientKey == "" {
		return "", "", "", BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	if len(clientKey) < 8 || len(clientKey) > 200 {
		return "", "", "", BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	return tenantID, actorID, clientKey, nil
}

func createLocationCommandFromBody(tenantID, actorID, clientKey, traceID string, raw []byte, body *locationBody) (ports.CreateLocationCommand, error) {
	if err := normalizeLocationBody(body); err != nil {
		return ports.CreateLocationCommand{}, err
	}
	route := "/admin/locations"
	hash, err := canonicalRequestHash(tenantID, createLocationCommand, route, body.Name, raw)
	if err != nil {
		return ports.CreateLocationCommand{}, BadRequest("invalid_json", "request body must be valid JSON")
	}
	operational := defaultOperational(body.Operational)
	return ports.CreateLocationCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, createLocationCommand, clientKey),
		IdempotencyScope:     createLocationCommand, RequestHash: hash, TraceID: traceID,
		LocationType: body.LocationType, LocationCode: body.LocationCode, Name: body.Name,
		ParentLocationID: body.ParentLocationID, Status: body.Status, Country: body.Country,
		StateRegion: body.StateRegion, District: body.District, Pincode: body.Pincode,
		Lat: body.Lat, Lng: body.Lng, Timezone: body.Timezone, Operational: operational,
	}, nil
}

func updateLocationCommandFromBody(tenantID, actorID, clientKey, traceID, locationID string, raw []byte, body *updateLocationBody) (ports.UpdateLocationCommand, error) {
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return ports.UpdateLocationCommand{}, BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	if body.LocationType != nil {
		*body.LocationType = strings.TrimSpace(*body.LocationType)
		if !allowedLocationTypes[*body.LocationType] {
			return ports.UpdateLocationCommand{}, BadRequest("invalid_location_type", "location_type is not supported")
		}
	}
	body.LocationCode = trimOptional(body.LocationCode)
	body.Name = trimOptional(body.Name)
	body.ParentLocationID = trimOptional(body.ParentLocationID)
	body.Status = trimOptional(body.Status)
	body.Country = trimOptional(body.Country)
	body.StateRegion = trimOptional(body.StateRegion)
	body.District = trimOptional(body.District)
	body.Pincode = trimOptional(body.Pincode)
	body.Timezone = trimOptional(body.Timezone)
	if body.Timezone != nil {
		normalizedTimezone, err := normalizeLocationTimezone(*body.Timezone)
		if err != nil {
			return ports.UpdateLocationCommand{}, err
		}
		body.Timezone = &normalizedTimezone
	}
	if body.Name != nil && (*body.Name == "" || len(*body.Name) > maxLocationTextLength) {
		return ports.UpdateLocationCommand{}, BadRequest("invalid_name", "name must be between 1 and 200 characters")
	}
	if body.Status != nil && !allowedLocationStatuses[*body.Status] {
		return ports.UpdateLocationCommand{}, BadRequest("invalid_status", "status is not supported")
	}
	if body.ParentLocationID != nil && !uuidutil.IsUUIDString(*body.ParentLocationID) {
		return ports.UpdateLocationCommand{}, BadRequest("invalid_parent_location_id", "parent_location_id must be a UUID")
	}
	if body.ParentLocationID != nil && *body.ParentLocationID == locationID {
		return ports.UpdateLocationCommand{}, BadRequest("invalid_parent_location_id", "location cannot be its own parent")
	}
	if err := validateGeo(body.Lat, body.Lng); err != nil {
		return ports.UpdateLocationCommand{}, err
	}
	if err := validateOperational(body.Operational); err != nil {
		return ports.UpdateLocationCommand{}, err
	}
	route := fmt.Sprintf("/admin/locations/%s", locationID)
	hash, err := canonicalRequestHash(tenantID, updateLocationCommand, route, locationID, raw)
	if err != nil {
		return ports.UpdateLocationCommand{}, BadRequest("invalid_json", "request body must be valid JSON")
	}
	return ports.UpdateLocationCommand{
		TenantID: tenantID, ActorID: actorID, ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey(tenantID, updateLocationCommand, locationID, clientKey),
		IdempotencyScope:     updateLocationCommand, RequestHash: hash, TraceID: traceID, LocationID: locationID,
		LocationType: body.LocationType, LocationCode: body.LocationCode, Name: body.Name,
		ParentLocationID: body.ParentLocationID, ClearParent: body.ClearParent, Status: body.Status,
		Country: body.Country, StateRegion: body.StateRegion, District: body.District, Pincode: body.Pincode,
		Lat: body.Lat, Lng: body.Lng, Timezone: body.Timezone, Operational: body.Operational,
		RowVersion: *body.RowVersion,
	}, nil
}

func normalizeLocationBody(body *locationBody) error {
	body.LocationType = strings.TrimSpace(body.LocationType)
	body.LocationCode = trimOptional(body.LocationCode)
	body.Name = strings.TrimSpace(body.Name)
	body.ParentLocationID = trimOptional(body.ParentLocationID)
	body.Status = strings.TrimSpace(body.Status)
	body.Country = strings.TrimSpace(body.Country)
	body.StateRegion = trimOptional(body.StateRegion)
	body.District = trimOptional(body.District)
	body.Pincode = trimOptional(body.Pincode)
	normalizedTimezone, err := normalizeLocationTimezone(body.Timezone)
	if err != nil {
		return err
	}
	body.Timezone = normalizedTimezone
	if !allowedLocationTypes[body.LocationType] {
		return BadRequest("invalid_location_type", "location_type is not supported")
	}
	if body.Name == "" || len(body.Name) > maxLocationTextLength {
		return BadRequest("invalid_name", "name must be between 1 and 200 characters")
	}
	if body.Status == "" {
		body.Status = "active"
	}
	if !allowedLocationStatuses[body.Status] {
		return BadRequest("invalid_status", "status is not supported")
	}
	if body.Country == "" {
		body.Country = "IN"
	}
	if body.ParentLocationID != nil && !uuidutil.IsUUIDString(*body.ParentLocationID) {
		return BadRequest("invalid_parent_location_id", "parent_location_id must be a UUID")
	}
	if err := validateGeo(body.Lat, body.Lng); err != nil {
		return err
	}
	return validateOperational(body.Operational)
}

func normalizeLocationTimezone(raw string) (string, error) {
	timezone := strings.TrimSpace(raw)
	if timezone == "" {
		return biztime.DefaultTimezone, nil
	}
	if timezone != biztime.DefaultTimezone {
		return "", BadRequest("invalid_timezone", "timezone must be Asia/Kolkata for India-only Goat OS")
	}
	return biztime.DefaultTimezone, nil
}

func validateCapacityBody(body *createCapacityBody) error {
	body.CapacityKind = strings.TrimSpace(body.CapacityKind)
	body.EffectiveFrom = strings.TrimSpace(body.EffectiveFrom)
	body.EffectiveTo = trimOptional(body.EffectiveTo)
	body.Source = strings.TrimSpace(body.Source)
	body.SourceRef = trimOptional(body.SourceRef)
	body.Notes = trimOptional(body.Notes)
	if !allowedCapacityKinds[body.CapacityKind] {
		return BadRequest("invalid_capacity_kind", "capacity_kind is not supported")
	}
	if body.CapacityValue <= 0 {
		return BadRequest("invalid_capacity_value", "capacity_value must be greater than zero")
	}
	if !datePattern(body.EffectiveFrom) {
		return BadRequest("invalid_effective_from", "effective_from must be YYYY-MM-DD")
	}
	if body.EffectiveTo != nil && !datePattern(*body.EffectiveTo) {
		return BadRequest("invalid_effective_to", "effective_to must be YYYY-MM-DD")
	}
	if !allowedCapacitySources[body.Source] {
		return BadRequest("invalid_source", "source is not supported")
	}
	if err := validateOptionalText("source_ref", body.SourceRef, 500); err != nil {
		return err
	}
	return validateOptionalText("notes", body.Notes, maxLocationNotesLength)
}

func validateUpdateAliasBody(body *updateAliasBody) error {
	body.AliasCode = strings.TrimSpace(body.AliasCode)
	body.SourceContext = strings.TrimSpace(body.SourceContext)
	body.Notes = trimOptional(body.Notes)
	if body.AliasCode == "" || len(body.AliasCode) > maxLocationTextLength {
		return BadRequest("invalid_alias_code", "alias_code must be between 1 and 200 characters")
	}
	if !allowedAliasSourceContexts[body.SourceContext] {
		return BadRequest("invalid_source_context", "source_context is not supported")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	return validateOptionalText("notes", body.Notes, maxLocationNotesLength)
}

func validateReasonAndRowVersion(reason string, rowVersion *int) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > maxLocationNotesLength {
		return BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if rowVersion == nil || *rowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	return nil
}

func validateReviewItemBody(body *createReviewItemBody) error {
	body.ReviewType = strings.TrimSpace(body.ReviewType)
	body.SourceContext = trimOptional(body.SourceContext)
	body.SourceLabel = trimOptional(body.SourceLabel)
	body.NormalizedSourceLabel = trimOptional(body.NormalizedSourceLabel)
	body.CanonicalLocationID = trimOptional(body.CanonicalLocationID)
	body.EvidenceHash = strings.TrimSpace(body.EvidenceHash)
	if !allowedReviewTypes[body.ReviewType] {
		return BadRequest("invalid_review_type", "review_type is not supported")
	}
	if body.CanonicalLocationID != nil && !uuidutil.IsUUIDString(*body.CanonicalLocationID) {
		return BadRequest("invalid_canonical_location_id", "canonical_location_id must be a UUID")
	}
	for _, id := range body.CandidateLocationIDs {
		if !uuidutil.IsUUIDString(strings.TrimSpace(id)) {
			return BadRequest("invalid_candidate_location_ids", "candidate_location_ids must contain only UUIDs")
		}
	}
	if len(bytes.TrimSpace(body.Evidence)) == 0 {
		body.Evidence = json.RawMessage(`{}`)
	}
	var evidence map[string]any
	if err := json.Unmarshal(body.Evidence, &evidence); err != nil {
		return BadRequest("invalid_evidence", "evidence must be a JSON object")
	}
	if body.EvidenceHash == "" || len(body.EvidenceHash) > 128 {
		return BadRequest("invalid_evidence_hash", "evidence_hash must be between 1 and 128 characters")
	}
	return nil
}

func validateGeo(lat, lng *float64) error {
	if lat != nil && (*lat < -90 || *lat > 90) {
		return BadRequest("invalid_lat", "lat must be between -90 and 90")
	}
	if lng != nil && (*lng < -180 || *lng > 180) {
		return BadRequest("invalid_lng", "lng must be between -180 and 180")
	}
	return nil
}

func validateOperational(op *domain.OperationalAttributes) error {
	if op == nil {
		return nil
	}
	op.Notes = trimOptional(op.Notes)
	return validateOptionalText("operational.notes", op.Notes, maxLocationNotesLength)
}

func defaultOperational(op *domain.OperationalAttributes) domain.OperationalAttributes {
	if op == nil {
		return domain.OperationalAttributes{
			UsableForCounts: true, UsableForFeed: true, UsableForVaccination: true, UsableForSOP: true,
		}
	}
	return *op
}

func decodeJSON[T any](raw []byte, typeName string) (*T, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body T
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match "+typeName)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func canonicalRequestHash(tenantID, command, route, subjectID string, raw []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{tenantID, command, route, subjectID, string(canonical)}, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func storedKey(parts ...string) string {
	return strings.Join(parts, ":")
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validateOptionalText(field string, value *string, maxLen int) error {
	if value != nil && len(*value) > maxLen {
		return BadRequest("invalid_"+strings.ReplaceAll(field, ".", "_"), field+" is too long")
	}
	return nil
}

func datePattern(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	for i, r := range value {
		if i == 4 || i == 7 {
			if r != '-' {
				return false
			}
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func locationMutationResponse(result *ports.LocationMutationResult, clientKey, traceID string) *domain.LocationMutationResponse {
	return &domain.LocationMutationResponse{
		Location: result.Location,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: traceID,
	}
}

func aliasMutationResponse(result *ports.LocationAliasMutationResult, clientKey, traceID string) *domain.LocationAliasResponse {
	return &domain.LocationAliasResponse{
		Alias: result.Alias,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: traceID,
	}
}

func capacityMutationResponse(result *ports.LocationCapacityMutationResult, clientKey, traceID string) *domain.LocationCapacityResponse {
	return &domain.LocationCapacityResponse{
		Capacity: result.Capacity,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: traceID,
	}
}

func deleteMutationResponse(result *ports.LocationDeleteMutationResult, clientKey, traceID string) *domain.LocationDeleteResponse {
	return &domain.LocationDeleteResponse{
		ResourceType: result.ResourceType,
		ResourceID:   result.ResourceID,
		LocationID:   result.LocationID,
		Deleted:      true,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: traceID,
	}
}

func reviewMutationResponse(result *ports.LocationReviewMutationResult, clientKey, traceID string) *domain.LocationReviewItemResponse {
	return &domain.LocationReviewItemResponse{
		ReviewItem: result.ReviewItem,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: traceID,
	}
}
