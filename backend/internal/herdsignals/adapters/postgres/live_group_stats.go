package postgres

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// livePenMediansSQL: %s = tagLocationJoin, %s = herdSignalsLiveFilter WHERE clause.
const livePenMediansSQL = `
		SELECT g.shed_id::text,
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY tl.motion_delta::float8)
		         FILTER (WHERE tl.motion_delta IS NOT NULL AND tl.gap_delta IS NOT TRUE),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY tl.tag_temperature_c::float8)
		         FILTER (WHERE tl.tag_temperature_c IS NOT NULL)
		FROM public.herd_signal_tag_latest tl
		%s
		%s
		  AND tl.mapping_state = 'mapped'
		  AND g.shed_id IS NOT NULL
		GROUP BY g.shed_id
	`

// ListLivePenMedians returns, per pen (goats.shed_id of the mapped animal), the median 15m
// motion_delta of non-gap tags and the median tag temperature across the WHOLE filtered live
// cohort in ONE aggregate query.
//
// GET /herd-signals/live used to walk every tag in 5k keyset pages (up to 50k rows) and run the
// full batched enrichment (identifier resolve, goats, pens, 24h baselines, battery history) over
// all of them only to derive these two medians for the page's pen-group comparison. The medians
// only need tag_latest + the mapped animal's pen, so they are aggregated here instead.
//
// Membership matches the old in-memory path: same herdSignalsLiveFilter predicate WITHOUT
// movement_state (listAllTagsLatest passed nil for it), mapping_state = 'mapped' (enrichment only
// resolves mapped rows to an animal, so only those ever carried a ShedID), and a non-null pen.
func (r *Repository) ListLivePenMedians(ctx context.Context, tenantID string, parkID, shedID, liveState, mappingState, pattern, q *string) (map[string]ports.PenMedians, error) {
	where, args, _ := herdSignalsLiveFilter(tenantID, parkID, shedID, nil, liveState, mappingState, pattern, q)
	// projection-review: membership=herd_signal_tag_latest rows matching the live filter (park/shed/live_state/mapping_state/pattern/q, never movement_state or cursor/limit) whose tag is mapped to an active smart-tag-capable animal with a pen; group_key=goats.shed_id; join_cardinality=tagLocationJoin is LATERAL LIMIT 1 to one goat and goats/locations are joined on their primary keys, so each tag_latest row contributes at most one row; pagination=whole-filter aggregate independent of the live page size and cursor; scope=tl.tenant_id plus the shared herdSignalsLiveFilter predicate
	query := fmt.Sprintf(livePenMediansSQL, tagLocationJoin, where)
	bound := sqlbind.MustBind(query, args...)
	rows, err := r.db.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ports.PenMedians)
	for rows.Next() {
		var pen string
		var m ports.PenMedians
		if err := rows.Scan(&pen, &m.MotionMedian, &m.TempMedian); err != nil {
			return nil, err
		}
		out[pen] = m
	}
	return out, rows.Err()
}
