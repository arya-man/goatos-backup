package http

import (
	"errors"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// ExportCSV handles GET /herd-signals/export.csv -- the live view the operator is looking at,
// as a file, honouring every filter the list honours.
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	actor := domain.Actor{TenantID: tenantID(r), UserID: actorID(r)}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	query := r.URL.Query()
	parkID := optionalParam(query.Get("park_id"))
	shedID := optionalParam(query.Get("shed_id"))
	movementState := optionalParam(query.Get("movement_state"))
	mappingState := optionalParam(query.Get("mapping_state"))
	pattern := optionalParam(query.Get("pattern"))
	q := optionalParam(query.Get("q"))

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="herd-signals-export.csv"`)

	// Counts bytes already sent, so a mid-stream failure is not "corrected" by appending a JSON
	// error body to a half-written CSV -- that produces a file whose last line is JSON and which
	// opens as a corrupt sheet that LOOKS like data. A truncated download the client's parser
	// rejects is honest; a silently corrupted one is not. Same discipline as the weighing export.
	counting := &countingResponseWriter{ResponseWriter: w}
	if err := h.service.ExportCSV(ctx, actor, parkID, shedID, movementState, mappingState, pattern, q, counting); err != nil {
		if counting.written > 0 {
			h.log.Error("herd_signals_export_failed_mid_stream", "bytes_written", counting.written, "error", err.Error())
			return
		}
		h.log.Error("herd_signals_export_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "export_failed", "message": "failed to export herd signals"},
			err)
		return
	}
}

// GetTagActivity handles GET /herd-signals/tags/{tag_id}/activity.
func (h *Handler) GetTagActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	actor := domain.Actor{TenantID: tenantID(r), UserID: actorID(r)}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"},
			nil)
		return
	}

	tagID := r.PathValue("tag_id")
	if tagID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "missing_tag_id", "message": "tag_id is required"},
			nil)
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "missing_from_to", "message": "from and to timestamps required"},
			nil)
		return
	}

	resp, err := h.service.GetTagActivity(ctx, actor, tagID, from, to)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			h.log.Warn("get_tag_activity_invalid_request", "tag_id", tagID, "error", err.Error())
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				map[string]interface{}{"code": "invalid_request", "message": err.Error()},
				err)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
				map[string]interface{}{"code": "tag_not_found", "message": "no such tag for this tenant"},
				nil)
			return
		}
		h.log.Error("get_tag_activity_failed", "tag_id", tagID, "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "activity_failed", "message": "failed to read farm activity"},
			err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// optionalParam turns an absent query parameter into a nil pointer, so "not filtered" and
// "filtered to the empty string" stay different things.
func optionalParam(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// countingResponseWriter records whether any body bytes reached the client, so the export
// handler can tell "failed before anything was sent" (safe to write a proper error status) from
// "failed halfway through a download" (must not append anything).
type countingResponseWriter struct {
	http.ResponseWriter
	written int
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.written += n
	return n, err
}
