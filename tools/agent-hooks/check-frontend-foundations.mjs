#!/usr/bin/env node

// Static ratchet for the admin-web framework baseline. This complements ESLint,
// tsc, node:test, and Next build by guarding repository-level invariants those
// tools cannot see together: runtime parity, test discovery, React server/client
// boundaries, Server Action request handling, and new fixed browser sleeps.

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const adminRoot = "apps/admin-web";
const expectedNodeMajor = "24";
const requiredTestGlobs = [
  "components/**/*.test.mjs",
  "features/**/*.test.mjs",
  "lib/**/*.test.mjs",
  "scripts/**/*.test.mjs",
];

// Existing debt is a ratchet, not an approval. Any lower count passes; any new
// path or higher count fails until the waits are replaced by web-first signals.
const fixedWaitBaseline = new Map([
  ["apps/admin-web/scripts/smoke-vaccination-authoring-live.mjs", 4],
  ["apps/admin-web/scripts/smoke-visual-live.mjs", 2],
  ["apps/admin-web/scripts/sop-builder-e2e.mjs", 40],
]);

// This pre-existing DLQ transition delegates auth/authorization to the
// server-only API adapter and validates its form, but the adapter has no caller
// idempotency/OCC parameter yet. New action surfaces get no such baseline.
const replayBaseline = new Set([
  "apps/admin-web/features/operations-dlq/actions.ts",
]);

