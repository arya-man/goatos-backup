// Command backfill-protocol-rule-lineage labels the rules that already exist, so the FIRST publish
// after this ships carries unchanged work forward instead of cancelling and re-minting it.
//
// Carry-over pairs a retired version's rules to the effective version's by identity and content
// fingerprint, both read from protocol_rule_lineage. The publisher writes that row, so only rules
// published AFTER the lineage table shipped have one. Without a backfill the first publish -- the
// one where a director adds a vaccine to a live plan -- cannot prove the other vaccines are
// unchanged, falls back to cancel-and-re-mint, and churns exactly the work this feature exists to
// protect. That is safe, but it is not what was asked for.
//
// The fingerprint is computed here by the SAME domain helpers the publisher uses, from the stored
// rule row. Deriving it in SQL would risk a fingerprint that differs from the publisher's by a
// byte, which is worse than no lineage at all: rules would look CHANGED forever and never carry
// over.
//
// Idempotent: re-running rewrites the same values. Safe to run before or after the deploy, though
// before the first publish is the point.
//
// Usage:
//
//	DATABASE_URL=... go run ./cmd/backfill-protocol-rule-lineage [-tenant-id <uuid>] [-apply]
//
// Without -apply it reports what it would write and changes nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

type ruleRow struct {
	tenantID            string
	protocolVersionID   string
	ruleID              string
	doseCode            string
	sequence            int32
	triggerType         string
	offsetDays          int32
	dueWindowDays       int32
	minGapDays          int32
	repeat              string
	repeatUntilAfterAge string
	catchUp             string
	eligibilityJSON     []byte
	proofPolicy         []byte
	sopVersionID        *string
	withdrawalDays      *int32
}

