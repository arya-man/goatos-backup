// Package farmvaluation is the ONE SQL statement of how a live animal is priced from Sales
// Config's Farm valuation (docs/decisions/loadwise-stock-valuation.md). Two pages price animals off
// that config -- Farm value (the whole live herd) and Load wise (the animals a load still holds) --
// and each used to carry its own copy of the stage, bucket and rate SQL. Two copies of a pricing
// rule are two prices for one animal the day one of them is edited, so both now splice these
// fragments, and `make loadwise-stock-valuation-guard` refuses a third copy.
//
// The rule, in order:
//   - the animal's valuation STAGE is the authored stage whose register entries name its milk
//     cohort (which wins) or its management stage, compared through one normalizer
//     (upper, strip non-alphanumerics -- domain.NormalizeStageMatch);
//   - its SPECIES is goat or sheep (goats.species); any other value is not valued;
//   - its GENDER is male or female; a Mother is female, and an animal with no gender on file reads
//     female (the recorded 2026-09-23 decision);
//   - the bucket is `<stage>_<species>_<gender>` (domain.ValuationBucketKey) and its price per kg
//     is that bucket's authored figure.
//
// A tenant with no authored row reads the seeded defaults, which are GENERATED from
// salesdomain.DefaultValuationAssumptions at init so the SQL fallback and the Go defaults cannot
// disagree. $1 is the tenant id in every fragment.
package farmvaluation

import (
	"fmt"
	"strings"

	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
)

// PricingCTEs is spliced into a WITH clause (followed by a comma by the caller when more CTEs
// follow). It defines:
//
//	fv_stage_by_match(match_norm, stage)                   one row per normalized register entry
//	fv_rates(bucket, label, fixed_weight_kg, price_per_kg, display_order)   one row per bucket
var PricingCTEs = buildPricingCTEs()

// StageJoinsSQL returns the two LEFT JOINs that resolve an animal's valuation stage, to append to
// the caller's FROM clause. cohortNorm and stageNorm are SQL expressions already normalized the
// stageNormSQL way. Both joins are 1:0..1 by construction (fv_stage_by_match is DISTINCT ON).
func StageJoinsSQL(cohortNorm, stageNorm string) string {
	return fmt.Sprintf(`LEFT JOIN fv_stage_by_match fv_sc ON %[1]s <> '' AND fv_sc.match_norm = %[1]s
    LEFT JOIN fv_stage_by_match fv_sm ON fv_sm.match_norm = %[2]s`, cohortNorm, stageNorm)
}

// BucketKeySQL is the SQL mirror of salesdomain.ValuationBucketKey over the fv_sc / fv_sm joins:
// 'unmapped' when the stage is not authored or the species is neither goat nor sheep. stageNorm is
// the normalized management stage (for the Mother rule), species the raw species column, sex the
// raw sex column.
func BucketKeySQL(stageNorm, species, sex string) string {
	sp := SpeciesSQL(species)
	return fmt.Sprintf(`CASE WHEN COALESCE(fv_sc.stage, fv_sm.stage) IS NULL OR %[2]s NOT IN (%[4]s) THEN 'unmapped'
        ELSE COALESCE(fv_sc.stage, fv_sm.stage) || '_' || %[2]s || '_' ||
             CASE WHEN %[1]s = 'MOTHER' THEN 'female' WHEN lower(btrim(COALESCE(%[3]s, ''))) = 'male' THEN 'male' ELSE 'female' END
        END`, stageNorm, sp, sex, speciesList())
}

// SpeciesSQL normalizes a species column the one way both pages compare it.
func SpeciesSQL(col string) string {
	return fmt.Sprintf("lower(btrim(COALESCE(%s, '')))", col)
}

// NormSQL normalizes a stage or cohort column exactly as salesdomain.NormalizeStageMatch does.
func NormSQL(col string) string {
	return fmt.Sprintf("upper(regexp_replace(btrim(COALESCE(%s, '')), '[^A-Za-z0-9]+', '', 'g'))", col)
}

func speciesList() string {
	parts := make([]string, 0, len(salesdomain.ValuationSpecies))
	for _, s := range salesdomain.ValuationSpecies {
		parts = append(parts, sqlString(s.Key))
	}
	return strings.Join(parts, ", ")
}

func speciesAlternation() string {
	parts := make([]string, 0, len(salesdomain.ValuationSpecies))
	for _, s := range salesdomain.ValuationSpecies {
		parts = append(parts, s.Key)
	}
	return strings.Join(parts, "|")
}

func speciesValues() string {
	parts := make([]string, 0, len(salesdomain.ValuationSpecies))
	for i, s := range salesdomain.ValuationSpecies {
		parts = append(parts, fmt.Sprintf("(%s, %d)", sqlString(s.Key), i+1))
	}
	return strings.Join(parts, ", ")
}

