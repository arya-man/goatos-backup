// Package http exposes the Verification module's queue read + verdict write over REST/JSON.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

type Handler struct {
	service     *app.Service
	dutyReader  VerificationModuleDutyReader
	reviewEvent ports.ReviewEventRepository
	log         *slog.Logger
}

// VerificationModuleDutyReader resolves the active verify duties held by one authenticated actor.
// It deliberately returns module keys rather than devices: authorization must not disappear merely
// because a legitimate verifier has not registered an FCM token.
type VerificationModuleDutyReader interface {
	ListVerifyModuleKeys(ctx context.Context, tenantID, actorID string) ([]string, error)
}

// statusAll is the queue's "no status filter" query value — see the QueueStatusOption kdoc.
const statusAll = "all"

func NewHandler(service *app.Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func (h *Handler) WithModuleDutyReader(reader VerificationModuleDutyReader) *Handler {
	h.dutyReader = reader
	return h
}

// WithReviewEventRepository wires the video-review-analytics ingest/read repository
// (verification_review_events, migration 000116). Left optional/nil-safe like the duty reader
// above so existing wiring call sites do not have to change until they opt in.
func (h *Handler) WithReviewEventRepository(repo ports.ReviewEventRepository) *Handler {
	h.reviewEvent = repo
	return h
}

func Register(mux *nethttp.ServeMux, h *Handler) {
	mux.HandleFunc("GET /verification/queue", h.ListQueue)
	mux.HandleFunc("GET /verification/action-queue", h.ListActionQueue)
	mux.HandleFunc("GET /verify/alerts", h.ListAlerts)
	mux.HandleFunc("POST /verification/items/{item_id}/verdict", h.RecordVerdict)
	mux.HandleFunc("POST /verification/items/{item_id}/close", h.CloseItem)
	mux.HandleFunc("POST /verification/submissions/{submission_id}/close", h.CloseSubmission)
	mux.HandleFunc("POST /verification/vaccination-batches/{batch_id}/close", h.CloseVaccinationBatch)
	mux.HandleFunc("POST /verification/review-events", h.RecordReviewEvents)
	mux.HandleFunc("GET /verification/items/{item_id}/review-facts", h.GetItemReviewFacts)
	mux.HandleFunc("GET /verification/oversight-analytics", h.GetOversightAnalytics)
}

type queueItemResponse struct {
	ItemID       string  `json:"item_id"`
	Vertical     string  `json:"vertical"`
	Module       string  `json:"module"`
	Category     string  `json:"category"`
	SubjectLabel *string `json:"subject_label,omitempty"`
	// SubjectNote is the raiser's own words about this work item (e.g. why a movement was
	// requested), shown to the verifier alongside the evidence. Distinct from SubjectLabel,
	// which is system-composed identity.
	SubjectNote *string `json:"subject_note,omitempty"`
	// ContextRows is what the reviewed work was EXPECTED to be -- for a feed packing proof, the
	// frozen ration for that pen-session. Backend-composed label/value pairs in the producer's
	// order; clients render them verbatim and never parse them. Always present (possibly empty)
	// so a client can render the block unconditionally.
	ContextRows []contextRowResponse `json:"context_rows"`
	Status      string               `json:"status"`
	// VerdictState is what this item is DOING, as opposed to Status, which is only what the
	// verifier decided. "awaiting_review" | "applying" | "settled" -- see
	// domain.VerdictState* for why the two are not the same thing. Clients must render
	// "applying" as work still in flight, NEVER as finished.
	VerdictState               string             `json:"verdict_state"`
	VerdictReason              *string            `json:"verdict_reason,omitempty"`
	OperatorID                 *string            `json:"operator_id,omitempty"`
	OperatorName               *string            `json:"operator_name,omitempty"` // backend-owned display label
	ShedID                     *string            `json:"shed_id,omitempty"`
	ShedLabel                  *string            `json:"shed_label,omitempty"`                   // backend-owned display label
	PartitionLabel             *string            `json:"partition_label,omitempty"`              // raw partition label ('1', 'Part 3'); omitted for non-partitioned
	OperationalLocationDisplay *string            `json:"operational_location_display,omitempty"` // backend-owned composed location ("Castro 2", "Godel 1 - Part 3")
	ParkID                     *string            `json:"park_id,omitempty"`
	ParkLabel                  *string            `json:"park_label,omitempty"` // backend-owned display label
	CapturedAt                 string             `json:"captured_at"`
	VerifiedBy                 *string            `json:"verified_by,omitempty"`
	VerifiedByName             *string            `json:"verified_by_name,omitempty"` // backend-owned display label
	VerifiedAt                 *string            `json:"verified_at,omitempty"`
	ClosedBy                   *string            `json:"closed_by,omitempty"`
	ClosedAt                   *string            `json:"closed_at,omitempty"`
	RowVersion                 int                `json:"row_version"`
	Media                      []domain.MediaItem `json:"media"`
	// evidence_available means "a signed download link was resolved for every media_ref" — it does
	// NOT assert the bytes are retrievable (see domain.QueueRow.EvidenceLinkResolved). A link that
	// later 410s with proof_object_missing is the terminal signal clients must render.
	EvidenceAvailable bool           `json:"evidence_available"`
	Source            sourceResponse `json:"source"`
	// Watch is the queue table's lightweight per-item watch-telemetry summary (see
	// domain.ItemWatchState). Nil when review-event telemetry is unavailable for this deployment
	// (h.reviewEvent not wired) -- distinct from "not opened", which is a real negative fact.
	Watch *watchStateResponse `json:"watch,omitempty"`
}

// watchStateResponse is the wire shape for domain.ItemWatchState: "percent watched if known, 'not
// opened' if no item_opened event, absent when telemetry is unavailable" (see the "Watch" column
// spec on the Verify queue table).
type watchStateResponse struct {
	Opened         bool `json:"opened"`
	PercentWatched *int `json:"percent_watched,omitempty"`
}

type sourceResponse struct {
	Module       string  `json:"module"`
	TaskID       *string `json:"task_id,omitempty"`
	SubmissionID *string `json:"submission_id,omitempty"`
	RefType      string  `json:"ref_type"`
	RefID        string  `json:"ref_id"`
}

type queueListResponse struct {
	Items         []queueItemResponse              `json:"items"`
	FilterOptions domain.QueueFilterOptions        `json:"filter_options"`
	DriveClosures []domain.VaccinationBatchClosure `json:"drive_closures,omitempty"`
	NextCursor    *string                          `json:"next_cursor"`
	TraceID       string                           `json:"trace_id"`
}

// contextRowResponse is one backend-composed "what was expected" line on the wire.
type contextRowResponse struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// toContextRowResponses maps the domain rows to the wire, ALWAYS returning a non-nil slice so the
// field marshals as [] rather than null -- a client rendering a list should not have to special-case
// the empty producer.
func toContextRowResponses(rows []domain.ContextRow) []contextRowResponse {
	out := make([]contextRowResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, contextRowResponse{Label: row.Label, Value: row.Value})
	}
	return out
}

