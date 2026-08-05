package postgres

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// ReviewEventRepository implements ports.ReviewEventRepository against verification_review_events
// (migration 000112). Kept in its own small file/struct per the file-organization rule -- it is a
// distinct bounded concern (video-review analytics) from the verdict/queue Repository above, even
// though both live in the same package and share the pool.
type ReviewEventRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewReviewEventRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *ReviewEventRepository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &ReviewEventRepository{pool: pool, timeout: queryTimeout}
}

// InsertReviewEvents appends the batch with ON CONFLICT DO NOTHING keyed on
// (tenant_id, client_event_id) -- the idempotent-write contract for a client that may retry the
// same flush after a network blip. `inserted` is the number of ACTUALLY new rows (via RETURNING),
// not the batch size, so an exact replay reports 0 and a caller can prove no double-count.
//
// One INSERT ... SELECT * FROM UNNEST(...) rather than one INSERT per event: see
// docs/decisions/scale-anti-patterns.md -> "N+1 query". A verifier-review batch is small (bounded
// by the client flush interval), but the set-based form costs nothing extra and keeps the pattern
// uniform with every other bulk write path in this codebase.
func (r *ReviewEventRepository) InsertReviewEvents(ctx context.Context, batch domain.ReviewEventBatch) (int, error) {
	if len(batch.Events) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	itemIDs := make([]string, len(batch.Events))
	proofIDs := make([]*string, len(batch.Events))
	actorIDs := make([]string, len(batch.Events))
	sessionIDs := make([]string, len(batch.Events))
	eventTypes := make([]string, len(batch.Events))
	occurredAts := make([]time.Time, len(batch.Events))
	payloads := make([][]byte, len(batch.Events))
	clientEventIDs := make([]string, len(batch.Events))
	for i, e := range batch.Events {
		itemIDs[i] = e.ItemID
		proofIDs[i] = e.ProofID
		actorIDs[i] = e.ActorID
		sessionIDs[i] = e.SessionID
		eventTypes[i] = string(e.EventType)
		occurredAts[i] = e.OccurredAt
		payloadJSON, err := json.Marshal(e.Payload)
		if err != nil {
			return 0, err
		}
		payloads[i] = payloadJSON
		clientEventIDs[i] = e.ClientEventID
	}

	rows, err := r.pool.Query(ctx, `
INSERT INTO verification_review_events (
    tenant_id, item_id, proof_id, actor_id, session_id, event_type, occurred_at, payload, client_event_id
)
SELECT $1::uuid, item_id, proof_id, actor_id, session_id, event_type, occurred_at, payload::jsonb, client_event_id
FROM UNNEST(
    $2::uuid[], $3::uuid[], $4::uuid[], $5::text[], $6::text[], $7::timestamptz[], $8::text[], $9::uuid[]
) AS t(item_id, proof_id, actor_id, session_id, event_type, occurred_at, payload, client_event_id)
ON CONFLICT (tenant_id, client_event_id) DO NOTHING
RETURNING event_id`,
		batch.TenantID, itemIDs, proofIDs, actorIDs, sessionIDs, eventTypes, occurredAts, payloads, clientEventIDs,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	inserted := 0
	for rows.Next() {
		inserted++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return inserted, nil
}

type reviewEventRow struct {
	actorID    string
	eventType  string
	occurredAt time.Time
	positionMs *int64
	durationMs *int64
}

// ItemReviewFacts fetches every event for one item (bounded by verification_review_events_item_
// actor_time_idx -- one item's event count, not a whole-table scan) and derives, per actor:
//
//   - ProofDurationMs: max VideoDurationMs any event on that (item, actor) reported.
//   - WatchedDistinctMs: union of [position-derived] covered play spans, so replaying the same 2
//     seconds five times never counts as 10 seconds of watching. Computed by merging the played
//     intervals implied by consecutive play->pause/ended/seek transitions and summing the DISTINCT
//     covered length -- not by summing (pause_time - play_time) per pair, which double-counts
//     overlapping replays.
//   - PlayCount/PauseCount/SeekAttemptCount: raw event tallies.
//   - TimeToVerdictSeconds: verdict_recorded.occurred_at - item_opened.occurred_at.
//   - WatchedFull: WatchFraction >= WatchedFullThreshold.
func (r *ReviewEventRepository) ItemReviewFacts(ctx context.Context, tenantID, itemID string) ([]domain.ItemReviewFacts, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT actor_id, event_type, occurred_at,
       (payload->>'video_position_ms')::bigint,
       (payload->>'video_duration_ms')::bigint
FROM verification_review_events
WHERE tenant_id = $1::uuid AND item_id = $2::uuid
ORDER BY actor_id, occurred_at`, tenantID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byActor := map[string][]reviewEventRow{}
	order := []string{}
	for rows.Next() {
		var row reviewEventRow
		if err := rows.Scan(&row.actorID, &row.eventType, &row.occurredAt, &row.positionMs, &row.durationMs); err != nil {
			return nil, err
		}
		if _, ok := byActor[row.actorID]; !ok {
			order = append(order, row.actorID)
		}
		byActor[row.actorID] = append(byActor[row.actorID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	facts := make([]domain.ItemReviewFacts, 0, len(order))
	for _, actorID := range order {
		facts = append(facts, computeActorFacts(itemID, actorID, byActor[actorID]))
	}
	return facts, nil
}

// WatchedFullThreshold is the configurable bar for the "did they actually watch it" integrity
// signal. 0.9 (90%) tolerates a verifier who skipped the last few seconds of credits/blank frames
// without letting a rubber-stamped 10%-watched approval count as full.
const WatchedFullThreshold = 0.9

type interval struct{ startMs, endMs int64 }

func computeActorFacts(itemID, actorID string, rows []reviewEventRow) domain.ItemReviewFacts {
	facts := domain.ItemReviewFacts{ItemID: itemID, ActorID: actorID}
	var intervals []interval
	var playStart *int64
	var playStartAt time.Time

	for _, row := range rows {
		if row.durationMs != nil && *row.durationMs > facts.ProofDurationMs {
			facts.ProofDurationMs = *row.durationMs
		}
		switch domain.ReviewEventType(row.eventType) {
		case domain.ReviewEventItemOpened:
			t := row.occurredAt
			facts.ItemOpenedAt = &t
		case domain.ReviewEventVerdictRecorded:
			t := row.occurredAt
			facts.VerdictRecordedAt = &t
		case domain.ReviewEventVideoPlay:
			facts.PlayCount++
			if row.positionMs != nil {
				pos := *row.positionMs
				playStart = &pos
				playStartAt = row.occurredAt
			} else {
				zero := int64(0)
				playStart = &zero
				playStartAt = row.occurredAt
			}
		case domain.ReviewEventVideoPause, domain.ReviewEventVideoEnded:
			if domain.ReviewEventType(row.eventType) == domain.ReviewEventVideoPause {
				facts.PauseCount++
			}
			if playStart != nil {
				end := *playStart + row.occurredAt.Sub(playStartAt).Milliseconds()
				if row.positionMs != nil {
					end = *row.positionMs
				}
				if end > *playStart {
					intervals = append(intervals, interval{startMs: *playStart, endMs: end})
				}
				playStart = nil
			}
		case domain.ReviewEventVideoSeekAttempt:
			facts.SeekAttemptCount++
			if playStart != nil {
				// A seek interrupts the current play span at the point it happened.
				elapsed := *playStart + row.occurredAt.Sub(playStartAt).Milliseconds()
				if elapsed > *playStart {
					intervals = append(intervals, interval{startMs: *playStart, endMs: elapsed})
				}
				if row.positionMs != nil {
					pos := *row.positionMs
					playStart = &pos
				} else {
					playStart = nil
				}
				playStartAt = row.occurredAt
			}
		}
	}
	facts.WatchedDistinctMs = mergeIntervalsDistinctMs(intervals)
	if facts.ProofDurationMs > 0 {
		frac := float64(facts.WatchedDistinctMs) / float64(facts.ProofDurationMs)
		if frac > 1 {
			frac = 1
		}
		facts.WatchFraction = frac
		facts.WatchedFull = frac >= WatchedFullThreshold
	}
	if facts.ItemOpenedAt != nil && facts.VerdictRecordedAt != nil {
		secs := facts.VerdictRecordedAt.Sub(*facts.ItemOpenedAt).Seconds()
		facts.TimeToVerdictSeconds = &secs
	}
	return facts
}

// mergeIntervalsDistinctMs sums the DISTINCT covered length of a set of possibly-overlapping
// [start,end) millisecond spans -- this is what stops a verifier replaying the same 2 seconds of
// video from inflating watch time: two overlapping 2s replays merge into one 2s covered span, not
// 4s of summed duration.
func mergeIntervalsDistinctMs(spans []interval) int64 {
	if len(spans) == 0 {
		return 0
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].startMs < spans[j].startMs })
	var total int64
	curStart, curEnd := spans[0].startMs, spans[0].endMs
	for _, s := range spans[1:] {
		if s.startMs > curEnd {
			total += curEnd - curStart
			curStart, curEnd = s.startMs, s.endMs
			continue
		}
		if s.endMs > curEnd {
			curEnd = s.endMs
		}
	}
	total += curEnd - curStart
	return total
}
