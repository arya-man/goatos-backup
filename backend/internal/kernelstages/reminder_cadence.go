package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// cadenceAudienceResolver is the slice of the workforce roster service the ladder needs to resolve
// who a fire is addressed to. Declared as an interface at the point of use so the audience rule can
// be unit-tested against a roster fake that mirrors the real seeded seat vocabulary, without a
// database.
type cadenceAudienceResolver interface {
	// ResolveModuleDutyRecipientsBatch resolves the park-scoped operational audience by module duty.
	ResolveModuleDutyRecipientsBatch(ctx context.Context, tenantID, scopeType string, scopeIDs []string, moduleCode string, dutyTypes []string, at time.Time) (map[string][]workforcedomain.NotificationRecipient, error)
	// ResolvePositionRecipientsBatch resolves the tenant leadership audience, which is role-grant
	// truth (ceo_internal / pc_director), not a module duty.
	ResolvePositionRecipientsBatch(ctx context.Context, tenantID, scopeType string, scopeIDs, positionCodes []string, at time.Time) (map[string][]workforcedomain.NotificationRecipient, error)
}

// ReminderCadenceStage materializes the vaccination reminder cadence ladder
// (T-7 days, day-start/afternoon/EOD, due-today) by calling SweepReminderCadence to find
// fires at each ladder slot, resolving recipients from the workforce roster (every seat holding the
// vaccination execute/manage duty in the park, plus tenant leadership on the EOD rung), and queueing
// notification_requests rows via QueueReminderCadenceBatch. This replaces the deleted
// cmd/calendar-reminder-sweeper. Operational (15m) cadence.
type ReminderCadenceStage struct {
	deps     Deps
	calendar *calendarapp.Service
	roster   cadenceAudienceResolver
	tenantID string
	logger   *slog.Logger
	// now is the clock the sweep is evaluated at. Production uses the real IST wall clock; tests
	// inject a fixed instant (e.g. an evening slot after every ladder rung) so the ladder is
	// deterministic regardless of when the suite runs. Kept parallel to calendarapp.Service.now.
	now func() time.Time
	// lastRunIterations records how many drain iterations the most recent Run() executed. Test-only
	// observability (same-package tests read it) proving the drain terminates on no-progress rather
	// than spinning the iteration cap when fires are unclaimable (no recipients).
	lastRunIterations int
}

// The park-scoped operational audience for the reminder ladder is resolved by MODULE DUTY, not by a
// literal position-code list.
//
// It used to be []string{"operator", "park_head", "phc_manager"}. None of those three except
// park_head is a code the roster seeder ever writes: real seats are named
// "vaccination_operator_<name>" and "preventive_care_manager", and "phc_manager" exists nowhere
// outside that list. So the ladder addressed one seat out of three, and the operator who actually
// has to run the drive received no T-7, no 08:00, no 13:00 and no due-today reminder. A literal list
// that silently drifts from the seeder is the defect class; naming the DUTY instead means a renamed
// seat that still holds the duty is still notified, and a same-named seat without the duty is not.
//
// (module_code, duty_type) here is exactly what seed-position-duties derives into
// position_module_duties (migration 000157) for vaccination seats: operators carry 'execute';
// park_head / preventive_care_manager / shed_manager (manager+ tiers) carry 'manage'.
const reminderCadenceModuleCode = "pc.vaccination"

var reminderCadenceDutyTypes = []string{"execute", "manage"}

// reminderCadenceTenantPositionCodes is the tenant-scoped leadership audience for the same ladder.
var reminderCadenceTenantPositionCodes = []string{"pc_director", "ceo_internal"}

