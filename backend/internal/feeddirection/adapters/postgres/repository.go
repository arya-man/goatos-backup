// Package postgres reads the authored configuration and the shed catalog that feed-direction
// generation resolves against.
//
// EVERY READ HERE IS SET-BASED AND BOUNDED. The whole authored config for a park is loaded in one
// batch of eight independent statements, once per request -- never per shed, never per grain, and
// never per session.
// The alternative shape (resolve a rate when a grain needs it) is the N+1 fan-out the scale rules
// ban, and it would be doubly bad here because the grid is small: it would trade ~700 rows of one
// read for hundreds of round trips.
//
// This package writes NOTHING. feedconfig is the only writer of the feed_* config tables, and
// counts is the only reader of the census. Reading the shared locations catalog is the one
// cross-module read, and it is the same read feedconfig's own FKs rely on.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

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

var _ ports.ConfigRepository = (*Repository)(nil)

// ---------------------------------------------------------------------------
// Shed scope
// ---------------------------------------------------------------------------

// MaxShedScopeRows bounds the unpaged scope read.
//
// It is a FAIL-CLOSED TRIPWIRE, not a page size. The bound is set well above the largest live park
// (78 active sheds) with room for a farm that doubles its sheds, so a legitimate request never
// reaches it; if one ever does, the premise that makes this read safe -- that the filtered scope is
// bounded by physical infrastructure -- has stopped holding, and the right answer is to say so
// rather than to return a total covering an arbitrary prefix of the park.
const MaxShedScopeRows = 500

// MaxRationRateRows and MaxSessionItemRows are the same fail-closed tripwire pattern as
// MaxShedScopeRows (P2-CAP), applied to loadRates and loadSessionItems. Both currently read
// small, bounded-by-authored-config sets (~721 rates, ~10 slots for the largest live park) with no
// LIMIT/+1 tripwire at all before this fix -- unlike ListShedScope and
// ProjectedGrainsForSheds in the same package, which already fail closed rather than silently
// truncate. A config table can in principle grow past its current size (more parks, more ration
// groups, more feed items), and a silently truncated ration grid is the same wrong-pack-weight
// failure mode ListShedScope's comment describes: some cells would resolve off a partial grid
// while looking complete. 10x the current largest live park's row count with headroom, well below
// any realistic per-request Postgres/memory cost.
const (
	MaxRationRateRows  = 5000
	MaxSessionItemRows = 500
)

// scale-guard:ignore: UNPAGED read of ONE park's active SHED CATALOG, bounded by physical infrastructure (sheds a farm has built) and NOT by herd size, animals, obligations, or events -- 78 rows in the largest live park against a hard MaxShedScopeRows=500 tripwire that FAILS the request rather than truncating. This is the set the whole-scope summary must cover; a paged read here can only produce the page-scoped total that this query exists to fix. Covered by locations_tenant_parent_status_idx (tenant_id, parent_location_id, status, display_order, name, location_id). Re-check this premise if a park's shed count ever approaches the tripwire.
const shedScopeSQL = `
SELECT l.location_id::text,
       COALESCE(NULLIF(l.name, ''), l.location_code, '') AS shed_label
FROM locations l
WHERE l.tenant_id = $1::uuid
  AND l.parent_location_id = $2::uuid
  AND l.location_type = 'shed'
  AND l.status = 'active'
  AND ($3 = '' OR l.location_id = NULLIF($3, '')::uuid)
ORDER BY l.display_order, l.name, l.location_id
LIMIT $4`

// scale-guard:ignore: UNPAGED read of the tenant's ACTIVE PARK catalog, bounded by physical infrastructure (a tenant has order-of two parks) and NOT by herd size, animals, obligations, or events. Covered by locations_tenant_type_status_idx. This is the farm filter vocabulary + default-park source; a park list that grew with the herd would be a different query.
const listParksSQL = `
SELECT l.location_id::text,
       COALESCE(NULLIF(l.location_code, ''), l.name, '') AS park_label
FROM locations l
WHERE l.tenant_id = $1::uuid
  AND l.location_type = 'park'
  AND l.status = 'active'
ORDER BY l.display_order, l.name, l.location_id
LIMIT $2`

// MaxParkRows caps the park catalog read. A tenant has order-of two parks; the cap is generous
// headroom that FAILS closed rather than truncating, on the same principle as MaxShedScopeRows.
const MaxParkRows = 200

