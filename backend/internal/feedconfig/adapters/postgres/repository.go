// Package postgres implements the feed-config repository against the tables created by migrations
// 000003 (the ration grid) and 000004 (the dispatch clock + the write ledger).
//
// TWO THINGS THIS FILE IS RESPONSIBLE FOR GETTING RIGHT
//
//  1. EFFECTIVE DATING. Every write closes-and-opens rather than overwriting, so the history of an
//     authored value stays reconstructable. The one exception is a same-business-day re-edit, which
//     corrects in place because valid_to > valid_from cannot hold for a zero-length window. This is
//     the identical three-way reconciliation seed-feed-ration performs; the two implementations must
//     not drift, or the UI and the seed would mean different things by "change this rate".
//
//  2. IDEMPOTENCY. The client key, the request fingerprint, and the effect are written in ONE
//     transaction. An exact replay returns the original result without re-running anything; a
//     same-key/different-payload replay is a conflict. There is no post-commit best-effort step:
//     a ledger row exists if and only if its side effects committed.
//
// NORMALIZED KEYS. The stored *_key columns are GENERATED from feed_config_norm(label), so writes
// pass the raw LABEL and lookups wrap the raw value in feed_config_norm(). Never hand-normalize in
// Go: there is exactly one normalizer and it lives in the database, which is what stops a writer
// from bypassing it.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// writeLogIdempotencyConstraint is the unique index whose violation means "another transaction
// committed this same client key first".
const writeLogIdempotencyConstraint = "feed_config_write_log_idempotency_uidx"

// feedItemNaturalKeyConstraint is the unique index on (tenant_id, feed_item_key). Its violation
// means another transaction added the same feed item between our duplicate pre-read and our insert,
// which is reported to the author as "already exists" rather than as a server error.
const feedItemNaturalKeyConstraint = "feed_item_catalog_natural_key_uidx"

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout}
}

var _ ports.Repository = (*Repository)(nil)

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------
//
// Every list read fetches Limit+1 rows and reports has_more from whether the extra row existed. It
// never runs a COUNT over the filtered set: a total on every page is compute-on-read, and the grid
// UI needs "is there another page", not a total.
//
// Paging is bounded LIMIT/OFFSET rather than keyset. These are authored config tables whose whole
// contents are small and stable (1442 rates, 31 tags, 10 items, 7 groups), the operator pages a
// grid rather than draining an append-only queue, and the service rejects an offset past 5000
// outright -- so the offset cannot grow without bound. See the scale-guard annotations below.

