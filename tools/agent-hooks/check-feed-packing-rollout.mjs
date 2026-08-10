#!/usr/bin/env node

// Blocks the CONTRACT half of the feed-packing pen-day rollout from shipping in the same release as
// the EXPAND half.
//
// WHY THIS IS A GUARD AND NOT A COMMENT
// -------------------------------------
// Migration 000148 collapsed feed_packing_completions to one row per pen-day and ADDED the pen-day
// unique index while deliberately KEEPING the old session-bearing one
// (feed_packing_completions_natural_uq). The deployed binary inserts with
//   ON CONFLICT (tenant_id, park_id, shed_id, partition_key, session_no, target_date, workflow)
// and Postgres requires a unique index matching that exact column list, so dropping the old index
// before every instance runs the new binary makes EVERY packing submission from an old instance fail
// with 42P10 ("no unique or exclusion constraint matching the ON CONFLICT specification").
//
// The trap that made this worth automating: backend/cmd/migrate applies EVERY pending migration
// sequentially in one run. There is no per-release gate and no staged-apply flag. So authoring the
// drop as its own numbered migration and writing "apply this only after the rollout" in its header
// changes NOTHING -- it runs back-to-back with 000148, still before the new revision serves, and
// recreates the exact window the split was meant to remove. Splitting the SQL is not the control;
// shipping in two RELEASES is, and a file comment cannot enforce that.
//
// A documented rule with no executable check is not a gate (AGENTS.md). This is the check.
//
// WHEN THE CONTRACT MIGRATION IS GENUINELY DUE
// --------------------------------------------
// Once every API instance runs the pen-day binary, author the drop AND retire this guard in the SAME
// change: delete this script, its manifest entry, its Make target and its CI wiring. That makes
// landing the contract step a visible, reviewable act instead of a silent one.
//
// SCOPE / BLIND SPOTS (stated per the guard-honesty rule):
//   - It matches DROP statements against the compatibility index BY NAME. A migration that removed
//     the index by some other route -- dropping and recreating the table, ALTER TABLE ... DROP
//     CONSTRAINT under a different constraint name, or dynamic SQL in a DO block -- would not be
//     seen. Those are not plausible ways to reach this index, but they are not covered.
//   - It reasons about the repository, not about what is deployed. It cannot know whether the
//     rollout has actually completed; that is exactly why retiring it is a deliberate human act.

import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const MIGRATIONS = resolve(repo, "backend/migrations/postgres");

// The compatibility index the deployed (pre-pen-day) binary's ON CONFLICT clause resolves against.
const COMPAT_INDEX = "feed_packing_completions_natural_uq";
// A DROP of it in any migration authored after the expand is the premature contract step.
const DROP_PATTERN = new RegExp(String.raw`DROP\s+INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+EXISTS\s+)?${COMPAT_INDEX}\b`, "i");

// The EXPAND migration. Anything at or below this version is already-applied history and is out of
// scope: 000137, for instance, legitimately DROPS and immediately RECREATES the index to widen its
// key with partition_key. Only a migration authored AFTER the expand can be the premature contract
// step, and an applied migration may never be amended anyway (AGENTS.md), so scoping by version is
// both precise and the only honest reading.
const EXPAND_VERSION = 148;

function migrationVersion(filename) {
  const m = /^(\d+)_/.exec(filename);
  return m ? Number(m[1]) : Number.NaN;
}

/** Returns findings for one migration's text. Exported for the self-test. */
export function inspectMigration(filename, sql) {
  const version = migrationVersion(filename);
  if (Number.isFinite(version) && version <= EXPAND_VERSION) return [];
  // Only the Up section can drop it in a forward deploy; a Down section is a rollback path and is
  // out of scope for the rollout window.
  const up = sql.split(/^--\s*\+goose\s+Down/im)[0] ?? sql;
  if (!DROP_PATTERN.test(up)) return [];
  return [
    `${filename}: drops ${COMPAT_INDEX}, the index the currently-deployed binary's ON CONFLICT ` +
      `clause requires. backend/cmd/migrate applies every pending migration in one run, so this ` +
      `would execute before the pen-day binary serves and fail every packing submission from an ` +
      `old instance with 42P10. The contract half belongs to a LATER release; land it together ` +
      `with the deliberate retirement of this guard.`,
  ];
}

