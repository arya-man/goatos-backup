// Package domain declares WHICH alerts have a configurable audience and WHO hears each one
// by default (maintainer decision 2026-09-08).
//
// Until this package existed every leadership-facing push named its audience as a literal
// position code in the notifier that sent it. That was correct on the day each was written and
// unchangeable afterwards without a release: "the Park Head should also hear the low-stock
// alert" was a code change. The maintainer's ask is that the audience is CONFIGURABLE, and
// configurable BY DESIGNATION -- the job title -- never by person, because an alert is
// addressed to a desk and whoever holds the desk hears it.
//
// This file is the catalog: one row per configurable alert, carrying the module it belongs
// to, the farm wording an admin reads, and the DEFAULT designations -- exactly the audience
// the code carried before, so deploying the feature changes nobody's phone. The stored
// override (notification_alert_audiences) replaces the default for one alert; its absence IS
// the default.
//
// Two things are deliberately NOT here. Pushes addressed to a PERSON -- the operator whose bag
// was reopened, the packer whose proof was bounced, the park head whose pen visit is due --
// are not designation questions and stay on their member resolvers. And the Slack channel
// posts are addressed to a channel, not a desk.
package domain

import (
	"sort"
	"strings"
)

// Designation codes, matching designation_catalog.designation_code. The catalog is the
// authority for which exist; these constants are the ones the defaults below name.
const (
	DesignationCEO                 = "ceo_internal"
	DesignationPCDirector          = "pc_director"
	DesignationGrowthDirector      = "growth_director"
	DesignationFeedDirector        = "feed_director"
	DesignationHealthDirector      = "health_director"
	DesignationProcurementDirector = "procurement_director"
	DesignationBreedingDirector    = "breeding_director"
	DesignationProcurementManager  = "procurement_manager"
	DesignationParkHead            = "park_head"
	DesignationVerifier            = "verifier"
	DesignationOperator            = "operator"
	// DesignationHR is the per-person `hr` role (maintainer decision 2026-09-10).
	DesignationHR = "hr"
)

// Scope is where a designation's seat lives: a tenant-wide desk (a director) or a park desk (a
// park head). It decides which scope the workforce resolver is asked with.
type Scope string

const (
	ScopeTenant Scope = "tenant"
	ScopePark   Scope = "park"
)

// parkScopedDesignations are the desks that exist once per PARK. Everything else is tenant-wide.
// A park-scoped designation on an alert that carries no park (the daily low-stock run, for
// example) resolves to nobody for that designation rather than guessing a park.
// The verifier is deliberately NOT here: it is a TENANT desk (one verifier reviews proof videos
// across every park -- see the verifier admin-web workspace rule), seated as a tenant role grant.
var parkScopedDesignations = map[string]struct{}{
	DesignationParkHead:           {},
	DesignationOperator:           {},
	DesignationProcurementManager: {},
}

// DesignationScope reports whether a designation is a park desk or a tenant desk.
func DesignationScope(code string) Scope {
	if _, ok := parkScopedDesignations[strings.TrimSpace(code)]; ok {
		return ScopePark
	}
	return ScopeTenant
}

// Module is the farm-facing grouping an alert is listed under.
type Module struct {
	Key   string
	Label string
}

// Modules in display order.
var Modules = []Module{
	{Key: "vaccination", Label: "Vaccination"},
	{Key: "weighing", Label: "Weighing"},
	{Key: "feed", Label: "Feed"},
	{Key: "pc_care", Label: "Preventive Care"},
	{Key: "health", Label: "Health"},
	{Key: "counts", Label: "Herd Operations"},
	{Key: "procurement", Label: "Procurement"},
	{Key: "leadership", Label: "Leadership Tasks"},
	{Key: "leave", Label: "Leave"},
}

// Alert is one configurable notification and its default audience.
type Alert struct {
	// Key is "<module>.<alert>", stable and machine-facing; it is what notifiers ask for and
	// what the stored override is keyed by.
	Key    string
	Module string
	// Label and Blurb are the admin-facing words, rendered verbatim by the matrix screen.
	Label string
	Blurb string
	// DefaultDesignations is the audience when the tenant has not customised the alert. It is
	// listed in the order the notifier used to resolve it, so a recipient list built from the
	// default is byte-for-byte what the notifier produced before this catalog existed.
	DefaultDesignations []string
}

