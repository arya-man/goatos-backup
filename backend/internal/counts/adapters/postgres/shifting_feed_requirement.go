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
// projection-review: producer_unique=(tenant_id, shifting_event_id) in shifting_events;
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
           feed_config_norm(g.breed) AS breed_key, count(*)::bigint AS head_count
    FROM events e
    LEFT JOIN LATERAL jsonb_array_elements_text(COALESCE(e.payload->'goat_ids','[]'::jsonb)) gid ON true
    LEFT JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = gid::uuid
    GROUP BY e.shifting_event_id, e.destination_park_id, e.destination_shed_id, e.target_stage,
             feed_config_norm(g.breed)
), resolved AS (
    SELECT g.*, tag.shed_tag_id, tag.shed_tag_label, tag.shed_tag_key, tag.applies_to,
           rg.ration_group_id,
           CASE WHEN tag.applies_to = 'kid' THEN 'Kid' ELSE rg.ration_group_label END AS ration_group_label,
           feed_config_norm(CASE WHEN tag.applies_to = 'kid' THEN 'Kid' ELSE rg.ration_group_label END) AS ration_group_key,
           tag.updated_at AS tag_updated_at, rg.updated_at AS group_updated_at
    FROM grains g
    LEFT JOIN feed_shed_tags tag
      ON tag.tenant_id = $1::uuid AND tag.shed_tag_key = feed_config_norm(g.target_stage)
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
    SELECT e.shifting_event_id, e.target_stage, r.breed_key, r.head_count,
           r.shed_tag_id, r.ration_group_id, r.ration_group_key,
           p.feed_item_key, p.feed_item_label, p.session_template_item_ids,
           rate.ration_rate_id, rate.grams_per_head, rate.updated_at AS rate_updated_at,
           factor.shed_factor_id, COALESCE(factor.multiplier, 1.0) AS multiplier,
           factor.updated_at AS factor_updated_at,
           r.tag_updated_at, r.group_updated_at, p.updated_at AS item_updated_at,
           EXISTS (
               SELECT 1 FROM feed_experiment_config x
               WHERE x.tenant_id=$1::uuid AND x.park_id=e.destination_park_id
                 AND x.shed_id=e.destination_shed_id AND x.status='active'
           ) AS has_experiment
    FROM events e
    LEFT JOIN resolved r ON r.shifting_event_id=e.shifting_event_id
    LEFT JOIN planned p ON p.shifting_event_id=e.shifting_event_id
    LEFT JOIN feed_ration_rates rate
      ON rate.tenant_id=$1::uuid AND rate.park_id=e.destination_park_id
     AND rate.ration_group_key=r.ration_group_key AND rate.shed_tag_key=feed_config_norm(e.target_stage)
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
         WHEN target_stage IS NULL THEN 'selected destination management stage is missing'
         WHEN bool_or(has_experiment) THEN 'destination shed uses experiment feed config; no stage-matched ration may be guessed'
         WHEN bool_or(breed_key IS NULL) THEN 'one or more movement animals have no breed'
         WHEN bool_or(shed_tag_id IS NULL) THEN 'selected management stage is absent from active feed shed tags'
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
           rate_updated_at::text, factor_updated_at::text), '|' ORDER BY breed_key) AS config_material
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
