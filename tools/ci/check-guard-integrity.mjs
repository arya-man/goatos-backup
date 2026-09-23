#!/usr/bin/env node
// Meta-guard: a guard that is REGISTERED is not the same as a guard that RUNS, and a guard that
// runs is not the same as a guard that DOES ANYTHING.
//
// The three meta-guards that already exist check names and files. A judge showed two ways to
// switch a guard off with all three still green:
//
//   empty its build target so it runs nothing   registration green, weakening green, inputs green
//   gut its script to "print ok, exit 0"        registration green, weakening green, inputs green
//
// In both the script, its manifest row, its target name and its CI step all survive, the step
// runs, and it exits clean. `check-guardrail-registration.mjs` counts NAMES: it never asks whether
// a target invokes its script, or whether a script does work. A third shape is the same family and
// is live rather than hypothetical: a target nothing invokes. Nothing is deleted there either - it
// was simply never connected - and 217 tests ran nowhere while CI stayed green.
//
// Three rules, each aimed at one of those:
//   A  a registered guard's build target must invoke that guard's own script
//   B  a guard's script must be shown to do work, by watching it READ something
//   C  a build target that no CI path reaches is not wired, however complete it looks
//
// Rule B deliberately looks at behaviour, never at the text of the script. A check that passes
// because a file is long enough, or because its bytes match a recorded hash, is the same
// false-green one layer over: the first is defeated by padding, the second by any honest edit.
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const MANIFEST = "tools/ci/guardrail-manifest.json";
const MAKEFILE = "Makefile";
const RUN_LOCAL_CI = "tools/ci/run-local-ci.sh";
const RECORDER = "tools/ci/lib/guard-probe-recorder.cjs";
const FLOOR_FILE = "tools/ci/guard-floor.json";

// Targets a CI run enters at. Everything else has to be reachable FROM one of these.
export const CI_ROOTS = ["guardrails", "check", "ci-local"];

// ---------------------------------------------------------------- Makefile

/** target -> { recipe: string[], invokes: string[] } for every target in a Makefile. */
export function parseMakefile(text) {
  const targets = new Map();
  let current = null;
  for (const raw of String(text ?? "").split("\n")) {
    if (raw.startsWith("\t")) {
      if (current) targets.get(current).recipe.push(raw.slice(1));
      continue;
    }
    const header = /^([A-Za-z0-9_.\-\/ ]+):(?!=)/.exec(raw);
    if (!header) {
      // A blank or comment line does not end a recipe in make, but a non-recipe,
      // non-target line does; treat anything else as ending the current recipe.
      if (raw.trim() === "" || raw.trimStart().startsWith("#")) continue;
      current = null;
      continue;
    }
    const names = header[1].trim().split(/\s+/);
    if (names[0] === ".PHONY") { current = null; continue; }
    for (const name of names) {
      if (!targets.has(name)) targets.set(name, { recipe: [], invokes: [] });
    }
    current = names[0];
  }
  for (const [, entry] of targets) {
    for (const line of entry.recipe) {
      for (const match of line.matchAll(/\$\(MAKE\)\s+([A-Za-z0-9_.\-]+)/g)) entry.invokes.push(match[1]);
      for (const match of line.matchAll(/\bmake\s+([A-Za-z0-9_.\-]+)/g)) entry.invokes.push(match[1]);
    }
  }
  return targets;
}

/** Targets a CI shell script enters directly, e.g. `step "x" make x-guard`. */
export function ciEntryTargets(runLocalCiText) {
  const found = new Set();
  for (const match of String(runLocalCiText ?? "").matchAll(/\bmake\s+([A-Za-z0-9_.\-]+)/g)) found.add(match[1]);
  return found;
}

export function reachableTargets(targets, roots) {
  const seen = new Set();
  const queue = [...roots];
  while (queue.length) {
    const name = queue.pop();
    if (seen.has(name) || !targets.has(name)) {
      seen.add(name);
      continue;
    }
    seen.add(name);
    for (const next of targets.get(name).invokes) if (!seen.has(next)) queue.push(next);
  }
  return seen;
}

// ------------------------------------------------- rule A: the target runs the script

/**
 * The recipe must actually invoke the guard's own script. An emptied recipe, or one that runs a
 * different script, is the mutation this rule exists for.
 */
