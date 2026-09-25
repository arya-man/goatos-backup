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
    || (group.extensions ?? []).some((extension) => filePath.endsWith(extension))
    // `dirs` match files directly inside a directory (one Go package), never its subtree.
    || (group.dirs ?? []).includes(filePath.slice(0, filePath.lastIndexOf("/") + 1));
}

function matchesExcept(pathname, group = {}) {
  const filePath = normalized(pathname);
  if ((group.exceptFiles ?? []).includes(filePath)) return false;
  if ((group.exceptPrefixes ?? []).some((prefix) => filePath.startsWith(prefix))) return false;
  return matches(filePath, group);
}

// The query-plans job is two independent DB gates. Each one is scheduled only when the diff
// touches an input that can change its verdict (rules.queryPlans.<step>); a full/unmapped run
// schedules both. See component-paths.json `queryPlans` for what each gate actually reads.
export const QUERY_PLAN_STEPS = ["sqlc", "commandboard"];

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
      queryPlanSteps: [...QUERY_PLAN_STEPS],
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
      queryPlanSteps: [],
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
  const queryPlanSteps = QUERY_PLAN_STEPS.filter((step) => (
    full || paths.some((filePath) => matches(filePath, rules.queryPlans?.[step]))
  ));
  for (const step of queryPlanSteps) reasons.push(`query-plans: ${step} gate inputs changed`);
  const queryPlans = queryPlanSteps.length > 0;
  if (queryPlans) selectedJobs.push("query-plans");
  if (components.adminWeb) selectedJobs.push("admin-web");
  if (components.android) selectedJobs.push("android");

  return { common: true, ...components, queryPlans, queryPlanSteps, full, paths, reasons, selectedJobs };
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
    console.log(`query_plan_steps=${(result.queryPlanSteps ?? []).join(",")}`);
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
  // PR #419 shape: an obligation adapter Go change. Neither plan gate reads that package
  // (validate-sqlc-query-plans.sh EXPLAINs inline SQL over migrations; the command-board gate's
  // test closure does not import obligation), so neither can change verdict -- no DB job.
  const obligationGoChange = classifyPaths([
    "backend/internal/obligation/adapters/postgres/assigned_batch_tasks.go",
    "backend/internal/obligation/adapters/postgres/assigned_batch_tasks_test.go",
  ]);
  assert.deepEqual(obligationGoChange.selectedJobs, ["common", "backend"],
    "an obligation Go-only change must not pay for the query-plan DB gates");
  assert.deepEqual(obligationGoChange.queryPlanSteps, []);
  const qp = (paths) => classifyPaths(paths).queryPlanSteps;
  assert.deepEqual(qp(["backend/migrations/postgres/000500_x.sql"]), ["sqlc", "commandboard"],
    "a migration changes both gates' database");
  assert.deepEqual(qp(["backend/internal/obligation/adapters/postgres/sqlc/queries.sql"]), ["sqlc"],
    "a sqlc query/schema edit schedules the sqlc plan gate");
  assert.deepEqual(qp(["backend/tests/integration/validate-sqlc-query-plans.sh"]), ["sqlc"]);
  // tools/postgres-ci.sh is unmapped shared tooling, so it forces the full suite and both gates.
  assert.deepEqual(qp(["tools/postgres-ci.sh"]), ["sqlc", "commandboard"]);
  assert.deepEqual(pick(["tools/dev/commandboard-query-plan-guard.sh"]).selectedJobs, ["common", "backend", "query-plans"]);
  assert.deepEqual(qp(["tools/dev/commandboard-query-plan-guard.sh"]), ["commandboard"]);
  assert.deepEqual(qp(["backend/internal/platform/pgtest/pgtest.go"]), ["commandboard"],
    "the pgtest harness is the command-board gate's DB harness");
  assert.deepEqual(qp(["backend/internal/verification/samplingsql/sql.go"]), ["commandboard"],
    "a package in the command-board test's import closure schedules it");
  assert.deepEqual(qp(["backend/go.sum"]), ["commandboard"]);
  assert.deepEqual(qp(["Makefile"]), ["sqlc", "commandboard"], "forced full suite runs both");
  assert.deepEqual(classifyPaths([]).queryPlanSteps, ["sqlc", "commandboard"]);
  assert.deepEqual(qp(["docs/progress/local-ci.md"]), []);
  // The command board's statements live in vaccinationexecution, NOT vaccination, and that one
  // missing prefix meant a PR touching only those statements skipped the query-plans job entirely
  // -- and ci-required then VERIFIED the skip as expected and went green. The endpoint whose plans
  // this gate exists to protect is exactly the one that returned 500 in staging, so a
  // command-board-only change must schedule the gate.
  const commandBoardQueryChange = classifyPaths([
    "backend/internal/vaccinationexecution/adapters/postgres/commandboard_sql.go",
  ]);
  assert.deepEqual(commandBoardQueryChange.queryPlanSteps, ["commandboard"]);
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
  assert.deepEqual(pick(["cloudbuild.stg.yaml"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  assert.deepEqual(pick(["tools/deploy/stg-clouddeploy-release.sh"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
  // package permissions is compiled into the command-board plan test, so it schedules that gate
  // (and only that gate); its postgres adapter subpackage is not in the closure.
  assert.deepEqual(pick(["backend/internal/permissions/routes.go"]), {
    common: true, backend: true, adminWeb: false, android: false, full: false,
    selectedJobs: ["common", "backend", "query-plans"],
  });
  assert.deepEqual(qp(["backend/internal/permissions/routes.go"]), ["commandboard"]);
  assert.deepEqual(qp(["backend/internal/permissions/adapters/postgres/grants.go"]), []);
  // CI tooling itself runs the common job (whose self-tests cover this scoping), never the DB gates.
  assert.deepEqual(pick(["tools/ci/run-local-ci.sh", "tools/ci/ci-scope.mjs", "tools/ci/component-paths.json"]).selectedJobs, ["common"]);
  // Build helpers sourced by other jobs (Gradle/Java/dispatch) are not common-only: they force the full suite.
  for (const helper of ["tools/ci/gradle-run.sh", "tools/ci/java21.sh", "tools/ci/parallel-dispatch.sh", "tools/ci/gradle-home.sh", "tools/ci/gradle-init/goatos-machine-lock.init.gradle"]) {
    assert.equal(pick([helper]).full, true, `${helper} must force the full suite`);
  }
  // Android DTOs are hand-mapped; no Android build or test reads contracts/ or
  // packages/api-client/, so a contract edit cannot fail the Android job. Android
  // still runs whenever its own DTOs/code change.
  // A fan-out no longer implies the DB plan gates: neither gate reads contracts/ or a UI file.
  assert.deepEqual(pick(["contracts/openapi/app-api.yaml"]), {
    common: true, backend: true, adminWeb: true, android: false, full: false,
    selectedJobs: ["common", "backend", "admin-web"],
  });
  assert.deepEqual(pick(["apps/admin-web/features/verification-review/verification-review-page.tsx"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "admin-web", "android"],
  });
  assert.deepEqual(pick(["apps/goatos-android/feature/feature-verify/src/main/kotlin/sg/mesha/goatos/feature/verify/VerifyQueueScreen.kt"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "admin-web", "android"],
  });
  assert.deepEqual(pick(["backend/internal/verification/adapters/proofmedia/resolver.go"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "admin-web", "android"],
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
  // Other workflows are not run by ci-local: a land.yml/nightly edit must not force the full suite.
  assert.equal(pick([".github/workflows/land.yml"]).full, false);
  assert.equal(pick([".github/workflows/land.yml"]).android, false);
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

// The command-board plan gate is scheduled from queryPlans.commandboard. Its package list must cover
// the plan test's whole import closure (`go list -test -deps`), or a change to a newly imported
// package would silently skip the gate. Fails closed when Go cannot answer.
export function checkQueryPlanClosure(rules = JSON.parse(readFileSync(rulesPath, "utf8"))) {
  const out = execFileSync("go", ["list", "-test", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}",
    "./internal/vaccinationexecution/adapters/postgres/"], { cwd: "backend", encoding: "utf8" });
  const prefix = "github.com/vgoats/goatos/backend/";
  const dirs = [...new Set(out.split("\n").filter((l) => l.startsWith(prefix))
    .map((l) => `backend/${l.slice(prefix.length).split(" ")[0].replace(/\.test$/, "")}/`))];
  const rule = rules.queryPlans.commandboard;
  const missing = dirs.filter((d) => !(rule.dirs ?? []).includes(d) && !(rule.prefixes ?? []).some((p) => d.startsWith(p)));
  assert.deepEqual(missing, [],
    "component-paths.json queryPlans.commandboard.dirs misses packages in the command-board plan test's import closure");
  assert.ok(dirs.length > 5, "go list returned an implausibly small closure");
  console.log(`ci-scope: command-board plan gate covers its ${dirs.length}-package import closure`);
}

if (process.argv.includes("--self-test")) {
  selfTest();
  if (process.argv.includes("--check-query-plan-closure")) checkQueryPlanClosure();
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