const sourceExtension = /\.(?:js|jsx|mjs|ts|tsx)$/;
const testExtension = /\.test\.(?:js|mjs|ts|tsx)$/;
const fixedWaitRe = /\bpage\.waitForTimeout\s*\(|new\s+Promise\s*\([^;\n]*\bsetTimeout\s*\(/g;
const routerNavigationRe = /\brouter\.(?:replace|push)\s*\(/;
const routerNavigationAllRe = /\brouter\.(?:replace|push)\s*\(/g;
const startTransitionCallRe = /\bstartTransition\s*\(/g;
const selectBlockRe = /<select\b[\s\S]*?<\/select>/g;

function finding(file, message) {
  return `${file}: ${message}`;
}

function nodeParityFindings({ packageJson, packageLock, nvmrc, docker, workflows }) {
  const findings = [];
  if (packageJson.engines?.node !== `${expectedNodeMajor}.x`) {
    findings.push(finding(`${adminRoot}/package.json`, `engines.node must be ${expectedNodeMajor}.x`));
  }
  if (packageLock.packages?.[""]?.engines?.node !== `${expectedNodeMajor}.x`) {
    findings.push(finding(`${adminRoot}/package-lock.json`, `root engines.node must be ${expectedNodeMajor}.x`));
  }
  if (nvmrc.trim() !== expectedNodeMajor) {
    findings.push(finding(`${adminRoot}/.nvmrc`, `must pin Node ${expectedNodeMajor}`));
  }

  const imageMajors = [...docker.matchAll(/^FROM\s+node:(\d+)(?:[.-])/gm)].map((match) => match[1]);
  if (imageMajors.length < 2 || imageMajors.some((major) => major !== expectedNodeMajor)) {
    findings.push(finding(`${adminRoot}/Dockerfile`, `all Node build/runtime stages must use major ${expectedNodeMajor}`));
  }

  const ciMajors = [...workflows.matchAll(/node-version:\s*["']?(\d+)/g)].map((match) => match[1]);
  if (ciMajors.length === 0 || ciMajors.some((major) => major !== expectedNodeMajor)) {
    findings.push(finding(".github/workflows", `all setup-node jobs must use major ${expectedNodeMajor}`));
  }
  return findings;
}

function tsConfigFindings(tsconfig) {
  const findings = [];
  for (const flag of ["strict", "noEmit", "isolatedModules"]) {
    if (tsconfig.compilerOptions?.[flag] !== true) {
      findings.push(finding(`${adminRoot}/tsconfig.json`, `compilerOptions.${flag} must remain true`));
    }
  }
  return findings;
}

function testDiscoveryFindings(testScript, testFiles) {
  const findings = [];
  for (const glob of requiredTestGlobs) {
    if (!testScript.includes(glob)) {
      findings.push(finding(`${adminRoot}/package.json`, `test script must discover ${glob}`));
    }
  }
  for (const file of testFiles) {
    const relative = file.slice(`${adminRoot}/`.length);
    if (!requiredTestGlobs.some((glob) => relative.startsWith(glob.split("/")[0] + "/")) || !file.endsWith(".test.mjs")) {
      findings.push(finding(file, "is outside the committed node:test discovery contract"));
    }
  }
  return findings;
}

function hasDirective(source, directive) {
  return source.trimStart().startsWith(`"${directive}"`) || source.trimStart().startsWith(`'${directive}'`);
}

function importsFrom(source) {
  const imports = [];
  const re = /\bimport\s+(?!["'])([\s\S]*?)\s+from\s+["']([^"']+)["']/g;
  for (const match of source.matchAll(re)) {
    imports.push({ clause: match[1].trim(), specifier: match[2] });
  }
  return imports;
}

function isTypeOnlyClause(clause) {
  if (clause.startsWith("type ")) return true;
  const braces = clause.match(/^\{([\s\S]*)\}$/);
  if (!braces) return false;
  return braces[1]
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean)
    .every((part) => part.startsWith("type "));
}

function isServerOnlySpecifier(specifier) {
  return specifier === "server-only" ||
    specifier === "next/headers" ||
    specifier === "next/cache" ||
    specifier === "@/lib/api/server" ||
    specifier === "@/lib/action-helpers" ||
    specifier === "@/lib/auth/server-session" ||
    specifier === "@/lib/auth/firebase-refresh" ||
    /(?:^|\/)api\/[^/]*-server$/.test(specifier) ||
    /(?:^|\/)calendar-server$/.test(specifier);
}

function boundaryFindings(file, source) {
  const findings = [];
  if (hasDirective(source, "use client")) {
    for (const entry of importsFrom(source)) {
      if (isServerOnlySpecifier(entry.specifier) && !isTypeOnlyClause(entry.clause)) {
        findings.push(finding(file, `Client Component imports server-only runtime module ${entry.specifier}`));
      }
    }
    if (/\bprocess\.env\.(?!NEXT_PUBLIC_)[A-Z0-9_]+/.test(source)) {
      findings.push(finding(file, "Client Component reads a non-NEXT_PUBLIC environment value"));
    }
  }

  const mustDeclareServerOnly = /(?:^|\/)(?:server|action-helpers|server-session|firebase-refresh|[^/]*-server)\.ts$/.test(file);
  if (mustDeclareServerOnly && !hasDirective(source, "use server") && !/^\s*import\s+["']server-only["'];?/m.test(source)) {
    findings.push(finding(file, "server adapter must begin with import \"server-only\""));
  }
  return findings;
}

function actionFindings(file, source) {
  if (!hasDirective(source, "use server")) return [];
  const findings = [];
  if (/\bfetch\s*\(/.test(source) || /from\s+["']@goatos\/api-client/.test(source)) {
    findings.push(finding(file, "Server Actions must use the authenticated server-only API adapter, not raw fetch/generated clients"));
  }
  const delegated = importsFrom(source).some(({ clause, specifier }) =>
    !isTypeOnlyClause(clause) && (specifier === "@/lib/api/server" || /-server$/.test(specifier)),
  );
  if (!delegated) {
    findings.push(finding(file, "Server Action file must delegate authentication/authorization through an approved server API adapter"));
  }
  const validatesInput = /required(?:String|Number|EvidenceRef)|optionalString|\binEnum\b|\bvalidate\w*\s*\(|\btypeof\s+|\.trim\(\)|Number\.is(?:Finite|Integer)|\bif\s*\(\s*!/.test(source);
  if (!validatesInput) {
    findings.push(finding(file, "Server Action file has no explicit input validation boundary"));
  }
  const replayProtected = /idempotency|row_version|rowVersion|expected_row_version|stableMutationKey|randomUUID|createHash/.test(source);
  const readOnlyAction = /server-action-read-only/.test(source) &&
    /\bget[A-Z]\w*\s*\(/.test(source) &&
    !/\b(?:create|update|delete|mutate|record|submit|approve|reject|verdict)[A-Z]\w*\s*\(/.test(source);
  if (!replayProtected && !readOnlyAction && !replayBaseline.has(file)) {
    findings.push(finding(file, "Server Action mutation has no stable idempotency key or optimistic-concurrency replay protection"));
  }
  return findings;
}

function fixedWaitFindings(file, source, baseline = fixedWaitBaseline) {
  const count = [...source.matchAll(fixedWaitRe)].length;
  const allowed = baseline.get(file) ?? 0;
  return count > allowed
    ? [finding(file, `contains ${count} fixed browser wait(s); baseline is ${allowed}; use Playwright web-first assertions/actions`)]
    : [];
}

function callSpans(source, callRe) {
  const spans = [];
  for (const match of source.matchAll(callRe)) {
    const open = match.index + match[0].lastIndexOf("(");
    let depth = 0;
    let quote = "";
    let lineComment = false;
    let blockComment = false;
    let escaped = false;

    for (let index = open; index < source.length; index += 1) {
      const char = source[index];
      const next = source[index + 1] ?? "";

      if (lineComment) {
        if (char === "\n") lineComment = false;
        continue;
      }
      if (blockComment) {
        if (char === "*" && next === "/") {
          blockComment = false;
          index += 1;
        }
        continue;
      }
      if (quote) {
        if (escaped) {
          escaped = false;
        } else if (char === "\\") {
          escaped = true;
        } else if (char === quote) {
          quote = "";
        }
        continue;
      }

      if (char === "/" && next === "/") {
        lineComment = true;
        index += 1;
        continue;
      }
      if (char === "/" && next === "*") {
        blockComment = true;
        index += 1;
        continue;
      }
      if (char === "\"" || char === "'" || char === "`") {
        quote = char;
        continue;
      }
      if (char === "(") depth += 1;
      if (char === ")") {
        depth -= 1;
        if (depth === 0) {
          spans.push({ start: match.index, end: index + 1 });
          break;
        }
      }
    }
  }
  return spans;
}

function urlSelectResponsivenessFindings(file, source) {
  if (!hasDirective(source, "use client")) return [];
  if (!source.includes("useSearchParams")) return [];
  if (!routerNavigationRe.test(source)) return [];
  if (!/<select\b/.test(source) || !/\bonChange=/.test(source)) return [];

  const findings = [];
  const routerWrites = [...source.matchAll(routerNavigationAllRe)];
  const transitionSpans = callSpans(source, startTransitionCallRe);
  const unwrappedRouterWrite = routerWrites.some((match) =>
    !transitionSpans.some((span) => span.start <= match.index && match.index < span.end),
  );
  const hasTransition = /\buseTransition\b/.test(source) && !unwrappedRouterWrite;
  const hasOptimisticState =
    /\buseOptimistic\b/.test(source) ||
    /\boptimistic[A-Z_]?[\w$]*\b/i.test(source) ||
    /\beffective(?:Search|Params|Value|Field|Drive|Selection)\b/.test(source) ||
    /\bselected(?:Drive|PageSize|Value)\b/.test(source) ||
    /\bfieldValue\s*\(/.test(source);
  const staleSelect = [...source.matchAll(selectBlockRe)].some((match) => {
    const block = match[0];
    const selectTag = block.match(/^<select\b[^>]*>/)?.[0] ?? "";
    return /\bonChange=/.test(block) &&
      /\bvalue=\{\s*(?:field\.value|driveBatchId|pageSize|props\.\w+|[A-Za-z_$][\w$]*\.value)\s*(?:\?\?|[?:}]|$)/.test(selectTag) &&
      !/\bvalue=\{\s*(?:fieldValue\s*\(|selected[A-Z]\w*|effective[A-Z]\w*|optimistic[A-Z]\w*)/.test(selectTag);
  });

  if (!hasTransition) {
    findings.push(finding(file, "URL-writing select control must wrap router.push/replace in useTransition"));
  }
  if (!hasOptimisticState || staleSelect) {
    findings.push(finding(file, "URL-writing select control must render optimistic selected state while the server refresh is pending"));
  }
  return findings;
}

function routeErrorFindings(source) {
  const file = `${adminRoot}/app/(admin)/error.tsx`;
  const findings = [];
  if (!hasDirective(source, "use client")) findings.push(finding(file, "must be a Client Component"));
  if (!/AdminRouteError/.test(source) || !/reset/.test(source)) {
    findings.push(finding(file, "must render a visible retry boundary wired to Next.js reset()"));
  }
  if (/error\.message/.test(source)) findings.push(finding(file, "must not expose raw server error messages"));
  return findings;
}

function selfTest() {
  const goodNode = {
    packageJson: { engines: { node: "24.x" } },
    packageLock: { packages: { "": { engines: { node: "24.x" } } } },
    nvmrc: "24\n",
    docker: "FROM node:24-alpine AS deps\nFROM node:24-alpine AS runtime\n",
    workflows: "node-version: 24\n",
  };
  assert.equal(nodeParityFindings(goodNode).length, 0);
  assert.ok(nodeParityFindings({ ...goodNode, docker: "FROM node:22-alpine\nFROM node:24-alpine\n" }).length > 0);
  assert.equal(tsConfigFindings({ compilerOptions: { strict: true, noEmit: true, isolatedModules: true } }).length, 0);
  assert.ok(tsConfigFindings({ compilerOptions: { strict: false, noEmit: true, isolatedModules: true } }).length > 0);
  assert.equal(testDiscoveryFindings(requiredTestGlobs.join(" "), [`${adminRoot}/lib/a.test.mjs`]).length, 0);
  assert.ok(testDiscoveryFindings("features/**/*.test.mjs", [`${adminRoot}/scripts/a.test.mjs`]).length > 0);

  const badClient = '"use client";\nimport { getSecret } from "@/lib/api/server";\n';
  const goodClient = '"use client";\nimport type { ApiResult } from "@/lib/api/server";\n';
  assert.ok(boundaryFindings(`${adminRoot}/components/bad.tsx`, badClient).length > 0);
  assert.equal(boundaryFindings(`${adminRoot}/components/good.tsx`, goodClient).length, 0);
  assert.ok(boundaryFindings(`${adminRoot}/lib/api/new-server.ts`, "export const x = 1;\n").length > 0);

  const goodAction = '"use server";\nimport { mutate } from "@/lib/api/server";\nexport async function save(v: string) { if (!v.trim()) throw new Error(); return mutate(v, { row_version: 1 }); }\n';
  const goodReadOnlyAction = '"use server";\nimport { getReport } from "@/lib/api/server";\n// server-action-read-only: GET-backed export; no mutation replay key required.\nexport async function exportReport(v: string) { if (!v.trim()) throw new Error(); return getReport({ id: v.trim() }); }\n';
  const badReadOnlyMarkerMutation = '"use server";\nimport { submitReport } from "@/lib/api/server";\n// server-action-read-only: forged marker.\nexport async function save(v: string) { if (!v.trim()) throw new Error(); return submitReport(v); }\n';
  const badAction = '"use server";\nexport async function save(v) { return fetch(v); }\n';
  assert.equal(actionFindings(`${adminRoot}/features/good/actions.ts`, goodAction).length, 0);
  assert.equal(actionFindings(`${adminRoot}/features/good-export/actions.ts`, goodReadOnlyAction).length, 0);
  assert.ok(actionFindings(`${adminRoot}/features/bad-export/actions.ts`, badReadOnlyMarkerMutation).some((item) => item.includes("replay protection")));
  assert.ok(actionFindings(`${adminRoot}/features/bad/actions.ts`, badAction).length >= 3);
  assert.equal(fixedWaitFindings("old.mjs", "await page.waitForTimeout(10);", new Map([["old.mjs", 1]])).length, 0);
  assert.ok(fixedWaitFindings("new.mjs", "await page.waitForTimeout(10);", new Map()).length > 0);
  const staleUrlSelect = '"use client";\nimport { useRouter, useSearchParams } from "next/navigation";\nexport function Bad({ value }) { const router = useRouter(); const sp = useSearchParams(); return <select value={value} onChange={(event) => router.replace(`?x=${event.target.value}`)} />; }\n';
  const responsiveUrlSelect = '"use client";\nimport { useState, useTransition } from "react";\nimport { useRouter, useSearchParams } from "next/navigation";\nexport function Good({ value }) { const router = useRouter(); const sp = useSearchParams(); const [isPending, startTransition] = useTransition(); const [optimistic, setOptimistic] = useState(null); const selectedValue = optimistic ?? value; return <select value={selectedValue} onChange={(event) => { setOptimistic(event.target.value); startTransition(() => router.replace(`?x=${event.target.value}`)); }} />; }\n';
  const siblingStaleSelect = '"use client";\nimport { useState, useTransition } from "react";\nimport { useRouter, useSearchParams } from "next/navigation";\nexport function Mixed({ value, other }) { const router = useRouter(); const sp = useSearchParams(); const [isPending, startTransition] = useTransition(); const [optimistic, setOptimistic] = useState(null); const selectedValue = optimistic ?? value; return <><select value={selectedValue} onChange={(event) => { setOptimistic(event.target.value); startTransition(() => router.replace(`?x=${event.target.value}`)); }}><option /></select><select value={other.value} onChange={(event) => { startTransition(() => router.replace(`?y=${event.target.value}`)); }}><option /></select></>; }\n';
  const firstWriteUnwrapped = '"use client";\nimport { useState, useTransition } from "react";\nimport { useRouter, useSearchParams } from "next/navigation";\nexport function FirstBad({ value }) { const router = useRouter(); const sp = useSearchParams(); const [isPending, startTransition] = useTransition(); const [optimistic, setOptimistic] = useState(null); const selectedValue = optimistic ?? value; return <><select value={selectedValue} onChange={(event) => { setOptimistic(event.target.value); router.replace(`?x=${event.target.value}`); }}><option /></select><button onClick={() => startTransition(() => router.replace(\"?ok=1\"))}>ok</button></>; }\n';
  const laterWriteUnwrapped = '"use client";\nimport { useState, useTransition } from "react";\nimport { useRouter, useSearchParams } from "next/navigation";\nexport function LaterBad({ value, other }) { const router = useRouter(); const sp = useSearchParams(); const [isPending, startTransition] = useTransition(); const [optimistic, setOptimistic] = useState(null); const selectedValue = optimistic ?? value; return <><select value={selectedValue} onChange={(event) => { setOptimistic(event.target.value); startTransition(() => router.replace(`?x=${event.target.value}`)); }}><option /></select><select value={other} onChange={(event) => { router.replace(`?y=${event.target.value}`); }}><option /></select></>; }\n';
  assert.ok(urlSelectResponsivenessFindings(`${adminRoot}/features/bad-filter.tsx`, staleUrlSelect).length >= 2);
  assert.equal(urlSelectResponsivenessFindings(`${adminRoot}/features/good-filter.tsx`, responsiveUrlSelect).length, 0);
  assert.ok(urlSelectResponsivenessFindings(`${adminRoot}/features/mixed-filter.tsx`, siblingStaleSelect).length > 0);
  assert.ok(urlSelectResponsivenessFindings(`${adminRoot}/features/first-bad-filter.tsx`, firstWriteUnwrapped).length > 0);
  assert.ok(urlSelectResponsivenessFindings(`${adminRoot}/features/later-bad-filter.tsx`, laterWriteUnwrapped).length > 0);
  assert.equal(routeErrorFindings('"use client";\nexport default ({error, reset}) => <AdminRouteError error={error} reset={reset} />;').length, 0);
  assert.ok(routeErrorFindings('export default ({error}) => <div>{error.message}</div>;').length > 0);
  console.log("frontend-foundations guard: self-test passed");
}

function read(relative) {
  return readFileSync(resolve(repo, relative), "utf8");
}

function adminFiles() {
  return execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", adminRoot], {
    cwd: repo,
    encoding: "utf8",
  })
    .split("\n")
    .map((file) => file.trim())
    .filter((file) => file && existsSync(resolve(repo, file)));
}

function run() {
  const files = adminFiles();
  const sourceFiles = files.filter((file) => sourceExtension.test(file));
  const findings = [
    ...nodeParityFindings({
      packageJson: JSON.parse(read(`${adminRoot}/package.json`)),
      packageLock: JSON.parse(read(`${adminRoot}/package-lock.json`)),
      nvmrc: read(`${adminRoot}/.nvmrc`),
      docker: read(`${adminRoot}/Dockerfile`),
      workflows: [read(".github/workflows/ci.yml"), read(".github/workflows/stg-pr-gate.yml")].join("\n"),
    }),
    ...tsConfigFindings(JSON.parse(read(`${adminRoot}/tsconfig.json`))),
    ...testDiscoveryFindings(
      JSON.parse(read(`${adminRoot}/package.json`)).scripts?.test ?? "",
      files.filter((file) => testExtension.test(file)),
    ),
  ];

  for (const file of sourceFiles) {
    const source = read(file);
    findings.push(...boundaryFindings(file, source));
    findings.push(...actionFindings(file, source));
    findings.push(...fixedWaitFindings(file, source));
    findings.push(...urlSelectResponsivenessFindings(file, source));
  }

  const routeError = `${adminRoot}/app/(admin)/error.tsx`;
  if (!existsSync(resolve(repo, routeError))) {
    findings.push(finding(routeError, "missing App Router segment error boundary"));
  } else {
    findings.push(...routeErrorFindings(read(routeError)));
  }

  if (findings.length > 0) {
    console.error("frontend-foundations guard failed:");
    for (const item of findings) console.error(`- ${item}`);
    process.exit(1);
  }
  console.log(`frontend-foundations guard: ok (${sourceFiles.length} source file(s), ${files.filter((file) => testExtension.test(file)).length} test file(s))`);
}

if (process.argv.includes("--self-test")) selfTest();
else run();
