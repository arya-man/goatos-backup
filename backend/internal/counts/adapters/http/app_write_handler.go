package http

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// App-tier Counts write surface (mobile-facing).
//
// These three endpoints let a FIELD OPERATOR record the three count-moving events from the phone: a
// shifting (movement) event, a birth, and a death.
//
// These routes record operator facts and apply the workflow-specific gate. Concretely:
//
//   - birth: immediately creates one canonical child per litter member plus one count-only PENDING
//     approval. Each goat.created event opens that child's Birth work; approval only admits the
//     litter to herd counts.
//   - death: creates a PENDING request only. The animal stays alive and its open obligations stay
//     open until a ceo_internal approves it. The dead+died guardrail is enforced HERE at submit so
//     a bad pairing is rejected on the phone, and again at approval.
//   - shifting: unchanged in that shifting_events already landed with
//     authorization_state='pending'; the new part is the linked approval request that puts it in a
//     park_head's queue. Approving it MOVES THE ANIMALS named in goat_ids.
//
// The payload is validated at SUBMIT time through the owning module's Prepare* seam so an operator
// gets an immediate, specific error instead of a rejection days later from an approver.
//
// Every route requires a client Idempotency-Key and honours the repo's mandatory idempotency
// contract: first call performs the write, an exact replay returns the original result with no new
// side effects, and a same-key/different-payload replay is rejected with 409.
//
// Decision endpoints live in approval_handler.go.
const (
	appShiftingEventRoute = "/app/counts/shifting-events"
	appBirthEventRoute    = "/app/counts/birth-events"
	appDeathEventRoute    = "/app/counts/death-events"

	// appPromoteIdentifierRoute assigns a permanent RFID to a temporary-tagged goat (the Convert
	// tab). Gated on CountsWrite: promoting a temp tag is the operator's retag, not an approval.
	appPromoteIdentifierRoute = "/app/counts/goats/{goat_id}/promote-identifier"

	// appShiftingDestinationsRoute serves the park -> shed cascade the shifting form's destination
	// dropdowns are built from. It is a READ on the write surface: it is gated on CountsWrite, not
	// on the admin-tier locations.read, because the operator who must pick a destination is exactly
	// the operator who may record the movement -- and RolesAuthorize ANDs a route's permissions, so
	// naming locations.read here would lock out every operator who lacks it.
	appShiftingDestinationsRoute = "/app/counts/shifting/destinations"

	// appTemporaryTaggedGoatsRoute lists goats that still carry an active temporary tag (the operator
	// "Awaiting RFID" list that the Convert/promote flow acts on). Like the destinations cascade it is
	// a READ on the write surface, gated on CountsWrite: the operator who may promote is exactly the
	// operator who must see the list, and RolesAuthorize ANDs a route's permissions -- naming
	// counts.read here (leadership-only) would 403 every field operator.
	appTemporaryTaggedGoatsRoute = "/app/counts/goats/temporary-tagged"

	// appBirthBreedsRoute serves the breed picker on the operator birth form: the breeds present on
	// the live herd. Like the destinations cascade it is a READ on the write surface, gated on
	// CountsWrite -- the operator who records a birth is exactly the operator who picks the newborn's
	// breed, and the read-only Counts Breakdown that also exposes breeds is CountsRead, which a field
	// operator does not hold. RolesAuthorize ANDs a route's permissions, so naming counts.read here
	// would 403 every field operator.
	appBirthBreedsRoute = "/app/counts/breeds"

	appShiftingEventCommand = "counts.app.shifting_event"
	appBirthEventCommand    = "counts.app.birth_event"
	appDeathEventCommand    = "counts.app.death_event"

	// appShiftingSourceSystem marks these rows as a first-party canonical write (not a file
	// import). It is one of the values allowed by shifting_events_source_check.
	appShiftingSourceSystem = "goatos_canonical"

	appWriteMaxBodyBytes = 1 << 20
)

// ShiftingEventRecorder is the minimal slice of counts/app.Service this handler needs.
// *countsapp.Service already satisfies it.
type ShiftingEventRecorder interface {
	RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (id string, replay bool, err error)

	// ShiftingDestinations serves the operator's park -> shed destination cascade.
	ShiftingDestinations(ctx context.Context, tenantID string) (domain.ShiftingDestinationCatalog, error)

	// ActiveBreeds serves the operator birth form's breed picker: the breeds present on the live herd.
	ActiveBreeds(ctx context.Context, tenantID string) ([]domain.CountsBreakdownSeriesPoint, error)

	// DeriveShiftingImpacts builds the impact rows for a single-animal movement that supplied none.
	DeriveShiftingImpacts(ctx context.Context, tenantID, destinationShedID string, goatIDs []string) ([]domain.ShiftingEventImpact, error)

	// DeriveShiftingSource reads a single named animal's current park/shed so a movement submitted
	// without an explicit source still records where it started.
	DeriveShiftingSource(ctx context.Context, tenantID string, goatIDs []string) (parkID *string, shedID *string, err error)
}

// NOTE: the handler prepares each birth child through identity validation, but the approval service
// owns the single transaction that persists the litter and its count-only approval request.
//
// Death remains unapplied until approval; births create canonical goats at submission and approval
// only flips the litter's herd-count eligibility. Both mutations still use identity's guarded
// commands rather than a handler-owned direct write seam.

// GoatLifecycleValidator validates a birth/death payload WITHOUT applying it, so an operator learns
// at submit time that a dob is malformed or that a death is not a valid dead+died pairing.
// *identityapp.Service satisfies it.
type GoatLifecycleValidator interface {
	PrepareCreateAdminGoat(ctx context.Context, in identityapp.CreateAdminGoatInput) (identityports.CreateAdminGoatCommand, error)
	BirthProvisionalPrefix(ctx context.Context, tenantID, parkID string) (string, error)
	PrepareCriticalDeathExit(ctx context.Context, in identityapp.ExitGoatInput) (identityports.ExitGoatCommand, error)
	// PromoteTemporaryIdentifier assigns a permanent RFID to a temporary-tagged goat, atomically
	// retiring the temp. Applied directly (not an approval): it is the operator's retag action, like
	// the admin identifier flow. *identityapp.Service satisfies it.
	PromoteTemporaryIdentifier(ctx context.Context, in identityapp.PromoteTemporaryIdentifierInput) (*identitydomain.AdminGoatResponse, error)
	// ListTemporaryTaggedGoats backs the operator "Awaiting RFID" list -- the goats a promote can be
	// run against. It is a READ on the write surface (gated on CountsWrite): the operator who may
	// promote is exactly the operator who must see the list. *identityapp.Service satisfies it.
	ListTemporaryTaggedGoats(ctx context.Context, in identityapp.ListTemporaryTaggedGoatsInput) (*identitydomain.TemporaryTaggedGoatsResult, error)
}

