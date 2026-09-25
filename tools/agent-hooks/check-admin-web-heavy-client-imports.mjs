#!/usr/bin/env node
// admin-web-heavy-client-imports guard (2026-09-25, from PR #415 commit 0afa8abf8).
//
// PATTERN: heavy browser SDKs (markdown renderer + plugins, OpenTelemetry tracing, the
// Firebase modular SDK) must reach the browser as their OWN lazy chunk -- `import()` /
// `next/dynamic` when the feature is first used -- never as a static import inside the
// client graph. 0afa8abf8 moved react-markdown/remark-gfm/rehype-highlight, the Faro tracing
// instrumentation and firebase/* behind dynamic imports: first-load JS ~440 -> ~325 KB gzip
// per route, mobile Lighthouse 53-76 -> 83-92, LCP ~5.6 s -> ~3.8 s.
//
// WHAT IS CHECKED (structurally, over the import graph, not by token):
//   1. Build the static import graph of apps/admin-web (relative and "@/" specifiers; `import
//      type` and `import()` edges are NOT static runtime edges and are skipped).
//   2. The CLIENT GRAPH is every module that has "use client" plus everything statically
//      reachable from one.
//   3. A finding is a module in the client graph that statically imports a HEAVY_MODULES entry.
//   A module reached only through `import()` is a lazy boundary and passes -- that is the fix.
//   Server-only modules (route handlers, server components never imported by a client module)
//   pass: their imports never ship to the browser.
//
// RATCHET: tools/agent-hooks/admin-web-heavy-client-imports.baseline.json freezes the
// occurrences on main when the guard landed. It may only SHRINK; a count above baseline fails,
// and a stale (over-counted) entry fails too so fixed debt cannot be silently re-spent.
//
// BLIND SPOTS: package-internal barrels (a light package that re-exports a heavy one) are not
// followed; bare-specifier aliases other than "@/" are not resolved; `require()` is not seen;
// the HEAVY_MODULES list is curated -- a new heavy SDK must be added here. Bundle-size budgets
// (next build output) are the runtime backstop and are review-only today.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve, posix } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const baselinePath = "tools/agent-hooks/admin-web-heavy-client-imports.baseline.json";
export const HEAVY_MODULES = [
  /^react-markdown$/,
  /^remark-/,
  /^rehype-/,
  /^@grafana\/faro-web-tracing$/,
  /^@opentelemetry\//,
  /^firebase\//,
  /^@firebase\//,
];

