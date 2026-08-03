// Package notificationbridge provides the durable outbox consumer for verification events
// (backend/internal/verification/adapters/postgres/repository.go publishes
// verification.item.pending, verification.verdict.rework, verification.verdict.approved).
// This consumer is the NOTIFICATION PUSH LAYER for the generic Verification vertical
// (context/architecture/verification-module-design.md): it reads already-decided verification
// status changes from the durable outbox and produces notification_requests rows.
// It is a pure, read-only CONSUMER that never drives, reimplements, or depends on the
// verification state machine itself — its only job: resolve recipients and queue notifications.
package notificationbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// Real event types published by the verification module to outbox_messages
// (verification/adapters/postgres/repository.go: EventItemPending/EventVerdictRework/EventVerdictApproved):
// - verification.item.pending: proof submitted, an assigned verifier must review it.
// - verification.verdict.rework: proof rejected, the operator must rework + resubmit.
// - verification.verdict.approved: proof approved (no-op push; metrics/digest only).
const (
	EventVerificationItemPending     = "verification.item.pending"
	EventVerificationVerdictRework   = "verification.verdict.rework"
	EventVerificationVerdictApproved = "verification.verdict.approved"
	EventVerificationItemClosed      = "verification.item.closed"
	EventVaccinationDriveReady       = "verification.vaccination_drive.ready"
	EventVaccinationDriveClosed      = "verification.vaccination_drive.closed"
)

// notification_type values this consumer writes. Both are in the migration 000179
// notification_requests_type_check allowed set ('verification_pending', 'rework'), the
// SAME two the legacy VerificationNotifier uses — see NotificationTypeVerificationPending /
// NotificationTypeRework in verification_notify.go and TestNotificationTypeEnumGuard.

// legacyVaccinationSourceModule / legacyVaccinationSourceRefType identify a generic
// verification_item that was materialized FROM a legacy vaccination SOP proof submission
// (sopbridge/vaccination_submission.go emitVerificationItem sets Source.Module="vaccination",
// Source.RefType="sop_submission"). Such an item's underlying SOP task, when reworked, ALSO
// fans out one vaccination.verify.rejected per completion (sopbridge/verify_fanout.go
// OnTaskReworked) that the legacy VerificationNotifier already routes to operator + park head.
// See legacyHandledVaccination below for the exact suppression rule.
const (
	legacyVaccinationSourceModule  = "vaccination"
	legacyVaccinationSourceRefType = "sop_submission"
	positionPCDirector             = "pc_director"
	positionGrowthDirector         = "growth_director"
	positionCEOInternal            = "ceo_internal"
	// positionFeedDirector / positionHealthDirector are the tenant seats that own Feed and
	// Counts (maintainer decision 2026-08-01). health_director is NOT pc_director: Health and
	// Preventive Care are separate departments, so routing a counts proof to the PC Director
	// would be the same wrong-module defect weighing had.
	positionFeedDirector   = "feed_director"
	positionHealthDirector = "health_director"

	moduleWeighing = "weighing"
	// moduleFeed / moduleCounts mirror feeddirection/domain.VerificationModuleFeed and
	// tasks/domain.VerificationModuleCounts, the strings their verificationbridge enqueuers
	// write into the item's Module.
	moduleFeed   = "feed"
	moduleCounts = "counts"

	// dutyModuleFeed / dutyModuleCounts are the position_module_duties.module_code values whose
	// 'verify' duty holders review these modules' proofs. dutyModuleFeed matches the
	// "feeding" -> "feed.direction" mapping in backend/cmd/seed-position-duties/main.go.
	dutyModuleFeed   = "feed.direction"
	dutyModuleCounts = "counts"
)

// pendingModuleProfile is the per-module routing + copy contract of a verification.item.pending
// push. The generic verification vertical is shared by several owning modules, so WHO is told and
// WHAT they are told must be selected by the item's own Module rather than assumed to be
// vaccination. Before this existed the handler hardcoded the vaccination verify duty and the PC
// director, so a weighing proof reached the vaccination verifier and the PC director with
// vaccination wording, while the growth director -- who owns weighing -- was never told.
type pendingModuleProfile struct {
	// messageKeyPrefix is the stable, locale-independent notification identifier the Android
	// client uses to look up its own localized string for this module's pushes (issue #27:
	// Title/Body stay English-only fallbacks; message_key + structured context fields are the
	// real localization contract). Never reuse dutyModule for this — dutyModule is a
	// position_module_duties.module_code ("feed.direction") that has drifted from the
	// human-readable module name ("feed") and is not meant to be a translation-catalog key.
	messageKeyPrefix string
	// dutyModule is the position_module_duties.module_code whose 'verify' duty holders review this
	// module's proofs at the park.
	dutyModule string
	// leadershipPosition is the tenant-scope director seat that owns the module.
	leadershipPosition string
	// leadershipRoleLabel is the recipient role label recorded on the queued notification.
	leadershipRoleLabel  string
	verifierTitle        string
	verifierBodySuffix   string
	leadershipTitle      string
	leadershipBodySuffix string
	leadershipScreen     string
	leadershipTarget     string

	// The three LIFECYCLE pushes that follow the pending one. They used to be hardcoded to the
	// PC Director in vaccination wording for EVERY module ("Vaccination proof verified",
	// "vaccination record"), which is the same wrong-module defect the pending path already
	// fixed: a feed or counts approval pushed vaccination copy to the wrong director. Each
	// module now carries its own recipient seat (leadershipPosition above) and its own copy.
	approvedTitle string
	approvedBody  string
	// approvedScreen/approvedTarget, reworkScreen/reworkTarget and closedScreen/closedTarget are
	// the client route the tap opens. They stay module-owned for the same reason the titles do.
	approvedScreen string
	approvedTarget string
	reworkTitle    string
	reworkBody     string
	// reworkReasonBody is the body used when the verifier supplied a reason; the reason is
	// inserted verbatim between the two halves.
	reworkReasonPrefix string
	reworkReasonSuffix string
	reworkScreen       string
	reworkTarget       string
	closedTitle        string
	closedBody         string
	closedScreen       string
	closedTarget       string
}

