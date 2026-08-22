// Package repair cleans up duplicate open obligations left behind by the old due-date
// identity, and labels the survivors with the cause that produced them.
//
// The duplicates exist because obligation identity used to include the DUE DATE. A repeat
// dose is anchored to when the previous dose was actually given, so its due date legitimately
// moves, and each generation pass that recomputed a moved date inserted a SECOND row beside
// the first rather than moving it. The code fix anchors identity to the cause instead. This
// package is the other half: it cleans up what the old identity already produced, and stamps
// the surviving rows so the new partial unique indexes actually cover them.
//
// Without the stamping, a row created before the fix carries no repeat metadata, so the
// indexes -- which are partial on the metadata being present -- skip it entirely, and the
// first anchored insert after deploy lands beside it as yet another duplicate.
//
// Everything here is idempotent and bounded. A second run over an already-repaired tenant
// changes nothing and reports every row it skipped along with why.
package repair

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	repeatReason = "repeat_cycle_duplicate_repaired"
	// One plan, one draft. Where a scope already holds more than one, the newest survives --
	// it is the one someone was most recently working in -- and the losers are RETIRED rather
	// than deleted: unreachable work either way (the list renders the first draft it finds),
	// but a retired row keeps what the farm typed and stays in the version history.
	draftsReason   = "duplicate_draft_retired"
	campaignReason = "campaign_duplicate_repaired"
	courseReason   = "course_dose_duplicate_repaired"

	// A repeat cycle's cause, reconstructed after the fact, is the animal's most recent
	// accepted administration of the rule's own vaccine -- referenced exactly the way
	// generation references it, as vaccine|administered-at|dose. Anchoring instead to the
	// completed obligation would give the legacy row a reference generation never computes,
	// so the two would not collapse and the duplicate would come straight back.
	sourceTrustedHistory = "trusted_history"
)

// Config selects what to repair and whether to actually do it.
type Config struct {
	TenantID string
	Mode     string
	Limit    int
	// Apply, not DryRun: the zero value has to be the harmless one. A repair whose default
	// is to mutate is one forgotten field away from retiring live work.
	Apply bool
}

// Counters is the job's whole report. Every row it looked at lands in exactly one bucket, so
// the numbers add up to the rows examined and a skip always states its reason.
type Counters struct {
	GroupsExamined      int
	DuplicatesRetired   int
	CyclesLabelled      int
	SkippedAlreadyValid int
	SkippedNoAnchor     int
	SkippedNotRepeat    int
}

// Modes lists every repairable duplicate class.
func Modes() []string { return []string{"repeat", "course", "campaign", "drafts"} }

// ValidMode reports whether mode names a repairable class.
func ValidMode(mode string) bool {
	for _, m := range Modes() {
		if m == mode {
			return true
		}
	}
	return false
}

// canceller is the narrow slice of the obligation repository this job needs. Retiring a
// duplicate goes through the repository's own cancel path rather than a direct UPDATE,
// because cancelling an obligation also has to release its batch's reserved stock and
// recompute the batch's planned quantity. A hand-rolled UPDATE here would leave those drifting.
type canceller interface {
	CancelOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (string, bool, error)
}

type openRow struct {
	ObligationID   string
	IdempotencyKey string
	Status         string
	HasMetadata    bool
}

type dupGroup struct {
	VersionID string
	RuleID    string
	TargetID  string
	Sequence  int32
	CycleRef  string
	Rows      []openRow
}

func (g dupGroup) sameGroup(versionID, ruleID, targetID string, sequence int32, cycleRef string) bool {
	return g.VersionID == versionID && g.RuleID == ruleID && g.TargetID == targetID &&
		g.Sequence == sequence && g.CycleRef == cycleRef
}

// Run performs the repair described by cfg.
// pageSize bounds one database round trip. Limit bounds the whole run.
const pageSize = 500

