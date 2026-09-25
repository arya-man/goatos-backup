#!/usr/bin/env node
// step-input-digest.mjs — THE one definition of "the inputs a ci-local step can see".
//
// run-local-ci.sh keys every step's pass-marker by this digest (plus the step's
// name, job, exact command and CI-relevant env), so a re-run after a fix commit
// re-executes ONLY the steps whose inputs changed. check-local-ci-evidence.mjs
// recomputes the SAME digest at push time for every step the receipt lists, so a
// reused pass is accepted only when its inputs are byte-identical to the tree
// being pushed. One implementation, two callers: they cannot drift.
//
// A digest is built from git TREE object ids of committed content at <sha>
// (never the working tree — run-local-ci.sh disables the cache on a dirty tree):
//
//   whole    : the entire tree + the CI diff base. Every guard whose inputs are not
//              precisely known lands here. It includes the base because many guards
//              are diff-scoped against origin/main.
//   backend  : backend/ contracts/ tools/ci/ tools/scale-guard/ Makefile
//   adminweb : apps/admin-web/ contracts/ mock/ packages/ root package + lock files
//              tools/ci/ Makefile
//   android  : apps/goatos-android/ contracts/ tools/ci/ Makefile
//
// Narrow sets are used ONLY for build/compile/test steps whose inputs are those
// directories (run-local-ci.sh -> step_input_set). Unknown steps fall back to
// `whole`. Adding a step to a narrow set is a deliberate review decision.
//
// Usage:
//   node tools/ci/step-input-digest.mjs --set <whole|backend|adminweb|android> --base <sha> [--sha <sha>]
//   node tools/ci/step-input-digest.mjs --self-test
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const INPUT_SETS = {
  whole: { paths: ["."], includeBase: true },
  backend: { paths: ["backend", "contracts", "tools/ci", "tools/scale-guard", "Makefile"], includeBase: false },
  adminweb: {
    paths: ["apps/admin-web", "contracts", "mock", "packages", "package.json", "package-lock.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "tools/ci", "Makefile"],
    includeBase: false,
  },
  android: { paths: ["apps/goatos-android", "contracts", "tools/ci", "Makefile"], includeBase: false },
};

function objectId(sha, path, cwd) {
  const spec = path === "." ? `${sha}^{tree}` : `${sha}:${path}`;
  try {
    return execFileSync("git", ["rev-parse", "--verify", "--quiet", spec], { cwd, stdio: ["ignore", "pipe", "ignore"] }).toString("utf8").trim() || "MISSING";
  } catch {
    return "MISSING";
  }
}

export function inputDigest({ set, base, sha = "HEAD", cwd = process.cwd() }) {
  const spec = INPUT_SETS[set];
  if (!spec) throw new Error(`unknown input set: ${set}`);
  const resolved = execFileSync("git", ["rev-parse", "--verify", `${sha}^{commit}`], { cwd }).toString("utf8").trim();
  const lines = [`v=1`, `set=${set}`];
  for (const p of spec.paths) lines.push(`path=${p} id=${objectId(resolved, p, cwd)}`);
  if (spec.includeBase) lines.push(`base=${base || ""}`);
  return createHash("sha256").update(lines.join("\n")).digest("hex");
}

function arg(args, name) {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
}

function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), "step-input-digest-"));
  const git = (...a) => execFileSync("git", a, { cwd: dir, stdio: ["ignore", "pipe", "ignore"] }).toString("utf8").trim();
  const put = (p, c) => { mkdirSync(join(dir, p, ".."), { recursive: true }); writeFileSync(join(dir, p), c); };
  const fails = [];
  const expect = (cond, msg) => { if (!cond) fails.push(msg); };
  try {
    git("init", "-q");
    git("config", "user.email", "t@mesha.sg");
    git("config", "user.name", "t");
    put("backend/a.go", "package a\n");
    put("apps/goatos-android/b.kt", "val b = 1\n");
    put("apps/admin-web/c.ts", "export const c = 1\n");
    put("tools/ci/run-local-ci.sh", "echo v1\n");
    put("docs/x.md", "x\n");
    git("add", "-A");
    git("commit", "-qm", "c1");
    const base = git("rev-parse", "HEAD");
    const d = (set, sha = "HEAD", b = base) => inputDigest({ set, base: b, sha, cwd: dir });
    const c1 = Object.fromEntries(Object.keys(INPUT_SETS).map((s) => [s, d(s)]));

    // (1) nothing changed -> every digest identical (full reuse)
    git("commit", "-q", "--allow-empty", "-m", "empty");
    for (const s of Object.keys(INPUT_SETS)) expect(d(s) === c1[s], `no-op commit changed ${s} digest`);

    // (2) change a backend file -> backend + whole re-run; android + adminweb reused
    put("backend/a.go", "package a\n// fix\n");
    git("commit", "-qam", "backend fix");
    expect(d("backend") !== c1.backend, "backend change did not invalidate backend digest");
    expect(d("whole") !== c1.whole, "backend change did not invalidate whole digest");
    expect(d("android") === c1.android, "backend change invalidated android digest");
    expect(d("adminweb") === c1.adminweb, "backend change invalidated adminweb digest");

    // (3) change the step script -> every set that owns tools/ci re-runs
    const c2 = Object.fromEntries(Object.keys(INPUT_SETS).map((s) => [s, d(s)]));
    put("tools/ci/run-local-ci.sh", "echo v2\n");
    git("commit", "-qam", "script");
    for (const s of Object.keys(INPUT_SETS)) expect(d(s) !== c2[s], `step-script change did not invalidate ${s}`);

    // (4) docs-only change -> only whole moves
    const c3 = Object.fromEntries(Object.keys(INPUT_SETS).map((s) => [s, d(s)]));
    put("docs/x.md", "y\n");
    git("commit", "-qam", "docs");
    expect(d("whole") !== c3.whole, "docs change did not invalidate whole");
    for (const s of ["backend", "adminweb", "android"]) expect(d(s) === c3[s], `docs change invalidated ${s}`);

    // (5) a different CI base invalidates diff-scoped (whole) steps only
    expect(d("whole", "HEAD", "0".repeat(40)) !== d("whole"), "base change did not invalidate whole");
    expect(d("backend", "HEAD", "0".repeat(40)) === d("backend"), "base change invalidated backend");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  if (fails.length) {
    for (const f of fails) console.error(`step-input-digest self-test FAIL: ${f}`);
    process.exit(1);
  }
  console.log("step-input-digest: self-test passed");
}

const isMain = process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href;
if (isMain) {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) selfTest();
  else {
    const set = arg(args, "--set");
    if (!set) {
      console.error("usage: step-input-digest.mjs --set <whole|backend|adminweb|android> --base <sha> [--sha <sha>] | --self-test");
      process.exit(2);
    }
    process.stdout.write(inputDigest({ set, base: arg(args, "--base") || "", sha: arg(args, "--sha") || "HEAD" }) + "\n");
  }
}
