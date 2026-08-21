// Command repair-obligation-duplicates retires duplicate open obligations and labels the
// survivors with the cause that produced them.
//
// The duplicates exist because obligation identity used to include the DUE DATE. A repeat
// dose is anchored to when the previous dose was actually given, so its due date legitimately
// moves, and each generation pass that recomputed a moved date inserted a SECOND row beside
// the first rather than moving it. The code fix anchors identity to the cause instead. This
// job is the other half: it cleans up what the old identity already produced, and stamps the
// surviving rows so the new partial unique indexes actually cover them.
//
// Without the stamping, a row created before the fix carries no repeat metadata, so the
// indexes -- which are partial on the metadata being present -- skip it entirely, and the
// first anchored insert after deploy lands beside it as yet another duplicate.
//
// Everything here is idempotent and bounded. A second run over an already-repaired tenant
// changes nothing and reports every row it skipped along with why.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	repeatReason   = "repeat_cycle_duplicate_repaired"
	campaignReason = "campaign_duplicate_repaired"

	// A repeat cycle's cause, when reconstructed after the fact, is the animal's most recent
	// completed obligation under the same rule.
	sourceCompletedObligation = "completed_obligation"
)

type config struct {
	TenantID string
	Mode     string
	Limit    int
	Timeout  time.Duration
	DryRun   bool
}

// counters is the job's whole report. Every row the job looked at lands in exactly one
// bucket, so the numbers add up to the rows examined and a skip always states its reason.
type counters struct {
	GroupsExamined      int
	DuplicatesRetired   int
	CyclesLabelled      int
	SkippedAlreadyValid int
	SkippedNoAnchor     int
	SkippedNotRepeat    int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "repair-obligation-duplicates"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	pool, err := platformpg.Connect(ctx, platformpg.ConfigFromEnv())
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := obligationpg.NewRepository(pool, 30*time.Second)
	got, err := repair(ctx, pool, repo, cfg)
	if err != nil {
		return err
	}
	mode := "applied"
	if cfg.DryRun {
		mode = "dry_run"
	}
	fmt.Printf(
		"repair-obligation-duplicates %s mode=%s tenant=%s groups_examined=%d duplicates_retired=%d cycles_labelled=%d skipped_already_valid=%d skipped_no_anchor=%d skipped_not_repeat=%d\n",
		mode, cfg.Mode, cfg.TenantID,
		got.GroupsExamined, got.DuplicatesRetired, got.CyclesLabelled,
		got.SkippedAlreadyValid, got.SkippedNoAnchor, got.SkippedNotRepeat,
	)
	return nil
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("repair-obligation-duplicates", flag.ContinueOnError)
	cfg := config{}
	fs.StringVar(&cfg.TenantID, "tenant", "", "tenant id to repair (required)")
	fs.StringVar(&cfg.Mode, "mode", "repeat", "repeat|campaign: which duplicate class to repair")
	fs.IntVar(&cfg.Limit, "limit", 500, "maximum duplicate groups to process in one run")
	fs.DurationVar(&cfg.Timeout, "timeout", 10*time.Minute, "overall run timeout")
	// Applying is opt-in. A repair job that mutates by default is one mistyped flag away from
	// retiring live work.
	fs.BoolVar(&cfg.DryRun, "dry-run", true, "report what would change without changing it")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return config{}, errors.New("-tenant is required")
	}
	switch cfg.Mode {
	case "repeat", "campaign":
	default:
		return config{}, fmt.Errorf("unknown -mode %q: want repeat or campaign", cfg.Mode)
	}
	if cfg.Limit <= 0 {
		return config{}, errors.New("-limit must be positive")
	}
	return cfg, nil
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
	Rows      []openRow
}