func Run(ctx context.Context, pool *pgxpool.Pool, repo canceller, cfg Config) (Counters, error) {
	var got Counters
	if cfg.Mode == "drafts" {
		return retireDuplicateDrafts(ctx, pool, cfg)
	}
	// Paged with a keyset cursor rather than a single LIMIT window. Rows that cannot be
	// repaired -- no reconstructable cause -- stay open and sort to the front, so a plain
	// LIMIT would hand back the same unrepairable groups on every run and the tail of a large
	// tenant would never be reached. The cursor steps past them.
	var cursor groupKey
	for got.GroupsExamined < cfg.Limit {
		size := pageSize
		if remaining := cfg.Limit - got.GroupsExamined; remaining < size {
			size = remaining
		}
		groups, err := loadDuplicateGroups(ctx, pool, cfg, cursor, size)
		if err != nil {
			return got, err
		}
		if len(groups) == 0 {
			return got, nil
		}
		cursor = groups[len(groups)-1].key()
		if err := repairGroups(ctx, pool, repo, cfg, groups, &got); err != nil {
			return got, err
		}
	}
	return got, nil
}

// groupKey is the keyset cursor: the ordered identity of the last group processed.
type groupKey struct {
	VersionID string
	RuleID    string
	TargetID  string
	Sequence  int32
	CycleRef  string
}

func (g dupGroup) key() groupKey {
	return groupKey{VersionID: g.VersionID, RuleID: g.RuleID, TargetID: g.TargetID, Sequence: g.Sequence, CycleRef: g.CycleRef}
}

func repairGroups(ctx context.Context, pool *pgxpool.Pool, repo canceller, cfg Config, groups []dupGroup, got *Counters) error {
	now := time.Now().UTC()
	reason := repeatReason
	switch cfg.Mode {
	case "campaign":
		reason = campaignReason
	case "course":
		reason = courseReason
	}
	for _, g := range groups {
		got.GroupsExamined++
		// The survivor is the row furthest along: an animal already being worked must not
		// have its obligation retired underneath the person doing the work. Among equals the
		// newest row wins, because it holds the most recently computed due date.
		survivor := g.Rows[0]
		for _, row := range g.Rows[1:] {
			if cfg.Apply {
				// One cancel per duplicate row is the point, not an oversight. Cancelling an
				// obligation also releases its batch's reserved stock and recomputes that batch's
				// planned quantity; a bulk UPDATE would leave both drifting, which is the defect
				// this job exists to avoid creating. The loop is bounded twice over -- by the
				// group's own size (a handful) and by the run's --limit -- and the whole job is a
				// deliberate operational one-shot, not a request path.
				// scale-guard:ignore: per-row cancel is required for batch stock reconciliation; bounded by group size and --limit
				if _, _, err := repo.CancelOpenObligationByIdempotencyKey(ctx, cfg.TenantID, row.IdempotencyKey, reason, now); err != nil {
					return fmt.Errorf("retire duplicate %s: %w", row.ObligationID, err)
				}
			}
			got.DuplicatesRetired++
		}
		if cfg.Mode != "repeat" {
			// Neither a manual campaign nor a fixed course dose has a repeat cause to anchor
			// to; deduplication is the whole repair. Counted in its own bucket so the classes
			// never look conflated in the report.
			got.SkippedNotRepeat++
			continue
		}
		if survivor.HasMetadata {
			got.SkippedAlreadyValid++
			continue
		}
		labelled, err := labelSurvivor(ctx, pool, cfg, survivor)
		if err != nil {
			return err
		}
		if !labelled {
			// No completed dose under this rule for this animal, so the cycle's cause cannot
			// be reconstructed. Left unlabelled rather than guessed: a wrong anchor is worse
			// than none, because it would make two genuinely different cycles collide.
			got.SkippedNoAnchor++
			continue
		}
		got.CyclesLabelled++
	}
	return nil
}

