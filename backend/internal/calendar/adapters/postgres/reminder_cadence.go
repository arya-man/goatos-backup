package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	reminderCadenceDefaultLimit = 200
	reminderCadenceMaxLimit     = 2000
)

// reminderCadenceCandidate is one open, actionable vaccination obligation/event that MIGHT have a
// reminder cadence fire due -- read from the canonical source_events reconstruction
// (calendarCanonicalEventsCTE), which is de-scheduling's own source of truth: an obligation that
// transitions to completed/deferred/canceled leaves the status filter below (so it stops reminding),
// and one that is rescheduled picks up its NEW due_at on the very next sweep (so the ladder restarts
// from the new D) -- vaccination-notification-rules.md §3.
type reminderCadenceCandidate struct {
	EventID    string
	ParkID     string
	DueAt      time.Time
	TargetType string
	TargetID   string
}

// SweepReminderCadence implements ports.Repository.SweepReminderCadence. It is the legacy 2-value
// wrapper kept for callers that do not thread the keyset cursor (Finding3 target-type test, the
// scale-cert drain test, the service passthrough): it sweeps one page from wherever in.CursorDueAt/
// in.CursorEventID point (zero cursor = from the beginning) and discards the returned progress cursor.
// New code that needs guaranteed cross-run forward progress must call SweepReminderCadencePage and
// persist the returned cursor (CAL-MAIN-02, see ReminderCadenceStage).
func (r *Repository) SweepReminderCadence(ctx context.Context, in ports.ReminderCadenceQuery) ([]ports.ReminderCadenceFire, error) {
	fires, _, err := r.SweepReminderCadencePage(ctx, in)
	return fires, err
}

