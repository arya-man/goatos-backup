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

function allComponents() {
  return { backend: true, adminWeb: true, android: true };
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

  for (const filePath of paths) {
    if (matches(filePath, rules.ciCommonOnly)) {
      reasons.push(`${filePath}: CI helper/tooling self-test coverage only`);
      continue;
    }

    if (matches(filePath, rules.forceFull)) {
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
  const obligationQueryChange = classifyPaths([
    "backend/internal/obligation/adapters/postgres/repository.go",
  ]);
  assert.equal(obligationQueryChange.queryPlans, true,
    "a production obligation-query change must schedule the PostgreSQL plan gate");
  assert.equal(obligationQueryChange.selectedJobs.includes("query-plans"), true,
    "normal PR/push CI must not leave the query-plan gate manual");
  assert.deepEqual(pick(["apps/admin-web/app/page.tsx"]), {
    common: true, backend: false, adminWeb: true, android: false, full: false,
    selectedJobs: ["common", "admin-web"],
  });
  assert.deepEqual(pick(["apps/goatos-android/app/build.gradle.kts"]), {
    common: true, backend: false, adminWeb: false, android: true, full: false,
    selectedJobs: ["common", "android"],
  });
  assert.deepEqual(pick(["tools/ci/land-main.sh"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
  });
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
  assert.deepEqual(pick(["contracts/openapi/app-api.yaml"]), {
    common: true, backend: true, adminWeb: true, android: true, full: false,
    selectedJobs: ["common", "backend", "query-plans", "admin-web", "android"],
  });
  assert.deepEqual(pick(["docs/runbooks/local-ci.md"]), {
    common: true, backend: false, adminWeb: false, android: false, full: false,
    selectedJobs: ["common"],
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
