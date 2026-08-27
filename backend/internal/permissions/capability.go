package permissions

import "sort"

// Per-person module access (maintainer decision 2026-08-24).
//
// Access used to be DERIVED: a person carried a role + department + park, and four
// separate mechanisms turned those into modules and permissions. That model had already
// broken in production -- STG carries four people wearing stacked job titles (one wears
// FIVE), a department literally named after an individual (`avishek_health_access`), and
// a duplicated roster row for the same human. Those are all workarounds for the same
// missing thing: no way to say "this person, this module, this much authority".
//
// So access becomes ASSIGNED. A person is granted, per SURFACE (web / mobile), a LEVEL on
// each MODULE. This file is the single place that says what a level MEANS in permissions.
//
// Three properties this file must keep, because each one is load-bearing:
//
//  1. LEVELS ARE NOT A CUMULATIVE LADDER. Each level authors its FULL permission set. It
//     is tempting to make `oversee` mean `do` + oversight verbs, and that is exactly wrong
//     for Feed: the Feed Director reads every page of the feed chain and deliberately
//     CANNOT record a transport task as done (permissions.FeedDirectionComplete is absent
//     from RoleFeedDirector on purpose, with a comment saying so). A cumulative ladder
//     would silently hand him that authority the first time someone set him to `oversee`.
//     Authoring full sets keeps the asymmetry expressible and auditable.
//
//  2. THE CATALOG IS THE CONTRACT, NOT THE UI. The admin-web screen renders levels the
//     backend declares; it never maps a level to permissions itself. A client that could
//     name its own permissions could grant itself any of them.
//
//  3. PARITY IS THE ACCEPTANCE TEST. The one-time backfill must reproduce every existing
//     person's CURRENT effective permission set. capability_parity_test.go compares this
//     catalog's output against rolePermissions for every role and reports the diff; a
//     difference is either encoded here or accepted in writing. Nobody gains or loses
//     authority on cutover day by accident.
const (
	// SurfaceWeb is admin-web. SurfaceMobile is the Android app. A person may hold a
	// DIFFERENT level on the same module per surface -- a director who approves at a desk
	// and only glances on the phone is the normal case, not an exception.
	SurfaceWeb    = "web"
	SurfaceMobile = "mobile"
)

// Levels, ordered least to most authority for DISPLAY. The order is a UI affordance only;
// it does NOT imply set inclusion (see property 1 above).
const (
	// LevelNone is absence: the module is not in the person's menu and grants nothing.
	// Stored explicitly rather than as a missing row so "deliberately removed" and "never
	// considered" stay distinguishable in the audit trail.
	LevelNone = "none"
	// LevelView sees the module's screens and cards and can open nothing that changes state.
	LevelView = "view"
	// LevelDo performs the module's own field work -- execute, complete, record.
	LevelDo = "do"
	// LevelOversee judges other people's work -- verify, approve, reassign, oversee.
	// Deliberately NOT a superset of LevelDo: overseeing work and doing it are different
	// jobs, and for Feed they are separated on purpose.
	LevelOversee = "oversee"
	// LevelConfigure authors the standing rules the module operates under -- publish a
	// protocol, write a ration, plan a campaign.
	LevelConfigure = "configure"
)

// LevelOrder is the display order for the level picker.
var LevelOrder = []string{LevelNone, LevelView, LevelDo, LevelOversee, LevelConfigure}

// CapabilityCopy is the farm wording for one capability, rendered verbatim by the access
// editor. The client never composes these -- "oversee" is not a word to show an admin.
type CapabilityCopy struct {
	Level string
	Label string
	Blurb string
}

// CapabilityVocabulary is the ordered capability list the editor renders, LevelNone
// excluded: removing access is unticking everything, not a fifth chip to choose.
var CapabilityVocabulary = []CapabilityCopy{
	{Level: LevelView, Label: "View", Blurb: "Can open the screens and read them. Cannot change anything."},
	{Level: LevelDo, Label: "Do", Blurb: "Carries out the work: records, captures, completes."},
	{Level: LevelOversee, Label: "Oversee", Blurb: "Judges other people's work: approve, verify, send back."},
	{Level: LevelConfigure, Label: "Set up", Blurb: "Authors the standing rules the work follows, and plans it."},
}

