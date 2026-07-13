#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const paths = {
  adr: "docs/decisions/one-million-postgres-readiness.md",
  plan: "docs/protocol-engine/high-scale-kernel-validation-plan.md",
  rehearsal: "context/execution/gcp-disposable-1m-scale-test-handoff-2026-07-13.md",
};

function compact(value) {
  return value.replace(/\s+/g, " ").trim();
}

export function findingsForDocs(input) {
  const adr = compact(input.adr);
  const plan = compact(input.plan);
  const rehearsal = compact(input.rehearsal);
  const findings = [];

  const require = (condition, message) => {
    if (!condition) findings.push(message);
  };

  require(
    /\| P0 \| Run one-million certification on the committed `goatos-stg-1m-benchmark-v1` Cloud SQL profile/.test(adr),
    "ADR P0 must require the committed goatos-stg Cloud SQL benchmark profile"
  );
  require(
    /defines a `goatos-dev` VM rehearsal and cleanup workflow only/.test(adr),
    "ADR must label the disposable goatos-dev workflow as rehearsal-only"
  );
  require(
    !/Run the disposable GCP one-million certification/.test(adr),
    "ADR must not let the disposable VM run close certification"
  );

  require(
    /disposable `goatos-dev` Compute Engine workflow .* is a rehearsal environment/.test(plan),
    "validation plan must identify the disposable goatos-dev VM as rehearsal"
  );
  require(
    /cannot satisfy this staging certification floor/.test(plan),
    "validation plan must reject the VM verdict as staging certification"
  );

  require(
    /^# Disposable GCP 1M Scale Rehearsal Handoff/.test(input.rehearsal),
    "disposable VM handoff title must say rehearsal"
  );
  require(
    /purpose=scale-rehearsal/.test(rehearsal),
    "disposable VM resources must use the scale-rehearsal purpose label"
  );
  require(
    /visibly labeled `non_certifying`/.test(rehearsal),
    "VM report must carry a non_certifying label"
  );
  require(
    /`PASSED` means the VM rehearsal passed/.test(rehearsal),
    "VM report must define PASSED as a rehearsal verdict"
  );
  require(
    /separate certifying run uses the committed `goatos-stg-1m-benchmark-v1` Cloud SQL profile/.test(rehearsal),
    "VM handoff must point certification to the committed staging Cloud SQL profile"
  );

  for (const forbidden of [
    /Use an ordinary on-demand VM for certification/,
    /purpose=scale-certification/,
    /- certification boundary \(`projection_reads` or `full_kernel`\)/,
  ]) {
    require(!forbidden.test(rehearsal), `disposable VM handoff contains forbidden certification claim: ${forbidden}`);
  }

  return findings;
}

function selfTest() {
  const good = {
    adr: "| P0 | Run one-million certification on the committed `goatos-stg-1m-benchmark-v1` Cloud SQL profile against an exact commit. | evidence | The handoff defines a `goatos-dev` VM rehearsal and cleanup workflow only.",
    plan: "The disposable `goatos-dev` Compute Engine workflow in the handoff is a rehearsal environment. Its verdict cannot satisfy this staging certification floor.",
    rehearsal: "# Disposable GCP 1M Scale Rehearsal Handoff\nThe separate certifying run uses the committed `goatos-stg-1m-benchmark-v1` Cloud SQL profile.\npurpose=scale-rehearsal\nvisibly labeled `non_certifying`\n`PASSED` means the VM rehearsal passed.",
  };
  if (findingsForDocs(good).length !== 0) {
    throw new Error(`compliant fixture failed: ${findingsForDocs(good).join("; ")}`);
  }

  const badAdr = { ...good, adr: good.adr.replace("Run one-million certification on the committed `goatos-stg-1m-benchmark-v1` Cloud SQL profile", "Run the disposable GCP one-million certification") };
  const badPlan = { ...good, plan: good.plan.replace("cannot satisfy this staging certification floor", "is useful evidence") };
  const badHandoff = { ...good, rehearsal: good.rehearsal.replace("purpose=scale-rehearsal", "purpose=scale-certification") };
  for (const fixture of [badAdr, badPlan, badHandoff]) {
    if (findingsForDocs(fixture).length === 0) throw new Error("non-compliant fixture passed");
  }
  console.log("scale-certification-docs-guard self-test: passed");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  const input = Object.fromEntries(
    Object.entries(paths).map(([key, relative]) => [key, readFileSync(resolve(repo, relative), "utf8")])
  );
  const findings = findingsForDocs(input);
  if (findings.length > 0) {
    for (const finding of findings) console.error(`scale-certification-docs-guard: ${finding}`);
    process.exit(1);
  }
  console.log("scale-certification-docs-guard: passed");
}

main();
