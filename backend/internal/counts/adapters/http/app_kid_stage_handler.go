package http

import (
	"fmt"
	"net/http"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The kid stage ladder's operator card (maintainer decision 2026-08-20,
// docs/decisions/kid-stage-age-ladder.md).
//
// GET /app/counts/shifting/stage-due lists the kids whose age has crossed a ladder step
// (K0->K1 at 2 days, K1->K2 at 7), grouped per (park, step) with the candidate destination pens.
// A group with exactly ONE candidate pen is normally auto-raised by the sweeper before anyone
// sees it; a group with ZERO or SEVERAL candidates is exactly what this card exists for — the
// operator adds the group to the raise form's basket and selects the destination themselves.
//
// Raising from the card and the sweeper running are safe concurrently: whichever writes first
// puts the kids into an in-flight movement, and the due read excludes in-flight kids, so the
// other side simply finds nothing left to raise.
//
// GOLDEN FRONTEND RULE: the card's title is backend-composed farm copy; the phone renders it
// verbatim and never assembles its own sentence from counts and stage codes.

type appKidStageDueResponse struct {
	BusinessDate string                `json:"business_date"`
	Groups       []appKidStageDueGroup `json:"groups"`
}

type appKidStageDueGroup struct {
	ParkID    string `json:"park_id"`
	ParkName  string `json:"park_name"`
	FromStage string `json:"from_stage"`
	ToStage   string `json:"to_stage"`
	// Title is the operator-facing card copy, e.g. "2 K0 kids are due to move to K1".
	Title string              `json:"title"`
	Goats []appKidStageDueGoat `json:"goats"`
	// Candidates are the park's pens whose destination tag is the step's target. Zero or several
	// means the operator chooses; exactly one means the sweeper will normally raise it itself.
	Candidates []appShiftingDestinationShed `json:"candidates"`
	// AutoRaisePending is true when exactly one candidate pen exists: the group is the sweeper's
	// to raise, and the card is informational rather than actionable.
	AutoRaisePending bool `json:"auto_raise_pending"`
}

type appKidStageDueGoat struct {
	GoatID          string  `json:"goat_id"`
	DisplayID       string  `json:"display_id"`
	Tag             string  `json:"tag"`
	ParkID          string  `json:"park_id"`
	ShedID          string  `json:"shed_id"`
	ParkName        string  `json:"park_name"`
	ShedName        string  `json:"shed_name"`
	PartitionLabel  *string `json:"partition_label,omitempty"`
	ManagementStage string  `json:"management_stage"`
	BornOn          string  `json:"born_on"`
}

// WithKidStageLadder supplies the ladder raiser whose DueGroups read backs the stage-due card.
func (h *AppWriteHandler) WithKidStageLadder(raiser *countsapp.KidStageLadderRaiser) *AppWriteHandler {
	h.kidStageLadder = raiser
	return h
}

// ListKidStageDue serves the operator card. CountsWrite like the destinations catalog: the card
// exists to start a raise, so its audience is exactly the raise form's.
func (h *AppWriteHandler) ListKidStageDue(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		h.writeError(w, r, http.StatusUnauthorized, "missing_tenant", "tenant scope required", nil)
		return
	}
	if h.kidStageLadder == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "not_configured", "stage-due is not available", nil)
		return
	}
	groups, err := h.kidStageLadder.DueGroups(r.Context(), tenantID, time.Now())
	if err != nil {
		h.writeCountsError(w, r, err)
		return
	}
	out := appKidStageDueResponse{
		BusinessDate: biztime.BusinessDate(time.Now()),
		Groups:       make([]appKidStageDueGroup, 0, len(groups)),
	}
	for _, group := range groups {
		goats := make([]appKidStageDueGoat, 0, len(group.Goats))
		for _, goat := range group.Goats {
			goats = append(goats, appKidStageDueGoat{
				GoatID:          goat.GoatID,
				DisplayID:       goat.DisplayID,
				Tag:             goat.Tag,
				ParkID:          goat.ParkID,
				ShedID:          goat.ShedID,
				ParkName:        goat.ParkName,
				ShedName:        goat.ShedName,
				PartitionLabel:  goat.PartitionLabel,
				ManagementStage: goat.ManagementStage,
				BornOn:          goat.BornOn.Format("2006-01-02"),
			})
		}
		candidates := make([]appShiftingDestinationShed, 0, len(group.Candidates))
		for _, pen := range group.Candidates {
			candidates = append(candidates, appShiftingDestinationShed{
				ShedID:                     pen.ShedID,
				Name:                       pen.Name,
				ManagementStages:           pen.ManagementStages,
				PartitionLabel:             pen.PartitionLabel,
				OperationalLocationDisplay: pen.Display,
			})
		}
		kidWord, verb := "kids", "are"
		if len(group.Goats) == 1 {
			kidWord, verb = "kid", "is"
		}
		out.Groups = append(out.Groups, appKidStageDueGroup{
			ParkID:    group.ParkID,
			ParkName:  group.ParkName,
			FromStage: group.FromStage,
			ToStage:   group.ToStage,
			Title: fmt.Sprintf("%d %s %s in %s %s due to move to %s",
				len(group.Goats), group.FromStage, kidWord, group.ParkName, verb, group.ToStage),
			Goats:            goats,
			Candidates:       candidates,
			AutoRaisePending: group.AutoRaisable(),
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}