// ModuleCapability declares one module's levels. A level absent from Levels is not
// offerable for that module -- Sales has no `configure`, so the picker must not show one.
type ModuleCapability struct {
	// Key is the stable module id, shared with the mobile module registry
	// (workforce/app.moduleNavRegistry) and department_module_grants.module_key.
	Key string
	// Label is the FARM word for this module, rendered verbatim by the access editor.
	// It lives here, beside the permissions, for the same reason the mobile module
	// registry keeps its labels in Go: the client must never invent a name for a module,
	// and a raw key like "aas_health" must never reach a screen.
	Label string
	// Blurb is one plain sentence about what the module covers, shown under the label so
	// whoever assigns access does not have to guess what they are granting.
	Blurb string
	// Surfaces are the surfaces this module exists on at all. Feed Config is web-only;
	// pc_care execution is phone-first. Offering a level on a surface the module does not
	// have would grant permissions behind a screen that does not exist.
	Surfaces []string
	// Levels maps a level to the COMPLETE permission set it grants. Never partial, never
	// inherited from a lower level.
	Levels map[string][]string
}

// moduleCapabilities is the catalog. Adding a module here is what makes it assignable;
// there is deliberately no fallback for an unknown module key, so a typo grants nothing
// rather than something unintended.
var moduleCapabilities = []ModuleCapability{
	{
		Key:      "vaccination",
		Label:    "Vaccination",
		Blurb:    "Drives, doses and the vaccination schedule.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {VaccinationRead, VaccinationOverviewRead, VaccinationAlertsRead, ObligationRead, ProtocolRead},
			LevelDo:   {VaccinationRead, VaccinationOverviewRead, VaccinationAlertsRead, ObligationRead, ProtocolRead, TaskExecute},
			// Supervising a park's vaccination execution: watch the drive, sign off task work.
			// NO TaskAssign -- handing out today's work is roster authority and lives on the
			// people module, so a supervisor with no team cannot assign work.
			LevelOversee: {
				VaccinationRead, VaccinationOverviewRead, VaccinationAlertsRead, ObligationRead, ProtocolRead,
				VaccinationOverseeExecution, TaskVerify,
			},
			// Authoring the drive itself, which is what makes this the top level.
			LevelConfigure: {
				VaccinationRead, VaccinationOverviewRead, VaccinationAlertsRead, ObligationRead, ProtocolRead,
				VaccinationOverseeExecution, TaskVerify, VaccinationVerify, VaccinationCampaign,
			},
		},
	},
	{
		Key:      "weighing",
		Label:    "Weighing",
		Blurb:    "Weighing sessions and the weights board.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {WeighingMonitor},
			LevelDo:   {WeighingMonitor, WeighingExecute},
			// Browsing other people's weighing work. Deliberately WITHOUT WeighingExecute:
			// overseeing the operators and holding the scanner are different jobs.
			LevelOversee: {WeighingMonitor, WeighingOverseeOperators},
			// RAISING the task. CEO-only today (maintainer decision 2026-08-01) and deliberately
			// WITHOUT Execute: the CEO plans weighing and never carries it out. A cumulative
			// ladder would have handed the CEO the scanner.
			LevelConfigure: {WeighingMonitor, WeighingPlan},
		},
	},
	{
		Key:      "counts",
		Label:    "Herd Operations",
		Blurb:    "Births, deaths and shifting animals between pens.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			// Alerts only. The Counts SCREENS are counts.read, which sits at LevelDo with the
			// capture write: Counts is a deliberately OFF feature held back by exactly
			// counts.read / counts.write, and the Health Director is its declared OWNER while
			// holding neither (AGENTS.md -- ownership is not access). LevelView is what that
			// ownership looks like: he is notified, and the module stays dark.
			LevelView: {CountsAlertsRead},
			// Phone capture: record a birth, a death, a shifting. Deliberately WITHOUT
			// counts.read -- that is the admin-web Counts screens, which is a different
			// authority and the one actually holding this OFF feature closed.
			LevelDo: {CountsAlertsRead, CountsWrite},
			// The three approve_* permissions travel together: they are one job (deciding a
			// raised birth / death / shifting), and splitting them would let someone approve a
			// death but not the shifting it implies.
			//
			// Deliberately NO CountsRead/CountsAlertsRead here. The retired `counts_approver`
			// role carries approval authority and NOTHING else -- no bootstrap, no read, no
			// write (maintainer decision 2026-08-05, granted BY NAME to individuals). Folding
			// the reads in would hand every named approver the Counts screens, and Counts is a
			// deliberately OFF feature held back by exactly counts.read / counts.write.
			// Someone who needs both ticks View as well.
			LevelOversee: {CountsApproveLifecycle, CountsApproveShifting, CountsApproveAccess},
			// The admin-web Counts screens (counts.read). Held by ceo_internal alone today; this
			// is the tick that would switch the feature on, so it sits at the top level.
			LevelConfigure: {CountsAlertsRead, CountsRead, CountsWrite},
		},
	},
	{
		Key:      "feed_direction",
		Label:    "Feed",
		Blurb:    "The daily feed sheet, packing, transport and distribution.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {FeedDirectionRead, FeedPackingRead, FeedWastageRead, FeedTransportRead},
			LevelDo:   {FeedDirectionRead, FeedPackingRead, FeedWastageRead, FeedTransportRead, FeedDirectionComplete},
			// THE HEMANT CASE, and the reason levels are not cumulative. Oversee reads every
			// page of the feed chain and adds FeedDirectionOversee -- and deliberately does NOT
			// carry FeedDirectionComplete. The director sees the daily transport tasks and still
			// cannot record one as done. Adding Complete here reverses a recorded decision.
			LevelOversee: {FeedDirectionRead, FeedPackingRead, FeedWastageRead, FeedTransportRead, FeedDirectionOversee},
			LevelConfigure: {
				FeedDirectionRead, FeedPackingRead, FeedWastageRead, FeedTransportRead, FeedDirectionOversee,
				FeedConfigRead, FeedConfigWrite,
			},
		},
	},
	{
		// The roadmap teaser. `breeding` is a `soon` module: it renders as a disabled drawer
		// row and opens nothing, so it carries NO permissions at any level. It needs a
		// catalog entry all the same, because the phone offers a soon module only when the
		// principal is offered its key -- and once the bar reads ticks, an untickable module
		// is an unofferable one. Without this the CEO lost the Breeding row.
		Key:      "breeding",
		Label:    "Breeding",
		Blurb:    "Coming soon. Nothing to open yet.",
		Surfaces: []string{SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {},
		},
	},
	{
		// The birth / death / shifting approval queue. Its OWN module rather than a tab
		// inside Herd Operations (maintainer decision 2026-08-05): approving is not
		// capturing, and the audiences barely overlap -- the two named approvers hold no
		// counts.write, and operators hold no approval authority.
		//
		// It carries ONLY the three approve permissions, matching the retired
		// `counts_approver` role that is granted BY NAME to individuals: no bootstrap, no
		// read, no write. The three travel together because they are one job -- deciding a
		// raised birth, death or shifting -- and splitting them would let someone approve a
		// death but not the shifting it implies.
		Key:      "approvals",
		Label:    "Approvals",
		Blurb:    "Deciding the birth, death and shifting requests raised from the field.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelOversee: {CountsApproveAccess, CountsApproveLifecycle, CountsApproveShifting},
		},
	},
	{
		// The kid-milk round: prepare the feed, then give it. Its OWN module rather than a
		// level on Herd Operations (maintainer decision 2026-07-31, which split it out of
		// Counts on the phone and gave it its own admin-web group): Counts owns the
		// herd-register EVENTS -- birth, death, shifting -- while the milk round is a daily
		// operational routine sharing neither their grain nor their read models.
		//
		// The permissions are deliberately the SAME ones the routes already require
		// (counts.read for the admin-web read, counts.write for the phone capture): a drawer
		// regrouping must not silently widen or narrow who may write.
		Key:      "milk",
		Label:    "Milk",
		Blurb:    "The daily kid-milk round: preparing the feed and giving it.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			// NO LevelView, deliberately, and the parity test is what forced it: counts.read
			// is the permission holding the Counts feature OFF (ceo_internal alone holds it),
			// so a `view` level carrying it would hand the whole Counts console to every
			// operator and park head the moment they were given the milk round.
			//
			// LevelDo is the phone round -- /app/counts/milk-preparation requires exactly
			// counts.write, and no read. LevelConfigure adds the admin-web read, which is the
			// same tick that switches Counts on, so it sits at the top level for the same
			// reason it does there.
			LevelDo:        {CountsWrite},
			LevelConfigure: {CountsRead, CountsWrite},
		},
	},
	{
		Key:      "aas_health",
		Label:    "Health",
		Blurb:    "Sick-goat reports, diagnosis and treatment courses.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			// Reading the animal's health and RAISING a sick-goat report -- field work every
			// tier does, including one that never carries out a course.
			LevelView: {HealthRead, HealthReport},
			// Carrying out the prescribed course.
			LevelDo: {HealthRead, HealthReport, HealthExecute},
			// Diagnosing is a clinical judgement, and writing a health fact onto an animal
			// follows it. Executing a course someone else prescribed does not.
			// Diagnosing is a clinical judgement; executing a course someone else prescribed is
			// not. Recording the health fact ON the animal is herd_register's LevelDo, because a
			// non-clinical role (the Growth Director) holds that write with no health read.
			LevelOversee: {HealthRead, HealthReport, HealthDiagnose},
			// Authoring the standing treatment rulebook (/health/config). Versioned, never
			// edited in place -- see docs/decisions/health-config-authoring.md.
			LevelConfigure: {HealthRead, HealthDiagnose, HealthConfigRead, HealthConfigWrite},
		},
	},
	{
		Key:      "pc_care",
		Label:    "Preventive Care",
		Blurb:    "Deworming, hoof and hair trimming, tick control.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView:    {PCCareMonitor},
			LevelDo:      {PCCareMonitor, PCCareExecute},
			LevelOversee: {PCCareMonitor, PCCareOverseeOperators},
			// Planning without executing, exactly as weighing above.
			LevelConfigure: {PCCareMonitor, PCCarePlan},
		},
	},
	{
		Key:   "procurement",
		Label: "Procurement",
		Blurb: "Source entry: animals bought in.",
		// Mobile too: the operator records a source entry on the phone, so a web-only
		// procurement module silently dropped procurement.read/write for every operator.
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView:    {ProcurementRead},
			LevelDo:      {ProcurementRead, ProcurementWrite},
			LevelOversee: {ProcurementRead, ProcurementReview},
		},
	},
	{
		// Vendors are a SEPARATE module from source entry, and the split is not cosmetic:
		// a park head reviews procurement at his park and has no vendor access at all, while
		// a procurement manager runs the vendor desk and records no source entry. Bundling
		// them forced one to gain the other's authority on cutover.
		Key:      "vendors",
		Label:    "Vendors",
		Blurb:    "The vendor list and what each one is paid.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			LevelView: {VendorRead},
			LevelDo:   {VendorRead, VendorWrite},
			// What a vendor costs is a finance read, held apart from editing the vendor record.
			LevelOversee: {VendorRead, VendorWrite, VendorFinanceRead},
		},
	},
	{
		Key:      "sales",
		Label:    "Sales",
		Blurb:    "Animals sold and the sales ledger.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			LevelView: {SalesRead},
			LevelDo:   {SalesRead, SalesWrite},
		},
	},
	{
		Key:      "verification",
		Label:    "Video Verification",
		Blurb:    "Reviewing the proof videos operators record.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			// Leadership keeps the READ (see the queue, the media, the recorded verdicts) without
			// the verdict itself -- maintainer decision 2026-08-03, separation of duty.
			LevelView: {VerificationReview},
			// SEPARATION-OF-DUTY BOUNDARY. verification.verdict belongs to the verifier alone;
			// an independent check the checked party can sign is not independent. Under the old
			// role model this was structurally impossible to grant to leadership. It is now a
			// tick, so the admin-web screen MUST warn when it is combined with LevelOversee or
			// LevelConfigure on a module the same person executes. See PermissionSeparationRisk.
			// THE VERIFIER. Casting the verdict comes with the evidence tools needed to judge
			// it -- the timeline and the capture-date filter.
			LevelDo: {
				VerificationReview, VerificationVerdict,
				VerificationEvidenceTimeline, VerificationFilterByCaptureDate,
				// The older per-vaccination verify and the shared task sign-off travel with the
				// verdict: they are the same act on a different record.
				VaccinationVerify, TaskVerify,
			},
			// A module director acting on a verdict someone else cast: close the work, send it
			// back, reassign it. Deliberately WITHOUT VerificationOversee -- the company-wide
			// module/capture-date filters are a separate authority (STG incident 2026-08-12,
			// where those filters rendered for every role that could open the page).
			LevelOversee: {VerificationReview, VerificationAct},
			// Company-wide oversight: the filters, the timeline, every module's queue.
			LevelConfigure: {
				VerificationReview, VerificationAct, VerificationOversee,
				VerificationFilterByCaptureDate, VerificationEvidenceTimeline,
			},
		},
	},
	{
		Key:      "people",
		Label:    "People",
		Blurb:    "The staff directory, rosters, devices and access.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {OperatorsRead, RosterRead},
			// Running a team: manage the roster and hand out today's work.
			LevelDo: {OperatorsRead, RosterRead, OperatorsManageRoster, RosterManage, TaskAssign},
			// Issuing or reclaiming a person's device, and reading their audit trail.
			LevelOversee: {
				OperatorsRead, RosterRead, OperatorsManageRoster, RosterManage, TaskAssign,
				OperatorsManageDevice, OperatorsViewAudit,
			},
			// Creating people, activating/deactivating them, and changing what they may do IS
			// this screen's own authority. It is the top level deliberately: whoever holds it can
			// grant everything else in this catalog.
			LevelConfigure: {
				OperatorsRead, RosterRead, OperatorsManageRoster, OperatorsManageDevice, RosterManage, TaskAssign,
				OperatorsViewAudit, OperatorsWrite, OperatorsActivate, OperatorsDeactivate,
				OperatorsManageCapability,
			},
		},
	},
	{
		Key:   "config",
		Label: "Protocols & SOPs",
		Blurb: "The standing rules and written procedures work follows.",
		// Mobile too: a park head reads the SOP on the phone while running the work.
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {ProtocolRead, SOPRead},
			LevelDo:   {ProtocolRead, SOPRead, SOPWrite},
			LevelConfigure: {
				ProtocolRead, SOPRead, SOPWrite, SOPPublish, ProtocolWrite, ProtocolPublish,
			},
		},
	},
	{
		Key:      "herd_register",
		Label:    "Herd Register",
		Blurb:    "Individual animals: tags, breed, sex and health facts.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {GoatRead},
			// Recording a health fact about one animal. Held by roles with no health-module
			// access at all (the Growth Director), which is why it lives here and not there.
			LevelDo: {GoatRead, GoatWriteHealth},
			// Editing the animal's identity -- tags, breed, sex.
			LevelOversee: {GoatRead, GoatWriteIdentity},
			// Reclassifying an animal's shed stage rewrites where the herd thinks it sits, so it
			// sits above ordinary identity edits (today: ceo_internal alone).
			LevelConfigure: {GoatRead, GoatWriteIdentity, GoatReclassifyShedStage},
		},
	},
	{
		Key:      "locations",
		Label:    "Parks & Sheds",
		Blurb:    "The park, shed and pen directory.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			LevelView: {LocationsRead},
			LevelDo:   {LocationsRead, LocationsWrite},
			// Reviewing a proposed location WITHOUT editing or retiring one -- the verifier's
			// shape. Bundling these handed a verifier the power to retire a shed.
			LevelOversee:   {LocationsRead, LocationsReview},
			LevelConfigure: {LocationsRead, LocationsWrite, LocationsReview, LocationsRetire},
		},
	},
	{
		Key:      "calendar",
		Label:    "Calendar",
		Blurb:    "The planned work calendar across every module.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {CalendarRead},
			LevelDo:   {CalendarRead, CalendarAction},
		},
	},
	{
		// Aflatoxin strip testing on a purchased feed load (maintainer decisions 2026-08-25
		// and 2026-08-26).
		Key:      "toxin",
		Label:    "Toxin Testing",
		Blurb:    "The aflatoxin strip test every purchased feed load owes.",
		Surfaces: []string{SurfaceWeb, SurfaceMobile},
		Levels: map[string][]string{
			LevelView: {ToxinRead},
			// Running the 7-step test. The named park heads hold this.
			LevelDo: {ToxinRead, ToxinExecute},
			// Judging the result. Deliberately WITHOUT ToxinExecute -- the CEO/CXO watches and
			// judges but never runs a test, because someone who could film the steps would be
			// approving their own evidence (maintainer decision 2026-08-26, correcting the
			// 2026-08-25 grant). This is the second place in this catalog where a higher level
			// is NOT a superset, and it is a separation of duty rather than an oversight.
			LevelOversee: {ToxinRead, ToxinVerdict},
		},
	},
	{
		// The feed purchase ledger. DEDICATED permissions, never a reuse of ProcurementRead --
		// operator and park_head hold that for the source-entry screens they work, and this
		// ledger carries supplier prices and payment state (maintainer decision 2026-08-24).
		Key:      "feed_purchases",
		Label:    "Feed Purchases",
		Blurb:    "Feed bought in: supplier, quantity, price and payment.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			// The Feed Director holds READ ONLY: he owns what the farm feeds and is accountable
			// for the stock cards these loads are counted from, but buying is the procurement desk's job.
			LevelView: {FeedPurchaseRead},
			LevelDo:   {FeedPurchaseRead, FeedPurchaseWrite},
		},
	},
	{
		// Setting what SHARE of proof videos a verifier must actually watch. Its own module
		// rather than a level on `verification`, because it is a different authority: oversight
		// WATCHES the verification workload, while this DECIDES how much of it a human must
		// watch. The PC Director holds VerificationOversee and must NOT hold this -- a director
		// setting the depth of the check on his own department's work is the separation of duty
		// that keeps the verdict off leadership (maintainer decision 2026-08-26). Folding it into
		// the verification module's top level would have handed it to him.
		Key:      "verification_policy",
		Label:    "Review Sampling",
		Blurb:    "How much of each kind of proof video gets watched.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			LevelConfigure: {VerificationSampling},
		},
	},
	{
		// Sensor/collar signal ingestion and its mapping to animals. Small and CEO-only today,
		// but it must exist as a module: a permission no level grants is unassignable, and the
		// route requiring it becomes dead to everyone.
		Key:      "herd_signals",
		Label:    "Herd Signals",
		Blurb:    "Collar and sensor readings from the herd.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			LevelView:      {HerdSignalsRead},
			LevelDo:        {HerdSignalsRead, HerdSignalsIngest},
			LevelConfigure: {HerdSignalsRead, HerdSignalsIngest, HerdSignalsMap},
		},
	},
	{
		Key:      "operations",
		Label:    "System Repair",
		Blurb:    "Replaying failed background work. Engineering use.",
		Surfaces: []string{SurfaceWeb},
		Levels: map[string][]string{
			LevelView: {},
			// Replaying a dead-lettered message is a repair action on the event spine.
			LevelOversee: {OperationsRepair},
		},
	},
}