func main() {
	tenant := flag.String("tenant-id", "", "restrict to one tenant (default: every tenant)")
	apply := flag.Bool("apply", false, "write the rows; without it nothing is changed")
	resolve := flag.String("resolve-duplicates", "", `resolve pre-existing duplicate open obligations: "keep-earliest" cancels every open row after the earliest due date in each (animal, rule, sequence) group. Off by default, because it closes somebody's scheduled vaccination.`)
	flag.Parse()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")))
	if err != nil {
		fail(fmt.Errorf("connect: %w", err))
	}
	defer pool.Close()

	tenantID := strings.TrimSpace(*tenant)
	rows, err := loadRulesMissingLineage(ctx, pool, tenantID)
	if err != nil {
		fail(err)
	}
	// No early return when every rule is already labelled: the obligation stamping below is a
	// separate job, and a re-run after a partial pass must still finish it.

	// Collected and written in ONE statement rather than a round trip per rule: a tenant's plan
	// history runs to hundreds of rules, and a backfill that walks them one at a time is the
	// n+1 shape the scale guard exists to stop.
	var (
		tenants, versions, ruleIDs, identities, fingerprints []string
		skipped                                              int
	)
	for _, r := range rows {
		identity := protodomain.RuleIdentityKey(protodomain.VaccineCodeForRule(r.eligibilityJSON), r.doseCode, r.sequence)
		fingerprint := protodomain.RuleContentFingerprint(protodomain.NewRule{
			DoseCode:            r.doseCode,
			Sequence:            r.sequence,
			TriggerType:         r.triggerType,
			OffsetDays:          r.offsetDays,
			DueWindowDays:       r.dueWindowDays,
			MinGapDays:          r.minGapDays,
			Repeat:              r.repeat,
			RepeatUntilAfterAge: r.repeatUntilAfterAge,
			CatchUp:             r.catchUp,
			EligibilityJSON:     r.eligibilityJSON,
			ProofPolicy:         r.proofPolicy,
			SopVersionID:        r.sopVersionID,
			WithdrawalDays:      r.withdrawalDays,
		})
		// A rule whose eligibility JSON cannot be canonicalised gets no row, exactly as at publish
		// time. It keeps the cancel-and-re-mint behaviour rather than a fingerprint nothing else
		// would ever reproduce.
		if strings.TrimSpace(identity) == "" || fingerprint == "" {
			skipped++
			continue
		}
		tenants = append(tenants, r.tenantID)
		versions = append(versions, r.protocolVersionID)
		ruleIDs = append(ruleIDs, r.ruleID)
		identities = append(identities, identity)
		fingerprints = append(fingerprints, fingerprint)
	}

	written := len(ruleIDs)
	if *apply && written > 0 {
		if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_lineage (tenant_id, protocol_version_id, rule_id, identity_key, content_fingerprint)
SELECT t::uuid, v::uuid, r::uuid, i, f
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) AS s(t, v, r, i, f)
ON CONFLICT (tenant_id, rule_id) DO UPDATE
SET protocol_version_id = EXCLUDED.protocol_version_id,
    identity_key = EXCLUDED.identity_key,
    content_fingerprint = EXCLUDED.content_fingerprint`,
			tenants, versions, ruleIDs, identities, fingerprints); err != nil {
			fail(fmt.Errorf("write lineage rows: %w", err))
		}
	}

	// Stamp the identity onto the animals' EXISTING open work as well.
	//
	// Reconciliation finds an animal's current obligation by rule identity, so an obligation
	// written before this column existed is invisible to it -- and generation would insert beside
	// it, booking the same dose twice. Derived from the lineage row of whichever rule the
	// obligation already points at, so it says exactly what the rule says.
	// Duplicate resolution runs FIRST when asked for, so the rows it closes stop being ambiguous
	// and the stamping below can label the survivors in the same run.
	if strings.TrimSpace(*resolve) != "" {
		closed, err := resolveDuplicateOpenWork(ctx, pool, tenantID, *resolve, *apply)
		if err != nil {
			fail(err)
		}
		verb := "would close"
		if *apply {
			verb = "closed"
		}
		fmt.Printf("backfill-protocol-rule-lineage: %s %d duplicate open obligation(s), keeping the earliest due date in each group\n", verb, closed)
	}

	// Only rows whose identity is UNAMBIGUOUS are stamped. An animal that already holds two open
	// obligations for one dose cannot be labelled, because the label would assert the invariant
	// -- one open obligation per identity -- that the data itself contradicts, and the unique
	// index would reject it.
	//
	// Those groups are reported rather than resolved. Picking a survivor means cancelling
	// somebody's scheduled vaccination on a guess about which of two dates is right, and a
	// backfill is the wrong place to make that call silently. They are pre-existing duplicates:
	// this change neither created them nor depends on fixing them, and the rows it leaves alone
	// keep behaving exactly as they do today.
	var stamped int64
	conflicts, err := countAmbiguousIdentities(ctx, pool, tenantID)
	if err != nil {
		fail(err)
	}
	if *apply {
		tag, err := stampObligationIdentities(ctx, pool, tenantID)
		if err != nil {
			fail(fmt.Errorf("stamp obligation identities: %w", err))
		}
		stamped = tag.RowsAffected()
	}

	mode := "would write"
	if *apply {
		mode = "wrote"
	}
	fmt.Printf("backfill-protocol-rule-lineage: %s %d lineage row(s), stamped %d open obligation(s) with their rule identity, skipped %d rule(s) whose content could not be fingerprinted\n", mode, written, stamped, skipped)
	if conflicts > 0 {
		fmt.Printf("backfill-protocol-rule-lineage: %d (animal, rule, sequence) group(s) already hold more than one OPEN obligation and were left unlabelled.\n", conflicts)
		fmt.Println("  These are pre-existing duplicates, not something this change introduced. Until one of each pair is closed")
		fmt.Println("  they keep the behaviour they have today; reconciliation simply cannot claim them. List them with:")
		fmt.Println(`    SELECT oi.target_id, l.identity_key, oi."sequence", count(*), array_agg(oi.obligation_id), array_agg(oi.due_at)`)
		fmt.Println(`    FROM obligation_instances oi JOIN protocol_rule_lineage l ON l.tenant_id = oi.tenant_id AND l.rule_id = oi.rule_id`)
		fmt.Println(`    WHERE oi.status IN ('scheduled','due','in_progress','deferred')`)
		fmt.Println(`    GROUP BY 1,2,3 HAVING count(*) > 1;`)
	}
	if !*apply {
		fmt.Println("re-run with -apply to write them")
	}
}

// loadRulesMissingLineage reads every rule with no lineage row. Retired versions are included on
// purpose: carry-over pairs FROM the retired side, so a retired rule without lineage is exactly
// the case that cannot be recognised at the next publish.
func loadRulesMissingLineage(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]ruleRow, error) {
	rows, err := pool.Query(ctx, `
SELECT pr.tenant_id::text, pr.protocol_version_id::text, pr.rule_id::text, pr.dose_code, pr."sequence",
       pr.trigger_type, pr.offset_days, pr.due_window_days, pr.min_gap_days, pr."repeat",
       COALESCE(pr.repeat_until_after_age, '')::text, pr.catch_up, pr.eligibility_json, pr.proof_policy,
       pr.sop_version_id::text, pr.withdrawal_days
FROM protocol_rules pr
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id AND pv.protocol_version_id = pr.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
LEFT JOIN protocol_rule_lineage l
  ON l.tenant_id = pr.tenant_id AND l.rule_id = pr.rule_id
WHERE l.rule_id IS NULL
  AND pd.category = 'vaccination'
  AND ($1::uuid IS NULL OR pr.tenant_id = $1::uuid)
ORDER BY pr.tenant_id, pr.protocol_version_id, pr.rule_id`, nullableUUID(tenantID))
	if err != nil {
		return nil, fmt.Errorf("read rules missing lineage: %w", err)
	}
	defer rows.Close()

	var out []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(
			&r.tenantID, &r.protocolVersionID, &r.ruleID, &r.doseCode, &r.sequence,
			&r.triggerType, &r.offsetDays, &r.dueWindowDays, &r.minGapDays, &r.repeat,
			&r.repeatUntilAfterAge, &r.catchUp, &r.eligibilityJSON, &r.proofPolicy,
			&r.sopVersionID, &r.withdrawalDays,
		); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read rules missing lineage: %w", err)
	}
	return out, nil
}

// resolveDuplicateOpenWork closes the later rows in each duplicated (animal, rule, sequence)
// group, keeping the earliest due date.
//
// This is off by default and named explicitly, because it CANCELS somebody's scheduled
// vaccination. Keeping the earliest is the defensible default -- it is the date the animal was
// already told it owed, and a later duplicate is the artefact -- but it is still a decision about
// medical work, so it happens only when a human asks for it by name.
//
// Rows are canceled, never deleted: the audit trail keeps what was there, and a wrong call is
// recoverable by reading it back.
func resolveDuplicateOpenWork(ctx context.Context, pool *pgxpool.Pool, tenantID, strategy string, apply bool) (int64, error) {
	if strategy != "keep-earliest" {
		return 0, fmt.Errorf("unknown -resolve-duplicates strategy %q (only \"keep-earliest\" is supported)", strategy)
	}
	const selectLosers = `
WITH ranked AS (
  SELECT oi.obligation_id,
         row_number() OVER (
           PARTITION BY oi.tenant_id, oi.target_type, oi.target_id, l.identity_key, oi."sequence"
           ORDER BY oi.due_at, oi.obligation_id
         ) AS rank
  FROM obligation_instances oi
  JOIN protocol_rule_lineage l ON l.tenant_id = oi.tenant_id AND l.rule_id = oi.rule_id
  WHERE oi.status IN ('scheduled', 'due', 'deferred')
    AND oi.rule_identity_key IS NULL
    AND ($1::uuid IS NULL OR oi.tenant_id = $1::uuid)
)
SELECT obligation_id FROM ranked WHERE rank > 1`

	if !apply {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM (`+selectLosers+`) x`, nullableUUID(tenantID)).Scan(&n); err != nil {
			return 0, fmt.Errorf("count duplicate open work: %w", err)
		}
		return n, nil
	}

	// in_progress is absent from the statuses above on purpose: an operator is administering that
	// dose right now, and closing it from a maintenance command would erase work in flight.
	tag, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'canceled', row_version = row_version + 1, updated_at = now()