// pendingModuleProfiles is keyed by the item's Module (VerificationEventPayload.Module, which the
// producing bridge sets from its own module constant: "vaccination" for the SOP bridge,
// weighingdomain.VerificationModuleWeighing = "weighing" for weighing).
var pendingModuleProfiles = map[string]pendingModuleProfile{
	legacyVaccinationSourceModule: {
		messageKeyPrefix:     "vaccination",
		dutyModule:           moduleVaccination,
		leadershipPosition:   positionPCDirector,
		leadershipRoleLabel:  "pc_director",
		verifierTitle:        "Video verification waiting",
		verifierBodySuffix:   " vaccinated; video is waiting for verification.",
		leadershipTitle:      "Vaccination video pending",
		leadershipBodySuffix: " vaccinated; video verification is pending.",
		leadershipScreen:     "vaccination_overview",
		leadershipTarget:     "/vaccination",

		approvedTitle:      "Vaccination proof verified",
		approvedBody:       "The proof is ready for operational closure.",
		approvedScreen:     "leadership_close",
		approvedTarget:     "/vaccination",
		reworkTitle:        "Vaccination proof rejected — rework needed",
		reworkBody:         "The verifier rejected a vaccination proof. This needs to be resubmitted.",
		reworkReasonPrefix: "The verifier rejected a vaccination proof. Reason: ",
		reworkReasonSuffix: " Please resubmit.",
		reworkScreen:       "record",
		reworkTarget:       "/vaccination",
		closedTitle:        "Vaccination record closed",
		closedBody:         "The verified vaccination record is now complete.",
		closedScreen:       "record",
		closedTarget:       "/vaccination",
	},
	moduleWeighing: {
		messageKeyPrefix:     "weighing",
		dutyModule:           moduleWeighing,
		leadershipPosition:   positionGrowthDirector,
		leadershipRoleLabel:  roleLabelGrowthDirector,
		verifierTitle:        "Weighing video waiting",
		verifierBodySuffix:   " weighed; video is waiting for verification.",
		leadershipTitle:      "Weighing video pending",
		leadershipBodySuffix: " weighed; video verification is pending.",
		leadershipScreen:     "weighing_overview",
		// Both leadership-facing weighing pushes are about PROOF, and "/weighing" is the
		// operator's own work list -- a Growth Director who tapped one landed on an empty
		// My Work with no route to the video. The leadership proof gallery is the surface that
		// answers what these two pushes announce. (The bucket-level deep link the lifecycle
		// consumer emits is not available here: this payload carries the shed LOCATION id, never
		// the campaign/bucket identity that names a weighing task.)
		leadershipTarget: weighingEvidenceTarget,

		approvedTitle:      "Weighing proof verified",
		approvedBody:       "The proof is ready for operational closure.",
		approvedScreen:     "leadership_close",
		approvedTarget:     weighingEvidenceTarget,
		reworkTitle:        "Weighing proof rejected — rework needed",
		reworkBody:         "The verifier rejected a weighing proof. This needs to be resubmitted.",
		reworkReasonPrefix: "The verifier rejected a weighing proof. Reason: ",
		reworkReasonSuffix: " Please resubmit.",
		reworkScreen:       "record",
		reworkTarget:       "/weighing",
		closedTitle:        "Weighing record closed",
		closedBody:         "The verified weighing record is now complete.",
		closedScreen:       "record",
		closedTarget:       "/weighing",
	},
	// Feed covers BOTH gated feed completions -- packing and distribution -- plus feed transport;
	// all three enqueue with Module="feed" and are kept apart only by ref_type, so one profile is
	// correct here. Wording is feed's own ("fed"), never vaccination's ("vaccinated"), and the
	// tap route is the feed surface. Feed_Director.pdf M1 makes the Feed Director the person who
	// confirms daily that feeding SOP videos are actually being reviewed.
	moduleFeed: {
		messageKeyPrefix:     "feed",
		dutyModule:           dutyModuleFeed,
		leadershipPosition:   positionFeedDirector,
		leadershipRoleLabel:  positionFeedDirector,
		verifierTitle:        "Feed video waiting",
		verifierBodySuffix:   " fed; video is waiting for verification.",
		leadershipTitle:      "Feed video pending",
		leadershipBodySuffix: " fed; feed video verification is pending.",
		leadershipScreen:     "feed_overview",
		leadershipTarget:     "/feed",

		approvedTitle:      "Feed proof verified",
		approvedBody:       "The proof is ready for operational closure.",
		approvedScreen:     "leadership_close",
		approvedTarget:     "/feed",
		reworkTitle:        "Feed proof rejected — rework needed",
		reworkBody:         "The verifier rejected a feed proof. This needs to be resubmitted.",
		reworkReasonPrefix: "The verifier rejected a feed proof. Reason: ",
		reworkReasonSuffix: " Please resubmit.",
		reworkScreen:       "record",
		reworkTarget:       "/feed",
		closedTitle:        "Feed record closed",
		closedBody:         "The verified feed record is now complete.",
		closedScreen:       "record",
		closedTarget:       "/feed",
	},
	// Counts proofs (the shifting/movement completion video) route to the Health Director, who
	// owns Counts per the 2026-08-01 maintainer decision. Deliberately NOT pc_director.
	moduleCounts: {
		messageKeyPrefix:     "counts",
		dutyModule:           dutyModuleCounts,
		leadershipPosition:   positionHealthDirector,
		leadershipRoleLabel:  positionHealthDirector,
		verifierTitle:        "Counts video waiting",
		verifierBodySuffix:   " recorded; video is waiting for verification.",
		leadershipTitle:      "Counts video pending",
		leadershipBodySuffix: " recorded; counts video verification is pending.",
		leadershipScreen:     "counts_overview",
		leadershipTarget:     "/counts",

		approvedTitle:      "Counts proof verified",
		approvedBody:       "The proof is ready for operational closure.",
		approvedScreen:     "leadership_close",
		approvedTarget:     "/counts",
		reworkTitle:        "Counts proof rejected — rework needed",
		reworkBody:         "The verifier rejected a counts proof. This needs to be resubmitted.",
		reworkReasonPrefix: "The verifier rejected a counts proof. Reason: ",
		reworkReasonSuffix: " Please resubmit.",
		reworkScreen:       "record",
		reworkTarget:       "/counts",
		closedTitle:        "Counts record closed",
		closedBody:         "The verified counts record is now complete.",
		closedScreen:       "record",
		closedTarget:       "/counts",
	},
}

