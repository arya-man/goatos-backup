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

// ReminderCadenceStage materializes the vaccination reminder cadence ladder
// (T-7 days, twice-daily, due-today) by calling SweepReminderCadence to find
// fires at each ladder slot, resolving recipients from the workforce roster
// (operators, park heads, PHC managers), and queueing notification_requests rows
// via QueueReminderCadenceBatch. This replaces the deleted cmd/calendar-reminder-sweeper.
// Operational (15m) cadence.
type ReminderCadenceStage struct {
	deps     Deps
	calendar *calendarapp.Service
	roster   *workforceapp.RosterService
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

// reminderCadencePositionCodes is the park-scoped operational audience for the reminder ladder
// (vaccination-notification-rules.md §4a): the operator(s), park head, and PHC manager for the park
// the drive is due in. HQ-tier roles (director/COO/CXO) are deliberately excluded here — they receive
// a digest + escalations, never the per-drive ladder.
var reminderCadencePositionCodes = []string{"operator", "park_head", "phc_manager"}

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
// 2. For each fire, resolve recipients from workforce (operators, park heads, PHC managers)
// 3. QueueReminderCadenceBatch inserts notification_requests rows (idempotent per fire)
func (s *ReminderCadenceStage) Run(ctx context.Context) error {
	if strings.TrimSpace(s.tenantID) == "" {
		return errors.New("reminder cadence stage: tenant id is required")
	}

	now := s.now()

	// Drain all eligible reminder cadence fires: loop, each iteration sweeping a bounded page,
	// resolving recipients, and queueing. A correct queue CLAIMS each fire that produced >=1
	// notification (QueueReminderCadenceBatch), so the next sweep at the same `now` no longer
	// re-derives it and the backlog of recipient-backed fires converges to zero — this drains
	// farms with >200 events (vaccination-notification-rules.md §3).
	//
	// Termination is on NO PROGRESS, not on "zero fires returned":
	//   - a page that returns zero fires  -> fully drained -> stop.
	//   - a page that CLAIMS zero fires    -> the remaining fires are unclaimable right now
	//     (no recipient assigned yet: Finding 1 deliberately refuses to burn their marker).
	//     Re-sweeping would return the SAME fires forever, so we stop and DEFER them to the next
	//     cadence tick instead of spinning the whole iteration cap doing nothing.
	const (
		perTickLimit  = 200 // bounded sweep per tick (avoid loading an unbounded result set into memory)
		maxIterations = 50  // safety cap; a converging drain typically needs only a handful
	)
	iterations := 0
	runs := 0
	totalQueued := 0
	defer func() { s.lastRunIterations = runs }()
	for iterations = 0; iterations < maxIterations; iterations++ {
		runs++
		fires, err := s.calendar.SweepReminderCadence(ctx, calendarports.ReminderCadenceQuery{
			TenantID: s.tenantID,
			Now:      now,
			Limit:    perTickLimit,
		})
		if err != nil {
			return fmt.Errorf("sweep reminder cadence (iteration %d): %w", iterations, err)
		}

		if len(fires) == 0 {
			// Backlog fully drained; no more fires to process.
			break
		}

		// Collect unique parks from all fires so we can batch-resolve recipients once.
		parkMap := make(map[string]bool)
		for _, fire := range fires {
			parkMap[fire.ParkID] = true
		}
		var parks []string
		for parkID := range parkMap {
			parks = append(parks, parkID)
		}

		// Resolve recipients for all parks at once (batch query, not per-park N+1). The park is a 'center'
		// scope in the workforce model; the ladder audience is the park's operator/park-head/PHC-manager
		// seats (vaccination-notification-rules.md §4a).
		// Batched resolve for ALL parks in this drain page in one = ANY($parks) round trip.
		// scale-guard:ignore: per-PAGE batched read in the bounded drain loop, not per-park/per-row.
		recipientsByParkPosition, err := s.roster.ResolvePositionRecipientsBatch(
			ctx,
			s.tenantID,
			"center", // parks are 'center'-scoped positions
			parks,
			reminderCadencePositionCodes,
			now,
		)
		if err != nil {
			return fmt.Errorf("resolve position recipients batch: %w", err)
		}

		// ResolvePositionRecipientsBatch keys its result by "<scopeID>|<positionCode>", so a park with
		// three seats (operator, park_head, phc_manager) yields three separate keys. Fold every seat's
		// devices back to the bare park id and dedup by device (one member may hold two seats, or one
		// device serve two members) so a recipient is pushed at most once per fire.
		recipientsByPark := foldRecipientsByPark(recipientsByParkPosition)

		// Map fires to fire inputs with resolved recipients.
		var fireInputs []calendarports.ReminderCadenceFireInput
		for _, fire := range fires {
			fireInputs = append(fireInputs, calendarports.ReminderCadenceFireInput{
				Fire:       fire,
				Title:      renderReminderTitle(fire),
				Body:       renderReminderBody(fire),
				Context:    renderReminderContext(fire),
				Recipients: recipientsByPark[fire.ParkID],
			})
		}

		// Queue the batch of fires (set-based insert, claims each fire atomically).
		queued, err := s.calendar.QueueReminderCadenceBatch(ctx, calendarports.QueueReminderCadenceBatch{
			TenantID: s.tenantID,
			Channel:  "push_fcm",
			Fires:    fireInputs,
		})
		if err != nil {
			return fmt.Errorf("queue reminder cadence batch (iteration %d): %w", iterations, err)
		}

		totalQueued += queued
		if s.logger != nil {
			s.logger.Debug("reminder_cadence_stage_iteration",
				"tenant_id", s.tenantID,
				"iteration", iterations,
				"fires_in_batch", len(fires),
				"notification_requests_queued", queued,
			)
		}

		if queued == 0 {
			// No fire was claimed this iteration (every returned fire had zero resolvable
			// recipients). Claiming happens iff >=1 notification is inserted, so queued==0 means
			// zero progress. Stop and defer these fires to the next cadence tick — re-sweeping now
			// would just return the same unclaimable fires and spin the iteration cap.
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
		"type":           "vaccination_reminder",
		"obligation_id":  fire.RepresentativeObligationID,
		"park_id":        fire.ParkID,
		"fire_type":      fire.NotificationType,
		"fire_slot":      fire.Slot,
		"obligation_count": fmt.Sprintf("%d", fire.ObligationCount),
		"screen":         "calendar",
		"href":           "/calendar",
	}
}