func repair(ctx context.Context, pool *pgxpool.Pool, repo canceller, cfg config) (counters, error) {
	var got counters
	groups, err := loadDuplicateGroups(ctx, pool, cfg)
	if err != nil {
		return got, err
	}
	now := time.Now().UTC()
	reason := repeatReason
	if cfg.Mode == "campaign" {
		reason = campaignReason
	}
	for _, g := range groups {
		got.GroupsExamined++
		// The survivor is the row furthest along: an animal already being worked must not
		// have its obligation retired underneath the person doing the work. Among equals the
		// newest row wins, because it holds the most recently computed due date.
		survivor := g.Rows[0]
		for _, row := range g.Rows[1:] {
			if !cfg.DryRun {
				if _, _, err := repo.CancelOpenObligationByIdempotencyKey(ctx, cfg.TenantID, row.IdempotencyKey, reason, now); err != nil {
					return got, fmt.Errorf("retire duplicate %s: %w", row.ObligationID, err)
				}
			}
			got.DuplicatesRetired++
		}
		if cfg.Mode == "campaign" {
			// A manual campaign has no repeat cause to anchor to; deduplication is the whole
			// repair. Counted explicitly so the two classes never look conflated in the report.
			got.SkippedNotRepeat++
			continue
		}
		if survivor.HasMetadata {
			got.SkippedAlreadyValid++
			continue
		}
		labelled, err := labelSurvivor(ctx, pool, cfg, survivor)
		if err != nil {
			return got, err
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
	return got, nil
}

// loadDuplicateGroups returns groups of open obligations that share an (animal, rule,
// protocol version) -- rows ordered survivor-first.
func loadDuplicateGroups(ctx context.Context, pool *pgxpool.Pool, cfg config) ([]dupGroup, error) {
	// status_rank keeps a row that is already being worked ahead of one that is merely
	// scheduled, so the survivor is never pulled out from under an operator mid-task.
	rows, err := pool.Query(ctx, `
WITH open_rows AS (
  SELECT oi.protocol_version_id::text AS version_id,
         oi.rule_id::text             AS rule_id,
         oi.target_id::text           AS target_id,
         oi.obligation_id::text       AS obligation_id,
         oi.idempotency_key,
         oi.status,
         (oi.repeat_cycle_source IS NOT NULL AND oi.repeat_cycle_source_ref IS NOT NULL) AS has_metadata,
         CASE oi.status
           WHEN 'in_progress' THEN 3
           WHEN 'due'         THEN 2
           WHEN 'scheduled'   THEN 1
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
    AND CASE
          WHEN $2::text = 'campaign'
            THEN lower(coalesce(pr.trigger_type, '')) = 'manual_campaign'
          ELSE lower(coalesce(pr.repeat, 'none')) NOT IN ('', 'none')
              OR lower(coalesce(pr.trigger_type, '')) = 'after_previous_completion'
        END
),
dups AS (
  SELECT version_id, rule_id, target_id
  FROM open_rows
  GROUP BY version_id, rule_id, target_id
  HAVING count(*) > 1
  ORDER BY version_id, rule_id, target_id
  LIMIT $3
)
SELECT o.version_id, o.rule_id, o.target_id, o.obligation_id, o.idempotency_key, o.status, o.has_metadata
FROM open_rows o
JOIN dups d USING (version_id, rule_id, target_id)
ORDER BY o.version_id, o.rule_id, o.target_id,
         o.has_metadata DESC, o.status_rank DESC, o.created_at DESC`,
		cfg.TenantID, cfg.Mode, cfg.Limit)
	if err != nil {
		return nil, fmt.Errorf("load duplicate groups: %w", err)
	}
	defer rows.Close()

	var out []dupGroup
	for rows.Next() {
		var versionID, ruleID, targetID string
		var row openRow
		if err := rows.Scan(&versionID, &ruleID, &targetID, &row.ObligationID, &row.IdempotencyKey, &row.Status, &row.HasMetadata); err != nil {
			return nil, fmt.Errorf("scan duplicate row: %w", err)
		}
		if n := len(out); n > 0 && out[n-1].VersionID == versionID && out[n-1].RuleID == ruleID && out[n-1].TargetID == targetID {
			out[n-1].Rows = append(out[n-1].Rows, row)
			continue
		}
		out = append(out, dupGroup{VersionID: versionID, RuleID: ruleID, TargetID: targetID, Rows: []openRow{row}})
	}
	return out, rows.Err()
}

// labelSurvivor stamps the surviving row with the completed dose that caused it. Reports
// false when no such dose exists, leaving the row untouched.
func labelSurvivor(ctx context.Context, pool *pgxpool.Pool, cfg config, survivor openRow) (bool, error) {
	if cfg.DryRun {
		// Still resolve the anchor, so a dry run's counts match what an apply run would do
		// rather than optimistically assuming every row is repairable.
		var exists bool
		err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM obligation_instances done
  JOIN obligation_instances target
    ON target.tenant_id = done.tenant_id
   AND target.rule_id = done.rule_id
   AND target.target_id = done.target_id
  WHERE target.tenant_id = $1
    AND target.obligation_id = $2::uuid
    AND done.status = 'completed'
    AND done.completed_at IS NOT NULL)`, cfg.TenantID, survivor.ObligationID).Scan(&exists)
		if err != nil {
			return false, fmt.Errorf("probe anchor: %w", err)
		}
		return exists, nil
	}
	// The WHERE clause re-checks that the row is still unlabelled, so two concurrent runs
	// cannot both claim to have labelled it.
	tag, err := pool.Exec(ctx, `
WITH anchor AS (
  SELECT done.obligation_id, done.completed_at
  FROM obligation_instances done
  JOIN obligation_instances target
    ON target.tenant_id = done.tenant_id
   AND target.rule_id = done.rule_id
   AND target.target_id = done.target_id
  WHERE target.tenant_id = $1
    AND target.obligation_id = $2::uuid
    AND done.status = 'completed'
    AND done.completed_at IS NOT NULL
  ORDER BY done.completed_at DESC
  LIMIT 1
)
UPDATE obligation_instances oi
SET repeat_cycle_source = $3,
    repeat_cycle_source_ref = anchor.obligation_id::text,
    repeat_cycle_anchor_obligation_id = anchor.obligation_id,
    repeat_cycle_anchor_at = anchor.completed_at,
    repeat_cycle_due_at = oi.due_at,
    row_version = oi.row_version + 1,
    updated_at = now()
FROM anchor
WHERE oi.tenant_id = $1
  AND oi.obligation_id = $2::uuid
  AND oi.repeat_cycle_source_ref IS NULL`,
		cfg.TenantID, survivor.ObligationID, sourceCompletedObligation)
	if err != nil {
		return false, fmt.Errorf("label survivor %s: %w", survivor.ObligationID, err)
	}
	return tag.RowsAffected() > 0, nil
}