// NewReminderCadenceStage builds the reminder cadence stage.
func NewReminderCadenceStage(deps Deps, tenantID string) *ReminderCadenceStage {
	calendarRepo := calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	workforceRepo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	return &ReminderCadenceStage{
		deps:     deps,
		calendar: calendarapp.NewService(calendarRepo),
		roster:   workforceapp.NewRosterService(workforceRepo, workforceRepo),
		tenantID: tenantID,
		logger:   deps.Logger,
		now:      func() time.Time { return time.Now().In(biztime.DefaultLocation()) },
	}
}

// withClock overrides the stage clock. Test-only seam: the production constructor always installs the
// real IST wall clock, and Run is exercised unchanged — only the as-of instant is pinned.
func (s *ReminderCadenceStage) withClock(now func() time.Time) *ReminderCadenceStage {
	s.now = now
	return s
}

// Name implements worker.StageRunner.
func (s *ReminderCadenceStage) Name() string { return "reminder-cadence" }

// Run performs one reminder cadence sweep pass:
// 1. SweepReminderCadence finds drives at each ladder slot (T-7, T-daily, T-0)
// 2. For each fire, resolve recipients from workforce (the park's vaccination duty holders)
// 3. QueueReminderCadenceBatch inserts notification_requests rows (idempotent per fire)
func (s *ReminderCadenceStage) Run(ctx context.Context) error {
	if strings.TrimSpace(s.tenantID) == "" {
		return errors.New("reminder cadence stage: tenant id is required")
	}

	now := s.now()

	// CAL-MAIN-02: page the candidate scan with a stable keyset cursor persisted across ticks. Each
	// iteration sweeps ONE bounded page from the cursor, resolves recipients, queues, then advances the
	// cursor to the last candidate that page scanned. A queue CLAIMS each fire that produced >=1
	// notification (QueueReminderCadenceBatch) so a re-scan after a wrap never re-fires it. Because the
	// cursor moves strictly forward, later farms are reached instead of being starved behind a
	// LIMIT-full first page of already-fired candidates, and the persisted cursor lets a tick that hits
	// the per-run page cap resume next tick where it stopped (vaccination-notification-rules.md §3).
	//
	// Termination:
	//   - the page is the tail of the candidate set (Exhausted) -> wrap the cursor to the start and stop;
	//   - a page CLAIMS zero fires (every fire unclaimable: no recipient yet, Finding 1 refuses to burn
	//     the marker) -> stop and DEFER to the next tick. The cursor has already advanced PAST that page,
	//     so this is not a spin: next tick resumes after it (or wraps and retries after a full cycle).
	const (
		perTickLimit  = 200 // bounded sweep per tick (avoid loading an unbounded result set into memory)
		maxIterations = 50  // per-run page cap; a converging drain typically needs only a handful
	)

	// Resume from the cursor persisted by the previous tick (CAL-MAIN-02). A zero cursor
	// (zero time, "") starts from the beginning of the candidate set.
	cursorDueAt, cursorEventID, err := s.calendar.LoadReminderCadenceCursor(ctx, s.tenantID)
	if err != nil {
		return fmt.Errorf("reminder cadence: load cursor: %w", err)
	}

	iterations := 0
	runs := 0
	totalQueued := 0
	exhausted := false
	defer func() { s.lastRunIterations = runs }()
	for iterations = 0; iterations < maxIterations; iterations++ {
		runs++
		fires, sweepCursor, err := s.calendar.SweepReminderCadencePage(ctx, calendarports.ReminderCadenceQuery{
			TenantID:      s.tenantID,
			Now:           now,
			Limit:         perTickLimit,
			CursorDueAt:   cursorDueAt,
			CursorEventID: cursorEventID,
		})
		if err != nil {
			return fmt.Errorf("sweep reminder cadence (iteration %d): %w", iterations, err)
		}

		// No candidate was scanned this page (quiet hours, or the cursor is already past the end without
		// being flagged exhausted). Nothing to advance or queue; stop and let the next tick retry.
		if sweepCursor.EventID == "" && !sweepCursor.Exhausted {
			break
		}

		queued := 0
		if len(fires) > 0 {
			// Collect unique parks from all fires so we can batch-resolve recipients once.
			parkMap := make(map[string]bool)
			for _, fire := range fires {
				parkMap[fire.ParkID] = true
			}
			var parks []string
			for parkID := range parkMap {
				parks = append(parks, parkID)
			}

			// Resolve the audience for every park on this page in ONE batched pair of reads (never a
			// per-park N+1): the park-scoped operational audience by MODULE DUTY, plus the tenant
			// leadership audience, which is attached only to the EOD leadership rung.
			recipientsByPark, tenantRecipients, err := s.resolveCadenceAudience(ctx, parks, now)
			if err != nil {
				return err
			}

			// Map fires to fire inputs with resolved recipients.
			var fireInputs []calendarports.ReminderCadenceFireInput
			for _, fire := range fires {
				recipients := recipientsByPark[fire.ParkID]
				if isLeadershipCadenceSlot(fire) {
					recipients = appendRecipients(recipients, tenantRecipients)
				}
				fireInputs = append(fireInputs, calendarports.ReminderCadenceFireInput{
					Fire:       fire,
					Title:      renderReminderTitle(fire),
					Body:       renderReminderBody(fire),
					Context:    renderReminderContext(fire),
					Recipients: recipients,
				})
			}

			// Queue the batch of fires (set-based insert, claims each fire atomically).
			queued, err = s.calendar.QueueReminderCadenceBatch(ctx, calendarports.QueueReminderCadenceBatch{
				TenantID: s.tenantID,
				Channel:  "push_fcm",
				Fires:    fireInputs,
			})
			if err != nil {
				return fmt.Errorf("queue reminder cadence batch (iteration %d): %w", iterations, err)
			}
			totalQueued += queued
		}

		if s.logger != nil {
			s.logger.Debug("reminder_cadence_stage_iteration",
				"tenant_id", s.tenantID,
				"iteration", iterations,
				"fires_in_batch", len(fires),
				"notification_requests_queued", queued,
			)
		}

		// Advance the keyset cursor to the last candidate this page scanned, so the next iteration (and,
		// once persisted, the next tick) resumes strictly after it.
		if sweepCursor.EventID != "" {
			cursorDueAt, cursorEventID = sweepCursor.DueAt, sweepCursor.EventID
		}

		if sweepCursor.Exhausted {
			// Reached the tail of the candidate set. Wrap to the start (below) and stop.
			exhausted = true
			break
		}

		if len(fires) > 0 && queued == 0 {
			// This page produced fires but none were claimable (zero resolvable recipients). The cursor
			// has already advanced past them, so this is a DEFER, not a spin: stop and let the next tick
			// resume after them (or retry them after a full cycle wraps the cursor).
			if s.logger != nil {
				s.logger.Info("reminder_cadence_stage_deferred_no_recipients",
					"tenant_id", s.tenantID,
					"iteration", iterations,
					"deferred_fires", len(fires),
				)
			}
			break
		}
	}

	// Persist progress across ticks. If we exhausted the candidate set, wrap back to the start so newly
	// due candidates are re-scanned next tick; otherwise resume from where this run stopped so
	// candidates beyond the per-run page cap are eventually reached (CAL-MAIN-02). A save failure is
	// non-fatal: the next tick simply re-processes from the previously stored cursor.
	if exhausted {
		cursorDueAt, cursorEventID = time.Time{}, ""
	}
	if err := s.calendar.SaveReminderCadenceCursor(ctx, s.tenantID, cursorDueAt, cursorEventID, time.Now()); err != nil {
		if s.logger != nil {
			s.logger.Warn("reminder_cadence_cursor_save_failed",
				"tenant_id", s.tenantID,
				"error", err.Error(),
			)
		}
	}

	if iterations >= maxIterations {
		if s.logger != nil {
			s.logger.Warn("reminder_cadence_stage_capped",
				"tenant_id", s.tenantID,
				"iterations_capped_at", maxIterations,
				"total_queued", totalQueued,
			)
		}
	} else if totalQueued > 0 && s.logger != nil {
		s.logger.Info("reminder_cadence_stage_drained",
			"tenant_id", s.tenantID,
			"total_iterations", iterations,
			"total_notification_requests_queued", totalQueued,
		)
	} else if s.logger != nil {
		s.logger.Info("reminder_cadence_stage_no_fires", "tenant_id", s.tenantID)
	}
	return nil
}