type AppWriteHandler struct {
	shifting  ShiftingEventRecorder
	approvals ApprovalWorkflow
	validator GoatLifecycleValidator
	// execution owns what happens AFTER a shifting is authorized: complete, cancel, and the
	// operator's pending-execution queue. See shifting_execution_handler.go.
	execution ShiftingExecutionWorkflow
	log       *slog.Logger
}

func NewAppWriteHandler(shifting ShiftingEventRecorder, log *slog.Logger) *AppWriteHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AppWriteHandler{shifting: shifting, log: log}
}

// WithApprovalWorkflow turns the three submit routes into PENDING-request writers and enables the
// decision surface. Without it the handler has no approval workflow wired and the submit routes
// report that rather than silently applying (which is the behaviour the maintainer decision
// retired).
func (h *AppWriteHandler) WithApprovalWorkflow(approvals ApprovalWorkflow, validator GoatLifecycleValidator) *AppWriteHandler {
	h.approvals = approvals
	h.validator = validator
	return h
}

func RegisterAppWrites(mux *http.ServeMux, h *AppWriteHandler) {
	mux.HandleFunc("GET "+appShiftingDestinationsRoute, h.ListShiftingDestinations)
	mux.HandleFunc("GET "+appBirthBreedsRoute, h.ListBirthBreeds)
	mux.HandleFunc("GET "+appTemporaryTaggedGoatsRoute, h.ListTemporaryTaggedGoats)
	mux.HandleFunc("POST "+appShiftingEventRoute, h.RecordShiftingEvent)
	mux.HandleFunc("POST "+appBirthEventRoute, h.RecordBirthEvent)
	mux.HandleFunc("POST "+appDeathEventRoute, h.RecordDeathEvent)
	mux.HandleFunc("POST "+appPromoteIdentifierRoute, h.PromoteTemporaryIdentifier)
}

type appPromoteIdentifierResponse struct {
	GoatID           string `json:"goat_id"`
	IdempotentReplay bool   `json:"idempotent_replay"`
}

// PromoteTemporaryIdentifier assigns a permanent RFID to a temporary-tagged goat, atomically
// retiring the temp. Applied directly through identity's guarded promote command -- no approval.
func (h *AppWriteHandler) PromoteTemporaryIdentifier(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	if h.validator == nil {
		h.writeError(w, r, http.StatusNotImplemented, "promote_unavailable",
			"identity promote workflow is not configured", nil)
		return
	}
	goatID := strings.TrimSpace(r.PathValue("goat_id"))
	if goatID == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_goat_id", "goat_id is required", nil)
		return
	}
	clientKey, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	result, err := h.validator.PromoteTemporaryIdentifier(r.Context(), identityapp.PromoteTemporaryIdentifierInput{
		TenantID:       tenantID,
		ActorID:        httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey: clientKey,
		TraceID:        appTraceID(r),
		GoatID:         goatID,
		RawBody:        body,
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, appPromoteIdentifierResponse{
		GoatID:           result.Goat.GoatID,
		IdempotentReplay: result.Idempotency.Replayed,
	})
}

// ---------------------------------------------------------------------------
// Shifting events
// ---------------------------------------------------------------------------

type appShiftingEventRequest struct {
	SourceParkID      *string                    `json:"source_park_id,omitempty"`
	SourceShedID      *string                    `json:"source_shed_id,omitempty"`
	DestinationParkID string                     `json:"destination_park_id"`
	DestinationShedID string                     `json:"destination_shed_id"`
	EffectiveAt       *time.Time                 `json:"effective_at,omitempty"`
	Priority          string                     `json:"priority,omitempty"`
	Category          string                     `json:"category,omitempty"`
	ProofRef          *string                    `json:"proof_ref,omitempty"`
	Impacts           []appShiftingImpactRequest `json:"impacts"`

	// Comment is the raiser's OPTIONAL note on why the animals are moving (maintainer decision
	// 2026-07-31). It is read by the approving park head and by the verifier reviewing the
	// evidence. A blank or whitespace-only value normalizes to absent, so "the operator wrote
	// nothing" has exactly one representation downstream instead of two.
	Comment *string `json:"comment,omitempty"`

	// GoatIDs names the individual animals this movement covers. REQUIRED, and load-bearing:
	// approving the request relocates EXACTLY these animals to the destination shed.
	//
	// Maintainer decision (2026-07-19): a shifting event must name the animals it moves. It was
	// previously optional because shifting_events is an aggregate model -- impacts record "12 head
	// of Boer" by breed grain, never which twelve animals -- so a count-only movement had no
	// per-animal list to give. That is exactly the shape being retired: an authorized movement that
	// names nobody relocates nobody, so the herd register and the shed-scoped vaccination
	// obligations silently disagree with the count the operator just reported. This service will
	// still NOT infer which animals a head count referred to, because guessing would relocate real
	// animals that nobody selected -- so the list is now demanded up front, at SUBMIT, where the
	// operator can still supply it. The seeded shifting SOP form DSL has always declared goat_ids
	// as a required goat_lookup field; this makes the API contract agree with it.
	// See the note in domain.ShiftingApprovalEffect.
	GoatIDs []string `json:"goat_ids"`
}

type appShiftingImpactRequest struct {
	BreedID        *string `json:"breed_id,omitempty"`
	BreedKey       string  `json:"breed_key"`
	BreedLabel     string  `json:"breed_label,omitempty"`
	StageTag       *string `json:"stage_tag,omitempty"`
	AgeClass       *string `json:"age_class,omitempty"`
	Sex            *string `json:"sex,omitempty"`
	HeadCount      int32   `json:"head_count"`
	PregnantCount  int32   `json:"pregnant_count,omitempty"`
	LactatingCount int32   `json:"lactating_count,omitempty"`
	WarmupCount    int32   `json:"warmup_count,omitempty"`
}