function selfTest() {
  const cases = [
    {
      name: "a migration dropping the compatibility index is BLOCKED",
      sql: `-- +goose Up\nDROP INDEX IF EXISTS ${COMPAT_INDEX};\n`,
      wantBlocked: true,
    },
    {
      name: "CONCURRENTLY spelling is BLOCKED too",
      sql: `-- +goose Up\nDROP INDEX CONCURRENTLY ${COMPAT_INDEX};\n`,
      wantBlocked: true,
    },
    {
      name: "lowercase is BLOCKED",
      sql: `-- +goose Up\ndrop index if exists ${COMPAT_INDEX};\n`,
      wantBlocked: true,
    },
    {
      // ADVERSARIAL SIBLING: the expand migration creates the NEW index and merely NAMES the old one
      // in prose. Matching the bare name rather than a DROP statement would fail 000148 itself and
      // make the guard useless.
      name: "creating the pen-day index while only mentioning the old one PASSES",
      sql:
        `-- +goose Up\n-- The old ${COMPAT_INDEX} is deliberately KEPT for the rollout.\n` +
        `CREATE UNIQUE INDEX IF NOT EXISTS feed_packing_completions_pen_day_uq\n  ON feed_packing_completions (tenant_id);\n`,
      wantBlocked: false,
    },
    {
      // ADVERSARIAL SIBLING: a DIFFERENT index on the same table must not be caught.
      name: "dropping an unrelated index on the same table PASSES",
      sql: `-- +goose Up\nDROP INDEX IF EXISTS feed_packing_completions_serving_idx;\n`,
      wantBlocked: false,
    },
    {
      // ADVERSARIAL SIBLING: a rollback path is not a forward-deploy risk.
      name: "a DROP that appears only in the Down section PASSES",
      sql: `-- +goose Up\nSELECT 1;\n-- +goose Down\nDROP INDEX IF EXISTS ${COMPAT_INDEX};\n`,
      wantBlocked: false,
    },
    {
      // ADVERSARIAL SIBLING, and the case that caught the guard's first draft: 000137 drops and
      // immediately RECREATES this index to widen its key. It is already-applied history, it is
      // BELOW the expand version, and flagging it would make the guard permanently red and useless.
      name: "an already-applied historical migration below the expand version PASSES",
      filename: "000137_feed_completions_partition_label.sql",
      sql: `-- +goose Up\nDROP INDEX IF EXISTS ${COMPAT_INDEX};\nCREATE UNIQUE INDEX ${COMPAT_INDEX} ON feed_packing_completions (tenant_id);\n`,
      wantBlocked: false,
    },
    {
      name: "a NEW migration above the expand version dropping it is BLOCKED",
      filename: "000149_feed_packing_drop_session_natural_key.sql",
      sql: `-- +goose Up\nDROP INDEX IF EXISTS ${COMPAT_INDEX};\n`,
      wantBlocked: true,
    },
  ];

  for (const c of cases) {
    const blocked = inspectMigration(c.filename ?? "000200_fixture.sql", c.sql).length > 0;
    if (blocked !== c.wantBlocked) {
      console.error(`feed-packing-rollout guard self-test FAILED: ${c.name} (blocked=${blocked}, want ${c.wantBlocked})`);
      process.exit(1);
    }
  }
  console.log(`feed-packing-rollout guard self-test: ok (${cases.length} cases)`);
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const findings = [];
  for (const file of readdirSync(MIGRATIONS).filter((f) => f.endsWith(".sql")).sort()) {
    findings.push(...inspectMigration(file, readFileSync(resolve(MIGRATIONS, file), "utf8")));
  }
  if (findings.length) {
    console.error("feed-packing-rollout guard: FAIL");
    for (const f of findings) console.error(`  - ${f}`);
    console.error("\nSee backend/migrations/postgres/000148_feed_packing_pen_day_grain.sql and docs/decisions/feed-distribution-verification.md");
    process.exit(1);
  }
  console.log("feed-packing-rollout guard: ok (compatibility index retained for the rollout)");
}

main();