// PendingNotificationDutyModules returns the position_module_duties.module_code of every module
// that routes a pending-proof push, sorted for determinism.
//
// It is exported for ONE reason: backend/cmd/seed-position-duties must seed a 'verify' duty
// holder for exactly these modules, and its seed-closeout assertion must fail when one has none.
// Re-listing the modules in the seeder by hand is how position_module_duties ended up with zero
// verify rows while every notification test stayed green -- the seeder emitted only
// 'execute'/'manage', so ResolveModuleDutyRecipients matched nothing and EVERY verifier push,
// vaccination and weighing included, resolved to zero devices in the field.
func PendingNotificationDutyModules() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(pendingModuleProfiles))
	for _, profile := range pendingModuleProfiles {
		if _, ok := seen[profile.dutyModule]; ok {
			continue
		}
		seen[profile.dutyModule] = struct{}{}
		out = append(out, profile.dutyModule)
	}
	sort.Strings(out)
	return out
}

// pendingProfileFor selects the routing contract for an item's module. There is deliberately NO
// fallback: defaulting an unclaimed module to the vaccination profile is how weighing proofs came
// to notify the vaccination verifier and the PC director in vaccination words, for months, with
// nothing in the logs that read as wrong. A module with no declared owner is a wiring gap, and the
// only safe answer is to notify nobody loudly rather than the wrong people quietly.
func pendingProfileFor(module string) (pendingModuleProfile, bool) {
	profile, ok := pendingModuleProfiles[strings.ToLower(strings.TrimSpace(module))]
	return profile, ok
}

// verificationSource is the producer's source back-reference (verificationVerdictPayload /
// verificationItemPendingPayload in the verification repository). module + ref_type are the
// two fields that identify a legacy-overlapping vaccination item.
type verificationSource struct {
	Module       string `json:"module"`
	TaskID       string `json:"task_id"`
	SubmissionID string `json:"submission_id"`
	RefType      string `json:"ref_type"`
	RefID        string `json:"ref_id"`
}

// VerificationEventPayload is the outbox payload emitted by the verification repository for all
// three events (verificationItemPendingPayload / verificationVerdictPayload). Field set is fixed
// by that producer contract: tenant/item identity + classification + who-to-route-to
// (operator/shed/park) + the decision/reason + the source back-reference used for legacy dedup.
type VerificationEventPayload struct {
	TenantID     string             `json:"tenant_id"`
	ItemID       string             `json:"item_id"`
	Vertical     string             `json:"vertical"`
	Module       string             `json:"module"`
	Category     string             `json:"category"`
	SubjectLabel string             `json:"subject_label"`
	OperatorID   string             `json:"operator_id"`
	ShedID       string             `json:"shed_id"`
	ParkID       string             `json:"park_id"`
	Decision     string             `json:"decision"` // "approved" | "rejected" (verdict events)
	Status       string             `json:"status"`   // status alias kept alongside decision
	Reason       string             `json:"reason"`   // optional rework reason
	VerifiedBy   string             `json:"verified_by"`
	ClosedBy     string             `json:"closed_by"`
	BatchID      string             `json:"batch_id"`
	CapturedAt   string             `json:"captured_at"`
	Source       verificationSource `json:"source"`
}

// legacyHandledVaccination reports whether this item is ALSO covered by the legacy
// vaccination.verify.rejected notification path, so the generic rework push must be suppressed
// to avoid double-notifying the operator + park head. True exactly when the item was produced
// from a legacy vaccination SOP submission (Source.Module="vaccination" AND
// Source.RefType="sop_submission"). Non-vaccination generic verticals (feed/diagnosis/death/
// breeding, future modules) have NO legacy notifier and are never suppressed.
func (p VerificationEventPayload) legacyHandledVaccination() bool {
	return strings.EqualFold(strings.TrimSpace(p.Source.Module), legacyVaccinationSourceModule) &&
		strings.EqualFold(strings.TrimSpace(p.Source.RefType), legacyVaccinationSourceRefType)
}

// VerificationEventConsumer is a durable, idempotent consumer of verification.item.pending,
// verification.verdict.rework, and verification.verdict.approved. It resolves each event to its
// recipient set and produces notification_requests rows via the shared calendar notification queue.
//
// Idempotency: every write goes through QueueRoleNotifications, whose per-recipient INSERT is
// ON CONFLICT (tenant_id, idempotency_key) DO NOTHING keyed by (tenant, EventKey, device), so an
// at-least-once redelivery of the SAME event never duplicates a row.
//
// DLQ: a corrupt (unparseable) payload is a poison message — it can never succeed on retry — so it
// is returned as eventbus.PermanentError, which the durable domain-event consumer nacks straight to
// the DLQ (domainconsumer/app/service.go: eventbus.IsPermanentError → domain_event_permanent_failure_nacked_for_dlq).
// A transient failure (recipient resolution / queue write) is returned as a plain error so the same
// consumer RETRIES it; idempotency makes that replay side-effect-free.
//
// Advisory only: the consumer writes notification_requests rows and NOTHING else. It never completes
// obligations, completes/rejects verification items, or mutates any owning-vertical state — the
// verification vertical retains sole completion authority.
type VerificationEventConsumer struct {
	recipients RecipientResolver
	queue      NotificationQueue
	logger     *slog.Logger
	// locations is optional park/shed name enrichment (see location_names.go). Enriches approval
	// copy to be meaningful: instead of abstract "The proof is ready for operational closure",
	// an approval says "ET+TT vaccination proof for Shed A (Park Name) is verified." per
	// docs/decisions/2026-08-02-meaningful-notification-copy.md.
	locations *LocationNameResolver
	// vaccineLabels is optional vaccine label enrichment (see vaccine_labels.go). Resolves
	// vaccination_rules.vaccine_label for a bounded set of rule IDs per event.
	vaccineLabels *VaccineLabelResolver
}