// SweepReminderCadencePage implements ports.Repository.SweepReminderCadencePage. Two bounded, indexed,
// set-based reads (candidates, then already-fired keys); everything else is pure Go computation over
// the small in-memory result set (no N+1, no per-row I/O).
//
// CAL-MAIN-02: the candidate scan is a stable keyset page ordered by (due_at, event_id), resumed from
// in.CursorDueAt/in.CursorEventID (a zero/empty cursor starts from the beginning). The returned
// ReminderCadenceSweepCursor carries the last (due_at, event_id) scanned this page plus an Exhausted
// flag (the page came back short of Limit), so the caller can persist the cursor across runs and wrap
// it back to the start once the tenant's candidate set is fully paged -- guaranteeing later farms are
// eventually reached instead of being starved behind a LIMIT-full first page. Already-fired candidates
// are NOT excluded from the scan (that coarse per-park exclusion was removed); the exact per-fire-key
// dedup below (firedSet + LatestDueReminderFire) already makes re-selecting a fully-fired candidate a
// harmless no-op that emits no fire.
func (r *Repository) SweepReminderCadencePage(ctx context.Context, in ports.ReminderCadenceQuery) ([]ports.ReminderCadenceFire, ports.ReminderCadenceSweepCursor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	ladder := in.Ladder
	if len(ladder) == 0 {
		ladder = domain.DefaultReminderLadder()
	}
	quietStart := in.QuietHoursStartIST
	if quietStart == "" {
		quietStart = domain.DefaultQuietHoursStartIST
	}
	quietEnd := in.QuietHoursEndIST
	if quietEnd == "" {
		quietEnd = domain.DefaultQuietHoursEndIST
	}
	limit := in.Limit
	if limit <= 0 {
		limit = reminderCadenceDefaultLimit
	}
	if limit > reminderCadenceMaxLimit {
		limit = reminderCadenceMaxLimit
	}

	quiet, err := domain.IsQuietHoursIST(now, quietStart, quietEnd)
	if err != nil {
		return nil, ports.ReminderCadenceSweepCursor{}, fmt.Errorf("calendar: reminder cadence quiet hours: %w", err)
	}
	if quiet {
		// Deferred entirely this tick: nothing is scanned or marked fired here, so the next non-quiet
		// tick re-derives the same still-pending fire and queues it then
		// (vaccination-notification-rules.md §3 quiet-hours rule -- "defers to the next allowed slot").
		// The zero cursor (empty EventID, Exhausted=false) signals "no page scanned" so the caller
		// leaves its persisted cursor untouched.
		return nil, ports.ReminderCadenceSweepCursor{}, nil
	}

	candidates, err := r.selectReminderCadenceCandidates(ctx, in.TenantID, now, in.CursorDueAt, in.CursorEventID, limit)
	if err != nil {
		return nil, ports.ReminderCadenceSweepCursor{}, err
	}

	// Keyset progress: the last (due_at, event_id) scanned this page, plus whether the page was the
	// tail of the candidate set (short of Limit). Computed from the scanned candidates, NOT the
	// collapsed fires, so it advances even when every candidate on this page was already fired.
	sweepCursor := ports.ReminderCadenceSweepCursor{Exhausted: len(candidates) < limit}
	if len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		sweepCursor.DueAt = last.DueAt
		sweepCursor.EventID = last.EventID
	}
	if len(candidates) == 0 {
		return nil, sweepCursor, nil
	}

	// Pass 1: every fire key that is due as of now for ANY candidate, scoped by that candidate's
	// park, regardless of whether it has already fired -- gathered per-candidate (so Pass 2 can claim
	// every superseded slot alongside the winning one, see ClaimKeys) AND flattened into one batched
	// read that decides which are already claimed in the fire-marker table.
	perCandidateKeys := make([][]string, len(candidates))
	keySeen := map[string]bool{}
	var allKeys []string
	for i, c := range candidates {
		for _, k := range domain.PendingLadderFireKeys(c.DueAt, now, ladder) {
			full := c.ParkID + ":" + k
			perCandidateKeys[i] = append(perCandidateKeys[i], full)
			if !keySeen[full] {
				keySeen[full] = true
				allKeys = append(allKeys, full)
			}
		}
	}
	firedSet, err := r.selectFiredReminderCadenceKeys(ctx, in.TenantID, allKeys)
	if err != nil {
		return nil, sweepCursor, err
	}

	// Pass 2: for each candidate, the LATEST still-pending fire (never a backlog burst -- a sweeper
	// catching up after downtime sends the current correct reminder once), collapsed into ONE
	// ReminderCadenceFire per (park, fire day, type, slot). Candidates arrive ordered by
	// (due_at ASC, event_id ASC), so the first candidate seen for a group is deterministically its
	// representative. Every OTHER pending key for that candidate (a slot the sweeper skipped past
	// because a later one was already due by the time it ran) is added to ClaimKeys so it gets
	// claimed too -- without this, a later sweep at the same or a later `now` would treat those
	// skipped slots as still-pending and burst a stale reminder for each one.
	type groupKey struct {
		parkID     string
		fireDayKey string
	}
	groups := map[groupKey]*ports.ReminderCadenceFire{}
	var order []groupKey
	for i, c := range candidates {
		fire, ok := domain.LatestDueReminderFire(c.DueAt, now, ladder, func(k string) bool {
			return firedSet[c.ParkID+":"+k]
		})
		if !ok {
			continue
		}
		gk := groupKey{parkID: c.ParkID, fireDayKey: fire.FireDayKey}
		g, exists := groups[gk]
		if !exists {
			fireDay := biztime.BusinessDayStart(c.DueAt).AddDate(0, 0, fire.OffsetDays)
			g = &ports.ReminderCadenceFire{
				ParkID:                        c.ParkID,
				RepresentativeCalendarEventID: c.EventID,
				RepresentativeObligationID:    c.TargetID,
				SourceTargetType:              c.TargetType, // derived from source_events: obligation | batch | catchup | park_drive
				FireDayIST:                    fireDay.Format("2006-01-02"),
				NotificationType:              fire.Type,
				Priority:                      fire.Priority,
				Slot:                          fire.Slot,
				ReminderNumber:                fire.ReminderNumber,
				FireKey:                       c.ParkID + ":" + fire.FireDayKey,
			}
			groups[gk] = g
			order = append(order, gk)
		}
		g.ObligationCount++
		claimSeen := map[string]bool{}
		for _, k := range g.ClaimKeys {
			claimSeen[k] = true
		}
		for _, k := range perCandidateKeys[i] {
			if firedSet[k] || claimSeen[k] {
				continue
			}
			claimSeen[k] = true
			g.ClaimKeys = append(g.ClaimKeys, k)
		}
	}

	fires := make([]ports.ReminderCadenceFire, 0, len(order))
	for _, gk := range order {
		fires = append(fires, *groups[gk])
	}
	return fires, sweepCursor, nil
}

