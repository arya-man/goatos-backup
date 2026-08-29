package http

import (
	"net/http"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
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

	// BirthPlacement is the newborn-placement contract for THIS park (maintainer decision
	// 2026-08-20): which pens a kid born here may go into, and the farm-worded notice the birth
	// form shows above them.
	//
	// It rides on the destinations response rather than a route of its own because the birth form
	// already fetches and caches this catalog, and the pens it names are rows of this very catalog
	// -- a second endpoint would be a second copy of the same bounded configuration, cached
	// separately and able to disagree with the picker beside it.
	//
	// BACKEND OWNS THE MODE AND THE COPY. The phone must not re-derive "how many kid pens does this
	// park have" by filtering sheds on destination_stage: the mode also governs whether the WRITE
	// will accept a freely chosen pen, and a client that computed its own answer could offer a pen
	// the birth would then refuse.
	BirthPlacement appBirthPlacement `json:"birth_placement"`
}

// appBirthPlacement is the wire form of domain.BirthPlacementResolution.
type appBirthPlacement struct {
	// Mode is "automatic" (exactly one kid pen -- shown read-only, not chosen), "choose" (several
	// kid pens -- the picker offers only these), or "record_later" (no kid pen configured -- the
	// operator picks freely and the kid's care steps carry Record shed).
	Mode string `json:"mode"`
	// Notice is farm-worded copy rendered VERBATIM above the placement field.
	Notice string `json:"notice"`
	// Pens is this park's kid pens, empty in record_later mode and exactly one entry in automatic
	// mode. Each carries the full operational location so the form never re-derives a display
	// string from shed_id + partition_label.
	Pens []appBirthPlacementPen `json:"pens"`
}

type appBirthPlacementPen struct {
	ShedID                     string  `json:"shed_id"`
	ShedName                   string  `json:"shed_name"`
	PartitionLabel             *string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string  `json:"operational_location_display"`
}

type appShiftingDestinationShed struct {
	ShedID           string   `json:"shed_id"`
	Name             string   `json:"name"`
	ManagementStages []string `json:"management_stages"`

	// PartitionLabel is omitted for a non-partitioned destination entry (the bare shed) and set to
	// the raw stored label for a real-partition entry. A partitioned shed appears multiple times in
	// its park's sheds list, once per real partition -- never as a synthesized "whole shed" option.
	PartitionLabel *string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is the operator-facing operational-location label ("Yashoda",
	// "Castro - 2", "Godel 1 - Part 3"), so the client renders exactly what
	// oploc.OperationalLocation.Display produces and never re-derives it from ShedID +
	// PartitionLabel itself.
	//
	// The wire name is `operational_location_display`, NOT `display`. It shipped as `display`
	// until 2026-08-06 while OpenAPI's ShiftingDestinationShed and Android's
	// CountsDestinationShedDto both declared the canonical name, so the client deserialized it to
	// "" on every row -- silently, because an absent key takes its default. Nobody noticed only
	// because the screen re-derived the label locally, which is the defect this field exists to
	// prevent.
	OperationalLocationDisplay string `json:"operational_location_display"`

	// DestinationStage is the tag a movement INTO this pen would stamp, and
	// DestinationStageReason is the farm-worded explanation when it would stamp none. Exactly one
	// of the two is ever non-empty (domain.DestinationStageResolution guarantees it).
	//
	// These exist for the raise form's TAG TOGGLE (maintainer decision 2026-08-15): the operator
	// chooses "keep current tag" or "use destination tag", so the form has to show WHICH tag the pen
	// would give and grey the option out, with a reason, when the pen cannot give one.
	//
	// BACKEND OWNS BOTH STRINGS. The phone renders them verbatim -- it must not re-derive the tag
	// from management_stages (that is the residents' raw list, not the resolved answer, and the
	// resolution rules -- authored-pen-tag-first, mixed, empty, clinical -- live in
	// counts/domain), and it must not compose its own reason from a blank tag, because a blank tag
	// does not say WHY it is blank.
	DestinationStage       string `json:"destination_stage"`
	DestinationStageReason string `json:"destination_stage_reason"`
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
			// Resolved with the SAME function the raise handler uses, against the SAME catalog, so
			// the tag the form shows on the toggle is byte-for-byte the tag the raise will stamp.
			// Two implementations of "what tag does this pen give" would drift, and the operator
			// would approve one answer while the movement recorded another.
			stage := domain.ResolveShiftingDestinationPenStageDetailed(
				shed.ConfiguredStage, shed.ManagementStages, catalog.ManagementStages,
			)
			sheds = append(sheds, appShiftingDestinationShed{
				ShedID:                     shed.ShedID,
				Name:                       shed.Name,
				ManagementStages:           shed.ManagementStages,
				PartitionLabel:             shed.PartitionLabel,
				OperationalLocationDisplay: shed.Display,
				DestinationStage:           stage.Stage,
				DestinationStageReason:     stage.Reason,
			})
		}
		// Resolved with the SAME function the birth write validates against, from the SAME catalog
		// rows, so the pens this form offers are byte-for-byte the pens the write accepts.
		placement := domain.ResolveBirthPlacement(park.Sheds)
		pens := make([]appBirthPlacementPen, 0, len(placement.Pens))
		for _, pen := range placement.Pens {
			pens = append(pens, appBirthPlacementPen{
				ShedID:                     pen.ShedID,
				ShedName:                   pen.ShedName,
				PartitionLabel:             pen.PartitionLabel,
				OperationalLocationDisplay: pen.Display,
			})
		}
		parks = append(parks, appShiftingDestinationPark{
			ParkID: park.ParkID,
			Name:   park.Name,
			Sheds:  sheds,
			BirthPlacement: appBirthPlacement{
				Mode:   placement.Mode,
				Notice: placement.Notice,
				Pens:   pens,
			},
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