type appShiftingEventResponse struct {
	ShiftingEventID string `json:"shifting_event_id"`
	// ApprovalRequestID is the queue entry a park head decides. The movement is NOT applied until
	// that decision.
	ApprovalRequestID string `json:"approval_request_id"`
	Status            string `json:"status"`
	IdempotentReplay  bool   `json:"idempotent_replay"`
}

// appApprovalSubmitResponse is shared by birth/death. Birth fills Children with the canonical
// goats created at submit; death leaves it empty because its exit still applies on approval.
type appApprovalSubmitResponse struct {
	ApprovalRequestID string                    `json:"approval_request_id"`
	RequestType       string                    `json:"request_type"`
	Status            string                    `json:"status"`
	RaisedAt          time.Time                 `json:"raised_at"`
	IdempotentReplay  bool                      `json:"idempotent_replay"`
	Children          []domain.BirthChildResult `json:"children,omitempty"`
}

// maxShiftingCommentRunes bounds the raiser's optional note. Counted in RUNES so the limit is
// the same number of characters an operator typing Kannada, Telugu, or Hindi sees, and so it
// matches Postgres char_length() in shifting_events_raise_comment_length_check exactly.
const maxShiftingCommentRunes = 1000

var (
	// Governing product-doc taxonomy (maintainer decision 2026-07-20): Priority High/Low,
	// Category Growth/Health/Breeding/Delivery. The prior normal/high/emergency +
	// routine/pregnancy/warmup/... vocabulary is retired (see migration 000016).
	allowedShiftingPriority = map[string]bool{"high": true, "low": true}
	allowedShiftingCategory = map[string]bool{
		"growth": true, "health": true, "breeding": true, "delivery": true,
	}
)