// NewVerificationEventConsumer constructs the consumer over the workforce recipient resolver and
// the calendar notification queue (the same two seams the legacy VerificationNotifier uses).
func NewVerificationEventConsumer(recipients RecipientResolver, queue NotificationQueue, logger *slog.Logger) *VerificationEventConsumer {
	return &VerificationEventConsumer{recipients: recipients, queue: queue, logger: logger}
}

// WithLocationNames attaches park/shed name enrichment. Chainable at construction time.
func (c *VerificationEventConsumer) WithLocationNames(resolver *LocationNameResolver) *VerificationEventConsumer {
	c.locations = resolver
	return c
}

// WithVaccineLabels attaches vaccine label enrichment. Chainable at construction time.
func (c *VerificationEventConsumer) WithVaccineLabels(resolver *VaccineLabelResolver) *VerificationEventConsumer {
	c.vaccineLabels = resolver
	return c
}

var _ eventbus.Handler = (*VerificationEventConsumer)(nil)

// Register subscribes the consumer to the three verification events on a bus. In production the
// outbox relay (eventbus publisher) and the domain-event consumer both dispatch these to the bus.
func (c *VerificationEventConsumer) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationItemPending, c)
	bus.Subscribe(EventVerificationVerdictRework, c)
	bus.Subscribe(EventVerificationVerdictApproved, c)
	bus.Subscribe(EventVerificationItemClosed, c)
	bus.Subscribe(EventVaccinationDriveReady, c)
	bus.Subscribe(EventVaccinationDriveClosed, c)
}

// HandleEvent routes by event type. A corrupt payload → PermanentError (DLQ); a well-formed but
// non-actionable event (approved, or a missing routing field) → nil no-op; a transient dependency
// failure → plain error (retryable).
func (c *VerificationEventConsumer) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if c == nil || c.recipients == nil || c.queue == nil {
		return nil
	}

	switch e.Type {
	case EventVerificationItemPending, EventVerificationVerdictRework,
		EventVerificationVerdictApproved, EventVerificationItemClosed,
		EventVaccinationDriveReady, EventVaccinationDriveClosed:
		p, err := decodePayload(e.Payload)
		if err != nil {
			// Poison message: unparseable JSON can never succeed on retry → DLQ.
			return eventbus.PermanentError(fmt.Errorf("verification_notify_consumer: decode %s payload: %w", e.Type, err))
		}
		if e.Type == EventVerificationItemPending {
			return c.handleItemPending(ctx, p)
		}
		if e.Type == EventVerificationVerdictRework {
			return c.handleVerdictRework(ctx, p)
		}
		if e.Type == EventVerificationVerdictApproved {
			return c.handleVerdictApproved(ctx, p)
		}
		if e.Type == EventVerificationItemClosed {
			return c.handleItemClosed(ctx, p)
		}
		if e.Type == EventVaccinationDriveReady {
			return c.handleVaccinationDriveReady(ctx, p)
		}
		return c.handleVaccinationDriveClosed(ctx, p)
	default:
		return nil
	}
}

func (c *VerificationEventConsumer) handleVaccinationDriveReady(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	batchID := strings.TrimSpace(p.BatchID)
	if tenantID == "" || batchID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recipients []calendarports.NotificationRecipient
	if parkID := strings.TrimSpace(p.ParkID); parkID != "" {
		parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
		if err != nil {
			return err
		}
		recipients = append(recipients, toQueueRecipients(parkHeadDevices, "park_head")...)
	}
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionPCDirector)
	if err != nil {
		return err
	}
	recipients = append(recipients, toQueueRecipients(directorDevices, "pc_director")...)
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return err
	}
	recipients = dedupeQueueRecipients(append(recipients, toQueueRecipients(ceoDevices, "ceo")...))
	// Name the park: "all proof videos for this vaccination drive are verified" gives a
	// director nothing to act on (2026-08-02 meaningful-notification rule).
	readyPark := "this park"
	if parkID := strings.TrimSpace(p.ParkID); parkID != "" {
		if name := strings.TrimSpace(c.locations.ResolveNames(ctx, tenantID, parkID)[parkID]); name != "" {
			readyPark = name
		}
	}
	eventKey := EventVaccinationDriveReady + ":" + batchID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(batchID),
		TargetType:       "vaccination_batch",
		TargetID:         batchID,
		NotificationType: "verification_approved",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Vaccination drive ready to close",
		Body:             "All proof videos for the vaccination drive at " + readyPark + " are verified. It is ready to close.", // notification-copy:ignore: body names the park via readyPark (resolved above)
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         "verification_approved",
			"screen":       "leadership_close",
			"message_key":  "vaccination.drive.ready_to_close",
			"batch_id":     batchID,
			"park_id":      p.ParkID,
			"group_key":    "verification:" + tenantID + ":vaccination_drive",
			"collapse_key": "verification:" + tenantID + ":vaccination_drive",
			"priority":     priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}

