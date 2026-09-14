package app

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// SOP-DRIVEN FOLLOW-UP CONTRACT (maintainer decision 2026-09-13,
// docs/decisions/sop-driven-herd-operations.md).
//
// The Herd Operations SOPs are no longer library documents: the tasks engine opens birth /
// death / shifting / reconcile workflows from the PUBLISHED version's `follow_up` section. So a
// version of one of those codes that has no usable follow_up must never become publishable --
// the engine would refuse to open the next birth. The compiler's own validator
// (tasksdomain.ValidateFollowUp) runs here at CreateVersion time against the tenant's Task Type
// Registry, and the required tracks per code are asserted, so the failure is a 400 on the web
// with the field named, not a silent stop of the herd register.

// TaskTypeSource reads the tenant's Task Type Registry for validation.
type TaskTypeSource interface {
	ListActiveTaskTypes(ctx context.Context, tenantID string) (tasksdomain.TaskTypeRegistry, error)
}

// WithTaskTypeSource wires the registry reader (the postgres repository implements it).
func (s *Service) WithTaskTypeSource(src TaskTypeSource) *Service {
	s.taskTypes = src
	return s
}

// requiredFollowUpTracks names, per herd-operations SOP code, the tracks the engine opens.
var requiredFollowUpTracks = map[string][]string{
	tasksdomain.SOPCodeBirth:     {tasksdomain.TemplateKeyBirthKid, tasksdomain.TemplateKeyBirthMother},
	tasksdomain.SOPCodeDeath:     {tasksdomain.TemplateKeyDeath},
	tasksdomain.SOPCodeShifting:  {tasksdomain.TemplateKeyShifting},
	tasksdomain.SOPCodeReconcile: {tasksdomain.TemplateKeyReconcile},
}

// FollowUpRequired reports whether a SOP code's versions must carry a follow_up section.
func FollowUpRequired(sopCode string) bool {
	_, ok := requiredFollowUpTracks[sopCode]
	return ok
}

func (s *Service) validateFollowUpContract(ctx context.Context, report *domain.ValidationReport, tenantID, sopCode string, formDSL map[string]any) {
	required := FollowUpRequired(sopCode)
	if _, present := formDSL["follow_up"]; !present {
		if required {
			addError(report, "form_dsl.follow_up", "required", "this SOP runs the operator's steps from follow_up; add at least one track before publishing")
		}
		return
	}
	followUp, err := tasksdomain.ParseFollowUp(formDSL)
	if err != nil {
		addError(report, "form_dsl.follow_up", "invalid", err.Error())
		return
	}
	registry, err := s.taskTypeRegistry(ctx, tenantID)
	if err != nil {
		addError(report, "form_dsl.follow_up", "registry_unavailable", "the task type registry could not be read: "+err.Error())
		return
	}
	for _, problem := range tasksdomain.ValidateFollowUp(followUp, registry) {
		addError(report, "form_dsl.follow_up", "invalid", problem)
	}
	for _, track := range requiredFollowUpTracks[sopCode] {
		if _, ok := followUp.Track(track); !ok {
			addError(report, "form_dsl.follow_up.tracks", "missing_track", fmt.Sprintf("%s must keep a %q track: the engine opens that workflow from it", sopCode, track))
		}
	}
}

func (s *Service) taskTypeRegistry(ctx context.Context, tenantID string) (tasksdomain.TaskTypeRegistry, error) {
	if s.taskTypes != nil {
		reg, err := s.taskTypes.ListActiveTaskTypes(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if len(reg) > 0 {
			return reg, nil
		}
	}
	// A tenant whose registry has not been seeded validates against the seeded registry, which is
	// what migration 000304 inserts; the compiler at open reads the live rows.
	return tasksdomain.SeededTaskTypes()
}
