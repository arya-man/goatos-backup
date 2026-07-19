package http

import (
	"net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Destination catalog for the operator's shifting form.
//
// GOLDEN FRONTEND RULE. The option vocabulary a shifting form offers is backend-owned business
// data, not frontend state: parks, sheds, their ids, their display names and their ordering all
// come from Postgres via this route. The phone renders what it is given -- it must not hold a
// hardcoded shed list, must not sort the options itself, and must not construct a destination from
// a typed name.

type appShiftingDestinationsResponse struct {
	Parks []appShiftingDestinationPark `json:"parks"`
}

type appShiftingDestinationPark struct {
	ParkID string                       `json:"park_id"`
	Name   string                       `json:"name"`
	Sheds  []appShiftingDestinationShed `json:"sheds"`
}

type appShiftingDestinationShed struct {
	ShedID string `json:"shed_id"`
	Name   string `json:"name"`
}

// ListShiftingDestinations returns the active park -> shed cascade for the caller's tenant.
//
// The response is a CASCADE rather than a flat shed list on purpose. Shed names repeat across parks
// -- "Castro 1" exists under more than one park -- so a flat list would show the operator two
// identical-looking options. Nesting sheds under their park makes the choice unambiguous to a human,
// and every shed still carries its own shed_id so the subsequent write is addressed by id and is
// never ambiguous to the server regardless of what the UI displayed.
//
// The payload is bounded configuration (a couple of parks, ~154 sheds) and is meant to be fetched
// once and cached on-device. It is deliberately not paginated: it is a catalog, not a feed.
func (h *AppWriteHandler) ListShiftingDestinations(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}

	catalog, err := h.shifting.ShiftingDestinations(r.Context(), tenantID)
	if err != nil {
		h.writeCountsError(w, r, err)
		return
	}

	// Built with non-nil slices throughout so an empty tenant serializes as {"parks":[]} and a
	// shed-less park as "sheds":[] -- never JSON null, which a client would have to special-case.
	parks := make([]appShiftingDestinationPark, 0, len(catalog.Parks))
	for _, park := range catalog.Parks {
		sheds := make([]appShiftingDestinationShed, 0, len(park.Sheds))
		for _, shed := range park.Sheds {
			sheds = append(sheds, appShiftingDestinationShed{ShedID: shed.ShedID, Name: shed.Name})
		}
		parks = append(parks, appShiftingDestinationPark{
			ParkID: park.ParkID,
			Name:   park.Name,
			Sheds:  sheds,
		})
	}

	httpresponse.WriteJSON(w, http.StatusOK, appShiftingDestinationsResponse{Parks: parks})
}
