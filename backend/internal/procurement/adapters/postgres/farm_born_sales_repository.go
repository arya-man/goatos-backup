package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// FARM BORN SALES read (maintainer request 2026-09-18, docs/decisions/sales-farm-born.md).
//
// RECORDED CROSS-MODULE REPORTING READ, the load-wise shape. Procurement owns
// procurement_load_goats and joins OUT to goats (breed, sex, stage, park, terminal outcome),
// goat_identifiers (the tag), goat_shed_partitions + locations (the pen) and
// goat_sale_allocations + sales_deals (sale date, buyer, the deal share). Read-only over every one
// of those tables and reporting grain only: nothing here gates a sale, an exit or any write; the
// sales module's own lock (migration 000173) is untouched because the dependency points the
// other way.

var _ ports.FarmBornSalesRepository = (*Repository)(nil)

// farmBornPopulationSQL is the shared front of both reads: every not-on-a-load animal, whatever
// the register's origin field says (maintainer instruction 2026-09-19), with its register facts
// and its pen resolved ONCE.
//
// The population is the exact complement of the load-wise membership (accepted rows on
// procurement_load_goats), so an animal is on exactly one of the two pages.
//
// The pen is the animal's own goat_shed_partitions row over goats.shed_id. ExitGoat never touches
// shed_id and the partition row outlives the exit, so a sold animal still reads back the pen it
// left from. A sale allocation snapshots park/shed/partition at tagging, and that snapshot wins
// for a sold animal when present -- it is where the animal was SOLD from, which is the question
// the pen breakdown answers.
const farmBornPopulationSQL = `
WITH on_load AS (
    SELECT DISTINCT plg.goat_id
    FROM public.procurement_load_goats plg
    WHERE plg.tenant_id = $1 AND plg.current_state = 'accepted_herd_intake'
),
ident AS (
    -- One tag per animal: the primary tag first, else the secondary, newest first. The same
    -- animal_identifier_1 / _2 rule the register uses; other identifier types are not tags.
    SELECT DISTINCT ON (gi.goat_id) gi.goat_id, gi.identifier_value
    FROM public.goat_identifiers gi
    WHERE gi.tenant_id = $1 AND gi.status = 'active'
      AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    ORDER BY gi.goat_id, CASE gi.identifier_type WHEN 'animal_identifier_1' THEN 0 ELSE 1 END, gi.created_at DESC
),
deal_share AS (
    -- One tagged allocation per live animal (partial unique index on (tenant, goat) WHERE
    -- status = 'tagged'), so this attaches at most 1:1. The per-deal tagged count pre-aggregates
    -- the many side, so a deal's value divides over exactly its tagged animals.
    SELECT a.goat_id, a.sales_deal_id, a.park_id, a.shed_id, a.partition_label,
           d.sale_date, d.buyer_name,
           CASE WHEN d.sales_value > 0 THEN (d.sales_value / cnt.tagged)::float8 END AS share
    FROM public.goat_sale_allocations a
    JOIN public.sales_deals d ON d.tenant_id = a.tenant_id AND d.id = a.sales_deal_id AND d.status = 'Deal Closed'
    JOIN (
        SELECT tenant_id, sales_deal_id, count(*)::numeric AS tagged
        FROM public.goat_sale_allocations
        WHERE tenant_id = $1 AND status = 'tagged'
        GROUP BY tenant_id, sales_deal_id
    ) cnt ON cnt.tenant_id = a.tenant_id AND cnt.sales_deal_id = a.sales_deal_id
    WHERE a.tenant_id = $1 AND a.status = 'tagged'
),
pop AS (
    SELECT g.goat_id, g.display_id, COALESCE(i.identifier_value, '') AS tag,
           lower(g.species) AS species, COALESCE(g.breed, '') AS breed, lower(g.sex) AS sex,
           COALESCE(g.management_stage, '') AS stage,
           CASE
               WHEN g.lifecycle_status = 'sold' OR g.exit_reason = 'sold' THEN 'sold'
               WHEN g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu') THEN 'on_farm'
               ELSE 'other'
           END AS bucket,
           COALESCE(ds.park_id, g.park_id) AS park_id,
           COALESCE(ds.shed_id, g.shed_id) AS shed_id,
           COALESCE(ds.partition_label, gsp.partition_label, '') AS partition_label,
           -- A business DATE, never shifted: the deal's own sale date, else the exit stamped
           -- when the animal was marked sold, read as its IST calendar day.
           COALESCE(ds.sale_date, (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date) AS sale_date,
           ds.share, COALESCE(ds.buyer_name, '') AS buyer_name, ds.sales_deal_id
    FROM public.goats g
    LEFT JOIN ident i ON i.goat_id = g.goat_id
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1 AND gsp.goat_id = g.goat_id
    LEFT JOIN deal_share ds ON ds.goat_id = g.goat_id
    WHERE g.tenant_id = $1
      AND g.merged_into_goat_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM on_load ol WHERE ol.goat_id = g.goat_id)
),
located AS (
    SELECT p.*,
           COALESCE(NULLIF(park.name, ''), park.location_code, '') AS park_name,
           COALESCE(upper(park.location_code), '') AS park_code,
           COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_name,
           regexp_replace(lower(btrim(COALESCE(NULLIF(p.partition_label, ''), 'whole'))), '^part[[:space:]]+', '') AS partition_key
    FROM pop p
    LEFT JOIN public.locations park ON park.tenant_id = $1 AND park.location_id = p.park_id
    LEFT JOIN public.locations shed ON shed.tenant_id = $1 AND shed.location_id = p.shed_id
)`

