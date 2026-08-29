package domain

// PersonSummary is one row of the People/HRMS directory: every workforce member
// with their park, department, and designation. Grain: workforce_member (one
// row per member, no fan-out joins — park/department resolve 1:1 off the
// member's own columns).
type PersonSummary struct {
	PersonID    string  `json:"person_id"`
	UserID      *string `json:"user_id"`
	FirstName   *string `json:"first_name"`
	LastName    *string `json:"last_name"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email"`
	Status      string  `json:"status"`
	// RoleHint is the member's primary role hint (operator, park_head,
	// pc_director, ...). The authoritative grant list lives on
	// user_scope_grants; this is the directory's display role.
	RoleHint string `json:"role_hint"`
	// DesignationGrade is the HR grade (cxo/director/manager/assistant_manager)
	// or empty when ungraded.
	DesignationGrade *string `json:"designation_grade"`
	ParkID           *string `json:"park_id"`
	ParkLabel        *string `json:"park_label"`
	DepartmentID     *string `json:"department_id"`
	DepartmentLabel  *string `json:"department_label"`
	CreatedAt        string  `json:"created_at"`
	RowVersion       int     `json:"row_version"`
	// ClockInTodayLabel is today's clock-in time (IST "08:12") when the person
	// has clocked in today, nil otherwise — the All People "Clocked in" chip
	// (docs/features/clock-in-out/plan.md). Backend-composed; render verbatim.
	ClockInTodayLabel *string `json:"clock_in_today_label"`

	// Proof-work statistics from the verification queue, at VERIFICATION ITEM
	// grain: one item = one submitted proof set (a lump-sum submission's several
	// clips are still ONE item). Withdrawn/superseded items are excluded from
	// every number — the work was redone, so neither side of a rate should count
	// it. Uploads = approved + rejected + pending + not-reviewed by construction.
	ProofUploads int `json:"proof_uploads"`
	// ProofApproved counts proofs a VERIFIER approved. It deliberately excludes
	// the ones the randomization policy settled without review (maintainer
	// decision 2026-08-26): those were never watched, so counting them here would
	// report an operator's work as checked-and-accepted when nobody looked at it,
	// and — worse — would dilute the rejection rate below by padding its
	// denominator with proofs no one judged.
	ProofApproved int `json:"proof_approved"`
	ProofRejected int `json:"proof_rejected"`
	ProofPending  int `json:"proof_pending"`
	// ProofNotReviewed counts proofs settled by the randomization policy rather
	// than by a person. It is reported rather than hidden because it is the honest
	// difference between "this operator's work was checked" and "this operator's
	// work was accepted": at a 40% share, most of a good operator's proofs land
	// here, and that is a fact about the POLICY, not about him.
	ProofNotReviewed int `json:"proof_not_reviewed"`
	// ProofRejectionPct = rejected / (approved + rejected), rounded to a whole
	// percent — the share of REVIEWED proofs that were rejected. Nil until at
	// least one verdict exists, so a new operator shows "no reviews yet" rather
	// than a fabricated 0%. Policy-settled proofs are on neither side of it.
	ProofRejectionPct *int `json:"proof_rejection_pct"`
}

// PeopleCatalog carries the business-managed vocabularies the directory's
// filters and the Add Person form need: real parks and departments from the
// database, never frontend constants.
type PeopleCatalog struct {
	Parks       []PeopleCatalogOption `json:"parks"`
	Departments []PeopleCatalogOption `json:"departments"`
}

type PeopleCatalogOption struct {
	ID    string `json:"id"`
	Code  string `json:"code"`
	Label string `json:"label"`
}

type PeopleListResponse struct {
	Items []PersonSummary `json:"items"`
	// NextCursor pages forward (keyset); empty means the listing is complete.
	NextCursor string        `json:"next_cursor"`
	Catalog    PeopleCatalog `json:"catalog"`
	TraceID    string        `json:"trace_id"`
}

// CreatePersonRequest creates a person AND their working login in one call:
// Firebase email/password account, active scope grant, allowlist admission, and
// the workforce_members row.
type CreatePersonRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	// Role is the RBAC role granted at creation (operator, park_head, verifier,
	// pc_director, growth_director, feed_director, health_director). Operator and
	// park head are park-scoped and REQUIRE ParkID; the rest are tenant-scoped.
	Role   string `json:"role"`
	ParkID string `json:"park_id"`
	// DepartmentID drives the mobile module nav via department_module_grants.
	DepartmentID     string `json:"department_id"`
	DesignationGrade string `json:"designation_grade"`
}

// PersonLogin reports the login-account outcome of a create.
type PersonLogin struct {
	Email string `json:"email"`
	// AccountStatus is "created" (a fresh Firebase account now exists with the
	// convention password) or "existing" (the email already had an account; its
	// password was NOT changed).
	AccountStatus string `json:"account_status"`
}

type PersonResponse struct {
	Person  PersonSummary `json:"person"`
	Login   PersonLogin   `json:"login"`
	TraceID string        `json:"trace_id"`
}