// RecordShiftingEvent records an operator-reported movement between sheds.
//
// The event is recorded with the service defaults authorization_state="pending" /
// verification_state="unverified" / event_status="pending": a field operator REPORTS a movement,
// they do not self-authorize it.
func (h *AppWriteHandler) RecordShiftingEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	clientKey, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	var req appShiftingEventRequest
	if err := decodeStrictJSON(body, &req, "RecordShiftingEventRequest"); err != nil {
		h.writeAppError(w, r, err)
		return
	}
	normalized, err := normalizeShiftingEventRequest(req)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	// The movement adopts the DESTINATION SHED's cohort. The operator is not asked (maintainer
	// decision 2026-08-03, superseding the three-mode chooser): the raise resolves one concrete
	// answer from the same backend-owned catalog the form already renders, and snapshots it.
	//
	// Resolving HERE, at raise time, rather than at completion is deliberate and unchanged from the
	// superseded design: the snapshot is what the park head approves and what the audit trail
	// shows. A completion-time re-read would let the destination shed's residents drift between
	// approval and application, so the stage actually applied would be one nobody approved.
	//
	// domain.ResolveShiftingDestinationStage owns the rule and its fallbacks; "" means preserve each
	// animal's current stage, which is the relocation path's existing behaviour for an empty target.
	stageMode, targetStage := "keep_current", ""
	{
		catalog, catalogErr := h.shifting.ShiftingDestinations(r.Context(), tenantID)
		if catalogErr != nil {
			h.writeCountsError(w, r, catalogErr)
			return
		}
		var destinationStages []string
		for _, park := range catalog.Parks {
			for _, shed := range park.Sheds {
				if shed.ShedID == normalized.DestinationShedID {
					destinationStages = shed.ManagementStages
				}
			}
		}
		if resolved := domain.ResolveShiftingDestinationStage(destinationStages, catalog.ManagementStages); resolved != "" {
			// Recorded as the existing 'destination_stage' mode: the column's meaning ("this target
			// came from the destination shed") is exactly what the resolver produced, so no schema
			// change is needed and pre-existing rows keep their recorded raise-time intent.
			stageMode, targetStage = "destination_stage", resolved
		}
	}

	// CR-05: check that the approval workflow is available BEFORE writing anything.
	//
	// The movement row (shifting_events) and its approval request are separate transactions; a
	// shifting_events row is inert until an approval links it and flips it to authorized, so an
	// orphan is a pending movement nobody can act on, and a client retry with the same
	// Idempotency-Key replays the event insert and creates the missing request, converging. The one
	// window that does NOT self-heal is the workflow being UNCONFIGURED: retrying forever cannot
	// create a request when there is no workflow to create it in, so the movement would sit orphaned
	// permanently. Gate that deterministic case here, before the event write, so an unavailable
	// workflow records NO movement at all rather than a permanent orphan.
	if h.approvals == nil {
		h.writeError(w, r, http.StatusNotImplemented, "approvals_unavailable",
			"approval workflow is not configured", nil)
		return
	}

	// payload_hash and request_fingerprint are derived ONLY from the canonical client request, never
	// from server-generated values such as raised_at. That is what makes an exact replay hash
	// identically (so the repo returns the original row instead of inserting a second movement) while
	// a same-key/different-payload replay hashes differently and is rejected as a conflict.
	canonical, err := canonicalRequestBytes(tenantID, appShiftingEventCommand, appShiftingEventRoute, normalized)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	// Same instant, tagged Asia/Kolkata: raised_at/effective_at are business timestamps that get
	// stored, displayed, and bucketed into business days, so the location must be the business one.
	// This is safe for idempotency: the canonical hash above covers only the client request.
	raisedAt := time.Now().In(biztime.DefaultLocation())
	// effective_at stays OPTIONAL and server-defaulted: the operator is standing at the shed
	// reporting a movement that just happened, so "now" is the right answer and the phone no longer
	// sends the field. It remains part of the contract for back-dated corrections.
	effectiveAt := raisedAt
	if normalized.EffectiveAt != nil {
		effectiveAt = *normalized.EffectiveAt
	}

	// Impacts are derived AFTER the canonical hash is taken, never before, and that ordering is
	// load-bearing for idempotency. The hash must cover only what the CLIENT sent: if a derived
	// impact were folded into it, an exact replay of the same request would hash differently the
	// moment the animal's breed or stage row changed underneath, and a legitimate retry would be
	// rejected as a same-key/different-payload conflict.
	impacts := shiftingImpacts(normalized)
	if len(impacts) == 0 {
		derived, err := h.shifting.DeriveShiftingImpacts(
			r.Context(), tenantID, normalized.DestinationShedID, normalized.GoatIDs)
		if err != nil {
			h.writeCountsError(w, r, err)
			return
		}
		impacts = derived
	}

	// The source park/shed is backfilled here for the same reason, under the same ordering rule: the
	// phone no longer sends a source the server can read off the animal, but the stored event still
	// has to record where the movement started. Deriving it AFTER the canonical hash keeps a retry
	// that arrives once the animal has already been moved hashing identically, so it replays onto the
	// original row instead of being rejected as a same-key/different-payload conflict.
	//
	// An explicitly supplied source always wins: a back-dated or corrective submission is stating a
	// source the current placement no longer reflects, and overwriting it with "where the animal is
	// now" would silently rewrite the operator's claim.
	// Backfilling the source DEGRADES, it never fails the write. Unlike the impact derivation --
	// which is the only thing standing between a movement and an empty projection row, and so fails
	// closed -- the source is enrichment of a field the caller was allowed to omit. Failing here
	// would newly impose goat-existence validation on the explicit-impacts path, which deliberately
	// does not read the animal at all, and would reject writes that are valid today.
	sourceParkID, sourceShedID := normalized.SourceParkID, normalized.SourceShedID
	if sourceParkID == nil && sourceShedID == nil {
		derivedPark, derivedShed, err := h.shifting.DeriveShiftingSource(r.Context(), tenantID, normalized.GoatIDs)
		switch {
		case errors.Is(err, countsapp.ErrImpactNotDerivable):
			// Multi-animal movement: no single truthful origin. Leave the source absent rather than
			// labelling every animal with one animal's shed.
		case errors.Is(err, ports.ErrGoatNotFound):
			// Nothing to read the source from. The no-impacts path still fails closed on this same
			// condition a few lines above, via DeriveShiftingImpacts.
		case err != nil:
			h.writeCountsError(w, r, err)
			return
		default:
			sourceParkID, sourceShedID = derivedPark, derivedShed
			// CR-02: cross-park validation must ALSO run on the DERIVED source, not only on an
			// explicit one. normalizeShiftingEventRequest rejects an explicit source_park_id that
			// disagrees with the destination, but the simplified submit omits source_park_id entirely,
			// so without this a goat standing in park A could produce an AUTHORIZED A->B movement:
			// the cross-park invariant would only be caught at approval time by the relocation's
			// ground-truth guard, after the event, its outbox row, and the approval request were all
			// written. Fail closed HERE, before any of those writes, so a cross-park submit records
			// nothing at all.
			if derivedPark != nil && *derivedPark != normalized.DestinationParkID {
				h.writeAppError(w, r, identityapp.BadRequest("cross_park_move_forbidden",
					"a shifting event must keep the animals within the same park"))
				return
			}
		}
	}

	event := domain.ShiftingEvent{
		TenantID: tenantID,
		// The logical key is the business identity of this reported movement. Deriving it from the
		// client idempotency key keeps an exact retry (same key) collapsing onto the same row, while
		// two genuinely separate submissions stay two separate movements.
		LogicalShiftingEventKey: "app-counts-shifting:" + clientKey,
		Priority:                normalized.Priority,
		Category:                normalized.Category,
		SourceParkID:            sourceParkID,
		SourceShedID:            sourceShedID,
		DestinationParkID:       normalized.DestinationParkID,
		DestinationShedID:       normalized.DestinationShedID,
		ManagementStageMode:     stageMode,
		TargetManagementStage:   targetStage,
		RaisedAt:                raisedAt,
		EffectiveAt:             effectiveAt,
		SourceSystem:            appShiftingSourceSystem,
		SourceRef:               appShiftingEventRoute + ":" + clientKey,
		ProofRef:                normalized.ProofRef,
		RaiseComment:            normalized.Comment,
		PayloadHash:             stableHash("counts-app-shifting-payload", canonical),
		IdempotencyKey:          "app-counts-shifting:" + clientKey,
		RequestFingerprint:      stableHash("counts-app-shifting-request", canonical),
		Impacts:                 impacts,
	}

	id, replay, err := h.shifting.RecordShiftingEvent(r.Context(), event)
	if err != nil {
		h.writeCountsError(w, r, err)
		return
	}

	// The movement row is already pending (authorization_state='pending'); the approval request is
	// what puts it in a park head's queue and what carries the animals to move on approval.
	//
	// The two writes are separate transactions, which is safe here ONLY because a shifting_events
	// row is inert until authorized: an orphan is a pending movement nobody can act on, never an
	// applied one. Both writes are keyed off the same client Idempotency-Key, so a retry replays
	// the event insert and creates the missing request, converging. (Birth and death have no such
	// pending representation, which is exactly why their payloads are held on the request itself.)
	// Workflow availability was already checked before the event write (CR-05), so an unconfigured
	// workflow can no longer leave a permanent orphan here.
	approvalPayload, err := json.Marshal(struct {
		ShiftingEventID       string  `json:"shifting_event_id"`
		DestinationParkID     string  `json:"destination_park_id"`
		DestinationShedID     string  `json:"destination_shed_id"`
		SourceParkID          *string `json:"source_park_id,omitempty"`
		SourceShedID          *string `json:"source_shed_id,omitempty"`
		Priority              string  `json:"priority,omitempty"`
		Category              string  `json:"category,omitempty"`
		ManagementStageMode   string  `json:"management_stage_mode"`
		TargetManagementStage string  `json:"target_management_stage"`
		// The raiser's note travels WITH the approval request, not just on the movement row: the
		// park head decides from this payload, so a comment the operator wrote to justify the move
		// has to be in front of them at the moment they approve or reject.
		Comment *string `json:"comment,omitempty"`
		// Never omitempty: normalizeShiftingEventRequest guarantees a non-empty set, so a stored
		// payload without goat_ids is a corruption signal the approval path must be able to see.
		GoatIDs []string `json:"goat_ids"`
	}{
		ShiftingEventID:   id,
		DestinationParkID: normalized.DestinationParkID,
		DestinationShedID: normalized.DestinationShedID,
		// The approval payload carries the SAME derived source the event stored, so the request a
		// park head reads shows the movement's real origin rather than a blank "from".
		SourceParkID:          sourceParkID,
		SourceShedID:          sourceShedID,
		Priority:              normalized.Priority,
		Category:              normalized.Category,
		ManagementStageMode:   stageMode,
		TargetManagementStage: targetStage,
		Comment:               normalized.Comment,
		GoatIDs:               normalized.GoatIDs,
	})
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
		return
	}
	shiftingEventID := id
	request, requestReplay, err := h.approvals.SubmitRequest(r.Context(), domain.ApprovalRequestSubmission{
		TenantID:           tenantID,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            approvalPayload,
		ShiftingEventID:    &shiftingEventID,
		RaisedByUserID:     httpmiddleware.ActorIDFromContext(r.Context()),
		RaisedAt:           raisedAt,
		IdempotencyKey:     "app-counts-shifting:" + clientKey,
		RequestFingerprint: stableHash("counts-app-shifting-request", canonical),
	})
	if err != nil {
		h.writeApprovalError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, appShiftingEventResponse{
		ShiftingEventID:   id,
		ApprovalRequestID: request.ApprovalRequestID,
		Status:            request.Status,
		IdempotentReplay:  replay && requestReplay,
	})
}