// moduleCapabilityIndex is built once at init; lookups are hot (every request resolves a
// principal's permissions) and a linear scan of the catalog per module row is wasteful.
var moduleCapabilityIndex = func() map[string]ModuleCapability {
	out := make(map[string]ModuleCapability, len(moduleCapabilities))
	for _, mod := range moduleCapabilities {
		if _, dup := out[mod.Key]; dup {
			// A duplicate key would SILENTLY shadow the earlier entry and quietly change what a
			// level grants. Fail at startup instead.
			panic("permissions: duplicate module capability key " + mod.Key)
		}
		out[mod.Key] = mod
	}
	return out
}()

// ModuleCapabilities returns the catalog for contract compilation. The admin-web access
// editor renders modules, surfaces, and offerable levels from THIS, never from a frontend
// constant list -- the backend owns the vocabulary.
func ModuleCapabilities() []ModuleCapability {
	out := make([]ModuleCapability, len(moduleCapabilities))
	copy(out, moduleCapabilities)
	return out
}

// LookupModuleCapability reports the catalog entry for a module key.
func LookupModuleCapability(moduleKey string) (ModuleCapability, bool) {
	mod, ok := moduleCapabilityIndex[moduleKey]
	return mod, ok
}

// ModuleSupportsSurface reports whether a module exists on a surface at all.
func ModuleSupportsSurface(moduleKey, surface string) bool {
	mod, ok := moduleCapabilityIndex[moduleKey]
	if !ok {
		return false
	}
	for _, s := range mod.Surfaces {
		if s == surface {
			return true
		}
	}
	return false
}