func toQueueItemResponse(row domain.QueueRow) queueItemResponse {
	var verifiedAt *string
	if row.Item.VerifiedAt != nil {
		s := row.Item.VerifiedAt.Format(rfc3339Nano)
		verifiedAt = &s
	}
	var closedAt *string
	if row.Item.ClosedAt != nil {
		s := row.Item.ClosedAt.Format(rfc3339Nano)
		closedAt = &s
	}
	media := row.Media
	if media == nil {
		media = []domain.MediaItem{}
	}
	// Compose operational_location_display through oploc; partitionLabel is compatibility metadata
	// and must not be appended to an exact shed label.
	var operationalLocationDisplay *string
	if row.Item.ShedLabel != nil {
		partLabel := ""
		if row.Item.PartitionLabel != nil {
			partLabel = *row.Item.PartitionLabel
		}
		loc := oploc.OperationalLocation{
			ShedName:       *row.Item.ShedLabel,
			PartitionLabel: partLabel,
		}
		display := loc.Display()
		if display != "" {
			operationalLocationDisplay = &display
		}
	}

	return queueItemResponse{
		ItemID:                     row.Item.ItemID,
		Vertical:                   row.Item.Vertical,
		Module:                     row.Item.Module,
		Category:                   row.Item.Category,
		SubjectLabel:               row.Item.SubjectLabel,
		SubjectNote:                row.Item.SubjectNote,
		ContextRows:                toContextRowResponses(row.Item.ContextRows),
		Status:                     row.Item.Status,
		VerdictState:               row.Item.VerdictState(),
		VerdictReason:              row.Item.VerdictReason,
		OperatorID:                 row.Item.OperatorID,
		OperatorName:               row.Item.OperatorName,
		ShedID:                     row.Item.ShedID,
		ShedLabel:                  row.Item.ShedLabel,
		PartitionLabel:             row.Item.PartitionLabel,
		OperationalLocationDisplay: operationalLocationDisplay,
		ParkID:                     row.Item.ParkID,
		ParkLabel:                  row.Item.ParkLabel,
		CapturedAt:                 row.Item.CapturedAt.Format(rfc3339Nano),
		VerifiedBy:                 row.Item.VerifiedBy,
		VerifiedByName:             row.Item.VerifiedByName,
		VerifiedAt:                 verifiedAt,
		ClosedBy:                   row.Item.ClosedBy,
		ClosedAt:                   closedAt,
		RowVersion:                 row.Item.RowVersion,
		Media:                      media,
		EvidenceAvailable:          row.EvidenceLinkResolved,
		Source: sourceResponse{
			Module:       row.Item.Source.Module,
			TaskID:       row.Item.Source.TaskID,
			SubmissionID: row.Item.Source.SubmissionID,
			RefType:      row.Item.Source.RefType,
			RefID:        row.Item.Source.RefID,
		},
	}
}

