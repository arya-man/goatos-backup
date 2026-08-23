#!/usr/bin/env node
// additive-publish-guard — a publish only touches the rules it changed.
//
// Lens CD-ADDITIVE-PUBLISH. A plan version holds every vaccine, so publishing is routine:
// adding a 6th vaccine to a plan of 5 must leave those 5 operationally untouched. The mechanism
// is rule lineage (identity_key + content_fingerprint) plus carry-over running BEFORE supersede.
//
// Three things silently break that guarantee, and none of them look like a bug in review:
//   1. the carry-over call disappearing from the supersede path (back to cancel-and-re-mint)
//   2. due_at or status joining the carry-over UPDATE (a "harmless" refresh that moves work)
//   3. a scheduling field dropping out of the fingerprint (an edit that reads as unchanged)
//
// Structural, not diff-scoped: these are invariants of the files themselves, cheap to check in
// full every run, and a diff-scoped version would miss a deletion made in an unrelated commit.

import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

const GENERATION = "backend/internal/vaccination/app/generation.go";
const OBLIGATION_REPO = "backend/internal/obligation/adapters/postgres/repository.go";
const LINEAGE = "backend/internal/protocol/domain/rule_lineage.go";
const DOC = "docs/preventive-care-vaccination/additive-publish.md";

const CARRY_OVER = "CarryOverUnchangedVaccinationObligations";
const SUPERSEDE_SWEEP = "GoatsWithVaccinationObligationsOutsideVersions";

// Every field that can change WHAT an animal owes or WHEN. Dropping one from the fingerprint
// makes an edit to it look like no edit at all, so the rule carries over and the change never
// reaches a single animal.
const FINGERPRINT_FIELDS = [
  "trigger_type",
  "offset_days",
  "due_window_days",
  "min_gap_days",
  "repeat",
  "repeat_until_after_age",
  "catch_up",
  "sop_version_id",
  "withdrawal_days",
  "eligibility_json",
  "proof_policy",
];

// Columns that must never appear in the carry-over UPDATE's SET clause. Carrying over means the
// animal owes the same thing on the same day; touching either of these is a reschedule wearing
// carry-over's clothes.
const FORBIDDEN_IN_SET = ["due_at", "status"];

function read(rel) {
  const path = resolve(repoRoot, rel);
  if (!existsSync(path)) return null;
  return readFileSync(path, "utf8");
}

// The SET clause of the carry-over UPDATE: from "SET" to the next clause keyword.
export function carryOverSetClause(source) {
  const fnStart = source.indexOf(`func (r *Repository) ${CARRY_OVER}(`);
  if (fnStart < 0) return null;
  const updateAt = source.indexOf("UPDATE obligation_instances", fnStart);
  if (updateAt < 0) return null;
  const setAt = source.indexOf("SET ", updateAt);
  if (setAt < 0) return null;
  const endAt = source.indexOf("FROM ", setAt);
  return source.slice(setAt, endAt < 0 ? setAt + 400 : endAt);
}

// Carry-over has to be invoked, and invoked before the supersede sweep, inside the same function.
export function carryOverPrecedesSupersede(source) {
  const fnStart = source.indexOf("func (s *GenerationService) supersedeRetiredPlanWork(");
  if (fnStart < 0) return { ok: false, reason: "supersedeRetiredPlanWork not found" };
  const fnEnd = source.indexOf("\nfunc ", fnStart + 1);
  const body = source.slice(fnStart, fnEnd < 0 ? source.length : fnEnd);
  const carry = body.indexOf(CARRY_OVER);
  const sweep = body.indexOf(SUPERSEDE_SWEEP);
  if (carry < 0) {
    return { ok: false, reason: `${CARRY_OVER} is never called: every publish cancels and re-mints the whole plan again` };
  }
  if (sweep < 0) return { ok: false, reason: `${SUPERSEDE_SWEEP} not found` };
  if (carry > sweep) {
    return { ok: false, reason: "carry-over runs AFTER the supersede sweep: unchanged work is cancelled before anything can rebind it" };
  }
  return { ok: true };
}

export function missingFingerprintFields(source) {
  const fnStart = source.indexOf("func RuleContentFingerprint(");
  if (fnStart < 0) return FINGERPRINT_FIELDS.slice();
  const fnEnd = source.indexOf("\nfunc ", fnStart + 1);
  const body = source.slice(fnStart, fnEnd < 0 ? source.length : fnEnd);
  return FINGERPRINT_FIELDS.filter((field) => !body.includes(`"${field}=`));
}

