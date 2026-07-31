package permissions

import "context"

const (
	RoleVerifier   = "verifier"
	RoleParkHead   = "park_head"
	RolePCDirector = "pc_director"
	// RoleGrowthDirector is the concrete grant key for the Growth Director business role.
	// Business role keys use name_director order (`growth_director`), matching `pc_director`.
	RoleGrowthDirector = "growth_director"
	RoleOperator       = "operator"
	RoleCEOInternal    = "ceo_internal"

	GoatRead                  = "goat.read"
	GoatWriteIdentity         = "goat.write_identity"
	GoatWriteHealth           = "goat.write_health"
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
	VaccinationVerify         = "vaccination.verify"
	VaccinationCampaign       = "vaccination.campaign"
	WeighingPlan              = "weighing.plan"
	WeighingMonitor           = "weighing.monitor"
	WeighingExecute           = "weighing.execute"
	CalendarRead              = "calendar.read"
	CalendarAction            = "calendar.action"
	ProcurementRead           = "procurement.read"
	ProcurementWrite          = "procurement.write"
	ProcurementReview         = "procurement.review"
	RosterRead                = "roster.read"
	RosterManage              = "roster.manage"
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
	// FeedDirectionComplete is the operator WRITE twin FeedPackingRead's comment anticipated: it gates
	// POST /feed-direction/complete, where an operator records that one shed-session's feed direction
	// was carried out (with optional video proof). It is deliberately separate from the feed reads --
	// reading the dispatch sheet is not the same authority as recording that the feeding happened --
	// and separate from feed_config.write, which authors the ration grid rather than executing it.
	//
	// Granted to the tiers that dispatch feed on the ground: RoleOperator (the phone operator who
	// walks the park), RoleParkHead (execution on their own ground), and RoleCEOInternal (the
	// founder/builder visibility invariant). NOT granted to RoleVerifier (checks captured work, does
	// not dispatch) or RolePCDirector (oversight, not execution).
	FeedDirectionComplete = "feed_direction.complete"
	// VerificationReview is the generic Verification vertical's queue-read + verdict-write
	// permission (context/architecture/verification-module-design.md). It is granted ONLY to the
	// Verifier role and, per the org-role-model truth table, as a CEO/CxO override — never to
	// Operator/Manager (capture) or Head/Director (act). Separation of duty: capturer != verifier.
	VerificationReview = "verification.review"
	VerificationAct    = "verification.act"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleVerifier: {
		GoatRead: {}, GoatWriteIdentity: {},
		LocationsRead: {}, LocationsReview: {},
		OperatorsRead: {}, AppBootstrap: {},
		TaskRead: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {},
		CalendarRead:    {},
		ProcurementRead: {}, ProcurementReview: {},
		RosterRead: {},
		// The Video Verification Team's exclusive permission (verification-module-design.md §2.4 /
		// org-role-model.md truth table): Verify media (approve/reject + reason). No other role holds
		// this except the CEO/CxO override above.
		VerificationReview: {},
	},
	RoleParkHead: {
		GoatRead:      {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, AppBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {},
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
		FeedDirectionComplete: {},
		VerificationAct:       {},
	},
	RolePCDirector: {
		GoatRead: {}, GoatWriteHealth: {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskExecute: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
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
	RoleGrowthDirector: {
		AppBootstrap: {}, AdminWebBootstrap: {},
		LocationsRead: {}, OperatorsRead: {},
		WeighingPlan: {}, WeighingMonitor: {}, WeighingExecute: {},
		CalendarRead: {},
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
		FeedDirectionComplete: {},
	},
	RoleCEOInternal: {
		GoatRead: {}, GoatWriteIdentity: {}, GoatWriteHealth: {},
		LocationsRead: {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, OperationsRepair: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ProtocolWrite: {}, ProtocolPublish: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		VaccinationOverviewRead: {},
		WeighingPlan:            {},
		WeighingMonitor:         {},
		WeighingExecute:         {},
		CalendarRead:            {}, CalendarAction: {},
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
		FeedConfigRead:        {},
		FeedConfigWrite:       {},
		FeedPackingRead:       {},
		FeedDirectionComplete: {},
		VerificationReview:    {},
		VerificationAct:       {},
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

func isProductAdminRole(role string) bool {
	return role == RoleCEOInternal
}
