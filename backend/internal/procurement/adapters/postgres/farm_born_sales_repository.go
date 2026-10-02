package postgres

import (
	"context"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/herdstage"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// FARM BORN SALES read (maintainer request 2026-09-18, docs/decisions/sales-farm-born.md).
//
// RECORDED CROSS-MODULE REPORTING READ, the load-wise shape. Procurement reads OUT to goats
// (origin, breed, sex, stage, park, terminal outcome), goat_identifiers (the tag),
// goat_shed_partitions + locations (the pen) and goat_sale_allocations + sales_deals (sale date,
// buyer, the deal share). Read-only over every one of those tables and reporting grain only:
// nothing here gates a sale, an exit or any write; the sales module's own lock (migration 000173)
// is untouched because the dependency points the other way.

var _ ports.FarmBornSalesRepository = (*Repository)(nil)

// farmBornPopulationSQL is the shared front of both reads: every animal the register marks
// BORN HERE (goats.origin_type = 'birth'), with its register facts and its pen resolved ONCE.
//
// The population is the register's own origin field and nothing else (maintainer decision
// 2026-09-22, SUPERSEDING the 2026-09-19 not-on-a-load rule). It is therefore NOT the complement
// of the load-wise membership any more: an animal whose origin is blank or 'procured' while it
// sits on no purchase load appears on NEITHER page. That gap is the register's to close, and it
// is visible rather than papered over -- the alternative counted bought animals as farm born.
//
// The pen is the animal's own goat_shed_partitions row over goats.shed_id. ExitGoat never touches
// shed_id and the partition row outlives the exit, so a sold animal still reads back the pen it
// left from. A sale allocation snapshots park/shed/partition at tagging, and that snapshot wins
// for a sold animal when present -- it is where the animal was SOLD from, which is the question
// the pen breakdown answers.
const farmBornPopulationSQL = `
WITH ident AS (
    -- One tag per animal: the primary tag first, else the secondary, newest first. The same
    -- animal_identifier_1 / _2 rule the register uses; other identifier types are not tags.
    SELECT DISTINCT ON (gi.goat_id) gi.goat_id, gi.identifier_value
    FROM public.goat_identifiers gi
    WHERE gi.tenant_id = $1 AND gi.status = 'active'
      AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    ORDER BY gi.goat_id, CASE gi.identifier_type WHEN 'animal_identifier_1' THEN 0 ELSE 1 END, gi.created_at DESC
),
` + saleLineShareCTEs + `,
deal_share AS (
    -- Each animal is priced from ITS OWN SALE LINE by the shared saleLineShareCTEs, the same CTE
    -- Load wise reads, and only a CLOSED deal is a sale (maintainer decisions 2026-09-25). Every
    -- live tagged allocation is kept, closed or not, because an animal tagged to a deal still OPEN
    -- is its own bucket ('tagged_open', taggedSaleOutcomeWhens -- the SAME rule Load wise reads):
    -- neither sold nor on farm. Its deal lends it the pen it was tagged from and nothing else --
    -- no sale date, buyer, deal or value until the deal closes.
    -- projection-review: membership=animal_share, one row per live tagged animal, closed or open; group_key=goat_id, priced per (deal, species, bucket) inside saleLineShareCTEs; join_cardinality=1:{0,1} per animal through the tagged partial unique index; pagination=none, whole-filter read; scope=tenant_id
    SELECT goat_id, sales_deal_id, park_id, shed_id, partition_label, sale_date, buyer_name, share, closed
    FROM animal_share
),
pop AS (
    SELECT g.goat_id, g.display_id, COALESCE(i.identifier_value, '') AS tag,
           lower(g.species) AS species, COALESCE(g.breed, '') AS breed, lower(g.sex) AS sex,
           COALESCE(g.management_stage, '') AS stage,
           CASE` + taggedSaleOutcomeWhens + `
               WHEN g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu') THEN 'on_farm'
               ELSE 'other'
           END AS bucket,
           COALESCE(ds.park_id, g.park_id) AS park_id,
           COALESCE(ds.shed_id, g.shed_id) AS shed_id,
           COALESCE(ds.partition_label, gsp.partition_label, '') AS partition_label,
           -- A business DATE, never shifted: the deal's own sale date, else the exit stamped
           -- when the animal was marked sold, read as its IST calendar day.
           -- An open deal contributes none of the sale facts: they are its once it closes.
           COALESCE(CASE WHEN ds.closed THEN ds.sale_date END, (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date) AS sale_date,
           CASE WHEN ds.closed THEN ds.share END AS share,
           COALESCE(CASE WHEN ds.closed THEN ds.buyer_name END, '') AS buyer_name,
           CASE WHEN ds.closed THEN ds.sales_deal_id END AS sales_deal_id
    FROM public.goats g
    LEFT JOIN ident i ON i.goat_id = g.goat_id
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1 AND gsp.goat_id = g.goat_id
    LEFT JOIN deal_share ds ON ds.goat_id = g.goat_id
    WHERE g.tenant_id = $1
      AND g.merged_into_goat_id IS NULL
      AND g.origin_type = 'birth'
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
// projection-review: membership=goats at ROW grain (one animal), narrowed to the tenant, to
// origin_type = 'birth' (a column predicate on the animal's own row, so nothing can fan out), the
// optional park / pen / species / breed / sex / stage predicates, and EITHER on farm today OR sold
// with a sale date inside [$2, $3]; group_key=goat_id -- every CTE it joins is 1:{0,1} per animal
// (ident DISTINCT ON goat_id, goat_shed_partitions PK (tenant, goat), deal_share through the
// tagged partial unique index, locations on PK), so the read returns exactly one row per admitted
// animal and the domain's counts are counts of animals; join_cardinality=1:{0,1} on every branch,
// the per-deal tagged count pre-aggregated before it joins; pagination=none, whole-filter read
// (the ledger page is sliced in the domain after the summary is computed); scope=tenant_id on
// every branch.
//
// scale-guard:ignore: whole-filter reporting read over the herd register at the 5k-50k envelope,
// aggregated once per request in one set-based statement over the tenant indexes (goats PK and
// tenant scan, goat_identifiers goat index, the goat_sale_allocations live-uniqueness index). The same shape and reasoning as the load-wise
// member scan, which reads the other half of the same herd.
const farmBornAnimalsSQL = farmBornPopulationSQL + `
SELECT l.goat_id::text, l.display_id, l.tag, l.species, l.breed, l.sex, l.stage,
       -- The park as the page's farm chips name it (the code, "CBE"), so a pen row and the chip
       -- above it never call one farm two names; the full name only when a park has no code.
       COALESCE(l.park_id::text, ''), COALESCE(NULLIF(l.park_code, ''), l.park_name),
       COALESCE(l.shed_id::text, ''), l.shed_name, l.partition_label,
       l.bucket, COALESCE(l.sale_date::text, ''), l.share, l.buyer_name,
       COALESCE(l.sales_deal_id::text, '')
FROM located l
-- On farm and tagged-not-closed are TODAY's states, whatever the window; only a sale is dated.
WHERE (l.bucket IN ('on_farm', 'tagged_open') OR (l.bucket = 'sold' AND l.sale_date BETWEEN $2::date AND $3::date))
  AND ($4::text = '' OR l.park_id::text = $4)
  AND ($5::text = '' OR (l.shed_id::text = $5 AND l.partition_key = $6))
  AND ($7::text = '' OR l.species = $7)
  AND ($8::text = '' OR lower(btrim(l.breed)) = $8)
  AND ($9::text = '' OR l.sex = $9)
  -- The stage filter offers ONE fattening option and matches the three stored values it stands
  -- for (herdstage.ExpandFilter), so this predicate is a SET where the others are a single value.
  AND (cardinality($10::text[]) = 0 OR lower(btrim(l.stage)) = ANY($10::text[]))
ORDER BY l.sale_date DESC NULLS LAST, l.tag, l.goat_id`

// farmBornOptionsSQL is the filter bar's vocabulary: every distinct park,
// pen, species, breed, sex and stage the population has, on farm or sold, so the bar offers only
// choices that match something. One row per distinct combination; the Go side splits them into
// the six lists.
//
// projection-review: membership=the same located population as the facts read (tenant,
// origin_type = 'birth'), restricted to animals on farm or sold; group_key=the distinct
// (park, shed, partition_key, species, breed, sex, FOLDED stage, stage name) tuple -- a vocabulary,
// not a count, so no figure is derived from it and a fold that maps three codes onto one can only
// remove a row, never invent one; join_cardinality=1:{0,1} per animal on every branch as above,
// plus animal_stage_lookup LEFT JOINed once on its unique (tenant_id, stage_code) key -- a strict
// 1:{0,1} LABEL-ONLY lookup reached through the folded code, supplying no identity, and the
// CROSS JOIN LATERAL is a scalar CASE over the row itself, exactly one row per input row;
// pagination=none; scope=tenant_id on every branch.
//
// scale-guard:ignore: bounded DISTINCT over the same set-based population read; the result is the
// vocabulary (tens of rows), not the herd.
const farmBornOptionsSQL = farmBornPopulationSQL + `
SELECT DISTINCT
       COALESCE(l.park_id::text, ''), l.park_name, l.park_code,
       COALESCE(l.shed_id::text, ''), l.shed_name, l.partition_label, l.partition_key,
       l.species, l.breed, l.sex, folded.code, COALESCE(sl.name, '')
FROM located l
-- The FATTENING FOLD, and the lookup name for the code it folds onto. This page carries a sex
-- filter, so the sexed fattening tags arrive as ONE option -- named by the COHORT's authored row
-- ("Fattening"), which is also what Counts Breakdown's stage filter calls it. Naming it from a
-- member instead would label the whole cohort "Fattening female" on one screen and something else
-- on the other. The membership is BOUND ($2/$3, from platform/herdstage) rather than spelled out
-- here, so this SQL is not a second copy of the vocabulary. Every other stage keeps its own code,
-- exactly as before: the join is reached only through the folded code, and a stage with no lookup
-- row falls back to it.
CROSS JOIN LATERAL (
  SELECT CASE WHEN lower(btrim(COALESCE(l.stage, ''))) = ANY($2::text[])
              THEN $3::text ELSE l.stage END AS code
) folded
LEFT JOIN animal_stage_lookup sl
       ON sl.tenant_id = $1::uuid AND sl.stage_code = folded.code
WHERE l.bucket IN ('on_farm', 'sold', 'tagged_open')`

// FarmBornAnimals implements ports.FarmBornSalesRepository.
func (r *Repository) FarmBornAnimals(ctx context.Context, tenantID string, f domain.FarmBornFilter) ([]domain.FarmBornAnimalFact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	shedID := strings.TrimSpace(f.ShedID)
	partitionKey := ""
	if shedID != "" {
		partitionKey = oploc.NormalizePartition(f.Partition)
	}
	boundAnimals := sqlbind.MustBind(farmBornAnimalsSQL,
		tenantID, f.From, f.To,
		strings.TrimSpace(f.ParkID), shedID, partitionKey,
		strings.ToLower(strings.TrimSpace(f.Species)),
		strings.ToLower(strings.TrimSpace(f.Breed)),
		strings.ToLower(strings.TrimSpace(f.Sex)),
		lowerAll(herdstage.ExpandFilter(compactStage(f.Stage))),
	)
	rows, err := r.pool.Query(ctx, boundAnimals.SQL(), boundAnimals.Args()...)
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
			// An on-farm animal carries no sale, whatever a stale allocation says; an animal tagged
			// to an open deal carries none YET.
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

	boundOptions := sqlbind.MustBind(farmBornOptionsSQL, tenantID, herdstage.LowerMembers(), herdstage.FatteningKey)
	rows, err := r.pool.Query(ctx, boundOptions.SQL(), boundOptions.Args()...)
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
		var parkID, parkName, parkCodeValue, shedID, shedName, partitionLabel, partitionKey, sp, breed, sex, stage, stageName string
		if err := rows.Scan(&parkID, &parkName, &parkCodeValue, &shedID, &shedName, &partitionLabel, &partitionKey, &sp, &breed, &sex, &stage, &stageName); err != nil {
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
		// `stage` is already the FOLDED code: the three fattening values arrive as one. Its label
		// is the cohort's authored name when the lookup has one, and its own code otherwise --
		// herdstage.DisplayLabel, the same rule Counts Breakdown's filter reads, so the two pages
		// cannot call one cohort two things.
		if s := strings.TrimSpace(stage); s != "" {
			key := strings.ToLower(s)
			label := herdstage.DisplayLabel(s, stageName)
			// Spellings that differ only by case are ONE stage; the first-sorting label names it.
			if cur, seen := stages[key]; !seen || label < cur.Label {
				stages[key] = domain.FarmBornOption{Key: key, Label: label}
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

// compactStage turns the page's single stage parameter into the set the predicate binds: empty
// means no filter, so a blank value must not become a one-element set matching nothing.
func compactStage(stage string) []string {
	if trimmed := strings.TrimSpace(stage); trimmed != "" {
		return []string{trimmed}
	}
	return nil
}

// lowerAll matches the predicate's own lower(btrim(...)) normalization; a nil set stays nil so
// cardinality() reads zero and the filter is off.
func lowerAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.ToLower(strings.TrimSpace(value)))
	}
	return out
}