// farmBornAnimalsSQL serves the page's facts: on-farm animals, and animals sold inside the window,
// both narrowed by the herd-dimension predicates. Blank predicate parameters mean no filter.
//
// projection-review: membership=goats at ROW grain (one animal), narrowed to the tenant, NOT on an
// accepted load row (a semi-join, so a two-load animal cannot fan out), the optional park / pen /
// species / breed / sex / stage predicates, and EITHER on farm today OR sold with a sale date
// inside [$2, $3]; group_key=goat_id -- every CTE it joins is 1:{0,1} per animal
// (ident DISTINCT ON goat_id, goat_shed_partitions PK (tenant, goat), deal_share through the
// tagged partial unique index, locations on PK), so the read returns exactly one row per admitted
// animal and the domain's counts are counts of animals; join_cardinality=1:{0,1} on every branch,
// the per-deal tagged count pre-aggregated before it joins; pagination=none, whole-filter read
// (the ledger page is sliced in the domain after the summary is computed); scope=tenant_id on
// every branch.
//
// scale-guard:ignore: whole-filter reporting read over the herd register at the 5k-50k envelope,
// aggregated once per request in one set-based statement over the tenant indexes (goats PK and
// tenant scan, procurement_load_goats_goat_state_idx, goat_identifiers goat index, the
// goat_sale_allocations live-uniqueness index). The same shape and reasoning as the load-wise
// member scan, which reads the other half of the same herd.
const farmBornAnimalsSQL = farmBornPopulationSQL + `
SELECT l.goat_id::text, l.display_id, l.tag, l.species, l.breed, l.sex, l.stage,
       COALESCE(l.park_id::text, ''), l.park_name,
       COALESCE(l.shed_id::text, ''), l.shed_name, l.partition_label,
       l.bucket, COALESCE(l.sale_date::text, ''), l.share, l.buyer_name,
       COALESCE(l.sales_deal_id::text, '')
FROM located l
WHERE (l.bucket = 'on_farm' OR (l.bucket = 'sold' AND l.sale_date BETWEEN $2::date AND $3::date))
  AND ($4::text = '' OR l.park_id::text = $4)
  AND ($5::text = '' OR (l.shed_id::text = $5 AND l.partition_key = $6))
  AND ($7::text = '' OR l.species = $7)
  AND ($8::text = '' OR lower(btrim(l.breed)) = $8)
  AND ($9::text = '' OR l.sex = $9)
  AND ($10::text = '' OR lower(btrim(l.stage)) = $10)
ORDER BY l.sale_date DESC NULLS LAST, l.tag, l.goat_id`

