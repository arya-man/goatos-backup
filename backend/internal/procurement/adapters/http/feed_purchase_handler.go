package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// FeedPurchaseService is the behaviour this transport depends on.
type FeedPurchaseService interface {
	ListFeedPurchases(ctx context.Context, tenantID string, q app.FeedPurchaseListQuery) (ports.FeedPurchasePage, error)
	FeedPurchaseOptions(ctx context.Context, tenantID string) (ports.FeedPurchaseOptions, error)
	CreateFeedPurchase(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error)
}

// FeedPurchaseHandler serves /procurement/feed-purchases.
type FeedPurchaseHandler struct {
	service FeedPurchaseService
	log     *slog.Logger
}

func NewFeedPurchaseHandler(service FeedPurchaseService, log ...*slog.Logger) *FeedPurchaseHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &FeedPurchaseHandler{service: service, log: l}
}

// RegisterFeedPurchases mounts the feed-purchase ledger and its entry route.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the permission
// table is matched by method + pattern, and a mismatch serves the route ungated.
func RegisterFeedPurchases(mux *http.ServeMux, h *FeedPurchaseHandler) {
	mux.HandleFunc("GET /procurement/feed-purchases", h.ListFeedPurchases)
	mux.HandleFunc("POST /procurement/feed-purchases", h.CreateFeedPurchase)
	mux.HandleFunc("GET /procurement/feed-purchase-options", h.FeedPurchaseOptions)
}

// maxFeedPurchaseRequestBytes caps a write body. The largest legitimate record-purchase payload is
// well under a kilobyte; the cap stops a hostile client streaming an unbounded body into memory.
const maxFeedPurchaseRequestBytes = 64 * 1024

// ListFeedPurchases serves GET /procurement/feed-purchases.
func (h *FeedPurchaseHandler) ListFeedPurchases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := h.intParam(w, r, q.Get("limit"), "invalid_limit", "That page size is not valid.")
	if !ok {
		return
	}
	offset, ok := h.intParam(w, r, q.Get("offset"), "invalid_offset", "That page is not valid.")
	if !ok {
		return
	}

	page, err := h.service.ListFeedPurchases(r.Context(), tenantID(r), app.FeedPurchaseListQuery{
		Farm:   q.Get("farm"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		h.writeErr(w, r, app.FeedPurchaseHTTPError(err))
		return
	}

	items := make([]feedPurchasePayload, 0, len(page.Purchases))
	for _, p := range page.Purchases {
		items = append(items, toFeedPurchasePayload(p))
	}
	httpresponse.WriteJSON(w, http.StatusOK, feedPurchasePagePayload{
		Purchases: items,
		// Total, QuantityKg and SpendRupees are WHOLE-FILTER aggregates, not page-local sums; the
		// screen derives its page count and its header figures from them.
		Total:       page.Total,
		QuantityKg:  page.QuantityKg,
		SpendRupees: page.SpendRupees,
		Limit:       domain.ClampFeedPurchasePageSize(limit),
		Offset:      offset,
	})
}

// FeedPurchaseOptions serves GET /procurement/feed-purchase-options.
func (h *FeedPurchaseHandler) FeedPurchaseOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := h.service.FeedPurchaseOptions(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.FeedPurchaseHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toFeedPurchaseOptionsPayload(opts))
}

// CreateFeedPurchase serves POST /procurement/feed-purchases.
func (h *FeedPurchaseHandler) CreateFeedPurchase(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This purchase could not be recorded safely. Try again."))
		return
	}
	var body feedPurchaseWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	created, err := h.service.CreateFeedPurchase(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.FeedPurchaseHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toFeedPurchasePayload(created))
}

// intParam parses an optional integer query parameter, rejecting a malformed one rather than
// silently reading it as zero.
func (h *FeedPurchaseHandler) intParam(w http.ResponseWriter, r *http.Request, raw, code, message string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		h.writeErr(w, r, app.BadRequest(code, message))
		return 0, false
	}
	return parsed, true
}

// decode reads and validates a JSON write body.
//
// DisallowUnknownFields is deliberate: a client sending "quantity_k" must be told, not silently
// ignored into a zero required field. Same fail-loud rule the fixture loaders use.
func (h *FeedPurchaseHandler) decode(w http.ResponseWriter, r *http.Request, dst *feedPurchaseWritePayload) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxFeedPurchaseRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That purchase form could not be read. Check the fields and try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That purchase form could not be read. Check the fields and try again."))
		return false
	}
	return true
}

func (h *FeedPurchaseHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}
