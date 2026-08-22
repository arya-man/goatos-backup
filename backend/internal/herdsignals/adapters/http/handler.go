package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Ingest request caps (security review, HIGH): the handler previously decoded an unbounded
// array straight into one transaction holding FOR UPDATE row locks, with no limit on body size
// or packet count. A single gateway batch is at most a few hundred tags; these are generous but
// finite.
const (
	maxIngestBodyBytes         = 2 << 20 // 2 MiB
	maxIngestPacketsPerRequest = 2000
)

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func actorID(r *http.Request) string {
	return httpmiddleware.ActorIDFromContext(r.Context())
}

// AppService defines the interface the handler expects from the app service.
type AppService interface {
	IngestPackets(ctx context.Context, actor domain.Actor, req domain.IngestRequest) (domain.IngestResponse, error)
	ListLive(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, mappingState, pattern, q *string, cursor string, limit int) (domain.LiveResponse, error)
	GetTimeline(ctx context.Context, actor domain.Actor, tagID, from, to string, bucketSeconds int) (domain.TimelineResponse, error)
	ListGateways(ctx context.Context, actor domain.Actor) (domain.GatewaysResponse, error)
	GetInsights(ctx context.Context, actor domain.Actor) (domain.InsightsResponse, error)
	ExportCSV(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, mappingState, pattern, q *string, w io.Writer) error
	GetTagActivity(ctx context.Context, actor domain.Actor, tagID, from, to string) (domain.ActivityResponse, error)
}

// Handler handles HTTP requests for herd signals.
type Handler struct {
	service AppService
	log     *slog.Logger
}

// NewHandler creates a new herd signals HTTP handler.
func NewHandler(service AppService, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

// Register registers herd signals routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /herd-signals/packets", h.IngestPackets)
	mux.HandleFunc("GET /herd-signals/live", h.ListLive)
	mux.HandleFunc("GET /herd-signals/tags/{tag_id}/timeline", h.GetTimeline)
	mux.HandleFunc("GET /herd-signals/gateways", h.ListGateways)
	mux.HandleFunc("GET /herd-signals/insights", h.GetInsights)
	mux.HandleFunc("GET /herd-signals/export.csv", h.ExportCSV)
	mux.HandleFunc("GET /herd-signals/tags/{tag_id}/activity", h.GetTagActivity)
}

// IngestPackets handles POST /herd-signals/packets.
func (h *Handler) IngestPackets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract actor from context (set by middleware)
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Bound the request body BEFORE decoding (security review, HIGH): the handler previously
	// decoded an unbounded array straight into one transaction holding FOR UPDATE row locks, so
	// an oversized or absurdly long-array payload could hold locks and memory indefinitely.
	// MaxBytesReader caps total bytes read; the packet-count cap below catches a payload that
	// stays under the byte cap by using short/repeated field values but still carries an
	// unreasonable number of packets.
	r.Body = http.MaxBytesReader(w, r.Body, maxIngestBodyBytes)

	// Strict JSON decode
	var req domain.IngestRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				map[string]interface{}{"code": "request_too_large", "message": fmt.Sprintf("request body exceeds %d bytes", maxIngestBodyBytes)},
				err)
			return
		}
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "invalid_request", "message": "invalid request body"},
			err)
		return
	}

	if len(req.Packets) > maxIngestPacketsPerRequest {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "too_many_packets", "message": fmt.Sprintf("request carries %d packets, max %d per request", len(req.Packets), maxIngestPacketsPerRequest)},
			nil)
		return
	}

	// Call service
	resp, err := h.service.IngestPackets(ctx, actor, req)
	if err != nil {
		h.log.Error("ingest_packets_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "ingest_failed", "message": "failed to ingest packets"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ListLive handles GET /herd-signals/live.
func (h *Handler) ListLive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract actor from context
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Parse query parameters
	parkID := r.URL.Query().Get("park_id")
	shedID := r.URL.Query().Get("shed_id")
	movementState := r.URL.Query().Get("movement_state")
	mappingState := r.URL.Query().Get("mapping_state")
	pattern := r.URL.Query().Get("pattern")
	q := r.URL.Query().Get("q")

	cursor := r.URL.Query().Get("cursor")
	limit := 25 // default per contract; max 200
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	// Convert empty strings to nil pointers
	var parkIDPtr, shedIDPtr, movementStatePtr, mappingStatePtr, patternPtr, qPtr *string
	if parkID != "" {
		parkIDPtr = &parkID
	}
	if shedID != "" {
		shedIDPtr = &shedID
	}
	if movementState != "" {
		movementStatePtr = &movementState
	}
	if mappingState != "" {
		mappingStatePtr = &mappingState
	}
	if pattern != "" {
		patternPtr = &pattern
	}
	if q != "" {
		qPtr = &q
	}

	// Call service
	resp, err := h.service.ListLive(ctx, actor, parkIDPtr, shedIDPtr, movementStatePtr, mappingStatePtr, patternPtr, qPtr, cursor, limit)
	if err != nil {
		h.log.Error("list_live_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "list_failed", "message": "failed to list live tags"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// GetTimeline handles GET /herd-signals/tags/{tag_id}/timeline.
func (h *Handler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract actor from context
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Extract tag_id from path
	tagID := r.PathValue("tag_id")
	if tagID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "missing_tag_id", "message": "tag_id is required"},
			nil)
		return
	}

	// Parse query parameters
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "missing_from_to", "message": "from and to timestamps required"},
			nil)
		return
	}

	bucketSeconds := 0 // 0 = let the service select the tier from the requested range
	if bucketStr := r.URL.Query().Get("bucket_seconds"); bucketStr != "" {
		if b, err := strconv.Atoi(bucketStr); err == nil && b > 0 {
			bucketSeconds = b
		}
	}

	// Call service
	resp, err := h.service.GetTimeline(ctx, actor, tagID, from, to, bucketSeconds)
	if err != nil {
		// Defect 7 fix: a caller-input validation failure (bad range, unsupported
		// bucket_seconds, too many buckets) previously mapped to 500 like a real backend
		// failure. domain.ErrValidation distinguishes the two.
		if errors.Is(err, domain.ErrValidation) {
			h.log.Warn("get_timeline_invalid_request", "tag_id", tagID, "error", err.Error())
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				map[string]interface{}{"code": "invalid_request", "message": err.Error()},
				err)
			return
		}
		h.log.Error("get_timeline_failed", "tag_id", tagID, "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "timeline_failed", "message": "failed to get timeline"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ListGateways handles GET /herd-signals/gateways.
func (h *Handler) ListGateways(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract actor from context
	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	// Call service
	resp, err := h.service.ListGateways(ctx, actor)
	if err != nil {
		h.log.Error("list_gateways_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "gateways_failed", "message": "failed to list gateways"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// GetInsights handles GET /herd-signals/insights.
func (h *Handler) GetInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	actor := domain.Actor{
		TenantID: tenantID(r),
		UserID:   actorID(r),
	}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	resp, err := h.service.GetInsights(ctx, actor)
	if err != nil {
		h.log.Error("get_insights_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "insights_failed", "message": "failed to compute insights"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}