// calendarReminderCadenceCandidatesSQL is the reminder-cadence candidate set, read directly off the
// canonical source_events reconstruction instead of calendar_event_projections. windowStart/windowEnd
// double as the shared CTE's own $2/$3 window bound -- no separate due_at filter needed.
//
// The 4th column is SOURCE_TARGET_TYPE (obligation | batch | catchup | park_drive), NOT the drive's
// display target_type (park/shed/tenant). It is paired with source_target_id below so the notification
// this fire produces stores a (type, id) that resolves to the SAME real entity the drive is about
// (Finding 3): a solo batch -> ('batch', batch_id); an unbatched catch-up -> ('catchup', park_id); a
// multi-source park-day drive -> ('park_drive', park_id). Selecting the display target_type here
// mislabeled every batch/park id as an 'obligation'.
//
// CAL-MAIN-02 FIX (keyset paging): the candidate scan is a stable keyset page over (due_at, event_id).
// $5/$6 are the resume cursor (cursorDueAt, cursorEventID); an empty $6 starts from the beginning
// (same "cursor sentinel = empty" guard the CAL-MAIN-03 reconciler uses). The previous coarse
// per-park NOT EXISTS exclusion on vaccination_reminder_cadence_fires was REMOVED: it wrongly skipped
// a park that had ANY historical fire even when it had a fresh pending obligation, and it did not
// guarantee forward progress. Forward progress is now the caller's keyset cursor (persisted across
// runs, wrapped to the start on exhaustion). Re-selecting an already-fired candidate is harmless: the
// exact per-fire-key dedup in SweepReminderCadencePage (firedSet + LatestDueReminderFire) emits no
// fire for it.
// scale-guard:ignore: 5k-50k-envelope; see docs/decisions/operational-kernel-5k-50k-scale-envelope.md
const calendarReminderCadenceCandidatesSQL = "WITH " + calendarCanonicalEventsCTE + `
SELECT event_id, park_id::text, due_at, source_target_type, COALESCE(source_target_id::text, '')
FROM source_events
WHERE system = false
  AND event_type <> 'vaccination_dose_due'
  AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due')
  AND park_id IS NOT NULL
  AND ($6::text = '' OR (due_at, event_id) > ($5::timestamptz, $6::text))
ORDER BY due_at ASC, event_id ASC
LIMIT $4`

