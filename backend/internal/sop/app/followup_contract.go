package app

import (
	"context"
	"fmt"
	"strings"

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
//
// SHIFTING is deliberately absent (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md):
// its operator work is the card-with-slots `shifting` section, not a follow_up track. The seeded
// follow_up track stays on existing versions as dormant history and is still validated when
// present; it is simply no longer demanded.
var requiredFollowUpTracks = map[string][]string{
	tasksdomain.SOPCodeBirth:     {tasksdomain.TemplateKeyBirthKid, tasksdomain.TemplateKeyBirthMother},
	tasksdomain.SOPCodeDeath:     {tasksdomain.TemplateKeyDeath},
	tasksdomain.SOPCodeReconcile: {tasksdomain.TemplateKeyReconcile},
}

// FollowUpRequired reports whether a SOP code's versions must carry a follow_up section. A
// GENERAL SOP (code general.*) always does: its "main" track IS the work the operator starts.
func FollowUpRequired(sopCode string) bool {
	if strings.HasPrefix(sopCode, "general.") {
		return true
	}
	_, ok := requiredFollowUpTracks[sopCode]
	return ok
}

// requiredTracksFor is the track list a code must keep: the herd-operations map, or "main" for
// a general SOP.
func requiredTracksFor(sopCode string) []string {
	if strings.HasPrefix(sopCode, "general.") {
		return []string{tasksdomain.GeneralTrackKey}
	}
	return requiredFollowUpTracks[sopCode]
}

func (s *Service) validateFollowUpContract(ctx context.Context, report *domain.ValidationReport, tenantID, sopCode string, formDSL map[string]any) {
	required := FollowUpRequired(sopCode)
	if _, present := formDSL["follow_up"]; !present {
		if required {
			addError(report, "form_dsl.follow_up", "required", "this SOP runs the operator's steps from follow_up; add at least one track before publishing")
		}
		return
	}
	for _, path := range tasksdomain.UnknownFollowUpKeys(formDSL) {
		addError(report, "form_dsl."+path, "unknown_key", path+": not a follow-up field")
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
	for _, track := range requiredTracksFor(sopCode) {
		if _, ok := followUp.Track(track); !ok {
			addError(report, "form_dsl.follow_up.tracks", "missing_track", fmt.Sprintf("%s must keep a %q track: the engine opens that workflow from it", sopCode, track))
		}
	}
	for _, req := range requiredEngineHookSteps[sopCode] {
		track, ok := followUp.Track(req.track)
		if !ok {
			continue // reported above
		}
		kept := false
		for _, step := range track.Steps {
			if registry[step.TaskType].EngineHook == req.hook {
				kept = true
				break
			}
		}
		if !kept {
			addError(report, "form_dsl.follow_up.tracks", "engine_step_removed",
				fmt.Sprintf("the %q step (%s) cannot be removed from the %q track: %s", req.title, req.stepKey, req.track, req.why))
		}
	}
}

// requiredEngineHookStep is an authored step the server acts on, so deleting it changes what the
// engine does rather than what the operator is asked.
type requiredEngineHookStep struct {
	track, hook, stepKey, title, why string
}

// requiredEngineHookSteps names, per SOP code, the hooked steps whose removal breaks engine
// behaviour. Only two qualify: the kid's tag step is the gate that keeps a birth open until the kid
// carries a permanent RFID, and the pen step is the only place a kid whose pen was not resolved at
// birth is placed. The other hooks only shape a read or a label -- weight (weigh_kg), colostrum
// feeds (the Colostrum lens), death clips (death_evidence; death follows the SOP, decision 1) and
// the reconcile return (the completion hook runs on the last step, whatever it is) -- so the SOP
// may drop them.
var requiredEngineHookSteps = map[string][]requiredEngineHookStep{
	tasksdomain.SOPCodeBirth: {
		{track: tasksdomain.TemplateKeyBirthKid, hook: tasksdomain.EngineHookTagKid, stepKey: tasksdomain.ActionKeyTagTheKid, title: "Tag the kid",
			why: "it keeps the birth open until the kid carries a permanent RFID"},
		{track: tasksdomain.TemplateKeyBirthKid, hook: tasksdomain.EngineHookRecordPen, stepKey: tasksdomain.ActionKeyRecordShed, title: "Record pen",
			why: "it is the only place a kid whose pen was not known at birth is placed"},
	},
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
	// what migration 000308 inserts; the compiler at open reads the live rows.
	return tasksdomain.SeededTaskTypes()
}