function selfTest() {
  const failures = [];
  const ok = `func (s *GenerationService) supersedeRetiredPlanWork(ctx) {
    s.obl.CarryOverUnchangedVaccinationObligations(ctx)
    s.obl.GoatsWithVaccinationObligationsOutsideVersions(ctx)
  }
func next() {}`;
  if (!carryOverPrecedesSupersede(ok).ok) failures.push("self-test: correct order rejected");

  const reversed = `func (s *GenerationService) supersedeRetiredPlanWork(ctx) {
    s.obl.GoatsWithVaccinationObligationsOutsideVersions(ctx)
    s.obl.CarryOverUnchangedVaccinationObligations(ctx)
  }
func next() {}`;
  if (carryOverPrecedesSupersede(reversed).ok) failures.push("self-test: reversed order accepted");

  const dropped = `func (s *GenerationService) supersedeRetiredPlanWork(ctx) {
    s.obl.GoatsWithVaccinationObligationsOutsideVersions(ctx)
  }
func next() {}`;
  if (carryOverPrecedesSupersede(dropped).ok) failures.push("self-test: missing carry-over accepted");

  const setSource = `func (r *Repository) CarryOverUnchangedVaccinationObligations(ctx) {
  r.pool.Exec(ctx, \`UPDATE obligation_instances oi
SET protocol_version_id = er.protocol_version_id,
    due_at = now()
FROM protocol_rules retired\`)
}`;
  const clause = carryOverSetClause(setSource);
  if (!clause || !clause.includes("due_at")) failures.push("self-test: SET clause extraction missed due_at");

  if (missingFingerprintFields("func RuleContentFingerprint(in NewRule) string {}").length !== FINGERPRINT_FIELDS.length) {
    failures.push("self-test: empty fingerprint body reported as complete");
  }

  if (failures.length) {
    for (const f of failures) console.error(`  ${f}`);
    console.error("check-additive-publish self-test: FAIL");
    process.exit(1);
  }
  console.log("check-additive-publish self-test: PASS (pure-function checks only -- run without --self-test to check the tree)");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();

  const findings = [];

  const generation = read(GENERATION);
  if (!generation) findings.push(`${GENERATION}: missing`);
  else {
    const order = carryOverPrecedesSupersede(generation);
    if (!order.ok) findings.push(`${GENERATION}: ${order.reason}`);
  }

  const repo = read(OBLIGATION_REPO);
  if (!repo) findings.push(`${OBLIGATION_REPO}: missing`);
  else {
    const clause = carryOverSetClause(repo);
    if (clause === null) {
      findings.push(`${OBLIGATION_REPO}: ${CARRY_OVER} has no UPDATE ... SET to check`);
    } else {
      for (const column of FORBIDDEN_IN_SET) {
        if (new RegExp(`(^|[\\s,])${column}\\s*=`, "m").test(clause)) {
          findings.push(
            `${OBLIGATION_REPO}: carry-over SET assigns ${column}. Carrying over means the animal owes the same thing on the same day -- if ${column} must move, the rule content changed and the cancel-and-regenerate path is the correct one.`,
          );
        }
      }
    }
  }

  const lineage = read(LINEAGE);
  if (!lineage) findings.push(`${LINEAGE}: missing`);
  else {
    const missing = missingFingerprintFields(lineage);
    if (missing.length) {
      findings.push(
        `${LINEAGE}: RuleContentFingerprint no longer covers ${missing.join(", ")}. Editing one of those would read as "unchanged", carry over, and never reach an animal.`,
      );
    }
  }

  if (!read(DOC)) findings.push(`${DOC}: missing -- the lens has no written invariants to review against`);

  if (findings.length) {
    console.error("additive-publish-guard: FAIL");
    for (const f of findings) console.error(`  - ${f}`);
    console.error("\nSee docs/preventive-care-vaccination/additive-publish.md and CD-ADDITIVE-PUBLISH in the review-lens ledger.");
    process.exit(1);
  }
  console.log("additive-publish-guard: ok (carry-over precedes supersede, SET touches neither due_at nor status, fingerprint covers every scheduling field).");
}

main();