func (c *VerificationEventConsumer) handleVaccinationDriveClosed(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	batchID := strings.TrimSpace(p.BatchID)
	if tenantID == "" || batchID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return err
	}
	// A close notice that says only "the Director closed a vaccination drive" tells a leader
	// nothing they can act on (2026-08-02 meaningful-notification rule). Name the park.
	closedPark := "this park"
	if parkID := strings.TrimSpace(p.ParkID); parkID != "" {
		if name := strings.TrimSpace(c.locations.ResolveNames(ctx, tenantID, parkID)[parkID]); name != "" {
			closedPark = name
		}
	}
	eventKey := EventVaccinationDriveClosed + ":" + batchID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(batchID),
		TargetType:       "vaccination_batch",
		TargetID:         batchID,
		NotificationType: "verification_closed",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            "Vaccination drive closed",
		Body:             "The vaccination drive at " + closedPark + " was closed by the Director.", // notification-copy:ignore: body names the park via closedPark (resolved above)
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":   "verification_closed",
			"screen": "record",
			// The record screen belongs to VACCINATION. Every push that names it must declare its
			// module, because the client can no longer assume one: a module-blind match on
			// screen="record" was sending weighing and feed rework notices into the vaccination
			// record for that shed. An absent category now means "not vaccination", so omitting it
			// here would silently strand this drive-closed notice on the recipient's home screen.
			"category":     "vaccination",
			"message_key":  "vaccination.drive.closed",
			"batch_id":     batchID,
			"group_key":    "verification:" + tenantID + ":vaccination_drive",
			"collapse_key": "verification:" + tenantID + ":vaccination_drive",
			"priority":     priorityNormal,
		},
		Recipients: toQueueRecipients(ceoDevices, "ceo"),
	})
	return err
}

func (c *VerificationEventConsumer) handleVerdictApproved(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" || parkID == "" {
		return nil
	}
	// Same no-fallback rule as the pending path: an unclaimed module notifies NOBODY loudly
	// rather than pushing another module's director another module's wording.
	profile, known := pendingProfileFor(p.Module)
	if !known {
		c.logUnroutedModule(ctx, "verification_approved_notification_unrouted_module", tenantID, itemID, parkID, p.Module)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
	if err != nil {
		return err
	}
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, profile.leadershipPosition)
	if err != nil {
		return err
	}
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return err
	}
	recipients := dedupeQueueRecipients(
		append(append(
			toQueueRecipients(parkHeadDevices, "park_head"),
			toQueueRecipients(directorDevices, profile.leadershipRoleLabel)...),
			toQueueRecipients(ceoDevices, "ceo")...),
	)

	// Enrich the approval body with specific, meaningful details: park, shed, vaccine/category, date.
	// This closes defect 2026-08-02: abstract copy like "The proof is ready for operational closure"
	// tells a director nothing they can act on. The enriched body names the exact park/shed/vaccine/date
	// so they can correlate it to their work. Per docs/decisions/2026-08-02-meaningful-notification-copy.md.
	approvedTitle := profile.approvedTitle
	approvedBody := profile.approvedBody
	approvedBodyEnriched := enrichApprovedNotificationCopy(ctx, c.locations, c.vaccineLabels, c.logger,
		tenantID, p.Module, parkID, p.ShedID, p.Category, p.Source.TaskID)
	if approvedBodyEnriched != "" {
		approvedBody = approvedBodyEnriched
	}

	eventKey := EventVerificationVerdictApproved + ":" + itemID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: "verification_approved",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            approvedTitle,
		Body:             approvedBody,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         "verification_approved",
			"screen":       profile.approvedScreen,
			"target":       profile.approvedTarget,
			"message_key":  profile.messageKeyPrefix + ".proof.approved",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}

func (c *VerificationEventConsumer) handleItemClosed(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	operatorID := strings.TrimSpace(p.OperatorID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" {
		return nil
	}
	// A WITHDRAWAL is a retraction, not an operational closure, and it has a different
	// audience. Operational closure tells the OPERATOR their record is done. A withdrawal
	// says the opposite: the producing module superseded the source record, so the review
	// this item asked for is cancelled. The person holding stale work is the VERIFIER who
	// received the verification.item.pending push -- and the operator is the one who caused
	// the withdrawal (they edited their own draft), so pushing them a "closed" notice would
	// be both wrong and noisy.
	if strings.TrimSpace(p.Status) == verificationdomain.StatusWithdrawn {
		return c.handleItemWithdrawn(ctx, p, tenantID, itemID, parkID)
	}
	if operatorID == "" {
		return nil
	}
	profile, known := pendingProfileFor(p.Module)
	if !known {
		c.logUnroutedModule(ctx, "verification_closed_notification_unrouted_module", tenantID, itemID, parkID, p.Module)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	operatorDevices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
	if err != nil {
		return err
	}
	eventKey := EventVerificationItemClosed + ":" + itemID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: "verification_closed",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            profile.closedTitle,
		Body:             profile.closedBody,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         "verification_closed",
			"screen":       profile.closedScreen,
			"target":       profile.closedTarget,
			"message_key":  profile.messageKeyPrefix + ".record.closed",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityNormal,
		},
		Recipients: toQueueRecipients(operatorDevices, "operator"),
	})
	return err
}