// ListParks returns every active park in the tenant, ordered deterministically. It is the farm
// filter vocabulary the feed screens render and the default-park source when a request omits
// park_id.
func (r *Repository) ListParks(ctx context.Context, tenantID string) ([]ports.Park, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, listParksSQL, tenantID, MaxParkRows+1)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list parks: %w", err)
	}
	defer rows.Close()

	out := make([]ports.Park, 0)
	for rows.Next() {
		var park ports.Park
		if err := rows.Scan(&park.ParkID, &park.Label); err != nil {
			return nil, fmt.Errorf("feeddirection: scan park: %w", err)
		}
		out = append(out, park)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feeddirection: list parks: %w", err)
	}
	if len(out) > MaxParkRows {
		return nil, fmt.Errorf("%w: tenant has more than %d active parks", ports.ErrScopeTooLarge, MaxParkRows)
	}
	return out, nil
}

// ListShedScope returns every active shed matching the request filters, unpaged.
//
// ONE QUERY, ONE POPULATION. This replaced a separate paged shed query: the service now slices its
// page out of this ordered scope in memory. That is deliberate -- two separately-predicated queries
// could select different populations, and the summary would then describe a set the visible rows do
// not come from. Ordering is stable (display_order, name, location_id) so the in-memory slice is a
// real page.
//
// It reads MaxShedScopeRows+1 and FAILS on overflow rather than returning the first N. See
// ports.ErrScopeTooLarge: a quietly truncated scope is a quietly short pack weight.
func (r *Repository) ListShedScope(ctx context.Context, q ports.ShedScopeQuery) (ports.ShedScope, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if err := r.requirePark(ctx, q.TenantID, q.ParkID); err != nil {
		return ports.ShedScope{}, err
	}

	rows, err := r.pool.Query(ctx, shedScopeSQL, q.TenantID, q.ParkID, q.ShedID, MaxShedScopeRows+1)
	if err != nil {
		return ports.ShedScope{}, fmt.Errorf("feeddirection: list shed scope: %w", err)
	}
	defer rows.Close()

	out := ports.ShedScope{Items: []ports.Shed{}}
	for rows.Next() {
		var shed ports.Shed
		if err := rows.Scan(&shed.ShedID, &shed.Label); err != nil {
			return ports.ShedScope{}, fmt.Errorf("feeddirection: scan scope shed: %w", err)
		}
		out.Items = append(out.Items, shed)
	}
	if err := rows.Err(); err != nil {
		return ports.ShedScope{}, fmt.Errorf("feeddirection: list shed scope: %w", err)
	}
	if len(out.Items) > MaxShedScopeRows {
		return ports.ShedScope{}, fmt.Errorf(
			"%w: park %s has more than %d active sheds", ports.ErrScopeTooLarge, q.ParkID, MaxShedScopeRows)
	}
	return out, nil
}

