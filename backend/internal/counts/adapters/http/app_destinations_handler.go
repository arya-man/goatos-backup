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
	Parks            []appShiftingDestinationPark `json:"parks"`
	ManagementStages []string                     `json:"management_stages"`
}

type appShiftingDestinationPark struct {
	ParkID string                       `json:"park_id"`
	Name   string                       `json:"name"`
	Sheds  []appShiftingDestinationShed `json:"sheds"`
}

type appShiftingDestinationShed struct {
	ShedID           string   `json:"shed_id"`
	Name             string   `json:"name"`
	ManagementStages []string `json:"management_stages"`
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
			sheds = append(sheds, appShiftingDestinationShed{ShedID: shed.ShedID, Name: shed.Name, ManagementStages: shed.ManagementStages})
		}
		parks = append(parks, appShiftingDestinationPark{
			ParkID: park.ParkID,
			Name:   park.Name,
			Sheds:  sheds,
		})
	}

	httpresponse.WriteJSON(w, http.StatusOK, appShiftingDestinationsResponse{Parks: parks, ManagementStages: catalog.ManagementStages})
}

// Breed vocabulary for the operator's birth form.
//
// GOLDEN FRONTEND RULE, same as the destinations cascade above: the breeds a birth form offers are
// backend-owned business data (the breeds present on the live herd), not frontend state. The phone
// renders what it is given -- it must not hold a hardcoded breed list and must not let an operator
// type a breed. Every option carries the canonical `key` the birth write stores.
//
// WHY A DEDICATED OPERATOR ROUTE. The same breed vocabulary is exposed by the Counts Breakdown
// `breeds` facet, but that screen is gated on CountsRead, which a field operator does not hold
// (operators have CountsWrite to record births/deaths, not the read-only census). Sourcing the
// picker from the breakdown facet therefore 403s for the very users who record births. This route
// serves the identical vocabulary on the CountsWrite surface so the picker works for operators.

type appBirthBreedsResponse struct {
	Breeds []appBirthBreedOption `json:"breeds"`
}

type appBirthBreedOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// ListBirthBreeds returns the breeds present on the tenant's live herd, most-common first, for the
// operator birth form's breed picker.
func (h *AppWriteHandler) ListBirthBreeds(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "missing tenant context", nil)
		return
	}

	points, err := h.shifting.ActiveBreeds(r.Context(), tenantID)
	if err != nil {
		h.writeCountsError(w, r, err)
		return
	}

	// Non-nil slice so an empty herd serializes as {"breeds":[]}, never JSON null.
	breeds := make([]appBirthBreedOption, 0, len(points))
	for _, point := range points {
		breeds = append(breeds, appBirthBreedOption{Key: point.Key, Label: point.Label, Count: point.Count})
	}

	httpresponse.WriteJSON(w, http.StatusOK, appBirthBreedsResponse{Breeds: breeds})
}
