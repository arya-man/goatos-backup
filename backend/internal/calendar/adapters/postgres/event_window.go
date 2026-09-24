package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Single-event canonical reads (detail, existence) used to rebuild the whole canonical CTE over
// canonicalUnboundedWindow (now-2y .. now+2y): every drive, obligation and SOP task of four years, to
// find one event (stg: 1.2-1.4 s per call, and detail paid it twice). The event_id already says which
// business day the event lives on -- in the id itself (parkdrive/catchup) or one primary-key read away
// (assignment planned_date, batch planned/window dates, obligation due_at, completion date) -- so the
// point lookup can run the same CTE over a few business days instead.
//
// The narrowed window is [min(anchor, today) - 1 day, max(anchor, today) + 2 days) in Asia/Kolkata
// business days. It always contains TODAY, so calendarTodayInRequestedWindow stays true exactly as it
// is under the wide window and the P1 drive rollover (an open past drive re-dated onto today, 45-day
// lookback) behaves identically; it always contains the anchor business day, so the event's own
// sources and its (park, day) membership grain are inside it. Ids whose anchor cannot be derived
// (config/SOP "calendar:<uuid>", unknown shapes) keep the wide window.

// canonicalEventAnchorKind names the primary-key lookup that yields an event's business-day anchor.
type canonicalEventAnchorKind int

const (
	anchorNone canonicalEventAnchorKind = iota
	anchorDay
	anchorAssignment
	anchorBatch
	anchorObligation
	anchorCompletion
)

type canonicalEventAnchor struct {
	kind canonicalEventAnchorKind
	id   string // PK for the lookup kinds
	day  string // YYYY-MM-DD for anchorDay
}

// parseCanonicalEventAnchor maps an event_id onto the lookup that dates it. Pure; no I/O.
func parseCanonicalEventAnchor(eventID string) canonicalEventAnchor {
	eventID = strings.TrimSpace(eventID)
	if parsed, err := domain.ParseDriveEventID(eventID); err == nil {
		switch {
		case parsed.AssignmentID != "":
			return canonicalEventAnchor{kind: anchorAssignment, id: parsed.AssignmentID}
		case parsed.BatchID != "":
			return canonicalEventAnchor{kind: anchorBatch, id: parsed.BatchID}
		case parsed.DueDay != "":
			return canonicalEventAnchor{kind: anchorDay, day: parsed.DueDay}
		}
	}
	if id, ok := strings.CutPrefix(eventID, "obligation:"); ok && uuidutil.IsUUIDString(id) {
		return canonicalEventAnchor{kind: anchorObligation, id: id}
	}
	if id, ok := strings.CutPrefix(eventID, "completion:"); ok && uuidutil.IsUUIDString(id) {
		return canonicalEventAnchor{kind: anchorCompletion, id: id}
	}
	return canonicalEventAnchor{kind: anchorNone}
}

// canonicalNarrowWindow is the [from, to) window for an event anchored on business days
// [minDay, maxDay]. It always spans today (see the file comment).
func canonicalNarrowWindow(minDay, maxDay string, now time.Time) (time.Time, time.Time, error) {
	loc := biztime.DefaultLocation()
	lo, err := time.ParseInLocation("2006-01-02", minDay, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	hi, err := time.ParseInLocation("2006-01-02", maxDay, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	today := biztime.BusinessDayStart(now)
	if today.Before(lo) {
		lo = today
	}
	if today.After(hi) {
		hi = today
	}
	return lo.AddDate(0, 0, -1), hi.AddDate(0, 0, 2), nil
}

// Anchor lookups: one primary-key read each, returning the business-day span [min, max] as text.
const (
	calendarAssignmentAnchorSQL = `
SELECT planned_date::text, planned_date::text
FROM vaccination_drive_assignments
WHERE tenant_id = $1::uuid AND assignment_id = $2::uuid`

	// A batch card is dated by its assignment planned_date(s) when it has any, else by its own
	// planned_date or window (batch_events' COALESCE); take the whole span so every date a
	// batch:<id> card can carry is inside the window.
	calendarBatchAnchorSQL = `
SELECT
  LEAST(ob.planned_date, (ob.window_start AT TIME ZONE 'Asia/Kolkata')::date, (ob.window_end AT TIME ZONE 'Asia/Kolkata')::date, vda.min_day)::text,
  GREATEST(ob.planned_date, (ob.window_start AT TIME ZONE 'Asia/Kolkata')::date, (ob.window_end AT TIME ZONE 'Asia/Kolkata')::date, vda.max_day)::text
FROM obligation_batches ob
LEFT JOIN LATERAL (
  SELECT min(a.planned_date) AS min_day, max(a.planned_date) AS max_day
  FROM vaccination_drive_assignments a
  WHERE a.tenant_id = ob.tenant_id AND a.batch_id = ob.batch_id
) vda ON true
WHERE ob.tenant_id = $1::uuid AND ob.batch_id = $2::uuid`

	calendarObligationAnchorSQL = `
SELECT (due_at AT TIME ZONE 'Asia/Kolkata')::date::text, (due_at AT TIME ZONE 'Asia/Kolkata')::date::text
FROM obligation_instances
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`

	calendarCompletionAnchorSQL = `
SELECT (COALESCE(administered_at, created_at) AT TIME ZONE 'Asia/Kolkata')::date::text,
       (COALESCE(administered_at, created_at) AT TIME ZONE 'Asia/Kolkata')::date::text
FROM vaccination_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// canonicalEventWindow returns the [from, to) window a single-event canonical read should use for
// eventID. It returns ports.ErrNotFound when the event's own anchor row does not exist (the canonical
// CTE could not emit that id either), and the wide canonicalUnboundedWindow for ids it cannot date.
func canonicalEventWindow(ctx context.Context, q rowQuerier, tenantID, eventID string, now time.Time) (time.Time, time.Time, error) {
	anchor := parseCanonicalEventAnchor(eventID)
	var sql string
	switch anchor.kind {
	case anchorDay:
		return canonicalNarrowWindow(anchor.day, anchor.day, now)
	case anchorAssignment:
		sql = calendarAssignmentAnchorSQL
	case anchorBatch:
		sql = calendarBatchAnchorSQL
	case anchorObligation:
		sql = calendarObligationAnchorSQL
	case anchorCompletion:
		sql = calendarCompletionAnchorSQL
	default:
		from, to := canonicalUnboundedWindow(now)
		return from, to, nil
	}
	var minDay, maxDay pgtype.Text
	err := q.QueryRow(ctx, sql, tenantID, anchor.id).Scan(&minDay, &maxDay)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, time.Time{}, ports.ErrNotFound
	}
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("calendar: resolve event window: %w", err)
	}
	if !minDay.Valid || !maxDay.Valid {
		// A batch with no planned date, window or assignment: nothing to narrow on.
		from, to := canonicalUnboundedWindow(now)
		return from, to, nil
	}
	return canonicalNarrowWindow(minDay.String, maxDay.String, now)
}
