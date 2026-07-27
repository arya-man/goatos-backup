#!/usr/bin/env node

// Vaccination seed/generation invariant:
// - every accepted live goat must resolve to a real shed during seed/import;
// - goat vaccination obligations are shed-scoped only;
// - park/tenant fallback scopes are forbidden for goat vaccination obligations.
// - vaccination drive batches are park-scoped only, even when produced from shed obligations.
// - shed proof submit writes must never materialize all shared parent-task scan items.

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const files = {
  seed: "backend/cmd/seed-vaccination-real/main.go",
  generation: "backend/internal/vaccination/app/generation.go",
  sweeper: "backend/internal/obligation/app/sweeper.go",
  closeout: "tools/dev/seed-closeout.sh",
  proof: "tools/dev/check-goat-shed-integrity.sh",
  driveProof: "tools/dev/check-vaccination-drive-clubbing-proof.sh",
  sopRepo: "backend/internal/sop/adapters/postgres/repository.go",
  sopSubmitTest: "backend/internal/sop/adapters/postgres/shed_submit_state_integration_test.go",
};

function problemIfMissing(text, pattern, message) {
  return pattern.test(text) ? [] : [message];
}

function problemIfPresent(text, pattern, message) {
  return pattern.test(text) ? [message] : [];
}

export function validate(readText, present = () => true) {
  const problems = [];
  for (const [label, path] of Object.entries(files)) {
    if (!present(path)) problems.push(`${label}: missing required file ${path}`);
  }
  if (problems.length > 0) return problems;

  const seed = readText(files.seed);
  const generation = readText(files.generation);
  const sweeper = readText(files.sweeper);
  const closeout = readText(files.closeout);
  const proof = readText(files.proof);
  const driveProof = readText(files.driveProof);
  const sopRepo = readText(files.sopRepo);
  const sopSubmitTest = readText(files.sopSubmitTest);

  problems.push(
    ...problemIfMissing(seed, /seedFallbackFarm/, `${files.seed}: missing deterministic fallback farm for incomplete source placement`),
    ...problemIfMissing(seed, /seedFallbackShed/, `${files.seed}: missing deterministic fallback shed for incomplete source placement`),
    ...problemIfMissing(seed, /verifyActiveGoatsHaveShed/, `${files.seed}: seed must prove active goats have real shed placement`),
    ...problemIfMissing(seed, /verifyVaccinationObligationsShedScoped/, `${files.seed}: seed must prove vaccination obligations are shed-scoped`),
    ...problemIfMissing(seed, /func\s+seedObligationScope\s*\(\s*shedID\s+string\s*\)/, `${files.seed}: seedObligationScope must accept only shedID`),
    ...problemIfMissing(seed, /return\s+"shed"\s*,\s*shedID\s*,\s*nil/, `${files.seed}: seedObligationScope must return shed scope from shedID`),
    ...problemIfPresent(seed, /No shed -> skip/i, `${files.seed}: seed must not skip accepted goats without source shed; fill deterministic placement`),
    ...problemIfPresent(seed, /return\s+"park"\s*,/, `${files.seed}: seedObligationScope must not fall back to park scope`),
    ...problemIfPresent(seed, /return\s+"tenant"\s*,/, `${files.seed}: seedObligationScope must not fall back to tenant scope`),
  );

  problems.push(
    ...problemIfMissing(generation, /missing required park\/shed placement/, `${files.generation}: generation must fail closed when placement is missing`),
    ...problemIfMissing(generation, /return\s+"shed"\s*,\s*g\.ShedID\s*,\s*nil/, `${files.generation}: generationScope must create shed-scoped obligations only`),
    ...problemIfPresent(generation, /return\s+"park"\s*,/, `${files.generation}: generationScope must not create park-scoped goat obligations`),
    ...problemIfPresent(generation, /return\s+"tenant"\s*,/, `${files.generation}: generationScope must not create tenant-scoped goat obligations`),
  );

  problems.push(
    ...problemIfMissing(sweeper, /vaccinationDriveBatchScope/, `${files.sweeper}: sweeper must convert shed-scoped goat obligations into park-scoped vaccination drive batches`),
    ...problemIfMissing(sweeper, /return\s+"park"\s*,\s*parkID\s*,\s*nil/, `${files.sweeper}: vaccination drive batches must be park-scoped`),
    ...problemIfMissing(sweeper, /missing park_id/, `${files.sweeper}: fallback batching must fail closed when a shed obligation has no park_id`),
  );

  problems.push(
    ...problemIfMissing(closeout, /check-goat-shed-integrity\.sh/, `${files.closeout}: seed closeout must run goat-shed-integrity proof`),
    ...problemIfMissing(proof, /active_goat_shed_invariant/, `${files.proof}: DB proof missing active goat shed invariant`),
    ...problemIfMissing(proof, /vaccination_obligation_shed_scope_invariant/, `${files.proof}: DB proof missing obligation shed-scope invariant`),
    ...problemIfMissing(driveProof, /ob\.scope_type <> 'park'/, `${files.driveProof}: DB proof must reject non-park vaccination drive batches`),
  );

  problems.push(
    ...problemIfMissing(sopRepo, /filterSubmissionItemsToProofSheds/, `${files.sopRepo}: shed-level proof submit must filter over-broad parent-task scan items by proof shed before inserting sop_submission_items`),
    ...problemIfMissing(sopRepo, /shedProofSubjectIDs/, `${files.sopRepo}: shed-level proof submit must extract shed subject ids from proof_refs instead of trusting parent task state`),
    ...problemIfMissing(sopRepo, /FROM\s+goats[\s\S]{0,500}goat_id\s*=\s*ANY\(\$2::uuid\[\]\)[\s\S]{0,200}shed_id\s*=\s*ANY\(\$3::uuid\[\]\)/, `${files.sopRepo}: shed-level proof submit filter must constrain candidate goat_id list and keep only proof shed_ids`),
    ...problemIfMissing(sopSubmitTest, /TestSubmitTaskShedProofFiltersOverBroadScanItemsToThatShed/, `${files.sopSubmitTest}: missing regression where a shed proof submit receives scan items from two sheds and writes only the proof shed`),
    ...problemIfMissing(sopSubmitTest, /shedOneItems\s*!=\s*1\s*\|\|\s*shedTwoItems\s*!=\s*0/, `${files.sopSubmitTest}: shed proof regression must assert sibling shed items stay zero`),
    ...problemIfMissing(sopSubmitTest, /TestSubmitTaskAllowsSecondShedSubmitWhenSharedParkTaskAlreadyNeedsReview/, `${files.sopSubmitTest}: missing regression that sibling shed submit remains legal while shared parent task is needs_review`),
    ...problemIfMissing(sopSubmitTest, /TestSubmitTaskRejectsFreshSubmitWhenSharedParkTaskAccepted/, `${files.sopSubmitTest}: missing regression that accepted shared parent is terminal for fresh submit keys`),
  );

  return problems;
}