const rfc3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

func (h *Handler) ListQueue(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.listQueue(w, r, permissions.VerificationReview, "", false)
}

func (h *Handler) ListActionQueue(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.listQueue(w, r, permissions.VerificationAct, "", true)
}

// ListAlerts returns pending verification items for a module/feature. The verifier is
// tenant-scoped, so this endpoint returns all pending items across all parks for the
// requested category. The category query parameter is required.
func (h *Handler) ListAlerts(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	category := strings.TrimSpace(q.Get("category"))
	if category == "" {
		h.respondError(w, r, app.BadRequest("missing_category", "category query parameter is required"))
		return
	}
	h.listQueue(w, r, permissions.VerificationReview, domain.StatusPending, false)
}

func (h *Handler) listQueue(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	permission string,
	forcedStatus string,
	actionQueue bool,
) {
	q := r.URL.Query()
	category := strings.TrimSpace(q.Get("category"))
	var categories []string
	if permission == permissions.VerificationReview {
		authorizedCategories, ok := h.resolveVerifierCategories(w, r, category)
		if !ok {
			return
		}
		// Clear the single category ONLY when authorization actually replaced it with a
		// multi-category set. resolveVerifierCategories returns a NIL slice for CEO/CxO to mean
		// "no AUTHORIZATION narrowing is needed" -- it does not mean "ignore what the caller
		// asked for". Clearing unconditionally destroyed the caller's own filter in that case, so
		// leadership opening /verify?category=vaccination_proof from the Vaccination nav leaf got
		// every module's queue: the sidebar selection silently did nothing and the status pill
		// counted the whole tenant (52) instead of the module (19). A real verifier was never
		// affected -- the resolver hands her back []string{category} -- which is why this only
		// ever reproduced for leadership.
		if len(authorizedCategories) > 0 {
			category = ""
			categories = authorizedCategories
		}
	}
	limit, ok := parsePositiveLimit(q.Get("limit"))
	if !ok {
		h.respondError(w, r, app.BadRequest("invalid_limit", "limit must be a positive integer"))
		return
	}
	var cursor *domain.Cursor
	if raw := strings.TrimSpace(q.Get("cursor")); raw != "" {
		decoded, err := domain.DecodeCursor(raw)
		if err != nil {
			h.respondError(w, r, app.BadRequest("invalid_cursor", "cursor must be a valid verification queue cursor"))
			return
		}
		cursor = &decoded
	}
	restricted, parkIDs := verificationParkScope(r, permission)
	status := forcedStatus
	includeAllStatuses := actionQueue
	if status == "" {
		status = q.Get("status")
		// `status=all` is the explicit "no status filter" selection. It cannot be expressed by
		// omitting the parameter: an absent status defaults to pending in the service, which is the
		// landing tab. Without this, an All tab would silently render Due only.
		if strings.EqualFold(strings.TrimSpace(status), statusAll) {
			status = ""
			includeAllStatuses = true
		}
	}
	missedOnly := false
	if raw := strings.TrimSpace(q.Get("missed")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			h.respondError(w, r, app.BadRequest("invalid_missed", "missed must be a boolean"))
			return
		}
		missedOnly = parsed
	}
	params := ports.ListQueueParams{
		TenantID:             tenantID(r),
		Category:             category,
		Categories:           categories,
		Vertical:             q.Get("vertical"),
		Module:               q.Get("module"),
		NavigationModule:     q.Get("nav_module"),
		Status:               status,
		BusinessDate:         q.Get("business_date"),
		BusinessDateFrom:     q.Get("business_date_from"),
		BusinessDateTo:       q.Get("business_date_to"),
		MissedOnly:           missedOnly,
		ParkID:               q.Get("park_id"),
		ShedID:               q.Get("shed_id"),
		Cursor:               cursor,
		Limit:                limit,
		ParkIDs:              parkIDs,
		ScopeRestricted:      restricted,
		ReadyForClosure:      forcedStatus == domain.StatusApproved,
		IncludeAllStatuses:   includeAllStatuses,
		SubmissionScopedOnly: actionQueue,
		OpenOnly:             actionQueue,
		// awaiting_application=true is the verifier's "what I decided that has not landed yet"
		// view. It is what stops an emptied pending queue from being the ONLY feedback a verifier
		// gets: a verdict is applied asynchronously, so "I decided it" and "the farm's records
		// changed" are two different moments and the surface has to be able to name the gap.
		// Status is deliberately left alone -- the caller asks for the state, not for a status.
		AwaitingApplicationOnly: strings.EqualFold(strings.TrimSpace(q.Get("awaiting_application")), "true"),
		// IsVerifierQueueRead is true for the verifier queue read (verification.review path),
		// false for other callers (leadership action queue, alerts). Verifier queue read does NOT
		// clamp to today — it returns the full pending backlog ordered oldest-first.
		IsVerifierQueueRead: permission == permissions.VerificationReview && !actionQueue,
		// OversightFiltersEnabled gates the CROSS-MODULE oversight filters (module chips,
		// capture-date range) on the CAPABILITY, never on a role string. See
		// permissions.VerificationOversee and ports.ListQueueParams.OversightFiltersEnabled.
		OversightFiltersEnabled: holdsVerificationPermission(r, permissions.VerificationOversee),
	}
	result, err := h.service.ListQueue(r.Context(), params)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	// Drive-closure cards belong to whoever holds verification.act (CEO/Director), NOT to the
	// endpoint. Gating them on actionQueue meant leadership only saw them because their Videos
	// nav happened to point at /verify/action; moving that href to the review queue silently
	// removed the close card. The verifier holds verification.verdict, never act, so this stays
	// off their queue.
	var closures []domain.VaccinationBatchClosure
	if holdsVerificationPermission(r, permissions.VerificationAct) {
		closures, err = h.service.ListReadyVaccinationBatchClosures(r.Context(), params)
		if err != nil {
			h.respondError(w, r, err)
			return
		}
	}
	items := make([]queueItemResponse, len(result.Items))
	itemIDs := make([]string, len(result.Items))
	for i, row := range result.Items {
		items[i] = toQueueItemResponse(row)
		itemIDs[i] = row.Item.ItemID
	}
	// Watch state is a bounded batch read over exactly this page's item_ids -- never the whole
	// queue -- so it stays a single query per page load. Missing telemetry (h.reviewEvent unwired,
	// or the query itself failing) degrades to an absent "watch" field per row rather than failing
	// the whole queue read: the Watch column is additive UI, not a queue-read dependency.
	if h.reviewEvent != nil && len(itemIDs) > 0 {
		if states, err := h.reviewEvent.WatchStates(r.Context(), params.TenantID, itemIDs); err == nil {
			for i, id := range itemIDs {
				if state, ok := states[id]; ok {
					items[i].Watch = &watchStateResponse{Opened: state.Opened, PercentWatched: state.PercentWatched}
				}
			}
		} else {
			h.log.Warn("verification.watch_states_failed", "error", err)
		}
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, queueListResponse{Items: items, FilterOptions: result.FilterOptions, DriveClosures: closures, NextCursor: result.NextCursor, TraceID: traceID(r)})
}

