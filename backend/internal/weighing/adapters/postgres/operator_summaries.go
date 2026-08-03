package postgres

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// operatorSummaries is the OPERATOR-grain roll-up behind the weighing oversight
// surface: one row per person holding weighing work, with the backend's own tally
// of what their buckets hold.
//
// WHY IT EXISTS. The oversight screen is called "Operators" and rendered a flat
// list of SHEDS with no name on any of them, so a director could not answer the one
// question the screen is for — who did what. Grouping the loaded shed rows on the
// phone would not have fixed it: the shed list is a ~20-row keyset page, so every
// per-person total would have described the page rather than the person, and it
// would have changed as the reader scrolled. The tally therefore has to be
// computed here, over the WHOLE filter, exactly like campaignCounts.
//
// TWO NAMED FACTS, not one ambiguous count. AnimalsWeighedCount and
// AnimalsSubmittedCount are summed here from the SAME two predicates the per-bucket
// fragment uses (readyToCloseCountsSQL in close.go) — see that fragment's comment
// for the definitions. Both surfaces therefore report the same two numbers for the
// same work, and an operator who has weighed 3 animals without pressing Submit
// reads as "3 weighed · 0 submitted" on every screen rather than 3 on one and 0 on
// another.
//
// NO DENOMINATORS. Weighing is free-flow: migration 000079 dropped
// weighing_expected_animals, so there is no expected roster and no total to take a
// share of. Every column below is a plain count that a renderer states as-is. The
// four state counts are disjoint and exhaustive over the person's buckets, which is
// what lets a client draw them as a state ladder rather than as an invented
// fraction.
//
// projection-review: membership=non-canceled weighing_campaign_sheds rows of one
// tenant, optionally narrowed to one park and/or one operator;
// group_key=(operator_user_id); join_cardinality=weighing_campaigns is 1:1 on
// (tenant_id, campaign_id) and only supplies the park predicate, locations is not
// joined at all, and workforce_members is filtered to status='active' whose only
// (tenant_id,user_id) uniqueness is the partial active index — so no joined side
// can multiply a bucket row; pagination=NONE by design, bounded instead by
// domain.MaxOperatorSummaries because a roll-up that pages cannot answer "who did
// what" (see the constant); scope=tenant_id plus the same optional operator and
// park predicates the row query uses.
//
// Grain proof:
//
//	producer weighing_campaign_sheds unique: (campaign_shed_id) PK, tenant-scoped
//	                                 (tenant_id, campaign_id, campaign_shed_id)
//	consumer operator summary row    group:  (tenant_id, operator_user_id)
//	weighing_observations is 1..N per bucket and weighing_shed_observations is 1..N
//	per bucket; NEITHER is joined into the grouped row path. Both are reached only
//	through correlated scalar subqueries evaluated per bucket, so they contribute
//	zero extra rows and cannot inflate ShedCount or any state count.
//
// Grain proof (counts are disjoint/exhaustive):
//
//	'canceled' is excluded by the WHERE clause (retracted work, never real work),
//	and the shed status check constraint (migration 000058) admits exactly
//	{pending, in_progress, completed, closed, canceled}. The four FILTERs below
//	partition the remaining four values one-to-one, so their sum is ShedCount.
//
// PARK NARROWING, unlike campaignCounts. The Active/Completed tab numbers stay
// still while the park chip moves because they describe the whole scope. These rows
// are the opposite: they ARE what the park chip selects, so a summary that ignored
// the chip would name people who hold no work in the park on screen.
func (r *Repository) operatorSummaries(ctx context.Context, tenantID, operatorUserID, parkID string) ([]domain.OperatorSummary, error) {
	rows, err := r.pool.Query(ctx, `
SELECT COALESCE(cs.operator_user_id::text, ''),
       COALESCE(op.display_name, ''),
       count(*)::int,
       count(*) FILTER (WHERE cs.status='pending')::int,
       count(*) FILTER (WHERE cs.status='in_progress')::int,
       count(*) FILTER (WHERE cs.status='completed')::int,
       count(*) FILTER (WHERE cs.status='closed')::int,
       count(*) FILTER (WHERE (
         EXISTS (SELECT 1 FROM weighing_observations wo
                  WHERE wo.tenant_id=cs.tenant_id AND wo.campaign_shed_id=cs.campaign_shed_id
                    AND wo.submitted_at IS NOT NULL AND wo.verification_status='rework')
         OR EXISTS (SELECT 1 FROM weighing_shed_observations wso
                     WHERE wso.tenant_id=cs.tenant_id AND wso.campaign_shed_id=cs.campaign_shed_id
                       AND wso.withdrawn_at IS NULL AND wso.verification_status='rework')
       ))::int,
       COALESCE(sum(
         (SELECT count(*) FROM weighing_observations wo
           WHERE wo.tenant_id=cs.tenant_id AND wo.campaign_shed_id=cs.campaign_shed_id)
         + COALESCE((SELECT sum(wso.animal_count) FROM weighing_shed_observations wso
                      WHERE wso.tenant_id=cs.tenant_id AND wso.campaign_shed_id=cs.campaign_shed_id
                        AND wso.withdrawn_at IS NULL), 0)
       ), 0)::int,
       COALESCE(sum(
         (SELECT count(*) FROM weighing_observations wo
           WHERE wo.tenant_id=cs.tenant_id AND wo.campaign_shed_id=cs.campaign_shed_id
             AND wo.submitted_at IS NOT NULL)
         + COALESCE((SELECT sum(wso.animal_count) FROM weighing_shed_observations wso
                      WHERE wso.tenant_id=cs.tenant_id AND wso.campaign_shed_id=cs.campaign_shed_id
                        AND wso.withdrawn_at IS NULL), 0)
       ), 0)::int
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns wc
  ON wc.tenant_id=cs.tenant_id AND wc.campaign_id=cs.campaign_id
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
WHERE cs.tenant_id=$1::uuid
  AND cs.status <> 'canceled'
  AND wc.status <> 'canceled'
  AND ($2::uuid IS NULL OR cs.operator_user_id=$2::uuid)
  AND ($3::uuid IS NULL OR wc.park_id=$3::uuid)
GROUP BY cs.operator_user_id, op.display_name
ORDER BY COALESCE(op.display_name, '') ASC, COALESCE(cs.operator_user_id::text, '') ASC
LIMIT $4`,
		tenantID,
		nullableString(strings.TrimSpace(operatorUserID)),
		nullableString(strings.TrimSpace(parkID)),
		domain.MaxOperatorSummaries)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.OperatorSummary, 0, 8)
	for rows.Next() {
		var s domain.OperatorSummary
		if err := rows.Scan(&s.OperatorUserID, &s.OperatorDisplayName, &s.ShedCount,
			&s.NotStartedCount, &s.CapturingCount, &s.SubmittedCount, &s.AcceptedCount,
			&s.ReworkCount, &s.AnimalsWeighedCount, &s.AnimalsSubmittedCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
