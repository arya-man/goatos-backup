package ports

import "context"

// ApprovalDisplayNames is the id -> human-name lookup the approvals queue needs to render a row
// without showing anyone a UUID.
//
// Both maps are id -> name and both are ALWAYS non-nil, so a caller composing copy can index them
// directly. A missing id is a missing key, never an error: name enrichment degrades the copy, it
// never fails the queue read.
type ApprovalDisplayNames struct {
	// Locations is location_id -> park/shed name, for the sheds a shifting request names.
	Locations map[string]string
	// People is user_id -> the person's display name, for whoever raised the request.
	People map[string]string
	// AnimalLocations is goat_id -> that animal's CURRENT operational location ("park, shed" or
	// "park, shed partition" -- see internal/platform/oploc), for a death row's location clause.
	// Resolved from the animal's live goats/goat_shed_partitions row, not from any request
	// payload: a death is terminal and never moves goats.shed_id, so the animal's current row is
	// still its location at time of death.
	AnimalLocations map[string]string
}

// ApprovalNameResolver turns the ids stored on an approval request into the names an approver
// actually reads.
//
// GOLDEN FRONTEND RULE (AGENTS.md): visible labels are backend-owned. The approvals queue used to
// hand the phone a raw `raised_by_user_id` and a `destination_shed_id`, and the Android client
// rendered them verbatim -- "Raised by 7f3a91c2-4d18-..." and "to shed 0b4e-...". Admin-web avoided
// this by resolving location ids to names in the renderer and dropping "Raised by" entirely, which
// left the two surfaces disagreeing about what the same row says. Resolving here makes the backend
// the single owner of that copy for both.
//
// SCALE: implementations MUST resolve the whole page in a bounded number of batched queries -- one
// per entity kind, over `= ANY($n::uuid[])` with the indexed column left bare. A per-row lookup
// would be the banned N+1 fan-out (docs/decisions/scale-anti-patterns.md): the queue page is capped
// at 20 rows, so a naive implementation turns one screen into 41 serial reads.
type ApprovalNameResolver interface {
	// ResolveApprovalNames returns names for the given ids within tenantID. Ids may repeat and may
	// be blank; implementations dedupe and drop blanks. An empty input returns empty maps without
	// touching the database.
	ResolveApprovalNames(ctx context.Context, tenantID string, locationIDs, userIDs, goatIDs []string) (ApprovalDisplayNames, error)
}
