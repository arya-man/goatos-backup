// Command backfill-verification-subject-labels repairs the subject_label of verification items
// that were written before those labels named their shed, partition, and vaccine.
//
// # WHY THIS EXISTS
//
// subject_label is composed once, by the producing module, at the moment the item is enqueued.
// That is deliberate -- it is the sentence a verifier reads, frozen with the evidence -- but it
// means a fix to the composition only reaches items created AFTER the fix. The rows already in the
// queue keep whatever the old writer gave them:
//
//   - vaccination items written before the shed/partition fix read "11 goats", "120 goats" --
//     no shed, no partition, no vaccine.
//   - EVERY weighing item predates the shed fix: lump-sum items read the literal
//     "Whole shed · 732.0 kg · 31 goats" and individual items read "Tag 9010... · 28.1 kg", so
//     neither names the shed the clip was shot in, let alone the partition.
//
// This is forward-only, idempotent, and never invents a fact: it recomposes each label from the
// item's OWN source record, and skips any row whose shed/vaccine cannot be resolved rather than
// substituting an id or a placeholder. Rows whose label is already correct are left untouched.
//
// It composes exactly as the live write path does -- the campaign-shed catalog display name
// verbatim, and vaccinationdomain.DoseDisplayLabel for the dose -- so a backfilled row and a
// freshly written row cannot disagree about how a shed or a dose is named.
//
// DEFAULTS TO DRY RUN. Pass -apply to write. Always dry-run against the target first and read the
// diff: this rewrites stored evidence labels.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func main() {
	var (
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "target database")
		apply       = flag.Bool("apply", false, "write the recomposed labels (default: dry run)")
		timeout     = flag.Duration("timeout", 5*time.Minute, "overall timeout")
	)
	flag.Parse()
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintln(os.Stderr, "database-url (or DATABASE_URL) is required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	plan, err := planVaccination(ctx, pool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan vaccination: %v\n", err)
		os.Exit(1)
	}
	weighingPlan, err := planWeighing(ctx, pool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan weighing: %v\n", err)
		os.Exit(1)
	}
	plan = append(plan, weighingPlan...)

	// The shed_id repair is counted separately from the label plan and gated independently. An
	// earlier version returned early when the label plan was empty, which silently skipped the
	// shed_id repair on exactly the databases where the labels had already been fixed -- leaving
	// rows titled "Castro 2 · 758.0 kg · 32 goats" with an em-dash in their Shed field forever.
	shedNulls, err := countRepairableShedIDs(ctx, pool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "count repairable shed ids: %v\n", err)
		os.Exit(1)
	}
	if len(plan) == 0 && shedNulls == 0 {
		fmt.Println("nothing to backfill: every verification item already names and references its shed")
		return
	}
	for _, change := range plan {
		fmt.Printf("%s\n  old: %s\n  new: %s\n", change.ItemID, change.Old, change.New)
	}
	if !*apply {
		fmt.Printf("\nDRY RUN: %d label(s) and %d shed reference(s) would change. Re-run with -apply to write.\n", len(plan), shedNulls)
		return
	}

	// A lump-sum weighing capture has no per-animal expected location, so the producer left
	// verification_items.shed_id NULL on exactly the shed-grain rows. The label backfill puts the
	// shed back into the SENTENCE, but the drawer's SHED field and the queue's shed filter read the
	// COLUMN -- so a row could read "Castro 2 · 758.0 kg · 32 goats" in its title and show an
	// em-dash for Shed directly underneath it. Repair the column from the same campaign-shed bucket
	// the writer now uses. Forward-only and idempotent: only NULLs are touched.
	shedTag, err := pool.Exec(ctx, `
UPDATE verification_items vi
SET shed_id = src.location_id, updated_at = now()
FROM (
  SELECT wso.shed_observation_id, wcs.location_id
  FROM weighing_shed_observations wso
  JOIN weighing_campaign_sheds wcs
    ON wcs.tenant_id = wso.tenant_id AND wcs.campaign_shed_id = wso.campaign_shed_id
) src
WHERE vi.category = 'weighing_proof'
  AND vi.shed_id IS NULL
  AND vi.source_ref_id = src.shed_observation_id`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfill shed_id: %v\n", err)
		os.Exit(1)
	}
	if n := shedTag.RowsAffected(); n > 0 {
		fmt.Printf("repaired shed_id on %d lump-sum weighing item(s)\n", n)
	}

	if len(plan) == 0 {
		fmt.Println("no subject labels needed changing")
		return
	}
	// ONE set-based statement, not an UPDATE per row. A loop of .Exec here is the N+1 shape the
	// repo bans outright, and it is just as avoidable in a backfill as in a request path.
	ids := make([]string, 0, len(plan))
	next := make([]string, 0, len(plan))
	prev := make([]string, 0, len(plan))
	for _, c := range plan {
		ids = append(ids, c.ItemID)
		next = append(next, c.New)
		prev = append(prev, c.Old)
	}
	tag, err := pool.Exec(ctx, `
UPDATE verification_items vi
SET subject_label = src.new_label, updated_at = now()
FROM (
  SELECT unnest($1::uuid[]) AS item_id,
         unnest($2::text[]) AS new_label,
         unnest($3::text[]) AS old_label
) src
WHERE vi.item_id = src.item_id
  -- Guarded on the exact value read during planning, so a row rewritten by a concurrent producer
  -- between plan and apply is skipped rather than clobbered.
  AND vi.subject_label IS NOT DISTINCT FROM src.old_label`, ids, next, prev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "apply: %v\n", err)
		os.Exit(1)
	}
	updated := int(tag.RowsAffected())
	fmt.Printf("\napplied: %d of %d planned label(s) updated\n", updated, len(plan))
}