// requirePark fails a request for a park that does not exist in the caller's tenant.
//
// It fails CLOSED rather than returning an empty page. An empty page for a mistyped park id looks
// exactly like a park with no sheds, and an operator would read "nothing to feed today" from what
// is actually a bad request.
func (r *Repository) requirePark(ctx context.Context, tenantID, parkID string) error {
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT true
FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid AND location_type = 'park'`,
		tenantID, parkID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrParkNotFound
	}
	if err != nil {
		return fmt.Errorf("feeddirection: resolve park: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Config snapshot
// ---------------------------------------------------------------------------

// LoadConfigSnapshot reads the whole authored configuration for one park, as of a business date.
//
// Eight independent set-based statements, issued once. They are sequential rather than batched
// into a pipeline because each is a small indexed read on a configuration table and the total is
// well inside the request budget; what matters for scale is that the count is CONSTANT -- it does
// not grow with sheds, grains, animals, or feed items.
//
// EFFECTIVE DATING. The rates, shed factors, and experiment rows are selected AS OF the target
// business date, not by valid_to IS NULL. Those are different questions: "what is in force now"
// versus "what was in force on the day being planned". Using the open row for a back-dated
// regeneration would reprint history with today's prices.
func (r *Repository) LoadConfigSnapshot(ctx context.Context, tenantID, parkID string, asOf time.Time) (domain.ConfigSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	snapshot := domain.ConfigSnapshot{
		ParkID:                parkID,
		ShedTagsByKey:         map[string]domain.ShedTag{},
		RationGroupByBreedKey: map[string]string{},
		FeedItems:             []domain.FeedItem{},
		RatesByKey:            map[string]domain.RationRate{},
		ShedFactorsByKey:      map[string]string{},
		Sessions:              []domain.SessionTemplate{},
		ExperimentByShedID:    map[string][]domain.ExperimentCell{},
	}

	// The business date is formatted in Go from an already-normalized Asia/Kolkata business-day
	// start. SQL now()/CURRENT_DATE is never used: the database's idea of "today" is UTC-based, and
	// a late-evening IST request would select the previous day's rates.
	asOfDate := asOf.Format("2006-01-02")

	if err := r.loadParkLabel(ctx, &snapshot, tenantID, parkID); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadShedTags(ctx, &snapshot, tenantID); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadRationGroups(ctx, &snapshot, tenantID); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadFeedItems(ctx, &snapshot, tenantID); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadRates(ctx, &snapshot, tenantID, parkID, asOfDate); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadShedFactors(ctx, &snapshot, tenantID, parkID, asOfDate); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadSessions(ctx, &snapshot, tenantID, parkID, asOfDate); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	if err := r.loadExperiments(ctx, &snapshot, tenantID, parkID); err != nil {
		return domain.ConfigSnapshot{}, err
	}
	return snapshot, nil
}

func (r *Repository) loadParkLabel(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID, parkID string) error {
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(location_code, ''), name, '')
FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid`, tenantID, parkID).Scan(&snapshot.ParkLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrParkNotFound
	}
	if err != nil {
		return fmt.Errorf("feeddirection: load park label: %w", err)
	}
	return nil
}