// Alert keys. Each is asked for by exactly the notifier it names; the test in catalog_test.go
// pins that every constant is in the catalog.
const (
	AlertVaccinationDueTodayLeadership = "vaccination.due_today_leadership"
	AlertVaccinationWorkMissed         = "vaccination.work_missed"
	AlertVaccinationDriveReady         = "vaccination.drive_ready"
	AlertVaccinationDriveClosed        = "vaccination.drive_closed"

	AlertWeighingPlanPublished  = "weighing.plan_published"
	AlertWeighingSubmitted      = "weighing.submitted"
	AlertWeighingReopened       = "weighing.reopened"
	AlertWeighingVerdictApprove = "weighing.verdict_approved"
	AlertWeighingVerdictRework  = "weighing.verdict_rework"
	AlertWeighingPenClosed      = "weighing.pen_closed"
	AlertWeighingTaskClosed     = "weighing.task_closed"
	AlertWeighingWorkCadence    = "weighing.work_cadence"

	AlertFeedLowStock           = "feed.low_stock"
	AlertLeadershipTaskRaised   = "leadership.task_raised"
	AlertLeadershipTaskDone     = "leadership.task_done"
	AlertFeedSaleReduce         = "feed.sale_reduce"
	AlertProcurementLoadOverdue = "procurement.load_overdue"
	AlertLeaveRequestRaised     = "leave.request_raised"
	AlertLeaveRequestDecided    = "leave.request_decided"
	AlertAnimalPurchaseDecided  = "procurement.animal_purchase_decided"
)

// Proof-lifecycle alert suffixes. The generic verification vertical is shared by several owning
// modules, and each module's director hears its own proofs; the key is "<module>.<suffix>".
const (
	ProofPendingSuffix  = "proof_pending"
	ProofApprovedSuffix = "proof_approved"
	ProofReworkSuffix   = "proof_rework"
	// ProofReviewSuffix is the VERIFIER's own push -- an ADDRESSED alert (see Resolver.Addressed):
	// the verifier on duty for the park is kept when the Verifier title is ticked; any other ticked
	// title receives a copy.
	ProofReviewSuffix = "proof_review"
)

// DirectorDesignations are the desks a leadership task can be raised from; the task-done push is
// addressed to whichever of them raised it.
var DirectorDesignations = []string{
	DesignationPCDirector, DesignationGrowthDirector, DesignationFeedDirector,
	DesignationHealthDirector, DesignationProcurementDirector, DesignationBreedingDirector,
}

// ProofAlertKey composes the proof-lifecycle key for a verification module ("weighing",
// "feed", ...). An unknown module composes a key the catalog does not carry, and the resolver
// then refuses it -- the same no-fallback rule the verification consumer applies to an
// unclaimed module.
func ProofAlertKey(module, suffix string) string {
	return strings.ToLower(strings.TrimSpace(module)) + "." + suffix
}

// proofModuleDirectors is the director desk that owns each verification module's proofs. It
// mirrors pendingModuleProfiles in notificationbridge, which remains the authority for copy and
// tap routes; this list only carries the DEFAULT audience.
var proofModuleDirectors = []struct {
	module   string
	label    string
	director string
	// group, when set, is the farm-facing Module the rows are listed under when the
	// verification module is a step of a larger one: the pen visit's proofs sit under
	// Preventive Care, whose director owns the pen's whole chain.
	group string
}{
	{module: "vaccination", label: "Vaccination", director: DesignationPCDirector},
	{module: "weighing", label: "Weighing", director: DesignationGrowthDirector},
	{module: "feed", label: "Feed", director: DesignationFeedDirector},
	{module: "pc_care", label: "Preventive Care", director: DesignationPCDirector},
	// The next-day pen visit (maintainer decision 2026-09-12): the last video of a pen's
	// vaccination / care chain, reviewed by the same verifier, owned by the PC Director.
	{module: "pen_visits", label: "Pen visit", director: DesignationPCDirector, group: "pc_care"},
	{module: "health", label: "Health", director: DesignationHealthDirector},
	{module: "counts", label: "Herd Operations", director: DesignationHealthDirector},
}

