package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The kid stage ladder auto-raiser (maintainer decision 2026-08-20).
//
// K0 -> K1 at 2 days of age, K1 -> K2 at 7 days (domain.KidStageLadder), business-day grain in
// Asia/Kolkata. The SYSTEM raises the shifting when a park has exactly ONE pen carrying the
// step's target tag; a park with ZERO or SEVERAL candidate pens gets an operator-facing due CARD
// instead ("operator will select any one") — auto-picking one of two K1 pens would move animals
// to a pen nobody chose, and refusing outright would hide due work.
//
// The raise is the SAME movement every operator raise produces — a pending shifting_events row
// plus its counts approval request — so nothing downstream changes: Park Head approval first,
// operator completion with the mandatory video second, verification third. The ladder automates
// the RAISE, never the move.

// KidStageLadderActorID is the fixed system actor recorded as the raiser of an auto-raised
// movement. It resolves to no workforce name on purpose: the approvals queue drops an
// unresolvable "Raised by" clause rather than rendering an id, which is the correct rendering of
// "the system raised this".
const KidStageLadderActorID = "00000000-0000-4000-8000-00000000ade0"

// kidStageSourceSystem matches the app write path's source_system so the movement satisfies the
// shifting_events source CHECK and reads as a canonical Goat OS raise.
const kidStageSourceSystem = "goatos_canonical"

// kidStageStageModeDestination mirrors the app write path's stage_mode vocabulary (the string the
// phone's tag toggle sends): the auto-raise always adopts the destination pen's tag.
const kidStageStageModeDestination = "destination_stage"

// KidStageLadderRepo is the narrow read port the raiser needs beyond the two services.
type KidStageLadderRepo interface {
	ListKidStageDueGoats(ctx context.Context, tenantID, fromStage string, bornOnOrBefore time.Time) ([]domain.KidStageDueGoat, error)
	ShiftingDestinationCatalog(ctx context.Context, tenantID string) (domain.ShiftingDestinationCatalog, error)
}

// KidStageLadderRaiser wires the due-set read to the standard raise pipeline.
type KidStageLadderRaiser struct {
	repo      KidStageLadderRepo
	counts    *Service
	approvals *ApprovalService
}

func NewKidStageLadderRaiser(repo KidStageLadderRepo, counts *Service, approvals *ApprovalService) *KidStageLadderRaiser {
	return &KidStageLadderRaiser{repo: repo, counts: counts, approvals: approvals}
}

// KidStageAutoRaiseResult reports what one sweep did, for the cmd's output and for tests.
type KidStageAutoRaiseResult struct {
	BusinessDate string
	// Raised is one entry per auto-raised movement.
	Raised []KidStageRaisedMovement
	// CardGroups are the due groups the sweep could NOT auto-raise (zero or several candidate
	// pens): the operator card's content.
	CardGroups []domain.KidStageDueGroup
}

type KidStageRaisedMovement struct {
	ShiftingEventID   string
	ApprovalRequestID string
	ParkID            string
	FromStage         string
	ToStage           string
	DestinationShedID string
	DestinationLabel  string
	GoatCount         int
	IdempotentReplay  bool
}

// DueGroups computes the due kids per (park, ladder step) with their candidate destination pens,
// as of the given instant's BUSINESS DATE. It is the shared read behind both the auto-raise and
// the operator card, so the two can never disagree about who is due.
func (r *KidStageLadderRaiser) DueGroups(ctx context.Context, tenantID string, asOf time.Time) ([]domain.KidStageDueGroup, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrMissingRequiredField
	}
	dayStart := biztime.BusinessDayStart(asOf)

	catalog, err := r.repo.ShiftingDestinationCatalog(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("counts: kid stage ladder catalog: %w", err)
	}
	parkNames := make(map[string]string, len(catalog.Parks))
	for _, park := range catalog.Parks {
		parkNames[park.ParkID] = park.Name
	}

	var groups []domain.KidStageDueGroup
	for _, step := range domain.KidStageLadder() {
		due, err := r.repo.ListKidStageDueGoats(ctx, tenantID, step.FromStage, domain.KidStageBornOnOrBefore(step, dayStart))
		if err != nil {
			return nil, fmt.Errorf("counts: kid stage ladder due goats (%s): %w", step.FromStage, err)
		}
		if len(due) == 0 {
			continue
		}
		byPark := make(map[string][]domain.KidStageDueGoat)
		var parkOrder []string
		for _, goat := range due {
			if _, seen := byPark[goat.ParkID]; !seen {
				parkOrder = append(parkOrder, goat.ParkID)
			}
			byPark[goat.ParkID] = append(byPark[goat.ParkID], goat)
		}
		for _, parkID := range parkOrder {
			groups = append(groups, domain.KidStageDueGroup{
				ParkID:     parkID,
				ParkName:   parkNames[parkID],
				FromStage:  step.FromStage,
				ToStage:    step.ToStage,
				Goats:      byPark[parkID],
				Candidates: kidStageCandidatePens(catalog, parkID, step.ToStage),
			})
		}
	}
	return groups, nil
}