// loadShedTags reads the authored tag vocabulary -- 31 rows tenant-wide.
//
// It carries applies_to, which is the field the ENTIRE kid/adult branch turns on. Retired tags are
// excluded: a retired tag is no longer part of the authored vocabulary, so an animal still
// carrying it must block rather than silently resolve through a rule the farm withdrew.
func (r *Repository) loadShedTags(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID string) error {
	rows, err := r.pool.Query(ctx, `
SELECT shed_tag_key, shed_tag_label, applies_to
FROM feed_shed_tags
WHERE tenant_id = $1::uuid AND status = 'active'`, tenantID)
	if err != nil {
		return fmt.Errorf("feeddirection: load shed tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, label, appliesTo string
		if err := rows.Scan(&key, &label, &appliesTo); err != nil {
			return fmt.Errorf("feeddirection: scan shed tag: %w", err)
		}
		snapshot.ShedTagsByKey[key] = domain.ShedTag{Label: label, AppliesTo: appliesTo}
	}
	return rows.Err()
}

// loadRationGroups reads the ADULT breed -> ration group map (7 rows). Kids bypass it entirely.
func (r *Repository) loadRationGroups(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID string) error {
	rows, err := r.pool.Query(ctx, `
SELECT breed_key, ration_group_label
FROM feed_ration_groups
WHERE tenant_id = $1::uuid`, tenantID)
	if err != nil {
		return fmt.Errorf("feeddirection: load ration groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, label string
		if err := rows.Scan(&key, &label); err != nil {
			return fmt.Errorf("feeddirection: scan ration group: %w", err)
		}
		snapshot.RationGroupByBreedKey[key] = label
	}
	return rows.Err()
}

// loadFeedItems reads the active catalog in authored display order. That order becomes the column
// order on every row and in every summary, so it is applied here once rather than re-sorted later.
func (r *Repository) loadFeedItems(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID string) error {
	rows, err := r.pool.Query(ctx, `
SELECT feed_item_key, feed_item_label
FROM feed_item_catalog
WHERE tenant_id = $1::uuid AND status = 'active'
ORDER BY display_order, feed_item_label`, tenantID)
	if err != nil {
		return fmt.Errorf("feeddirection: load feed items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, label string
		if err := rows.Scan(&key, &label); err != nil {
			return fmt.Errorf("feeddirection: scan feed item: %w", err)
		}
		snapshot.FeedItems = append(snapshot.FeedItems, domain.FeedItem{Label: label, Key: key})
	}
	return rows.Err()
}

// loadRates reads the ration grid in force on the target business date -- ~721 rows for a park.
//
// NO COALESCE, NO LEFT JOIN, NO DEFAULT. Only rows that exist are returned; a cell with no row is
// simply absent from the map, and the generator turns that absence into a BLOCKED quantity. This
// is migration 000003's safety-critical rule, and it is enforced by what this query does NOT do as
// much as by what it does.
//
// The as-of window is [valid_from, valid_to): valid_to is the day the successor takes effect, so a
// row is in force up to but not including it.
func (r *Repository) loadRates(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID, parkID, asOfDate string) error {
	// scale-guard:ignore: bounded set-based read of ONE park's authored ration grid (~721 rows live), loaded once per request rather than once per grain, capped at MaxRationRateRows+1 with a fail-closed ErrScopeTooLarge tripwire (P2-CAP) rather than a silent truncation. The predicate is covered by feed_ration_rates_asof_lookup_idx (tenant_id, park_id, ration_group_key, shed_tag_key, valid_from DESC); every bind is cast, never the indexed column.
	rows, err := r.pool.Query(ctx, `
SELECT ration_group_key, shed_tag_key, feed_item_key, grams_per_head::text
FROM feed_ration_rates
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND valid_from <= $3::date
  AND (valid_to IS NULL OR valid_to > $3::date)
LIMIT $4`, tenantID, parkID, asOfDate, MaxRationRateRows+1)
	if err != nil {
		return fmt.Errorf("feeddirection: load ration rates: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var groupKey, tagKey, itemKey, grams string
		if err := rows.Scan(&groupKey, &tagKey, &itemKey, &grams); err != nil {
			return fmt.Errorf("feeddirection: scan ration rate: %w", err)
		}
		count++
		if count > MaxRationRateRows {
			return fmt.Errorf(
				"%w: park %s has more than %d ration rate rows",
				ports.ErrScopeTooLarge, parkID, MaxRationRateRows)
		}
		// The stored *_key columns are GENERATED by feed_config_norm, and RateKey normalizes with
		// the Go twin of that function. Applying it to an already-normalized key is a no-op
		// (feed_config_norm is idempotent), so the two sides land on the same string.
		snapshot.RatesByKey[domain.RateKey(groupKey, tagKey, itemKey)] = domain.RationRate{GramsPerHead: grams}
	}
	return rows.Err()
}

// loadShedFactors reads the per-shed multipliers in force on the target date.
//
// Loaded for the WHOLE park in one read rather than per shed. A missing entry reads as 1.0 in the
// generator, which is safe in a way a missing RATE is not: declining to scale a ration cannot
// starve a shed, whereas inventing one can.
func (r *Repository) loadShedFactors(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID, parkID, asOfDate string) error {
	// scale-guard:ignore: bounded set-based read of ONE park's authored shed multipliers, loaded once per request. Covered by feed_shed_factors_current_lookup_idx / the natural-key index; binds are cast, the indexed columns are bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, feed_item_key, multiplier::text
FROM feed_shed_factors
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND valid_from <= $3::date
  AND (valid_to IS NULL OR valid_to > $3::date)`, tenantID, parkID, asOfDate)
	if err != nil {
		return fmt.Errorf("feeddirection: load shed factors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var shedID, itemKey, multiplier string
		if err := rows.Scan(&shedID, &itemKey, &multiplier); err != nil {
			return fmt.Errorf("feeddirection: scan shed factor: %w", err)
		}
		snapshot.ShedFactorsByKey[domain.ShedFactorKey(shedID, itemKey)] = multiplier
	}
	return rows.Err()
}

// loadSessions reads the park's active session split, in display order, together with the feed
// slots each session consists of.
//
// TWO statements for the whole park, not one per session. The slots are read in a single set-based
// query keyed on (tenant, park) and then attached to their sessions in memory -- a query per session
// would be the N+1 fan-out the scale rules ban, and it would buy nothing: a park has two sessions of
// five slots each.
//
// The slot read is AS OF the target business date, matching the rates and shed factors. "What did
// CBE session 2 consist of on the day being planned" is a different question from "what does it
// consist of now", and a back-dated regeneration must answer the first.
func (r *Repository) loadSessions(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID, parkID, asOfDate string) error {
	rows, err := r.pool.Query(ctx, `
SELECT session_no, session_label, split_fraction::text
FROM feed_session_templates
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND status = 'active'
ORDER BY display_order, session_no`, tenantID, parkID)
	if err != nil {
		return fmt.Errorf("feeddirection: load session templates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var session domain.SessionTemplate
		if err := rows.Scan(&session.SessionNo, &session.Label, &session.SplitFraction); err != nil {
			return fmt.Errorf("feeddirection: scan session template: %w", err)
		}
		// Non-nil even when the park declares no slots. Emptiness is a real state the generator acts
		// on (it blocks the session), so it must arrive as an empty list rather than as an
		// uninitialized field.
		session.Items = []domain.FeedItem{}
		snapshot.Sessions = append(snapshot.Sessions, session)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.loadSessionItems(ctx, snapshot, tenantID, parkID, asOfDate)
}

// loadSessionItems reads the park's authored feed slots -- the source workbook's `Template` tab --
// and attaches them to their sessions in slot (packing) order.
//
// THIS IS THE RECIPE, AND IT IS WHAT MAKES THE GENERATOR STOP WALKING THE CATALOG. Without it the
// generator asked every shed for every feed item the tenant knows about, including the four
// roughages that are substitution alternatives for one another. See migration 000005.
func (r *Repository) loadSessionItems(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID, parkID, asOfDate string) error {
	if len(snapshot.Sessions) == 0 {
		return nil
	}

	// scale-guard:ignore: bounded set-based read of ONE park's authored session slots (10 rows live -- 2 sessions x 5 slots), loaded once per request for the WHOLE park rather than once per session, capped at MaxSessionItemRows+1 with a fail-closed ErrScopeTooLarge tripwire (P2-CAP) rather than a silent truncation. Covered by feed_session_template_items_current_lookup_idx / _asof_lookup_idx (tenant_id, park_id, session_no, ...); every bind is cast, the indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT session_no, slot_no, feed_item_label, feed_item_key
FROM feed_session_template_items
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND status = 'active'
  AND valid_from <= $3::date
  AND (valid_to IS NULL OR valid_to > $3::date)
ORDER BY session_no, slot_no
LIMIT $4`, tenantID, parkID, asOfDate, MaxSessionItemRows+1)
	if err != nil {
		return fmt.Errorf("feeddirection: load session template items: %w", err)
	}
	defer rows.Close()

	bySession := map[int32][]domain.FeedItem{}
	count := 0
	for rows.Next() {
		var sessionNo, slotNo int32
		var item domain.FeedItem
		if err := rows.Scan(&sessionNo, &slotNo, &item.Label, &item.Key); err != nil {
			return fmt.Errorf("feeddirection: scan session template item: %w", err)
		}
		count++
		if count > MaxSessionItemRows {
			return fmt.Errorf(
				"%w: park %s has more than %d session template item rows",
				ports.ErrScopeTooLarge, parkID, MaxSessionItemRows)
		}
		bySession[sessionNo] = append(bySession[sessionNo], item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// In-memory attach. The loop issues no I/O -- the whole slot set was read above.
	for i := range snapshot.Sessions {
		if items, ok := bySession[snapshot.Sessions[i].SessionNo]; ok {
			snapshot.Sessions[i].Items = items
		}
	}
	return nil
}

// loadExperiments reads the park's hand-authored experiment sheds.
//
// absolute_kg is a SHED TOTAL in kg and head_count is informational -- see
// domain.ExperimentPlanner. head_count is not even read here, so it cannot be multiplied in by
// accident: the projected count from the counts module is the only head figure the generator sees,
// and the experiment planner flags it informational.
func (r *Repository) loadExperiments(ctx context.Context, snapshot *domain.ConfigSnapshot, tenantID, parkID string) error {
	// scale-guard:ignore: bounded set-based read of ONE park's hand-authored experiment sheds (an operator-entered list, tens of rows). Covered by feed_experiment_config_shed_lookup_idx (tenant_id, park_id, shed_id) WHERE status = 'active'.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, feed_item_key, feed_item_label, absolute_kg::text, experiment_category
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND status = 'active'
ORDER BY shed_id, feed_item_key`, tenantID, parkID)
	if err != nil {
		return fmt.Errorf("feeddirection: load experiment config: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var shedID string
		var cell domain.ExperimentCell
		if err := rows.Scan(&shedID, &cell.FeedItemKey, &cell.FeedItemLabel, &cell.AbsoluteKg, &cell.Category); err != nil {
			return fmt.Errorf("feeddirection: scan experiment config: %w", err)
		}
		snapshot.ExperimentByShedID[shedID] = append(snapshot.ExperimentByShedID[shedID], cell)
	}
	return rows.Err()
}