WHERE obligation_id IN (`+selectLosers+`)`, nullableUUID(tenantID))
	if err != nil {
		return 0, fmt.Errorf("close duplicate open work: %w", err)
	}
	return tag.RowsAffected(), nil
}

// stampObligationIdentities labels the animals' existing open work with the identity of the rule
// it already points at.
//
// Only UNAMBIGUOUS rows are touched: an animal holding two open obligations for one dose cannot be
// labelled, because the label asserts the one-open-obligation-per-identity invariant that the data
// itself contradicts. Those are reported instead -- choosing which of two scheduled vaccinations to
// cancel is a clinical decision, not a backfill's.
//
// Terminal rows are excluded. Completed and canceled work is history, and a missed dose must stay
// free to mint its successor.
func stampObligationIdentities(ctx context.Context, pool *pgxpool.Pool, tenantID string) (pgconn.CommandTag, error) {
	return pool.Exec(ctx, `
-- projection-review: membership=open obligation_instances joined to the lineage row of the rule they already point at, minus any row whose (animal, identity, sequence) group holds another open row; group_key=(target_id, identity_key, sequence); join_cardinality=one lineage row per rule_id (primary key) and the NOT EXISTS is a semi-join, so neither can fan a row out; pagination=n/a, one set-based statement run by a maintenance command, never on a request path; scope=one tenant when given, otherwise every tenant, which is the point of a backfill
UPDATE obligation_instances oi
SET rule_identity_key = l.identity_key,
    row_version = oi.row_version + 1,
    updated_at = now()