// resolveCadenceAudience resolves, for one swept page, who each fire is addressed to:
//
//   - the park-scoped OPERATIONAL audience, by module duty (pc.vaccination execute|manage) -- the
//     operator(s) who run the drive plus the park's managing seats, whatever those seats are named;
//   - the tenant-scoped LEADERSHIP audience (pc_director, ceo_internal), which is role-grant truth
//     rather than a module duty, and which the caller attaches ONLY to the EOD exception rung.
//
// Two batched reads for the whole page -- never one per park.
func (s *ReminderCadenceStage) resolveCadenceAudience(ctx context.Context, parks []string, now time.Time) (map[string][]calendarports.NotificationRecipient, []calendarports.NotificationRecipient, error) {
	// scale-guard:ignore: per-PAGE batched read in the bounded drain loop, not per-park/per-row.
	recipientsByParkPosition, err := s.roster.ResolveModuleDutyRecipientsBatch(
		ctx,
		s.tenantID,
		"center", // parks are 'center'-scoped positions
		parks,
		reminderCadenceModuleCode,
		reminderCadenceDutyTypes,
		now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve module duty recipients batch: %w", err)
	}
	// scale-guard:ignore: per-PAGE tenant leadership batch read; one fixed-size lookup, not per-row fanout.
	tenantRecipientsByPosition, err := s.roster.ResolvePositionRecipientsBatch(
		ctx,
		s.tenantID,
		"tenant",
		[]string{s.tenantID},
		reminderCadenceTenantPositionCodes,
		now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve tenant position recipients batch: %w", err)
	}

	// Both reads key their result by "<scopeID>|<positionCode>", so a park with three duty-holding
	// seats yields three separate keys. Fold every seat's devices back to the bare park id and dedup
	// by device (one member may hold two seats, or one device serve two members) so a recipient is
	// pushed at most once per fire.
	return foldRecipientsByPark(recipientsByParkPosition), foldTenantRecipients(tenantRecipientsByPosition), nil
}

