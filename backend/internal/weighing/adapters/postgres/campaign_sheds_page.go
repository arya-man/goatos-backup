package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// campaignShedCursor is the task-detail bucket keyset. display_name is the order
// the detail screen shows buckets in, and campaign_shed_id breaks ties so two
// buckets with the same display name cannot loop or skip.
type campaignShedCursor struct {
	DisplayName    string `json:"display_name"`
	CampaignShedID string `json:"campaign_shed_id"`
}

func encodeCampaignShedCursor(cursor campaignShedCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCampaignShedCursor(value string) (campaignShedCursor, error) {
	if strings.TrimSpace(value) == "" {
		return campaignShedCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return campaignShedCursor{}, err
	}
	var cursor campaignShedCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return campaignShedCursor{}, err
	}
	if cursor.CampaignShedID == "" {
		return campaignShedCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

// campaignShedAccessClause is the caller's authority as a SQL disjunction over ONE bucket row,
// rendered with whatever parameter positions the statement it is spliced into happens to have.
//
// It is generated rather than written twice because it must be IDENTICAL in the page query and
// in the count query. If they could drift, the page would show what the caller may see while the
// header count described a wider set -- which is a disclosure of its own (the count of another
// park's buckets) and the exact way a paged surface leaks without ever returning a row.
//
// The clause is ALWAYS applied. There is no "no access supplied" short circuit, because a zero
// CampaignAccess is the answer for an actor who passed a park-blind role gate and holds nothing
// here -- it must admit nothing rather than everything. The internal/CLI caller reaches this as
// Unrestricted (authorizedParkSet returns tenant-wide when a context carries no grants at all),
// which is a stated authority rather than an absent one.
//
// The arms are alternatives, matching CampaignAccess:
//   - unrestricted: tenant-wide plan-or-monitor authority.
//   - parks: the task's OWN park is in the caller's capability-scoped set. Read through an
//     EXISTS semijoin on weighing_campaigns, which is 1:1 on campaign_id and cannot multiply
//     the bucket row either way.
//   - assignee: the caller holds THIS bucket. Row-local, so it both admits and narrows -- a
//     caller with no park authority sees only their own buckets, which is what the old
//     operatorUserID parameter did. Deliberately not qualified by a park check: an operator is
//     park-bound in the database (weighing_operator_park_bound_guard), and requiring park
//     authority on top would 404 a Growth Director -- who holds WeighingMonitor and
//     WeighingExecute at once -- on their own assigned work in a park they do not monitor.
func campaignShedAccessClause(unrestricted, parks, assignee string) string {
	return `(
    ` + unrestricted + `::boolean
    OR EXISTS (
      SELECT 1 FROM weighing_campaigns wc
      WHERE wc.tenant_id=cs.tenant_id AND wc.campaign_id=cs.campaign_id
        AND wc.park_id = ANY(` + parks + `::uuid[])
    )
    OR (` + assignee + `::uuid IS NOT NULL AND cs.operator_user_id=` + assignee + `::uuid)
  )`
}

// ListCampaignSheds pages ONE task's buckets.
//
// projection-review: membership=weighing_campaign_sheds rows of one campaign, admitted row by row by the caller's access disjunction (which narrows to the caller's own buckets when they hold no park authority on the task); group_key=(tenant_id, campaign_id, campaign_shed_id); join_cardinality=nothing is joined into the row path — the per-bucket verification tallies are CORRELATED SUBQUERIES evaluated once per returned row by the planner, workforce_members is filtered to status='active' (its only (tenant_id,user_id) uniqueness is the partial active index), and weighing_campaigns is reached only through an EXISTS semijoin inside the access clause, so none of the three can fan the bucket row out; pagination=keyset on (display_name, campaign_shed_id) ASC with a ~20 default, with the access predicate inside WHERE so authorization happens before LIMIT and a page can never be short because unauthorized rows were dropped after it was cut; scope=tenant_id plus campaign_id, plus the access disjunction ($7 applies, $8 tenant-wide, $9 authorized park array, $10 assignee) evaluated against the SAME row snapshot the query returns and repeated verbatim in the count query so page and total describe one set.
//
// Authorization is INSIDE this query, not around it. It used to be a preceding
// CampaignParkID call in the service, authorized separately and then followed by this
// read: park_id is mutable, so a task that moved park between the two statements was
// authorized as its old park and paged as its new one, buckets and operator display
// names included. Re-checking after the read would only add a third read of the same
// moving value.
//
// Grain proof:
//
//	producer weighing_campaign_sheds unique: (campaign_shed_id) PK; tenant/campaign
//	  scoped (tenant_id, campaign_id, campaign_shed_id)
//	consumer page row                match:  the same triple — one row out per row in
//	weighing_observations are 1..N per bucket and weighing_shed_observations is
//	  0..1 OPEN per bucket (plus any number of withdrawn ones, since a rejected or
//	  reopened proof is kept as history). Both are read ONLY inside scalar count subqueries, so they
//	  contribute zero extra rows either way.
//
// TotalCount ranges over the SAME key set as the rows (tenant, campaign, and the
// same access clause), so the header count and the pages can never describe
// different bucket sets.
//
// Index-backed by weighing_campaign_sheds_detail_keyset_idx
// (tenant_id, campaign_id, display_name, campaign_shed_id) — migration 000063.
func (r *Repository) ListCampaignSheds(ctx context.Context, tenantID, campaignID, cursor string, limit int, access ports.CampaignAccess) (domain.CampaignShedPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = domain.CampaignShedPageSize
	}
	if limit > domain.MaxCampaignShedPageSize {
		limit = domain.MaxCampaignShedPageSize
	}
	cur, err := decodeCampaignShedCursor(cursor)
	if err != nil {
		return domain.CampaignShedPage{}, ports.ErrInvalidArgument
	}
	// An EMPTY (never nil) array: `= ANY('{}')` is FALSE, whereas `= ANY(NULL)` is NULL, and a
	// NULL arm inside this OR would make an otherwise-admitted row evaluate to NULL and vanish.
	accessParkIDs := append([]string{}, access.AuthorizedParkIDs...)
	accessAssignee := strings.TrimSpace(access.AssigneeUserID)
	// Admission decides only 404-vs-empty-page and is deliberately a SEPARATE, non-disclosing
	// question: it returns no row data, so a park that moves between it and the page query can
	// at worst produce a spurious "not found" or a spurious empty page. The disclosure boundary
	// is the page query's own predicate, which is evaluated against the rows it returns.
	//
	// It is the SAME disjunction the page applies, asked at task grain, so the answer agrees
	// with GetCampaign on the task header this page belongs to: an actor the header refuses
	// must not then be shown a 200 with an empty bucket list, and vice versa.
	//
	// Skipped only for the unrestricted arm, which is admitted by definition.
	if !access.Unrestricted {
		var admitted bool
		if err := r.pool.QueryRow(ctx, `
SELECT
  EXISTS (
    SELECT 1 FROM weighing_campaigns wc
    WHERE wc.tenant_id=$1::uuid AND wc.campaign_id=$2::uuid AND wc.park_id = ANY($3::uuid[])
  )
  OR (
    $4::uuid IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM weighing_campaign_sheds cs
      WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid AND cs.operator_user_id=$4::uuid
    )
  )`, tenantID, campaignID, accessParkIDs, nullableString(accessAssignee)).Scan(&admitted); err != nil {
			return domain.CampaignShedPage{}, err
		}
		if !admitted {
			// Same answer the preceding park check used to give, so a cross-park drilldown is
			// still indistinguishable from a task that does not exist.
			return domain.CampaignShedPage{}, ports.ErrNotFound
		}
	}
	rows, err := r.pool.Query(ctx, `
SELECT cs.campaign_shed_id::text, cs.campaign_id::text, cs.location_id::text, cs.location_type, cs.display_name,
  COALESCE(cs.partition_label, ''), cs.expected_animal_count, cs.weighing_category, cs.operator_user_id::text, COALESCE(op.display_name, ''),
  CASE WHEN cs.status IN ('completed','closed','canceled') THEN cs.status ELSE COALESCE(wi.work_state, cs.status) END,
  COALESCE(wi.planned_business_date::text, ''), COALESCE(wi.due_business_date::text, ''),
  `+readyToCloseCountsSQL+`
FROM weighing_campaign_sheds cs
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
LEFT JOIN weighing_work_items wi
  ON wi.tenant_id=cs.tenant_id
 AND wi.campaign_shed_id=cs.campaign_shed_id
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND (
    $3::text IS NULL
    OR (cs.display_name, cs.campaign_shed_id) > ($3::text, $4::uuid)
  )
  AND `+campaignShedAccessClause("$6", "$7", "$8")+`
ORDER BY cs.display_name, cs.campaign_shed_id
LIMIT $5`, tenantID, campaignID,
		nullableString(cur.DisplayName), nullableString(cur.CampaignShedID), limit+1,
		access.Unrestricted, accessParkIDs, nullableString(accessAssignee))
	if err != nil {
		return domain.CampaignShedPage{}, err
	}
	defer rows.Close()
	page := domain.CampaignShedPage{CampaignID: campaignID, Items: make([]domain.CampaignShed, 0, limit)}
	for rows.Next() {
		var shed domain.CampaignShed
		var submitted int
		var partitionLabel string
		if err := rows.Scan(&shed.CampaignShedID, &shed.CampaignID, &shed.LocationID, &shed.LocationType, &shed.DisplayName,
			&partitionLabel, &shed.ExpectedAnimalCount, &shed.WeighingCategory, &shed.OperatorUserID, &shed.OperatorDisplayName, &shed.Status,
			&shed.PlannedBusinessDate, &shed.DueBusinessDate,
			&shed.ClosureKind, &submitted, &shed.PendingVerificationCount, &shed.ReworkCount, &shed.LatestReworkReason, &shed.VerifiedCount, &shed.AnimalsWeighedCount, &shed.AnimalsSubmittedCount); err != nil {
			return domain.CampaignShedPage{}, err
		}
		applyShedPartitionDisplayWithStoredLabel(&shed, partitionLabel)
		shed.ReadyToClose = shed.Status == domain.StatusCompleted && submitted > 0 && shed.PendingVerificationCount == 0
		page.Items = append(page.Items, shed)
	}
	if err := rows.Err(); err != nil {
		return domain.CampaignShedPage{}, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.NextCursor = encodeCampaignShedCursor(campaignShedCursor{DisplayName: last.DisplayName, CampaignShedID: last.CampaignShedID})
		page.Items = page.Items[:limit]
	}
	// Whole-task count, deliberately a SEPARATE count-only query: folding it into
	// the paged query as a window function would force the LIMIT off and turn a
	// bounded keyset page into a full scan of the task's buckets on every page.
	//
	// It carries the SAME access clause, generated from the same function, so the header
	// count ranges over exactly the bucket set the pages can reach. A count that ignored
	// authority would report another park's bucket total to a caller who can see none of
	// them -- a smaller leak than the rows, but the same leak.
	if err := r.pool.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND `+campaignShedAccessClause("$3", "$4", "$5"),
		tenantID, campaignID,
		access.Unrestricted, accessParkIDs, nullableString(accessAssignee)).Scan(&page.TotalCount); err != nil {
		return domain.CampaignShedPage{}, err
	}
	return page, nil
}