func normalizeShiftingEventRequest(req appShiftingEventRequest) (appShiftingEventRequest, error) {
	req.SourceParkID = trimOptionalPtr(req.SourceParkID)
	req.SourceShedID = trimOptionalPtr(req.SourceShedID)
	req.ProofRef = trimOptionalPtr(req.ProofRef)
	req.Comment = trimOptionalPtr(req.Comment)
	req.DestinationParkID = strings.TrimSpace(req.DestinationParkID)
	req.DestinationShedID = strings.TrimSpace(req.DestinationShedID)
	req.Priority = strings.ToLower(strings.TrimSpace(req.Priority))
	req.Category = strings.ToLower(strings.TrimSpace(req.Category))
	if req.EffectiveAt != nil {
		// Normalize to UTC so two representations of the same instant are the same request.
		utc := req.EffectiveAt.UTC()
		req.EffectiveAt = &utc
	}
	if req.DestinationParkID == "" {
		return req, identityapp.BadRequest("missing_destination_park_id", "destination_park_id is required")
	}
	if req.DestinationShedID == "" {
		return req, identityapp.BadRequest("missing_destination_shed_id", "destination_shed_id is required")
	}
	// P0-1: Cross-park move prevention. Goats never move between parks; shed moves exist only
	// within one park. Validate that source_park_id == destination_park_id when a source is supplied.
	if req.SourceParkID != nil && *req.SourceParkID != req.DestinationParkID {
		return req, identityapp.BadRequest("cross_park_move_forbidden",
			"a shifting event must keep the animals within the same park")
	}
	// A present-but-invalid enum is REJECTED, never silently rewritten to a default the operator
	// never chose. An absent value falls through to the service's declared default.
	if req.Priority != "" && !allowedShiftingPriority[req.Priority] {
		return req, identityapp.BadRequest("invalid_priority", "priority must be high or low")
	}
	if req.Category != "" && !allowedShiftingCategory[req.Category] {
		return req, identityapp.BadRequest("invalid_category", "category must be growth, health, breeding, or delivery")
	}
	// A present-but-too-long comment is REJECTED, never silently truncated: the operator's own
	// words go in front of an approver and a verifier, so quietly cutting them changes what the
	// decision-maker reads. The bound matches shifting_events_raise_comment_length_check, so the
	// API and the column agree instead of the write failing later with a constraint violation.
	if req.Comment != nil && utf8.RuneCountInString(*req.Comment) > maxShiftingCommentRunes {
		return req, identityapp.BadRequest("comment_too_long",
			fmt.Sprintf("comment must be at most %d characters", maxShiftingCommentRunes))
	}
	// goat_ids is REQUIRED: a shifting event must name the animals it moves (maintainer decision,
	// 2026-07-19). Rejecting here -- at SUBMIT -- rather than at approval is deliberate: the
	// operator who knows which animals moved is standing at the shed right now, whereas the park
	// head approving hours later is not. A missing list must fail while it can still be supplied.
	if len(req.GoatIDs) == 0 {
		return req, identityapp.BadRequest("missing_goat_ids",
			"goat_ids is required and must name at least one animal")
	}
	// goat_ids is bounded and de-duplicated at SUBMIT time so the stored payload is already the
	// exact set approval will relocate. A duplicate id would otherwise make the approval's
	// requested-vs-moved comparison fail closed on a request that is actually fine.
	if len(req.GoatIDs) > identityports.MaxRelocateGoatsPerCommand {
		return req, identityapp.BadRequest("too_many_goat_ids",
			fmt.Sprintf("goat_ids must contain at most %d animals", identityports.MaxRelocateGoatsPerCommand))
	}
	seen := make(map[string]struct{}, len(req.GoatIDs))
	unique := make([]string, 0, len(req.GoatIDs))
	for i, id := range req.GoatIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			return req, identityapp.BadRequest("invalid_goat_id",
				fmt.Sprintf("goat_ids[%d] must not be empty", i))
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	sort.Strings(unique)
	req.GoatIDs = unique
	// impacts is OPTIONAL for the single-animal case and REQUIRED otherwise.
	//
	// impacts describes the movement at breed/cohort grain ("12 head of Boer, 3 pregnant"), which is
	// what the count projection consumes. Demanding it from a field operator who is moving ONE
	// animal the system already has canonical breed/stage/sex facts for is pure friction, so the
	// server derives that single row itself (see Service.DeriveShiftingImpacts).
	//
	// For TWO OR MORE animals the server derives one cohort row per distinct (shed, breed) cohort and
	// sums identical animals (Service.DeriveShiftingImpacts). A genuinely MIXED same-grain set --
	// same breed at the same shed but differing stage/age/sex -- is not auto-aggregated: derivation
	// returns ErrImpactNotDerivable and the handler answers missing_impacts, so the operator supplies
	// explicit per-cohort impacts. Note this is a RELAXATION at the transport edge only: counts/app.Service
	// still rejects an event that reaches it with zero impacts, so the invariant "a recorded movement
	// has at least one impact" is unchanged and still enforced below the handler.
	if len(req.Impacts) == 0 && len(req.GoatIDs) == 0 {
		return req, identityapp.BadRequest("missing_impacts",
			"impacts is required unless goat_ids names the animals to derive them from")
	}
	for i := range req.Impacts {
		impact := req.Impacts[i]
		impact.BreedID = trimOptionalPtr(impact.BreedID)
		impact.StageTag = trimOptionalPtr(impact.StageTag)
		impact.AgeClass = trimOptionalPtr(impact.AgeClass)
		impact.Sex = trimOptionalPtr(impact.Sex)
		impact.BreedKey = strings.ToLower(strings.TrimSpace(impact.BreedKey))
		impact.BreedLabel = strings.TrimSpace(impact.BreedLabel)
		if impact.BreedKey == "" {
			return req, identityapp.BadRequest("missing_breed_key", fmt.Sprintf("impacts[%d].breed_key is required", i))
		}
		if impact.BreedLabel == "" {
			impact.BreedLabel = impact.BreedKey
		}
		req.Impacts[i] = impact
	}
	return req, nil
}