// handleItemWithdrawn retracts the review request that verification.item.pending raised.
//
// It targets exactly the audience the pending push went to -- the park's module verify-duty
// holders -- and reuses the pending event's CalendarEventID/TargetID so the retraction lands on
// the same notification subject the verifier is already looking at. The producing module
// superseded the source record; there is nothing left to review.
func (c *VerificationEventConsumer) handleItemWithdrawn(ctx context.Context, p VerificationEventPayload, tenantID, itemID, parkID string) error {
	if parkID == "" {
		return nil
	}
	profile, known := pendingProfileFor(p.Module)
	if !known {
		c.logUnroutedModule(ctx, "verification_withdrawn_notification_unrouted_module", tenantID, itemID, parkID, p.Module)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	verifierDevices, err := c.recipients.ResolveModuleDutyRecipients(ctx, tenantID, scopeCenter, parkID, profile.dutyModule, dutyVerify)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve verifier recipients: %w", err) // retryable
	}
	recipients := dedupeQueueRecipients(toQueueRecipients(verifierDevices, "verifier"))
	if len(recipients) == 0 {
		return nil
	}
	subject := strings.TrimSpace(p.SubjectLabel)
	if subject == "" {
		subject = "A record"
	}
	eventKey := EventVerificationItemClosed + ":withdrawn:" + itemID
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: "verification_withdrawn",
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		// The title names the SUBJECT (the shed or record under review), not just the action: a
		// verifier with several pending reviews needs to know WHICH one was withdrawn without
		// opening the app. "Verification no longer needed" alone told them nothing actionable.
		Title:    subject + " — review withdrawn",
		Body:     subject + " was updated by the operator, so this review request is withdrawn.",
		TraceID:  eventKey,
		EventKey: eventKey,
		Context: map[string]string{
			"type":         "verification_withdrawn",
			"screen":       "verification",
			"target":       "/verification/items/" + itemID,
			"message_key":  profile.messageKeyPrefix + ".proof.withdrawn",
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"subject":      subject,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityNormal,
		},
		Recipients: recipients,
	})
	return err
}

// enrichApprovedNotificationCopy generates a specific, meaningful body for verification approval
// notifications instead of abstract copy. Returns empty string if enrichment fails, signaling the
// caller to use the fallback generic body.
//
// Example transformation for vaccination:
//
//	FROM: "The proof is ready for operational closure."
//	TO:   "ET+TT vaccination proof for Shed A (Park Name) is verified."
//
// Example for weighing:
//
//	FROM: "The proof is ready for operational closure."
//	TO:   "Weighing proof for Shed B (Park Name) is verified."
//
// Enrichment is optional: location / vaccine lookups are best-effort, and transient failures
// gracefully degrade to the fallback copy rather than blocking notification delivery.
func enrichApprovedNotificationCopy(ctx context.Context, locations locationNameSource,
	vaccineLabels vaccineLabelSource, logger *slog.Logger,
	tenantID, module, parkID, shedID, category, sourceTaskID string) string {
	parkID = strings.TrimSpace(parkID)
	shedID = strings.TrimSpace(shedID)
	category = strings.TrimSpace(category)
	module = strings.TrimSpace(module)
	sourceTaskID = strings.TrimSpace(sourceTaskID)

	// Resolve park and shed names (ONE batched query, not one per name).
	parkName, shedName := "", ""
	if locations != nil {
		locNames := locations.ResolveNames(ctx, tenantID, parkID, shedID)
		parkName = locNames[parkID]
		shedName = locNames[shedID]
	}

	// Build a farm-readable location phrase. "Shed A" or "Shed A (Park Name)".
	location := ""
	switch {
	case shedName != "" && parkName != "":
		location = shedName + " (" + parkName + ")"
	case shedName != "":
		location = shedName
	case parkName != "":
		location = parkName
	default:
		// Neither park nor shed resolved; fall back to generic copy.
		return ""
	}

	// For vaccination module: name the dose (e.g., "ET+TT vaccination proof for Shed A ...").
	//
	// C19c: this used to pass Category straight into ResolveVaccineLabels as if it were a
	// protocol_rules.rule_id. It never is -- production sends the fixed registry category
	// ("vaccination_proof", sopbridge.VaccinationVerificationCategory), so the lookup could not
	// match a row and EVERY vaccination approval degraded to the generic wording. The fix does not
	// wait for a new producer contract: the payload already carries Source.TaskID, and
	// obligation_instances links that sop task to the rules it discharged, so the dose identity is
	// recoverable from what production actually sends today. A rule id is still honoured if a
	// future producer sends one, because that is the cheaper and more precise key.
	if strings.EqualFold(module, legacyVaccinationSourceModule) && vaccineLabels != nil {
		if label := vaccinationDoseLabel(ctx, vaccineLabels, logger, tenantID, category, sourceTaskID); label != "" {
			return label + " vaccination proof for " + location + " is verified."
		}
	}

	// For non-vaccination modules (weighing, feed, counts), use module-agnostic wording.
	// Module names are internals; use the farm-readable gerund (weighing, feeding, etc).
	moduleNoun := "work"
	switch {
	case strings.EqualFold(module, legacyVaccinationSourceModule):
		moduleNoun = "vaccination"
	case strings.EqualFold(module, moduleWeighing):
		moduleNoun = "weighing"
	case strings.EqualFold(module, moduleFeed):
		moduleNoun = "feeding"
	case strings.EqualFold(module, moduleCounts):
		moduleNoun = "counts"
	}
	return moduleNoun + " proof for " + location + " is verified."
}

// locationNameSource and vaccineLabelSource are the two enrichment reads the approval copy needs,
// named as behaviour rather than as the concrete pool-backed resolvers. Both enrichments are
// optional decoration, so a copy path must be provable without a database standing behind it --
// that is the only reason these exist; production still passes the real resolvers.
type locationNameSource interface {
	ResolveNames(ctx context.Context, tenantID string, ids ...string) map[string]string
}

type vaccineLabelSource interface {
	ResolveVaccineLabels(ctx context.Context, tenantID string, ruleIDs ...string) map[string]string
	ResolveVaccineLabelsForTask(ctx context.Context, tenantID, sopTaskID string) []string
}

// vaccinationDoseLabel resolves the dose phrase for an approved vaccination proof from whichever
// identity the payload actually carries.
//
// Order is precision-first: a real protocol_rules.rule_id in category is one indexed row, so it
// wins when a producer ever sends one; otherwise the sop task is walked back to the rules it
// discharged. Only when NEITHER key is usable is the generic wording accepted, and that is warned
// once so a future producer change that drops the task id is visible instead of silently emptying
// every vaccination push of its dose again.
func vaccinationDoseLabel(ctx context.Context, vaccineLabels vaccineLabelSource, logger *slog.Logger,
	tenantID, category, sourceTaskID string) string {
	if looksLikeUUID(category) {
		if label := vaccineLabels.ResolveVaccineLabels(ctx, tenantID, category)[category]; label != "" {
			return label
		}
	}
	if sourceTaskID != "" {
		// A combo visit discharges several rules at once, which is what a phrase like "ET+TT"
		// says; joining them keeps the push truthful about everything that was verified.
		if labels := vaccineLabels.ResolveVaccineLabelsForTask(ctx, tenantID, sourceTaskID); len(labels) > 0 {
			return strings.Join(labels, "+")
		}
		return ""
	}
	if logger != nil {
		logger.WarnContext(ctx, "vaccine label enrichment skipped: payload carries neither a rule id nor a source task id",
			"tenant_id", tenantID, "category", category)
	}
	return ""
}