type verdictRequest struct {
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type verdictResponse struct {
	Item    queueItemResponse `json:"item"`
	TraceID string            `json:"trace_id"`
}

func (h *Handler) RecordVerdict(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body verdictRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	item, err := h.service.GetItem(r.Context(), tenantID(r), r.PathValue("item_id"))
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	if !verificationItemInScope(r, item, permissions.VerificationReview) {
		h.respondError(w, r, app.NotFound("item_not_found", "verification item not found"))
		return
	}
	if !h.authorizeSingleCategory(w, r, item.Category) {
		return
	}
	item, err = h.service.RecordVerdict(r.Context(), domain.Verdict{
		TenantID:       tenantID(r),
		ItemID:         r.PathValue("item_id"),
		Decision:       body.Decision,
		Reason:         body.Reason,
		VerifierID:     actorID(r),
		RowVersion:     body.RowVersion,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, verdictResponse{
		Item:    toQueueItemResponse(domain.QueueRow{Item: item}),
		TraceID: traceID(r),
	})
}

// resolveVerifierCategories returns the authorized categories for a verifier queue read.
//
// When category is specified, it validates that single category.
// When category is empty and the principal is a verifier (has verification.verdict),
// it resolves ALL categories for the modules the verifier is assigned to, enabling
// the "All evidence" view across multiple categories.
// When category is empty and the principal is NOT a verifier (CEO/CxO), it returns
// all categories (unrestricted view).
func (h *Handler) resolveVerifierCategories(w nethttp.ResponseWriter, r *nethttp.Request, category string) ([]string, bool) {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	if len(grants) == 0 || hasTenantWideRole(grants, tenantID(r), permissions.RoleCEOInternal) {
		// CEO/CxO has unrestricted access; no category filtering needed
		return nil, true
	}
	// A principal who cannot cast a VERDICT is not a verifier, and the module-duty scoping below
	// exists to keep ONE verifier inside the modules he is on duty for. Gating an oversight READ
	// on verify duties denies the queue to leadership entirely: the PC Director has no
	// position_module_duties row, so this returned 403 "verifier is not assigned to any module"
	// for a role whose whole job is to watch that module (observed on-device 2026-08-08, once the
	// missing verification.review grant was fixed and the request finally reached this gate).
	//
	// This restores the behaviour the function's own doc describes -- "when the principal is NOT a
	// verifier ... unrestricted view" -- which was implemented as a check for the CEO ROLE rather
	// than for the absence of the verdict PERMISSION, so every other leadership role fell through
	// into the verifier path. Read-only: casting a verdict is separately gated on
	// VerificationVerdict, which these principals do not hold.
	if !grantsAuthorize(grants, permissions.VerificationVerdict) {
		return nil, true
	}

	// If a specific category is provided, validate it
	if category != "" {
		module := ""
		for _, def := range h.service.Categories() {
			if def.Category == category {
				module = def.NavigationModule
				break
			}
		}
		if module == "" {
			h.respondError(w, r, app.BadRequest("unknown_category", "category is not registered"))
			return nil, false
		}
		if h.dutyReader == nil {
			h.respondError(w, r, app.Forbidden("module_scope_forbidden", "verifier is not assigned to this module"))
			return nil, false
		}
		modules, err := h.dutyReader.ListVerifyModuleKeys(r.Context(), tenantID(r), actorID(r))
		if err != nil {
			h.respondError(w, r, err)
			return nil, false
		}
		for _, allowed := range modules {
			// Compare in NAVIGATION-key space: duties are stored as module codes ("pc.vaccination"),
			// categories are registered against navigation keys ("vaccination").
			if navigationModuleForDutyCode(allowed) == module {
				return []string{category}, true
			}
		}
		h.respondError(w, r, app.Forbidden("module_scope_forbidden", "verifier is not assigned to this module"))
		return nil, false
	}

	// Category is empty: resolve all authorized categories for this verifier
	if h.dutyReader == nil {
		// Verifier is not configured with a duty reader, cannot resolve categories
		h.respondError(w, r, app.Forbidden("module_scope_forbidden", "verifier modules not configured"))
		return nil, false
	}

	modules, err := h.dutyReader.ListVerifyModuleKeys(r.Context(), tenantID(r), actorID(r))
	if err != nil {
		h.respondError(w, r, err)
		return nil, false
	}

	if len(modules) == 0 {
		// Verifier has no assigned modules, cannot provide a queue
		h.respondError(w, r, app.Forbidden("module_scope_forbidden", "verifier is not assigned to any module"))
		return nil, false
	}

	// Map module keys to categories
	authorizedCategories := make(map[string]bool)
	for _, def := range h.service.Categories() {
		// Check if this category's module is in the verifier's authorized modules
		defModule := def.NavigationModule
		for _, dutyModule := range modules {
			// Compare in NAVIGATION-key space
			if navigationModuleForDutyCode(dutyModule) == defModule {
				authorizedCategories[def.Category] = true
				break
			}
		}
	}

	if len(authorizedCategories) == 0 {
		// No categories found for the assigned modules
		h.respondError(w, r, app.Forbidden("module_scope_forbidden", "no categories available for assigned modules"))
		return nil, false
	}

	// Convert map to sorted slice for consistent ordering
	categories := make([]string, 0, len(authorizedCategories))
	for cat := range authorizedCategories {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	return categories, true
}

// authorizeSingleCategory validates authorization for a single specified category.
// Used by verdict recording and other single-item operations where a category is known.
func (h *Handler) authorizeSingleCategory(w nethttp.ResponseWriter, r *nethttp.Request, category string) bool {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	if len(grants) == 0 || hasTenantWideRole(grants, tenantID(r), permissions.RoleCEOInternal) {
		// CEO/CxO has unrestricted access
		return true
	}

	if category == "" {
		h.respondError(w, r, app.BadRequest("missing_category", "category is required for this operation"))
		return false
	}

	// Find the module for this category
	module := ""
	for _, def := range h.service.Categories() {
		if def.Category == category {
			module = def.NavigationModule
			break
		}
	}
	if module == "" {
		h.respondError(w, r, app.BadRequest("unknown_category", "category is not registered"))
		return false
	}

	if h.dutyReader == nil {
		h.respondError(w, r, app.Forbidden("module_scope_forbidden", "verifier is not assigned to this module"))
		return false
	}

	modules, err := h.dutyReader.ListVerifyModuleKeys(r.Context(), tenantID(r), actorID(r))
	if err != nil {
		h.respondError(w, r, err)
		return false
	}

	for _, allowed := range modules {
		// Compare in NAVIGATION-key space: duties are stored as module codes ("pc.vaccination"),
		// categories are registered against navigation keys ("vaccination").
		if navigationModuleForDutyCode(allowed) == module {
			return true
		}
	}

	h.respondError(w, r, app.Forbidden("module_scope_forbidden", "verifier is not assigned to this module"))
	return false
}

func hasTenantWideRole(grants []permissions.ActiveGrant, tenantID, role string) bool {
	for _, grant := range grants {
		if grant.Role == role && grant.ScopeType == "tenant" && grant.ScopeID == tenantID {
			return true
		}
	}
	return false
}

type closeItemRequest struct {
	RowVersion int `json:"row_version"`
}

func (h *Handler) CloseItem(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body closeItemRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	item, err := h.service.GetItem(r.Context(), tenantID(r), r.PathValue("item_id"))
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	if !verificationItemInScope(r, item, permissions.VerificationAct) {
		h.respondError(w, r, app.NotFound("item_not_found", "verification item not found"))
		return
	}
	item, err = h.service.CloseItem(r.Context(), domain.CloseAction{
		TenantID:       tenantID(r),
		ItemID:         r.PathValue("item_id"),
		ActorID:        actorID(r),
		RowVersion:     body.RowVersion,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, verdictResponse{
		Item:    toQueueItemResponse(domain.QueueRow{Item: item}),
		TraceID: traceID(r),
	})
}

type closeSubmissionResponse struct {
	Items   []queueItemResponse `json:"items"`
	TraceID string              `json:"trace_id"`
}

func (h *Handler) CloseSubmission(w nethttp.ResponseWriter, r *nethttp.Request) {
	idempotencyKey, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	submissionID := r.PathValue("submission_id")
	items, err := h.service.GetSubmissionItems(r.Context(), tenantID(r), submissionID)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	for _, item := range items {
		if !verificationItemInScope(r, item, permissions.VerificationAct) {
			h.respondError(w, r, app.NotFound("submission_not_found", "verification submission not found"))
			return
		}
	}
	items, err = h.service.CloseSubmission(r.Context(), domain.CloseSubmissionAction{
		TenantID:       tenantID(r),
		SubmissionID:   submissionID,
		ActorID:        actorID(r),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	responseItems := make([]queueItemResponse, len(items))
	for i, item := range items {
		responseItems[i] = toQueueItemResponse(domain.QueueRow{Item: item})
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, closeSubmissionResponse{
		Items:   responseItems,
		TraceID: traceID(r),
	})
}

func (h *Handler) CloseVaccinationBatch(w nethttp.ResponseWriter, r *nethttp.Request) {
	idempotencyKey, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	batchID := r.PathValue("batch_id")
	restricted, parkIDs := verificationParkScope(r, permissions.VerificationAct)
	closures, err := h.service.ListReadyVaccinationBatchClosures(r.Context(), ports.ListQueueParams{
		TenantID: tenantID(r), Category: "vaccination_proof",
		ParkIDs: parkIDs, ScopeRestricted: restricted,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	allowed := false
	for _, closure := range closures {
		if closure.BatchID == batchID {
			allowed = true
			break
		}
	}
	// A restricted (park-scoped) verifier is authorized only for batches inside their own park
	// scope -- ListReadyVaccinationBatchClosures is the sole source of that scoping today, so an
	// unlisted batch for a restricted caller stays a 404 (never leak batches outside scope).
	// An UNRESTRICTED caller (CEO/director) is authorized tenant-wide, so an unlisted batch here
	// means only ONE thing: it is not yet fully verified. Falling straight through to
	// CloseVaccinationBatch below (instead of a bare "not found") lets its named refusal --
	// "N animals still awaiting verification: G-00X, ..." -- reach the caller, which is what makes
	// leadership sign-off actionable instead of a dead end that looks like a missing batch.
	if !allowed && restricted {
		h.respondError(w, r, app.NotFound("batch_not_found", "vaccination batch not found"))
		return
	}
	items, err := h.service.CloseVaccinationBatch(r.Context(), domain.CloseVaccinationBatchAction{
		TenantID:       tenantID(r),
		BatchID:        batchID,
		ActorID:        actorID(r),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	responseItems := make([]queueItemResponse, len(items))
	for i, item := range items {
		responseItems[i] = toQueueItemResponse(domain.QueueRow{Item: item})
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, closeSubmissionResponse{
		Items:   responseItems,
		TraceID: traceID(r),
	})
}

func verificationParkScope(r *nethttp.Request, permission string) (bool, []string) {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	if len(grants) == 0 || hasTenantWidePermission(grants, tenantID(r), permission) {
		return false, nil
	}
	return true, permissions.ScopeIDsForPermission(grants, permission, "park")
}

// holdsVerificationPermission reports whether the caller holds the permission at ANY scope
// (tenant-wide or on at least one park), independent of which queue endpoint they called.
func holdsVerificationPermission(r *nethttp.Request, permission string) bool {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	if len(grants) == 0 {
		return false
	}
	if hasTenantWidePermission(grants, tenantID(r), permission) {
		return true
	}
	return len(permissions.ScopeIDsForPermission(grants, permission, "park")) > 0
}

func hasTenantWidePermission(grants []permissions.ActiveGrant, tenant, permission string) bool {
	for _, scopeID := range permissions.ScopeIDsForPermission(grants, permission, "tenant") {
		if scopeID == tenant {
			return true
		}
	}
	return false
}

func verificationItemInScope(r *nethttp.Request, item domain.Item, permission string) bool {
	restricted, parkIDs := verificationParkScope(r, permission)
	if !restricted {
		return true
	}
	if item.ParkID == nil {
		return false
	}
	for _, parkID := range parkIDs {
		if parkID == *item.ParkID {
			return true
		}
	}
	return false
}

func (h *Handler) respondError(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	status := nethttp.StatusInternalServerError
	envelope := domain.ErrorEnvelope{
		Code:        "internal_error",
		Message:     "internal server error",
		FieldErrors: []domain.FieldError{},
		TraceID:     traceID(r),
		Retryable:   true,
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		status = appErr.HTTPStatus
		envelope.Code = appErr.Code
		envelope.Message = appErr.Message
		envelope.Retryable = appErr.Retryable
		if len(appErr.FieldErrors) > 0 {
			envelope.FieldErrors = make([]domain.FieldError, len(appErr.FieldErrors))
			for i, fe := range appErr.FieldErrors {
				envelope.FieldErrors[i] = domain.FieldError{Field: fe.Field, Code: fe.Code, Message: fe.Message}
			}
		}
	}
	httpresponse.WriteError(w, r, h.log, status, envelope, err)
}

func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, dst any) bool {
	body, err := io.ReadAll(nethttp.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeBadJSON(w, r, "request body is too large or unreadable")
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		// An unknown/misspelled field is NOT malformed JSON, and saying so sends the caller
		// hunting for a syntax error in a body that parses fine. Name the field instead --
		// posting {"verdict":...} when the API takes "decision" is the common case.
		if field, ok := unknownJSONField(err); ok {
			writeBadJSON(w, r, "unknown field "+field+" in request body")
			return false
		}
		writeBadJSON(w, r, "request body must be valid JSON")
		return false
	}
	return true
}

func writeBadJSON(w nethttp.ResponseWriter, r *nethttp.Request, message string) {
	httpresponse.WriteJSON(w, nethttp.StatusBadRequest, domain.ErrorEnvelope{
		Code:        "invalid_json",
		Message:     message,
		FieldErrors: []domain.FieldError{},
		TraceID:     traceID(r),
	})
}

func parsePositiveLimit(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, false
	}
	return limit, true
}

func requireIdempotencyKey(w nethttp.ResponseWriter, r *nethttp.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 200 {
		writeBadJSON(w, r, "Idempotency-Key header must be between 8 and 200 characters")
		return "", false
	}
	return key, true
}

func tenantID(r *nethttp.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *nethttp.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

func traceID(r *nethttp.Request) string {
	tid := httpmiddleware.TraceIDFromContext(r.Context())
	if tid == "" {
		return "missing-trace"
	}
	return tid
}

// unknownJSONField pulls the offending field name out of encoding/json's DisallowUnknownFields
// error, whose text is the only place that name exists.
func unknownJSONField(err error) (string, bool) {
	const marker = "unknown field "
	msg := err.Error()
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return "", false
	}
	return strings.TrimSpace(msg[idx+len(marker):]), true
}

// grantsAuthorize reports whether any active grant's role carries the permission. It is the
// permission-space counterpart to hasTenantWideRole: scoping decisions that mean "is this a
// verifier?" must key on the CAPABILITY, not on one named role, or every role added later
// silently takes the wrong branch.
func grantsAuthorize(grants []permissions.ActiveGrant, permission string) bool {
	for _, grant := range grants {
		if permissions.RoleHasPermission(grant.Role, permission) {
			return true
		}
	}
	return false
}
