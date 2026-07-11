package domain

import "errors"

// ErrCapacityConfigStale is returned by an update whose expected RowVersion no longer matches the stored
// row (a concurrent admin already changed it). The caller must re-read and retry — never blind-overwrite.
var ErrCapacityConfigStale = errors.New("vaccination capacity config: stale row version")

// ---- Vaccination capacity / session-splitting planner ----
//
// A shed's due vaccination work is split into planned SESSIONS (visits/days) when the daily vaccination
// cap is exceeded. Capacity counts VACCINATIONS (obligation cells), not animals: one goat receiving FMD
// + HS = 2 vaccinations. The planner is deterministic: sessions = ceil(open cells / cap), spread over
// consecutive days from the shed's next due date; work that cannot fit within (maxBufferDays + 1) days
// is CapacityBreach ("Needs review").

// CapacityStatus is the INTERNAL machine vocabulary. It must never be shown raw in the CEO UI — the
// frontend renders the backend-provided CEO label (within_cap -> "Within cap", over_cap -> "Split",
// capacity_breach -> "Needs review").
type CapacityStatus string

const (
	CapacityWithinCap CapacityStatus = "within_cap"      // fits in a single day (sessions <= 1)
	CapacityOverCap   CapacityStatus = "over_cap"        // safely split across multiple days within the window
	CapacityBreach    CapacityStatus = "capacity_breach" // cannot fit within the safe window -> needs manager review
)

// CapacityConfig is the backend-owned daily cap config (vaccination_capacity_config). CapacityScope is
// where the cap number is defined (tenant now; center/shed later). OverflowPolicy documents what the
// planner does past the cap. RowVersion is the optimistic-concurrency token for admin edits (bumped on
// each update); a stale RowVersion on update is rejected as a conflict so concurrent admins never
// silently clobber each other.
type CapacityConfig struct {
	MaxPerDay      int    `json:"maxPerDay"`
	CapacityScope  string `json:"capacityScope"`
	MaxBufferDays  int    `json:"maxBufferDays"`
	OverflowPolicy string `json:"overflowPolicy"`
	RowVersion     int    `json:"rowVersion"`
}

// Valid capacity-config vocabularies (mirror the DB CHECK constraints in migration 000155). Exposed so
// the admin-ui contract + validation share one source. Only 'tenant' scope is honored by the planner
// today (the SQL uses a single tenant-wide cap); center/shed are accepted by the schema for later use.
var (
	CapacityScopes    = []string{"tenant", "center", "shed"}
	OverflowPolicies  = []string{"split_within_safe_window_then_mark_needs_review"}
	MaxPerDayCeiling  = 100000 // guardrail: a daily cap above this is almost certainly a typo, not a real limit.
	MaxBufferDaysCeil = 60     // guardrail: a safe window longer than this is not a business buffer.
)

// Validate checks an admin-submitted capacity config against the same rules the DB enforces, so a bad
// value returns a clean 400 instead of a raw constraint-violation 500. Returns a machine code + message.
func (c CapacityConfig) Validate() (code, message string, ok bool) {
	if c.MaxPerDay < 1 || c.MaxPerDay > MaxPerDayCeiling {
		return "invalid_max_per_day", "max vaccinations per day must be between 1 and 100000", false
	}
	if c.MaxBufferDays < 0 || c.MaxBufferDays > MaxBufferDaysCeil {
		return "invalid_max_buffer_days", "max buffer days must be between 0 and 60", false
	}
	if !contains(CapacityScopes, c.CapacityScope) {
		return "invalid_capacity_scope", "capacity scope must be one of: tenant, center, shed", false
	}
	if !contains(OverflowPolicies, c.OverflowPolicy) {
		return "invalid_overflow_policy", "overflow policy must be split_within_safe_window_then_mark_needs_review", false
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
		MaxPerDay:      100,
		CapacityScope:  "tenant",
		MaxBufferDays:  3,
		OverflowPolicy: "split_within_safe_window_then_mark_needs_review",
	}
}

// PlannedSession is one planned vaccination day for a shed: the date, how many vaccinations (cells) are
// planned that day (<= DailyLimit), the daily cap, and whether that day sits within the safe window
// (within_cap) or spills past it (capacity_breach). Per-session Capacity is never over_cap — "split" is
// a shed-level headline, not a single-day state.
type PlannedSession struct {
	Date         string         `json:"date"`         // Asia/Kolkata business date, YYYY-MM-DD
	Vaccinations int            `json:"vaccinations"` // cells planned that day
	DailyLimit   int            `json:"dailyLimit"`   // the configured cap
	Capacity     CapacityStatus `json:"capacity"`     // within_cap | capacity_breach
}
