package domain

// Per-person module access, as the People/HRMS access editor sees it.
//
// Every visible word here is composed by the backend: module labels, capability
// labels, the blurb under each, park names, designation names, and the
// separation-of-duty warnings. The screen renders them verbatim. That is the
// copy firewall, and it matters more than usual on this screen -- the raw
// vocabulary is `aas_health`, `oversee`, `pc_care`, and none of those are words
// to put in front of someone deciding what a colleague may do.

// AccessCapabilityOption is one capability chip in the editor.
type AccessCapabilityOption struct {
	// Level is the stored value ("view" / "do" / "oversee" / "configure").
	Level string `json:"level"`
	Label string `json:"label"`
	Blurb string `json:"blurb"`
}

// AccessModuleRow is one row of the editor: a module, what it offers on each
// surface, and what this person currently holds there.
//
// Offered and granted are kept as separate lists per surface rather than one
// list of {level, offered, granted} objects, because the editor's two cells are
// independent and a level offered on web is frequently not offered on mobile.
type AccessModuleRow struct {
	ModuleKey string `json:"module_key"`
	Label     string `json:"label"`
	Blurb     string `json:"blurb"`
	// OfferedWeb/OfferedMobile are the levels this module can be held at on that
	// surface, in display order. EMPTY means the module does not exist on that
	// surface at all, and the editor renders the cell as unavailable rather than
	// as an empty set of chips -- "Feed Config is web-only" and "nobody has ticked
	// anything yet" are different answers.
	OfferedWeb    []string `json:"offered_web"`
	OfferedMobile []string `json:"offered_mobile"`
	// GrantedWeb/GrantedMobile are what this person holds today.
	GrantedWeb    []string `json:"granted_web"`
	GrantedMobile []string `json:"granted_mobile"`
	// Pages are the module's individually tickable admin-web screens (page-grain
	// access, maintainer decision 2026-08-27), in sidebar order. EMPTY means the
	// module has no admin-web page of its own -- it is phone-only, or reached from
	// inside another screen -- and the editor renders no page list at all.
	Pages []AccessPageOption `json:"pages"`
	// GrantedPagesWeb are the pages this person keeps. It is meaningful ONLY on the
	// web surface: the phone composes its own navigation and is untouched by page
	// ticks. An empty list on a module the person HOLDS means every page (see
	// permissions.PageAccessForAssignments), which is what makes a page shipped
	// tomorrow reach them rather than nobody -- the backend expands it before
	// sending, so the editor always renders explicit ticks.
	GrantedPagesWeb []string `json:"granted_pages_web"`
}

// AccessPageOption is one tickable admin-web screen inside a module. The label is
// the SIDEBAR label, so the tick reads as the thing the person will actually see.
type AccessPageOption struct {
	PageKey string `json:"page_key"`
	Label   string `json:"label"`
}

// AccessParkOption is a selectable park.
type AccessParkOption struct {
	ParkID string `json:"park_id"`
	Label  string `json:"label"`
}

// AccessDesignationOption is a job title that can pre-fill the access rows.
type AccessDesignationOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Grade string `json:"grade,omitempty"`
}

// AccessWarning is a backend-composed caution about the CURRENT saved access.
// It never blocks a save: the maintainer's position is that this authority is
// theirs to grant. It exists so granting it is a decision rather than an
// accident -- under the retired role model the combination was structurally
// impossible, so nobody ever had to think about it.
type AccessWarning struct {
	ModuleKey string `json:"module_key"`
	Message   string `json:"message"`
}

// PersonAccessResponse is the whole editor payload in one read: the vocabulary,
// the options, and this person's current answer.
type PersonAccessResponse struct {
	PersonID    string `json:"person_id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
	// DesignationCode records which job title pre-filled this access. Advisory:
	// it never overrides the rows, and it is EMPTY for someone whose access was
	// composed by hand or migrated from stacked roles.
	DesignationCode string `json:"designation_code,omitempty"`
	// ScopeMode is "tenant" (every park) or "parks" (the ParkIDs below).
	ScopeMode string   `json:"scope_mode"`
	ParkIDs   []string `json:"park_ids"`

	Modules      []AccessModuleRow         `json:"modules"`
	Capabilities []AccessCapabilityOption  `json:"capabilities"`
	Parks        []AccessParkOption        `json:"parks"`
	Designations []AccessDesignationOption `json:"designations"`
	Warnings     []AccessWarning           `json:"warnings"`

	// RowVersion fences a concurrent save: two admins editing the same person
	// must not silently overwrite each other, and access is exactly the kind of
	// state where a lost update is invisible until someone cannot do their job.
	RowVersion int    `json:"row_version"`
	TraceID    string `json:"trace_id,omitempty"`
}

// AccessModuleWrite is one module row being saved.
type AccessModuleWrite struct {
	ModuleKey string   `json:"module_key"`
	Web       []string `json:"web"`
	Mobile    []string `json:"mobile"`
	// Pages narrows the WEB grant to specific screens of this module. The editor
	// sends every page it rendered, for the same wholesale reason the modules list
	// does: a missing key could not be told apart from a deliberate removal.
	Pages []string `json:"pages"`
}

// SavePersonAccessRequest replaces a person's access wholesale.
//
// WHOLESALE, not a patch: the editor always sends every module row it rendered,
// so an unticked module arrives as an empty list rather than as a missing key.
// A patch shape cannot distinguish "leave this alone" from "remove this", and on
// an access screen that ambiguity resolves into someone keeping authority the
// admin believed they had removed.
type SavePersonAccessRequest struct {
	DesignationCode string              `json:"designation_code"`
	ScopeMode       string              `json:"scope_mode"`
	ParkIDs         []string            `json:"park_ids"`
	Modules         []AccessModuleWrite `json:"modules"`
	RowVersion      int                 `json:"row_version"`
}

// DesignationDefaultsResponse is what picking a designation pre-fills, so the
// editor can apply it without a second round trip per module.
type DesignationDefaultsResponse struct {
	Code    string              `json:"code"`
	Label   string              `json:"label"`
	Modules []AccessModuleWrite `json:"modules"`
	TraceID string              `json:"trace_id,omitempty"`
}