// farmBornOptionsSQL is the filter bar's vocabulary: every distinct park,
// pen, species, breed, sex and stage the population has, on farm or sold, so the bar offers only
// choices that match something. One row per distinct combination; the Go side splits them into
// the six lists.
//
// projection-review: membership=the same located population as the facts read (tenant, not on a
// load), restricted to animals on farm or sold; group_key=the distinct
// (park, shed, partition_key, species, breed, sex, stage) tuple -- a vocabulary, not a count, so no
// figure is derived from it; join_cardinality=1:{0,1} per animal on every branch as above;
// pagination=none; scope=tenant_id on every branch.
//
// scale-guard:ignore: bounded DISTINCT over the same set-based population read; the result is the
// vocabulary (tens of rows), not the herd.
const farmBornOptionsSQL = farmBornPopulationSQL + `
SELECT DISTINCT
       COALESCE(l.park_id::text, ''), l.park_name, l.park_code,
       COALESCE(l.shed_id::text, ''), l.shed_name, l.partition_label, l.partition_key,
       l.species, l.breed, l.sex, l.stage
FROM located l
WHERE l.bucket IN ('on_farm', 'sold')`

// FarmBornAnimals implements ports.FarmBornSalesRepository.
func (r *Repository) FarmBornAnimals(ctx context.Context, tenantID string, f domain.FarmBornFilter) ([]domain.FarmBornAnimalFact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	shedID := strings.TrimSpace(f.ShedID)
	partitionKey := ""
	if shedID != "" {
		partitionKey = oploc.NormalizePartition(f.Partition)
	}
	rows, err := r.pool.Query(ctx, farmBornAnimalsSQL,
		tenantID, f.From, f.To,
		strings.TrimSpace(f.ParkID), shedID, partitionKey,
		strings.ToLower(strings.TrimSpace(f.Species)),
		strings.ToLower(strings.TrimSpace(f.Breed)),
		strings.ToLower(strings.TrimSpace(f.Sex)),
		strings.ToLower(strings.TrimSpace(f.Stage)),
	)
	if err != nil {
		return nil, fmt.Errorf("procurement: farm born animals: %w", err)
	}
	defer rows.Close()

	facts := make([]domain.FarmBornAnimalFact, 0, 256)
	for rows.Next() {
		var (
			fact  domain.FarmBornAnimalFact
			share *float64
		)
		if err := rows.Scan(
			&fact.GoatID, &fact.DisplayID, &fact.Tag, &fact.Species, &fact.Breed, &fact.Sex, &fact.Stage,
			&fact.ParkID, &fact.ParkName, &fact.ShedID, &fact.ShedName, &fact.PartitionLabel,
			&fact.Bucket, &fact.SaleDate, &share, &fact.BuyerName, &fact.DealID,
		); err != nil {
			return nil, fmt.Errorf("procurement: farm born animals scan: %w", err)
		}
		if fact.Bucket == domain.FarmBornSold {
			fact.SaleValue = share
		} else {
			// An on-farm animal carries no sale, whatever a stale allocation says.
			fact.SaleDate, fact.BuyerName, fact.DealID = "", "", ""
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("procurement: farm born animals rows: %w", err)
	}
	return facts, nil
}

// FarmBornOptions implements ports.FarmBornSalesRepository.
func (r *Repository) FarmBornOptions(ctx context.Context, tenantID string) (domain.FarmBornOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, farmBornOptionsSQL, tenantID)
	if err != nil {
		return domain.FarmBornOptions{}, fmt.Errorf("procurement: farm born options: %w", err)
	}
	defer rows.Close()

	parks := map[string]domain.FarmBornOption{}
	pens := map[string]domain.FarmBornOption{}
	// Parks order by CODE, CBE before CPT (maintainer decision 2026-09-16): "Channapatna" sorts
	// ahead of "Coimbatore" by name, which is the wrong way round on every All-parks surface.
	// Pens cluster by that same park order and sort by label inside a park.
	parkCode := map[string]string{}
	species := map[string]domain.FarmBornOption{}
	breeds := map[string]domain.FarmBornOption{}
	sexes := map[string]domain.FarmBornOption{}
	stages := map[string]domain.FarmBornOption{}
	for rows.Next() {
		var parkID, parkName, parkCodeValue, shedID, shedName, partitionLabel, partitionKey, sp, breed, sex, stage string
		if err := rows.Scan(&parkID, &parkName, &parkCodeValue, &shedID, &shedName, &partitionLabel, &partitionKey, &sp, &breed, &sex, &stage); err != nil {
			return domain.FarmBornOptions{}, fmt.Errorf("procurement: farm born options scan: %w", err)
		}
		if parkID != "" {
			parks[parkID] = domain.FarmBornOption{Key: parkID, Label: firstNonBlank(parkName, domain.FarmBornUnknownPark)}
			parkCode[parkID] = parkCodeValue
		}
		if shedID != "" {
			key := domain.FarmBornPenKey(shedID, partitionLabel)
			display := oploc.OperationalLocation{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}.Display()
			// Two spellings of one partition ("Part 3" and "3") share a normalized key and must be
			// ONE option; the first spelling seen names it.
			penID := shedID + "#" + partitionKey
			if _, seen := pens[penID]; !seen {
				pens[penID] = domain.FarmBornOption{Key: key, Label: firstNonBlank(display, domain.FarmBornUnknownPen), ParkID: parkID}
			}
		}
		if sp != "" {
			species[sp] = domain.FarmBornOption{Key: sp, Label: domain.FarmBornSpeciesLabel(sp)}
		}
		if b := strings.TrimSpace(breed); b != "" {
			// Spellings that differ only by case are ONE breed; the capitalised spelling names it
			// (byte order puts "Malai" before "malai"), so the label is stable across reads.
			key := strings.ToLower(b)
			if cur, seen := breeds[key]; !seen || b < cur.Label {
				breeds[key] = domain.FarmBornOption{Key: key, Label: b}
			}
		}
		if sex != "" {
			sexes[sex] = domain.FarmBornOption{Key: sex, Label: domain.FarmBornSexLabel(sex)}
		}
		if s := strings.TrimSpace(stage); s != "" {
			key := strings.ToLower(s)
			if cur, seen := stages[key]; !seen || s < cur.Label {
				stages[key] = domain.FarmBornOption{Key: key, Label: s}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return domain.FarmBornOptions{}, fmt.Errorf("procurement: farm born options rows: %w", err)
	}
	byPark := func(o domain.FarmBornOption) string {
		code := parkCode[o.ParkID]
		if o.ParkID == "" {
			code = parkCode[o.Key]
		}
		return code
	}
	return domain.FarmBornOptions{
		Parks:   sortedOptionsBy(parks, byPark),
		Pens:    sortedOptionsBy(pens, byPark),
		Species: sortedOptions(species),
		Breeds:  sortedOptions(breeds),
		Sexes:   sortedOptions(sexes),
		Stages:  sortedOptions(stages),
	}, nil
}

func sortedOptions(m map[string]domain.FarmBornOption) []domain.FarmBornOption {
	return sortedOptionsBy(m, func(domain.FarmBornOption) string { return "" })
}

// sortedOptionsBy orders options by a cluster key first (the park code), then label, then key.
func sortedOptionsBy(m map[string]domain.FarmBornOption, cluster func(domain.FarmBornOption) string) []domain.FarmBornOption {
	out := make([]domain.FarmBornOption, 0, len(m))
	for _, o := range m {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if ci, cj := cluster(out[i]), cluster(out[j]); ci != cj {
			return ci < cj
		}
		if li, lj := strings.ToLower(out[i].Label), strings.ToLower(out[j].Label); li != lj {
			return naturalLess(li, lj)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// naturalLess orders labels the way the farm reads them: "Yashoda 2" before "Yashoda 10", digit
// runs compared by value and everything else byte-wise.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ra, rb := a[0], b[0]
		if isDigit(ra) && isDigit(rb) {
			ia, ib := 0, 0
			for ia < len(a) && isDigit(a[ia]) {
				ia++
			}
			for ib < len(b) && isDigit(b[ib]) {
				ib++
			}
			na, nb := strings.TrimLeft(a[:ia], "0"), strings.TrimLeft(b[:ib], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[ia:], b[ib:]
			continue
		}
		if ra != rb {
			return ra < rb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