func proofAlerts() []Alert {
	out := make([]Alert, 0, len(proofModuleDirectors)*3)
	for _, m := range proofModuleDirectors {
		audience := []string{DesignationParkHead, m.director, DesignationCEO}
		group := m.module
		if m.group != "" {
			group = m.group
		}
		out = append(out,
			Alert{
				Key: ProofAlertKey(m.module, ProofPendingSuffix), Module: group,
				Label:               m.label + " proof waiting for review",
				Blurb:               "A proof video was submitted and is waiting for the verifier. The verifier is always told; this is the leadership copy.",
				DefaultDesignations: audience,
			},
			Alert{
				Key: ProofAlertKey(m.module, ProofApprovedSuffix), Module: group,
				Label:               m.label + " proof approved",
				Blurb:               "The verifier accepted a proof video.",
				DefaultDesignations: audience,
			},
			Alert{
				Key: ProofAlertKey(m.module, ProofReviewSuffix), Module: group,
				Label:               m.label + " video waiting for the verifier",
				Blurb:               "The verifier's own push when a proof video arrives for review. Sent to the verifier on duty when Verifier is ticked; any other ticked job title receives a copy.",
				DefaultDesignations: []string{DesignationVerifier},
			},
			Alert{
				Key: ProofAlertKey(m.module, ProofReworkSuffix), Module: group,
				Label:               m.label + " proof sent back",
				Blurb:               "The verifier rejected a proof video and the work must be recorded again. The operator is always told; this is the leadership copy.",
				DefaultDesignations: audience,
			},
		)
	}
	return out
}