function selfTest() {
  const good = {
    [files.seed]: `
const seedFallbackFarm = "SEED_INTAKE"
const seedFallbackShed = "Seed Intake Shed"
func seedObligationScope(shedID string) (scopeType, scopeID string, err error) {
  return "shed", shedID, nil
}
func verifyActiveGoatsHaveShed() {}
func verifyVaccinationObligationsShedScoped() {}
`,
    [files.generation]: `
func generationScope(_ string, g domain.EligibleGoat) (string,string,error) {
  return "", "", fmt.Errorf("missing required park/shed placement")
  return "shed", g.ShedID, nil
}
`,
    [files.sweeper]: `
func vaccinationDriveBatchScope() (string,string,error) {
  if parkID == "" { return "", "", fmt.Errorf("missing park_id") }
  return "park", parkID, nil
}
`,
    [files.closeout]: `bash tools/dev/check-goat-shed-integrity.sh`,
    [files.proof]: `active_goat_shed_invariant vaccination_obligation_shed_scope_invariant`,
    [files.driveProof]: `AND ob.scope_type <> 'park'`,
    [files.sopRepo]: `
func insertSubmissionItems() {
  keys, err = filterSubmissionItemsToProofSheds(ctx, tx, cmd.TenantID, cmd.Body.ProofRefs, keys)
}
func filterSubmissionItemsToProofSheds() {
  shedSubjectIDs := shedProofSubjectIDs(refs)
  rows, err := tx.Query(ctx, "SELECT goat_id::text FROM goats WHERE tenant_id = $1::uuid AND goat_id = ANY($2::uuid[]) AND shed_id = ANY($3::uuid[])", tenantID, goatIDs, shedSubjectIDs)
}
func shedProofSubjectIDs() {}
`,
    [files.sopSubmitTest]: `
func TestSubmitTaskShedProofFiltersOverBroadScanItemsToThatShed(t *testing.T) {
  if shedOneItems != 1 || shedTwoItems != 0 { t.Fatal() }
}
func TestSubmitTaskAllowsSecondShedSubmitWhenSharedParkTaskAlreadyNeedsReview(t *testing.T) {}
func TestSubmitTaskRejectsFreshSubmitWhenSharedParkTaskAccepted(t *testing.T) {}
`,
  };
  const clean = validate((path) => good[path]);
  if (clean.length !== 0) throw new Error(`self-test: expected clean fixture, got ${JSON.stringify(clean)}`);

  const bad = { ...good };
  bad[files.seed] = `
func seedObligationScope(parkID string) (scopeType, scopeID string, err error) {
  return "park", parkID, nil
}
`;
  bad[files.generation] = `func generationScope() { return "tenant", "x", nil }`;
  bad[files.closeout] = `echo no proof`;
  bad[files.sopRepo] = `func insertSubmissionItems() { /* inserts all parent scan items */ }`;
  bad[files.sopSubmitTest] = `func TestSubmitTaskAllowsSecondShedSubmitWhenSharedParkTaskAlreadyNeedsReview(t *testing.T) {}`;
  const findings = validate((path) => bad[path]);
  for (const expected of [
    "missing deterministic fallback farm",
    "must not fall back to park scope",
    "must not create tenant-scoped",
    "must run goat-shed-integrity proof",
    "must filter over-broad parent-task scan items",
    "missing regression where a shed proof submit receives scan items from two sheds",
  ]) {
    if (!findings.some((f) => f.includes(expected))) {
      throw new Error(`self-test: missing expected finding ${expected}; got ${JSON.stringify(findings)}`);
    }
  }
  console.log("goat-shed-scope guard: self-test passed");
}

function run() {
  const problems = validate(
    (path) => readFileSync(resolve(repo, path), "utf8"),
    (path) => existsSync(resolve(repo, path)),
  );
  if (problems.length > 0) {
    console.error("goat-shed-scope guard failed:");
    for (const problem of problems) console.error(`- ${problem}`);
    process.exit(1);
  }
  console.log("goat-shed-scope guard: ok");
}

if (process.argv.includes("--self-test")) selfTest();
else run();
