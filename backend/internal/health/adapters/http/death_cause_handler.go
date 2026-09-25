package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The searchable disease list the death form's "due to disease" dropdown reads.
//
// ONE READ, NO PAGING, and that is deliberate: the whole vocabulary is a few dozen diseases,
// the operator is searching it with their thumb while standing over a dead animal, and a
// paged dropdown that has to round-trip per keystroke is the wrong shape for that moment.
// It is PER TENANT since 2026-09-25: the built-in register plus the diseases the farm has
// authored in Health Config, so the client caches it offline and refreshes it on open.
//
// There is deliberately NO second route for raising a death from the Health screen. That
// screen sends the SAME `POST /app/counts/death-events` every death goes through, with the
// cause taken from the case it is standing on; a second death producer would mean a second
// approval path, and "nobody self-authorizes a death" is a recorded lock.

const healthDeathCausesRoute = "/app/health/death-causes"

// DeathCauseCatalogReader is the app-layer read this handler serves.
type DeathCauseCatalogReader interface {
	Catalog(ctx context.Context, tenantID string) (domain.DeathCauseCatalog, error)
}

type DeathCauseHandler struct {
	svc DeathCauseCatalogReader
	log *slog.Logger
}

func NewDeathCauseHandler(svc DeathCauseCatalogReader, log *slog.Logger) *DeathCauseHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DeathCauseHandler{svc: svc, log: log}
}

func RegisterDeathCauses(mux *http.ServeMux, h *DeathCauseHandler) {
	mux.HandleFunc("GET "+healthDeathCausesRoute, h.ListDeathCauses)
}

// ListDeathCauses serves every disease an operator may record a death as.
func (h *DeathCauseHandler) ListDeathCauses(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}
	catalog, err := h.svc.Catalog(r.Context(), tenantID)
	if err != nil {
		// A register that will not load is a deployment fault, and the honest answer is an
		// error rather than an empty list: an empty dropdown reads to the operator as "this
		// farm has no diseases", and they would record a normal death for an animal that
		// died of something the product could have named.
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "health death causes", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, catalog)
}