// kidStageCandidatePens returns the park's pens whose RESOLVED destination tag equals the step's
// target — through domain.ResolveShiftingDestinationPenStage, the ONE resolver every raise uses,
// so the pen the sweeper picks is exactly the pen a manual raise to it would stamp.
func kidStageCandidatePens(catalog domain.ShiftingDestinationCatalog, parkID, toStage string) []domain.ShiftingDestinationShed {
	var out []domain.ShiftingDestinationShed
	for _, park := range catalog.Parks {
		if park.ParkID != parkID {
			continue
		}
		for _, shed := range park.Sheds {
			resolved := domain.ResolveShiftingDestinationPenStage(shed.ConfiguredStage, shed.ManagementStages, catalog.ManagementStages)
			if resolved != "" && strings.EqualFold(resolved, toStage) {
				out = append(out, shed)
			}
		}
	}
	return out
}

// AutoRaise raises one movement per auto-raisable due group and returns the rest as card groups.
//
// Idempotency: the client key embeds the business date AND a digest of the exact goat set, so a
// rerun on the same day is safe two ways — kids already raised are excluded by the in-flight
// filter (the group vanishes), and a group whose membership grew (a birth recorded late) gets a
// NEW key rather than colliding with the earlier raise's fingerprint.
func (r *KidStageLadderRaiser) AutoRaise(ctx context.Context, tenantID string, asOf time.Time) (KidStageAutoRaiseResult, error) {
	groups, err := r.DueGroups(ctx, tenantID, asOf)
	if err != nil {
		return KidStageAutoRaiseResult{}, err
	}
	result := KidStageAutoRaiseResult{BusinessDate: biztime.BusinessDate(asOf)}
	for _, group := range groups {
		if !group.AutoRaisable() {
			result.CardGroups = append(result.CardGroups, group)
			continue
		}
		raised, err := r.raiseGroup(ctx, tenantID, asOf, group, group.Candidates[0])
		if err != nil {
			return result, fmt.Errorf("counts: kid stage auto-raise %s %s->%s: %w",
				group.ParkID, group.FromStage, group.ToStage, err)
		}
		result.Raised = append(result.Raised, raised)
	}
	return result, nil
}