// foldRecipientsByPark collapses the "<parkID>|<positionCode>"-keyed roster batch result into one
// deduped calendar-recipient slice per park id, converting the workforce recipient shape into the
// calendar ports shape. Dedup is by device id within a park (a member holding two seats, or two
// members sharing a device, must not be pushed twice for the same fire).
func foldRecipientsByPark(byParkPosition map[string][]workforcedomain.NotificationRecipient) map[string][]calendarports.NotificationRecipient {
	out := map[string][]calendarports.NotificationRecipient{}
	seen := map[string]bool{} // parkID + "|" + deviceID
	for key, list := range byParkPosition {
		parkID, positionCode, ok := splitParkPositionKey(key)
		if !ok {
			continue
		}
		for _, wr := range list {
			dedupKey := parkID + "|" + wr.DeviceID
			if seen[dedupKey] {
				continue
			}
			seen[dedupKey] = true
			out[parkID] = append(out[parkID], calendarports.NotificationRecipient{
				MemberID:  wr.WorkforceMemberID,
				DeviceID:  wr.DeviceID,
				FCMToken:  wr.FCMToken,
				RoleLabel: positionCode,
			})
		}
	}
	return out
}

func foldTenantRecipients(byTenantPosition map[string][]workforcedomain.NotificationRecipient) []calendarports.NotificationRecipient {
	seen := map[string]bool{}
	var out []calendarports.NotificationRecipient
	for key, list := range byTenantPosition {
		_, positionCode, ok := splitParkPositionKey(key)
		if !ok {
			continue
		}
		for _, wr := range list {
			if wr.DeviceID == "" || seen[wr.DeviceID] {
				continue
			}
			seen[wr.DeviceID] = true
			out = append(out, calendarports.NotificationRecipient{
				MemberID:  wr.WorkforceMemberID,
				DeviceID:  wr.DeviceID,
				FCMToken:  wr.FCMToken,
				RoleLabel: positionCode,
			})
		}
	}
	return out
}