// ListRationRates serves one page of the CURRENT grid for a park.
//
// valid_to IS NULL only: an edit screen shows what is in force, and mixing closed historical
// windows into it would present superseded rates as editable current values.
func (r *Repository) ListRationRates(ctx context.Context, q domain.RationRateQuery) (domain.RationRatePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over an authored config table (the full live grid is ~1442 rows for the whole tenant, park-filtered here); the service rejects offset > 5000, so the offset is bounded by construction and never grows with herd size.
	const query = `
SELECT ration_rate_id::text,
       park_id::text,
       ration_group_label,
       shed_tag_label,
       feed_item_label,
       grams_per_head::text,
       valid_from::text,
       valid_to::text,
       source_system
FROM feed_ration_rates
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND valid_to IS NULL
  AND ($3::text IS NULL OR ration_group_key = feed_config_norm($3))
  AND ($4::text IS NULL OR shed_tag_key = feed_config_norm($4))
  AND ($5::text IS NULL OR feed_item_key = feed_config_norm($5))
ORDER BY ration_group_label, shed_tag_label, feed_item_label, ration_rate_id
LIMIT $6 OFFSET $7`

	rows, err := r.pool.Query(ctx, query,
		q.TenantID, q.ParkID,
		nullIfEmpty(q.RationGroup), nullIfEmpty(q.ShedTag), nullIfEmpty(q.FeedItem),
		q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.RationRatePage{}, fmt.Errorf("feedconfig: list ration rates: %w", err)
	}
	defer rows.Close()

	out := domain.RationRatePage{Items: []domain.RationRate{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		var item domain.RationRate
		var validTo *string
		if err := rows.Scan(&item.RationRateID, &item.ParkID, &item.RationGroupLabel, &item.ShedTagLabel,
			&item.FeedItemLabel, &item.GramsPerHead, &item.ValidFrom, &validTo, &item.SourceSystem); err != nil {
			return domain.RationRatePage{}, fmt.Errorf("feedconfig: scan ration rate: %w", err)
		}
		item.ValidTo = validTo
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.RationRatePage{}, fmt.Errorf("feedconfig: list ration rates: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

func (r *Repository) ListRationGroups(ctx context.Context, tenantID string, page domain.Page) (domain.RationGroupPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over the breed -> ration-group map (7 live rows); the service rejects offset > 5000.
	const query = `
SELECT ration_group_id::text, breed_label, ration_group_label
FROM feed_ration_groups
WHERE tenant_id = $1::uuid
ORDER BY ration_group_label, breed_label, ration_group_id
LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, query, tenantID, page.Limit+1, page.Offset)
	if err != nil {
		return domain.RationGroupPage{}, fmt.Errorf("feedconfig: list ration groups: %w", err)
	}
	defer rows.Close()

	out := domain.RationGroupPage{Items: []domain.RationGroup{}, Limit: page.Limit, Offset: page.Offset}
	for rows.Next() {
		var item domain.RationGroup
		if err := rows.Scan(&item.RationGroupID, &item.BreedLabel, &item.RationGroupLabel); err != nil {
			return domain.RationGroupPage{}, fmt.Errorf("feedconfig: scan ration group: %w", err)
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.RationGroupPage{}, fmt.Errorf("feedconfig: list ration groups: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, page.Limit)
	return out, nil
}

func (r *Repository) ListShedTags(ctx context.Context, q domain.ShedTagQuery) (domain.ShedTagPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over the authored shed-tag vocabulary (31 live rows); the service rejects offset > 5000.
	const query = `
SELECT shed_tag_id::text, shed_tag_label, applies_to, display_order, status
FROM feed_shed_tags
WHERE tenant_id = $1::uuid
  AND ($2::text IS NULL OR applies_to = $2::text)
ORDER BY display_order, shed_tag_label, shed_tag_id
LIMIT $3 OFFSET $4`

	rows, err := r.pool.Query(ctx, query, q.TenantID, nullIfEmpty(q.AppliesTo), q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.ShedTagPage{}, fmt.Errorf("feedconfig: list shed tags: %w", err)
	}
	defer rows.Close()

	out := domain.ShedTagPage{Items: []domain.ShedTag{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		var item domain.ShedTag
		if err := rows.Scan(&item.ShedTagID, &item.ShedTagLabel, &item.AppliesTo, &item.DisplayOrder, &item.Status); err != nil {
			return domain.ShedTagPage{}, fmt.Errorf("feedconfig: scan shed tag: %w", err)
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ShedTagPage{}, fmt.Errorf("feedconfig: list shed tags: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

// ListFeedItems serves the catalog. The nutritional columns are scanned into pointers because they
// are genuinely NULL for every seeded item -- the source grid carries rates, not energy values, and
// an honest NULL is better than an invented number.
func (r *Repository) ListFeedItems(ctx context.Context, tenantID string, page domain.Page) (domain.FeedItemPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over the feed-item catalog (10 live rows); the service rejects offset > 5000.
	const query = `
SELECT feed_item_id::text,
       feed_item_label,
       energy_kcal_per_kg::text,
       dry_matter_factor::text,
       wastage_factor::text,
       display_order,
       status
FROM feed_item_catalog
WHERE tenant_id = $1::uuid
ORDER BY display_order, feed_item_label, feed_item_id
LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, query, tenantID, page.Limit+1, page.Offset)
	if err != nil {
		return domain.FeedItemPage{}, fmt.Errorf("feedconfig: list feed items: %w", err)
	}
	defer rows.Close()

	out := domain.FeedItemPage{Items: []domain.FeedItem{}, Limit: page.Limit, Offset: page.Offset}
	for rows.Next() {
		var item domain.FeedItem
		if err := rows.Scan(&item.FeedItemID, &item.FeedItemLabel, &item.EnergyKcalPerKg,
			&item.DryMatterFactor, &item.WastageFactor, &item.DisplayOrder, &item.Status); err != nil {
			return domain.FeedItemPage{}, fmt.Errorf("feedconfig: scan feed item: %w", err)
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.FeedItemPage{}, fmt.Errorf("feedconfig: list feed items: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, page.Limit)
	return out, nil
}

func (r *Repository) ListSessionTemplates(ctx context.Context, q domain.SessionTemplateQuery) (domain.SessionTemplatePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over a park's feeding-session template (2 live rows per park); the service rejects offset > 5000.
	const query = `
SELECT session_template_id::text, park_id::text, session_no, session_label,
       split_fraction::text, display_order, status
FROM feed_session_templates
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
ORDER BY display_order, session_no, session_template_id
LIMIT $3 OFFSET $4`

	rows, err := r.pool.Query(ctx, query, q.TenantID, q.ParkID, q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.SessionTemplatePage{}, fmt.Errorf("feedconfig: list session templates: %w", err)
	}
	defer rows.Close()

	out := domain.SessionTemplatePage{Items: []domain.SessionTemplate{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		var item domain.SessionTemplate
		if err := rows.Scan(&item.SessionTemplateID, &item.ParkID, &item.SessionNo, &item.SessionLabel,
			&item.SplitFraction, &item.DisplayOrder, &item.Status); err != nil {
			return domain.SessionTemplatePage{}, fmt.Errorf("feedconfig: scan session template: %w", err)
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.SessionTemplatePage{}, fmt.Errorf("feedconfig: list session templates: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

// ListScheduleConfig serves a park's CURRENT dispatch clocks, one per workflow.
func (r *Repository) ListScheduleConfig(ctx context.Context, q domain.ScheduleConfigQuery) (domain.ScheduleConfigPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over a park's dispatch clock (at most one open row per workflow, so 2); the service rejects offset > 5000.
	const query = `
SELECT feed_schedule_config_id::text,
       park_id::text,
       workflow,
       direction_time::text,
       correction_time::text,
       transport_time::text,
       valid_from::text,
       valid_to::text
FROM feed_schedule_config
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND valid_to IS NULL
  AND ($3::text IS NULL OR workflow = $3::text)
ORDER BY workflow, feed_schedule_config_id
LIMIT $4 OFFSET $5`

	rows, err := r.pool.Query(ctx, query, q.TenantID, q.ParkID, nullIfEmpty(q.Workflow), q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.ScheduleConfigPage{}, fmt.Errorf("feedconfig: list schedule config: %w", err)
	}
	defer rows.Close()

	out := domain.ScheduleConfigPage{Items: []domain.ScheduleConfig{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		var item domain.ScheduleConfig
		var transport, validTo *string
		if err := rows.Scan(&item.ScheduleConfigID, &item.ParkID, &item.Workflow, &item.DirectionTime,
			&item.CorrectionTime, &transport, &item.ValidFrom, &validTo); err != nil {
			return domain.ScheduleConfigPage{}, fmt.Errorf("feedconfig: scan schedule config: %w", err)
		}
		// transport_time stays a pointer all the way to the wire: NULL means the park has not declared
		// a cutoff, which the consumer must read as UNKNOWN rather than as "no deadline".
		item.TransportTime = transport
		item.ValidTo = validTo
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ScheduleConfigPage{}, fmt.Errorf("feedconfig: list schedule config: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

// ListShedFactors returns only AUTHORED factors. It deliberately does NOT synthesize a 1.0 row for
// every shed that has none: absence reads as 1.0 at lookup time, and materializing that default
// here would make an unauthored shed indistinguishable from one someone deliberately set to 1.0.
func (r *Repository) ListShedFactors(ctx context.Context, q domain.ShedFactorQuery) (domain.ShedFactorPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over authored per-shed multipliers (one optional row per shed x feed item, park-filtered); the service rejects offset > 5000.
	const query = `
SELECT shed_factor_id::text, park_id::text, shed_id::text, feed_item_label,
       multiplier::text, valid_from::text, valid_to::text
FROM feed_shed_factors
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND valid_to IS NULL
  AND ($3::uuid IS NULL OR shed_id = $3::uuid)
  AND ($4::text IS NULL OR feed_item_key = feed_config_norm($4))
ORDER BY shed_id, feed_item_label, shed_factor_id
LIMIT $5 OFFSET $6`

	rows, err := r.pool.Query(ctx, query, q.TenantID, q.ParkID,
		nullIfEmpty(q.ShedID), nullIfEmpty(q.FeedItem), q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.ShedFactorPage{}, fmt.Errorf("feedconfig: list shed factors: %w", err)
	}
	defer rows.Close()

	out := domain.ShedFactorPage{Items: []domain.ShedFactor{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		var item domain.ShedFactor
		var validTo *string
		if err := rows.Scan(&item.ShedFactorID, &item.ParkID, &item.ShedID, &item.FeedItemLabel,
			&item.Multiplier, &item.ValidFrom, &validTo); err != nil {
			return domain.ShedFactorPage{}, fmt.Errorf("feedconfig: scan shed factor: %w", err)
		}
		item.ValidTo = validTo
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ShedFactorPage{}, fmt.Errorf("feedconfig: list shed factors: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

// ListExperimentConfig serves a park's hand-authored EXPERIMENT sheds.
//
// BOTH statuses by default. A withdrawn shed's rows are retired rather than deleted, and the config
// screen must keep showing them: they are the authored quantities that come back if the shed is
// restored, and hiding them would make an accidental withdrawal invisible on the very screen that
// owns the decision.
//
// Ordered by (shed_id, partition_key, feed_item_key) so the walk matches
// feed_experiment_config_natural_key_uidx
// (tenant_id, park_id, shed_id, partition_key, feed_item_key) -- the predicate hits its leading
// columns and the sort is a prefix-ordered read of the same index, so no separate sort is needed.
// Ordering by feed_item_LABEL instead would silently force one.
//
// partition_key is in the ORDER BY, not just the index: a partitioned shed authors one cell per
// PEN, so leaving it out interleaves ten pens' cells by feed item and the screen shows ten
// indistinguishable rows of the same item.
//
// The locations join supplies the shed NAME so the backend can compose the operator-facing
// location itself (the backend-owns-labels rule). LEFT JOIN, not INNER: a config row whose shed
// row is missing degrades to a bare label rather than vanishing from the author's screen.
func (r *Repository) ListExperimentConfig(ctx context.Context, q domain.ExperimentConfigQuery) (domain.ExperimentConfigPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// park_id is OPTIONAL here, unlike every other read on this screen. The ration grid, the session
	// split and the dispatch clock are park-OWNED and have no cross-park meaning, but an experiment
	// cell already carries its own park_id, so the authored inventory can legitimately be listed for
	// the whole tenant. That is what lets a company-wide scope show all 35 pens instead of silently
	// showing one park's 17 -- the defect this widening fixes.
	//
	// The park is JOINED for its name, not composed client-side, because a cross-park list must say
	// which park each row belongs to and the shed NAME cannot carry that (Castro, Gandhi and Yashoda
	// each exist in both parks -- keying or labelling on the name alone merges them).
	//
	// ORDER BY leads with park so a cross-park page groups rather than interleaves, and still sorts
	// by shed/partition/item beneath it so a single-park read is byte-identical to what it was.
	//
	// scale-guard:ignore: bounded LIMIT/OFFSET over the tenant's hand-authored experiment sheds (35 pens x 5 items across both live parks); the operator authors these by hand so the set cannot grow with herd size, and the service rejects offset > 5000.
	const query = `
SELECT c.experiment_config_id::text,
       c.park_id::text,
       COALESCE(NULLIF(park.name, ''), park.location_code, '') AS park_name,
       c.shed_id::text,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_name,
       COALESCE(c.partition_label, '') AS partition_label,
       c.feed_item_label,
       c.absolute_kg::text,
       c.head_count,
       c.experiment_category,
       c.status
FROM feed_experiment_config c
LEFT JOIN locations shed
       ON shed.tenant_id = c.tenant_id
      AND shed.location_id = c.shed_id
LEFT JOIN locations park
       ON park.tenant_id = c.tenant_id
      AND park.location_id = c.park_id
WHERE c.tenant_id = $1::uuid
  AND ($2::uuid IS NULL OR c.park_id = $2::uuid)
  AND ($3::uuid IS NULL OR c.shed_id = $3::uuid)
  AND ($4::text IS NULL OR c.status = $4::text)
ORDER BY park.name, park.location_id, c.shed_id, c.partition_key, c.feed_item_key, c.experiment_config_id
LIMIT $5 OFFSET $6`

	rows, err := r.pool.Query(ctx, query, q.TenantID, nullIfEmpty(q.ParkID),
		nullIfEmpty(q.ShedID), nullIfEmpty(q.Status), q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.ExperimentConfigPage{}, fmt.Errorf("feedconfig: list experiment config: %w", err)
	}
	defer rows.Close()

	out := domain.ExperimentConfigPage{Items: []domain.ExperimentConfig{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		var item domain.ExperimentConfig
		// head_count stays a pointer all the way to the wire: NULL means the population was not
		// recorded alongside the quantity, and rendering that as 0 would state the shed is empty.
		var headCount *int32
		if err := rows.Scan(&item.ExperimentConfigID, &item.ParkID, &item.ParkName, &item.ShedID,
			&item.ShedName, &item.PartitionLabel, &item.FeedItemLabel,
			&item.AbsoluteKg, &headCount, &item.ExperimentCategory, &item.Status); err != nil {
			return domain.ExperimentConfigPage{}, fmt.Errorf("feedconfig: scan experiment config: %w", err)
		}
		item.HeadCount = headCount
		// Compose through oploc so this screen reads identically to every other surface, and so the
		// 'whole' sentinel can never reach a client. Constructed from the row's OWN authored label
		// rather than ResolveShedLocation, whose agree-or-go-bare rule is for inferring a shed's
		// partition from its animals -- here the pen is explicitly authored on the row.
		if !oploc.IsPartitioned(item.PartitionLabel) {
			item.PartitionLabel = ""
		}
		item.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedID:         item.ShedID,
			ShedName:       item.ShedName,
			PartitionLabel: item.PartitionLabel,
		}.Display()
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ExperimentConfigPage{}, fmt.Errorf("feedconfig: list experiment config: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

// UpsertExperimentConfigBatch authors every feed item of ONE pen in a single statement inside a
// single transaction.
//
// ONE STATEMENT, NOT A LOOP. The cells are passed as parallel arrays and expanded with UNNEST, so N
// authored items cost one round trip rather than N (the banned n-plus-one shape), and every cell
// lands or none does. The two arrays are built from the SAME ordered slice in the caller, so index i
// is always the same cell in both -- building one from a filtered copy and the other from the
// original is the parallel-array grain bug that silently pairs a quantity with the wrong feed item.
//
// ON CONFLICT targets the natural key's own columns, INCLUDING the two GENERATED ones
// (partition_key, feed_item_key). They are not inserted -- Postgres computes them -- but naming them
// as the conflict target is what makes the upsert land on the same row the unique index protects.
// Targeting (tenant, park, shed, feed_item_key) alone would collapse every pen of a partitioned shed
// onto one row, which is exactly what migration 000122 widened the key to prevent.
//
// status is forced back to 'active' on conflict for the same reason the single-cell write does it: a
// quantity stored on a retired row is a number nothing reads, so re-authoring a cell is an
// unambiguous statement that this pen is on the experiment workflow.
func (r *Repository) UpsertExperimentConfigBatch(ctx context.Context, cmd domain.UpsertExperimentConfigBatchCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindExperimentConfig, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}
		if err := requireShedInPark(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID); err != nil {
			return writeEffect{}, err
		}

		items := make([]string, 0, len(cmd.Cells))
		kgs := make([]string, 0, len(cmd.Cells))
		for _, cell := range cmd.Cells {
			items = append(items, cell.FeedItemLabel)
			kgs = append(kgs, cell.AbsoluteKg)
		}

		// RETURNING every affected row id, ordered by the generated feed_item_key so the ledger's
		// representative row is deterministic across replays rather than whichever row the executor
		// happened to touch first.
		rows, err := tx.Query(ctx, `
INSERT INTO feed_experiment_config (tenant_id, park_id, shed_id, partition_label, feed_item_label,
                                    absolute_kg, head_count, experiment_category, status, created_by)
SELECT $1::uuid, $2::uuid, $3::uuid, nullif(btrim($4),''), cell.item,
       cell.kg::numeric, $5, $6, 'active', nullif($7,'')::uuid
FROM unnest($8::text[], $9::text[]) AS cell(item, kg)
ON CONFLICT (tenant_id, park_id, shed_id, partition_key, feed_item_key) DO UPDATE
SET absolute_kg         = EXCLUDED.absolute_kg,
    head_count          = EXCLUDED.head_count,
    experiment_category = EXCLUDED.experiment_category,
    status              = 'active',
    updated_at          = now()
RETURNING experiment_config_id::text, feed_item_key`,
			cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.PartitionLabel,
			cmd.HeadCount, cmd.ExperimentCategory, actorUUID(cmd.ActorRef), items, kgs)
		if err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: batch upsert experiment config: %w", err)
		}
		defer rows.Close()

		type touched struct{ id, key string }
		written := make([]touched, 0, len(cmd.Cells))
		for rows.Next() {
			var t touched
			if err := rows.Scan(&t.id, &t.key); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: scan batch experiment config: %w", err)
			}
			written = append(written, t)
		}
		if err := rows.Err(); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: batch upsert experiment config: %w", err)
		}
		// Every cell must have produced a row. A short count means a conflict target did not match
		// what the caller believed it was addressing, and silently reporting success on a partial
		// enrolment is precisely the underfeed this whole write exists to prevent.
		if len(written) != len(cmd.Cells) {
			return writeEffect{}, fmt.Errorf("feedconfig: batch upsert wrote %d of %d cells", len(written), len(cmd.Cells))
		}
		sort.Slice(written, func(i, j int) bool { return written[i].key < written[j].key })

		// 'inserted' rather than a per-cell outcome: the ledger records one authoring ACT, and this
		// act's meaning is "this pen's quantities are now these". The result row is the pen's
		// lowest-keyed cell, which the constraint requires to be non-NULL for this outcome.
		return writeEffect{Outcome: domain.OutcomeInserted, ResultRowID: written[0].id}, nil
	})
}

// ListPens returns every operational location in a park -- each shed, and each pen of a subdivided
// shed -- flagged with whether it already carries experiment configuration.
//
// LEFT JOIN, not INNER. An undivided shed has no shed_partitions row at all and must still appear
// exactly once, as itself; an INNER JOIN would silently drop every whole-shed location and leave the
// enroller unable to offer them.
//
// partition_label is selected, NEVER normalized_label. They look interchangeable and are not:
// 'Part 3' is the human label and '3' is the scrubbed matching key, and selecting the key renders
// 'Mandela 2 - 3' to an operator. This is the defect that shipped, was fixed, and was reintroduced
// hours later by a hand-written query in another module -- see AGENTS.md rule 5a.
//
// The experiment flag is an EXISTS correlated on (shed_id, partition_key), the same natural key the
// experiment table is unique on, so it cannot disagree with what the enroller's filter should do.
// Matching on shed_id alone is precisely the bug being fixed.
//
// No per-animal table is touched. A pen holding zero animals is real, is listed, and is usually the
// one about to be filled -- deriving this catalog from goat placement is what hides it.
func (r *Repository) ListPens(ctx context.Context, q domain.PenQuery) (domain.PenPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded LIMIT/OFFSET over ONE park's location catalog (two live parks hold ~20 sheds and ~40 pens each); the set is authored infrastructure and cannot grow with herd size. Served by the locations parent index and shed_partitions' own (tenant_id, shed_id) key.
	const query = `
SELECT shed.location_id::text,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_name,
       COALESCE(sp.partition_label, '') AS partition_label,
       EXISTS (
         SELECT 1 FROM feed_experiment_config e
         WHERE e.tenant_id = shed.tenant_id
           AND e.shed_id = shed.location_id
           AND e.partition_key = CASE
                 WHEN sp.partition_label IS NULL OR btrim(sp.partition_label) = '' THEN 'whole'
                 ELSE feed_config_norm(sp.partition_label)
               END
       ) AS has_experiment_config
FROM locations shed
LEFT JOIN shed_partitions sp
       ON sp.tenant_id = shed.tenant_id
      AND sp.shed_id = shed.location_id
WHERE shed.tenant_id = $1::uuid
  AND shed.parent_location_id = $2::uuid
  AND shed.location_type = 'shed'
  AND shed.status = 'active'
ORDER BY shed.display_order, shed.name, shed.location_id, sp.normalized_label NULLS FIRST
LIMIT $3 OFFSET $4`

	rows, err := r.pool.Query(ctx, query, q.TenantID, q.ParkID, q.Page.Limit+1, q.Page.Offset)
	if err != nil {
		return domain.PenPage{}, fmt.Errorf("feedconfig: list pens: %w", err)
	}
	defer rows.Close()

	out := domain.PenPage{Items: []domain.Pen{}, Limit: q.Page.Limit, Offset: q.Page.Offset}
	for rows.Next() {
		item := domain.Pen{ParkID: q.ParkID}
		if err := rows.Scan(&item.ShedID, &item.ShedName, &item.PartitionLabel, &item.HasExperimentConfig); err != nil {
			return domain.PenPage{}, fmt.Errorf("feedconfig: scan pen: %w", err)
		}
		// Same composition as the experiment list, through oploc, so the enroller's option text and
		// the table row it becomes are byte-identical. IsPartitioned also filters the 'whole'
		// sentinel, which is a matching key and must never reach a client.
		if !oploc.IsPartitioned(item.PartitionLabel) {
			item.PartitionLabel = ""
		}
		item.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedID:         item.ShedID,
			ShedName:       item.ShedName,
			PartitionLabel: item.PartitionLabel,
		}.Display()
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.PenPage{}, fmt.Errorf("feedconfig: list pens: %w", err)
	}
	out.Items, out.HasMore = trimPage(out.Items, q.Page.Limit)
	return out, nil
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// UpsertRationRate authors one grid cell. See the package comment for the effective-dating and
// idempotency contracts this implements.
func (r *Repository) UpsertRationRate(ctx context.Context, cmd domain.UpsertRationRateCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindRationRate, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}

		// Lock the currently-open row for this key. FOR UPDATE, so two concurrent edits of the SAME
		// cell serialize instead of both deciding "no open row" and racing into the
		// feed_ration_rates_open_row_uidx partial unique index.
		var openID, openGrams, openValidFrom string
		err := tx.QueryRow(ctx, `
SELECT ration_rate_id::text, grams_per_head::text, valid_from::text
FROM feed_ration_rates
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND ration_group_key = feed_config_norm($3)
  AND shed_tag_key = feed_config_norm($4)
  AND feed_item_key = feed_config_norm($5)
  AND valid_to IS NULL
FOR UPDATE`, cmd.TenantID, cmd.ParkID, cmd.RationGroupLabel, cmd.ShedTagLabel, cmd.FeedItemLabel).
			Scan(&openID, &openGrams, &openValidFrom)

		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// No open row: this is the FIRST authored value for the cell. Note what is NOT happening
			// here -- the absence of a row was never read as 0 anywhere upstream.
			var newID string
			if err := tx.QueryRow(ctx, `
INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label,
                               grams_per_head, valid_from, source_system, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::numeric, $7::date, 'manual', nullif($8,'')::uuid)
RETURNING ration_rate_id::text`,
				cmd.TenantID, cmd.ParkID, cmd.RationGroupLabel, cmd.ShedTagLabel, cmd.FeedItemLabel,
				cmd.GramsPerHead, cmd.EffectiveFrom, actorUUID(cmd.ActorRef)).Scan(&newID); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: insert ration rate: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeInserted, ResultRowID: newID}, nil

		case err != nil:
			return writeEffect{}, fmt.Errorf("feedconfig: lock ration rate: %w", err)
		}

		if openGrams == cmd.GramsPerHead {
			// Already in force. No row is written and no window is opened: re-authoring the same number
			// is not a change, and recording it as one would litter the history with empty windows.
			return writeEffect{Outcome: domain.OutcomeUnchanged, ResultRowID: openID}, nil
		}
		if openValidFrom > cmd.EffectiveFrom {
			return writeEffect{}, ports.ErrFutureDatedRow
		}
		if openValidFrom == cmd.EffectiveFrom {
			// Same-business-day re-author: correct in place. A window closed on the day it opened would
			// violate valid_to > valid_from, and this is a correction of today's authoring rather than a
			// historical change worth its own window.
			if _, err := tx.Exec(ctx, `
UPDATE feed_ration_rates
SET grams_per_head = $2::numeric, updated_at = now()
WHERE ration_rate_id = $1::uuid`, openID, cmd.GramsPerHead); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: correct ration rate: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeCorrected, ResultRowID: openID}, nil
		}

		// Supersede: close yesterday's row and open today's. The old rate survives as history rather
		// than being overwritten -- this is the whole reason the table is effective-dated.
		if _, err := tx.Exec(ctx, `
UPDATE feed_ration_rates
SET valid_to = $2::date, updated_at = now()
WHERE ration_rate_id = $1::uuid`, openID, cmd.EffectiveFrom); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: close ration rate: %w", err)
		}
		var newID string
		if err := tx.QueryRow(ctx, `
INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label,
                               grams_per_head, valid_from, source_system, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::numeric, $7::date, 'manual', nullif($8,'')::uuid)
RETURNING ration_rate_id::text`,
			cmd.TenantID, cmd.ParkID, cmd.RationGroupLabel, cmd.ShedTagLabel, cmd.FeedItemLabel,
			cmd.GramsPerHead, cmd.EffectiveFrom, actorUUID(cmd.ActorRef)).Scan(&newID); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: insert superseding ration rate: %w", err)
		}
		return writeEffect{Outcome: domain.OutcomeSuperseded, ResultRowID: newID, SupersededRowID: openID}, nil
	})
}

// UpsertShedFactor authors one per-shed multiplier on the same effective-dated terms.
func (r *Repository) UpsertShedFactor(ctx context.Context, cmd domain.UpsertShedFactorCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindShedFactor, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}
		if err := requireShedInPark(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID); err != nil {
			return writeEffect{}, err
		}

		var openID, openMultiplier, openValidFrom string
		err := tx.QueryRow(ctx, `
SELECT shed_factor_id::text, multiplier::text, valid_from::text
FROM feed_shed_factors
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND feed_item_key = feed_config_norm($4)
  AND valid_to IS NULL
FOR UPDATE`, cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.FeedItemLabel).
			Scan(&openID, &openMultiplier, &openValidFrom)

		switch {
		case errors.Is(err, pgx.ErrNoRows):
			var newID string
			if err := tx.QueryRow(ctx, `
INSERT INTO feed_shed_factors (tenant_id, park_id, shed_id, feed_item_label, multiplier, valid_from, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::numeric, $6::date, nullif($7,'')::uuid)
RETURNING shed_factor_id::text`,
				cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.FeedItemLabel, cmd.Multiplier,
				cmd.EffectiveFrom, actorUUID(cmd.ActorRef)).Scan(&newID); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: insert shed factor: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeInserted, ResultRowID: newID}, nil
		case err != nil:
			return writeEffect{}, fmt.Errorf("feedconfig: lock shed factor: %w", err)
		}

		if openMultiplier == cmd.Multiplier {
			return writeEffect{Outcome: domain.OutcomeUnchanged, ResultRowID: openID}, nil
		}
		if openValidFrom > cmd.EffectiveFrom {
			return writeEffect{}, ports.ErrFutureDatedRow
		}
		if openValidFrom == cmd.EffectiveFrom {
			if _, err := tx.Exec(ctx, `
UPDATE feed_shed_factors
SET multiplier = $2::numeric, updated_at = now()
WHERE shed_factor_id = $1::uuid`, openID, cmd.Multiplier); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: correct shed factor: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeCorrected, ResultRowID: openID}, nil
		}

		if _, err := tx.Exec(ctx, `
UPDATE feed_shed_factors
SET valid_to = $2::date, updated_at = now()
WHERE shed_factor_id = $1::uuid`, openID, cmd.EffectiveFrom); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: close shed factor: %w", err)
		}
		var newID string
		if err := tx.QueryRow(ctx, `
INSERT INTO feed_shed_factors (tenant_id, park_id, shed_id, feed_item_label, multiplier, valid_from, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::numeric, $6::date, nullif($7,'')::uuid)
RETURNING shed_factor_id::text`,
			cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.FeedItemLabel, cmd.Multiplier,
			cmd.EffectiveFrom, actorUUID(cmd.ActorRef)).Scan(&newID); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: insert superseding shed factor: %w", err)
		}
		return writeEffect{Outcome: domain.OutcomeSuperseded, ResultRowID: newID, SupersededRowID: openID}, nil
	})
}

// CreateFeedItem adds one entry to the tenant's feed-item catalog.
//
// NOT PARK-SCOPED, and that is the schema speaking: feed_item_catalog is keyed
// (tenant_id, feed_item_key), so a feed item is a TENANT vocabulary that both parks author rates
// against. There is no requireLocation call here because there is no location in the key.
//
// ONE OUTCOME: 'inserted'. There is no corrected/superseded/unchanged branch, because this is an
// ADD rather than an edit -- a duplicate label is ErrFeedItemExists (see ports), never an in-place
// rewrite of an item's authored attributes.
//
// THE DUPLICATE CHECK IS BELT AND BRACES, DELIBERATELY. The pre-read gives the author a clean
// "already exists" instead of a constraint violation, but it cannot be the only guard: two
// concurrent adds of the same label both read "absent" and both proceed. The unique-violation arm
// below is what actually makes that impossible, and feed_item_catalog_natural_key_uidx is what
// makes the vocabulary single-valued. Removing either one leaves a real hole -- the pre-read alone
// races, and the index alone reports a 500 to someone who typed a name that already exists.
func (r *Repository) CreateFeedItem(ctx context.Context, cmd domain.CreateFeedItemCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindFeedItem, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		// feed_config_norm on BOTH sides, matching the generated feed_item_key column. Comparing raw
		// labels would let "Dry Masoor Bhusa " through as a second entry that every rate keyed on the
		// normalized label would then collapse back onto.
		var existingID string
		err := tx.QueryRow(ctx, `
SELECT feed_item_id::text
FROM feed_item_catalog
WHERE tenant_id = $1::uuid AND feed_item_key = feed_config_norm($2)`,
			cmd.TenantID, cmd.FeedItemLabel).Scan(&existingID)
		switch {
		case err == nil:
			return writeEffect{}, ports.ErrFeedItemExists
		case !errors.Is(err, pgx.ErrNoRows):
			return writeEffect{}, fmt.Errorf("feedconfig: check feed item: %w", err)
		}

		// An absent display_order appends to the END of the catalog rather than taking the column's
		// DEFAULT 0, which would silently place every new item FIRST in every dropdown on the screen.
		// Resolved inside this transaction so two concurrent adds cannot both read the same maximum
		// -- and a tie is harmless anyway: the listing breaks ties on label, then id.
		//
		// This is a PRESENTATION position. It is the one value in this module derived rather than
		// authored, and it is derivable precisely because no feeding decision reads it.
		displayOrder := cmd.DisplayOrder
		if displayOrder == nil {
			var next int32
			if err := tx.QueryRow(ctx, `
SELECT coalesce(max(display_order), 0) + 1
FROM feed_item_catalog
WHERE tenant_id = $1::uuid`, cmd.TenantID).Scan(&next); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: resolve feed item display order: %w", err)
			}
			displayOrder = &next
		}

		// status is 'active' on creation: an item added to the vocabulary is one the author intends to
		// use. The three nutritional attributes bind as NULL when absent -- an honest "not measured",
		// never a 0 that would claim someone measured it.
		var newID string
		if err := tx.QueryRow(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, energy_kcal_per_kg, dry_matter_factor,
                               wastage_factor, display_order, status)
VALUES ($1::uuid, $2, $3::numeric, $4::numeric, $5::numeric, $6, 'active')
RETURNING feed_item_id::text`,
			cmd.TenantID, cmd.FeedItemLabel, cmd.EnergyKcalPerKg, cmd.DryMatterFactor,
			cmd.WastageFactor, displayOrder).Scan(&newID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == feedItemNaturalKeyConstraint {
				// A concurrent add of the same label won the race between our pre-read and this insert.
				// Same answer as the pre-read would have given, which is the answer the author needs.
				return writeEffect{}, ports.ErrFeedItemExists
			}
			return writeEffect{}, fmt.Errorf("feedconfig: insert feed item: %w", err)
		}
		return writeEffect{Outcome: domain.OutcomeInserted, ResultRowID: newID}, nil
	})
}

// UpsertScheduleConfig authors one park/workflow dispatch clock.
//
// All three times are bound as ::time and stored WITHOUT an offset -- they are recurring
// Asia/Kolkata wall-clock rules, not instants. Do not introduce a timezone conversion here.
func (r *Repository) UpsertScheduleConfig(ctx context.Context, cmd domain.UpsertScheduleConfigCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindScheduleConfig, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}

		var openID, openDirection, openCorrection, openValidFrom string
		var openTransport *string
		err := tx.QueryRow(ctx, `
SELECT feed_schedule_config_id::text, direction_time::text, correction_time::text,
       transport_time::text, valid_from::text
FROM feed_schedule_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND workflow = $3
  AND valid_to IS NULL
FOR UPDATE`, cmd.TenantID, cmd.ParkID, cmd.Workflow).
			Scan(&openID, &openDirection, &openCorrection, &openTransport, &openValidFrom)

		switch {
		case errors.Is(err, pgx.ErrNoRows):
			var newID string
			if err := tx.QueryRow(ctx, `
INSERT INTO feed_schedule_config (tenant_id, park_id, workflow, direction_time, correction_time,
                                  transport_time, valid_from, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4::time, $5::time, $6::time, $7::date, nullif($8,'')::uuid)
RETURNING feed_schedule_config_id::text`,
				cmd.TenantID, cmd.ParkID, cmd.Workflow, cmd.DirectionTime, cmd.CorrectionTime,
				cmd.TransportTime, cmd.EffectiveFrom, actorUUID(cmd.ActorRef)).Scan(&newID); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: insert schedule config: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeInserted, ResultRowID: newID}, nil
		case err != nil:
			return writeEffect{}, fmt.Errorf("feedconfig: lock schedule config: %w", err)
		}

		// All THREE times are compared, transport included. Comparing only the two NOT NULL ones would
		// silently swallow an edit that changed nothing but the transport cutoff -- reporting
		// "unchanged" for a change the author really made.
		if openDirection == cmd.DirectionTime && openCorrection == cmd.CorrectionTime &&
			ptrEqual(openTransport, cmd.TransportTime) {
			return writeEffect{Outcome: domain.OutcomeUnchanged, ResultRowID: openID}, nil
		}
		if openValidFrom > cmd.EffectiveFrom {
			return writeEffect{}, ports.ErrFutureDatedRow
		}
		if openValidFrom == cmd.EffectiveFrom {
			if _, err := tx.Exec(ctx, `
UPDATE feed_schedule_config
SET direction_time = $2::time, correction_time = $3::time, transport_time = $4::time, updated_at = now()
WHERE feed_schedule_config_id = $1::uuid`,
				openID, cmd.DirectionTime, cmd.CorrectionTime, cmd.TransportTime); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: correct schedule config: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeCorrected, ResultRowID: openID}, nil
		}

		if _, err := tx.Exec(ctx, `
UPDATE feed_schedule_config
SET valid_to = $2::date, updated_at = now()
WHERE feed_schedule_config_id = $1::uuid`, openID, cmd.EffectiveFrom); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: close schedule config: %w", err)
		}
		var newID string
		if err := tx.QueryRow(ctx, `
INSERT INTO feed_schedule_config (tenant_id, park_id, workflow, direction_time, correction_time,
                                  transport_time, valid_from, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4::time, $5::time, $6::time, $7::date, nullif($8,'')::uuid)
RETURNING feed_schedule_config_id::text`,
			cmd.TenantID, cmd.ParkID, cmd.Workflow, cmd.DirectionTime, cmd.CorrectionTime,
			cmd.TransportTime, cmd.EffectiveFrom, actorUUID(cmd.ActorRef)).Scan(&newID); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: insert superseding schedule config: %w", err)
		}
		return writeEffect{Outcome: domain.OutcomeSuperseded, ResultRowID: newID, SupersededRowID: openID}, nil
	})
}

// UpsertExperimentConfig authors one experiment shed's ABSOLUTE kg of one feed item.
//
// TWO-WAY, NOT THREE-WAY. feed_experiment_config has no valid_from/valid_to (see migration 000006),
// so there is no close-and-open branch here and no 'superseded' outcome: an existing row is
// corrected in place. That is not a weaker version of the ration-rate contract, it is a different
// table with a different guarantee -- an experiment quantity is a hand-entered figure for a running
// trial, not a standing rule whose past values must stay reconstructable to explain an old feed
// sheet. The write ledger still records who changed what, when, and to which row.
//
// AN UPDATE ALSO FORCES status BACK TO 'active'. Authoring a quantity for a withdrawn shed and
// leaving it retired would store a number nothing reads: the direction path filters on
// status = 'active'. Re-authoring a cell is an unambiguous statement that this shed is on the
// experiment workflow.
func (r *Repository) UpsertExperimentConfig(ctx context.Context, cmd domain.UpsertExperimentConfigCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindExperimentConfig, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}
		if err := requireShedInPark(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID); err != nil {
			return writeEffect{}, err
		}

		// Lock the row for this key. FOR UPDATE so two concurrent edits of the SAME cell serialize
		// instead of both deciding "no row" and racing into feed_experiment_config_natural_key_uidx.
		//
		// partition_key is part of the predicate because it is part of that unique key. Without it
		// this QueryRow matched EVERY pen of a partitioned shed -- ten rows for Mandela 1 -- so an
		// edit either failed or locked an arbitrary pen and wrote the author's number onto it.
		// feed_experiment_partition_key mirrors the column's own generation expression, so the
		// predicate and the index can never disagree about what 'whole' means.
		var openID, openKg, openCategory, openStatus string
		var openHeadCount *int32
		partitionKey := feedExperimentPartitionKey(cmd.PartitionLabel)
		err := tx.QueryRow(ctx, `
SELECT experiment_config_id::text, absolute_kg::text, head_count, experiment_category, status
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND partition_key = $5
  AND feed_item_key = feed_config_norm($4)
FOR UPDATE`, cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.FeedItemLabel, partitionKey).
			Scan(&openID, &openKg, &openHeadCount, &openCategory, &openStatus)

		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// First authored cell for this (shed, item). If it is also the shed's first cell overall,
			// this write is what ENROLS the shed onto the experiment workflow -- membership is the flag.
			var newID string
			// partition_label is written; partition_key is GENERATED from it, so the pen the author
			// clicked is the pen the row belongs to. Omitting the label here defaulted every insert
			// to the 'whole' sentinel, quietly creating a shed-wide row beside the real pens.
			if err := tx.QueryRow(ctx, `
INSERT INTO feed_experiment_config (tenant_id, park_id, shed_id, partition_label, feed_item_label,
                                    absolute_kg, head_count, experiment_category, status, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, nullif($9,''), $4, $5::numeric, $6, $7, 'active', nullif($8,'')::uuid)
RETURNING experiment_config_id::text`,
				cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.FeedItemLabel, cmd.AbsoluteKg,
				cmd.HeadCount, cmd.ExperimentCategory, actorUUID(cmd.ActorRef),
				strings.TrimSpace(cmd.PartitionLabel)).Scan(&newID); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: insert experiment config: %w", err)
			}
			// See reactivateExperimentShed: a brand-new cell inserted 'active' into a shed that
			// still carries OTHER retired rows would leave the shed mixed-status, which
			// ExperimentPlanner.Applies reads as "enrolled" while feeding only the active subset.
			if err := reactivateExperimentShed(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID); err != nil {
				return writeEffect{}, err
			}
			// CR-07: sync shed-level metadata to every OTHER row of this shed. head_count and
			// experiment_category are shed-level facts (see syncExperimentShedMetadata), not
			// per-item ones, even though this table stores one row per (shed, feed item).
			if err := syncExperimentShedMetadata(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID, partitionKey, newID, cmd.HeadCount, cmd.ExperimentCategory); err != nil {
				return writeEffect{}, err
			}
			return writeEffect{Outcome: domain.OutcomeInserted, ResultRowID: newID}, nil
		case err != nil:
			return writeEffect{}, fmt.Errorf("feedconfig: lock experiment config: %w", err)
		}

		// EVERY field is compared, status included. Comparing only the quantity would report
		// "unchanged" for an edit that re-enrolled a withdrawn shed — which is a change of workflow,
		// the largest change this screen can make.
		if openKg == cmd.AbsoluteKg && openCategory == cmd.ExperimentCategory &&
			openStatus == domain.ExperimentStatusActive && int32PtrEqual(openHeadCount, cmd.HeadCount) {
			return writeEffect{Outcome: domain.OutcomeUnchanged, ResultRowID: openID}, nil
		}
		if _, err := tx.Exec(ctx, `
UPDATE feed_experiment_config
SET absolute_kg = $2::numeric,
    head_count = $3,
    experiment_category = $4,
    status = 'active',
    updated_at = now()
WHERE experiment_config_id = $1::uuid`,
			openID, cmd.AbsoluteKg, cmd.HeadCount, cmd.ExperimentCategory); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: correct experiment config: %w", err)
		}
		// CR-07: sync shed-level metadata to every OTHER row of this shed (see
		// syncExperimentShedMetadata and the insert branch above).
		if err := syncExperimentShedMetadata(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID, partitionKey, openID, cmd.HeadCount, cmd.ExperimentCategory); err != nil {
			return writeEffect{}, err
		}
		// ROOT-CAUSE FIX (P1 follow-up): editing ONE cell of a retired shed must not leave the
		// shed mixed active/retired. SetExperimentShedStatus documents the invariant that
		// ExperimentPlanner.Applies enrols a shed the moment ANY row is active, and
		// feeddirection's LoadConfigSnapshot loads ONLY active rows and treats that set as the
		// COMPLETE experiment list for the shed (see ExperimentPlanner.PlanDaily). Before this
		// fix, re-authoring one retired cell flipped ONLY that row back to 'active', so a
		// retire-five/edit-one sequence silently produced a 1-item direction instead of 5 -- an
		// underfeed that never surfaced as an error. Reactivating the WHOLE shed atomically, in
		// the SAME transaction as the edit, is the safer fix versus disallowing the edit
		// outright: it matches what an operator editing a retired cell actually means
		// ("this shed is back on the experiment workflow"), and it cannot race with
		// SetExperimentShedStatus because both lock the shed's rows with the same
		// `FOR UPDATE ... WHERE shed_id = $3` pattern.
		if err := reactivateExperimentShed(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID); err != nil {
			return writeEffect{}, err
		}
		return writeEffect{Outcome: domain.OutcomeCorrected, ResultRowID: openID}, nil
	})
}

// reactivateExperimentShed brings EVERY row of one shed's experiment config back to 'active' in a
// single set-based UPDATE. It is called whenever UpsertExperimentConfig re-enrols a shed (by
// inserting a new cell or correcting an existing one), so a shed can never end this transaction
// with some rows active and some retired -- the exact mixed state that would make
// ExperimentPlanner.Applies enrol the shed while feeddirection's LoadConfigSnapshot loads only
// the active subset, silently truncating the generated direction.
//
// scale-guard:ignore: one set-based UPDATE over ONE shed's authored experiment cells (bounded by
// the feed-item catalog, a handful of rows per shed today), never a per-row loop.
// syncExperimentShedMetadata propagates head_count and experiment_category to every OTHER row of
// the given shed (CR-07).
//
// feed_experiment_config stores one row per (shed, PEN, feed item), and head_count and
// experiment_category are PEN-LEVEL facts: "how many animals are here" and "which arm is this on"
// do not vary by feed item, but they absolutely do vary by pen. ExperimentPlanner.PlanDaily reads
// them off cells[0] -- the first row for the group in whatever order the config snapshot loaded
// them -- so an edit that updated only its own row would leave siblings stale and the generated
// direction could serve an arbitrary sibling's values instead of the one just entered. This keeps
// the group in step.
//
// SCOPED TO THE PEN, not the shed. It was shed-wide on the stated assumption that these are
// "shed-level facts", which was true before this table became partition-aware and is now false:
// Mandela 1's ten pens each carry their own arm and head count (Part 1 = Sheep M NEW/10,
// Part 10 = B+S Goat F NEW/15). A shed-wide sweep flattened all ten to whichever pen was edited,
// destroying hand-keyed authored data with no way to recover it. The pen predicate is what makes
// an edit to one pen leave its neighbours alone.
//
// Called from the SAME transaction as the per-cell insert/update, immediately after it, so the
// pen's rows are consistent by the time the transaction commits -- there is never a window where a
// reader sees the edited cell's new metadata beside a sibling's old metadata.
func syncExperimentShedMetadata(ctx context.Context, tx pgx.Tx, tenantID, parkID, shedID, partitionKey, editedRowID string, headCount *int32, category string) error {
	if _, err := tx.Exec(ctx, `
UPDATE feed_experiment_config
SET head_count = $5,
    experiment_category = $6,
    updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND partition_key = $7
  AND experiment_config_id <> $4::uuid
  AND (head_count IS DISTINCT FROM $5 OR experiment_category IS DISTINCT FROM $6)`,
		tenantID, parkID, shedID, editedRowID, headCount, category, partitionKey); err != nil {
		return fmt.Errorf("feedconfig: sync experiment pen metadata: %w", err)
	}
	return nil
}

// feedExperimentPartitionKey mirrors the partition_key generation expression on
// feed_experiment_config exactly:
//
//	CASE WHEN partition_label IS NULL OR btrim(partition_label) = '' THEN 'whole'
//	     ELSE lower(btrim(partition_label)) END
//
// It exists so the write path's predicates key on the same value the generated column and its
// unique index hold. Re-deriving it inline at each call site is how the two drift.
func feedExperimentPartitionKey(label string) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return "whole"
	}
	return strings.ToLower(trimmed)
}

func reactivateExperimentShed(ctx context.Context, tx pgx.Tx, tenantID, parkID, shedID string) error {
	if _, err := tx.Exec(ctx, `
UPDATE feed_experiment_config
SET status = 'active', updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND status <> 'active'`, tenantID, parkID, shedID); err != nil {
		return fmt.Errorf("feedconfig: reactivate experiment shed: %w", err)
	}
	return nil
}

// SetExperimentShedStatus switches a WHOLE SHED between the experiment workflow and the normal
// per-head ration grid.
//
// ONE set-based UPDATE over the shed's rows, not a loop: the flip must be atomic in the business
// sense as well as the transactional one. A shed with some rows active and some retired would be
// enrolled (ExperimentPlanner.Applies matches on ANY active row) but fed only a subset of its
// authored items — a partially-fed experiment shed, which is worse than either whole state.
//
// A shed with NO rows is ErrShedNotFound rather than a silent success. There is no experiment
// configuration to switch, and inventing empty rows to carry a status would author cells nobody
// entered; a caller wanting to enrol a shed authors its first quantity instead.
func (r *Repository) SetExperimentShedStatus(ctx context.Context, cmd domain.SetExperimentShedStatusCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindExperimentConfig, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}
		if err := requireShedInPark(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.ShedID); err != nil {
			return writeEffect{}, err
		}

		// Lock the shed's rows and learn what state it is currently in, in one read. Ordered so two
		// concurrent flips of two sheds cannot deadlock on overlapping lock acquisition order.
		rows, err := tx.Query(ctx, `
SELECT experiment_config_id::text, status
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
ORDER BY experiment_config_id
FOR UPDATE`, cmd.TenantID, cmd.ParkID, cmd.ShedID)
		if err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: lock experiment shed: %w", err)
		}
		var firstID string
		alreadyInState := true
		count := 0
		for rows.Next() {
			var id, status string
			if err := rows.Scan(&id, &status); err != nil {
				rows.Close()
				return writeEffect{}, fmt.Errorf("feedconfig: scan experiment shed: %w", err)
			}
			if count == 0 {
				firstID = id
			}
			count++
			if status != cmd.Status {
				alreadyInState = false
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: lock experiment shed: %w", err)
		}
		if count == 0 {
			return writeEffect{}, ports.ErrShedNotFound
		}
		if alreadyInState {
			// Every row already carries the requested status. No write, and no ledger claim that a
			// workflow changed when it did not.
			return writeEffect{Outcome: domain.OutcomeUnchanged, ResultRowID: firstID}, nil
		}

		// scale-guard:ignore: one set-based UPDATE over ONE shed's authored experiment cells (5 rows today, bounded by the feed-item catalog); it is the whole-shed atomic flip described above, never a per-row loop.
		if _, err := tx.Exec(ctx, `
UPDATE feed_experiment_config
SET status = $4, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND status IS DISTINCT FROM $4`,
			cmd.TenantID, cmd.ParkID, cmd.ShedID, cmd.Status); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: set experiment shed status: %w", err)
		}
		return writeEffect{Outcome: domain.OutcomeCorrected, ResultRowID: firstID}, nil
	})
}

// ---------------------------------------------------------------------------
// The shared write envelope
// ---------------------------------------------------------------------------

// writeEffect is what an individual upsert body decided and did.
type writeEffect struct {
	Outcome         string
	ResultRowID     string
	SupersededRowID string
}

// runWrite wraps every authored write in the module's idempotency contract, so no individual upsert
// can ship without it.
//
// The sequence, all inside ONE transaction:
//
//  1. Replay lookup on (tenant, idempotency_key). A hit with a MATCHING fingerprint returns the
//     original result and runs no side effect. A hit with a DIFFERENT fingerprint is a conflict:
//     two different edits are claiming one identity and guessing between them would write a ration
//     nobody authored.
//  2. The effect itself (the closure), which locks the open row and performs the effective-dated
//     close/insert/correct.
//  3. The ledger insert, carrying the key, the fingerprint, and the row ids the effect produced.
//
// Because 2 and 3 share the transaction, a ledger entry exists if and only if its side effects
// committed. There is no best-effort post-commit path.
//
// Step 3's unique-index violation is the CONCURRENT duplicate case: another transaction committed
// the same key between our step 1 and our step 3. We roll back (discarding our now-duplicate side
// effects) and re-read the winner's result, which is the same answer the client would have got had
// the two requests arrived in series.
func (r *Repository) runWrite(
	ctx context.Context,
	kind string,
	identity domain.WriteIdentity,
	effect func(ctx context.Context, tx pgx.Tx) (writeEffect, error),
) (domain.WriteResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if out, found, err := lookupWriteLog(ctx, tx, identity); err != nil || found {
		return out, err
	}

	eff, err := effect(ctx, tx)
	if err != nil {
		return domain.WriteResult{}, err
	}

	var writeID string
	err = tx.QueryRow(ctx, `
INSERT INTO feed_config_write_log (
  tenant_id, write_kind, idempotency_key, request_fingerprint, outcome,
  result_row_id, superseded_row_id, effective_from, actor_ref
) VALUES (
  $1::uuid, $2, $3, $4, $5,
  nullif($6,'')::uuid, nullif($7,'')::uuid, $8::date, $9
)
RETURNING feed_config_write_id::text`,
		identity.TenantID, kind, identity.IdempotencyKey, identity.RequestFingerprint, eff.Outcome,
		eff.ResultRowID, eff.SupersededRowID, identity.EffectiveFrom, identity.ActorRef).Scan(&writeID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == writeLogIdempotencyConstraint {
			// A concurrent request with the same key won. Discard our duplicate side effects and answer
			// with the winner's result -- read on a FRESH transaction, because ours is now aborted.
			_ = tx.Rollback(ctx)
			return r.replayCommittedWrite(ctx, identity)
		}
		return domain.WriteResult{}, fmt.Errorf("feedconfig: record write log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.WriteResult{}, err
	}
	return domain.WriteResult{
		WriteID:         writeID,
		Kind:            kind,
		Outcome:         eff.Outcome,
		ResultRowID:     eff.ResultRowID,
		SupersededRowID: eff.SupersededRowID,
		EffectiveFrom:   identity.EffectiveFrom,
	}, nil
}

// replayCommittedWrite re-reads the winner of a concurrent same-key race, outside the aborted
// transaction.
func (r *Repository) replayCommittedWrite(ctx context.Context, identity domain.WriteIdentity) (domain.WriteResult, error) {
	out, found, err := lookupWriteLog(ctx, r.pool, identity)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if !found {
		// The unique index fired but the row is not readable. That is not a state this schema can
		// reach through the normal path, so report it rather than silently retrying the write.
		return domain.WriteResult{}, fmt.Errorf("feedconfig: idempotency key %q conflicted but its write log row is unreadable", identity.IdempotencyKey)
	}
	return out, nil
}

// writeLogQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so the replay lookup runs
// identically inside the write transaction and on the fresh connection used after a race.
type writeLogQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// lookupWriteLog is the replay half of the idempotency contract.
//
// A matching fingerprint returns the ORIGINAL result with Replayed=true and no side effects. A
// differing fingerprint is ErrIdempotencyConflict -- never a second write, and never a silent
// overwrite of the first edit.
func lookupWriteLog(ctx context.Context, q writeLogQuerier, identity domain.WriteIdentity) (domain.WriteResult, bool, error) {
	var out domain.WriteResult
	var storedFingerprint string
	var resultRow, supersededRow *string
	err := q.QueryRow(ctx, `
SELECT feed_config_write_id::text, write_kind, request_fingerprint, outcome,
       result_row_id::text, superseded_row_id::text, effective_from::text
FROM feed_config_write_log
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, identity.TenantID, identity.IdempotencyKey).
		Scan(&out.WriteID, &out.Kind, &storedFingerprint, &out.Outcome, &resultRow, &supersededRow, &out.EffectiveFrom)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WriteResult{}, false, nil
	}
	if err != nil {
		return domain.WriteResult{}, false, fmt.Errorf("feedconfig: load write log: %w", err)
	}
	if storedFingerprint != identity.RequestFingerprint {
		return domain.WriteResult{}, false, ports.ErrIdempotencyConflict
	}
	out.ResultRowID = derefString(resultRow)
	out.SupersededRowID = derefString(supersededRow)
	out.Replayed = true
	return out, true, nil
}