func shiftingImpacts(req appShiftingEventRequest) []domain.ShiftingEventImpact {
	out := make([]domain.ShiftingEventImpact, 0, len(req.Impacts))
	for _, impact := range req.Impacts {
		out = append(out, domain.ShiftingEventImpact{
			GrainKey:       strings.ToLower(req.DestinationShedID) + ":" + impact.BreedKey,
			BreedID:        impact.BreedID,
			BreedKey:       impact.BreedKey,
			BreedLabel:     impact.BreedLabel,
			StageTag:       impact.StageTag,
			AgeClass:       impact.AgeClass,
			Sex:            impact.Sex,
			HeadCount:      impact.HeadCount,
			PregnantCount:  impact.PregnantCount,
			LactatingCount: impact.LactatingCount,
			WarmupCount:    impact.WarmupCount,
			RiskFlagsJSON:  []byte("{}"),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Birth events
// ---------------------------------------------------------------------------

// RecordBirthEvent records a birth as a goat creation with origin_type pinned to "birth".
//
// There is no birth aggregate: a birth IS goat creation. The body is the identity module's
// CreateAdminGoatRequest, so identity keeps ownership of every rule (dob required, dob <= entry_date,
// identifier uniqueness, location resolution, evidence refs) and of the idempotency contract. This
// handler only pins origin_type so the app route cannot create a procured/imported animal.
func (h *AppWriteHandler) RecordBirthEvent(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	fields, err := decodeJSONObject(body, "RecordBirthEventRequest")
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	// origin_type is pinned, not silently rewritten: an absent value becomes "birth", but a
	// PRESENT value that disagrees is rejected rather than being quietly overwritten.
	if raw, present := fields["origin_type"]; present {
		var declared string
		if err := json.Unmarshal(raw, &declared); err != nil || strings.TrimSpace(declared) != "birth" {
			h.writeError(w, r, http.StatusBadRequest, "invalid_origin_type",
				"origin_type must be birth on "+appBirthEventRoute, nil)
			return
		}
	}
	fields["origin_type"] = json.RawMessage(`"birth"`)

	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	clientKey, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	if h.approvals == nil || h.validator == nil {
		h.writeError(w, r, http.StatusNotImplemented, "approvals_unavailable",
			"approval workflow is not configured", nil)
		return
	}

	var litterSize int
	if raw, present := fields["litter_size"]; !present || json.Unmarshal(raw, &litterSize) != nil || litterSize < 1 || litterSize > 3 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_litter_size", "litter_size must be 1, 2, or 3", nil)
		return
	}
	if litterSize > 1 && (jsonFieldPresent(fields, "animal_identifier_1") || jsonFieldPresent(fields, "temporary_identifier")) {
		h.writeError(w, r, http.StatusBadRequest, "identifier_not_allowed_for_litter",
			"twins and triplets receive one server-generated provisional identifier per child", nil)
		return
	}
	var parkID string
	if raw, present := fields["park_id"]; !present || json.Unmarshal(raw, &parkID) != nil || strings.TrimSpace(parkID) == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_park_id", "park_id is required", nil)
		return
	}
	prefix, err := h.validator.BirthProvisionalPrefix(r.Context(), tenantID, parkID)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}

	// One submission fans out to one independently identified canonical goat per child. Every
	// command is fully identity-validated before the Counts repository commits the litter and its
	// count-approval row atomically. The per-child key and derived tag are stable across offline
	// retries; collision salts are bounded and deterministic.
	commands := make([]identityports.CreateAdminGoatCommand, 0, litterSize)
	childDescriptors := make([]map[string]any, 0, litterSize)
	usedTags := make(map[string]struct{}, litterSize)
	canonicalFields := cloneJSONFields(fields)
	delete(canonicalFields, "animal_identifier_1")
	delete(canonicalFields, "animal_identifier_2")
	delete(canonicalFields, "temporary_identifier")
	const maxProvisionalAttempts = 10
	for childOrdinal := 1; childOrdinal <= litterSize; childOrdinal++ {
		var prepared identityports.CreateAdminGoatCommand
		var temporaryIdentifier string
		for attempt := 0; attempt < maxProvisionalAttempts; attempt++ {
			temporaryIdentifier = deriveProvisionalTemporaryTag(prefix, clientKey, childOrdinal, attempt)
			if _, duplicate := usedTags[temporaryIdentifier]; duplicate {
				continue
			}
			childFields := cloneJSONFields(canonicalFields)
			rawTag, marshalErr := json.Marshal(temporaryIdentifier)
			if marshalErr != nil {
				h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", marshalErr)
				return
			}
			childFields["temporary_identifier"] = rawTag
			forwarded, marshalErr := json.Marshal(childFields)
			if marshalErr != nil {
				h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", marshalErr)
				return
			}
			prepared, err = h.validator.PrepareCreateAdminGoat(r.Context(), identityapp.CreateAdminGoatInput{
				TenantID: tenantID, ActorID: httpmiddleware.ActorIDFromContext(r.Context()),
				IdempotencyKey: fmt.Sprintf("%s:child:%d", clientKey, childOrdinal),
				TraceID:        appTraceID(r), RawBody: forwarded,
			})
			if err == nil {
				break
			}
			if !isIdentifierOwnedError(err) {
				h.writeAppError(w, r, err)
				return
			}
		}
		if err != nil {
			h.writeAppError(w, r, err)
			return
		}
		if prepared.DamID == nil {
			h.writeError(w, r, http.StatusBadRequest, "mother_not_found", "mother RFID must resolve to a canonical female goat", nil)
			return
		}
		usedTags[temporaryIdentifier] = struct{}{}
		commands = append(commands, prepared)
		childDescriptors = append(childDescriptors, map[string]any{
			"child_ordinal": childOrdinal, "temporary_identifier": temporaryIdentifier,
		})
		if childOrdinal == 1 {
			rawDam, _ := json.Marshal(*prepared.DamID)
			canonicalFields["dam_id"] = rawDam
		}
	}
	childrenRaw, err := json.Marshal(childDescriptors)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
		return
	}
	canonicalFields["children"] = childrenRaw
	forwarded, err := json.Marshal(canonicalFields)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	canonical, err := canonicalRequestBytes(tenantID, appBirthEventCommand, appBirthEventRoute, json.RawMessage(forwarded))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	result, err := h.approvals.SubmitBirthRequest(r.Context(), domain.ApprovalRequestSubmission{
		TenantID:           tenantID,
		RequestType:        domain.ApprovalRequestTypeBirth,
		Payload:            forwarded,
		RaisedByUserID:     httpmiddleware.ActorIDFromContext(r.Context()),
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "app-counts-birth:" + clientKey,
		RequestFingerprint: stableHash("counts-app-birth-request", canonical),
	}, commands)
	if err != nil {
		h.writeApprovalError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusAccepted, appApprovalSubmitResponse{
		ApprovalRequestID: result.Approval.ApprovalRequestID,
		RequestType:       result.Approval.RequestType,
		Status:            result.Approval.Status,
		RaisedAt:          result.Approval.RaisedAt,
		IdempotentReplay:  result.Replayed,
		Children:          result.Children,
	})
}