// LevelOffered reports whether a module offers a level. LevelNone is always offerable --
// removing access must never be blocked by the catalog.
func LevelOffered(moduleKey, level string) bool {
	if level == LevelNone {
		return true
	}
	mod, ok := moduleCapabilityIndex[moduleKey]
	if !ok {
		return false
	}
	_, offered := mod.Levels[level]
	return offered
}

// ModuleAssignment is one saved row of a person's access: what they hold on one module,
// on one surface.
//
// Capabilities is a SET, not a single level, and that is forced by the real roster rather
// than chosen for flexibility. Dinakar captures herd-operation counts AND approves them --
// today he wears `operator` (counts.write) and `counts_approver` (approve only) at the same
// time. The Assistant Manager tier does the same by construction. A single-select ladder
// cannot say "does the work AND signs off on it", and making a higher level imply the lower
// ones would hand the Feed Director FeedDirectionComplete (see the catalog comment). A set
// says both things exactly, and reads on screen as "read some, write some".
type ModuleAssignment struct {
	Module  string
	Surface string
	// Capabilities are the levels held on this module/surface. Empty means no access -- the
	// stored row is kept so "deliberately removed" stays distinguishable from "never set".
	Capabilities []string
	// Pages narrows this row to specific admin-web screens of the module (page-grain
	// access, maintainer decision 2026-08-27). EMPTY MEANS EVERY PAGE, so a page shipped
	// tomorrow reaches whoever already holds the module instead of nobody. Web-only: the
	// phone builds its own navigation and ignores this field. See capability_pages.go.
	Pages []string
}