// catalog is the full ordered list. Built once; read through Catalog().
var catalog = func() []Alert {
	base := []Alert{
		{
			Key: AlertVaccinationDueTodayLeadership, Module: "vaccination",
			Label:               "Vaccination due-today checkpoint (20:30)",
			Blurb:               "Pens scheduled today that are still not submitted by 20:30. Operators and park managers are always reminded; this is the leadership copy.",
			DefaultDesignations: []string{DesignationPCDirector, DesignationCEO},
		},
		{
			Key: AlertVaccinationWorkMissed, Module: "vaccination",
			Label:               "Vaccination work missed",
			Blurb:               "A scheduled vaccination passed its day without being done. The assigned operator is always told; this is the leadership copy.",
			DefaultDesignations: []string{DesignationParkHead, DesignationPCDirector},
		},
		{
			Key: AlertVaccinationDriveReady, Module: "vaccination",
			Label:               "Vaccination drive fully verified",
			Blurb:               "Every proof video of a drive has been verified and the drive can be closed.",
			DefaultDesignations: []string{DesignationParkHead, DesignationPCDirector, DesignationCEO},
		},
		{
			Key: AlertVaccinationDriveClosed, Module: "vaccination",
			Label:               "Vaccination drive closed",
			Blurb:               "The Preventive Care Director closed a drive.",
			DefaultDesignations: []string{DesignationCEO},
		},
		{
			Key: AlertWeighingPlanPublished, Module: "weighing",
			Label:               "Weighing plan published",
			Blurb:               "A weighing task was planned and its pens assigned. The assigned operator is always told.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertWeighingSubmitted, Module: "weighing",
			Label:               "Weighing pen submitted",
			Blurb:               "An operator submitted a pen's weights and proof.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertWeighingReopened, Module: "weighing",
			Label:               "Weighing pen reopened",
			Blurb:               "A submitted pen was reopened for more scans. The operator is always told.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertWeighingVerdictApprove, Module: "weighing",
			Label:               "Weighing proof approved",
			Blurb:               "The verifier accepted a weighing proof.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertWeighingVerdictRework, Module: "weighing",
			Label:               "Weighing proof sent back",
			Blurb:               "The verifier rejected weighing proof and the pen must be captured again. The operator is always told; this is the upward copy.",
			DefaultDesignations: []string{DesignationGrowthDirector},
		},
		{
			Key: AlertWeighingPenClosed, Module: "weighing",
			Label:               "Weighing pen closed",
			Blurb:               "A pen's weighing was closed, or every one of its proofs was verified.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertWeighingTaskClosed, Module: "weighing",
			Label:               "Weighing task closed",
			Blurb:               "A whole weighing task was closed.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertWeighingWorkCadence, Module: "weighing",
			Label:               "Weighing due today or delayed",
			Blurb:               "Weighing work is due today, or unfinished work rolled over to today. The operator is always told; this is the leadership copy.",
			DefaultDesignations: []string{DesignationGrowthDirector, DesignationCEO},
		},
		{
			Key: AlertFeedLowStock, Module: "feed",
			Label:               "Feed running low",
			Blurb:               "A feed at a park will run out within the alert horizon. Sent once a day per feed while it stays low.",
			DefaultDesignations: []string{DesignationCEO, DesignationFeedDirector, DesignationProcurementDirector},
		},
		{
			Key: AlertFeedSaleReduce, Module: "feed",
			Label:               "Feed to reduce after a sale",
			Blurb:               "Animals were tagged to a sale: the notice on confirmation naming the pens and the feed day, and the reminder on that feed day asking whether the ration was reduced.",
			DefaultDesignations: []string{DesignationFeedDirector},
		},
		{
			Key: AlertLeadershipTaskRaised, Module: "leadership",
			Label:               "Task assigned to you, or changed by its raiser",
			Blurb:               "A task was raised for a CXO, director or park head, or moved by the person who raised it. Sent to the person it is addressed to; any other ticked job title receives a copy.",
			DefaultDesignations: []string{DesignationCEO},
		},
		{
			Key: AlertLeadershipTaskDone, Module: "leadership",
			Label:               "Task status changed by its assignee",
			Blurb:               "The assignee moved a task (doing, done, reopened, cancelled). Sent to the person who raised it when their job title is ticked; any other ticked job title receives a copy.",
			DefaultDesignations: append([]string{}, DirectorDesignations...),
		},
		{
			Key: AlertProcurementLoadOverdue, Module: "procurement",
			Label:               "Load held past 90 days",
			Blurb:               "A purchased load bought more than 90 days ago still holds animals. Sent once a day per load.",
			DefaultDesignations: []string{DesignationCEO},
		},
		{
			Key: AlertLeaveRequestRaised, Module: "leave",
			Label:               "Leave requested",
			Blurb:               "Someone asked for leave from the Clock screen. Sent to the park head of their park and to HR, the two who must approve it; any other ticked job title receives a copy.",
			DefaultDesignations: []string{DesignationParkHead, DesignationHR},
		},
		{
			Key: AlertLeaveRequestDecided, Module: "leave",
			Label:               "Leave approved or rejected",
			Blurb:               "A leave request reached its final answer. Sent to the person who asked; any other ticked job title receives a copy.",
			DefaultDesignations: []string{DesignationOperator},
		},
		{
			Key: AlertAnimalPurchaseDecided, Module: "procurement",
			Label:               "Animal purchase accepted or rejected",
			Blurb:               "The CEO decided on a candidate animal. Sent to the person who recorded it; any other ticked job title receives a copy.",
			DefaultDesignations: []string{DesignationProcurementDirector},
		},
	}
	all := append(base, proofAlerts()...)
	// Stable module-grouped order for the screen: modules in their declared order, alerts in
	// catalog order within a module.
	moduleRank := map[string]int{}
	for i, m := range Modules {
		moduleRank[m.Key] = i
	}
	sort.SliceStable(all, func(i, j int) bool {
		return moduleRank[all[i].Module] < moduleRank[all[j].Module]
	})
	return all
}()

// Catalog returns every configurable alert in display order. The slice is a copy; callers may
// not mutate the catalog.
func Catalog() []Alert {
	out := make([]Alert, len(catalog))
	copy(out, catalog)
	return out
}

// AlertByKey looks one alert up. Unknown keys return ok=false; callers must treat that as a
// wiring defect, never as "no audience".
func AlertByKey(key string) (Alert, bool) {
	key = strings.TrimSpace(key)
	for _, alert := range catalog {
		if alert.Key == key {
			return alert, true
		}
	}
	return Alert{}, false
}

// ModuleLabel returns the farm label for a module key, or the key itself when unknown.
func ModuleLabel(key string) string {
	for _, m := range Modules {
		if m.Key == key {
			return m.Label
		}
	}
	return key
}