// ---------------------------------------------------------------------------
// Death events
// ---------------------------------------------------------------------------

// RecordDeathEvent records a death through the dedicated guardrailed critical-death exit.
//
// GUARDRAIL: the lifecycle_status="dead" + exit_reason="died" pairing is NOT re-implemented or
// relaxed here. The body (minus the goat_id addressing field, which this route carries in the body
// rather than the path) is forwarded verbatim to identity's CriticalDeathExit, whose
// validateCriticalDeathExit still rejects anything that is not that exact pairing. The ordinary
// /exit path still refuses death, and this route still emits goat.exited, which auto-cancels the
// animal's open obligations.
func (h *AppWriteHandler) RecordDeathEvent(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	fields, err := decodeJSONObject(body, "RecordDeathEventRequest")
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	rawGoatID, present := fields["goat_id"]
	if !present {
		h.writeError(w, r, http.StatusBadRequest, "missing_goat_id", "goat_id is required", nil)
		return
	}
	var goatID string
	if err := json.Unmarshal(rawGoatID, &goatID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_goat_id", "goat_id must be a string", nil)
		return
	}
	// goat_id addresses the animal; identity's ExitGoatRequest decodes strictly, so it must not
	// remain in the forwarded body. Every other field is passed through untouched.
	delete(fields, "goat_id")
	forwarded, err := json.Marshal(fields)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}

	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}
	clientKey, err := appIdempotencyKey(r)
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}
	if h.approvals == nil || h.validator == nil {
		h.writeError(w, r, http.StatusNotImplemented, "approvals_unavailable",
			"approval workflow is not configured", nil)
		return
	}
	subjectGoatID := strings.TrimSpace(goatID)

	// GUARDRAIL AT SUBMIT TIME. PrepareCriticalDeathExit runs validateCriticalDeathExit, so a body
	// that is not the exact lifecycle_status="dead" + exit_reason="died" pairing is rejected right
	// here -- the operator finds out immediately, and a payload the guarded path would refuse can
	// never be parked in an approver's queue. The guardrail runs AGAIN at approval, on the same
	// guarded command, and once more inside identity's adapter. Preparing applies nothing: the
	// animal is exited, and its open obligations cancelled, only when the request is approved.
	if _, err := h.validator.PrepareCriticalDeathExit(r.Context(), identityapp.ExitGoatInput{
		TenantID:       tenantID,
		ActorID:        httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey: clientKey,
		TraceID:        appTraceID(r),
		GoatID:         subjectGoatID,
		RawBody:        forwarded,
	}); err != nil {
		h.writeAppError(w, r, err)
		return
	}

	// The stored payload keeps goat_id inline (this route has no {goat_id} path segment); the
	// approval path splits it back out before handing the body to identity.
	stored, err := json.Marshal(fields)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	var storedFields map[string]json.RawMessage
	if err := json.Unmarshal(stored, &storedFields); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
		return
	}
	storedFields["goat_id"] = rawGoatID
	storedPayload, err := json.Marshal(storedFields)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
		return
	}

	canonical, err := canonicalRequestBytes(tenantID, appDeathEventCommand, appDeathEventRoute, json.RawMessage(storedPayload))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", err)
		return
	}
	request, replay, err := h.approvals.SubmitRequest(r.Context(), domain.ApprovalRequestSubmission{
		TenantID:           tenantID,
		RequestType:        domain.ApprovalRequestTypeDeath,
		Payload:            storedPayload,
		SubjectGoatID:      &subjectGoatID,
		RaisedByUserID:     httpmiddleware.ActorIDFromContext(r.Context()),
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "app-counts-death:" + clientKey,
		RequestFingerprint: stableHash("counts-app-death-request", canonical),
	})
	if err != nil {
		h.writeApprovalError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusAccepted, appApprovalSubmitResponse{
		ApprovalRequestID: request.ApprovalRequestID,
		RequestType:       request.RequestType,
		Status:            request.Status,
		RaisedAt:          request.RaisedAt,
		IdempotentReplay:  replay,
	})
}

// ---------------------------------------------------------------------------
// Shared transport helpers
// ---------------------------------------------------------------------------