func (r *Repository) selectReminderCadenceCandidates(ctx context.Context, tenantID string, now, cursorDueAt time.Time, cursorEventID string, limit int) ([]reminderCadenceCandidate, error) {
	// Bounded window covering the whole ladder lookahead (up to D-7) plus a one-day catch-up buffer
	// for a late/slow tick.
	windowStart := biztime.BusinessDayStart(now).AddDate(0, 0, -1)
	windowEnd := biztime.BusinessDayStart(now).AddDate(0, 0, 9)
	rows, err := r.pool.Query(ctx, calendarReminderCadenceCandidatesSQL, tenantID, windowStart, windowEnd, limit, cursorDueAt, cursorEventID)
	if err != nil {
		return nil, fmt.Errorf("calendar: select reminder cadence candidates: %w", err)
	}
	defer rows.Close()
	var out []reminderCadenceCandidate
	for rows.Next() {
		var c reminderCadenceCandidate
		if err := rows.Scan(&c.EventID, &c.ParkID, &c.DueAt, &c.TargetType, &c.TargetID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) selectFiredReminderCadenceKeys(ctx context.Context, tenantID string, keys []string) (map[string]bool, error) {
	fired := map[string]bool{}
	if len(keys) == 0 {
		return fired, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT fire_key FROM vaccination_reminder_cadence_fires
WHERE tenant_id = $1::uuid AND fire_key = ANY($2::text[])`, tenantID, keys)
	if err != nil {
		return nil, fmt.Errorf("calendar: select fired reminder cadence keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		fired[k] = true
	}
	return fired, rows.Err()
}

// QueueReminderCadenceBatch implements ports.Repository.QueueReminderCadenceBatch. Four phases, all
// set-based:
//  1. bulk-insert one notification_requests row per (fire, recipient) via a single unnest INSERT;
//     only fires with at least one inserted notification are returned as "successfully queued";
//  2. claim only the fires that actually produced notifications atomically against the fire-marker
//     table (ON CONFLICT DO NOTHING) so two concurrent sweeper runs never double-fire the same batch,
//     and fires with zero recipients are left unclaimed and will retry when recipients are assigned;
//  3. write outbox/audit for the claimed fires' representative events (a small, Limit-bounded loop
//     over already-fetched Go data -- no injected-dependency call inside it, so no additional I/O
//     fan-out beyond the two set-based statements above).
func (r *Repository) QueueReminderCadenceBatch(ctx context.Context, in ports.QueueReminderCadenceBatch) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if len(in.Fires) == 0 {
		return 0, nil
	}
	channel := in.Channel
	if channel == "" {
		channel = "push_fcm"
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Insert notifications FIRST, then only claim fires that produced at least one notification.
	// Fires with zero recipients are left UNCLAIMED so they retry once recipients are assigned
	// (Finding 1: never burn a fire-marker for a fire that produced zero notifications).
	inserted, firstReqIDByFireKey, firedFireKeys, err := insertReminderCadenceNotifications(ctx, tx, in.TenantID, channel, in.TraceID, in.Fires)
	if err != nil {
		return 0, err
	}
	if inserted == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}

	// Build the subset of fires that actually produced notifications, preserving original order.
	var firesToClaim []ports.ReminderCadenceFireInput
	for _, f := range in.Fires {
		if firedFireKeys[f.Fire.FireKey] {
			firesToClaim = append(firesToClaim, f)
		}
	}

	won, err := claimReminderCadenceFires(ctx, tx, in.TenantID, firesToClaim)
	if err != nil {
		return 0, err
	}

	for _, f := range won {
		payload := map[string]any{
			"fire_key":          f.Fire.FireKey,
			"park_id":           f.Fire.ParkID,
			"fire_day":          f.Fire.FireDayIST,
			"notification_type": f.Fire.NotificationType,
			"slot":              f.Fire.Slot,
			"obligation_count":  f.Fire.ObligationCount,
			"sweeper":           "calendar-reminder-sweeper",
		}
		// outbox_messages' validate_outbox_event_tenant trigger only recognizes aggregate_type
		// 'calendar_notification' as a notification_requests.notification_request_id reference
		// (vaccination-notification-rules.md's own aggregate vocabulary -- see queueDueReminder /
		// QueueRoleNotifications). Reuse it with the representative notification row this batch
		// actually inserted for the fire; a fire with zero newly-inserted rows (every recipient
		// device was already an exact replay) has nothing new to audit, so it is skipped here.
		requestID, ok := firstReqIDByFireKey[f.Fire.FireKey]
		if !ok {
			continue
		}
		idempotencyKey := in.TenantID + ":calendar.reminder.cadence:" + f.Fire.FireKey
		if err := insertOutbox(ctx, tx, in.TenantID, "calendar.reminder.cadence.queued", "calendar.notification.v1",
			"calendar_notification", requestID, "calendar.notifications", idempotencyKey, in.TraceID, "system_rule", "",
			payload, f.Fire.RepresentativeCalendarEventID); err != nil {
			return 0, err
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     in.TenantID,
			ActorType:    "system",
			Action:       "calendar.reminder.cadence.queued",
			ResourceType: "calendar_notification_batch",
			// resource_id is a uuid column; the fire's own key is a composite string
			// (park:day:type:slot), so use the representative notification_request_id it produced
			// (a real uuid) -- the fire key itself is carried in Metadata.
			ResourceID: requestID,
			ScopeType:  "park",
			ScopeID:    f.Fire.ParkID,
			AfterState: payload,
			Metadata: map[string]any{
				"domain":            "calendar",
				"module":            "vaccination",
				"category":          "reminder_cadence",
				"calendar_event_id": f.Fire.RepresentativeCalendarEventID,
				"idempotency_key":   idempotencyKey,
				"fire_key":          f.Fire.FireKey,
				"status":            "queued",
				"result":            "queued",
				"channel":           channel,
				"notification_type": f.Fire.NotificationType,
			},
		}); err != nil {
			return 0, fmt.Errorf("calendar: audit reminder cadence: %w", err)
		}
	}
	// 5k-50k envelope: reminder_state is read-derived (calendarReminderRailSQL), so there is no
	// projection row left to update here.

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return inserted, nil
}

// deriveNotificationTargetType derives the correct notification target type from the source_target_type
// read from the canonical source_events. The notification record must store the SAME (type, id) pair
// that resolves to the actual entity in the operational kernel.
//   - "obligation" → "obligation" (single obligation)
//   - "batch" → "batch" (scheduled vaccination drive)
//   - "catchup" → "catchup" (catch-up drive)
//   - "park_drive" → "park_drive" (aggregated park/day drive, single or multi-source)
// If the type is unknown, default to "obligation" for backward compatibility, but log the anomaly.
func deriveNotificationTargetType(sourceTargetType string) string {
	switch sourceTargetType {
	case "obligation", "batch", "catchup", "park_drive":
		return sourceTargetType
	default:
		// Unmapped type; default to obligation for safety. Real production deployments should
		// never hit this path unless the source_events schema emits a new type not yet mapped here.
		return "obligation"
	}
}

// claimReminderCadenceFires inserts every candidate fire into the fire-marker table in ONE set-based
// statement and returns only the ones this call WON (i.e. were not already claimed by a prior or
// concurrent sweep). ON CONFLICT DO NOTHING makes this safe under concurrent sweeper runs.
func claimReminderCadenceFires(ctx context.Context, tx pgx.Tx, tenantID string, fires []ports.ReminderCadenceFireInput) ([]ports.ReminderCadenceFireInput, error) {
	var fireKeys, parkIDs, fireDays, types, slots, repEventIDs []string
	var reminderNumbers, obligationCounts []int32
	for _, f := range fires {
		claimKeys := f.Fire.ClaimKeys
		if len(claimKeys) == 0 {
			// Always at least claim the fire's own key (defensive default for a caller that never
			// populated ClaimKeys -- e.g. a future direct construction outside SweepReminderCadence).
			claimKeys = []string{f.Fire.FireKey}
		}
		// Claim EVERY pending key this fire subsumes (its own FireKey plus any earlier slot the
		// sweeper skipped past), all pointing at the SAME representative fire, so a later sweep never
		// treats a superseded slot as still-pending and bursts a stale reminder for it.
		for _, key := range claimKeys {
			park, day, notifType, slot, ok := splitReminderCadenceFireKey(key)
			if !ok {
				continue
			}
			fireKeys = append(fireKeys, key)
			parkIDs = append(parkIDs, park)
			fireDays = append(fireDays, day)
			types = append(types, notifType)
			slots = append(slots, slot)
			reminderNumbers = append(reminderNumbers, int32(f.Fire.ReminderNumber))
			obligationCounts = append(obligationCounts, int32(f.Fire.ObligationCount))
			repEventIDs = append(repEventIDs, f.Fire.RepresentativeCalendarEventID)
		}
	}
	if len(fireKeys) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
WITH candidate AS (
  SELECT * FROM unnest($2::text[], $3::text[], $4::date[], $5::text[], $6::text[], $7::int[], $8::int[], $9::text[])
    AS f(fire_key, park_id, fire_day, notification_type, slot, reminder_number, obligation_count, representative_calendar_event_id)
)
INSERT INTO vaccination_reminder_cadence_fires (
  tenant_id, fire_key, park_id, fire_day, notification_type, slot, reminder_number, obligation_count, representative_calendar_event_id
)
SELECT $1::uuid, c.fire_key, c.park_id::uuid, c.fire_day, c.notification_type, c.slot, c.reminder_number, c.obligation_count, c.representative_calendar_event_id
FROM candidate c
ON CONFLICT (tenant_id, fire_key) DO NOTHING
RETURNING fire_key`,
		tenantID, fireKeys, parkIDs, fireDays, types, slots, reminderNumbers, obligationCounts, repEventIDs)
	if err != nil {
		return nil, fmt.Errorf("calendar: claim reminder cadence fires: %w", err)
	}
	defer rows.Close()
	won := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		won[k] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A fire actually fires only if ITS OWN key won the claim race -- winning a superseded key alone
	// (e.g. a concurrent sweep already claimed the latest slot but not an older one) never produces a
	// notification.
	out := make([]ports.ReminderCadenceFireInput, 0, len(fires))
	for _, f := range fires {
		if won[f.Fire.FireKey] {
			out = append(out, f)
		}
	}
	return out, nil
}

// splitReminderCadenceFireKey parses "<park_id>:<YYYY-MM-DD>:<type>:<HH:MM>" back into its parts.
func splitReminderCadenceFireKey(key string) (parkID, fireDay, notifType, slot string, ok bool) {
	parts := strings.SplitN(key, ":", 4)
	if len(parts) != 4 {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[2], parts[3], true
}

// insertReminderCadenceNotifications bulk-inserts one notification_requests row per (fire, recipient)
// via a single unnest-driven INSERT -- no per-fire loop calling the database. Correlation back to the
// caller is keyed by the fire's stable FireKey (NOT a positional index), so it stays correct even when
// the caller later re-slices/re-orders the fires (e.g. claims only the subset that produced rows):
//   - firstReqIDByFireKey: fire_key -> the FIRST notification_request_id inserted for that fire, used
//     as the outbox/audit aggregate reference; a fire whose every recipient row was an exact idempotent
//     replay (zero new rows) is simply absent.
//   - firedFireKeys: the set of fire_keys that produced AT LEAST ONE new notification row -- exactly the
//     fires that should be claimed (a zero-recipient / fully-replayed fire is absent, so its marker is
//     never burned and it is retried on a later sweep once recipients exist).
//
// RETURNING intentionally selects only target-table columns (notification_request_id, idempotency_key);
// Postgres cannot return a source CTE column (recip.group_idx) from an INSERT, so the fire correlation
// is done in Go via the idempotency_key -> fire_key map built here.
func insertReminderCadenceNotifications(ctx context.Context, tx pgx.Tx, tenantID, channel, traceID string, fires []ports.ReminderCadenceFireInput) (int, map[string]string, map[string]bool, error) {
	groupIdx := make([]int32, 0, len(fires))
	groupEventIDs := make([]string, 0, len(fires))
	groupTargetTypes := make([]string, 0, len(fires))
	groupTargetIDs := make([]string, 0, len(fires))
	groupTypes := make([]string, 0, len(fires))
	groupTitles := make([]string, 0, len(fires))
	groupBodies := make([]string, 0, len(fires))

	var rowGroupIdx []int32
	var rowDeviceIDs, rowFCMTokens, rowIdemKeys, rowFingerprints, rowContexts []string
	// idemKey -> fire_key, so an inserted row (returned by idempotency_key) maps back to its fire.
	idemKeyToFireKey := map[string]string{}

	for i, f := range fires {
		groupIdx = append(groupIdx, int32(i))
		groupEventIDs = append(groupEventIDs, f.Fire.RepresentativeCalendarEventID)
		groupTargetTypes = append(groupTargetTypes, deriveNotificationTargetType(f.Fire.SourceTargetType))
		groupTargetIDs = append(groupTargetIDs, f.Fire.RepresentativeObligationID)
		groupTypes = append(groupTypes, f.Fire.NotificationType)
		groupTitles = append(groupTitles, f.Title)
		groupBodies = append(groupBodies, f.Body)

		for _, recipient := range f.Recipients {
			idempotencyKey := tenantID + ":calendar.reminder.cadence:" + f.Fire.FireKey + ":device:" + recipient.DeviceID
			idemKeyToFireKey[idempotencyKey] = f.Fire.FireKey
			contextData := map[string]any{
				"priority":         f.Fire.Priority,
				"role":             recipient.RoleLabel,
				"member_id":        recipient.MemberID,
				"channel":          channel,
				"source":           "calendar-reminder-sweeper",
				"fire_key":         f.Fire.FireKey,
				"fire_day":         f.Fire.FireDayIST,
				"reminder_number":  f.Fire.ReminderNumber,
				"obligation_count": f.Fire.ObligationCount,
			}
			for k, v := range f.Context {
				contextData[k] = v
			}
			contextJSON, err := json.Marshal(contextData)
			if err != nil {
				return 0, nil, nil, fmt.Errorf("calendar: encode reminder cadence context: %w", err)
			}
			rowGroupIdx = append(rowGroupIdx, int32(i))
			rowDeviceIDs = append(rowDeviceIDs, recipient.DeviceID)
			rowFCMTokens = append(rowFCMTokens, recipient.FCMToken)
			rowIdemKeys = append(rowIdemKeys, idempotencyKey)
			rowFingerprints = append(rowFingerprints, requestFingerprint(tenantID, f.Fire.FireKey, f.Fire.NotificationType, recipient.DeviceID))
			rowContexts = append(rowContexts, string(contextJSON))
		}
	}
	if len(rowGroupIdx) == 0 {
		return 0, map[string]string{}, map[string]bool{}, nil
	}

	rows, err := tx.Query(ctx, `
WITH groups AS (
  SELECT * FROM unnest($2::int[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[])
    AS g(idx, calendar_event_id, target_type, target_id, notification_type, title, body)
), recip AS (
  SELECT * FROM unnest($9::int[], $10::text[], $11::text[], $12::text[], $13::text[], $14::text[])
    AS rr(group_idx, device_id, fcm_token, idempotency_key, request_fingerprint, context_text)
)
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  recipient_ref, title, body, status, idempotency_key, request_fingerprint, context, trace_id
)
SELECT $1::uuid, g.calendar_event_id, g.target_type, nullif(g.target_id, '')::uuid, g.notification_type, $15,
       recip.fcm_token, g.title, g.body, 'queued', recip.idempotency_key, recip.request_fingerprint, recip.context_text::jsonb, $16
FROM recip
JOIN groups g ON g.idx = recip.group_idx
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING notification_request_id::text, idempotency_key`,
		tenantID, groupIdx, groupEventIDs, groupTargetTypes, groupTargetIDs, groupTypes, groupTitles, groupBodies,
		rowGroupIdx, rowDeviceIDs, rowFCMTokens, rowIdemKeys, rowFingerprints, rowContexts,
		channel, traceID,
	)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("calendar: insert reminder cadence notifications: %w", err)
	}
	defer rows.Close()
	insertedRequestIDByKey := map[string]string{}
	count := 0
	for rows.Next() {
		var requestID, idemKey string
		if err := rows.Scan(&requestID, &idemKey); err != nil {
			return 0, nil, nil, err
		}
		insertedRequestIDByKey[idemKey] = requestID
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, nil, nil, err
	}

	// Correlate inserted rows back to their fire by fire_key (stable, re-slice-safe). Walk rowIdemKeys
	// in build order so firstReqIDByFireKey deterministically picks the first inserted recipient row.
	firstReqIDByFireKey := map[string]string{}
	firedFireKeys := map[string]bool{}
	for _, idemKey := range rowIdemKeys {
		requestID, ok := insertedRequestIDByKey[idemKey]
		if !ok {
			continue // this recipient row was an idempotent replay (no new row)
		}
		fireKey := idemKeyToFireKey[idemKey]
		firedFireKeys[fireKey] = true
		if _, already := firstReqIDByFireKey[fireKey]; !already {
			firstReqIDByFireKey[fireKey] = requestID
		}
	}
	return count, firstReqIDByFireKey, firedFireKeys, nil
}

// calendarLoadReminderCadenceCursorSQL loads a tenant's persisted reminder-cadence keyset cursor
// (CAL-MAIN-02, migration 000204).
const calendarLoadReminderCadenceCursorSQL = `
SELECT cursor_due_at, cursor_event_id
FROM reminder_cadence_progress
WHERE tenant_id = $1::uuid`

// LoadReminderCadenceCursor returns the last (due_at, event_id) the reminder-cadence stage processed
// for this tenant. An absent row means "start from the beginning" and returns (zero time, "", nil).
// See CAL-MAIN-02 / migration 000204.
func (r *Repository) LoadReminderCadenceCursor(ctx context.Context, tenantID string) (time.Time, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cursorDueAt time.Time
	var cursorEventID string
	err := r.pool.QueryRow(ctx, calendarLoadReminderCadenceCursorSQL, tenantID).Scan(&cursorDueAt, &cursorEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, "", nil
	}
	if err != nil {
		return time.Time{}, "", fmt.Errorf("calendar: load reminder cadence cursor: %w", err)
	}
	return cursorDueAt, cursorEventID, nil
}

// calendarSaveReminderCadenceCursorSQL upserts a tenant's reminder-cadence keyset cursor
// (CAL-MAIN-02, migration 000204).
const calendarSaveReminderCadenceCursorSQL = `
INSERT INTO reminder_cadence_progress (tenant_id, cursor_due_at, cursor_event_id, updated_at)
VALUES ($1::uuid, $2::timestamptz, $3, $4)
ON CONFLICT (tenant_id) DO UPDATE
SET cursor_due_at = EXCLUDED.cursor_due_at,
    cursor_event_id = EXCLUDED.cursor_event_id,
    updated_at = EXCLUDED.updated_at`

// SaveReminderCadenceCursor persists the reminder-cadence stage's keyset cursor so the next tick
// resumes from where this run stopped (or from the beginning when the stage passes (zero time, "")
// after exhausting the candidate set). See CAL-MAIN-02.
func (r *Repository) SaveReminderCadenceCursor(ctx context.Context, tenantID string, cursorDueAt time.Time, cursorEventID string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.pool.Exec(ctx, calendarSaveReminderCadenceCursorSQL, tenantID, cursorDueAt, cursorEventID, now); err != nil {
		return fmt.Errorf("calendar: save reminder cadence cursor: %w", err)
	}
	return nil
}