func sqlString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func buildPricingCTEs() string {
	def := salesdomain.DefaultValuationAssumptions()
	var stageRows []string
	for _, st := range def.Stages {
		for _, m := range st.Matches {
			stageRows = append(stageRows, fmt.Sprintf("(%s, %d, %s)", sqlString(st.Stage), st.DisplayOrder, sqlString(salesdomain.NormalizeStageMatch(m))))
		}
	}
	var rateRows []string
	for _, b := range def.Buckets {
		w := "NULL::float8"
		if b.FixedWeightKg != nil {
			w = fmt.Sprintf("%g::float8", *b.FixedWeightKg)
		}
		rateRows = append(rateRows, fmt.Sprintf("(%s, %s, %s, %g::float8, %d)", sqlString(b.Bucket), sqlString(b.Label), w, b.PricePerKg, b.DisplayOrder))
	}
	return `fv_stage_rules AS (
    SELECT st.stage, st.display_order,
           upper(regexp_replace(btrim(mt.match), '[^A-Za-z0-9]+', '', 'g')) AS match_norm
    FROM public.sales_valuation_assumptions va
    CROSS JOIN LATERAL jsonb_to_recordset(va.stages) AS st(stage text, display_order int, matches jsonb)
    CROSS JOIN LATERAL jsonb_array_elements_text(st.matches) AS mt(match)
    WHERE va.tenant_id = $1::uuid
    UNION ALL
    SELECT * FROM (VALUES
        ` + strings.Join(stageRows, ",\n        ") + `
    ) d(stage, display_order, match_norm)
    WHERE NOT EXISTS (SELECT 1 FROM public.sales_valuation_assumptions va WHERE va.tenant_id = $1::uuid)
),
-- One row per register entry, so the stage joins cannot fan an animal out. Two stages claiming one
-- entry is REFUSED at the write; this keeps the read total against a row written before that rule.
fv_stage_by_match AS (
    SELECT DISTINCT ON (match_norm) match_norm, stage
    FROM fv_stage_rules
    ORDER BY match_norm, display_order, stage
),
fv_rates AS (
    SELECT DISTINCT ON (bucket) bucket, label, fixed_weight_kg, price_per_kg, display_order
    FROM (
        SELECT b.bucket, b.label, b.fixed_weight_kg, b.price_per_kg, b.display_order
        FROM public.sales_valuation_assumptions va
        CROSS JOIN LATERAL jsonb_to_recordset(va.buckets) AS b(bucket text, label text, fixed_weight_kg float8, price_per_kg float8, display_order int)
        WHERE va.tenant_id = $1::uuid AND b.bucket ~ ` + "'_(" + speciesAlternation() + ")_(female|male)$'" + `
        UNION ALL
        -- A row stored BEFORE the species split (` + "`<stage>_<gender>`" + `, pre-000470) prices both
        -- species alike, exactly as it did before the split, so the read is right during the deploy
        -- window and against a database the migration has not reached. A species-keyed row of the
        -- same bucket wins (it sorts first: display_order is ranked behind every authored row).
        -- Its card is named the way a save names it (domain.BucketLabel): the STAGE's own label,
        -- then species, then gender; and it sorts stage, species, gender like an authored row.
        SELECT regexp_replace(b.bucket, '_(female|male)$', '') || '_' || sp.key || '_' || substring(b.bucket from '_(female|male)$'),
               COALESCE(
                   (SELECT min(st->>'label') FROM jsonb_array_elements(va.stages) st
                    WHERE st->>'stage' = regexp_replace(b.bucket, '_(female|male)$', '')),
                   regexp_replace(b.label, ' · (Female|Male)$', '')
               ) || ' · ' || initcap(sp.key) || ' · ' || initcap(substring(b.bucket from '_(female|male)$')),
               b.fixed_weight_kg, b.price_per_kg,
               100000 + COALESCE(
                   (SELECT min(so.ord)::int FROM jsonb_array_elements(va.stages) WITH ORDINALITY AS so(st, ord)
                    WHERE so.st->>'stage' = regexp_replace(b.bucket, '_(female|male)$', '')), 99) * 10
                      + sp.ord * 2 + CASE WHEN b.bucket ~ '_male$' THEN 1 ELSE 0 END
        FROM public.sales_valuation_assumptions va
        CROSS JOIN LATERAL jsonb_to_recordset(va.buckets) AS b(bucket text, label text, fixed_weight_kg float8, price_per_kg float8, display_order int)
        CROSS JOIN (VALUES ` + speciesValues() + `) AS sp(key, ord)
        WHERE va.tenant_id = $1::uuid AND b.bucket ~ '_(female|male)$' AND b.bucket !~ ` + "'_(" + speciesAlternation() + ")_(female|male)$'" + `
        UNION ALL
        SELECT * FROM (VALUES
            ` + strings.Join(rateRows, ",\n            ") + `
        ) d(bucket, label, fixed_weight_kg, price_per_kg, display_order)
        WHERE NOT EXISTS (SELECT 1 FROM public.sales_valuation_assumptions va WHERE va.tenant_id = $1::uuid)
    ) x
    WHERE price_per_kg IS NOT NULL AND price_per_kg > 0
    ORDER BY bucket, display_order
)`
}