func (h *AppWriteHandler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, appWriteMaxBodyBytes))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is too large or unreadable", err)
		return nil, false
	}
	return body, true
}

// appIdempotencyKey applies the same client idempotency-key rule identity's write paths apply
// (validateWriteHeaders), so all three Counts app writes reject a missing/oversized key identically.
func appIdempotencyKey(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return "", identityapp.BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	if len(key) < 8 || len(key) > 200 {
		return "", identityapp.BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	return key, nil
}

func appTraceID(r *http.Request) string {
	if traceID := httpmiddleware.TraceIDFromContext(r.Context()); traceID != "" {
		return traceID
	}
	return "missing-trace"
}

func decodeStrictJSON(raw []byte, dest any, schemaName string) error {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return identityapp.BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return identityapp.BadRequest("invalid_json", "request body must match "+schemaName)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return identityapp.BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return nil
}

// decodeJSONObject parses the body as a generic JSON object so the handler can pin/strip exactly one
// transport field before forwarding it. Field-level validation stays in the owning module, which
// decodes the forwarded body strictly against its own schema.
func decodeJSONObject(raw []byte, schemaName string) (map[string]json.RawMessage, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, identityapp.BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return nil, identityapp.BadRequest("invalid_json", "request body must match "+schemaName)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, identityapp.BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return fields, nil
}

func canonicalRequestBytes(tenantID, command, route string, body any) ([]byte, error) {
	return json.Marshal(struct {
		TenantID string `json:"tenant_id"`
		Command  string `json:"command"`
		Route    string `json:"route"`
		Body     any    `json:"body"`
	}{TenantID: tenantID, Command: command, Route: route, Body: body})
}

func stableHash(domainSeparator string, payload []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(domainSeparator))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// jsonFieldPresent reports whether a decoded body carries a non-null, non-empty string field.
// A JSON null or "" reads as absent, matching identity's trimOptionalString normalization.
func jsonFieldPresent(fields map[string]json.RawMessage, key string) bool {
	raw, ok := fields[key]
	if !ok {
		return false
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil {
		// A non-string shape is left for identity's strict decode to reject with a specific error.
		return true
	}
	return value != nil && strings.TrimSpace(*value) != ""
}

// deriveProvisionalTemporaryTag mints the park-prefixed five-digit provisional tag for one child.
//
// The tag is DERIVED FROM THE CLIENT IDEMPOTENCY KEY, never random. It is injected into the request
// body before that body is marshalled, and the same body feeds the approval request fingerprint —
// so a random tag makes every retry of one Idempotency-Key look like a changed payload, which the
// approval store rejects as ErrIdempotencyConflict (409). The Android write path is the offline
// outbox, which retries the same key until it gets a response, so that would wedge the operator's
// queue forever on a birth that already recorded. Deriving the tag makes a retry rebuild a
// byte-identical body and replay cleanly.
//
// attempt salts the derivation so the bounded collision-retry loop walks to a different tag while
// each attempt stays reproducible on replay.
func deriveProvisionalTemporaryTag(prefix, clientKey string, childOrdinal, attempt int) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "goatos-provisional-kid-tag:%s:%s:%d:%d", prefix, clientKey, childOrdinal, attempt))
	n := binary.BigEndian.Uint64(sum[:8]) % 100000
	return fmt.Sprintf("%s-%05d", strings.ToUpper(strings.TrimSpace(prefix)), n)
}

func cloneJSONFields(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// isIdentifierOwnedError matches identity's identifier-uniqueness rejection
// ("<type> already belongs to animal <id>"), the only prepare failure a regenerated provisional
// tag can heal.
func isIdentifierOwnedError(err error) bool {
	var appErr *identityapp.Error
	return errors.As(err, &appErr) && appErr.Code == "invalid_goat_create" &&
		strings.Contains(appErr.Message, "already belongs to animal")
}

func trimOptionalPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (h *AppWriteHandler) writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *identityapp.Error
	if errors.As(err, &appErr) {
		h.writeError(w, r, appErr.HTTPStatus, appErr.Code, appErr.Message, err)
		return
	}
	h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
}

// writeCountsError maps the counts module's own sentinel errors onto HTTP status codes. The two
// conflict sentinels are the idempotency contract's "same key, different payload" rejection.
func (h *AppWriteHandler) writeCountsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict",
			"Idempotency-Key was reused with a different payload", err)
	case errors.Is(err, ports.ErrLogicalKeyConflict):
		h.writeError(w, r, http.StatusConflict, "logical_key_conflict",
			"shifting event key was reused with a different payload", err)
	case errors.Is(err, ports.ErrIdempotencyInProgress):
		h.writeError(w, r, http.StatusConflict, "idempotency_in_progress",
			"a request with this Idempotency-Key is still in progress", err)
	// A named animal that genuinely does not resolve is a 404. An existing terminal animal is a
	// distinct 422 eligibility error so a dead/sold/exited RFID never reads as a missing route.
	case errors.Is(err, ports.ErrGoatNotFound):
		h.writeError(w, r, http.StatusNotFound, "goat_not_found",
			"one or more goat_ids did not resolve in this tenant", err)
	case errors.Is(err, ports.ErrGoatNotShiftable):
		h.writeError(w, r, http.StatusUnprocessableEntity, "goat_not_shiftable",
			"this animal is no longer active and cannot be shifted", err)
	case errors.Is(err, countsapp.ErrImpactNotDerivable):
		h.writeError(w, r, http.StatusBadRequest, "missing_impacts",
			"the animals in this movement form a mixed cohort (same breed and shed but differing stage/age/sex); supply explicit impacts to state the cohort split", err)
	case errors.Is(err, countsapp.ErrMissingRequiredField),
		errors.Is(err, countsapp.ErrMissingImpact),
		errors.Is(err, countsapp.ErrInvalidCount),
		errors.Is(err, countsapp.ErrInvalidJSON):
		h.writeError(w, r, http.StatusBadRequest, "invalid_shifting_event", err.Error(), err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", err)
	}
}

func (h *AppWriteHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, identitydomain.ErrorEnvelope{
		Code:        code,
		Message:     message,
		FieldErrors: []identitydomain.FieldError{},
		TraceID:     appTraceID(r),
		Retryable:   status >= http.StatusInternalServerError,
	}, cause)
}
