#!/usr/bin/env node

// Blocks new/changed aggregate projections that lack explicit grain/key proof
// and adversarial regression tests. Includes committed, staged, unstaged, and
// untracked work so a dirty tree cannot produce a false "no relevant changes".

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const MARKER = /projection-review:\s*membership=([^;\n]+);\s*group_key=([^;\n]+);\s*join_cardinality=([^;\n]+);\s*pagination=([^;\n]+);\s*scope=([^;\n]+)/i;
const TESTS = {
  cardinality: /(?:OneToMany|MultipleDimensions)/,
  pagination: /(?:Pagination|PageBoundary|MultiPage)/,
  date: /(?:DateShift|ScheduledDate|ExecutionDate)/,
  scope: /(?:ScopeHierarchy|ParkScope|CohortScope)/,
  status: /(?:StatusMatrix|EveryStatus|StatusBuckets)/,
};
const PROCESS_INTEGRITY_REPO = "backend/internal/processintegrity/adapters/postgres/repository.go";
const SOP_REPO = "backend/internal/sop/adapters/postgres/repository.go";
const VACCINATION_EXECUTION_REPO = "backend/internal/vaccinationexecution/adapters/postgres/repository.go";

function git(args, allowFailure = false) {
  try {
    return execFileSync("git", args, { cwd: repo, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  } catch (error) {
    if (allowFailure) return "";
    throw error;
  }
}

function baseRef() {
  const configured = process.env.AGGREGATE_PROJECTION_BASE;
  if (configured) return configured;
  if (git(["rev-parse", "--verify", "origin/main"], true).trim()) return "origin/main";
  return git(["rev-parse", "HEAD^"], true).trim() || "HEAD";
}

function zlist(output) {
  return output.split("\0").filter(Boolean);
}

function changedFiles(base) {
  const files = new Set([
    ...zlist(git(["diff", "--name-only", "-z", `${base}...HEAD`], true)),
    ...zlist(git(["diff", "--name-only", "-z"], true)),
    ...zlist(git(["diff", "--cached", "--name-only", "-z"], true)),
    ...zlist(git(["ls-files", "--others", "--exclude-standard", "-z"], true)),
  ]);
  return [...files];
}

function diffFor(file, base, untracked) {
  if (untracked.has(file)) {
    const source = readFileSync(resolve(repo, file), "utf8");
    return source.split("\n").map((line) => `+${line}`).join("\n");
  }
  return [
    git(["diff", "--unified=80", `${base}...HEAD`, "--", file], true),
    git(["diff", "--unified=80", "HEAD", "--", file], true),
  ].join("\n");
}

function hunks(diff) {
  const split = diff.split(/^@@.*$/m);
  const parts = split.length > 1 ? split.slice(1) : [diff];
  return parts.map((part) => {
    const lines = part.split("\n").filter((line) => !line.startsWith("---") && !line.startsWith("+++"));
    return {
      visible: lines.filter((line) => !line.startsWith("-")).map((line) => line.replace(/^\+/, "")).join("\n"),
      added: lines.filter((line) => line.startsWith("+")).map((line) => line.slice(1)).join("\n"),
    };
  });
}

function isCandidate(hunk) {
  const shape = /\b(?:COUNT|SUM|AVG|JSONB?_AGG|ARRAY_AGG)\s*\(/i.test(hunk.visible)
    && /\bJOIN\b/i.test(hunk.visible)
    && /\bGROUP\s+BY\b/i.test(hunk.visible);
  const touched = /\b(?:SELECT|WITH|JOIN|GROUP\s+BY|COUNT|SUM|AVG|JSONB?_AGG|ARRAY_AGG)\b/i.test(hunk.added);
  return shape && touched;
}

export function inspectFixture(sourceHunks, changedTests) {
  const candidates = sourceHunks.filter(isCandidate);
  if (candidates.length === 0) return [];
  const failures = [];
  for (const [index, candidate] of candidates.entries()) {
    // A real marker is prose and wraps across several comment lines; the MARKER fields are
    // `[^;\n]+`, so a wrapped `membership=...` never matched and the guard demanded a marker that
    // was already there. Normalise the comment block to one line first: strip `//` / `--` / `*`
    // prefixes and collapse the wrapping, then match. This changes only what the guard can SEE,
    // never what it requires — all five fields are still mandatory, and a genuinely absent or
    // partial marker still fails (asserted in the self-test).
    const unwrapped = candidate.visible
      .replace(/^[ \t]*(?:\/\/|--|\*)[ \t]?/gm, "")
      .replace(/\n[ \t]*/g, " ");
    if (!MARKER.test(unwrapped)) failures.push(`aggregate hunk ${index + 1}: missing complete projection-review marker`);
  }
  const requirements = new Set(["cardinality", "pagination"]);
  const joined = candidates.map((h) => h.visible).join("\n");
  if (/\b(?:due_at|due_date|planned_date|window_start|window_end|execution_date)\b/i.test(joined)) requirements.add("date");
  if (/\b(?:scope_type|scope_id|parent_location_id|park_id|shed_id|cohort_id)\b/i.test(joined)) requirements.add("scope");
  if (/\bstatus\b/i.test(joined)) requirements.add("status");
  for (const requirement of requirements) {
    if (!TESTS[requirement].test(changedTests)) failures.push(`missing changed ${requirement} adversarial test (${TESTS[requirement]})`);
  }
  return failures;
}

function inspectProcessIntegrityShedGrain(source) {
  const failures = [];
  const badTaskState = /task_state\s+IN\s*\(\s*'submitted'\s*,\s*'needs_review'\s*\)/i;
  const shedProjection = /submission_state|completion_recorded|proof_count|verification_state/i;
  if (badTaskState.test(source) && shedProjection.test(source)) {
    failures.push(
      "process-integrity shed-grain state must not derive proof/verification/submitted from shared parent task_state; use submission_state/completion/proof facts",
    );
  }
  const bareParentSubmission = /FROM\s+sop_submissions\s+sub[\s\S]{0,500}?sub\.task_id\s*=\s*COALESCE\s*\(\s*oi\.sop_task_id\s*,\s*ob\.sop_task_id\s*\)/i;
  if (bareParentSubmission.test(source)) {
    failures.push(
      "process-integrity shed-grain submission lookup must join sop_submission_items by current goat before reading sop_submissions; latest parent submission leaks across sibling sheds",
    );
  }
  return failures;
}

function inspectShedSubmitTerminalState(source) {
  const failures = [];
  const submitTask = source.match(/func \(r \*Repository\) SubmitTask[\s\S]*?\nfunc /);
  const body = submitTask?.[0] ?? "";
  if (!body) return failures;
  if (/WHERE\s+tenant_id\s*=\s*\$1::uuid[\s\S]{0,160}?state\s+IN\s*\([^)]*'accepted'[^)]*\)/i.test(body)) {
    failures.push("SubmitTask must not include accepted in the writeable sop_tasks state set; accepted is terminal except exact idempotency replay");
  }
  const insertIndex = body.indexOf("INSERT INTO sop_submissions");
  const acceptedCheckIndex = body.search(/currentState\s*==\s*"accepted"|state\s*=\s*'accepted'/i);
  if (insertIndex >= 0 && (acceptedCheckIndex < 0 || acceptedCheckIndex > insertIndex)) {
    failures.push("SubmitTask must reject accepted tasks before inserting sop_submissions, fanouts, audits, or movement side effects");
  }
  return failures;
}

function inspectVaccinationExecutionDimensionFanout(source) {
  const failures = [];
  const directProtocolDimensionJoin =
    /\bJOIN\s+protocol_rule_dimensions\s+\w+\s+ON\s+\w+\.tenant_id\s*=\s*pr\.tenant_id\s+AND\s+\w+\.rule_id\s*=\s*pr\.rule_id/i;
  if (directProtocolDimensionJoin.test(source)) {
    failures.push(
      "vaccination execution counts must not join protocol_rule_dimensions directly; collapse/deduplicate dimensions before counting drive animals or shed cards will explode (e.g. 120 -> 480)",
    );
  }
  return failures;
}

function selfTest() {
  const sql = `-- projection-review: membership=batch_members; group_key=batch_id; join_cardinality=dimensions pre-aggregated; pagination=one tenant aggregate before paging; scope=explicit park/shed/cohort CASE\nSELECT park_id, status, due_date, COUNT(*) FROM obligations JOIN dimensions USING (rule_id) GROUP BY park_id, status, due_date`;
  const hunk = { visible: sql, added: sql };
  const goodTests = "TestDriveOneToMany TestDrivePageBoundary TestDriveDateShift TestDriveScopeHierarchy TestDriveStatusMatrix";
  const bad = inspectFixture([{ visible: sql.replace(/-- projection-review.*\n/, ""), added: sql }], "");
  const good = inspectFixture([hunk], goodTests);
  const badShedGrain = inspectProcessIntegrityShedGrain("CASE WHEN task_state IN ('submitted', 'needs_review') THEN 'uploaded' END\nsubmission_state");
  const goodShedGrain = inspectProcessIntegrityShedGrain("CASE WHEN submission_state IN ('submitted', 'needs_review') THEN 'uploaded' END\ntask_state");
  const badParentSubmission = inspectProcessIntegrityShedGrain("FROM sop_submissions sub\nWHERE sub.tenant_id = oi.tenant_id\n  AND sub.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)");
  const goodParentSubmission = inspectProcessIntegrityShedGrain("FROM sop_submission_items si\nJOIN sop_submissions sub ON sub.submission_id = si.submission_id\nWHERE si.goat_id = oi.target_id");
  const badAcceptedSubmit = inspectShedSubmitTerminalState("func (r *Repository) SubmitTask() {\nINSERT INTO sop_submissions\nUPDATE sop_tasks SET state=$3 WHERE tenant_id = $1::uuid AND state IN ('queued','accepted')\n}\nfunc next() {}");
  const goodAcceptedSubmit = inspectShedSubmitTerminalState("func (r *Repository) SubmitTask() {\nif currentState == \"accepted\" { return ports.ErrConflict }\nINSERT INTO sop_submissions\nUPDATE sop_tasks SET state=$3 WHERE state IN ('queued','needs_review')\n}\nfunc next() {}");
  const badVaccinationFanout = inspectVaccinationExecutionDimensionFanout("LEFT JOIN protocol_rule_dimensions prd\n  ON prd.tenant_id = pr.tenant_id\n AND prd.rule_id = pr.rule_id");
  const goodVaccinationFanout = inspectVaccinationExecutionDimensionFanout("LEFT JOIN LATERAL (\n  SELECT MIN(NULLIF(dim.vaccine_code, '')) AS vaccine_code\n  FROM protocol_rule_dimensions dim\n  WHERE dim.tenant_id = pr.tenant_id\n    AND dim.rule_id = pr.rule_id\n) prd ON true");
  if (
    bad.length !== 6 ||
    good.length !== 0 ||
    badShedGrain.length !== 1 ||
    goodShedGrain.length !== 0 ||
    badParentSubmission.length !== 1 ||
    goodParentSubmission.length !== 0 ||
    badAcceptedSubmit.length !== 2 ||
    goodAcceptedSubmit.length !== 0 ||
    badVaccinationFanout.length !== 1 ||
    goodVaccinationFanout.length !== 0
  ) {
    console.error("aggregate-projection-guard self-test failed", { bad, good, badShedGrain, goodShedGrain, badParentSubmission, goodParentSubmission, badAcceptedSubmit, goodAcceptedSubmit, badVaccinationFanout, goodVaccinationFanout });
    process.exit(1);
  }
  console.log("aggregate-projection-guard self-test: PASS");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  const base = baseRef();
  const files = changedFiles(base);
  const untracked = new Set(zlist(git(["ls-files", "--others", "--exclude-standard", "-z"], true)));
  const sourceFiles = files.filter((file) =>
    /^(?:backend|tools)\/.*\.(?:go|sql)$/.test(file) &&
    !file.endsWith("_test.go") &&
    file !== "backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql" &&
    !/^backend\/internal\/[^/]+\/adapters\/postgres\/sqlc\/schema\.sql$/.test(file)
  );
  const testFiles = files.filter((file) => /(?:_test\.go|\.test\.[cm]?[jt]s|\.spec\.[cm]?[jt]s)$/.test(file));
  const sourceHunks = sourceFiles.flatMap((file) => hunks(diffFor(file, base, untracked)).filter(isCandidate).map((h) => ({ ...h, file })));
  const shedGrainFailures = files.includes(PROCESS_INTEGRITY_REPO)
    ? inspectProcessIntegrityShedGrain(readFileSync(resolve(repo, PROCESS_INTEGRITY_REPO), "utf8"))
    : [];
  const terminalSubmitFailures = files.includes(SOP_REPO)
    ? inspectShedSubmitTerminalState(readFileSync(resolve(repo, SOP_REPO), "utf8"))
    : [];
  const vaccinationFanoutFailures = files.includes(VACCINATION_EXECUTION_REPO)
    ? inspectVaccinationExecutionDimensionFanout(readFileSync(resolve(repo, VACCINATION_EXECUTION_REPO), "utf8"))
    : [];
  if (sourceHunks.length === 0) {
    if (shedGrainFailures.length || terminalSubmitFailures.length || vaccinationFanoutFailures.length) {
      console.error("aggregate-projection-guard: FAIL");
      for (const failure of shedGrainFailures) console.error(`  - ${failure}`);
      for (const failure of terminalSubmitFailures) console.error(`  - ${failure}`);
      for (const failure of vaccinationFanoutFailures) console.error(`  - ${failure}`);
      console.error("\nSee docs/decisions/scale-anti-patterns.md");
      process.exit(1);
    }
    console.log("aggregate-projection-guard: PASS (no changed aggregate projection hunks)");
    return;
  }
  // Only newly-added test lines count. Merely touching a test file that already
  // contained a conveniently named test must not satisfy this change's proof.
  const changedTests = testFiles.flatMap((file) =>
    hunks(diffFor(file, base, untracked)).map((hunk) => hunk.added),
  ).join("\n");
  const failures = inspectFixture(sourceHunks, changedTests);
  failures.push(...shedGrainFailures);
  failures.push(...terminalSubmitFailures);
  failures.push(...vaccinationFanoutFailures);
  if (failures.length) {
    console.error("aggregate-projection-guard: FAIL");
    for (const hunk of sourceHunks) console.error(`  candidate: ${hunk.file}`);
    for (const failure of failures) console.error(`  - ${failure}`);
    console.error("\nSee .agents/skills/goatos-code-review/references/aggregates-and-projections.md");
    process.exit(1);
  }
  console.log(`aggregate-projection-guard: PASS (${sourceHunks.length} reviewed aggregate hunk(s))`);
}

main();