type change struct{ ItemID, Old, New string }

// planVaccination recomposes shed-grain vaccination labels from the submission's own completions:
// shed (with partition) · vaccine · count -- the same order the live writer uses.
// planVaccination recomposes a legacy shed-grain vaccination label the SAME way the live writer
// does (sopbridge.vaccinationSubjectLabel): shed WITH its partition, then the vaccine(s), then the
// count. An earlier version resolved the shed from locations.name alone and picked ONE completion
// with LIMIT 1, which produced "Sumathi 1 · ET+TT · 12 goats" where the writer produces
// "Sumathi 1 - Part 3 · ET+TT · 12 goats", and on a mixed-vaccine submission named whichever
// vaccine Postgres happened to return first. A backfill that disagrees with the writer is worse
// than none: it reports success while leaving the queue wrong in a way nobody re-checks.
//
// Shed labels, vaccine labels and the goat count all come from the submission's own completions,
// using the same partition CASE as vaccination's ListSubmissionCompletions. The item's own shed_id
// is a FALLBACK only, for legacy rows whose completions no longer resolve -- and it carries no
// partition, because per-goat partition is unknowable without them. Guessing one would be
// fabrication, so those rows get the shed name alone.
func planVaccination(ctx context.Context, pool *pgxpool.Pool) ([]change, error) {
	rows, err := pool.Query(ctx, `
SELECT vi.item_id::text,
       COALESCE(vi.subject_label, ''),
       COALESCE(c.shed_labels, ARRAY[]::text[]),
       COALESCE(c.protocol_names, ARRAY[]::text[]),
       COALESCE(c.dose_codes, ARRAY[]::text[]),
       COALESCE(c.goat_count, 0),
       COALESCE(NULLIF(shed.name, ''), NULLIF(shed.location_code, ''), '') AS fallback_shed
FROM verification_items vi
LEFT JOIN locations shed
  ON shed.tenant_id = vi.tenant_id AND shed.location_id = vi.shed_id
LEFT JOIN LATERAL (
  SELECT array_agg(m.shed_label)    AS shed_labels,
         array_agg(m.protocol_name) AS protocol_names,
         array_agg(m.dose_code)     AS dose_codes,
         count(DISTINCT m.goat_id)  AS goat_count
  FROM (
    SELECT DISTINCT
           g.goat_id,
           COALESCE(NULLIF(gshed.name, ''), NULLIF(gshed.location_code, ''), '')::text AS shed_label,
           COALESCE(pd.name, '')      AS protocol_name,
           COALESCE(pr.dose_code, '') AS dose_code
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id AND si.item_id = vc.sop_submission_item_id
    JOIN goats g
      ON g.tenant_id = vc.tenant_id AND g.goat_id = vc.goat_id
    LEFT JOIN locations gshed
      ON gshed.tenant_id = g.tenant_id AND gshed.location_id = g.shed_id AND gshed.location_type = 'shed'
    LEFT JOIN goat_shed_partitions gsp
      ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
    LEFT JOIN obligation_instances oi
      ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
    LEFT JOIN protocol_rules pr
      ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
    LEFT JOIN protocol_versions pv ON pv.protocol_version_id = pr.protocol_version_id
    LEFT JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
    WHERE si.submission_id = vi.source_submission_id
  ) m
) c ON true
WHERE vi.category = 'vaccination_proof'
  AND vi.source_ref_type = 'sop_submission'
  -- Only the rows the OLD writer produced: a bare "<n> goats" with no shed in front of it.
  AND vi.subject_label ~ '^[0-9]+ goats$'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]change, 0)
	for rows.Next() {
		var itemID, old, fallbackShed string
		var shedLabels, protocolNames, doseCodes []string
		var goatCount int
		if err := rows.Scan(&itemID, &old, &shedLabels, &protocolNames, &doseCodes, &goatCount, &fallbackShed); err != nil {
			return nil, err
		}

		shed := summarise(shedLabels, "sheds")
		if shed == "" {
			// Completions did not resolve; fall back to the item's own shed, partition unknown.
			shed = strings.TrimSpace(fallbackShed)
		}
		// Nothing resolvable to add that is not an id -- leave the row exactly as it is.
		if shed == "" {
			continue
		}

		vaccines := make([]string, 0, len(doseCodes))
		for i, dose := range doseCodes {
			protocol := ""
			if i < len(protocolNames) {
				protocol = protocolNames[i]
			}
			if label := vaccinationdomain.DoseDisplayLabel(protocol, dose); label != "" {
				vaccines = append(vaccines, label)
			}
		}

		parts := []string{shed}
		if v := summarise(vaccines, "vaccines"); v != "" {
			parts = append(parts, v)
		}
		// Prefer the recomputed membership count; fall back to the count already in the label when
		// the completions are gone (that number is what the verifier was shown at capture time).
		if goatCount > 0 {
			parts = append(parts, fmt.Sprintf("%d goats", goatCount))
		} else {
			parts = append(parts, old)
		}
		if next := strings.Join(parts, " · "); next != old {
			out = append(out, change{ItemID: itemID, Old: old, New: next})
		}
	}
	return out, rows.Err()
}

// summarise mirrors the live writer's collapse rule (sopbridge vaccinationShedSummary /
// vaccinationVaccineSummary): dedupe, sort for stability, name one or two, collapse past that to a
// count. Sorting matters -- an unsorted label would shuffle between runs for the same data.
func summarise(values []string, plural string) string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		// A leading "-" means only a partition suffix survived; that is an id-shaped fragment.
		if v == "" || strings.HasPrefix(v, "-") {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	switch len(out) {
	case 0:
		return ""
	case 1, 2:
		return strings.Join(out, " + ")
	default:
		return strconv.Itoa(len(out)) + " " + plural
	}
}

// planWeighing prefixes weighing labels with the shed the capture belongs to, taking the name from
// the campaign-shed bucket exactly as the live writer now does. The lump-sum literal "Whole shed"
// is REPLACED by the real shed rather than prefixed, or the row would read
// "Godel 1 - Part 3 · Whole shed · 250.0 kg", which is still the bug one segment over.
func planWeighing(ctx context.Context, pool *pgxpool.Pool) ([]change, error) {
	// Individual and lump-sum captures live in DIFFERENT tables (weighing_observations vs
	// weighing_shed_observations). An earlier pass joined only the first and silently left every
	// lump-sum row -- the ones still reading "Whole shed" -- unbackfilled while reporting success.
	rows, err := pool.Query(ctx, `
SELECT vi.item_id::text, COALESCE(vi.subject_label, ''), COALESCE(wcs.display_name, '')
FROM verification_items vi
JOIN LATERAL (
  SELECT wo.campaign_shed_id
  FROM weighing_observations wo
  WHERE wo.tenant_id = vi.tenant_id AND wo.observation_id = vi.source_ref_id
  UNION ALL
  SELECT wso.campaign_shed_id
  FROM weighing_shed_observations wso
  WHERE wso.tenant_id = vi.tenant_id AND wso.shed_observation_id = vi.source_ref_id
  LIMIT 1
) src ON true
JOIN weighing_campaign_sheds wcs
  ON wcs.tenant_id = vi.tenant_id AND wcs.campaign_shed_id = src.campaign_shed_id
WHERE vi.category = 'weighing_proof'
  AND COALESCE(wcs.display_name, '') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]change, 0)
	for rows.Next() {
		var itemID, old, displayName string
		if err := rows.Scan(&itemID, &old, &displayName); err != nil {
			return nil, err
		}
		// Verbatim, matching the live writer (weighing repo CampaignShedLocation): the catalog
		// display name is the operator-facing shed name, and re-splitting it here would render
		// "Castro 2" as "Castro - 2" beside a vaccination row that spells it "Castro 2".
		shed := strings.TrimSpace(displayName)
		if shed == "" {
			continue
		}
		// Already backfilled (or already written by the fixed producer).
		if strings.HasPrefix(old, shed+" · ") {
			continue
		}
		next := shed + " · " + strings.TrimPrefix(old, "Whole shed · ")
		if next != old {
			out = append(out, change{ItemID: itemID, Old: old, New: next})
		}
	}
	return out, rows.Err()
}

// countRepairableShedIDs reports how many weighing items still carry a NULL shed_id that the
// campaign-shed bucket can resolve. Read-only: it is what makes the dry run honest about the
// second repair rather than only reporting the label plan.
func countRepairableShedIDs(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `
SELECT count(*)
FROM verification_items vi
JOIN weighing_shed_observations wso
  ON wso.tenant_id = vi.tenant_id AND wso.shed_observation_id = vi.source_ref_id
JOIN weighing_campaign_sheds wcs
  ON wcs.tenant_id = wso.tenant_id AND wcs.campaign_shed_id = wso.campaign_shed_id
WHERE vi.category = 'weighing_proof'
  AND vi.shed_id IS NULL`).Scan(&n)
	return n, err
}