// decodePayload parses the outbox payload. Returns an error only for genuinely corrupt JSON (the
// DLQ trigger); an empty payload decodes to a zero-value struct that the handlers treat as a no-op.
func decodePayload(raw []byte) (VerificationEventPayload, error) {
	var p VerificationEventPayload
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return VerificationEventPayload{}, err
	}
	return p, nil
}

// handleItemPending notifies everyone who must react when an operator submits a shed for review:
// the park's vaccination verifier duty holder, that park's head, tenant PC directors, and tenant
// CEOs. Multiple goat-level verification items can be created for one shed submission; when the
// source carries submission_id, the idempotency key is submission-scoped so those items collapse to
// one queued push per recipient device.
func (c *VerificationEventConsumer) handleItemPending(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" {
		return nil // Not even identifiable → no-op.
	}
	if parkID == "" {
		// A well-formed item with no park is a PRODUCER wiring gap: every recipient of this push
		// resolves at park scope, so the operator's proof waits with nobody told. Silence is how
		// the missing weighing push hid for so long, so this is logged loudly instead.
		if c.logger != nil {
			c.logger.WarnContext(ctx, "verification_pending_notification_missing_park",
				"tenant_id", tenantID, "item_id", itemID, "module", p.Module,
				"source_module", p.Source.Module, "source_ref_type", p.Source.RefType)
		}
		return nil
	}

	profile, known := pendingProfileFor(p.Module)
	if !known {
		// Not retryable: a redelivery resolves nothing, because the gap is a missing entry in
		// pendingModuleProfiles, not a transient failure. Drop with a loud error so the item
		// surfaces as unrouted instead of being delivered to the wrong module's people.
		if c.logger != nil {
			c.logger.ErrorContext(ctx, "verification_pending_notification_unrouted_module",
				"tenant_id", tenantID, "item_id", itemID, "module", p.Module, "park_id", parkID,
				"remedy", "add the module to pendingModuleProfiles")
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	verifierDevices, err := c.recipients.ResolveModuleDutyRecipients(ctx, tenantID, scopeCenter, parkID, profile.dutyModule, dutyVerify)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve verifier recipients: %w", err) // retryable
	}
	parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve park head recipients: %w", err)
	}
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, profile.leadershipPosition)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve %s recipients: %w", profile.leadershipPosition, err)
	}
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return fmt.Errorf("verification_notify_consumer: resolve ceo recipients: %w", err)
	}
	verifierRecipients := dedupeQueueRecipients(toQueueRecipients(verifierDevices, "verifier"))
	leadershipRecipients := dedupeQueueRecipients(
		append(append(
			toQueueRecipients(parkHeadDevices, "park_head"),
			toQueueRecipients(directorDevices, profile.leadershipRoleLabel)...),
			toQueueRecipients(ceoDevices, "ceo")...),
	)
	if len(verifierRecipients)+len(leadershipRecipients) == 0 && c.logger != nil {
		c.logger.WarnContext(ctx, "verification_pending_notification_no_recipients",
			"tenant_id", tenantID, "item_id", itemID, "park_id", parkID)
	}

	eventKeySubject := itemID
	if sourceSubmissionID := strings.TrimSpace(p.Source.SubmissionID); sourceSubmissionID != "" {
		eventKeySubject = "submission:" + sourceSubmissionID
	}
	eventKey := EventVerificationItemPending + ":" + eventKeySubject
	animalSummary := strings.TrimSpace(p.SubjectLabel)
	if animalSummary == "" {
		animalSummary = "A shed"
	}
	verifierBody := animalSummary + profile.verifierBodySuffix
	leadershipBody := animalSummary + profile.leadershipBodySuffix
	baseContext := map[string]string{
		"type":         NotificationTypeVerificationPending,
		"item_id":      itemID,
		"park_id":      parkID,
		"shed_id":      p.ShedID,
		"category":     p.Category,
		"subject":      animalSummary,
		"group_key":    "verification:" + parkID + ":" + p.Category,
		"collapse_key": "verification:" + parkID + ":" + p.Category,
		"priority":     priorityNormal,
	}
	if len(verifierRecipients) > 0 {
		verifierContext := cloneContext(baseContext)
		verifierContext["screen"] = "verification"
		verifierContext["target"] = "/verification/items/" + itemID
		verifierContext["message_key"] = profile.messageKeyPrefix + ".proof.pending.verifier"
		_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
			TenantID:         tenantID,
			CalendarEventID:  verificationCalendarEventID(itemID),
			TargetType:       "verification_item",
			TargetID:         itemID,
			NotificationType: NotificationTypeVerificationPending,
			Channel:          channelPushFCM,
			Priority:         priorityNormal,
			Title:            profile.verifierTitle,
			Body:             verifierBody,
			TraceID:          eventKey,
			EventKey:         eventKey,
			Context:          verifierContext,
			Recipients:       verifierRecipients,
		})
		if err != nil {
			return err
		}
	}
	if len(leadershipRecipients) == 0 {
		return nil
	}
	leadershipContext := cloneContext(baseContext)
	leadershipContext["screen"] = profile.leadershipScreen
	leadershipContext["target"] = profile.leadershipTarget
	leadershipContext["message_key"] = profile.messageKeyPrefix + ".proof.pending.leadership"
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: NotificationTypeVerificationPending,
		Channel:          channelPushFCM,
		Priority:         priorityNormal,
		Title:            profile.leadershipTitle,
		Body:             leadershipBody,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context:          leadershipContext,
		Recipients:       leadershipRecipients,
	})
	return err
}

