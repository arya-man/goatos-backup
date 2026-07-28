package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// Event types this module consumes (both durable buses register these handlers through
// internal/eventwiring.RegisterWorkflowConsumers so the sets cannot drift).
const (
	EventGoatCreated         = "goat.created"
	EventGoatExited          = "goat.exited"
	EventGoatIdentifierAdded = "goat.identifier.added"
	EventCountsDeathReported = "counts.death.reported"
	EventCountsDeathRejected = "counts.death.rejected"

	EventVerificationVerdictApproved = "verification.verdict.approved"
	EventVerificationVerdictRework   = "verification.verdict.rework"
)

type countsDeathPayload struct {
	GoatID            string `json:"goat_id"`
	ApprovalRequestID string `json:"approval_request_id"`
}

// CountsDeathReportedHandler opens the operator upload workflow immediately after submission;
// CountsDeathRejectedHandler cancels it when admin rejects without changing the live goat.
type CountsDeathReportedHandler struct{ svc *Service }
type CountsDeathRejectedHandler struct{ svc *Service }

func NewCountsDeathReportedHandler(svc *Service) *CountsDeathReportedHandler {
	return &CountsDeathReportedHandler{svc: svc}
}
func NewCountsDeathRejectedHandler(svc *Service) *CountsDeathRejectedHandler {
	return &CountsDeathRejectedHandler{svc: svc}
}
func (h *CountsDeathReportedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventCountsDeathReported, h)
}
func (h *CountsDeathRejectedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventCountsDeathRejected, h)
}

func decodeCountsDeathEvent(e eventbus.Event) (string, error) {
	var p countsDeathPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return "", eventbus.PermanentError(err)
		}
	}
	goatID := strings.TrimSpace(p.GoatID)
	if goatID == "" {
		goatID = strings.TrimSpace(e.Key)
	}
	return goatID, nil
}

func (h *CountsDeathReportedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	goatID, err := decodeCountsDeathEvent(e)
	if err != nil || goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return err
	}
	return h.svc.OpenReportedDeathWorkflow(ctx, e.TenantID, goatID, e.OccurredAt)
}

func (h *CountsDeathRejectedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	goatID, err := decodeCountsDeathEvent(e)
	if err != nil || goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return err
	}
	return h.svc.CancelRejectedDeathWorkflow(ctx, e.TenantID, goatID, e.OccurredAt)
}

// goatCreatedPayload is the subset of the identity goat.created payload the workflow opener reads.
// dob/dam_id/sex/time_of_birth were added to the payload for this consumer (the canonical goats row
// is still read for placement and the authoritative time_of_birth).
type goatCreatedPayload struct {
	GoatID      string `json:"goat_id"`
	OriginType  string `json:"origin_type"`
	DamID       string `json:"dam_id"`
	TimeOfBirth string `json:"time_of_birth"`
}

// GoatCreatedWorkflowHandler opens the birth follow-up workflows (kid track + shared mother track)
// when a birth-origin goat is created during birth submission. Count approval happens later and
// does not participate in workflow creation.
type GoatCreatedWorkflowHandler struct {
	svc *Service
}

func NewGoatCreatedWorkflowHandler(svc *Service) *GoatCreatedWorkflowHandler {
	return &GoatCreatedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*GoatCreatedWorkflowHandler)(nil)

func (h *GoatCreatedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatCreated, h)
}

func (h *GoatCreatedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p goatCreatedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	if p.OriginType != "birth" {
		return nil
	}
	goatID := strings.TrimSpace(p.GoatID)
	if goatID == "" {
		goatID = strings.TrimSpace(e.Key)
	}
	if goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	occurredAt := e.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return h.svc.OpenBirthWorkflows(ctx, OpenBirthWorkflowsInput{
		TenantID:           e.TenantID,
		GoatID:             goatID,
		DamRef:             p.DamID,
		PayloadTimeOfBirth: p.TimeOfBirth,
		OccurredAt:         occurredAt,
	})
}

// goatExitedPayload is the subset of the identity goat.exited payload the death opener reads.
type goatExitedPayload struct {
	GoatID     string `json:"goat_id"`
	ExitReason string `json:"exit_reason"`
}

// GoatExitedWorkflowHandler releases the already-uploaded evidence to Verify when admin approval
// applies the death. Sold/culled/transferred exits release nothing.
type GoatExitedWorkflowHandler struct {
	svc *Service
}

func NewGoatExitedWorkflowHandler(svc *Service) *GoatExitedWorkflowHandler {
	return &GoatExitedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*GoatExitedWorkflowHandler)(nil)

func (h *GoatExitedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatExited, h)
}

func (h *GoatExitedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p goatExitedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	if p.ExitReason != "died" {
		return nil
	}
	goatID := strings.TrimSpace(p.GoatID)
	if goatID == "" {
		goatID = strings.TrimSpace(e.Key)
	}
	if goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	return h.svc.ReleaseApprovedDeathEvidence(ctx, e.TenantID, goatID, e.OccurredAt)
}