// requireLocation fails a write CLOSED when the addressed park/shed does not exist in the caller's
// tenant with the expected type.
//
// The foreign keys already guarantee the row exists, but not its TYPE: without this, a shed id
// passed as park_id would satisfy the FK and author configuration under a scope the feed direction
// path will never look at. A silent success there looks to the author exactly like a saved edit.
func requireLocation(ctx context.Context, tx pgx.Tx, tenantID, locationID, locationType string, notFound error) error {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT true
FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid AND location_type = $3`,
		tenantID, locationID, locationType).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}
	if err != nil {
		return fmt.Errorf("feedconfig: resolve %s: %w", locationType, err)
	}
	return nil
}

// requireShedInPark verifies shedID is an active shed whose parent_location_id is parkID.
// requireLocation on ParkID and ShedID independently only proves each location EXISTS with the
// right type; it does not prove the shed actually belongs to the given park, so a caller could
// author feed config for shed-from-park-A scoped under park-B (CR-03). This is the single place
// that closes that gap: any write path taking both a park and a shed argument must call this
// instead of two independent requireLocation calls for the shed side.
func requireShedInPark(ctx context.Context, tx pgx.Tx, tenantID, parkID, shedID string) error {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT true
FROM locations
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
  AND location_type = 'shed'
  AND parent_location_id = $3::uuid
  AND status = 'active'`,
		tenantID, shedID, parkID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrShedNotFound
	}
	if err != nil {
		return fmt.Errorf("feedconfig: resolve shed in park: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// trimPage turns a Limit+1 fetch into a Limit-sized page plus has_more.
func trimPage[T any](items []T, limit int32) ([]T, bool) {
	if int32(len(items)) > limit {
		return items[:limit], true
	}
	return items, false
}

// nullIfEmpty maps an absent filter to SQL NULL, which every filter predicate reads as "no filter".
// An empty string must never be bound literally: it would match nothing rather than everything.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// int32PtrEqual compares two nullable head counts. NULL ("not recorded") equals only NULL: it is a
// different state from an authored 0, which would state the shed is empty.
func int32PtrEqual(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func ptrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// actorUUID passes the actor through to created_by only when it looks like a uuid; created_by is a
// uuid column while actor_ref on the ledger is free text (it may be a service principal). The
// ledger's actor_ref is the authoritative audit field, so a non-uuid actor loses nothing here.
func actorUUID(actorRef string) string {
	if len(actorRef) == 36 && actorRef[8] == '-' && actorRef[13] == '-' && actorRef[18] == '-' && actorRef[23] == '-' {
		return actorRef
	}
	return ""
}