// logUnroutedModule is the shared no-fallback drop used by every lifecycle handler. Dropping is
// deliberate and not retryable: the gap is a missing pendingModuleProfiles entry, not a transient
// failure, and delivering to the vaccination default is exactly the wrong-module defect being
// closed here.
func (c *VerificationEventConsumer) logUnroutedModule(ctx context.Context, event, tenantID, itemID, parkID, module string) {
	if c.logger == nil {
		return
	}
	c.logger.ErrorContext(ctx, event,
		"tenant_id", tenantID, "item_id", itemID, "module", module, "park_id", parkID,
		"remedy", "add the module to pendingModuleProfiles")
}

func cloneContext(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// handleVerdictRework notifies the operator who submitted plus leadership. LEGACY DEDUP: if this
// item is a legacy-handled vaccination SOP item, the legacy vaccination.verify.rejected fan-out
// already notifies the same operator + park head, so the generic path only adds Director/CEO.
func (c *VerificationEventConsumer) handleVerdictRework(ctx context.Context, p VerificationEventPayload) error {
	tenantID := strings.TrimSpace(p.TenantID)
	itemID := strings.TrimSpace(p.ItemID)
	operatorID := strings.TrimSpace(p.OperatorID)
	parkID := strings.TrimSpace(p.ParkID)
	if tenantID == "" || itemID == "" || parkID == "" {
		return nil // Well-formed but non-routable → no-op.
	}
	profile, known := pendingProfileFor(p.Module)
	if !known {
		c.logUnroutedModule(ctx, "verification_rework_notification_unrouted_module", tenantID, itemID, parkID, p.Module)
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recipients []calendarports.NotificationRecipient
	legacyHandled := p.legacyHandledVaccination()
	if operatorID != "" && !legacyHandled {
		operatorDevices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, operatorID)
		if err != nil {
			return err // retryable
		}
		recipients = append(recipients, toQueueRecipients(operatorDevices, "operator")...)
	}
	// Park-head positions are scoped scope_type='center' for a park (see the legacy notifier's
	// identical note): resolve against scopeCenter, never the item's park scope directly.
	if !legacyHandled {
		parkHeadDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, scopeCenter, parkID, positionParkHead)
		if err != nil {
			return err // retryable
		}
		recipients = append(recipients, toQueueRecipients(parkHeadDevices, "park_head")...)
	} else if c.logger != nil {
		c.logger.InfoContext(ctx, "verification_rework_notification_suppressed_legacy_vaccination",
			"tenant_id", tenantID, "item_id", itemID, "park_id", parkID,
			"source_module", p.Source.Module, "source_ref_type", p.Source.RefType,
			"source_task_id", p.Source.TaskID)
	}
	directorDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, profile.leadershipPosition)
	if err != nil {
		return err
	}
	recipients = append(recipients, toQueueRecipients(directorDevices, profile.leadershipRoleLabel)...)
	ceoDevices, err := c.recipients.ResolvePositionRecipients(ctx, tenantID, "tenant", tenantID, positionCEOInternal)
	if err != nil {
		return err
	}
	recipients = append(recipients, toQueueRecipients(ceoDevices, "ceo")...)
	recipients = dedupeQueueRecipients(recipients)

	if len(recipients) == 0 && c.logger != nil {
		c.logger.WarnContext(ctx, "verification_rework_notification_no_recipients",
			"tenant_id", tenantID, "item_id", itemID, "park_id", parkID)
	}

	eventKey := EventVerificationVerdictRework + ":" + itemID
	body := profile.reworkBody
	if p.Reason != "" {
		body = profile.reworkReasonPrefix + p.Reason + profile.reworkReasonSuffix
	}
	// Name WHAT has to be redone. The body used to identify only the module ("a weighing
	// proof"), so an operator holding fifteen bounced captures was told to redo something,
	// somewhere. The item already carries the producing module's own subject sentence --
	// the animal's tag and weight for a weighing capture, the shed/partition for a
	// vaccination one -- and the pending and withdrawn pushes already lead with it. This
	// closes the one lifecycle push that did not.
	if subject := strings.TrimSpace(p.SubjectLabel); subject != "" {
		body = subject + " — " + body
	}
	_, err = c.queue.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         tenantID,
		CalendarEventID:  verificationCalendarEventID(itemID),
		TargetType:       "verification_item",
		TargetID:         itemID,
		NotificationType: NotificationTypeRework,
		Channel:          channelPushFCM,
		Priority:         priorityHigh,
		Title:            profile.reworkTitle,
		Body:             body,
		TraceID:          eventKey,
		EventKey:         eventKey,
		Context: map[string]string{
			"type":         NotificationTypeRework,
			"screen":       profile.reworkScreen,
			"target":       profile.reworkTarget,
			"message_key":  profile.messageKeyPrefix + ".proof.rework",
			"reason":       p.Reason,
			"item_id":      itemID,
			"park_id":      parkID,
			"shed_id":      p.ShedID,
			"category":     p.Category,
			"group_key":    "verification:" + parkID + ":" + p.Category,
			"collapse_key": "verification:" + parkID + ":" + p.Category,
			"priority":     priorityHigh,
		},
		Recipients: recipients,
	})
	return err
}

// verificationCalendarEventID is the calendar_event_id a verification notification_requests row
// links to. There is no FK to enforce against (calendar_event_projections is retired; the FK was
// already dropped in migration 000186), so this is a plain naming convention: distinct from the
// vaccination "calendar:<sop_task_id>" namespace so a generic verification item and its underlying
// SOP task never collide on the same key.
func verificationCalendarEventID(itemID string) string {
	return "verification:" + itemID
}
