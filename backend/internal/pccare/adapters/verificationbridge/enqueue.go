// Package verificationbridge adapts the verification module's CreateItem to the pccare
// VerificationEnqueuer port, so a submitted PC Care task (every animal's videos) becomes ONE
// generic verification item the verifier queue lists. pccare never writes verification's tables.
package verificationbridge

import (
	"context"
	"strconv"
	"strings"

	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccaredomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the ONE verification-service method this bridge consumes.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

type categoryRegistry interface {
	RegisterCategory(verificationdomain.CategoryDefinition) error
}

// Enqueuer bridges pccare submits into the verifier queue.
type Enqueuer struct {
	verification verificationCreator
}

// New constructs the bridge over the verification service.
func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

// RegisterCategories installs the verifier queue categories used by PC Care. Keep this helper
// in the bridge so API, outbox-relay, and Pub/Sub consumer cannot hand-maintain divergent labels.
func RegisterCategories(reg categoryRegistry) error {
	for order, workCategory := range pccaredomain.Categories {
		if err := reg.RegisterCategory(verificationdomain.CategoryDefinition{
			Vertical:              pccaredomain.VerificationVerticalPreventiveCare,
			Module:                pccaredomain.VerificationModulePCCare,
			Category:              pccaredomain.VerificationCategoryFor(workCategory),
			NavigationModule:      pccaredomain.VerificationModulePCCare,
			NavigationModuleLabel: "Preventive Care",
			PageKey:               pccaredomain.VerificationCategoryFor(workCategory),
			PageLabel:             pccaredomain.CategoryLabel(workCategory),
			PageOrder:             order + 1,
		}); err != nil {
			return err
		}
	}
	return nil
}

var _ pccareapp.VerificationEnqueuer = (*Enqueuer)(nil)

// EnqueuePCCareVerification maps the pccare request to a verification CreateItem. The whole
// task's videos travel on ONE item (maintainer-confirmed grain). CreateItem is idempotent on
// (tenant, idempotency_key), so a retry after a prior failure heals rather than duplicates.
//
// Every clip carries the scanned tag, the operator, and the capture time burned into its
// overlay, so the verifier can tell which animal and which step a clip proves; the item's
// context rows carry the task-level expectation (category, animal count, per-animal recording
// summary).
func (e *Enqueuer) EnqueuePCCareVerification(ctx context.Context, in pccareapp.VerificationEnqueueRequest) error {
	category := pccaredomain.VerificationCategoryFor(in.Category)

	// Subject: "Deworming · Castro - 2" — or, for a per-vaccine stock task (no shed),
	// "Vaccine Inventory · FMD". Degrades rather than composing a dangling separator.
	loc := oploc.OperationalLocation{ShedName: in.ShedName, PartitionLabel: in.PartitionLabel}
	locDisplay := loc.Display()
	if locDisplay == "" {
		locDisplay = strings.TrimSpace(in.VaccineLabel)
	}
	baseLabel := pccaredomain.CategoryLabel(in.Category)
	var label *string
	switch {
	case baseLabel != "" && locDisplay != "":
		fullLabel := baseLabel + " · " + locDisplay
		label = &fullLabel
	case baseLabel != "":
		label = &baseLabel
	case locDisplay != "":
		label = &locDisplay
	}

	mediaRefs := make([]string, 0, len(in.MediaRefs))
	for _, ref := range in.MediaRefs {
		if strings.TrimSpace(ref.ProofRef) == "" {
			continue
		}
		mediaRefs = append(mediaRefs, ref.ProofRef)
	}

	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     pccaredomain.VerificationVerticalPreventiveCare,
		Module:       pccaredomain.VerificationModulePCCare,
		Category:     category,
		SubjectLabel: label,
		ContextRows:  contextRows(in),
		Source: verificationdomain.SourceRef{
			Module:  pccaredomain.VerificationModulePCCare,
			RefType: pccaredomain.VerificationRefTypeTask,
			RefID:   in.TaskID,
		},
		MediaRefs:      mediaRefs,
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// contextRows is the verifier's "what was expected" block: the work done, how many animals it
// covered, and which day it was planned for. Farm language, rendered verbatim.
func contextRows(in pccareapp.VerificationEnqueueRequest) []verificationdomain.ContextRow {
	rows := make([]verificationdomain.ContextRow, 0, 3)
	rows = append(rows, verificationdomain.ContextRow{Label: "Work", Value: pccaredomain.CategoryLabel(in.Category)})
	if strings.TrimSpace(in.VaccineLabel) != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Vaccine", Value: strings.TrimSpace(in.VaccineLabel)})
	}
	if in.AnimalCount > 0 {
		rows = append(rows, verificationdomain.ContextRow{Label: "Animals in this task", Value: strconv.Itoa(int(in.AnimalCount))})
	}
	if strings.TrimSpace(in.PlannedBusinessDate) != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Planned for", Value: in.PlannedBusinessDate})
	}
	return rows
}

func ptrIfSet(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
