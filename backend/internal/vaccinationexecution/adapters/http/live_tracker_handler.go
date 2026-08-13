package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// GetVaccinationLiveTracker serves the live drive-day tracker. GET /vaccination/live-tracker
//
// business_date is a BUSINESS DATE, not a projection as_of: the response is always reconstructed
// from canonical rows for that drive day, so a past day is a legitimate read and must NOT emit
// historical_as_of_unsupported the way the current-view execution reads do. It is bounded to the
// last week so the page cannot be pointed at an unbounded historical scan.
func (h *Handler) GetVaccinationLiveTracker(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	loc := biztime.DefaultLocation()
	now := h.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	businessDate := today
	if raw := strings.TrimSpace(query.Get("business_date")); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			h.badRequest(w, r, "invalid_business_date", "business_date must be a YYYY-MM-DD date")
			return
		}
		earliest := today.AddDate(0, 0, -vaccexecd.LiveTrackerBusinessDateLookbackDays)
		if parsed.Before(earliest) || parsed.After(today) {
			h.badRequest(w, r, "invalid_business_date", "business_date must fall within the supported drive-day window")
			return
		}
		businessDate = parsed
	}

	q := vaccexecd.LiveTrackerQuery{
		TenantID:      tenantID(r),
		BusinessDate:  businessDate,
		ActivityLimit: vaccexecd.LiveTrackerDefaultActivity,
	}

	requestedPark := strings.TrimSpace(query.Get("park_id"))
	if requestedPark != "" && !uuidutil.IsUUIDString(requestedPark) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
		return
	}
	// Park scope is BACKEND-owned: a park-bound actor must not be able to read the other park's
	// drive by omitting park_id, and naming a park they do not hold must be refused rather than
	// silently widened.
	parkID, ok := h.authorizedParkID(w, r, requestedPark)
	if !ok {
		return
	}
	if parkID != "" {
		q.ParkID = &parkID
	}

	if raw := strings.TrimSpace(query.Get("shed_id")); raw != "" {
		if !uuidutil.IsUUIDString(raw) {
			h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
			return
		}
		q.ShedID = &raw
	}
	if raw := strings.TrimSpace(query.Get("partition_label")); raw != "" {
		if len(raw) > 64 {
			h.badRequest(w, r, "invalid_partition_label", "partition_label must be 64 characters or fewer")
			return
		}
		q.PartitionLabel = &raw
	}
	if raw := strings.TrimSpace(query.Get("operator_id")); raw != "" {
		if !uuidutil.IsUUIDString(raw) {
			h.badRequest(w, r, "invalid_operator_id", "operator_id must be a UUID")
			return
		}
		q.OperatorID = &raw
	}
	if raw := strings.TrimSpace(query.Get("vaccine_code")); raw != "" {
		if len(raw) > 64 {
			h.badRequest(w, r, "invalid_vaccine_code", "vaccine_code must be 64 characters or fewer")
			return
		}
		q.VaccineCode = &raw
	}
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		if !vaccexecd.IsLiveTrackerStatus(raw) {
			h.badRequest(w, r, "invalid_status", "status must be one of active, done, pending, review")
			return
		}
		status := vaccexecd.LiveTrackerStatus(raw)
		q.Status = &status
	}
	if raw := strings.TrimSpace(query.Get("activity_limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		// Both bounds are REFUSED, not one refused and one silently clamped. The contract declares
		// minimum 1 / maximum 100; answering 200 with a clamped 100 and HTTP 200 tells the caller the
		// bound they asked for was honoured, which is how a paging client silently loses events.
		if err != nil || n <= 0 || n > vaccexecd.LiveTrackerMaxActivity {
			h.badRequest(w, r, "invalid_activity_limit",
				fmt.Sprintf("activity_limit must be an integer between 1 and %d", vaccexecd.LiveTrackerMaxActivity))
			return
		}
		q.ActivityLimit = n
	}
	if raw := strings.TrimSpace(query.Get("activity_before")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			h.badRequest(w, r, "invalid_activity_before", "activity_before must be an RFC3339 timestamp")
			return
		}
		cursor := parsed.In(loc)
		q.ActivityBefore = &cursor
	}
	// The feed's key is (occurred_at, event_id). Accepting the timestamp without its tiebreaker is
	// what silently dropped every event sharing the previous page's boundary timestamp.
	if raw := strings.TrimSpace(query.Get("activity_before_id")); raw != "" {
		if len(raw) > 128 {
			h.badRequest(w, r, "invalid_activity_before_id", "activity_before_id must be an event_id from next_cursor_event_id")
			return
		}
		q.ActivityBeforeID = &raw
	}

	resp, err := h.reader.LiveTracker(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}
