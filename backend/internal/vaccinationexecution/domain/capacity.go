package domain

// ---- Vaccination drive capacity / planner status vocabulary ----
//
// The medical obligation kernel decides which vaccine obligations are due. Drive planning capacity is an
// operations concept: it is consumed by unique animals handled by available operators on a business date,
// not by vaccine doses or obligation cells. One animal with a same-day vaccine bundle consumes one
// operator animal slot. See app.OperatorDrivePlanner for the assignment model.

// CapacityStatus is the INTERNAL machine vocabulary. It must never be shown raw in the CEO UI — the
// frontend renders the backend-provided CEO label (within_cap -> "Within cap", over_cap -> "Split",
// capacity_breach -> "Capacity action" (add operators or finish over cap inside the safe window).
type CapacityStatus string

const (
	CapacityWithinCap CapacityStatus = "within_cap"      // fits in a single day (sessions <= 1)
	CapacityOverCap   CapacityStatus = "over_cap"        // safely split across multiple days within the window
	CapacityBreach    CapacityStatus = "capacity_breach" // cannot fit within the authored cap/safe window; add operators or finish over cap
)

// CapacityConfig is the backend-owned drive cap config (vaccination_capacity_config). MaxPerDay is the
// animal cap used by an available vaccination operator on one business date. CapacityScope is where the
// cap number is defined (tenant now; center/shed later). OverflowPolicy documents what the planner does
// past the cap. RowVersion is the optimistic-concurrency token for admin edits (bumped on each update);
// a stale RowVersion on update is rejected as a conflict so concurrent admins never silently clobber each
// other.
type CapacityConfig struct {
	MaxPerDay      int    `json:"maxPerDay"`
	CapacityScope  string `json:"capacityScope"`
	MaxBufferDays  int    `json:"maxBufferDays"`
	OverflowPolicy string `json:"overflowPolicy"`
	RowVersion     int    `json:"rowVersion"`
	// MaxShotsPerAnimalPerDrive is an admin-editable override of the same-day per-animal shot cap
	// (migration 000045). nil means "no override" -- the planner falls back to the published rule_dsl
	// drive_policy value / code default (domain.DefaultMaxShotsPerAnimalPerDrive in the obligation
	// package). Non-nil must be >= 1 (see Validate).
	MaxShotsPerAnimalPerDrive *int `json:"maxShotsPerAnimalPerDrive"`
}

// Valid capacity-config vocabularies (mirror the DB CHECK constraints in migration 000155). Exposed so
// the admin-ui contract + validation share one source. Only 'tenant' scope is honored by the planner
// today (the SQL uses a single tenant-wide cap); center/shed are accepted by the schema for later use.
var (
	CapacityScopes    = []string{"tenant", "center", "shed"}
	OverflowPolicies  = []string{"split_within_safe_window_last_safe_may_exceed_cap"}
	MaxPerDayCeiling  = 200 // hard operator-day cap for vaccination drives.
	MaxBufferDaysCeil = 60  // guardrail: a safe window longer than this is not a business buffer.
)

// Validate checks an admin-submitted capacity config against the same rules the DB enforces, so a bad
// value returns a clean 400 instead of a raw constraint-violation 500. Returns a machine code + message.
func (c CapacityConfig) Validate() (code, message string, ok bool) {
	if c.MaxPerDay < 1 || c.MaxPerDay > MaxPerDayCeiling {
		return "invalid_max_per_day", "max animals per operator per day must be between 1 and 200", false
	}
	if c.MaxBufferDays < 0 || c.MaxBufferDays > MaxBufferDaysCeil {
		return "invalid_max_buffer_days", "max buffer days must be between 0 and 60", false
	}
	if !contains(CapacityScopes, c.CapacityScope) {
		return "invalid_capacity_scope", "capacity scope must be one of: tenant, center, shed", false
	}
	if !contains(OverflowPolicies, c.OverflowPolicy) {
		return "invalid_overflow_policy", "overflow policy must be split_within_safe_window_last_safe_may_exceed_cap", false
	}
	if c.MaxShotsPerAnimalPerDrive != nil && *c.MaxShotsPerAnimalPerDrive < 1 {
		return "invalid_max_shots_per_animal_per_drive", "max shots per animal per drive must be at least 1 (or omitted to clear the override)", false
	}
	return "", "", true
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// DefaultCapacityConfig is the code fallback for a tenant with no config row yet (migration 000155 seeds
// existing tenants; this covers tenants created afterward). Matches the maintainer-set seed.
func DefaultCapacityConfig() CapacityConfig {
	return CapacityConfig{
		MaxPerDay:      200,
		CapacityScope:  "tenant",
		MaxBufferDays:  7,
		OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap",
	}
}

// PlannedSession is the legacy shed-level session shape still returned by older read paths. New drive
// planning should prefer operator/date assignments from OperatorDrivePlanner because sessions alone cannot
// express operator availability, shed/partition ownership, or animal-bundle capacity.
type PlannedSession struct {
	Date         string         `json:"date"`         // Asia/Kolkata business date, YYYY-MM-DD
	Vaccinations int            `json:"vaccinations"` // legacy field name; use as animal work count in new paths
	DailyLimit   int            `json:"dailyLimit"`   // the configured animal cap
	Capacity     CapacityStatus `json:"capacity"`     // within_cap | capacity_breach
}
