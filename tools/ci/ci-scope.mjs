#!/usr/bin/env node

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const rulesPath = path.join(here, "component-paths.json");

function normalized(filePath) {
  return String(filePath).replaceAll("\\", "/").replace(/^\.\//, "");
}

function matches(pathname, group = {}) {
  const filePath = normalized(pathname);
  return (group.files ?? []).includes(filePath)
    || (group.prefixes ?? []).some((prefix) => filePath.startsWith(prefix))
    || (group.extensions ?? []).some((extension) => filePath.endsWith(extension));
}

function matchesExcept(pathname, group = {}) {
  const filePath = normalized(pathname);
  if ((group.exceptFiles ?? []).includes(filePath)) return false;
  if ((group.exceptPrefixes ?? []).some((prefix) => filePath.startsWith(prefix))) return false;
  return matches(filePath, group);
}

function allComponents() {
  return { backend: true, adminWeb: true, android: true };
}

function docsOnlyPaths(paths, rules) {
  if (paths.length === 0) return false;
  const group = rules.docsOnly ?? {};
  return paths.every((filePath) => (
    filePath.endsWith(".md")
    && (
      (group.files ?? []).includes(filePath)
      || (group.prefixes ?? []).some((prefix) => filePath.startsWith(prefix))
    )
  ));
}

export function classifyPaths(inputPaths, rules = JSON.parse(readFileSync(rulesPath, "utf8"))) {
  const paths = [...new Set(inputPaths.map(normalized).filter(Boolean))].sort();
  const components = { backend: false, adminWeb: false, android: false };
  const reasons = [];

  if (paths.length === 0) {
    return {
      common: true,
      ...allComponents(),
      queryPlans: true,
      full: true,
      paths,
      reasons: ["no diff paths were available; conservative full suite"],
      selectedJobs: ["common", "backend", "query-plans", "admin-web", "android"],
    };
  }

  if (docsOnlyPaths(paths, rules)) {
    return {
      common: false,
      ...components,
      queryPlans: false,
      full: false,
      paths,
      reasons: paths.map((filePath) => `${filePath}: docs-only fast lane`),
      selectedJobs: ["docs-only"],
    };
  }

  for (const filePath of paths) {
    if (matches(filePath, rules.ciCommonOnly)) {
      reasons.push(`${filePath}: CI helper/tooling self-test coverage only`);
      continue;
    }

    if (matchesExcept(filePath, rules.forceFull)) {
      Object.assign(components, allComponents());
      reasons.push(`${filePath}: CI/shared tooling change forces full suite`);
      continue;
    }

    let matched = false;
    for (const rule of rules.fanout ?? []) {
      if (!matches(filePath, rule)) continue;
      for (const component of rule.components) components[component] = true;
      reasons.push(`${filePath}: ${rule.name} fans out to ${rule.components.join(", ")}`);
      matched = true;
    }
    for (const [component, group] of Object.entries(rules.components ?? {})) {
      if (!matches(filePath, group)) continue;
      components[component] = true;
      reasons.push(`${filePath}: ${component}`);
      matched = true;
    }
    if (matched || matches(filePath, rules.commonOnly)) continue;

    Object.assign(components, allComponents());
    reasons.push(`${filePath}: unmapped path forces full suite`);
  }

  const full = components.backend && components.adminWeb && components.android
    && reasons.some((reason) => reason.includes("forces full suite"));
  const selectedJobs = ["common"];
  if (components.backend) selectedJobs.push("backend");
  const queryPlans = paths.some((filePath) => matches(filePath, rules.queryPlans))
    || reasons.some((reason) => reason.includes("fans out to"));
  if (queryPlans) selectedJobs.push("query-plans");
  if (components.adminWeb) selectedJobs.push("admin-web");
  if (components.android) selectedJobs.push("android");

  return { common: true, ...components, queryPlans, full, paths, reasons, selectedJobs };
}

function resolveCommit(ref) {
  return execFileSync("git", ["rev-parse", "--verify", `${ref}^{commit}`], { encoding: "utf8" }).trim();
}

export function classifyGitDiff(base, head = "HEAD") {
  if (!base || /^0+$/.test(base)) {
    return { ...classifyPaths([]), base: base || "", head, reasons: ["missing/zero base revision; conservative full suite"] };
  }
  try {
    const resolvedBase = resolveCommit(base);
    const resolvedHead = resolveCommit(head);
    const output = execFileSync(
      "git",
      ["diff", "--name-only", "--diff-filter=ACMRD", "-z", `${resolvedBase}...${resolvedHead}`],
      { encoding: "utf8" },
    );
    return { ...classifyPaths(output.split("\0").filter(Boolean)), base: resolvedBase, head: resolvedHead };
  } catch (error) {
    return {
      ...classifyPaths([]),
      base,
      head,
      reasons: [`could not resolve diff (${error.message}); conservative full suite`],
    };
  }
}

export function verifyRequiredResults(expected, results) {
  const failures = [];
  if (results.changes !== "success") failures.push(`changes=${results.changes || "missing"}`);
  for (const [name, required] of Object.entries(expected)) {
    const result = results[name] || "missing";
    const isRequired = required === true || required === "true";
    if (isRequired && result !== "success") failures.push(`${name} was required but result=${result}`);
    if (!isRequired && result !== "skipped") failures.push(`${name} was not required but result=${result}`);
  }
  return { ok: failures.length === 0, failures };
}

function argValue(name, fallback = undefined) {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

function printClassification(result, format) {
  if (format === "github") {
    console.log(`common=${result.common}`);
    console.log(`backend=${result.backend}`);
    console.log(`query_plans=${result.queryPlans}`);
    console.log(`admin_web=${result.adminWeb}`);
    console.log(`android=${result.android}`);
    console.log(`full=${result.full}`);
    console.log(`base=${result.base ?? ""}`);
    console.log(`head=${result.head ?? ""}`);
    console.log(`selected_jobs=${result.selectedJobs.join(",")}`);
    return;
  }
  console.log(JSON.stringify(result, null, 2));
}

function selfTest() {
  const pick = (paths) => {
    const { common, backend, adminWeb, android, full, selectedJobs } = classifyPaths(paths);
    return { common, backend, adminWeb, android, full, selectedJobs };
  };
  assert.deepEqual(pick(["backend/internal/api.go"]), {
    common: true, backend: true, adminWeb: false, android: false, full: false,
    selectedJobs: ["common", "backend"],
  });
  assert.deepEqual(pick(["backend/internal/adminui/app/service.go"]), {
    common: true, backend: true, adminWeb: true, android: false, full: false,
    selectedJobs: ["common", "backend", "admin-web"],
  });
  const obligationQueryChange = classifyPaths([
    "backend/internal/obligation/adapters/postgres/repository.go",
  ]);
  assert.equal(obligationQueryChange.queryPlans, true,
    "a production obligation-query change must schedule the PostgreSQL plan gate");
  assert.equal(obligationQueryChange.selectedJobs.includes("query-plans"), true,
    "normal PR/push CI must not leave the query-plan gate manual");
  // The command board's statements live in vaccinationexecution, NOT vaccination, and that one
  // missing prefix meant a PR touching only those statements skipped the query-plans job entirely
  // -- and ci-required then VERIFIED the skip as expected and went green. The endpoint whose plans
  // this gate exists to protect is exactly the one that returned 500 in staging, so a
  // command-board-only change must schedule the gate.
  const commandBoardQueryChange = classifyPaths([
    "backend/internal/vaccinationexecution/adapters/postgres/commandboard_sql.go",
  ]);
  assert.equal(commandBoardQueryChange.queryPlans, true,
    "a command-board query change must schedule the PostgreSQL plan gate");
  assert.equal(commandBoardQueryChange.selectedJobs.includes("query-plans"), true,
    "a command-board-only PR must not skip the query-plan gate");
  assert.deepEqual(pick(["apps/admin-web/app/page.tsx"]), {
    common: true, backend: false, adminWeb: true, android: false, full: false,
    selectedJobs: ["common", "admin-web"],
  });
  assert.deepEqual(pick(["apps/goatos-android/app/build.gradle.kts"]), {
    common: true, backend: false, adminWeb: false, android: true, full: false,
    selectedJobs: ["common", "android"],
  });
  // The dashboard guard is a local-CI script run by `common`; editing it must not rebuild the apps.
  assert.deepEqual(pick(["tools/agent-hooks/check-dashboard-automation-guard.mjs"]).selectedJobs, ["common"]);
  // Ask Mesha agent code is a Node service with its own unit tests in the common job; it must
  // not rebuild Android or rerun Go (was: unmapped -> full suite, ~30 min per agent change).
  assert.deepEqual(pick(["tools/ask-mesha-agent/server.mjs", "tools/ask-mesha-agent/eval/golden.json"]).selectedJobs, ["common"]);
  assert.deepEqual(pick(["tools/ci/land-main.sh"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  assert.deepEqual(pick(["tools/agent-hooks/postgres-bind-contract-baseline.json"]), {
    common: true, backend: true, adminWeb: false, android: false, full: false,
    selectedJobs: ["common", "backend"],
  });
  assert.equal(pick(["tools/agent-hooks/check-postgres-bind-contract.mjs"]).full, true);
  assert.deepEqual(pick(["tools/dashboard-automation/coverage-state.json"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  // The dashboard guard FORCES a commit-ledger edit on every landing, so an
  // unmapped ledger file made every landing rebuild Android and admin-web. These
  // rows name shas; they carry no product code.
  for (const ledger of [
    "tools/dashboard-automation/commit-classification/lane3.jsonl",
    "tools/dashboard-automation/commit-classification/lane5-android.jsonl",
    "tools/dashboard-automation/commit-classification/not-automatable.jsonl",
    "tools/dashboard-automation/commit-classification/web-A.jsonl",
  ]) {
    assert.deepEqual(pick([ledger]), {
      common: true, backend: false, adminWeb: false, android: false, full: false,
      selectedJobs: ["common"],
    }, `${ledger} must stay ledger-only`);
  }
  // A ledger edit beside real backend work must not drag the app builds in either.
  assert.deepEqual(pick([
    "backend/migrations/postgres/000394_retire_operator_onto_manager_roles.sql",
    "tools/dashboard-automation/commit-classification/lane4.jsonl",
  ]), {
    common: true, backend: true, adminWeb: false, android: false, full: false,
    selectedJobs: ["common", "backend", "query-plans"],
  });
  // ... but a genuinely unmapped path must STILL force the full suite: this
  // mapping narrows one known directory, it does not weaken the fallback.
  assert.equal(pick(["tools/dashboard-automation/lane-checks.json"]).full, true);
  // The ledger mapping is directory-shaped but its justification is content-shaped:
  // every file there is an append-only row set naming shas. Nothing enforced that,
  // so a generator script dropped into that directory would have inherited
  // common-only scoping silently. Pin the invariant the mapping actually relies on.
  {
    const ledgerDir = "tools/dashboard-automation/commit-classification";
    const tracked = execFileSync("git", ["ls-files", ledgerDir], { encoding: "utf8" })
      .split("\n").filter(Boolean);
    assert.ok(tracked.length > 0, `${ledgerDir} must be tracked`);
    const offenders = tracked.filter((f) => !f.endsWith(".jsonl"));
    assert.deepEqual(offenders, [],
      `${ledgerDir} is scoped common-only because it holds ONLY .jsonl commit ledgers; `
      + `${offenders.join(", ")} is not one, so either move it or narrow ciCommonOnly`);
  }
  assert.deepEqual(pick(["cloudbuild.stg.yaml"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  assert.deepEqual(pick(["tools/deploy/stg-clouddeploy-release.sh"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  assert.deepEqual(pick(["backend/internal/permissions/routes.go"]), {
    common: true, backend: true, adminWeb: false, android: false, full: false,
    selectedJobs: ["common", "backend"],
  });
  // Android DTOs are hand-mapped; no Android build or test reads contracts/ or
  // packages/api-client/, so a contract edit cannot fail the Android job. Android
  // still runs whenever its own DTOs/code change.
  assert.deepEqual(pick(["contracts/openapi/app-api.yaml"]), {
    common: true, backend: true, adminWeb: true, android: false, full: false,
    selectedJobs: ["common", "backend", "query-plans", "admin-web"],
  });
  assert.deepEqual(pick(["apps/admin-web/features/verification-review/verification-review-page.tsx"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "query-plans", "admin-web", "android"],
  });
  assert.deepEqual(pick(["apps/goatos-android/feature/feature-verify/src/main/kotlin/sg/mesha/goatos/feature/verify/VerifyQueueScreen.kt"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "query-plans", "admin-web", "android"],
  });
  assert.deepEqual(pick(["backend/internal/verification/adapters/proofmedia/resolver.go"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "query-plans", "admin-web", "android"],
  });
  assert.deepEqual(pick(["docs/progress/local-ci.md"]), {
    common: false, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["docs-only"],
  });
  assert.deepEqual(pick(["docs/preventive-care-vaccination/vaccination-rules.md"]), {
    common: false, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["docs-only"],
  });
  assert.deepEqual(pick(["docs/strategy/livestock-backed-exchange.md", "docs/progress/digital-goat-exchange-strategy-20260918.md"]), {
    common: false, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["docs-only"],
  });
  assert.deepEqual(pick(["AGENTS.md"]), {
    common: false, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["docs-only"],
  });
  assert.deepEqual(pick([".env.ceo-ai.local.example"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  assert.deepEqual(pick(["apps/goatos-android/local.properties.example"]), {
    common: true, backend: false, adminWeb: false, android: true, full: false,
    selectedJobs: ["common", "android"],
  });
  assert.deepEqual(pick(["fixtures/vaccination-cpt-operator-drive-2026-07-23/expected-drive-schedules.json"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  assert.equal(pick([".github/workflows/ci.yml"]).full, true);
  assert.equal(pick(["unknown-runtime/file.xyz"]).full, true);
  assert.equal(verifyRequiredResults(
    { common: true, backend: true, "admin-web": false, android: false, "live-api-latency": true },
    { changes: "success", common: "success", backend: "success", "admin-web": "skipped", android: "skipped", "live-api-latency": "success" },
  ).ok, true);
  assert.equal(verifyRequiredResults(
    { common: true, backend: true },
    { changes: "success", common: "success", backend: "skipped" },
  ).ok, false);
  console.log("ci-scope: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else if (process.argv.includes("--verify-results")) {
  const expected = JSON.parse(process.env.CI_EXPECTED_RESULTS || "{}");
  const results = JSON.parse(process.env.CI_JOB_RESULTS || "{}");
  const verdict = verifyRequiredResults(expected, results);
  if (!verdict.ok) {
    for (const failure of verdict.failures) console.error(`ci-required: ${failure}`);
    process.exit(1);
  }
  console.log("ci-required: all required component jobs passed and unrelated jobs were skipped");
} else {
  const result = classifyGitDiff(argValue("--base"), argValue("--head", "HEAD"));
  printClassification(result, argValue("--format", "json"));
}