const staticImportRe = /^\s*(?:import|export)\s+(?!type\b)(?:[^'";]*?\sfrom\s*)?["']([^"']+)["']/gm;

function isHeavy(spec) {
  return HEAVY_MODULES.some((re) => re.test(spec));
}

function staticSpecifiers(text) {
  const out = [];
  for (const m of text.matchAll(staticImportRe)) {
    // `import { type A, type B } from "x"` is type-only too.
    const clause = m[0];
    const braces = clause.match(/\{([^}]*)\}/);
    if (braces && !/import\s+\w/.test(clause.replace(/import\s+\{/, "import {"))) {
      const names = braces[1].split(",").map((s) => s.trim()).filter(Boolean);
      if (names.length > 0 && names.every((n) => n.startsWith("type "))) continue;
    }
    out.push(m[1]);
  }
  return out;
}

function resolveLocal(fromFile, spec, exists) {
  let base;
  if (spec.startsWith("@/")) base = posix.join("apps/admin-web", spec.slice(2));
  else if (spec.startsWith(".")) base = posix.normalize(posix.join(posix.dirname(fromFile), spec));
  else return null;
  for (const cand of [base, ...[".ts", ".tsx", ".js", ".jsx", ".mjs"].map((e) => base + e), ...["index.ts", "index.tsx", "index.js"].map((i) => posix.join(base, i))]) {
    if (exists(cand)) return cand;
  }
  return null;
}

export function findings(files, readText, exists) {
  const text = new Map(files.map((f) => [f, readText(f)]));
  const edges = new Map();
  const heavy = new Map();
  for (const f of files) {
    const specs = staticSpecifiers(text.get(f));
    edges.set(f, specs.map((s) => resolveLocal(f, s, exists)).filter(Boolean));
    const h = specs.filter(isHeavy);
    if (h.length) heavy.set(f, h);
  }
  // A "use client" module is a client-graph ROOT only when something imports it STATICALLY or it
  // is a Next entry file. One reached only through import()/next/dynamic is a lazy chunk: that is
  // the fix, so it must not be flagged.
  const staticallyImported = new Set();
  for (const ns of edges.values()) for (const n of ns) staticallyImported.add(n);
  const isEntry = (f) => /\/app\/.*\/(page|layout|template|error|loading|not-found|default)\.(t|j)sx?$/.test(f) || /\/app\/(page|layout|template|error|loading|not-found)\.(t|j)sx?$/.test(f);
  const client = new Set();
  const stack = files.filter(
    (f) => /^\s*["']use client["']/m.test(text.get(f).slice(0, 400)) && (staticallyImported.has(f) || isEntry(f)),
  );
  while (stack.length) {
    const f = stack.pop();
    if (client.has(f)) continue;
    client.add(f);
    for (const n of edges.get(f) || []) if (text.has(n)) stack.push(n);
  }
  const out = {};
  for (const [f, specs] of heavy) if (client.has(f)) out[f] = specs.length;
  return out;
}

function sourceFiles() {
  const out = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "apps/admin-web"], { cwd: repo, encoding: "utf8" });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter((f) => /\.(?:ts|tsx|js|jsx|mjs)$/.test(f) && !/\.(test|spec)\.|\/scripts\/|\/tests?\//.test(f))
    .filter((f) => existsSync(resolve(repo, f)));
}

function selfTest() {
  const fx = {
    "apps/admin-web/app/(admin)/layout.tsx": 'import Shell from "@/components/shell";\n',
    "apps/admin-web/components/shell.tsx": '"use client";\nimport { Panel } from "@/features/ai/panel";\nimport { Lazy } from "@/features/ai/lazy-panel";\n',
    "apps/admin-web/features/ai/panel.tsx": 'import ReactMarkdown from "react-markdown";\n',
    "apps/admin-web/features/ai/lazy-panel.tsx": '"use client";\nconst Md = dynamic(() => import("./markdown"));\n',
    "apps/admin-web/features/ai/markdown.tsx": '"use client";\nimport ReactMarkdown from "react-markdown";\n',
    "apps/admin-web/app/api/cfg/route.ts": 'import { initializeApp } from "firebase/app";\n',
    "apps/admin-web/lib/types.ts": '"use client";\nimport type { FirebaseOptions } from "firebase/app";\nimport { type User } from "firebase/auth";\n',
  };
  const files = Object.keys(fx);
  const got = findings(files, (f) => fx[f], (f) => f in fx);
  // Bad: panel.tsx is statically reached from a client shell (the pre-0afa8abf8 shape).
  // Good: markdown.tsx is "use client" with a static heavy import but is reached ONLY through
  // lazy-panel's import() -- it is the lazy chunk, the fix itself, and must pass.
  const want = { "apps/admin-web/features/ai/panel.tsx": 1 };
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    throw new Error(`self-test: want ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
  }
  // Good: the route handler (server-only) and type-only imports are never flagged.
  if (got["apps/admin-web/app/api/cfg/route.ts"] || got["apps/admin-web/lib/types.ts"]) throw new Error("self-test: false positive");
  // Ratchet: a count above baseline fails; a stale baseline entry fails.
  const over = compare({ a: 2 }, { a: 1 });
  const stale = compare({}, { a: 1 });
  const ok = compare({ a: 1 }, { a: 1 });
  if (!over.length || !stale.length || ok.length) throw new Error("self-test: ratchet compare broken");
  console.log("admin-web-heavy-client-imports guard: self-test passed");
}

function compare(actual, base) {
  const problems = [];
  for (const [f, n] of Object.entries(actual)) {
    if (n > (base[f] || 0)) problems.push(`${f}: ${n} static heavy-SDK import(s) in the client graph (baseline ${base[f] || 0}); load it with import()/next/dynamic when first used`);
  }
  for (const [f, n] of Object.entries(base)) {
    if ((actual[f] || 0) < n) problems.push(`${f}: baseline says ${n}, now ${actual[f] || 0} -- lower the baseline (ratchet only shrinks)`);
  }
  return problems;
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}
const files = sourceFiles();
const actual = findings(files, (f) => readFileSync(resolve(repo, f), "utf8"), (f) => existsSync(resolve(repo, f)));
if (process.argv.includes("--write-baseline")) {
  writeFileSync(resolve(repo, baselinePath), JSON.stringify(actual, null, 2) + "\n");
  console.log(`wrote ${baselinePath}`);
  process.exit(0);
}
const base = existsSync(resolve(repo, baselinePath)) ? JSON.parse(readFileSync(resolve(repo, baselinePath), "utf8")) : {};
const problems = compare(actual, base);
if (problems.length) {
  console.error("admin-web-heavy-client-imports guard failed:");
  for (const p of problems) console.error(`- ${p}`);
  console.error("See docs/decisions/scale-anti-patterns.md -> Proven performance patterns (PP-5).");
  process.exit(1);
}
console.log(`admin-web-heavy-client-imports guard: ok (${files.length} files, ${Object.keys(actual).length} baselined client module(s))`);
