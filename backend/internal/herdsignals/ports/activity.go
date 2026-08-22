package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// TagActivityScope is everything the farm-activity read needs to know about ONE tag before it
// may look at a single farm record: which animal is behind it (if any), which shed that animal
// is in, and -- decisively -- the instant the tag became that animal's tag.
type TagActivityScope struct {
	TagID  string
	TagMAC string
	// GoatID is empty when the tag resolves to no active smart-tag-capable identifier. An
	// empty GoatID means NO farm activity may be produced at all (migration 000197): the
	// packets are device telemetry and there is no animal to attribute anything to.
	GoatID   string
	ShedID   string
	ShedName string
	ParkName string
	// MonitoringSince is goat_identifiers.smart_tag_mapped_at (authoritative), falling back to
	// the denormalised herd_signal_tag_latest.animal_monitoring_since. Nil means the boundary
	// is unknown, which the caller must treat as fail-closed -- not as "since forever".
	MonitoringSince *time.Time
}

// ActivityReader reads the farm records that may be shown beside a tag's movement history.
//
// Both methods are tenant-scoped, time-bounded and count-bounded by contract. ListFarmActivity
// is deliberately several small indexed queries unioned in Go rather than one cross-module
// compute-on-read CTE (AGENTS.md scale anti-patterns): each source has its own index and its
// own grain, and a join that hides those differences would also hide that a feed record is
// about a shed and a weighing record is about a scanned string.
type ActivityReader interface {
	// GetTagActivityScope resolves the tag's animal, shed and monitoring boundary.
	// Returns nil, nil when the tenant has no such tag.
	GetTagActivityScope(ctx context.Context, tenantID, tagID string) (*TagActivityScope, error)

	// ListFarmActivity returns the recorded farm activity for the scope's animal (and its shed,
	// where the record is shed-grain) within [from, to], ordered by instant ascending, capped at
	// limit+1 rows so the caller can report truncation honestly. `from` is ALREADY clamped to
	// the monitoring boundary by the caller; this method must never be handed an unclamped
	// window.
	ListFarmActivity(ctx context.Context, tenantID string, scope TagActivityScope, from, to time.Time, limit int) ([]domain.ActivityEvent, error)
}
