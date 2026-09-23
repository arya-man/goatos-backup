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
import { fileURLToPath, pathToFileURL } from "node:url";
import { writeShims } from "./lib/guard-probe-shims.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const MANIFEST = "tools/ci/guardrail-manifest.json";
const MAKEFILE = "Makefile";
const RUN_LOCAL_CI = "tools/ci/run-local-ci.sh";
const RECORDER = "tools/ci/lib/guard-probe-recorder.cjs";
const LOADER = "tools/ci/lib/guard-probe-loader.mjs";
// The trees a guard checks the product in. Reading package.json at the root is not reading the
// product - which is exactly the one-line escape this keeps closed.
export const SOURCE_TREES = ["apps", "backend", "contracts", "tools", "docs", "context", "infra", "analytics", "packages", "fixtures", "mock"];
const FLOOR_FILE = "tools/ci/guard-floor.json";
const INPUTS_FILE = "tools/ci/guard-inputs.json";

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
export function probeReads(command, { cwd = repo, timeoutMs = 180000, scriptPath = null, watchModules = false } = {}) {
  const dir = mkdtempSync(path.join(os.tmpdir(), "goatos-guard-probe-"));
  const log = path.join(dir, "reads.txt");
  const shimLog = path.join(dir, "shim-args.txt");
  writeFileSync(log, "");
  writeFileSync(shimLog, "");
  try {
    const shimDir = writeShims(path.join(dir, "bin"), { log: shimLog, realPath: process.env.PATH ?? "" });
    // The module loader is only added for the SECOND look, because it costs a resolve hook on
    // every import; the first pass stays cheap.
    const loaderFlag = watchModules ? ` --import ${JSON.stringify(pathToFileURL(path.join(repo, LOADER)).href)}` : "";
    const nodeOptions = `${process.env.NODE_OPTIONS ?? ""} --require ${path.join(repo, RECORDER)}${loaderFlag}`.trim();
    const result = spawnSync("bash", ["-c", command], {
      cwd,
      encoding: "utf8",
      timeout: timeoutMs,
      env: {
        ...process.env,
        // Both of these reach CHILD processes on their own, which is the whole point: a guard's
        // reading is often not done by the guard's own process.
        PATH: `${shimDir}:${process.env.PATH ?? ""}`,
        NODE_OPTIONS: nodeOptions,
        GOATOS_GUARD_PROBE_LOG: log,
        GOATOS_GUARD_PROBE_ROOT: repo
      }
    });
    const fromNode = readFileSync(log, "utf8").split("\n").filter(Boolean);
    const fromShims = repoPathsInTrace(readFileSync(shimLog, "utf8"));
    const reads = [...new Set([...fromNode, ...fromShims])]
      .map((file) => path.posix.normalize(file))
      .filter((file) => !scriptPath || file !== path.posix.normalize(scriptPath));
    return {
      ran: result.error == null,
      status: result.status,
      timedOut: result.error?.code === "ETIMEDOUT",
      reads,
      stderr: String(result.stderr ?? "").slice(-400)
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

export function repoPathsInTrace(trace, { root = repo, fileExists = (file) => existsSync(file) } = {}) {
  const found = new Set();
  // One argument per line from the command wrappers, plus whitespace splitting for anything that
  // arrived as a single string. Keyed on the file EXISTING, never on the path looking a particular
  // way, so a guard reading somewhere nobody anticipated still counts.
  for (const token of String(trace ?? "").split(/[\n\s'"=()]+/)) {
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

/**
 * What counts as a guard doing its job. NOT "it opened a file": a guard gutted to read
 * package.json, print ok and exit opens a file, and walked straight past the first version of this
 * rule. A guard's own declared inputs are the files it is supposed to look at, so reading one of
 * THOSE is evidence about the thing being checked rather than evidence that a process ran.
 *
 * A guard that declares no inputs cannot be judged this way at all. That is said out loud and
 * counted, never rounded into a pass.
 */
export function workVerdict(guard, seen, declaredInputs) {
  if (!seen.ran || seen.status === 127) {
    return { kind: "not-run", reason: "its command could not be run here, so it was not watched doing work" };
  }
  if (seen.timedOut) {
    return { kind: "not-run", reason: "its command did not finish in time, so it was not watched doing work" };
  }
  const declared = (declaredInputs ?? []).map((file) => path.posix.normalize(file));
  const read = new Set(seen.reads.map((file) => path.posix.normalize(file)));
  if (!declared.length) {
    // REFUSED, not "not checked" and never a pass. A criterion that returns green for a third of
    // the guard set is the "ok having examined nothing" shape this whole check exists to catch,
    // arriving inside the thing built to catch it. A guard must say what it reads.
    //
    // The 42 that say nothing today are carried on a SHRINK-ONLY list, the way the exception and
    // telemetry ratchets carry whole-tree debt: the debt is visible, it can only go down, and a
    // NEW guard without declared inputs is refused outright.
    return {
      kind: "undeclared",
      reason: `declares no inputs, so nothing it opened is evidence it checked anything (it opened ${seen.reads.length} file(s), which proves only that it ran)`
    };
  }
  // A directory counts for the inputs under it: a guard that walks a tree reads the children.
  const hit = declared.filter((input) => read.has(input) || [...read].some((file) => file.startsWith(`${input}/`) || input.startsWith(`${file}/`)));
  if (hit.length) return { kind: "worked", how: "declared", read: hit.length, of: declared.length };

  // A declared input is the STRONGEST evidence, not the only evidence, and assuming otherwise
  // produced a false accusation: check-ui-vaccine-labels reads 1522 files it finds by globbing and
  // none of them is the single path declared for it. guard-inputs.json lists paths HARD-CODED IN a
  // guard, which is not the same thing as the files it reads - a weaker declaration than its name
  // suggests. So a guard that read the product's own source is credited, and the receipt says
  // which of the two kinds of evidence it had.
  const inSource = [...read].filter((file) => SOURCE_TREES.some((tree) => file.startsWith(`${tree}/`)));
  if (inSource.length) {
    return { kind: "worked", how: "source", read: inSource.length, of: declared.length };
  }
  return {
    kind: "no-work",
    reason: `ran and read none of the ${declared.length} file(s) it declares it checks, and nothing under the product's own source either, so whatever it opened, it is not checking them`
  };
}

export function validate({ manifest, makefileText, runLocalCiText, probe, changedFiles = null, extraReachable = [], floor = null, guardInputs = new Map(), undeclaredAllowed = new Set(), noTargetAllowed = new Set(), reprobe = null, exists = (file) => existsSync(path.join(repo, file)) }) {
  const problems = [];
  const notes = [];
  // Named out loud rather than folded into the pass: guards this mechanism cannot judge.
  const unjudged = [];
  const noTarget = [];
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
    if (!target) {
      // Two of the three rules read the build target, so a guard without one is outside them.
      // Same answer as for undeclared inputs: carried on a shrink-only list, never silent, and a
      // NEW guard without a target is refused.
      if (noTargetAllowed.has(guard.script)) {
        noTarget.push(id);
      } else {
        problems.push(`guard ${id}: declares no build target, so nothing can check that a CI path reaches it or that anything runs its script. Give it a target.`);
      }
    }
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
    let seen = probe(guard.realCheck, guard.script);
    let verdict = workVerdict(guard, seen, guardInputs.get(guard.script));
    if (verdict.kind === "no-work" && typeof reprobe === "function") {
      // NEVER accuse on the first look. A guard that reaches its inputs by importing them is
      // invisible to a probe that watches file reads, and reporting that as "checks nothing"
      // would be a check firing on correct work - which is the failure this rule was corrected
      // for once already. So the second look happens before the accusation, not after it.
      seen = reprobe(guard.realCheck, guard.script);
      verdict = workVerdict(guard, seen, guardInputs.get(guard.script));
    }
    if (verdict.kind === "not-run") {
      // Never report "read nothing" for a command that never started. That would be the same
      // false certainty this guard exists to remove, pointed the other way.
      notes.push(`guard ${id}: ${verdict.reason}`);
    } else if (verdict.kind === "undeclared") {
      if (undeclaredAllowed.has(guard.script)) {
        unjudged.push({ id, reason: verdict.reason });
      } else {
        problems.push(`guard ${id}: ${verdict.reason}. Declare what it reads in ${INPUTS_FILE}.`);
      }
    } else if (verdict.kind === "no-work") {
      // A guard that only looks at what a diff touched legitimately reads none of them when the
      // diff touches nothing. That is not clean and not a failure: it is NOT CHECKED, and saying
      // so is the whole point.
      if (guard.diffScoped) {
        notes.push(`guard ${id}: read none of the files it checks, because nothing it watches changed - NOT CHECKED, not clean`);
      } else {
        problems.push(`guard ${id}: ${verdict.reason}`);
      }
    } else {
      // Count what was READ, never what was listed. Several guards print a file count taken from
      // their candidate list: with apps/ absent entirely one still printed "539 files scanned"
      // having opened none. A log that asserts a scope nobody examined is worse than a bare "ok".
      notes.push(verdict.how === "declared"
        ? `guard ${id}: read ${verdict.read} of the ${verdict.of} file(s) it declares it checks`
        : `guard ${id}: read ${verdict.read} file(s) of the product's own source, though none of the ${verdict.of} path(s) declared for it - weaker evidence, and the declaration is probably stale`);
    }
  }

  // Rule D. A guard removed WHOLE - script, manifest row, build target and CI step together -
  // leaves a tree that is internally consistent, so no snapshot check can see it, and once the
  // deletion is in main's history no later branch's diff contains it either. The only thing that
  // survives that is a floor on the count, the way the exception and telemetry ratchets already
  // hold whole-tree debt. Lowering it needs the acknowledgement line those use.
  // Counted as DISTINCT SCRIPTS, never as rows. Counting rows is padded by adding a second row
  // pointing at a script that is already registered: delete a guard whole, add the duplicate, and
  // the total is unchanged. What the floor is protecting is how many things are actually guarded.
  const scripts = new Set(guards.map((guard) => guard.script).filter(Boolean));
  const duplicated = new Map();
  for (const guard of guards) {
    if (!guard.script) continue;
    duplicated.set(guard.script, (duplicated.get(guard.script) ?? 0) + 1);
  }
  for (const [script, count] of duplicated) {
    if (count > 1) notes.push(`${count} manifest rows point at ${script}; the floor counts it once`);
  }
  if (typeof floor === "number") {
    if (scripts.size < floor) {
      problems.push(`the manifest registers ${scripts.size} distinct guard scripts and the recorded floor is ${floor}; ${floor - scripts.size} guard(s) were removed whole, which nothing else can see once the deletion is in main. Raise the floor deliberately with a GUARD-WEAKENING-ACK line, or put them back.`);
    } else if (scripts.size > floor) {
      notes.push(`the manifest now registers ${scripts.size} distinct guard scripts, above the recorded floor of ${floor}; run with --update-floor to record the new one`);
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

  return { problems, notes, probed, unjudged, noTarget };
}

// ---------------------------------------------------------------- entry point

const args = parse(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : run());

/** What each guard says it reads. Its own answer, so judging it against this is not a new rule. */
function declaredInputs() {
  const file = path.join(repo, INPUTS_FILE);
  if (!existsSync(file)) throw new Error("the declared guard inputs are missing, so no guard can be shown to have looked at anything");
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  const map = new Map();
  for (const row of parsed?.guards ?? []) if (row?.guard) map.set(row.guard, row.inputs ?? []);
  if (!map.size) throw new Error("the declared guard inputs are empty, so no guard can be shown to have looked at anything");
  return map;
}

/**
 * The debt: guards that cannot be judged yet. Shrink-only - the recorded count is a ceiling, so
 * the list can lose members and never gain one, and a new guard has to declare itself properly.
 */
function carriedDebt() {
  const file = path.join(repo, FLOOR_FILE);
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  return {
    undeclaredAllowed: new Set(parsed?.guardsWithNoDeclaredInputs ?? []),
    noTargetAllowed: new Set(parsed?.guardsWithNoBuildTarget ?? [])
  };
}

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
  const result = validate({
    manifest,
    makefileText,
    runLocalCiText,
    probe: args.noProbe ? null : (command, script) => probeReads(command, { scriptPath: script }),
    reprobe: args.noProbe ? null : (command, script) => probeReads(command, { scriptPath: script, watchModules: true }),
    changedFiles,
    extraReachable: mustRunInCi(),
    floor: registeredGuardFloor(),
    guardInputs: declaredInputs(),
    ...carriedDebt()
  });
  const { problems, notes, probed } = result;
  if (args.updateFloor) {
    const file = path.join(repo, FLOOR_FILE);
    const current = JSON.parse(readFileSync(file, "utf8"));
    const inputs = declaredInputs();
    const scripts = [...new Set(manifest.guards.map((g) => g.script).filter(Boolean))];
    const next = {
      ...current,
      minimumRegisteredGuards: scripts.length,
      // Derived from the tree, never hand-listed: a hand-written list is the census defect again.
      guardsWithNoDeclaredInputs: scripts.filter((script) => !(inputs.get(script) ?? []).length).sort(),
      guardsWithNoBuildTarget: [...new Set(manifest.guards.filter((g) => !g.makeTarget).map((g) => g.script))].sort()
    };
    // An absent list has never been recorded, so the first run establishes it. A list that IS
    // recorded may only shrink.
    const grew = (before, after) => Array.isArray(before) && after.length > before.length;
    if (grew(current.guardsWithNoDeclaredInputs, next.guardsWithNoDeclaredInputs) ||
        grew(current.guardsWithNoBuildTarget, next.guardsWithNoBuildTarget)) {
      console.error("refusing to record a LARGER debt list: these may only shrink. Declare the new guard's inputs and give it a build target instead.");
      return 1;
    }
    writeFileSync(file, `${JSON.stringify(next, null, 2)}\n`);
    console.log(`recorded a floor of ${scripts.length} distinct guard scripts, ${next.guardsWithNoDeclaredInputs.length} with no declared inputs, ${next.guardsWithNoBuildTarget.length} with no build target`);
    return 0;
  }
  for (const note of notes) console.log(`note: ${note}`);
  // Said out loud, every run, instead of being folded into the pass. These are the guards this
  // mechanism cannot speak for; a reader must be able to tell them from the ones it cleared.
  if (result.unjudged.length) {
    console.log(`CARRIED DEBT — ${result.unjudged.length} guard(s) declare no inputs, so nothing they open is evidence they check anything. This list may only shrink; a new guard without declared inputs is refused.`);
    for (const row of result.unjudged) console.log(`  - ${row.id}`);
  }
  if (result.noTarget.length) {
    console.log(`CARRIED DEBT — ${result.noTarget.length} guard(s) declare no build target, so the target and reachability rules do not reach them. This list may only shrink: ${result.noTarget.join(", ")}`);
  }
  if (problems.length) {
    console.error("guard integrity: a guard can be switched off without anything noticing");
    for (const problem of problems) console.error(`  - ${problem}`);
    return 1;
  }
  console.log(`guard integrity: every registered guard's target runs its own script, every target that must run in CI is reachable, and ${probed} guard(s) were watched reading the files they declare they check`);
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
  // Every synthetic guard declares what it reads, because that is now the criterion.
  const guardInputs = new Map([
    ["tools/ci/check-a.mjs", ["backend/x.go", "backend/y.go"]],
    ["tools/ci/check-b.mjs", ["backend/z.go"]]
  ]);
  const reads = () => ({ ran: true, status: 0, reads: ["backend/x.go", "backend/z.go"] });
  const exists = () => true;
  const readsNothing = () => ({ ran: true, status: 0, reads: [] });
  const clean = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads , exists, guardInputs });
  if (clean.problems.length) throw new Error(`self-test: a wired guard must pass, got ${clean.problems.join("; ")}`);

  // Row 2 of the table: the target still exists and runs nothing.
  const emptied = makefile.replace("a-guard:\n\tnode tools/ci/check-a.mjs", "a-guard:\n\t@echo ok");
  const r2 = validate({ manifest, makefileText: emptied, runLocalCiText: runLocalCi, probe: reads , exists, guardInputs });
  if (!r2.problems.some((p) => p.includes("never runs tools/ci/check-a.mjs"))) {
    throw new Error("self-test: an emptied build target must be caught");
  }
  // A recipe of `true`, or `:`, is the same emptying written another way.
  for (const nothing of ["\ttrue", "\t:", "\t@true"]) {
    const variant = makefile.replace("a-guard:\n\tnode tools/ci/check-a.mjs", `a-guard:\n${nothing}`);
    const result = validate({ manifest, makefileText: variant, runLocalCiText: runLocalCi, probe: reads , exists, guardInputs });
    if (!result.problems.some((p) => p.includes("never runs"))) {
      throw new Error(`self-test: an emptied build target written as ${JSON.stringify(nothing.trim())} must be caught`);
    }
  }

  // Row 4 of the table: the script is there, the target runs it, and it does nothing.
  const r4 = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: readsNothing, exists, guardInputs });
  if (!r4.problems.some((p) => p.includes("read none of the"))) {
    throw new Error("self-test: a guard that reads none of the files it checks must be caught");
  }

  // THE ESCAPE THAT WALKED PAST THE FIRST VERSION, one line further on than the naive gutting:
  // a guard that opens package.json, prints ok and exits. It reads A file, so "opened something"
  // cleared it. It reads none of ITS OWN declared inputs, which is what is asked now.
  // Root files, deliberately: package.json is not the product. A guard that opens it has still
  // read none of the trees it is supposed to be checking.
  const opensSomethingElse = () => ({ ran: true, status: 0, reads: ["package.json", "README.md"] });
  const walkedPast = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: opensSomethingElse, exists, guardInputs });
  if (!walkedPast.problems.some((p) => p.includes("read none of the"))) {
    throw new Error("self-test: a guard that opens an unrelated file must not count as doing work");
  }

  // The weaker evidence is credited, and named as weaker.
  const globsInstead = validate({
    manifest, makefileText: makefile, runLocalCiText: runLocalCi, exists, guardInputs,
    probe: () => ({ ran: true, status: 0, reads: ["apps/admin-web/app/page.tsx", "apps/admin-web/lib/x.ts"] })
  });
  if (globsInstead.problems.length) {
    throw new Error(`self-test: a guard that read the product's source must not be accused, got ${globsInstead.problems.join("; ")}`);
  }
  if (!globsInstead.notes.some((n) => n.includes("weaker evidence"))) {
    throw new Error("self-test: weaker evidence must be reported as weaker, not as a clean pass");
  }

  // A guard that declares nothing is REFUSED unless it is on the shrink-only debt list.
  const undeclared = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, guardInputs: new Map() });
  if (!undeclared.problems.some((p) => p.includes("declares no inputs"))) {
    throw new Error("self-test: a guard that declares no inputs must be refused, not passed");
  }
  const carried = validate({
    manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists,
    guardInputs: new Map(), undeclaredAllowed: new Set(["tools/ci/check-a.mjs", "tools/ci/check-b.mjs"])
  });
  if (carried.problems.some((p) => p.includes("declares no inputs"))) {
    throw new Error("self-test: a guard on the carried-debt list must not fail the build");
  }
  if (carried.unjudged.length !== 2) throw new Error("self-test: carried debt must be reported, not hidden");

  // A guard with no build target is refused the same way, and carried the same way.
  const noTargetManifest = { guards: [{ ...manifest.guards[0], makeTarget: null }] };
  const refusedNoTarget = validate({ manifest: noTargetManifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, guardInputs });
  if (!refusedNoTarget.problems.some((p) => p.includes("declares no build target"))) {
    throw new Error("self-test: a guard with no build target must be refused, not skipped");
  }
  const carriedNoTarget = validate({
    manifest: noTargetManifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists,
    guardInputs, noTargetAllowed: new Set(["tools/ci/check-a.mjs"])
  });
  if (carriedNoTarget.problems.some((p) => p.includes("declares no build target"))) {
    throw new Error("self-test: a guard with no build target on the carried list must not fail the build");
  }

  // An accusation must never be made on the first look alone.
  let secondLooks = 0;
  const cleared = validate({
    manifest, makefileText: makefile, runLocalCiText: runLocalCi, exists, guardInputs,
    probe: () => ({ ran: true, status: 0, reads: ["package.json"] }),
    reprobe: () => { secondLooks += 1; return { ran: true, status: 0, reads: ["backend/x.go", "backend/z.go"] }; }
  });
  if (!secondLooks) throw new Error("self-test: a guard about to be accused must get a second look first");
  if (cleared.problems.some((p) => p.includes("read none of the"))) {
    throw new Error("self-test: a guard that reaches its inputs by importing them must not be accused");
  }

  // A timed-out command is "not watched", never "read nothing".
  const timedOut = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, exists, guardInputs, probe: () => ({ ran: false, timedOut: true, status: null, reads: [] }) });
  if (timedOut.problems.some((p) => p.includes("read none"))) {
    throw new Error("self-test: a command that did not finish must not be reported as reading nothing");
  }

  // A target nothing reaches: the live shape, where nothing was deleted at all.
  const orphan = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, extraReachable: ["orphan-target"] , exists, guardInputs });
  if (!orphan.problems.some((p) => p.includes("no CI path reaches it"))) {
    throw new Error("self-test: a target no CI path reaches must be caught");
  }
  const wired = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, extraReachable: ["a-guard"] , exists, guardInputs });
  if (wired.problems.length) throw new Error("self-test: a reachable target must pass");
  const absent = validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, extraReachable: ["no-such-target"] , exists, guardInputs });
  if (!absent.problems.some((p) => p.includes("does not exist"))) {
    throw new Error("self-test: a required target that does not exist must be caught");
  }

  // A guard whose target is unreachable is off, however well registered it is.
  const unreachable = makefile.replace("\t$(MAKE) b-guard\n", "");
  const r5 = validate({ manifest, makefileText: unreachable, runLocalCiText: runLocalCi, probe: reads , exists, guardInputs });
  if (!r5.problems.some((p) => p.includes("no CI path reaches its build target"))) {
    throw new Error("self-test: a guard whose target nothing reaches must be caught");
  }

  // THE PADDING ESCAPE: delete a guard whole and add a duplicate row pointing at a script that is
  // already registered. Row counts are unchanged; distinct scripts are not.
  const padded = { guards: [manifest.guards[0], { ...manifest.guards[0], id: "a-copy" }] };
  const paddedResult = validate({ manifest: padded, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, guardInputs, floor: 2 });
  if (!paddedResult.problems.some((p) => p.includes("removed whole"))) {
    throw new Error("self-test: a duplicate row must not be able to pad the floor back up");
  }

  // The floor sees a guard removed whole, which nothing else can.
  const shrunk = { guards: [manifest.guards[0]] };
  const below = validate({ manifest: shrunk, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, floor: 2 });
  if (!below.problems.some((p) => p.includes("removed whole"))) {
    throw new Error("self-test: a guard removed whole must be caught by the floor");
  }
  if (validate({ manifest, makefileText: makefile, runLocalCiText: runLocalCi, probe: reads, exists, guardInputs, floor: 2 }).problems.length) {
    throw new Error("self-test: a manifest at its floor must pass");
  }

  // A guard that only reads what a diff touched is "not checked", not "clean" - and not a failure.
  const diffScoped = { guards: [{ ...manifest.guards[0], diffScoped: true }] };
  const scoped = validate({ manifest: diffScoped, makefileText: makefile, runLocalCiText: runLocalCi, probe: readsNothing, exists, guardInputs });
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
