package permissions

import (
	"context"
	"sync"
)

const (
	RoleVerifier   = "verifier"
	RoleParkHead   = "park_head"
	RolePCDirector = "pc_director"
	// RoleGrowthDirector is the concrete grant key for the Growth Director business role.
	// Business role keys use name_director order (`growth_director`), matching `pc_director`.
	RoleGrowthDirector = "growth_director"
	// RoleFeedDirector is the concrete grant key for the Feed Director business role
	// (wiki/Handbooks/Feed_Director.pdf, ROLE PURPOSE: "owns the full feed chain: from
	// procuring and stocking feed and UHT milk, to directing consumption, monitoring
	// wastage, updating data, and verifying that ground-level feeding SOPs are actually
	// being followed"). Maintainer decision 2026-08-01: Feed -> feed_director.
	RoleFeedDirector = "feed_director"
	// RoleHealthDirector is the concrete grant key for the Health Director business role.
	//
	// It is a DISTINCT role from RolePCDirector, and merging them is prohibited: the org
	// model lists "Preventive Care — Vaccination, biosecurity & routine health protocols"
	// and "Health — Diagnosis, treatment & veterinary care" as separate departments
	// (wiki/Handbooks/Mesha-dept-directors.pdf, DEPARTMENTS grid; corroborated by
	// COO.pdf "VERTICALS UNDER COO").
	//
	// Counts ownership is a MAINTAINER EXTENSION, not a handbook fact: no handbook assigns
	// census/counting/headcount to anyone. The nearest written anchor is Health_Director.pdf
	// Responsibility 6 (Tagging: ear tag/RFID at birth, purchase, and re-tag — the identity
	// substrate a census sits on) and Responsibility 9 (death assessment). Recorded as a
	// decision in AGENTS.md and docs/runbooks/current-active-rbac-roles.md.
	RoleHealthDirector = "health_director"
	// RoleCountsApprover is a PER-PERSON authority grant, not a job. It carries exactly the
	// three Counts approval permissions and nothing else, so it can be handed to a NAMED
	// individual without widening the job role that person also holds.
	//
	// Maintainer decision 2026-08-05: the birth/death/shifting approval queue returns to the
	// phone (superseding the 2026-07-21 "approvals live on admin-web only" decision), and the
	// approvers are the CEO/CXO cohort plus two specific directors -- explicitly "rbac per
	// person, not per group".
	//
	// Why a narrow role rather than adding the permissions to pc_director/growth_director:
	// permissions in this package resolve from a caller's ROLES, and a caller holds every role
	// on their active user_scope_grants rows (see RolesAuthorize + httpmiddleware.routeRoles).
	// So granting THIS role to two named people gives exactly those two people approval
	// authority, while pc_director and growth_director keep their published definitions --
	// "Preventive Care only" and "Weighing and ONLY Weighing". A future PC Director inherits
	// no approval power by holding the job; someone must grant them this role by name. That
	// keeps the one-module-one-director segregation intact (director_module_segregation_test.go)
	// instead of quietly reversing it for everyone who ever holds those jobs.
	//
	// It grants no bootstrap, no read, and no write: a holder must already have AppBootstrap /
	// AdminWebBootstrap from their real job role to have anywhere to render this. A grant of
	// this role ALONE authorizes reaching the approvals routes and nothing else, which is the
	// intended fail-closed shape.
	RoleCountsApprover = "counts_approver"
	RoleOperator       = "operator"
	RoleCEOInternal    = "ceo_internal"
	// RoleProcurementManager runs the vendor register.
	//
	// Unlike RoleCountsApprover, this IS a job rather than a per-person authority: running the
	// procurement desk is somebody's role, and a future holder of that desk SHOULD inherit the
	// register. So the authority is attached by granting a named person THIS role, never by adding
	// vendor.* to an unrelated director job -- which would widen it to every future holder of that
	// job and reverse the one-module-one-director segregation lock.
	//
	// Catalog row: migration 000156 (tier 'manager', vertical 'procurement').
	RoleProcurementManager = "procurement_manager"

	GoatRead          = "goat.read"
	GoatWriteIdentity = "goat.write_identity"
	GoatWriteHealth   = "goat.write_health"
	// GoatReclassifyShedStage authorizes the whole-pen cohort reclassification behind the Counts
	// screen's "Change stage" action: retag every live animal in one shed+partition at once.
	//
	// It is DELIBERATELY separate from GoatWriteIdentity, which retags ONE animal against a
	// row_version the caller had to read first. This one retags a whole pen from a picker, applies
	// immediately with no approval and no proof, and moves every affected animal's vaccination
	// schedule by flipping kid/adult. Folding it into GoatWriteIdentity would hand that to every
	// holder of the ordinary goat write -- including park_head -- which is the widening the
	// per-person grant rule exists to prevent.
	//
	// Maintainer decision 2026-08-12: ceo_internal ONLY. Not operator, not park_head, and not
	// counts_approver (approving a movement someone else raised is not the same authority as
	// unilaterally reclassifying a pen).
	//
	// Maintainer decision 2026-08-14 WIDENS WHAT IT COVERS, not who holds it: it now also gates the
	// census-slice correction (/admin/goats/census-slice/*), which fixes a wrongly recorded breed or
	// sex on one Counts Breakdown row. Same shape and therefore same authority -- many animals, one
	// click, applied immediately with no approval and no proof, reached from the same screen. The
	// holder set is unchanged (ceo_internal), so this grants nothing to anyone new; it is one
	// permission for "bulk corrections made from the Counts census" rather than two names for one
	// kind of power.
	GoatReclassifyShedStage = "goat.reclassify_shed_stage"
	// HealthRead renders backend-owned disease-course work. HealthReport RAISES a sick-goat
	// report from the field; HealthDiagnose is the clinical authority over the configured
	// course; HealthExecute records the operator's treatment session. They are deliberately
	// narrower than the admin goat health-status write.
	//
	// Report vs diagnose (maintainer decision 2026-07-30): the field operator who SPOTS a sick
	// animal is the whole point of the phone Health module, so raising the case is its own
	// permission. Before this split the ONLY permission on POST /app/health/cases was
	// health.diagnose, which no operator holds — the phone showed them the ＋ Add-case button and
	// the write came back 403, where the outbox retried it forever behind a "Retrying sync" row.
	// Diagnosis/treatment authoring stays with the PC Director and CEO tier.
	HealthRead     = "health.read"
	HealthReport   = "health.report"
	HealthDiagnose = "health.diagnose"
	HealthExecute  = "health.execute"
	// HealthConfigRead gates the AUTHORED TREATMENT-PROTOCOL read surface (/health-config/*): the
	// per-disease, per-age-band day-by-day course -- which medicine, what dosage, by what route,
	// on which day, and for how many days.
	//
	// It is deliberately SEPARATE from HealthRead, which gates the operator's WORK (/app/health/*:
	// today's treatment sessions for the animals in front of them). Those are two different
	// authorities: an operator must see the steps for the case they are treating, and must not be
	// able to inspect and edit the standing clinical rulebook the whole herd is treated from.
	// HealthRead already delivers the steps a case pinned; this permission is about the rulebook.
	//
	// Nav consequence: the Health Config screen declares this permission, so a principal without it
	// does not receive that nav item. Hiding the item is not the control -- the routes require the
	// same permission, so the screen is unreachable rather than merely invisible.
	HealthConfigRead = "health.config.read"
	// HealthConfigWrite gates EDITING that rulebook: changing a medicine or a dosage, changing how
	// many days a course runs, adding a disease, and publishing any of it.
	//
	// This is the highest-consequence permission in the Health module, and it is granted narrowly
	// for a clinical reason. A dosage is an instruction a field operator follows on an animal
	// without re-deriving it -- the phone shows the step and they administer it. An incorrect
	// ration produces thinner animals a month later; an incorrect dosage can produce a dead one the
	// same day. So the authority to change it sits with the CEO tier and the Health Director, and
	// nowhere else.
	//
	// Deliberately NOT granted to RoleOperator (executes a course, does not author it), RolePark-
	// Head (runs a park's execution), RolePCDirector (Preventive Care is a DISTINCT department from
	// Health -- see RoleHealthDirector), or RoleVerifier (separation of duty: the verifier checks
	// captured work and must not be able to rewrite the standard that work is judged against).
	//
	// Every write it gates is versioned and ledgered: an edit builds a draft, publishing swaps
	// which version is live and retires the old one, `health_cases` pins the version each goat was
	// diagnosed under, and health_config_write_log records who changed what, when, and under which
	// idempotency key. The permission controls who may author; the version history and the ledger
	// are what make an authored change answerable afterwards.
	HealthConfigWrite         = "health.config.write"
	LocationsRead             = "locations.read"
	LocationsWrite            = "locations.write"
	LocationsReview           = "locations.review"
	LocationsRetire           = "locations.retire"
	OperatorsRead             = "operators.read"
	OperatorsWrite            = "operators.write"
	OperatorsActivate         = "operators.activate"
	OperatorsDeactivate       = "operators.deactivate"
	OperatorsManageDevice     = "operators.manage_device"
	OperatorsManageCapability = "operators.manage_capability"
	OperatorsManageRoster     = "operators.manage_roster"
	OperatorsViewAudit        = "operators.view_audit"
	OperationsRepair          = "operations.repair"
	AppBootstrap              = "app.bootstrap"
	AdminWebBootstrap         = "admin_web.bootstrap"
	SOPRead                   = "sop.read"
	SOPWrite                  = "sop.write"
	SOPPublish                = "sop.publish"
	TaskRead                  = "task.read"
	TaskAssign                = "task.assign"
	TaskExecute               = "task.execute"
	TaskVerify                = "task.verify"
	ProtocolRead              = "protocol.read"
	ProtocolWrite             = "protocol.write"
	ProtocolPublish           = "protocol.publish"
	ObligationRead            = "obligation.read"
	VaccinationRead           = "vaccination.read"
	VaccinationOverviewRead   = "vaccination.overview_read"
	// VaccinationAlertsRead gates the app-tier Alerts tab's backing feed
	// (GET /control-tower/vaccination), decoupled from the shared leadership
	// oversight bundle {ObligationRead, VaccinationRead} that the other ~15
	// vaccination admin/oversight routes use as an ANDed Permissions pair.
	//
	// Bug found on-device 2026-08-04: RoleOperator held neither ObligationRead
	// nor VaccinationRead, so its Alerts tab's ONLY backing request always 403'd
	// and the mobile client silently rendered a friendly-looking "No alerts yet"
	// empty state instead of a visible error (same defect shape 9e0cf5bbf fixed
	// for the counts verification tab: "a tab that looks permanently empty
	// rather than broken").
	//
	// The handler (processintegrity/adapters/http/handler.go, buildQuery) already
	// resolves park scope for this exact route via ResolveAuthorizedParkScope --
	// an operator's request auto-narrows to their OWN park and an explicit
	// other-park park_id 403s (park_scope_test.go) -- so granting operator this
	// route is safe: they get park-of-the-day vaccination alerts, nothing more.
	//
	// This is added as its own AnyPermissions option on the route (see routes.go)
	// rather than adding ObligationRead/VaccinationRead to RoleOperator's grant
	// set, precisely because that pair is ANDed across ~15 OTHER admin routes
	// (action-center, adherence, workflows, operations, schedule,
	// drive-assignments, execution, command, sheds, capacity-config) that are
	// NOT all proven park-scoped the way this one is -- widening the operator's
	// base grant set would have handed them tenant-wide leadership oversight
	// surfaces the Alerts tab never asked for. Mirrors the weighing precedent at
	// appListWeighingAlerts: "gated on WEIGHING capabilities ONLY... pointing the
	// weighing bar at [the vaccination feed] is exactly what made the previous
	// weighing alerts tab 403 for weighing operators and got it deleted" --
	// applied here in the opposite direction, a dedicated capability rather than
	// borrowing someone else's.
	VaccinationAlertsRead = "vaccination.alerts_read"
	VaccinationVerify     = "vaccination.verify"
	VaccinationCampaign   = "vaccination.campaign"
	// VaccinationOverseeExecution gates the READ-ONLY, park-scoped OVERSIGHT view of vaccination
	// execution on the app routes (/app/vaccination/execution): every shed/partition in the
	// actor's authorized park(s) instead of only the drives the caller was assigned.
	//
	// It is a capability, deliberately, and not a role-name check. The handler previously asked
	// "is your role one of {ceo_internal, pc_director, park_head}", which meant the org-role
	// catalog's composed preventive-care Director/Head -- the successors of exactly those flat
	// roles -- fell into the operator-assignment branch: they hold no task.execute and are
	// assigned no drive, so the read returned zero rows AND the response was not marked
	// read-only, leaving a tappable shed whose scan/submit is refused `task_not_assigned`.
	//
	// Holding this NEVER widens a write: scan/submit still require the caller to be the task's
	// assignee, and the handler marks the response viewerReadOnly for whoever holds it. Mirrors
	// WeighingOverseeOperators, the same see-other-people's-work capability for weighing.
	VaccinationOverseeExecution = "vaccination.oversee_execution"
	WeighingPlan                = "weighing.plan"
	WeighingMonitor             = "weighing.monitor"
	WeighingExecute             = "weighing.execute"
	// WeighingOverseeOperators gates SEEING OTHER PEOPLE'S weighing shed tasks -- the extra
	// "Operators" surface that lists work assigned to someone else, across parks, READ-ONLY.
	//
	// It is deliberately its own capability rather than a reuse of WeighingMonitor or a role check.
	// WeighingMonitor is the reopen/close AUTHORITY over a submitted scope; it is not "may browse
	// everyone's work", and conflating the two is what made the work list ask a role instead of a
	// capability -- an actor holding both execute and monitor fell into the unfiltered branch and
	// got every shed in every park with a live scan action, including sheds whose submit would be
	// refused because they belong to another assignee.
	//
	// Whoever holds this may only LOOK: the weighing write still requires the caller to be the
	// shed's assignee, so this capability never widens what anyone can record.
	WeighingOverseeOperators = "weighing.oversee_operators"
	CalendarRead             = "calendar.read"
	CalendarAction           = "calendar.action"
	ProcurementRead          = "procurement.read"
	ProcurementWrite         = "procurement.write"
	ProcurementReview        = "procurement.review"
	// VendorRead gates the procurement VENDOR REGISTER (/procurement/vendors): the farm's
	// counterparty contact book -- livestock agents and stockists, transport, feed, manure, pellet
	// factories, labour, insurance, test labs and site trades.
	//
	// It is DELIBERATELY not a reuse of ProcurementRead, which is held by seven roles including
	// RoleOperator and RoleParkHead because it gates the source-entry/intake screens those roles
	// actually work. The register is a different thing: it carries a vendor's negotiated price, its
	// banking instrument, and the phone number of the person the farm buys from. Gating it on
	// ProcurementRead would have handed every operator the payment details of every supplier as a
	// side effect of being able to see an arriving load.
	VendorRead = "procurement.vendor.read"
	// VendorWrite gates adding a vendor and editing one. Held by the same two roles as VendorRead
	// today; kept separate so a future read-only procurement analyst is expressible without a
	// schema change.
	VendorWrite = "procurement.vendor.write"
	// VendorFinanceRead gates the PAYMENT INSTRUMENTS on a vendor row -- bank name, account number,
	// IFSC, UPI id and PAN. Without it the register still renders in full; those five fields come
	// back null with FinanceRedacted set, so the screen says "hidden" rather than showing a
	// misleading blank.
	//
	// Maintainer decision 2026-08-12 was "leadership + a procurement role" WITHOUT a finance split,
	// so today it is granted to exactly the roles that hold VendorRead and nobody sees anything
	// different. It exists as a separate permission because withdrawing it later is then a one-line
	// grant change rather than a schema, API and UI change -- and because the redaction path has to
	// be built and tested from the start to be trustworthy at all. Do not fold it into VendorRead.
	VendorFinanceRead = "procurement.vendor.finance.read"
	RosterRead        = "roster.read"
	RosterManage      = "roster.manage"
	// CountsWrite gates the app-tier Counts write surface: an operator recording a shifting
	// (movement) event, a birth, or a death from the phone (/app/counts/*).
	//
	// It is a DEDICATED permission rather than a reuse of goat.write_identity/goat.write_health
	// on purpose. Per the maintainer decision recorded in AGENTS.md's business-rule lock, field
	// operators may record birth AND death from the mobile app, but granting them the admin goat
	// write permissions would also hand them every /admin/goats/* route. This permission widens
	// exactly the approved surface and nothing else; the guardrailed death semantics (the
	// dead+died pairing, the separate route, the obligation-cancelling goat.exited event) are
	// unchanged and still enforced inside the identity module.
	//
	// It is granted to the roles that already perform ground capture (the roles holding
	// task.execute), never to RoleVerifier (separation of duty: whoever captures does not verify)
	// and never to the Head/Director tiers, which act on verified work rather than capturing it.
	CountsWrite = "counts.write"
	// CountsRead gates the Counts CENSUS surface: the herd-register summary and the
	// counts breakdown (/counts/breakdown, /herd-register/summary), i.e. whole-herd
	// population figures.
	//
	// It is deliberately SEPARATE from CountsWrite and NARROWER than goat.read. Per the
	// maintainer decision (2026-07-18), field capture and census visibility are different
	// authorities: an Operator or Park Head records births/deaths/shiftings for their own
	// ground truth but does not get a tenant-wide population view, while CEO/CXO does.
	// Splitting it off goat.read is what makes that enforceable — goat.read is held by
	// nearly every role, so reusing it would have made the census effectively public.
	//
	// Nav consequence: the Counts module's census page declares this permission, so a
	// principal without it simply does not receive that nav item (and a principal with
	// NEITHER counts permission does not receive the Counts module at all). Hiding the
	// tab is not the control — the routes above require this same permission, so a hidden
	// page is unreachable, not merely invisible.
	CountsRead = "counts.read"
	// CountsAlertsRead gates the app-tier Counts Alerts feed's backing route
	// (GET /app/counts/alerts), decoupled from CountsRead/CountsWrite the same way
	// VaccinationAlertsRead is decoupled from ObligationRead/VaccinationRead above.
	//
	// This exists ONLY because COUNTS IS AN OFF FEATURE (AGENTS.md): health_director is Counts'
	// documented owner and the leadership recipient of every counts.proof.* verification push
	// (backend/internal/notificationbridge/verification_notify_consumer.go, moduleCounts
	// profile), but deliberately holds NEITHER CountsRead NOR CountsWrite -- granting either would
	// light up the Counts capture/census nav for that role and switch the feature on, which is
	// exactly what AGENTS.md's "Ownership and access are separate decisions here" forbids. Without
	// this permission, GET /app/counts/alerts would have to gate on CountsRead/CountsWrite/
	// VerificationReview, and health_director would 403 on their own inbox -- the identical
	// "recipient vs reader" gate-1 failure the weighing verifier hit on 2026-08-08, just with a
	// permission grant standing in for the missing park-scope fix on the other two feeds.
	//
	// Route AnyPermissions on appListCountsAlerts is [CountsAlertsRead, CountsWrite,
	// VerificationReview]: CountsAlertsRead admits health_director without widening any Counts
	// access; CountsWrite admits park_head and RoleCEOInternal (both already hold it);
	// VerificationReview admits the verifier. The query itself is scoped to
	// context->>'member_id' = the caller, so this permission opens the tab, not the data.
	CountsAlertsRead = "counts.alerts_read"
	// CountsApproveLifecycle gates approving/rejecting a BIRTH or DEATH request
	// (/app/counts/approvals/{id}/{approve,reject} for those two types).
	//
	// Birth submission creates canonical children immediately but keeps them outside herd counts;
	// birth approval admits the whole litter to those counts. Death submission leaves the goat alive,
	// and death approval exits it and cancels open obligations. Those herd-composition decisions sit
	// with the CEO/internal tier, not with the ground roles that capture the event.
	//
	// Deliberately NOT granted to RoleParkHead: a park head runs a park, and the whole point of
	// splitting this from CountsApproveShifting is that the person who authorizes a movement is
	// not thereby authorized to write animals into or out of existence. Never granted to
	// RoleOperator (they capture; capture is not approval) or RoleVerifier (separation of duty:
	// the verifier checks media, and giving them lifecycle approval would collapse the review
	// chain into one role).
	CountsApproveLifecycle = "counts.approve_lifecycle"
	// CountsApproveShifting gates approving/rejecting a SHIFTING request.
	//
	// A shifting event is a movement between sheds inside a park, so its approver is the park head
	// who owns that ground. Approving one is not a paperwork flip: it MOVES THE ANIMALS, updating
	// each named animal's canonical location and re-scoping its shed-scoped vaccination
	// obligations to the destination shed.
	//
	// Granted to RoleParkHead and the CEO/CXO tier. RoleCEOInternal holds it because the
	// platform-owner cohort must be able to unblock any queue, but that is a separate grant from
	// CountsApproveLifecycle by design -- the two authorities are independent, and holding one
	// never implies the other. Never granted to RoleOperator or RoleVerifier.
	CountsApproveShifting = "counts.approve_shifting"
	// CountsApproveAccess is the COARSE route gate on the approvals surface. It is not an
	// authority by itself and must never be treated as one.
	//
	// It exists because route-level authorization is static per (method, pattern) and the decision
	// endpoints address a request by ID -- the middleware cannot see whether that ID is a birth or
	// a shifting, because the type lives in the stored row. Route.Permissions is also ANDed, so
	// listing both fine-grained permissions on the route would lock out a park_head who legitimately
	// holds only one of them.
	//
	// So this permission answers only "may this caller reach the approvals surface at all", and the
	// REAL check is the type-specific one applied in the handler after the row is read
	// (DecidableApprovalRequestTypes). It is granted to exactly the roles that hold at least one of
	// CountsApproveLifecycle / CountsApproveShifting, so it never widens access on its own.
	CountsApproveAccess = "counts.approve_access"
	// FeedConfigRead gates the AUTHORED FEED CONFIGURATION read surface (/feed-config/*): the ration
	// grid, the breed -> ration-group map, the shed-tag vocabulary, the feed-item catalog, the
	// session split, the per-shed multipliers, and the per-park dispatch clock.
	//
	// It is deliberately SEPARATE from ProtocolRead, which gates the feed-DIRECTION surface
	// (/feed-direction/*). Those are two different authorities over two different things: direction
	// is today's operational output ("what goes to shed 4 this morning"), while this is the authored
	// rule that produced it ("Osmanabadi/Pregnant gets 250 g of concentrate at CBE"). A park head or
	// an operator legitimately needs to see today's direction without being able to inspect and edit
	// the tenant-wide grid the whole farm is fed from, and collapsing the two into one permission
	// would make that impossible to express.
	//
	// It is also narrower than a generic config-read: the grid is commercially meaningful
	// (it encodes the farm's whole feeding economics), which is why it sits with the CEO/CXO tier
	// rather than being handed to every authenticated principal the way /app/bootstrap is.
	//
	// Nav consequence: the Feed Config screen declares this permission, so a principal without it
	// does not receive that nav item. Hiding the item is not the control -- the routes require the
	// same permission, so the screen is unreachable rather than merely invisible.
	FeedConfigRead = "feed_config.read"
	// FeedConfigWrite gates EDITING that configuration: authoring a ration rate, a shed multiplier,
	// or a park's dispatch clock.
	//
	// This is the highest-consequence permission in the feed module, and it is granted narrowly for a
	// specific reason: a ration rate is a FEEDING INSTRUCTION for every animal matching its
	// (park, ration_group, shed_tag) key. One edit silently changes what hundreds of animals are fed
	// every day until someone notices. Unlike a vaccination obligation -- which surfaces as visible,
	// dated, chaseable work when it goes wrong -- an incorrect ration produces no alert at all; it
	// produces thinner animals a month later. So the authority to change it sits with the CEO/CXO
	// tier that owns farm economics, NOT with the ground roles that execute feeding.
	//
	// Deliberately NOT granted to RoleOperator or RoleParkHead: an operator packs and delivers what
	// the direction says, and a park head runs a park's execution. Neither authors the rule the whole
	// tenant is fed from. Never granted to RoleVerifier (separation of duty: the verifier checks
	// captured work and must not be able to rewrite the standard that work is judged against).
	//
	// Every write it gates is effective-dated and ledgered: an edit closes the current row and opens
	// a new one, and feed_config_write_log records who changed what, when, and under which
	// idempotency key. The permission controls who may author; the audit trail is what makes an
	// authored change answerable afterwards.
	FeedConfigWrite = "feed_config.write"
	// FeedPackingRead gates the PACKING worklist (/feed-packing/worklist): the per-shed, per-session
	// list of feed items and expected kg that a packer physically weighs out and bags.
	//
	// It is a separate permission from ProtocolRead, which gates the feed-DIRECTION surface
	// (/feed-direction/*), and the split is forward-looking rather than cosmetic. Direction and
	// packing are the same numbers read by different people for different purposes: direction is
	// the plan a park head reviews and corrects before dispatch, packing is the ground instruction
	// the store team executes. Those audiences will diverge -- the packing list is the natural
	// candidate for widening to RoleOperator once capture is built on it -- and reusing ProtocolRead
	// would make that widening impossible to express, because it would also hand the packing team
	// the vaccination protocol surface. Granting a distinct permission now costs nothing and keeps
	// that door open.
	//
	// It is granted to the tiers that own park execution and farm oversight: CEO/CXO, ParkHead, and
	// RoleCEOInternal (founder/builder visibility invariant -- the platform-owner cohort holds the
	// grants for every built visible module). Deliberately NOT granted to RoleVerifier: the verifier
	// checks captured work against a standard and has no role in dispatching feed.
	//
	// It is READ-ONLY and has no write twin today. The worklist records nothing: there is no proof
	// capture, no video, and no packing status stored anywhere -- the status field is derived from
	// the generation result. A future capture surface needs its own write permission, and reusing
	// this one for it would silently turn every reader into a recorder.
	FeedPackingRead = "feed_packing.read"
	// FeedDirectionRead gates the feed-DIRECTION read surface (/feed-direction/preview,
	// /feed-direction/generation-preview, and the counts-projection exception list).
	//
	// These routes were gated on ProtocolRead, which is the VACCINATION protocol read. That was
	// too broad in both directions: it handed every feed reader the vaccination protocol surface,
	// and — the reason it had to change — it made the Feed Director inexpressible. Giving
	// feed_director ProtocolRead to reach today's dispatch sheet would also give them Vaccination,
	// breaking the one-module-one-director segregation this role exists to enforce.
	//
	// It is granted to operator, park_head, verifier and ceo_internal (who all held ProtocolRead)
	// plus feed_director. It is DELIBERATELY NOT granted to pc_director, who therefore LOSES the
	// feed reads he reached only as a side effect of holding the vaccination protocol permission
	// (/feed-direction/preview, /feed-direction/generation-preview and the counts-projection
	// exception list). That narrowing is the point of the split -- one module, one director --
	// and TestDirectorHoldsNoOtherModulesCapabilities pins it. Recording it here because an
	// earlier version of this comment claimed the split narrowed nothing, which was false.
	FeedDirectionRead = "feed_direction.read"
	// FeedDirectionOversee gates the feed-projection EXCEPTION verdicts
	// (/feed-direction/counts-projection/exceptions/{id}/{resolve,dismiss}): deciding that a shed
	// whose projected head count could not be resolved is either corrected or knowingly ignored.
	//
	// Previously ProtocolWrite, which only RoleCEOInternal holds — so this is CEO plus the Feed
	// Director who owns the chain that produced the exception (Feed_Director.pdf C2 Feeding
	// Monitoring: "Any feeding that happened late, in the wrong quantity, or to the wrong cohort
	// must be flagged, investigated, and corrected immediately"). No role loses anything.
	FeedDirectionOversee = "feed_direction.oversee"
	// FeedDirectionComplete is the operator WRITE twin FeedPackingRead's comment anticipated: it gates
	// POST /feed-direction/complete, where an operator records that one shed-session's feed direction
	// was carried out (with optional video proof). It is deliberately separate from the feed reads --
	// reading the dispatch sheet is not the same authority as recording that the feeding happened --
	// and separate from feed_config.write, which authors the ration grid rather than executing it.
	//
	// Granted to the ground execution tiers only: RoleOperator (the phone operator who walks the
	// park) and RoleParkHead (execution on their own ground). NOT granted to leadership read roles:
	// RoleFeedDirector and RoleCEOInternal can inspect feed status, but must not reach record/upload.
	// Also not granted to RoleVerifier (checks captured work) or RolePCDirector (different module).
	FeedDirectionComplete = "feed_direction.complete"
	// FeedTransportRead gates the daily feed-TRANSPORT task list (GET /feed-transport/tasks): the
	// per-shed 15:30 IST task and whatever proof attempt each one currently carries.
	//
	// It is split out of FeedDirectionComplete (maintainer decision 2026-08-05: the Feed Director
	// must see every page of the module they own). Both the list read and the proof submit were
	// gated on FeedDirectionComplete, which is a WRITE permission -- so the only way to LOOK at the
	// transport worklist was to hold the authority to record that transport happened. That made the
	// Feed Director inexpressible on this surface for the same reason ProtocolRead did on the
	// dispatch sheet: the role deliberately holds no feed_direction.complete, because
	// Feed_Director.pdf puts field execution on the Park Head ("Executing is not directing"), and
	// TestDirectorHoldsNoOtherModulesCapabilities pins that.
	//
	// Granted to everyone who could already open the list (RoleOperator, RoleParkHead,
	// RoleCEOInternal -- so no principal loses the page) PLUS RoleFeedDirector. The SUBMIT route
	// keeps FeedDirectionComplete, so widening the read does not hand anyone the ability to record
	// a transport proof.
	FeedTransportRead = "feed_transport.read"
	// VerificationReview READS the generic Verification vertical's evidence queue
	// (context/architecture/verification-module-design.md): the media, the operator/shed/park
	// context, and whatever verdict has been recorded. It is a VISIBILITY permission — leadership
	// must be able to watch the same videos and see the same decisions in order to act on them —
	// so it is held by the Verifier and, per the org-role-model truth table, by CEO/CxO. It is
	// never granted to Operator/Manager (capture).
	VerificationReview = "verification.review"
	// VerificationVerdict RECORDS the approve/reject decision. Split out of VerificationReview by
	// maintainer decision 2026-08-03: reading the evidence and DECIDING on it are different
	// authorities, and the decision belongs to the Video Verification Team alone.
	//
	// Granted ONLY to RoleVerifier — deliberately NOT to RoleCEOInternal. This is a rare, explicit
	// carve-out from the founder/builder visibility invariant: the founder cohort keeps full
	// VISIBILITY of every verification item (VerificationReview above) and still owns the closing
	// act (VerificationAct below), but the independent second check is worthless if the people it
	// checks can sign it off themselves. Do not "restore" this to CEO to satisfy the visibility
	// invariant; visibility is already satisfied by the read.
	VerificationVerdict = "verification.verdict"
	VerificationAct     = "verification.act"
	// VerificationOversee gates the CROSS-MODULE OVERSIGHT controls on the /verify screen: the
	// module chips (All modules/Counts/Feed/Health/Milk/Vaccination), the capture-date range
	// picker, and any other tenant-wide filter that lets a caller slice the WHOLE verification
	// backlog across modules and days. It is a rendering/query-shape authority layered on TOP of
	// VerificationReview (the read itself) -- not a substitute for it.
	//
	// Incident (2026-08-12, STG): these filters shipped for the CEO's oversight view but rendered
	// for every role that can open /verify, including RoleVerifier, because /verify is a single
	// role-agnostic admin-web page. A verifier does not pick a module or a historical date range --
	// her queue is the open backlog for the categories she is on duty for, oldest-first (see
	// IsVerifierQueueRead in verification/app/service.go) -- so the extra controls were confusing
	// chrome on her working queue, not a capability she needed.
	//
	// Granted to RoleCEOInternal and RolePCDirector: exactly the two roles that already receive the
	// UNRESTRICTED (cross-category, "leadership") branch of
	// verification/adapters/http/handler.go's resolveVerifierCategories -- i.e. VerificationReview
	// without VerificationVerdict. This capability makes that existing distinction explicit and
	// checkable instead of leaving it as an inference over grant shape ("does this caller's
	// verdict permission absence imply oversight?"). Never granted to RoleVerifier (the same
	// separation of duty as VerificationVerdict/VerificationReview: the verifier works ONE
	// module's queue, oversight watches ALL of them) and never inferred from a role string --
	// callers must be checked for this permission, not for RoleCEOInternal/RolePCDirector by name.
	VerificationOversee = "verification.oversee"

	// VerificationFilterByCaptureDate gates the CAPTURE-DATE RANGE picker on /verify, on every
	// page of the verifier's workspace (maintainer decision 2026-08-17).
	//
	// It is SPLIT OUT of VerificationOversee because the two are different rules that the
	// 2026-08-12 incident happened to bundle. That incident was about CROSS-MODULE chrome
	// rendering for every role: module chips let a caller reshape the queue across modules she has
	// no duty in, and they stay leadership-only. A date range crosses no module boundary -- it
	// narrows the caller's OWN queue to the days she is working -- so the verifier holds this one
	// while VerificationOversee stays with leadership.
	//
	// Held by RoleVerifier, RolePCDirector and RoleCEOInternal. It adds NO verdict authority, no
	// cross-module reach, and no analytics: it only filters rows the holder could already see.
	VerificationFilterByCaptureDate = "verification.filter_capture_date"
	// VerificationEvidenceTimeline gates the VIDEO LOG on /verify: for ONE business day, per shed,
	// the time each proof was uploaded (feed distribution's three, feed packing's one, feed
	// transport's one, and the vaccination/weighing/birth/death/shifting proofs beside them).
	// Maintainer decision 2026-08-14.
	//
	// It is a SEPARATE capability from VerificationOversee, and the difference is the whole point.
	// Oversight is the cross-module BACKLOG chrome -- module chips, a historical capture-date range,
	// the analytics aggregate -- which the 2026-08-12 STG incident deliberately took away from the
	// verifier because it was confusing furniture on her working queue. This is one day's arrival
	// times for a shed. Granting it does NOT hand a caller the module chips, the date-range picker,
	// or the analytics drawer; those stay on VerificationOversee.
	//
	// Granted to RoleVerifier ALONGSIDE RoleCEOInternal and the four directors. That is deliberate
	// and it is the one place this constant departs from VerificationOversee's "the verifier works
	// ONE module's queue, oversight watches ALL of them" line: the video log is CROSS-MODULE for
	// her too, because the question it answers is "what arrived from this shed today", and a shed's
	// day is feed AND vaccination AND a death together. That line governs VERDICT authority and
	// queue chrome; this grants neither -- it is a read-only arrival log with no verdict entry
	// point, no filter that reshapes her queue, and no act/close capability. A verifier still
	// cannot decide an item outside her duty modules, because VerificationVerdict is unchanged.
	//
	// Never confuse this with VerificationReview: review is the QUEUE (items, media, verdicts) and
	// is what a caller needs to open /verify at all. A principal could hold this and not review, in
	// which case they see arrival times and can open nothing -- so it is always granted with review,
	// never instead of it.
	VerificationEvidenceTimeline = "verification.evidence_timeline"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleVerifier: {
		GoatRead: {}, GoatWriteIdentity: {},
		LocationsRead: {}, LocationsReview: {},
		// AdminWebBootstrap opens the admin-web shell for the verifier-only web workspace
		// (maintainer decision 2026-08-03). It is NOT a widening to the admin-web product: a
		// principal holding verification.review and NOT verification.act receives the
		// verifier lens from adminui/app/verifier_lens.go -- five registry-composed evidence
		// modules and the /actions route ONLY. Every other page contract is omitted from her
		// bootstrap, so requireAdminWebPageContract throws and the route fails closed rather
		// than rendering a greyed-out shell she could still reach by URL. The data routes
		// behind those pages remain independently gated by permissions she does not hold.
		OperatorsRead: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		TaskRead: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {},
		FeedDirectionRead: {},
		CalendarRead:      {},
		ProcurementRead:   {}, ProcurementReview: {},
		RosterRead: {},
		// The Video Verification Team's permissions: read the evidence queue, and record the
		// approve/reject verdict on it. VerificationVerdict is held by NO other role, CEO included
		// (maintainer decision 2026-08-03) — the second check must be independent of everyone whose
		// work it checks.
		VerificationReview:  {},
		VerificationVerdict: {},
		// The VIDEO LOG (maintainer decision 2026-08-14): one day, per shed, when each proof
		// arrived. Deliberately granted to the verifier even though VerificationOversee is not --
		// see that constant and VerificationEvidenceTimeline for why the two are separate. This
		// adds no verdict authority and no queue-reshaping filter.
		VerificationEvidenceTimeline: {},
		// The capture-date range on her own queue (maintainer decision 2026-08-17). Deliberately
		// granted even though VerificationOversee is not -- see that constant for why the two are
		// separate. It reshapes nothing across modules and adds no authority.
		VerificationFilterByCaptureDate: {},
	},
	RoleParkHead: {
		GoatRead:      {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, AppBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationOverseeExecution: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementWrite: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
		CountsWrite: {},
		// Approvals moved off the park head (maintainer decision 2026-07-21): approve/reject now
		// belongs to the four org tiers (director/head/manager/am) plus admin and ceo_internal, on
		// the admin-web Approvals page only. A park head no longer holds ANY counts.approve_*
		// permission, so they can neither list nor decide birth/death/shifting requests.
		// A park head dispatches feed on their own ground, so they read the packing worklist and may
		// record a shed-session as fed. They still hold no feed_config.* grant: executing a ration is
		// not authoring one.
		FeedPackingRead:       {},
		FeedDirectionRead:     {},
		FeedDirectionComplete: {},
		FeedTransportRead:     {},
		VerificationAct:       {},
		HealthRead:            {},
	},
	RolePCDirector: {
		GoatRead: {}, GoatWriteHealth: {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskExecute: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		VaccinationOverseeExecution: {},
		CalendarRead:                {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		// NOT FeedDirectionRead. pc_director reached the feed dispatch sheet only as a side
		// effect of holding protocol.read, which gated both the vaccination protocol and the
		// feed reads. Splitting feed_direction.read off is what makes one-module-one-director
		// enforceable, and keeping the incidental feed access here would defeat it: the PC
		// Director owns Vaccination, and Feed belongs to feed_director.
		// VerificationReview is the READ of the evidence queue (GET /verification/queue): the
		// items, their media, and the verdicts already recorded. VerificationAct is closing the
		// source task / requesting rework. The PC Director held ACT without REVIEW, so the
		// backend-composed nav offered a Videos entry whose only backing read 403'd -- observed
		// on-device 2026-08-08. AGENTS.md's verdict-exclusivity lock is explicit that leadership
		// KEEPS review and act and loses only VerificationVerdict (approve/reject), which stays
		// verifier-only and is deliberately NOT added here: an independent second check the
		// checked party can sign is not independent.
		VerificationReview: {}, VerificationAct: {},
		// The cross-module oversight filters on /verify (module chips, capture-date range) --
		// pc_director already receives the unrestricted, cross-category branch of
		// resolveVerifierCategories alongside RoleCEOInternal (VerificationReview without
		// VerificationVerdict), so this makes that existing distinction an explicit, checkable
		// capability instead of an inference over grant shape. See VerificationOversee's doc
		// comment.
		VerificationOversee: {},
		// Leadership keeps the date range it already had, now under its own capability.
		VerificationFilterByCaptureDate: {},
		// The VIDEO LOG (maintainer decision 2026-08-14): one day, per shed, when each proof
		// arrived. See VerificationEvidenceTimeline -- a separate capability from the oversight
		// chrome above, held here because leadership must see every built surface.
		VerificationEvidenceTimeline: {},
		// Clinical authority over the configured disease course (maintainer decision 2026-07-30);
		// raising a report is HealthReport, which every field tier holds.
		HealthRead: {}, HealthReport: {}, HealthDiagnose: {},
	},
	// RoleGrowthDirector runs Weighing and ONLY Weighing. The role key existed with no entry in
	// this map, which meant every RoleHasPermission check returned false and a growth_director
	// grant authorized nothing at all -- the Growth Director could not weigh, and their
	// `weighing_execute` bootstrap flag was false.
	//
	// Deliberately NO vaccination permission: the module drawer is composed from granted modules,
	// so this role gets exactly one nav item (Weighing). Adding a vaccination permission here later
	// is what would give them a second item -- that is the intended lever, not a nav template.
	//
	// A director's shed reach is broader than an operator's (both parks, via tenant scope) while an
	// operator stays inside their own park, but the weighing WRITE stays per-shed-assignment for
	// both: you weigh the sheds you were assigned, and nothing else. Weighing itself is free-flow
	// (raw RFID, no herd-animal join, no vaccination rules, weighing tables only), so no goat or
	// protocol read is needed here.
	//
	// THIS ENTRY IS THE SINGLE SOURCE OF TRUTH for growth_director. It used to be
	// shadowed: permissions_orgrole.go's init() reassigned
	// rolePermissions[RoleGrowthDirector] to a second, hand-maintained map, and every
	// permission declared here but absent there was silently inert. That override is
	// gone; registerRole now panics on any attempt to re-declare a role. The
	// director-tier permissions the override carried (goat/roster/task/verification
	// oversight) are merged in below so nothing that worked before is narrowed.
	RoleGrowthDirector: {
		AppBootstrap: {}, AdminWebBootstrap: {},
		LocationsRead: {}, OperatorsRead: {},
		// NOT WeighingPlan: planning a weighing task is CEO-only (maintainer decision
		// 2026-08-01). The Growth Director monitors weighing across both parks, oversees
		// the operators, and can execute -- but the task itself is raised by the CEO.
		WeighingMonitor: {}, WeighingExecute: {},
		// Only this role browses other people's weighing work (the Operators surface). The CEO does
		// not get it: a planner's first surface is the flat all-tasks list, so a second
		// someone-else's-work tab would be redundant for them.
		WeighingOverseeOperators: {},
		CalendarRead:             {}, CalendarAction: {},
		// Merged from the removed permissions_orgrole.go override -- these were the
		// EFFECTIVE grants before this fix, so dropping them here would trade one
		// silent authorization defect for another.
		GoatRead: {}, GoatWriteHealth: {},
		OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
	},
	// RoleFeedDirector runs the FEED chain and only the feed chain (maintainer decision
	// 2026-08-01; wiki/Handbooks/Feed_Director.pdf ROLE PURPOSE + P1-P6/C1-C4/M1).
	//
	// What it gets, and why:
	//   - FeedConfigRead/FeedConfigWrite: C1 Feed Directions and the authored ration grid the
	//     directions are computed from. The handbook makes this the role's own instrument
	//     ("Directions must be issued in writing the day before"), so unlike every other
	//     non-CEO role it authors the grid.
	//   - FeedDirectionRead + FeedPackingRead: the dispatch sheet and the packing worklist,
	//     i.e. daily checks 4-6 (directions issued / feeding compliance / wastage review).
	//   - FeedDirectionOversee: the exception verdicts on the projected-count feed inputs.
	//   - VerificationAct: M1 SOP Video Double Verification is REVIEW-OF-A-VERIFIER —
	//     the director confirms the verifier is reviewing, and acts on flagged violations.
	//
	// What it deliberately does NOT get:
	//   - FeedDirectionComplete: "All field execution happens through the Park Head"; the
	//     ground team feeds and records, the director directs. Executing is not directing.
	//   - VerificationReview: separation of duty. Double-verifying the verifier is not
	//     becoming the verifier; the verdict stays with RoleVerifier (tenant-level).
	//   - Any vaccination, weighing, or counts permission: one module, one director. This is
	//     the same shape as RoleGrowthDirector's "Weighing and ONLY Weighing" entry, and it is
	//     what TestDirectorModuleSegregation pins in both directions.
	RoleFeedDirector: {
		AppBootstrap: {}, AdminWebBootstrap: {},
		LocationsRead: {}, OperatorsRead: {},
		OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {},
		GoatRead: {}, SOPRead: {}, TaskRead: {}, TaskAssign: {},
		FeedConfigRead: {}, FeedConfigWrite: {},
		FeedDirectionRead: {}, FeedDirectionOversee: {}, FeedPackingRead: {},
		// The transport worklist READ (maintainer decision 2026-08-05). Paired deliberately with
		// the absence of FeedDirectionComplete below: the director sees every page of the feed
		// chain including the daily transport tasks, and still cannot record one as done.
		FeedTransportRead: {},
		CalendarRead:      {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
	},
	// RoleHealthDirector owns COUNTS (maintainer decision 2026-08-01) and is a DISTINCT role
	// from RolePCDirector -- Preventive Care and Health are separate departments in the org
	// model, and merging them is prohibited.
	//
	// Counts ownership is an EXTENSION of the handbook, recorded as a decision. What the
	// handbook does put on this desk, and what this grant therefore mirrors:
	//   - CountsRead: the census/herd-register surface this role is now accountable for.
	//   - GoatWriteIdentity: Responsibility 6 Tagging -- "Ensure all animals are correctly
	//     tagged (ear tags, RFID) at birth, purchase, and whenever tags are lost or replaced".
	//     This is the only written clause that puts population-changing events on the Health
	//     Director's desk, and it is the anchor for the counts extension.
	//   - GoatWriteHealth: Responsibilities 1-4 (observations, diagnosis, treatment tracking).
	//   - VerificationAct: Responsibility 5, the same double-verify-the-verifier duty.
	//
	// Deliberately NOT granted:
	//   - CountsWrite: capture is ground work (/app/counts/* is the operator's phone surface).
	//     Owning the census is not recording it.
	//   - CountsApproveLifecycle / CountsApproveShifting / CountsApproveAccess: birth/death
	//     admission sits with the CEO tier and shifting approval with the park head, per the
	//     existing comments on those permissions. Moving either onto this role is a SECOND
	//     undocumented extension (shifting is written to the BREEDING Director) and was not
	//     decided; it is listed as an open question rather than silently taken.
	//   - Any vaccination, weighing, or feed permission -- including every vaccination
	//     permission, precisely because health_director is NOT pc_director.
	RoleHealthDirector: {
		AppBootstrap: {}, AdminWebBootstrap: {},
		LocationsRead: {}, OperatorsRead: {},
		OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {},
		GoatRead: {}, GoatWriteIdentity: {}, GoatWriteHealth: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {},
		// DELIBERATELY NO CountsRead / CountsWrite. COUNTS IS AN OFF FEATURE (AGENTS.md): the
		// module is registered moduleStatusAvailable in workforce/app/bootstrap_copy.go and is
		// held back ONLY by counts.read / counts.write, which today only ceo_internal holds.
		// Granting counts.read here would light the Counts nav for this role and thereby switch
		// the feature on. health_director gets counts OWNERSHIP (it is the leadership recipient
		// for a counts proof, replacing the silent vaccination default) and NOT counts ACCESS
		// until the feature is deliberately turned on. Ownership and access are separate
		// decisions here.
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
		// The AUTHORED TREATMENT RULEBOOK (/health/config), maintainer decision 2026-08-06.
		//
		// This is the second documented extension to this role, and unlike the counts one it is
		// squarely inside the handbook: Health_Director.pdf Responsibilities 1-4 put observation,
		// diagnosis, treatment and treatment tracking on this desk, and the protocol IS the
		// treatment standard those responsibilities are carried out against. GoatWriteHealth above
		// already lets this role record a clinical fact about one animal; this lets it author the
		// standing course every animal with that disease is treated under.
		//
		// It does NOT come with any Preventive Care permission, and must not: pc_director and
		// health_director are separate departments and merging them is prohibited. Vaccination
		// protocol authoring stays on /config with ProtocolWrite, which this role does not hold.
		HealthConfigRead: {}, HealthConfigWrite: {},
		// CONFIRMING A DIAGNOSIS (maintainer decision 2026-08-14).
		//
		// The health SOP engine (backend/internal/health/diagnosis) is ADVISORY: it returns a
		// ranked proposal and a human confirms every Problem before a course opens. That
		// confirmation is the Health Director's defining job -- DIRECTOR_ENGINE.md puts "confirm
		// or override Problems" on this desk and nowhere else -- and it is the control that keeps
		// the engine advisory rather than autonomous.
		//
		// Before this grant the ONLY holders of HealthDiagnose were pc_director and
		// ceo_internal, so a PREVENTIVE CARE director was confirming Health diagnoses. That is
		// the cross-department merge this file forbids two comments above, and it was live.
		// Whether pc_director KEEPS HealthDiagnose is a separate maintainer decision and is
		// deliberately NOT changed here.
		//
		// HealthRead comes with it because HealthDiagnose is unusable without it: the work list
		// and the case detail (GET /app/health/work-items) are gated on HealthRead, so a
		// confirmer who cannot read the queue cannot see what they are confirming. It is also
		// what makes the weekly override review possible -- authoring the rulebook while blind
		// to the work done under it leaves the improvement loop with no input.
		//
		// Two are deliberately WITHHELD. HealthExecute: separation of duty -- the manager treats
		// from the card and this desk judges the result, so the same person must not both
		// confirm a diagnosis and record having administered it. HealthReport: DIRECTOR_ENGINE.md
		// says this role "does not fill the form or walk every animal"; raising a sick-goat
		// report stays with the field tiers that hold HealthReport.
		HealthRead: {}, HealthDiagnose: {},
		// CountsAlertsRead opens ONLY the Counts Alerts inbox (GET /app/counts/alerts) -- see its
		// doc comment above. It is deliberately NOT CountsRead/CountsWrite: COUNTS IS AN OFF
		// FEATURE and granting either of those would switch it on for this role.
		CountsAlertsRead: {},
	},
	// The whole role: three approval permissions, nothing else. See RoleCountsApprover's doc
	// comment for why this exists as its own role rather than as additions to pc_director /
	// growth_director.
	//
	// All three are present together deliberately. CountsApproveAccess is only the coarse route
	// gate and is documented as never widening access on its own -- it is granted to exactly the
	// roles holding at least one fine-grained approval permission, and the handler still applies
	// DecidableApprovalRequestTypes per row. Lifecycle (birth/death) and Shifting are both here
	// because the maintainer named these people as approvers of the queue, which is one queue
	// carrying all three request types; splitting them would have shown an approver rows they
	// could not decide.
	//
	// Nothing else belongs in this map. Every addition here silently widens what a per-person
	// authority grant carries, on every person already holding it.
	// RoleProcurementManager: the vendor register, and NOTHING else.
	//
	// It holds AdminWebBootstrap because the register is an admin-web screen and a role with no
	// bootstrap has nowhere to render. It holds ProcurementRead so the holder can see the
	// source-entry/intake screens their own suppliers feed into -- that permission is already held
	// by seven roles including operator and park_head, so it widens nothing.
	//
	// It deliberately does NOT hold ProcurementWrite or ProcurementReview: authoring the contact
	// book is not the same authority as accepting an arriving load of animals or passing a
	// pre-dispatch health decision. Those stay with the roles that already run intake.
	RoleProcurementManager: {
		AdminWebBootstrap: {},
		VendorRead:        {}, VendorWrite: {}, VendorFinanceRead: {},
		ProcurementRead: {},
	},
	RoleCountsApprover: {
		CountsApproveAccess:    {},
		CountsApproveLifecycle: {},
		CountsApproveShifting:  {},
	},
	RoleOperator: {
		GoatRead: {}, AppBootstrap: {}, TaskRead: {}, TaskExecute: {}, CalendarRead: {}, ProcurementRead: {}, ProcurementWrite: {},
		CountsWrite:     {},
		WeighingExecute: {},
		// Maintainer decision 2026-07-22: operators now see the Feed vertical on the phone. This
		// reverses the earlier "deliberately NOT granted to RoleOperator" note on the feed reads --
		// the operator dispatches and packs what the direction says, and the FeedPackingRead comment
		// above already anticipated this widening. ProtocolRead gates Feed Direction (the generated
		// dispatch sheet), FeedPackingRead gates the per-shed packing worklist. Both are READ-ONLY;
		// authoring the ration grid (feed_config.write) stays with the CEO/CXO tier and is NOT added.
		ProtocolRead:          {},
		FeedPackingRead:       {},
		FeedDirectionRead:     {},
		FeedDirectionComplete: {},
		FeedTransportRead:     {},
		HealthRead:            {}, HealthReport: {}, HealthExecute: {},
		// See VaccinationAlertsRead doc comment above: this is the operator's Alerts
		// tab feed only, NOT the shared ObligationRead/VaccinationRead admin bundle.
		VaccinationAlertsRead: {},
	},
	RoleCEOInternal: {
		GoatRead: {}, GoatWriteIdentity: {}, GoatWriteHealth: {},
		// The ONLY holder of the whole-pen cohort reclassification. See the constant's doc comment:
		// it applies immediately, with no approval and no proof, and flips kid/adult for the whole
		// pen. It is granted here and nowhere else.
		GoatReclassifyShedStage: {},
		LocationsRead:           {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, OperationsRepair: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ProtocolWrite: {}, ProtocolPublish: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		VaccinationOverviewRead:     {},
		VaccinationOverseeExecution: {},
		WeighingPlan:                {},
		WeighingMonitor:             {},
		// NOT WeighingExecute: the CEO plans weighing work and oversees it, and must never reach a
		// scan screen. Holding execute put a scannable surface in front of a planner who is assigned
		// no sheds, and the submit would be refused anyway because the write requires the caller to
		// be the shed's assignee. Reopen/close authority is WeighingMonitor and is unaffected.
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementWrite: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
		CountsWrite:            {},
		CountsRead:             {},
		CountsApproveLifecycle: {},
		CountsApproveShifting:  {},
		CountsApproveAccess:    {},
		// Founder/builder visibility invariant (AGENTS.md): the platform-owner leadership cohort must
		// hold the grants for every built visible module, so a founder account is never locked out of
		// the Feed Config screen it is expected to operate.
		FeedConfigRead:       {},
		FeedConfigWrite:      {},
		FeedPackingRead:      {},
		FeedDirectionRead:    {},
		FeedDirectionOversee: {},
		FeedTransportRead:    {},
		VerificationReview:   {},
		VerificationAct:      {},
		// Founder/builder visibility invariant, and the tenant-wide oversight filters (module
		// chips, capture-date range) on /verify -- CEO/CxO is exactly one of the two roles that
		// receives the unrestricted, cross-category branch of resolveVerifierCategories. See
		// VerificationOversee's doc comment.
		VerificationOversee: {},
		// Leadership keeps the date range it already had, now under its own capability.
		VerificationFilterByCaptureDate: {},
		// The VIDEO LOG on /verify (maintainer decision 2026-08-14), same founder/builder
		// visibility invariant. See VerificationEvidenceTimeline: a separate capability from the
		// oversight chrome above, and the verifier holds it too.
		VerificationEvidenceTimeline: {},
		HealthRead:                   {}, HealthReport: {}, HealthDiagnose: {}, HealthExecute: {},
		// The authored treatment rulebook (/health/config). Part of the founder/builder visibility
		// invariant above: the platform-owner cohort holds the grants for every built visible
		// module, so a founder is never locked out of a screen they are expected to operate.
		HealthConfigRead: {}, HealthConfigWrite: {},
		// The procurement vendor register (/procurement/vendors), including its payment
		// instruments. Founder/builder visibility invariant: the platform-owner cohort holds the
		// grants for every built visible module.
		VendorRead: {}, VendorWrite: {}, VendorFinanceRead: {},
	},
}

// DecidableApprovalRequestTypes maps a caller's roles onto the Counts approval request types they
// may decide.
//
// The decision endpoints are a single route pair addressed by request id, so the route-level
// permission check cannot tell a birth from a shifting -- the type lives in the stored row, not in
// the URL. This function is the authority the handler applies AFTER reading the request, and it is
// also what restricts the pending list so an approver is only shown work they can act on.
//
// A caller with neither permission gets an empty set, which fails closed: no listable rows and no
// decidable type.
func DecidableApprovalRequestTypes(roles []string) []string {
	types := make([]string, 0, 3)
	if RolesAuthorize(roles, []string{CountsApproveLifecycle}, false) {
		types = append(types, "birth", "death")
	}
	if RolesAuthorize(roles, []string{CountsApproveShifting}, false) {
		types = append(types, "shifting")
	}
	return types
}

type GrantSource interface {
	ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error)
	ActiveTenantGrants(ctx context.Context, userID, tenantID string) ([]ActiveGrant, error)
}

type ActiveGrant struct {
	Role      string
	ScopeType string
	ScopeID   string
}

type PendingEmailGrantClaim struct {
	TenantID        string
	UserID          string
	Email           string
	ExternalSubject string
	Issuer          string
	Source          string
	TraceID         string
}

type PendingEmailGrantResult struct {
	Matched         bool
	InsertedGrants  []ClaimedEmailGrant
	ExistingGrants  []ClaimedEmailGrant
	PendingGrantIDs []string
}

type ClaimedEmailGrant struct {
	PendingGrantID string `json:"pending_grant_id"`
	GrantID        string `json:"grant_id"`
	Role           string `json:"role"`
	ScopeType      string `json:"scope_type"`
	ScopeID        string `json:"scope_id"`
}

type PendingEmailGrantClaimer interface {
	ClaimPendingEmailGrant(ctx context.Context, claim PendingEmailGrantClaim) (PendingEmailGrantResult, error)
}

// registeredRoleOrigins records every role key whose permission set has been
// installed. It is seeded once from the rolePermissions literal (package-level
// vars are fully initialized before any init() runs, so this is deterministic
// regardless of file order) and then extended by registerRole.
var (
	registeredRoleOrigins   = map[string]struct{}{}
	registerRoleOriginsOnce sync.Once
)

// registerRole installs a role's permission set and is the ONLY supported way to
// do so from an init(). It PANICS on a role that already has an entry.
//
// This exists because a silent overwrite has now voided a declared permission
// TWICE. permissions_orgrole.go's init() used to reassign
// rolePermissions[RoleGrowthDirector] to a freshly built map; every permission
// present in the permissions.go literal but absent from that map became inert
// with no compile error, no test failure, and no runtime signal --
// WeighingOverseeOperators first, then WeighingPlan, which left the Growth
// Director 403'd out of the weighing planner routes they own. A guard that only
// re-added the missing permission would leave the next one to be dropped the
// same way; failing loudly at process start is what actually closes the class.
func registerRole(role string, perms map[string]struct{}) {
	registerRoleOriginsOnce.Do(func() {
		for existing := range rolePermissions {
			registeredRoleOrigins[existing] = struct{}{}
		}
	})
	if _, exists := registeredRoleOrigins[role]; exists {
		panic("permissions: role " + role + " is already declared; a second declaration would SILENTLY DROP every permission missing from the new set. Edit the single rolePermissions entry instead of reassigning it.")
	}
	registeredRoleOrigins[role] = struct{}{}
	rolePermissions[role] = perms
}

func RoleHasPermission(role, permission string) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	_, ok = perms[permission]
	return ok
}

func RolesAuthorize(roles []string, required []string, adminOnly bool) bool {
	if len(required) == 0 {
		return false
	}
	if adminOnly {
		for _, role := range roles {
			if isProductAdminRole(role) {
				return true
			}
		}
		return false
	}
	for _, permission := range required {
		allowed := false
		for _, role := range roles {
			if RoleHasPermission(role, permission) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

// RolesAuthorizeAny is the OR counterpart of RolesAuthorize: it reports whether
// the caller holds AT LEAST ONE of the listed permissions. An empty list denies,
// matching RolesAuthorize.
func RolesAuthorizeAny(roles []string, anyOf []string) bool {
	for _, permission := range anyOf {
		for _, role := range roles {
			if RoleHasPermission(role, permission) {
				return true
			}
		}
	}
	return false
}

func isProductAdminRole(role string) bool {
	return role == RoleCEOInternal
}
