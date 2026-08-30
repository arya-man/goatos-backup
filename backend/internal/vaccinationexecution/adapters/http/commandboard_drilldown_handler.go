package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	httpresponse "github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The command board's drilldown endpoints.
//
// Each of these lists used to ship inside GET /vaccination/command, computed tenant-wide for every
// cell on every render. That was ~62% of an endpoint which cost ~8.6s of SQL on the staging-scale
// tenant and returned 500 when the closed-without-dose statement exhausted the 15s pool timeout.
//
// Three properties are shared by all of them and each is load-bearing:
//
//   - THE SCOPE IS BACKEND-OWNED, exactly as it is on the board itself. park_id is clamped through
//     authorizedParkID, so a park-bound actor cannot reach another park's animals by naming them --
//     or by omitting the parameter. A drilldown is a different route, not a different trust
//     boundary, and splitting the endpoint must not split the authorization with it.
//
//   - THE CELL IS REQUIRED where the board has cells. A shed-vaccine or cohort drawer without its
//     cell is the tenant-wide scan this whole change exists to remove, so it is a 400 rather than a
//     slow 200.
//
//   - as_of IS CARRIED. The overdue/behind predicates are business-DATE comparisons against it, so
//     a drilldown resolved at a different as-of than the tile that raised it would list a different
//     animal set than the number the reader clicked.

// commandBoardDrilldownScope parses and authorizes the filter every drilldown shares with the board
// it hangs off: tenant, optional drive batch, capability-clamped park, as-of, and one page.
//
// Returns ok=false having already written the response when anything is refused.
func (h *Handler) commandBoardDrilldownScope(w http.ResponseWriter, r *http.Request) (vaccexecd.CommandBoardDrilldownQuery, bool) {
	q := vaccexecd.CommandBoardDrilldownQuery{TenantID: tenantID(r)}

	q.AsOf = h.now()
	if asOfStr := r.URL.Query().Get("as_of"); asOfStr != "" {
		if t, err := time.Parse(time.RFC3339, asOfStr); err == nil {
			q.AsOf = t.In(biztime.DefaultLocation())
		}
	}

	if driveBatchID := r.URL.Query().Get("drive_batch_id"); driveBatchID != "" {
		if !uuidutil.IsUUIDString(driveBatchID) {
			h.badRequest(w, r, "invalid_drive_batch_id", "drive_batch_id must be a valid UUID")
			return q, false
		}
		q.DriveBatchID = &driveBatchID
	}

	// Park scope is clamped to the actor's grants, never read verbatim off the query string.
	requestedPark := r.URL.Query().Get("park_id")
	if requestedPark != "" && !uuidutil.IsUUIDString(requestedPark) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a valid UUID")
		return q, false
	}
	parkID, ok := h.authorizedParkID(w, r, requestedPark)
	if !ok {
		return q, false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return q, false
		}
		q.Limit = limit
	}
	q.Cursor = r.URL.Query().Get("cursor")

	// Normalized clamps the page into the published bounds rather than refusing an oversized ask:
	// the caller gets the maximum page and the cursor to continue, which is the behaviour every
	// other keyset read on this service already has.
	return q.Normalized(), true
}

// commandBoardCohortCell parses the cohort cell keys. All four are required: the board collapses
// several dose codes onto one displayed vaccine label, so the drawer must name the whole set or it
// under-reports the column it was opened from.
func (h *Handler) commandBoardCohortCell(w http.ResponseWriter, r *http.Request, scope vaccexecd.CommandBoardDrilldownQuery) (vaccexecd.CommandBoardCohortCellQuery, bool) {
	cell := vaccexecd.CommandBoardCohortCellQuery{CommandBoardDrilldownQuery: scope}

	// cohort_park_id is the CELL's park and is deliberately distinct from park_id, which scopes the
	// board. The board may be tenant-wide while the cell belongs to one park.
	cohortParkID := r.URL.Query().Get("cohort_park_id")
	if cohortParkID != "" && !uuidutil.IsUUIDString(cohortParkID) {
		h.badRequest(w, r, "invalid_cohort_park_id", "cohort_park_id must be a valid UUID")
		return cell, false
	}
	// An empty cohort_park_id is a legitimate cell: the cohort query COALESCEs a shed with no park
	// to the empty string, so "" addresses the park-less cohort rather than meaning "unfiltered".
	cell.CohortParkID = cohortParkID

	cell.ManagementStage = strings.TrimSpace(r.URL.Query().Get("management_stage"))
	if cell.ManagementStage == "" {
		h.badRequest(w, r, "management_stage_required", "management_stage identifies the cohort cell and is required")
		return cell, false
	}
	cell.Sex = strings.TrimSpace(r.URL.Query().Get("sex"))
	if cell.Sex == "" {
		h.badRequest(w, r, "sex_required", "sex identifies the cohort cell and is required")
		return cell, false
	}

	for _, code := range strings.Split(r.URL.Query().Get("dose_codes"), ",") {
		if code = strings.TrimSpace(code); code != "" {
			cell.DoseCodes = append(cell.DoseCodes, code)
		}
	}
	if len(cell.DoseCodes) == 0 {
		h.badRequest(w, r, "dose_codes_required",
			"dose_codes identifies the cohort cell and is required; send the cell's doseCodes from the board response")
		return cell, false
	}
	return cell, true
}

