package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type shiftingFeedQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// loadShiftingFeedRequirements resolves every high-priority event in one set-based query.
//
// projection-review: membership=request-captured goat_ids for selected shifting events resolved against destination feed config; group_key=tenant_id shifting_event_id and feed_item_key; join_cardinality=request is one per event and goats are pre-aggregated by event, effective stage, pen and breed before config joins, so the config joins are label lookups onto an already-aggregated grain and cannot fan out; pagination=bounded explicit event-id batch with no page-local totals; scope=tenant_id destination park physical shed destination partition effective management stage and as-of date
// producer_unique=(tenant_id, shifting_event_id) in shifting_events;
// consumer_group=(tenant_id, shifting_event_id, feed_item_key). The selected approval request is
// one row per event through the bounded LATERAL selector; goat_ids is expanded then PRE-AGGREGATED
// to (event, breed) before joining config. Session items are DISTINCT per (event,item), rates are
// unique at (tenant,park,group,tag,item,valid_from) for the as-of window, and shed factors are unique
// at (tenant,park,shed,item,valid_from). Numerator and denominator both range over the exact goat_ids
// captured by that selected approval request. No page-local or preview animal set enters quantity.
func loadShiftingFeedRequirements(
	ctx context.Context, q shiftingFeedQueryer, tenantID string, eventIDs []string, asOf time.Time,
) (map[string]domain.ShiftingFeedRequirement, error) {
	out := make(map[string]domain.ShiftingFeedRequirement, len(eventIDs))
	if len(eventIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
WITH events AS (
    SELECT se.shifting_event_id, se.destination_park_id, se.destination_shed_id,
           CASE
             WHEN se.destination_partition_label IS NULL OR btrim(se.destination_partition_label) = '' THEN 'whole'
             ELSE feed_config_norm(se.destination_partition_label)
           END AS destination_partition_key,
           nullif(btrim(se.target_management_stage), '') AS target_stage,
           car.payload
    FROM shifting_events se
    LEFT JOIN LATERAL (
        SELECT ar.payload
        FROM counts_approval_requests ar
        WHERE ar.tenant_id = se.tenant_id AND ar.shifting_event_id = se.shifting_event_id
          AND ar.status IN ('pending','approved')
        ORDER BY CASE ar.status WHEN 'approved' THEN 0 ELSE 1 END, ar.raised_at DESC
        LIMIT 1
    ) car ON true
    WHERE se.tenant_id = $1::uuid AND se.shifting_event_id = ANY($2::uuid[])
), grains AS (
    SELECT e.shifting_event_id, e.destination_park_id, e.destination_shed_id, e.target_stage,
           -- EFFECTIVE STAGE = the stage these animals will actually be in after the move, which is
           -- the only honest thing to price a ration against.
           --
           -- target_stage is blank whenever counts/domain.ResolveShiftingDestinationStage declines to
           -- adopt a destination cohort: an EMPTY pen, a pen holding more than one cohort, a Flushing
           -- pen, or a cohort the relocation cannot write. Blank there means "keep each animal's
           -- current stage" -- it is a normal outcome, not a missing input, and the raiser is never
           -- asked for a stage (maintainer decision 2026-08-03). Keying the ration off target_stage
           -- alone therefore hard-blocked every high-priority movement into an empty pen with
           -- "selected destination management stage is missing", naming a choice the phone does not
           -- offer. Falling back to the animal's own management_stage prices exactly the cohort the
           -- animal keeps (maintainer decision 2026-08-12).
           --
           -- Per ANIMAL, not per event: a movement may carry two cohorts, and each is priced on its
           -- own stage. The grain already groups by animal attributes, so this adds no fan-out.
           COALESCE(e.target_stage, nullif(btrim(g.management_stage), '')) AS effective_stage,
           -- projection-review: membership=the goats named by the movement, joined 1:{0,1} to their own goat_shed_partitions row so a requirement line counts each animal once; group_key=the existing requirement grain PLUS the normalized partition key, so a feed requirement is computed per pen rather than smeared across a whole shed; join_cardinality=feed-config and location joins are primary-key label lookups, 1:{0,1}, no fan-out onto animals; pagination=none, a movement's requirement set is bounded by its own animal list; scope=tenant plus the shifting event being priced
           regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') AS partition_key,
           feed_config_norm(g.breed) AS breed_key, count(*)::bigint AS head_count
    FROM events e
    LEFT JOIN LATERAL jsonb_array_elements_text(COALESCE(e.payload->'goat_ids','[]'::jsonb)) gid ON true
    LEFT JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = gid::uuid
    -- 1:{0,1} per animal (goat_shed_partitions PK is (tenant_id, goat_id)) -- no fan-out.
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1::uuid AND gsp.goat_id = g.goat_id
    GROUP BY e.shifting_event_id, e.destination_park_id, e.destination_shed_id, e.target_stage,
             COALESCE(e.target_stage, nullif(btrim(g.management_stage), '')),
             regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', ''),
             feed_config_norm(g.breed)
), resolved AS (
    SELECT g.*, tag.shed_tag_id, tag.shed_tag_label, tag.shed_tag_key, tag.applies_to,
           rg.ration_group_id,
           CASE WHEN tag.applies_to = 'kid' THEN 'Kid' ELSE rg.ration_group_label END AS ration_group_label,
           feed_config_norm(CASE WHEN tag.applies_to = 'kid' THEN 'Kid' ELSE rg.ration_group_label END) AS ration_group_key,
           tag.updated_at AS tag_updated_at, rg.updated_at AS group_updated_at
    FROM grains g
    LEFT JOIN feed_shed_tags tag
      ON tag.tenant_id = $1::uuid AND tag.shed_tag_key = feed_config_norm(g.effective_stage)
     AND tag.status = 'active'
    LEFT JOIN feed_ration_groups rg
      ON rg.tenant_id = $1::uuid AND rg.breed_key = g.breed_key
), planned AS (
    SELECT e.shifting_event_id, sti.feed_item_key, min(sti.feed_item_label) AS feed_item_label,
           string_agg(st.session_template_id::text || '/' || sti.session_template_item_id::text,
                      ',' ORDER BY sti.session_no, sti.slot_no) AS session_template_item_ids,
           max(greatest(st.updated_at, sti.updated_at)) AS updated_at
    FROM events e
    JOIN feed_session_template_items sti
      ON sti.tenant_id = $1::uuid AND sti.park_id = e.destination_park_id
     AND sti.status = 'active' AND sti.valid_from <= $3::date
     AND (sti.valid_to IS NULL OR sti.valid_to > $3::date)
    JOIN feed_session_templates st
      ON st.tenant_id=sti.tenant_id AND st.park_id=sti.park_id AND st.session_no=sti.session_no
     AND st.status='active'
    GROUP BY e.shifting_event_id, sti.feed_item_key
), cells AS (
    SELECT e.shifting_event_id, e.target_stage, r.effective_stage, r.partition_key,
           r.breed_key, r.head_count,
           r.shed_tag_id, r.ration_group_id, r.ration_group_key,
           p.feed_item_key, p.feed_item_label, p.session_template_item_ids,
           rate.ration_rate_id, rate.grams_per_head, rate.updated_at AS rate_updated_at,
           factor.shed_factor_id, COALESCE(factor.multiplier, 1.0) AS multiplier,
           factor.updated_at AS factor_updated_at,
           r.tag_updated_at, r.group_updated_at, p.updated_at AS item_updated_at,
           EXISTS (
               SELECT 1 FROM feed_experiment_config x
               WHERE x.tenant_id=$1::uuid AND x.park_id=e.destination_park_id
                 AND x.shed_id=e.destination_shed_id
                 -- Experiment membership is an OPERATIONAL-LOCATION fact. One physical shed can
                 -- contain both experiment and ordinary pens, so omitting the destination partition
                 -- makes a sibling experiment allocation block the wrong movement (Yashoda 1-4
                 -- leaked onto Yashoda 10). x.partition_key stays bare for the natural-key index.
                 AND x.partition_key=e.destination_partition_key
                 AND x.status='active'
           ) AS has_experiment
    FROM events e
    LEFT JOIN resolved r ON r.shifting_event_id=e.shifting_event_id
    LEFT JOIN planned p ON p.shifting_event_id=e.shifting_event_id
    LEFT JOIN feed_ration_rates rate
      ON rate.tenant_id=$1::uuid AND rate.park_id=e.destination_park_id
     -- Same effective stage the shed tag was resolved from. Keying the RATE off e.target_stage while
     -- the TAG came from the animal's own stage would price one cohort against another's grid.
     AND rate.ration_group_key=r.ration_group_key AND rate.shed_tag_key=feed_config_norm(r.effective_stage)
     AND rate.feed_item_key=p.feed_item_key AND rate.valid_from <= $3::date
     AND (rate.valid_to IS NULL OR rate.valid_to > $3::date)
    LEFT JOIN feed_shed_factors factor
      ON factor.tenant_id=$1::uuid AND factor.park_id=e.destination_park_id
     AND factor.shed_id=e.destination_shed_id AND factor.feed_item_key=p.feed_item_key
     AND factor.valid_from <= $3::date AND (factor.valid_to IS NULL OR factor.valid_to > $3::date)
)
SELECT shifting_event_id::text, target_stage, feed_item_label,
       COALESCE(sum(head_count),0)::int,
       CASE
         WHEN bool_or(has_experiment) THEN 'destination shed uses experiment feed config; no stage-matched ration may be guessed'
         WHEN bool_or(breed_key IS NULL) THEN 'one or more movement animals have no breed'
         -- A blank TARGET stage is no longer a block -- it means the animals keep their own stage,
         -- and effective_stage above prices that. This fires only when an animal has no stage on
         -- EITHER side, which is a herd-data gap, not something the raiser could have chosen.
         WHEN bool_or(effective_stage IS NULL) THEN 'one or more movement animals have no management stage to price a ration against'
         WHEN bool_or(shed_tag_id IS NULL) THEN 'the movement management stage is absent from active feed shed tags'
         WHEN bool_or(ration_group_key IS NULL) THEN 'one or more animal breeds have no active ration group'
         WHEN bool_or(feed_item_key IS NULL) THEN 'destination park has no active feed session items'
         WHEN bool_or(ration_rate_id IS NULL) THEN 'active ration grid is missing one or more required feed rates'
         ELSE ''
       END AS blocked_reason,
       CASE WHEN bool_or(ration_rate_id IS NULL) THEN NULL
            ELSE sum(head_count * grams_per_head * multiplier)::text END AS quantity_grams,
       string_agg(concat_ws(':', shed_tag_id::text, ration_group_id::text,
           session_template_item_ids, ration_rate_id::text, shed_factor_id::text,
           tag_updated_at::text, group_updated_at::text, item_updated_at::text,
           rate_updated_at::text, factor_updated_at::text), '|'
           -- ORDER BY must be TOTAL, or the fingerprint reshuffles between two identical reads and
           -- the phone is told feed_config_changed for a config that did not change. breed_key alone
           -- stopped being unique here once one breed can appear under two stages; it was already
           -- non-unique across pens.
           ORDER BY breed_key, effective_stage, partition_key) AS config_material
FROM cells
GROUP BY shifting_event_id, target_stage, feed_item_key, feed_item_label
ORDER BY shifting_event_id, feed_item_label`, tenantID, eventIDs, asOf.In(biztime.DefaultLocation()).Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("counts: resolve shifting feed requirements: %w", err)
	}
	defer rows.Close()

	type fingerprintRow struct {
		Label    string `json:"label"`
		Quantity string `json:"quantity"`
		Material string `json:"material"`
	}
	fingerprintRows := map[string][]fingerprintRow{}
	for rows.Next() {
		var eventID string
		var stage, label, reason, quantity, material *string
		var animalCount int
		if err := rows.Scan(&eventID, &stage, &label, &animalCount, &reason, &quantity, &material); err != nil {
			return nil, fmt.Errorf("counts: scan shifting feed requirement: %w", err)
		}
		req := out[eventID]
		req.Status = "ready"
		req.AnimalCount = animalCount
		if stage != nil {
			req.TargetManagementStage = *stage
		}
		if reason != nil && strings.TrimSpace(*reason) != "" {
			req.Status = "blocked"
			if req.BlockedReason == "" {
				req.BlockedReason = *reason
			}
		}
		if label != nil && quantity != nil {
			req.Items = append(req.Items, domain.ShiftingFeedRequirementItem{FeedItemLabel: *label, QuantityGrams: *quantity})
			fingerprintRows[eventID] = append(fingerprintRows[eventID], fingerprintRow{*label, *quantity, valueOrEmpty(material)})
		}
		out[eventID] = req
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: iterate shifting feed requirements: %w", err)
	}
	for _, eventID := range eventIDs {
		req, ok := out[eventID]
		if !ok {
			req = domain.ShiftingFeedRequirement{Status: "blocked", BlockedReason: "movement or approval animal set is missing", Items: []domain.ShiftingFeedRequirementItem{}}
		}
		if req.Items == nil {
			req.Items = []domain.ShiftingFeedRequirementItem{}
		}
		if req.Status == "ready" {
			sort.Slice(req.Items, func(i, j int) bool { return req.Items[i].FeedItemLabel < req.Items[j].FeedItemLabel })
			payload, _ := json.Marshal(struct {
				Stage string           `json:"stage"`
				Count int              `json:"count"`
				Items []fingerprintRow `json:"items"`
			}{req.TargetManagementStage, req.AnimalCount, fingerprintRows[eventID]})
			sum := sha256.Sum256(payload)
			req.Fingerprint = hex.EncodeToString(sum[:])
		}
		out[eventID] = req
	}
	return out, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