// loadDuplicateGroups returns groups of open obligations that share an (animal, rule,
// protocol version) -- rows ordered survivor-first.
func loadDuplicateGroups(ctx context.Context, pool *pgxpool.Pool, cfg Config, after groupKey, limit int) ([]dupGroup, error) {
	// status_rank keeps a row that is already being worked ahead of one that is merely
	// scheduled, so the survivor is never pulled out from under an operator mid-task.
	rows, err := pool.Query(ctx, `
WITH open_rows AS (
  SELECT oi.protocol_version_id::text AS version_id,
         oi.rule_id::text             AS rule_id,
         oi.target_id::text           AS target_id,
         -- A manual campaign names the animals itself, so its slot is the rule, not a
         -- sequence: the campaign row and a generated row for the same rule are the same
         -- work under two sequence numbers.
         CASE WHEN $2::text = 'campaign' THEN 0 ELSE oi."sequence" END AS sequence,
         -- Rows already stamped with DIFFERENT causes are different cycles, and the new
         -- indexes say so. Grouping them together would have the repair destroy rows the
         -- identity model calls correct. Unstamped rows share the empty key, which is what
         -- puts the pre-fix duplicates in one group.
         CASE WHEN $2::text = 'repeat' THEN coalesce(oi.repeat_cycle_source_ref, '') ELSE '' END AS cycle_ref,
         oi.obligation_id::text       AS obligation_id,
         oi.idempotency_key,
         oi.status,
         (oi.repeat_cycle_source IS NOT NULL AND oi.repeat_cycle_source_ref IS NOT NULL) AS has_metadata,
         CASE oi.status
           WHEN 'in_progress' THEN 4
           WHEN 'due'         THEN 3
           WHEN 'scheduled'   THEN 2
           WHEN 'deferred'    THEN 1
           ELSE 0
         END AS status_rank,
         oi.created_at
  FROM obligation_instances oi
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  WHERE oi.tenant_id = $1
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
    AND CASE $2::text
          WHEN 'campaign' THEN lower(coalesce(pr.trigger_type, '')) = 'manual_campaign'
          -- A fixed course dose is due once. Its due date still moves -- a corrected date of
          -- birth or a drive realignment moves it -- and under due-date identity that minted
          -- a second row for the same slot. There is no repeat cause to anchor here, so the
          -- course slot itself (rule, animal, sequence) is the identity.
          WHEN 'course' THEN lower(coalesce(pr.repeat, 'none')) IN ('', 'none')
              AND lower(coalesce(pr.trigger_type, '')) NOT IN ('manual_campaign', 'after_previous_completion')
          ELSE lower(coalesce(pr.repeat, 'none')) NOT IN ('', 'none')
              OR lower(coalesce(pr.trigger_type, '')) = 'after_previous_completion'
        END
),
dups AS (
  SELECT version_id, rule_id, target_id, sequence, cycle_ref
  FROM open_rows
  GROUP BY version_id, rule_id, target_id, sequence, cycle_ref
  -- Every open repeat row is examined, not only the duplicated ones. A pre-fix row carries
  -- no metadata whether or not it happens to have a twin, and until it is stamped the
  -- partial indexes skip it and the first anchored insert after deploy lands beside it.
  -- Keyset cursor: strictly after the last group the previous page handled.
  HAVING (version_id, rule_id, target_id, sequence, cycle_ref) > ($4::text, $5::text, $6::text, $7::int, $8::text)
  ORDER BY version_id, rule_id, target_id, sequence, cycle_ref
  LIMIT $3
)
SELECT o.version_id, o.rule_id, o.target_id, o.sequence, o.cycle_ref, o.obligation_id, o.idempotency_key, o.status, o.has_metadata
FROM open_rows o
JOIN dups d USING (version_id, rule_id, target_id, sequence, cycle_ref)
-- Status first, then metadata. Ordering metadata first would let a freshly stamped
-- scheduled row outrank an unstamped in_progress one and cancel live work -- the
-- exact post-deploy shape, where the new anchored insert lands beside the old row
-- an operator is already mid-task on.
ORDER BY o.version_id, o.rule_id, o.target_id, o.sequence, o.cycle_ref,
         o.status_rank DESC, o.has_metadata DESC, o.created_at DESC`,
		cfg.TenantID, cfg.Mode, limit, after.VersionID, after.RuleID, after.TargetID, after.Sequence, after.CycleRef)
	if err != nil {
		return nil, fmt.Errorf("load duplicate groups: %w", err)
	}
	defer rows.Close()

	var out []dupGroup
	for rows.Next() {
		var versionID, ruleID, targetID, cycleRef string
		var sequence int32
		var row openRow
		if err := rows.Scan(&versionID, &ruleID, &targetID, &sequence, &cycleRef, &row.ObligationID, &row.IdempotencyKey, &row.Status, &row.HasMetadata); err != nil {
			return nil, fmt.Errorf("scan duplicate row: %w", err)
		}
		if n := len(out); n > 0 && out[n-1].sameGroup(versionID, ruleID, targetID, sequence, cycleRef) {
			out[n-1].Rows = append(out[n-1].Rows, row)
			continue
		}
		out = append(out, dupGroup{
			VersionID: versionID, RuleID: ruleID, TargetID: targetID,
			Sequence: sequence, CycleRef: cycleRef, Rows: []openRow{row},
		})
	}
	return out, rows.Err()
}

