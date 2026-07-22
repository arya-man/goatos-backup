package http

import (
	"net/http"
	"strconv"
	"strings"

	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// appTemporaryTaggedPageSize caps this read server-side. It is a phone work list -- one screen of
// goats awaiting a permanent RFID -- so a client asking for 500 rows must still get one page.
const appTemporaryTaggedPageSize = 20

type appTemporaryTaggedGoatItem struct {
	GoatID              string `json:"goat_id"`
	DisplayID           string `json:"display_id"`
	TemporaryIdentifier string `json:"temporary_identifier"`
	LocationDisplay     string `json:"location_display"`
	RowVersion          int32  `json:"row_version"`
}

type appTemporaryTaggedGoatsResponse struct {
	Items      []appTemporaryTaggedGoatItem `json:"items"`
	NextCursor *string                      `json:"next_cursor"`
}

// ListTemporaryTaggedGoats returns one keyset page of goats that still carry an active temporary tag
// so the operator can promote each to a permanent RFID. Delegates to identity's read; the page is
// ordered by display_id and next_cursor is the last row's display_id.
func (h *AppWriteHandler) ListTemporaryTaggedGoats(w http.ResponseWriter, r *http.Request) {
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

	pageSize := appTemporaryTaggedPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_page_size",
				"page_size must be a positive integer", nil)
			return
		}
		if parsed < pageSize {
			pageSize = parsed
		}
	}

	var cursor *string
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		cursor = &raw
	}

	result, err := h.validator.ListTemporaryTaggedGoats(r.Context(), identityapp.ListTemporaryTaggedGoatsInput{
		TenantID: tenantID,
		Limit:    pageSize,
		Cursor:   cursor,
		// Optional location filter (park -> shed cascade). Identity validates the UUIDs and rejects a
		// malformed value with a 400.
		ParkID:  strings.TrimSpace(r.URL.Query().Get("park_id")),
		ShedID:  strings.TrimSpace(r.URL.Query().Get("shed_id")),
		TraceID: appTraceID(r),
	})
	if err != nil {
		h.writeAppError(w, r, err)
		return
	}

	items := make([]appTemporaryTaggedGoatItem, 0, len(result.Items))
	for _, row := range result.Items {
		items = append(items, appTemporaryTaggedGoatItem{
			GoatID:              row.GoatID,
			DisplayID:           row.DisplayID,
			TemporaryIdentifier: row.TemporaryIdentifier,
			LocationDisplay:     row.LocationDisplay,
			RowVersion:          row.RowVersion,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, appTemporaryTaggedGoatsResponse{Items: items, NextCursor: result.NextCursor})
}