func appendRecipients(base []calendarports.NotificationRecipient, extra []calendarports.NotificationRecipient) []calendarports.NotificationRecipient {
	if len(extra) == 0 {
		return base
	}
	out := append([]calendarports.NotificationRecipient{}, base...)
	seen := map[string]bool{}
	for _, recipient := range out {
		seen[recipient.DeviceID] = true
	}
	for _, recipient := range extra {
		if recipient.DeviceID == "" || seen[recipient.DeviceID] {
			continue
		}
		seen[recipient.DeviceID] = true
		out = append(out, recipient)
	}
	return out
}

func isLeadershipCadenceSlot(fire calendarports.ReminderCadenceFire) bool {
	return fire.NotificationType == "due_today" && fire.Slot == "20:30"
}

// splitParkPositionKey parses the "<scopeID>|<positionCode>" key ResolvePositionRecipientsBatch emits.
func splitParkPositionKey(key string) (scopeID, positionCode string, ok bool) {
	idx := strings.LastIndex(key, "|")
	if idx <= 0 || idx == len(key)-1 {
		return "", "", false
	}
	return key[:idx], key[idx+1:], true
}

// renderReminderTitle returns a generic reminder title based on the fire type.
// The actual title/body rendering could be enhanced to include obligation/drive
// details (e.g., "Vaccination due today: ET (5 drives)") when that data is
// available in the fire structure.
func renderReminderTitle(fire calendarports.ReminderCadenceFire) string {
	switch fire.NotificationType {
	case "advance_notice":
		return "Vaccination due next week"
	case "reminder":
		return "Vaccination reminder"
	case "due_today":
		if isLeadershipCadenceSlot(fire) {
			return "Vaccination EOD exception"
		}
		return "Vaccination due today"
	case "overdue":
		return "Vaccination overdue"
	default:
		return "Vaccination notification"
	}
}

// renderReminderBody returns a body message with the collapsed obligation count.
func renderReminderBody(fire calendarports.ReminderCadenceFire) string {
	switch fire.NotificationType {
	case "advance_notice":
		return fmt.Sprintf("%d vaccination(s) due in 7 days", fire.ObligationCount)
	case "reminder":
		return fmt.Sprintf("%d vaccination(s) due soon", fire.ObligationCount)
	case "due_today":
		if isLeadershipCadenceSlot(fire) {
			return fmt.Sprintf("%d scheduled vaccination shed(s) still not submitted by 8:30 PM", fire.ObligationCount)
		}
		return fmt.Sprintf("%d vaccination(s) due today", fire.ObligationCount)
	case "overdue":
		return fmt.Sprintf("%d vaccination(s) overdue", fire.ObligationCount)
	default:
		return fmt.Sprintf("%d vaccination(s)", fire.ObligationCount)
	}
}

// renderReminderContext returns context metadata for deep-linking and observability.
func renderReminderContext(fire calendarports.ReminderCadenceFire) map[string]string {
	return map[string]string{
		"type":             "vaccination_reminder",
		"obligation_id":    fire.RepresentativeObligationID,
		"park_id":          fire.ParkID,
		"fire_type":        fire.NotificationType,
		"fire_slot":        fire.Slot,
		"obligation_count": fmt.Sprintf("%d", fire.ObligationCount),
		"screen":           "vaccination",
	}
}