// anchorCTE resolves the completed dose that caused a surviving cycle. Both the dry run and
// the apply path use this one definition, so a dry run cannot report a repair the apply
// would then decline to make.
//
// The anchor is the animal's LATEST verified administration of the rule's own vaccine --
// matched on vaccine code, exactly as generation matches it, and deliberately not capped at
// the row's due date. Capping it there looked safer and was the opposite: an overdue row
// whose animal has since been vaccinated again would be stamped with the OLD cause, while
// generation computes the new one, and the two no longer collapse. The repair would then
// manufacture the duplicate it exists to remove -- and worse than leaving the row alone,
// because an unstamped row is still caught by the guard's no-cause branch.
//
// It is only meaningful for a genuine repeat
// rule, where the cause of each cycle is the same rule's own previous dose. A pure
// after_previous_completion chain is caused by the UPSTREAM rule in the course, so
// reconstructing it from the same rule would stamp a stale anchor -- and a wrong anchor is
// worse than none, because it makes two genuinely different cycles collide.
const anchorCTE = `
WITH target AS (
  SELECT oi.tenant_id, oi.obligation_id, oi.target_id, oi.due_at,
         lower(btrim(coalesce(nullif(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
                              nullif(pv.rule_dsl -> 'vaccine' ->> 'code', ''), ''))) AS vaccine_code
  FROM obligation_instances oi
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  WHERE oi.tenant_id = $1
    AND oi.obligation_id = $2::uuid
    AND oi.repeat_cycle_source_ref IS NULL
    AND lower(coalesce(pr.repeat, 'none')) NOT IN ('', 'none')
),
anchor AS (
  SELECT vc.administered_at,
         t.vaccine_code || '|' ||
           to_char(vc.administered_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') || '|' ||
           coalesce(done_rule.sequence, 0)::text AS source_ref
  FROM vaccination_completions vc
  JOIN target t ON t.tenant_id = vc.tenant_id AND t.target_id = vc.goat_id
  JOIN obligation_instances done
    ON done.tenant_id = vc.tenant_id AND done.obligation_id = vc.obligation_id
  JOIN protocol_rules done_rule
    ON done_rule.tenant_id = done.tenant_id AND done_rule.rule_id = done.rule_id
  JOIN protocol_versions done_version
    ON done_version.tenant_id = done.tenant_id
   AND done_version.protocol_version_id = done.protocol_version_id
  WHERE vc.status = 'accepted'
    AND vc.verified_at IS NOT NULL
    AND lower(btrim(coalesce(nullif(done_rule.eligibility_json -> 'vaccine' ->> 'code', ''),
                             nullif(done_version.rule_dsl -> 'vaccine' ->> 'code', ''), '')))
        = t.vaccine_code
    AND t.vaccine_code <> ''
  ORDER BY vc.administered_at DESC
  LIMIT 1
)`