// GetCommandBoardClosedWithoutDose lists the animals behind the ClosedWithoutDose KPI tile.
// GET /vaccination/command/closed-without-dose
func (h *Handler) GetCommandBoardClosedWithoutDose(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.commandBoardDrilldownScope(w, r)
	if !ok {
		return
	}
	page, err := h.reader.CommandBoardClosedWithoutDoseAnimals(r.Context(), scope)
	if err != nil {
		h.commandBoardDrilldownError(w, r, err)
		return
	}
	writeJSON(w, page)
}

// GetCommandBoardShedVaccineAnimals lists the animals behind ONE shed-vaccine cell, with that
// shed's proof videos for the days the page's animals were recorded.
// GET /vaccination/command/shed-vaccine-animals
func (h *Handler) GetCommandBoardShedVaccineAnimals(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.commandBoardDrilldownScope(w, r)
	if !ok {
		return
	}
	shedID := r.URL.Query().Get("shed_id")
	if !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id identifies the cell and must be a valid UUID")
		return
	}
	vaccineCode := strings.TrimSpace(r.URL.Query().Get("vaccine_code"))
	if vaccineCode == "" {
		h.badRequest(w, r, "vaccine_code_required", "vaccine_code identifies the cell and is required")
		return
	}
	page, err := h.reader.CommandBoardShedVaccineAnimals(r.Context(), vaccexecd.CommandBoardShedVaccineAnimalsQuery{
		CommandBoardDrilldownQuery: scope,
		ShedID:                     shedID,
		// An unpartitioned shed's cell carries an empty partition label, which is a real cell key
		// and not a missing parameter.
		PartitionLabel: r.URL.Query().Get("partition_label"),
		VaccineCode:    vaccineCode,
	})
	if err != nil {
		h.commandBoardDrilldownError(w, r, err)
		return
	}
	writeJSON(w, page)
}

// GetCommandBoardCohortExceptions lists ONE cohort cell's dose-sequence exceptions.
// GET /vaccination/command/cohort-exceptions
func (h *Handler) GetCommandBoardCohortExceptions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.commandBoardDrilldownScope(w, r)
	if !ok {
		return
	}
	cell, ok := h.commandBoardCohortCell(w, r, scope)
	if !ok {
		return
	}
	page, err := h.reader.CommandBoardCohortExceptions(r.Context(), cell)
	if err != nil {
		h.commandBoardDrilldownError(w, r, err)
		return
	}
	writeJSON(w, page)
}

// GetCommandBoardCohortDays returns ONE cohort cell's administered-day split.
// GET /vaccination/command/cohort-days
func (h *Handler) GetCommandBoardCohortDays(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.commandBoardDrilldownScope(w, r)
	if !ok {
		return
	}
	cell, ok := h.commandBoardCohortCell(w, r, scope)
	if !ok {
		return
	}
	page, err := h.reader.CommandBoardCohortDays(r.Context(), cell)
	if err != nil {
		h.commandBoardDrilldownError(w, r, err)
		return
	}
	writeJSON(w, page)
}

// GetCommandBoardDriveOptions serves the drive picker's full catalogue, keyset-paginated.
// GET /vaccination/command/drives
//
// The board carries only the first page. The catalogue was 448ms and 753 KB of a response budgeted
// at 512 KB -- for a dropdown -- and was the command board's critical path once the drilldowns had
// moved off it.
func (h *Handler) GetCommandBoardDriveOptions(w http.ResponseWriter, r *http.Request) {
	// The picker uses the CALLER's own park scope, never the selected drive's park: narrowing the
	// catalogue to the park of the drive already chosen deletes every other park's drive from the
	// dropdown and strands the reader there.
	requestedPark := r.URL.Query().Get("park_id")
	if requestedPark != "" && !uuidutil.IsUUIDString(requestedPark) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a valid UUID")
		return
	}
	parkID, ok := h.authorizedParkID(w, r, requestedPark)
	if !ok {
		return
	}
	q := vaccexecd.CommandBoardDriveOptionsQuery{
		TenantID: tenantID(r),
		Cursor:   r.URL.Query().Get("cursor"),
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		q.Limit = limit
	}
	page, err := h.reader.CommandBoardDriveOptions(r.Context(), q)
	if err != nil {
		h.commandBoardDrilldownError(w, r, err)
		return
	}
	writeJSON(w, page)
}

// commandBoardDrilldownError maps a repository failure onto a status.
//
// A malformed cursor is the caller's fault and must read as a 400, not a 500: the cursor is opaque
// to the client but it is still client input, and a 500 sends an operator to look for a server
// fault that is not there.
func (h *Handler) commandBoardDrilldownError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(err.Error(), "cursor") {
		h.badRequest(w, r, "invalid_cursor", "cursor is not a valid page position for this list")
		return
	}
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "read_error", Message: err.Error(), TraceID: traceID(r)}, err)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}
