// Package verificationbridge adapts the verification module's CreateItem to the pen-routine
// VerificationEnqueuer port, so a submitted routine check (its captures plus its answers)
// becomes ONE generic verification item the verifier queue lists. penroutines never writes
// verification's tables.
package verificationbridge

import (
	"context"
	"strconv"
	"strings"

	proutapp "github.com/vgoats/goatos/backend/internal/penroutines/app"
	proutdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// wholePark is the location a whole-park routine's evidence is for.
const wholePark = "Whole park"

// verificationCreator is the ONE verification-service method this bridge consumes.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// Enqueuer bridges routine submits into the verifier queue.
type Enqueuer struct {
	verification verificationCreator
}

// New constructs the bridge over the verification service.
func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ proutapp.VerificationEnqueuer = (*Enqueuer)(nil)

// EnqueuePenRoutineVerification maps the submit to a verification CreateItem. CreateItem is
// idempotent on (tenant, idempotency_key), so a retry after a prior failure heals rather than
// duplicates; a redo after a rework carries a new row version and mints a fresh item.
func (e *Enqueuer) EnqueuePenRoutineVerification(ctx context.Context, in proutapp.VerificationEnqueueRequest) error {
	// Subject: "Pen cleaning · Castro 2" -- degrades to the routine name rather than composing
	// a dangling separator.
	label := strings.TrimSpace(in.RoutineName)
	if label == "" {
		label = "Routine check"
	}
	// A whole-park task names no pen: the verifier reads "Whole park" where a pen would be.
	parkTask := in.ScopeKind == proutdomain.ScopePark
	if parkTask {
		label = label + " · " + wholePark
	} else if pen := strings.TrimSpace(in.PenLabel); pen != "" {
		label = label + " · " + pen
	}
	rows := make([]verificationdomain.ContextRow, 0, 4+len(in.AnswerRows))
	if name := strings.TrimSpace(in.RoutineName); name != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Routine", Value: name})
	}
	if cadence := strings.TrimSpace(in.CadenceLine); cadence != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "When", Value: cadence})
	}
	kinds := proutdomain.SortWorkKinds(in.TriggerKinds)
	if len(kinds) > 0 {
		labels := make([]string, 0, len(kinds))
		for _, k := range kinds {
			labels = append(labels, proutdomain.WorkKindLabel(k))
		}
		value := strings.Join(labels, ", ")
		if in.SourceDate != "" {
			value += " on " + biztime.FarmDateFromBusinessDate(in.SourceDate)
		}
		rows = append(rows, verificationdomain.ContextRow{Label: "After", Value: value})
	}
	if park := strings.TrimSpace(in.ParkName); park != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Park", Value: park})
	}
	if parkTask {
		rows = append(rows, verificationdomain.ContextRow{Label: "Where", Value: wholePark})
	} else if pen := strings.TrimSpace(in.PenLabel); pen != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Pen", Value: pen})
	}
	for _, a := range in.AnswerRows {
		if strings.TrimSpace(a.Value) == "" {
			continue
		}
		rows = append(rows, verificationdomain.ContextRow{Label: a.Title, Value: a.Value})
	}
	// Every capture rides with its kind so the verifier's player is the right one for each
	// (a routine mixes photos and videos); labels count within kind: "Photo 1", "Video 1".
	media := make([]string, 0, len(in.Proofs))
	meta := make([]verificationdomain.MediaMeta, 0, len(in.Proofs))
	photos, videos := 0, 0
	for _, p := range in.Proofs {
		ref := strings.TrimSpace(p.Ref)
		if ref == "" {
			continue
		}
		media = append(media, ref)
		label := "Capture"
		switch p.Kind {
		case proutdomain.ProofKindPhoto:
			photos++
			label = "Photo " + strconv.Itoa(photos)
		case proutdomain.ProofKindVideo:
			videos++
			label = "Video " + strconv.Itoa(videos)
		}
		meta = append(meta, verificationdomain.MediaMeta{Label: label, Kind: p.Kind})
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:       in.TenantID,
		Vertical:       proutapp.VerificationVertical,
		Module:         proutapp.VerificationModule,
		Category:       proutapp.VerificationCategory,
		SubjectLabel:   &label,
		ContextRows:    rows,
		Source:         verificationdomain.SourceRef{Module: proutapp.VerificationModule, RefType: proutapp.VerificationRefType, RefID: in.TaskID},
		MediaRefs:      media,
		MediaMeta:      meta,
		OperatorID:     ptrIfSet(in.SubmittedBy),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

func ptrIfSet(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