// labelSurvivor stamps the surviving row with the completed dose that caused it. Reports
// false when no such dose exists, leaving the row untouched.
func labelSurvivor(ctx context.Context, pool *pgxpool.Pool, cfg Config, survivor openRow) (bool, error) {
	if !cfg.Apply {
		var exists bool
		if err := pool.QueryRow(ctx, anchorCTE+`
SELECT EXISTS (SELECT 1 FROM anchor)`, cfg.TenantID, survivor.ObligationID).Scan(&exists); err != nil {
			return false, fmt.Errorf("probe anchor: %w", err)
		}
		return exists, nil
	}
	// The target CTE re-checks that the row is still unlabelled, so two concurrent runs
	// cannot both claim to have labelled it.
	tag, err := pool.Exec(ctx, anchorCTE+`
UPDATE obligation_instances oi
SET repeat_cycle_source = $3,
    repeat_cycle_source_ref = anchor.source_ref,
    repeat_cycle_anchor_at = anchor.administered_at,
    repeat_cycle_due_at = oi.due_at,
    row_version = oi.row_version + 1,
    updated_at = now()
FROM anchor, target
WHERE oi.tenant_id = target.tenant_id
  AND oi.obligation_id = target.obligation_id`,
		cfg.TenantID, survivor.ObligationID, sourceTrustedHistory)
	if err != nil {
		if isRepeatCycleConflict(err) {
			// Another open cycle already claims this cause. Leaving the row unstamped is
			// correct: stamping it would assert two open cycles share one cause.
			return false, nil
		}
		return false, fmt.Errorf("label survivor %s: %w", survivor.ObligationID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// isRepeatCycleConflict reports a rejection by the partial unique indexes that enforce one
// open cycle per cause.
func isRepeatCycleConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return pgErr.ConstraintName == "obligation_repeat_cycle_open_anchor_unique_idx" ||
		pgErr.ConstraintName == "obligation_repeat_cycle_open_source_unique_idx"
}

// retireDuplicateDrafts leaves one draft per (tenant, plan, scope).
//
// The newest survives -- it is the one someone was most recently working in -- and the losers
// are RETIRED rather than deleted: unreachable work either way, since the list renders the
// first draft it finds, but a retired row keeps what the farm typed and stays in the version
// history.
//
// This has to happen BEFORE the one-draft unique index is built, because the index cannot be
// created over rows that already violate it. It is a command rather than a step inside that
// migration on purpose: retiring a draft is a data change on rows the seed itself authors, and
// a migration cannot be dry-run, cannot be reviewed row by row, and cannot be re-run
// selectively. Here it reports by default and mutates only with --apply.
func retireDuplicateDrafts(ctx context.Context, pool *pgxpool.Pool, cfg Config) (Counters, error) {
	var got Counters
	const losers = `
SELECT protocol_version_id::text
FROM (
  SELECT protocol_version_id,
         row_number() OVER (
           PARTITION BY tenant_id, protocol_id, scope_type, scope_id
           ORDER BY created_at DESC, protocol_version_id DESC
         ) AS rn
  FROM protocol_versions
  WHERE tenant_id = $1::uuid
    AND status = 'draft'
) ranked
WHERE ranked.rn > 1
LIMIT $2`

	rows, err := pool.Query(ctx, losers, cfg.TenantID, cfg.Limit)
	if err != nil {
		return got, fmt.Errorf("find duplicate drafts: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return got, fmt.Errorf("scan duplicate draft: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return got, err
	}

	got.GroupsExamined = len(ids)
	got.SkippedNotRepeat = len(ids) // counted in its own bucket: a draft is not a repeat cycle
	if !cfg.Apply || len(ids) == 0 {
		got.DuplicatesRetired = len(ids)
		return got, nil
	}
	tag, err := pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'retired',
    retired_at = now(),
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND protocol_version_id = ANY($2::uuid[])
  AND status = 'draft'`, cfg.TenantID, ids)
	if err != nil {
		return got, fmt.Errorf("retire duplicate drafts: %w", err)
	}
	got.DuplicatesRetired = int(tag.RowsAffected())
	return got, nil
}