func (r *KidStageLadderRaiser) raiseGroup(
	ctx context.Context,
	tenantID string,
	asOf time.Time,
	group domain.KidStageDueGroup,
	pen domain.ShiftingDestinationShed,
) (KidStageRaisedMovement, error) {
	goatIDs := make([]string, 0, len(group.Goats))
	for _, goat := range group.Goats {
		goatIDs = append(goatIDs, goat.GoatID)
	}
	sort.Strings(goatIDs)

	// The key is the movement's logical identity: this park, this step, this business date, this
	// exact animal set. Deterministic so a crashed sweep replays onto the same row.
	clientKey := fmt.Sprintf("auto-kid-stage:%s:%s:%s:%s",
		group.ParkID, strings.ToLower(group.ToStage), biztime.BusinessDate(asOf), shortDigest(goatIDs))
	canonical := clientKey + ":" + pen.ShedID + ":" + derefBlank(pen.PartitionLabel)

	impacts, err := r.counts.DeriveShiftingImpacts(ctx, tenantID, pen.ShedID, goatIDs)
	if err != nil {
		return KidStageRaisedMovement{}, fmt.Errorf("derive impacts: %w", err)
	}
	// Mixed source pens are expected here (kids gathered across kidding pens); the derivation
	// degrades shed/partition to absent for a mixed group and only a cross-park set errors —
	// impossible by construction, since groups are built per park.
	sourceParkID, sourceShedID, sourcePartition, err := r.counts.DeriveShiftingSource(ctx, tenantID, goatIDs)
	if err != nil {
		return KidStageRaisedMovement{}, fmt.Errorf("derive source: %w", err)
	}

	raisedAt := asOf.In(biztime.DefaultLocation())
	// The pen was selected because it RESOLVES to the step's target tag (kidStageCandidatePens
	// goes through the one shared resolver), so the target stage IS the ladder step's tag.
	targetStage := group.ToStage

	event := domain.ShiftingEvent{
		TenantID:                  tenantID,
		LogicalShiftingEventKey:   "app-counts-shifting:" + clientKey,
		Priority:                  "low",
		Category:                  "growth",
		SourceParkID:              sourceParkID,
		SourceShedID:              sourceShedID,
		SourcePartitionLabel:      sourcePartition,
		DestinationParkID:         group.ParkID,
		DestinationShedID:         pen.ShedID,
		DestinationPartitionLabel: pen.PartitionLabel,
		ManagementStageMode:       kidStageStageModeDestination,
		TargetManagementStage:     targetStage,
		RaisedAt:                  raisedAt,
		EffectiveAt:               raisedAt,
		SourceSystem:              kidStageSourceSystem,
		SourceRef:                 "auto-kid-stage-ladder:" + clientKey,
		PayloadHash:               stableKidStageHash("counts-app-shifting-payload", canonical),
		IdempotencyKey:            "app-counts-shifting:" + clientKey,
		RequestFingerprint:        stableKidStageHash("counts-app-shifting-request", canonical),
		Impacts:                   impacts,
	}
	eventID, replay, err := r.counts.RecordShiftingEvent(ctx, event)
	if err != nil {
		return KidStageRaisedMovement{}, fmt.Errorf("record event: %w", err)
	}

	comment := fmt.Sprintf("Raised automatically: %d %s kid(s) reached %s age (%s).",
		len(goatIDs), group.FromStage, group.ToStage, biztime.BusinessDate(asOf))
	approvalPayload, err := json.Marshal(struct {
		ShiftingEventID           string   `json:"shifting_event_id"`
		DestinationParkID         string   `json:"destination_park_id"`
		DestinationShedID         string   `json:"destination_shed_id"`
		DestinationPartitionLabel *string  `json:"destination_partition_label,omitempty"`
		SourceParkID              *string  `json:"source_park_id,omitempty"`
		SourceShedID              *string  `json:"source_shed_id,omitempty"`
		SourcePartitionLabel      *string  `json:"source_partition_label,omitempty"`
		Priority                  string   `json:"priority,omitempty"`
		Category                  string   `json:"category,omitempty"`
		ManagementStageMode       string   `json:"management_stage_mode"`
		TargetManagementStage     string   `json:"target_management_stage"`
		Comment                   *string  `json:"comment,omitempty"`
		GoatIDs                   []string `json:"goat_ids"`
	}{
		ShiftingEventID:           eventID,
		DestinationParkID:         group.ParkID,
		DestinationShedID:         pen.ShedID,
		DestinationPartitionLabel: pen.PartitionLabel,
		SourceParkID:              sourceParkID,
		SourceShedID:              sourceShedID,
		SourcePartitionLabel:      sourcePartition,
		Priority:                  "low",
		Category:                  "growth",
		ManagementStageMode:       kidStageStageModeDestination,
		TargetManagementStage:     targetStage,
		Comment:                   &comment,
		GoatIDs:                   goatIDs,
	})
	if err != nil {
		return KidStageRaisedMovement{}, fmt.Errorf("approval payload: %w", err)
	}
	request, requestReplay, err := r.approvals.SubmitRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           tenantID,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            approvalPayload,
		ShiftingEventID:    &eventID,
		RaisedByUserID:     KidStageLadderActorID,
		RaisedAt:           raisedAt,
		IdempotencyKey:     "app-counts-shifting:" + clientKey,
		RequestFingerprint: stableKidStageHash("counts-app-shifting-request", canonical),
	})
	if err != nil {
		return KidStageRaisedMovement{}, fmt.Errorf("submit approval: %w", err)
	}
	return KidStageRaisedMovement{
		ShiftingEventID:   eventID,
		ApprovalRequestID: request.ApprovalRequestID,
		ParkID:            group.ParkID,
		FromStage:         group.FromStage,
		ToStage:           group.ToStage,
		DestinationShedID: pen.ShedID,
		DestinationLabel:  pen.Display,
		GoatCount:         len(goatIDs),
		IdempotentReplay:  replay && requestReplay,
	}, nil
}

func shortDigest(goatIDs []string) string {
	h := sha256.Sum256([]byte(strings.Join(goatIDs, ",")))
	return hex.EncodeToString(h[:])[:12]
}

func stableKidStageHash(scope, canonical string) string {
	h := sha256.Sum256([]byte(scope + "\x00" + canonical))
	return hex.EncodeToString(h[:])
}

func derefBlank(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