// identifierAddedPayload is the subset of the identity goat.identifier.added payload this consumer
// reads (emitted by the promote path alongside goat.identifier.retired for the temp).
type identifierAddedPayload struct {
	GoatID          string `json:"goat_id"`
	IdentifierType  string `json:"identifier_type"`
	IdentifierValue string `json:"identifier_value"`
	Action          string `json:"action"`
}

// IdentifierAddedWorkflowHandler records the permanent-RFID prerequisite on "Tag the kid". It
// deliberately does not complete the step; the operator's mandatory tagging video does that.
type IdentifierAddedWorkflowHandler struct {
	svc *Service
}

func NewIdentifierAddedWorkflowHandler(svc *Service) *IdentifierAddedWorkflowHandler {
	return &IdentifierAddedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*IdentifierAddedWorkflowHandler)(nil)

func (h *IdentifierAddedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatIdentifierAdded, h)
}

func (h *IdentifierAddedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p identifierAddedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	// Only the PERMANENT primary identifier completes tag_the_kid. Temporary tags and secondary
	// RFIDs (animal_identifier_2) do not.
	if p.Action != "attach" || p.IdentifierType != "animal_identifier_1" {
		return nil
	}
	goatID := strings.TrimSpace(p.GoatID)
	if goatID == "" {
		goatID = strings.TrimSpace(e.Key)
	}
	if goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	at := e.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	return h.svc.CompleteTagAction(ctx, e.TenantID, goatID, at.UTC())
}

// deathVerdictPayload is the subset of the verification verdict payload this consumer reads.
type deathVerdictPayload struct {
	Status     string `json:"status"`
	Decision   string `json:"decision"`
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// DeathVerificationHandler applies the authorized verifier's verdict on a death evidence item:
//
//	verification.verdict.approved (our workflow_death_signoff item) -> sign-off completes, workflow completes
//	verification.verdict.rework   (our workflow_death_signoff item) -> both videos reset to rework
//
// It filters strictly on source.module + source.ref_type so shifting/feed/vaccination verdicts are
// ignored (and vice versa — the two counts-module consumers are kept apart by ref_type alone).
type DeathVerificationHandler struct {
	svc *Service
	now func() time.Time
}

type BirthVerificationHandler struct {
	svc *Service
	now func() time.Time
}

func NewBirthVerificationHandler(svc *Service, now func() time.Time) *BirthVerificationHandler {
	if now == nil {
		now = time.Now
	}
	return &BirthVerificationHandler{svc: svc, now: now}
}

func (h *BirthVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationVerdictApproved, h)
	bus.Subscribe(EventVerificationVerdictRework, h)
}

func (h *BirthVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != EventVerificationVerdictApproved && e.Type != EventVerificationVerdictRework {
		return nil
	}
	var p deathVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != domain.VerificationModuleCounts || p.Source.RefType != domain.VerificationRefTypeBirthSignoff {
		return nil
	}
	workflowID := strings.TrimSpace(p.Source.RefID)
	if workflowID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	verdictAt := e.OccurredAt
	if verdictAt.IsZero() {
		verdictAt = h.now().UTC()
	}
	cmd := ports.DeathVerdictCommand{
		TenantID: e.TenantID, WorkflowID: workflowID, VerifiedBy: strings.TrimSpace(p.VerifiedBy),
		Reason: strings.TrimSpace(p.Reason), VerdictAt: verdictAt,
	}
	if e.Type == EventVerificationVerdictApproved {
		return h.svc.ApplyBirthSignoffApproved(ctx, cmd)
	}
	return h.svc.BounceBirthVideoForRework(ctx, cmd)
}

func NewDeathVerificationHandler(svc *Service, now func() time.Time) *DeathVerificationHandler {
	if now == nil {
		now = time.Now
	}
	return &DeathVerificationHandler{svc: svc, now: now}
}

var _ eventbus.Handler = (*DeathVerificationHandler)(nil)

func (h *DeathVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationVerdictApproved, h)
	bus.Subscribe(EventVerificationVerdictRework, h)
}

func (h *DeathVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != EventVerificationVerdictApproved && e.Type != EventVerificationVerdictRework {
		return nil
	}
	var p deathVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != domain.VerificationModuleCounts || p.Source.RefType != domain.VerificationRefTypeDeathSignoff {
		return nil
	}
	workflowID := strings.TrimSpace(p.Source.RefID)
	if workflowID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	verdictAt := e.OccurredAt
	if verdictAt.IsZero() {
		verdictAt = h.now().UTC()
	}
	cmd := ports.DeathVerdictCommand{
		TenantID:   e.TenantID,
		WorkflowID: workflowID,
		VerifiedBy: strings.TrimSpace(p.VerifiedBy),
		Reason:     strings.TrimSpace(p.Reason),
		VerdictAt:  verdictAt,
	}
	switch e.Type {
	case EventVerificationVerdictApproved:
		return h.svc.ApplyDeathSignoffApproved(ctx, cmd)
	case EventVerificationVerdictRework:
		return h.svc.BounceDeathVideosForRework(ctx, cmd)
	}
	return nil
}