// HasCapability reports whether the row carries a capability.
func (a ModuleAssignment) HasCapability(level string) bool {
	for _, c := range a.Capabilities {
		if c == level {
			return true
		}
	}
	return false
}

// surfaceBootstrap is the permission that admits a principal to a surface at all. It is
// DERIVED, never assigned: holding at least one real module on a surface is what makes the
// surface reachable, so a person can never be left with modules they cannot log in to see,
// nor with a login that opens onto nothing.
var surfaceBootstrap = map[string]string{
	SurfaceWeb:    AdminWebBootstrap,
	SurfaceMobile: AppBootstrap,
}

// PermissionsForAssignments resolves a person's stored access rows into the flat permission
// set the route layer already checks. This is the ONE seam the per-person model needed: the
// ~95 authorization call sites keep asking "does this principal hold feed_direction.read",
// and only the source of the answer moved -- from a hardcoded role map to this function.
//
// Unknown modules, unknown surfaces, unoffered levels, and LevelNone all contribute NOTHING.
// A row the catalog does not understand must never widen access; the failure mode of a typo
// or a stale row is missing access, which is visible and reported, not silent authority.
func PermissionsForAssignments(assignments []ModuleAssignment) []string {
	set := make(map[string]struct{}, 32)
	surfacesInUse := make(map[string]struct{}, 2)

	for _, a := range assignments {
		if _, known := surfaceBootstrap[a.Surface]; !known {
			continue
		}
		mod, ok := moduleCapabilityIndex[a.Module]
		if !ok {
			continue
		}
		if !ModuleSupportsSurface(a.Module, a.Surface) {
			continue
		}
		for _, level := range a.Capabilities {
			if level == LevelNone || level == "" {
				continue
			}
			perms, offered := mod.Levels[level]
			if !offered {
				continue
			}
			// A capability granting no permissions (operations at view) still counts as real
			// access for the surface-bootstrap derivation below: the person was deliberately
			// given the screen, and the screen must open.
			surfacesInUse[a.Surface] = struct{}{}
			for _, p := range perms {
				set[p] = struct{}{}
			}
		}
	}

	for surface := range surfacesInUse {
		set[surfaceBootstrap[surface]] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	// Stable order: callers compare, log, and diff these sets (the backfill parity check
	// most of all), and Go's map iteration would make an identical set look different on
	// two reads.
	sort.Strings(out)
	return out
}

// PermissionSeparationRisk names a pair whose combination breaks a separation-of-duty rule
// that the retired role model enforced structurally. Under per-person assignment these
// become reachable by ticking, so the assignment screen must warn -- and the maintainer
// must decide deliberately rather than discover it later.
type PermissionSeparationRisk struct {
	// Module and Level are what the person is being given.
	Module string
	Level  string
	// ConflictsWithModule is what they already hold that makes the combination a risk.
	ConflictsWithModule string
	// Reason is farm-readable copy explaining the conflict, rendered verbatim.
	Reason string
}

// executableModules are modules whose LevelDo means "this person performs the field work"
// -- the work a verifier's verdict judges. Holding the verdict over your own work is the
// separation-of-duty break this guards.
var executableModules = map[string]string{
	"vaccination":    "vaccination",
	"weighing":       "weighing",
	"feed_direction": "feed",
	"counts":         "herd operations",
	"aas_health":     "health",
	"pc_care":        "preventive care",
}

// SeparationRisks reports the separation-of-duty warnings for a proposed access set. It
// does NOT block the save -- the maintainer's recorded position is that this authority is
// theirs to grant -- but an unwarned grant of it would be an accident, and this is exactly
// the accident the role model used to make impossible.
func SeparationRisks(assignments []ModuleAssignment) []PermissionSeparationRisk {
	holdsVerdict := false
	for _, a := range assignments {
		if a.Module == "verification" && a.HasCapability(LevelDo) {
			holdsVerdict = true
			break
		}
	}
	if !holdsVerdict {
		return nil
	}
	seen := make(map[string]struct{}, len(assignments))
	out := make([]PermissionSeparationRisk, 0, len(assignments))
	for _, a := range assignments {
		if !a.HasCapability(LevelDo) {
			continue
		}
		label, executable := executableModules[a.Module]
		if !executable {
			continue
		}
		if _, dup := seen[a.Module]; dup {
			continue
		}
		seen[a.Module] = struct{}{}
		out = append(out, PermissionSeparationRisk{
			Module:              "verification",
			Level:               LevelDo,
			ConflictsWithModule: a.Module,
			Reason: "This person would approve and reject proof for " + label +
				" work they carry out themselves. Verification is meant to be a second pair of eyes.",
		})
	}
	// Deterministic order: the warning list is rendered to a human, and the caller's
	// assignment slice order is not guaranteed stable across two loads of the same person.
	sort.Slice(out, func(i, j int) bool {
		return out[i].ConflictsWithModule < out[j].ConflictsWithModule
	})
	return out
}