export function targetInvokesItsScript(recipe, script) {
  const base = path.posix.basename(String(script ?? ""));
  if (!base) return false;
  return (recipe ?? []).some((line) => {
    const text = line.trim();
    if (!text || text.startsWith("#")) return false;
    // `@echo`-only recipes and `true`/`:` placeholders are exactly the emptied-target shape.
    return text.includes(script) || text.includes(base);
  });
}

// ------------------------------------------------- rule B: the script does work

/**
 * Runs the guard and reports which repo files it read. A guard gutted to print and exit reads
 * nothing; every real guard reads at least its own inputs. Its own script does not count, because
 * node reads that to run it at all.
 */
export function probeReads(command, { cwd = repo, timeoutMs = 120000, scriptPath = null } = {}) {
  const dir = mkdtempSync(path.join(os.tmpdir(), "goatos-guard-probe-"));
  const log = path.join(dir, "reads.txt");
  writeFileSync(log, "");
  try {
    const nodeOptions = `${process.env.NODE_OPTIONS ?? ""} --require ${path.join(repo, RECORDER)}`.trim();
    const shellTrace = /^\s*(bash|sh)\b/.test(command);
    const result = spawnSync("bash", [shellTrace ? "-xc" : "-c", command], {
      cwd,
      encoding: "utf8",
      timeout: timeoutMs,
      env: {
        ...process.env,
        NODE_OPTIONS: nodeOptions,
        GOATOS_GUARD_PROBE_LOG: log,
        GOATOS_GUARD_PROBE_ROOT: repo
      }
    });
    const fromNode = readFileSync(log, "utf8").split("\n").filter(Boolean);
    // A shell guard does its reading through grep/cat/find rather than through node, so its work
    // is read out of the execution trace instead.
    const fromShell = shellTrace ? repoPathsInTrace(result.stderr ?? "") : [];
    const reads = [...new Set([...fromNode, ...fromShell])]
      .filter((file) => !scriptPath || path.posix.normalize(file) !== path.posix.normalize(scriptPath));
    return { ran: result.error == null, status: result.status, reads, stderr: String(result.stderr ?? "").slice(-400) };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

/**
 * A shell guard reads through grep, cat and find rather than through node, so its work is read
 * out of the execution trace: any token on it that names a file really in the repo. Keyed on the
 * file existing, not on the path looking a particular way, so a guard that reads somewhere nobody
 * anticipated still counts as doing work.
 */
export function repoPathsInTrace(trace, { root = repo, fileExists = (file) => existsSync(file) } = {}) {
  const found = new Set();
  for (const token of String(trace ?? "").split(/[\s'"=()]+/)) {
    const candidate = token.replace(/^[+$]+/, "").trim();
    if (!candidate || candidate.startsWith("-")) continue;
    const full = path.isAbsolute(candidate) ? candidate : path.join(root, candidate);
    if (!full.startsWith(root)) continue;
    if (path.resolve(full) === path.resolve(root)) continue;
    if (fileExists(full)) found.add(path.relative(root, path.resolve(full)));
  }
  return [...found];
}

// ---------------------------------------------------------------- the check

export function validate({ manifest, makefileText, runLocalCiText, probe, changedFiles = null, extraReachable = [], floor = null, exists = (file) => existsSync(path.join(repo, file)) }) {
  const problems = [];
  const notes = [];
  const targets = parseMakefile(makefileText);
  const roots = new Set([...CI_ROOTS, ...ciEntryTargets(runLocalCiText)]);
  const reachable = reachableTargets(targets, [...roots]);
  const guards = manifest?.guards ?? [];
  if (!guards.length) {
    // Fail closed: an empty manifest must never read as "nothing to check".
    return { problems: ["the guardrail manifest lists no guards, so no guard could be checked"], notes, probed: 0 };
  }

  let probed = 0;
  for (const guard of guards) {
    const id = guard.id ?? "(unnamed)";
    const target = guard.makeTarget;

    // Rule A. The recipe must invoke this guard's own script.
    if (target) {
      const entry = targets.get(target);
      if (!entry) {
        problems.push(`guard ${id}: its build target "${target}" does not exist in the Makefile`);
      } else if (!targetInvokesItsScript(entry.recipe, guard.script)) {
        problems.push(`guard ${id}: its build target "${target}" never runs ${guard.script}, so the step passes without the guard`);
      }
      // Rule C, for a guard: the target has to be reachable from a CI entry point.
      if (entry && guard.requiredInCI !== false && !reachable.has(target)) {
        problems.push(`guard ${id}: no CI path reaches its build target "${target}", so it runs nowhere`);
      }
    }

    // Rule B. Watch the guard do work. Only for the guards this run is asked about, because
    // running all of them would slow every build.
    if (!probe) continue;
    if (changedFiles && !changedFiles.includes(guard.script)) continue;
    if (!guard.realCheck) {
      notes.push(`guard ${id}: declares no command to run, so it could not be watched doing work`);
      continue;
    }
    if (guard.script && !exists(guard.script)) continue; // the weakening guard owns deletion
    probed += 1;
    const seen = probe(guard.realCheck, guard.script);
    if (!seen.ran || seen.status === 127) {
      // Never report "read nothing" for a command that never started. That would be the same
      // false certainty this guard exists to remove, pointed the other way.
      notes.push(`guard ${id}: its command could not be run here, so it was not watched doing work`);
      continue;
    }
    if (seen.reads.length === 0) {
      // A guard that only looks at what a diff touched legitimately reads nothing when the diff
      // touches nothing. That is not the same as a guard that cannot read anything - but it is
      // also not a pass, so the log says which case it is instead of printing an unqualified ok.
      if (guard.diffScoped) {
        notes.push(`guard ${id}: examined no files in this run, because nothing it watches changed - NOT CHECKED, not clean`);
      } else {
        problems.push(`guard ${id}: ran and read nothing at all, so it cannot be checking anything`);
      }
    } else {
      // Count what was READ, never what was listed. Several guards print a file count taken from
      // their candidate list: with apps/ absent entirely one still printed "539 files scanned"
      // having opened none. A log that asserts a scope nobody examined is worse than a bare "ok".
      notes.push(`guard ${id}: read ${seen.reads.length} file(s) of the repo`);
    }
  }

  // Rule D. A guard removed WHOLE - script, manifest row, build target and CI step together -
  // leaves a tree that is internally consistent, so no snapshot check can see it, and once the
  // deletion is in main's history no later branch's diff contains it either. The only thing that
  // survives that is a floor on the count, the way the exception and telemetry ratchets already
  // hold whole-tree debt. Lowering it needs the acknowledgement line those use.
  if (typeof floor === "number") {
    if (guards.length < floor) {
      problems.push(`the manifest registers ${guards.length} guards and the recorded floor is ${floor}; ${floor - guards.length} guard(s) were removed whole, which nothing else can see. Raise the floor deliberately with a GUARD-WEAKENING-ACK line, or put them back.`);
    } else if (guards.length > floor) {
      notes.push(`the manifest now registers ${guards.length} guards, above the recorded floor of ${floor}; run with --update-floor to record the new one`);
    }
  }

  // Rule C, standalone: targets that must stay reachable whatever the manifest says.
  for (const target of extraReachable) {
    if (!targets.has(target)) {
      problems.push(`the target "${target}" is required to run in CI and does not exist`);
    } else if (!reachable.has(target)) {
      problems.push(`the target "${target}" exists and no CI path reaches it, so everything it runs runs nowhere`);
    }
  }

  return { problems, notes, probed };
}

// ---------------------------------------------------------------- entry point

const args = parse(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : run());

function registeredGuardFloor() {
  const file = path.join(repo, FLOOR_FILE);
  if (!existsSync(file)) throw new Error("the recorded floor on how many guards are registered is missing, so a whole guard could be removed unseen");
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  const floor = Number(parsed?.minimumRegisteredGuards);
  if (!Number.isFinite(floor) || floor <= 0) throw new Error("the recorded floor on how many guards are registered is not a number, so it protects nothing");
  return floor;
}

function mustRunInCi() {
  const file = path.join(repo, "tools/ci/must-run-in-ci.json");
  if (!existsSync(file)) {
    throw new Error("the list of targets that must run in CI is missing, so nothing could be checked");
  }
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  if (!Array.isArray(parsed?.targets) || parsed.targets.length === 0) {
    throw new Error("the list of targets that must run in CI is empty, so nothing could be checked");
  }
  return parsed.targets.map((row) => (typeof row === "string" ? row : row.target));
}

function run() {
  const manifest = JSON.parse(readFileSync(path.join(repo, MANIFEST), "utf8"));
  const makefileText = readFileSync(path.join(repo, MAKEFILE), "utf8");
  const runLocalCiText = readFileSync(path.join(repo, RUN_LOCAL_CI), "utf8");
  const changedFiles = args.all ? null : changedAgainstBase();
  const { problems, notes, probed } = validate({
    manifest,
    makefileText,
    runLocalCiText,
    probe: args.noProbe ? null : (command, script) => probeReads(command, { scriptPath: script }),
    changedFiles,
    extraReachable: mustRunInCi(),
    floor: registeredGuardFloor()
  });
  if (args.updateFloor) {
    const file = path.join(repo, FLOOR_FILE);
    const current = JSON.parse(readFileSync(file, "utf8"));
    writeFileSync(file, `${JSON.stringify({ ...current, minimumRegisteredGuards: manifest.guards.length }, null, 2)}\n`);
    console.log(`recorded a floor of ${manifest.guards.length} registered guards`);
    return 0;
  }
  for (const note of notes) console.log(`note: ${note}`);
  if (problems.length) {
    console.error("guard integrity: a guard can be switched off without anything noticing");
    for (const problem of problems) console.error(`  - ${problem}`);
    return 1;
  }
  console.log(`guard integrity: every registered guard's target runs its own script, every target that must run in CI is reachable, and ${probed} guard(s) were watched doing real work`);
  return 0;
}

function changedAgainstBase() {
  const base = process.env.GOATOS_GUARD_INTEGRITY_BASE || "origin/main";
  const result = spawnSync("git", ["diff", "--name-only", `${base}...HEAD`], { cwd: repo, encoding: "utf8" });
  if (result.status !== 0) return null; // no base to compare against: probe everything rather than nothing
  const committed = result.stdout.split("\n").filter(Boolean);
  const working = spawnSync("git", ["status", "--porcelain"], { cwd: repo, encoding: "utf8" });
  const dirty = (working.stdout ?? "").split("\n").filter(Boolean).map((line) => line.slice(3).trim());
  return [...new Set([...committed, ...dirty])];
}

function parse(raw) {
  const parsed = {};
  for (const arg of raw) {
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--all") parsed.all = true;
    else if (arg === "--no-probe") parsed.noProbe = true;
    else if (arg === "--update-floor") parsed.updateFloor = true;
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  const makefile = [
    "guardrails:",
    "\t$(MAKE) a-guard",
    "\t$(MAKE) b-guard",
    "",
    "a-guard:",
    "\tnode tools/ci/check-a.mjs",
    "",
    "b-guard:",
    "\tnode tools/ci/check-b.mjs",
    "",
    "orphan-target:",
    "\tnode --test tools/orphan/",
    ""
  ].join("\n");
  const runLocalCi = 'step "guardrails" make guardrails\n';
  const manifest = {
    guards: [
      { id: "a", script: "tools/ci/check-a.mjs", makeTarget: "a-guard", realCheck: "node tools/ci/check-a.mjs", requiredInCI: true },
      { id: "b", script: "tools/ci/check-b.mjs", makeTarget: "b-guard", realCheck: "node tools/ci/check-b.mjs", requiredInCI: true }
    ]
  };
  const reads = () => ({ ran: true, status: 0, reads: ["backend/x.go"] });
  const exists = () => true;
  const readsNothing = () => ({ ran: true, status: 0, reads: [] });
  const clean = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads , exists });
  if (clean.problems.length) throw new Error(`self-test: a wired guard must pass, got ${clean.problems.join("; ")}`);

  // Row 2 of the table: the target still exists and runs nothing.
  const emptied = makefile.replace("a-guard:\n\tnode tools/ci/check-a.mjs", "a-guard:\n\t@echo ok");
  const r2 = validate({ manifest, makefileText: emptied, runLocalCiText: runLocalCi, probe: reads , exists });
  if (!r2.problems.some((p) => p.includes("never runs tools/ci/check-a.mjs"))) {
    throw new Error("self-test: an emptied build target must be caught");
  }
  // A recipe of `true`, or `:`, is the same emptying written another way.
  for (const nothing of ["\ttrue", "\t:", "\t@true"]) {
    const variant = makefile.replace("a-guard:\n\tnode tools/ci/check-a.mjs", `a-guard:\n${nothing}`);
    const result = validate({ manifest, makefileText: variant, runLocalCiText: runLocalCi, probe: reads , exists });
    if (!result.problems.some((p) => p.includes("never runs"))) {
      throw new Error(`self-test: an emptied build target written as ${JSON.stringify(nothing.trim())} must be caught`);
    }
  }

  // Row 4 of the table: the script is there, the target runs it, and it does nothing.
  const r4 = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: readsNothing , exists });
  if (!r4.problems.some((p) => p.includes("read nothing at all"))) {
    throw new Error("self-test: a guard that reads nothing must be caught");
  }

  // A target nothing reaches: the live shape, where nothing was deleted at all.
  const orphan = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, extraReachable: ["orphan-target"] , exists });
  if (!orphan.problems.some((p) => p.includes("no CI path reaches it"))) {
    throw new Error("self-test: a target no CI path reaches must be caught");
  }
  const wired = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, extraReachable: ["a-guard"] , exists });
  if (wired.problems.length) throw new Error("self-test: a reachable target must pass");
  const absent = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, extraReachable: ["no-such-target"] , exists });
  if (!absent.problems.some((p) => p.includes("does not exist"))) {
    throw new Error("self-test: a required target that does not exist must be caught");
  }

  // A guard whose target is unreachable is off, however well registered it is.
  const unreachable = makefile.replace("\t$(MAKE) b-guard\n", "");
  const r5 = validate({ manifest, makefileText: unreachable, runLocalCiText: runLocalCi, probe: reads , exists });
  if (!r5.problems.some((p) => p.includes("no CI path reaches its build target"))) {
    throw new Error("self-test: a guard whose target nothing reaches must be caught");
  }

  // The floor sees a guard removed whole, which nothing else can.
  const shrunk = { guards: [manifest.guards[0]] };
  const below = validate({ manifest: shrunk, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, floor: 2 });
  if (!below.problems.some((p) => p.includes("removed whole"))) {
    throw new Error("self-test: a guard removed whole must be caught by the floor");
  }
  if (validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, floor: 2 }).problems.length) {
    throw new Error("self-test: a manifest at its floor must pass");
  }

  // A guard that only reads what a diff touched is "not checked", not "clean" - and not a failure.
  const diffScoped = { guards: [{ ...manifest.guards[0], diffScoped: true }] };
  const scoped = validate({ manifest: diffScoped, makefileText: makefile, runLocalCiText: runLocalCi, probe: readsNothing, exists });
  if (scoped.problems.some((p) => p.includes("read nothing"))) {
    throw new Error("self-test: a diff-scoped guard with nothing to look at must not be a failure");
  }
  if (!scoped.notes.some((n) => n.includes("NOT CHECKED"))) {
    throw new Error("self-test: a guard that examined nothing must say so, not print a clean verdict");
  }

  // A command that cannot run at all must be a note, never "read nothing".
  const cannotRun = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, exists, probe: () => ({ ran: true, status: 127, reads: [] }) });
  if (cannotRun.problems.some((p) => p.includes("read nothing"))) {
    throw new Error("self-test: a command that never started must not be reported as reading nothing");
  }

  // Fail closed on an empty manifest.
  if (!validate({ manifest: { guards: [] }, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads }).problems.length) {
    throw new Error("self-test: an empty manifest must refuse, not pass");
  }

  // The probe itself: watch a script that reads, and one that does not.
  const dir = mkdtempSync(path.join(os.tmpdir(), "goatos-guard-integrity-"));
  try {
    const worker = path.join(dir, "worker.mjs");
    writeFileSync(worker, `import { readFileSync } from "node:fs";\nreadFileSync(${JSON.stringify(path.join(repo, "Makefile"))});\n`);
    const gutted = path.join(dir, "gutted.mjs");
    writeFileSync(gutted, 'console.log("ok");\n');
    if (probeReads(`node ${worker}`).reads.length === 0) throw new Error("self-test: the probe must see a guard that reads");
    if (probeReads(`node ${gutted}`).reads.length !== 0) throw new Error("self-test: the probe must see that a gutted guard reads nothing");
    const shellWorker = path.join(dir, "worker.sh");
    writeFileSync(shellWorker, 'grep -q PHONY "$1"\n');
    if (probeReads(`bash ${shellWorker} Makefile`).reads.length === 0) {
      throw new Error("self-test: the probe must see a shell guard read");
    }
    const shellGutted = path.join(dir, "gutted.sh");
    writeFileSync(shellGutted, 'echo ok\nexit 0\n');
    if (probeReads(`bash ${shellGutted}`).reads.length !== 0) {
      throw new Error("self-test: the probe must see that a gutted shell guard reads nothing");
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }

  // The Makefile reader must find a real target and its recipe.
  const real = parseMakefile(readFileSync(path.join(repo, MAKEFILE), "utf8"));
  if (!real.has("guardrails")) throw new Error("self-test: the Makefile reader must find guardrails");
  if (!real.get("guardrails").invokes.length) throw new Error("self-test: guardrails must be read as invoking other targets");

  console.log("guard integrity: self-test passed");
  return 0;
}