FROM protocol_rule_lineage l
WHERE l.tenant_id = oi.tenant_id
  AND l.rule_id = oi.rule_id
  AND oi.rule_identity_key IS DISTINCT FROM l.identity_key
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
  AND ($1::uuid IS NULL OR oi.tenant_id = $1::uuid)
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances other
    JOIN protocol_rule_lineage ol
      ON ol.tenant_id = other.tenant_id AND ol.rule_id = other.rule_id
    WHERE other.tenant_id = oi.tenant_id
      AND other.target_type = oi.target_type
      AND other.target_id = oi.target_id
      AND other."sequence" = oi."sequence"
      AND ol.identity_key = l.identity_key
      AND other.status IN ('scheduled', 'due', 'in_progress', 'deferred')
      AND other.obligation_id <> oi.obligation_id
  )`, nullableUUID(tenantID))
}

// countAmbiguousIdentities counts the (animal, identity, sequence) groups that already hold more
// than one open obligation. They cannot be labelled without asserting an invariant the data
// contradicts, and choosing which of them to cancel is a clinical decision, not a migration's.
func countAmbiguousIdentities(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	var groups int
	if err := pool.QueryRow(ctx, `
-- projection-review: membership=open obligation_instances joined 1:1 to the lineage row of the rule they already point at; group_key=(target_id, identity_key, sequence) -- the grain the uniqueness invariant is stated at; join_cardinality=one lineage row per rule_id (primary key), so the join cannot fan a row out; pagination=n/a, a single scalar count run once by a maintenance command, never on a request path; scope=one tenant when given, otherwise every tenant, which is the point of a backfill
SELECT count(*) FROM (
  SELECT oi.target_id, l.identity_key, oi."sequence"
  FROM obligation_instances oi
  JOIN protocol_rule_lineage l ON l.tenant_id = oi.tenant_id AND l.rule_id = oi.rule_id
  WHERE oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
    AND ($1::uuid IS NULL OR oi.tenant_id = $1::uuid)
  GROUP BY 1, 2, 3
  HAVING count(*) > 1
) g`, nullableUUID(tenantID)).Scan(&groups); err != nil {
		return 0, fmt.Errorf("count ambiguous identities: %w", err)
	}
	return groups, nil
}

func nullableUUID(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "backfill-protocol-rule-lineage:", err)
	os.Exit(1)
}
