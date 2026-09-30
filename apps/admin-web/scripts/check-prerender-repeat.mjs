#!/usr/bin/env node
// guard: prerender-repeat (FIXJ-BUILD). `next build` failed INTERMITTENTLY while prerendering
// /_global-error ("Cannot read properties of undefined (reading 'palette')"): Next mounts
// app/loading.tsx as the Suspense fallback of that provider-less tree, and React only renders the
// fallback when the page chunk has not resolved yet. One green build proves little for a race, so
// this compiles once and runs the prerender step (`next build --experimental-build-mode=generate`)
// N times from the same compile output, each on a fresh copy, failing on the first prerender error.
// Before the fix it failed 3/3 in generate mode (3-5s per pass).
//
//   node scripts/check-prerender-repeat.mjs [--runs 3]
//
// It works in `.next` and puts an existing `.next` back afterwards (the webpack cache is reused so
// the compile is warm). A different distDir would make Next rewrite tsconfig.json mid-run.
import { execFileSync } from "node:child_process";
import { cpSync, existsSync, mkdtempSync, renameSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const appDir = fileURLToPath(new URL("..", import.meta.url));
const runsArg = process.argv.indexOf("--runs");
const runs = runsArg > -1 ? Number(process.argv[runsArg + 1]) : 3;
if (!Number.isInteger(runs) || runs < 1) throw new Error("--runs needs a positive integer");

const dist = path.join(appDir, ".next");
const saved = path.join(appDir, ".next-prerender-saved");
const snapshot = mkdtempSync(path.join(tmpdir(), "admin-web-prerender-"));
const nextCli = createRequire(import.meta.url).resolve("next/dist/bin/next");

function build(mode) {
  try {
    const out = execFileSync(process.execPath, [nextCli, "build", "--webpack", `--experimental-build-mode=${mode}`], {
      cwd: appDir, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, NEXT_TELEMETRY_DISABLED: "1" },
    });
    return { ok: true, out };
  } catch (error) {
    return { ok: false, out: `${error.stdout ?? ""}${error.stderr ?? ""}${error.stdout == null ? String(error) : ""}` };
  }
}

if (existsSync(saved)) throw new Error(`${saved} exists: a previous run was interrupted; move it back to .next or delete it`);
const hadDist = existsSync(dist);
if (hadDist) renameSync(dist, saved);
let failed = false;
try {
  if (hadDist && existsSync(path.join(saved, "cache"))) {
    rmSync(dist, { recursive: true, force: true });
    cpSync(path.join(saved, "cache"), path.join(dist, "cache"), { recursive: true });
  }
  const compiled = build("compile");
  if (!compiled.ok) {
    console.error(compiled.out.slice(-4000));
    throw new Error("next build --experimental-build-mode=compile failed");
  }
  cpSync(dist, snapshot, { recursive: true, filter: (src) => !src.startsWith(path.join(dist, "cache")) });
  // Next 16.2 (webpack) renames server/proxy.js -> middleware.js at the end of EVERY build mode, so
  // the compile output no longer has the proxy.js that generate renames again (ENOENT after a green
  // prerender). Put the compile-time name back in the snapshot so each generate pass runs to the end.
  for (const suffix of ["", ".nft.json"]) {
    const renamed = path.join(snapshot, "server", `middleware.js${suffix}`);
    const original = path.join(snapshot, "server", `proxy.js${suffix}`);
    if (existsSync(renamed) && !existsSync(original)) cpSync(renamed, original);
  }
  for (let run = 1; run <= runs; run += 1) {
    rmSync(dist, { recursive: true, force: true });
    cpSync(snapshot, dist, { recursive: true });
    const started = Date.now();
    const generated = build("generate");
    const prerenderError = /Error occurred prerendering page|Export encountered an error/.test(generated.out);
    console.log(`prerender pass ${run}/${runs}: ${generated.ok && !prerenderError ? "ok" : "FAILED"} (${Date.now() - started}ms)`);
    if (!generated.ok || prerenderError) {
      console.error(generated.out.slice(-4000));
      failed = true;
      break;
    }
  }
} finally {
  rmSync(snapshot, { recursive: true, force: true });
  if (hadDist) {
    rmSync(dist, { recursive: true, force: true });
    renameSync(saved, dist);
  }
}
if (failed) {
  console.error("prerender-repeat: a prerender pass failed; a root special file (app/loading.tsx, app/global-error.tsx) likely needs a provider it does not have. See scripts/global-error-prerender.test.mjs.");
  process.exit(1);
}
console.log(`prerender-repeat: ${runs}/${runs} prerender passes green`);
