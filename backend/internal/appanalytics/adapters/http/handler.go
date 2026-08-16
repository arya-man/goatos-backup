package appanalyticshttp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Handler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewHandler(pool *pgxpool.Pool, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{pool: pool, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /app/analytics/events", h.RecordEvent)
}

type recordEventRequest struct {
	EventName         string            `json:"event_name"`
	Properties        map[string]string `json:"properties"`
	ClientEventTimeMS int64             `json:"client_event_time_ms"`
	Flavor            string            `json:"flavor"`
	AppVersionName    string            `json:"app_version_name"`
	AppVersionCode    int               `json:"app_version_code"`
	ClientEventID     string            `json:"client_event_id"`
}

type recordEventResponse struct {
	Accepted bool `json:"accepted"`
}

func (h *Handler) RecordEvent(w http.ResponseWriter, r *http.Request) {
	var body recordEventRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid analytics event", err)
		return
	}
	body.EventName = strings.TrimSpace(body.EventName)
	if body.EventName == "" {
		h.writeError(w, r, http.StatusBadRequest, "missing_event_name", "event_name is required", nil)
		return
	}
	if body.Properties == nil {
		body.Properties = map[string]string{}
	}
	body.ClientEventID = strings.TrimSpace(body.ClientEventID)

	props, err := json.Marshal(body.Properties)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_properties", "invalid analytics properties", err)
		return
	}
	client, err := json.Marshal(httpmiddleware.ClientInfoMetadataFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_client_info", "invalid client context", err)
		return
	}

	var tenantID pgtype.UUID
	_ = tenantID.Scan(strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context())))
	var actorID pgtype.UUID
	_ = actorID.Scan(strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context())))
	var clientEventAt any
	if body.ClientEventTimeMS > 0 {
		clientEventAt = time.UnixMilli(body.ClientEventTimeMS).UTC()
	}

	_, err = h.pool.Exec(r.Context(), `
INSERT INTO analytics.app_events (
  tenant_id, actor_id, device_id, event_name, properties, client_event_time,
  flavor, app_version_name, app_version_code, request_id, trace_id, client_info, client_event_id
) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12::jsonb,$13)
ON CONFLICT (tenant_id, client_event_id) WHERE client_event_id IS NOT NULL
DO NOTHING`,
		tenantID,
		actorID,
		strings.TrimSpace(httpmiddleware.DeviceIDFromContext(r.Context())),
		body.EventName,
		string(props),
		clientEventAt,
		strings.TrimSpace(body.Flavor),
		strings.TrimSpace(body.AppVersionName),
		body.AppVersionCode,
		httpmiddleware.RequestIDFromContext(r.Context()),
		httpmiddleware.TraceIDFromContext(r.Context()),
		string(client),
		// NULL, never "": the partial unique index treats "" as a real value, so a blank id
		// would dedupe EVERY id-less event for a tenant against the first one (silent loss).
		nullableClientEventID(body.ClientEventID),
	)
	if err != nil {
		h.log.ErrorContext(r.Context(), "app_analytics_event_insert_failed", slog.String("event_name", body.EventName), slog.String("error", err.Error()))
		h.writeError(w, r, http.StatusInternalServerError, "analytics_insert_failed", "analytics event was not recorded", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, recordEventResponse{Accepted: true})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, map[string]any{
		"code":         code,
		"message":      message,
		"field_errors": []any{},
	}, cause)
}

// nullableClientEventID maps an absent/blank client_event_id to SQL NULL so the partial
// unique index (WHERE client_event_id IS NOT NULL) never treats "" as a dedupe key.
func nullableClientEventID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
