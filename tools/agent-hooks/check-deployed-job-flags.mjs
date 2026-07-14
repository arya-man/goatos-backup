#!/usr/bin/env node
// check-deployed-job-flags.mjs — KERN-001 guard.
//
// Every Cloud Run Job command/args pair declared in infra/envs/*/cloud_run_jobs.tf
// must resolve to (a) a binary that is actually built into a backend/Dockerfile*
// image, AND (b) a flag parser (backend/cmd/<binary>/*.go, non-test) that defines
// every flag the job passes. Without this guard, a job can silently reference a
// deleted binary or a retired flag: the container either fails to start ("no such
// file or directory") or starts and immediately exits via
// `flag provided but not defined: -X`, and neither failure mode is caught by
// `go build ./...` (Terraform args are plain strings, not Go, so the compiler
// cannot see this class of bug).
//
// This is exactly the class of defect found in KERN-001: obligation-sweeper's
// deployed job passed `-project-calendar` / `-project-vaccination-read-models`,
// two flags removed from backend/cmd/obligation-sweeper/main.go when the
// Calendar/vaccination screen projections were dropped, so the job never ran and
// calendar reminders + escalations (which the same binary is also responsible
// for, via -sweep-reminders/-sweep-escalations) had no runnable owner. Separately,
// four job blocks (process_integrity_projector, vaccination_execution_projector,
// vaccination_operations_projector, vaccination_projection_worker) referenced
// binaries deleted by an earlier commit (cb6fd35e, migration 000187) and were
// never removed from Terraform.
//
// Deterministic, offline: parses Terraform + Dockerfile + Go source as text. No
// `terraform plan`/`apply`, no cloud calls, no `go run`.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { resolve, join } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// ---------------------------------------------------------------------------
// Pure parsing helpers (unit-tested via --self-test below with fixture strings,
// not real files).
// ---------------------------------------------------------------------------

// Dockerfile that loops `for cmd in a b c; do ... ./cmd/${cmd} ...`.
function binariesFromLoopDockerfile(text) {
  const m = text.match(/for\s+cmd\s+in\s+([^;]+);\s*do/);
  if (!m) return [];
  return m[1].trim().split(/\s+/).filter(Boolean);
}

// Dockerfile that builds one literal binary, e.g. `-o /out/migrate ./cmd/migrate`
// or `ENTRYPOINT ["/app/bin/migrate"]` with no shell variable in the path.
function binariesFromLiteralDockerfile(text) {
  const found = new Set();
  for (const m of text.matchAll(/\.\/cmd\/([a-zA-Z0-9_-]+)(?!\$|\{)/g)) {
    found.add(m[1]);
  }
  return [...found];
}

// Returns the set of binary names actually built across every backend/Dockerfile*.
function buildableBinaries(dockerfileTexts) {
  const set = new Set();
  for (const text of dockerfileTexts) {
    for (const name of binariesFromLoopDockerfile(text)) set.add(name);
    for (const name of binariesFromLiteralDockerfile(text)) set.add(name);
  }
  return set;
}

// Extracts { binary, argsRaw }[] pairs from a cloud_run_jobs.tf source: every
// place a literal `command = ["/app/bin/NAME", ...]` is immediately followed by
// a literal `args = [...]`. Skips `command = each.value.command` (for_each
// interpolation) since those resolve through the kernel_jobs map, which is
// itself scanned as separate literal blocks in the same file.
function extractJobs(tfText) {
  const jobs = [];
  const re = /command\s*=\s*\[\s*"\/app\/bin\/([a-zA-Z0-9_-]+)"[^\]]*\]\s*\n\s*args\s*=\s*\[([^\]]*)\]/g;
  for (const m of tfText.matchAll(re)) {
    jobs.push({ binary: m[1], argsRaw: m[2] });
  }
  return jobs;
}

// Parses a Terraform `args = [...]` raw inner-string into flag names (without
// leading dashes, without `=value`). Ignores non-flag positional strings.
function flagNamesFromArgsRaw(argsRaw) {
  const names = [];
  for (const m of argsRaw.matchAll(/"(-{1,2}[^"=]+)(?:=[^"]*)?"/g)) {
    names.push(m[1].replace(/^-+/, ""));
  }
  return names;
}

// Parses Go source for flag definitions: fs.BoolVar(&x, "name", ...),
// flag.String("name", ...), fs.Duration("name", ...), etc. Covers both the
// *Var (pointer-based) and plain (returns *T) forms, and both the top-level
// `flag` package and a local FlagSet variable (any receiver name).
function definedFlagsFromGoSource(goText) {
  const names = new Set();
  const re = /\b(?:flag|[A-Za-z_][A-Za-z0-9_]*)\.(?:Bool|String|Int|Int64|Uint|Uint64|Float64|Duration)(?:Var)?\(\s*(?:&[A-Za-z0-9_.]+,\s*)?"([a-zA-Z0-9_-]+)"/g;
  for (const m of goText.matchAll(re)) names.add(m[1]);
  // `-h` / `-help` are provided by the flag package itself, never user-defined.
  names.add("h");
  names.add("help");
  return names;
}

// Given one job {binary, argsRaw}, the set of buildable binaries, a lookup of
// binary -> defined-flags Set (or undefined if the cmd package could not be
// read), and whether backend/cmd/<binary> exists at all, returns a list of
// human-readable findings (empty = job is deployable as declared).
function findingsForJob(job, buildable, definedFlagsByBinary, cmdDirExists) {
  const findings = [];
  if (!cmdDirExists) {
    findings.push(`binary "${job.binary}": no backend/cmd/${job.binary} package exists`);
    return findings; // no point checking flags against a package that isn't there
  }
  if (!buildable.has(job.binary)) {
    findings.push(`binary "${job.binary}": not built by any backend/Dockerfile* (job would fail to start: no such file or directory)`);
  }
  const defined = definedFlagsByBinary.get(job.binary);
  if (defined) {
    for (const flagName of flagNamesFromArgsRaw(job.argsRaw)) {
      if (!defined.has(flagName)) {
        findings.push(`binary "${job.binary}": passes undefined flag "-${flagName}" (flag provided but not defined)`);
      }
    }
  }
  return findings;
}

// ---------------------------------------------------------------------------
// Self-test
// ---------------------------------------------------------------------------

function selfTest() {
  // 1) A clean job: binary built, both flags defined -> no findings.
  const goodBuildable = new Set(["obligation-sweeper"]);
  const goodDefined = new Map([["obligation-sweeper", new Set(["timeout", "tenant-id"])]]);
  const goodJob = { binary: "obligation-sweeper", argsRaw: `"-timeout=180s"` };
  const goodFindings = findingsForJob(goodJob, goodBuildable, goodDefined, true);
  if (goodFindings.length) {
    throw new Error(`self-test rejected a valid job: ${goodFindings.join("; ")}`);
  }

  // 2) The actual KERN-001 regression: job passes a flag the binary no longer
  //    defines.
  const staleJob = {
    binary: "obligation-sweeper",
    argsRaw: `"-timeout=180s", "-project-calendar=false", "-project-vaccination-read-models=true"`,
  };
  const staleFindings = findingsForJob(staleJob, goodBuildable, goodDefined, true);
  if (staleFindings.length !== 2) {
    throw new Error(`self-test missed the stale-flag regression: got ${JSON.stringify(staleFindings)}`);
  }
  if (!staleFindings.some((f) => f.includes("-project-calendar"))) {
    throw new Error("self-test did not flag -project-calendar");
  }
  if (!staleFindings.some((f) => f.includes("-project-vaccination-read-models"))) {
    throw new Error("self-test did not flag -project-vaccination-read-models");
  }

  // 3) A job whose binary was deleted from the Dockerfile/cmd tree entirely
  //    (the vaccination-projection-worker class of bug).
  const deletedJob = { binary: "vaccination-projection-worker", argsRaw: `"-timeout=90s"` };
  const deletedFindings = findingsForJob(deletedJob, goodBuildable, new Map(), false);
  if (!deletedFindings.length) {
    throw new Error("self-test missed a job referencing a deleted cmd package");
  }

  // 4) Dockerfile parsing: loop-style and literal-style.
  const loopBinaries = binariesFromLoopDockerfile(
    "RUN set -e && for cmd in api obligation-sweeper kernel-worker; do go build -o /out/${cmd} ./cmd/${cmd}; done",
  );
  if (!loopBinaries.includes("kernel-worker") || !loopBinaries.includes("obligation-sweeper")) {
    throw new Error(`self-test failed to parse loop-style Dockerfile: ${JSON.stringify(loopBinaries)}`);
  }
  const literalBinaries = binariesFromLiteralDockerfile("go build -o /out/migrate ./cmd/migrate");
  if (!literalBinaries.includes("migrate")) {
    throw new Error(`self-test failed to parse literal-style Dockerfile: ${JSON.stringify(literalBinaries)}`);
  }

  // 5) Flag-definition parsing across both Var and plain forms.
  const flags = definedFlagsFromGoSource(
    'fs.StringVar(&cfg.TenantID, "tenant-id", "", "tenant id")\n' +
      'fs.BoolVar(&cfg.SweepReminders, "sweep-reminders", true, "x")\n' +
      'dueBeforeRaw := fs.String("due-before", "", "x")\n',
  );
  if (!flags.has("tenant-id") || !flags.has("sweep-reminders") || !flags.has("due-before")) {
    throw new Error(`self-test failed to parse Go flag definitions: ${JSON.stringify([...flags])}`);
  }

  // 6) extractJobs on a realistic multi-job tf fragment.
  const tfFixture = `
    obligation_sweeper = {
      command = ["/app/bin/obligation-sweeper"]
      args    = ["-timeout=180s", "-project-calendar=false"]
    }
    notification_dispatcher = {
      command = ["/app/bin/notification-dispatcher"]
      args    = ["-timeout=45s", "-limit=100"]
    }
  `;
  const parsedJobs = extractJobs(tfFixture);
  if (parsedJobs.length !== 2 || parsedJobs[0].binary !== "obligation-sweeper") {
    throw new Error(`self-test failed to extract jobs from tf fixture: ${JSON.stringify(parsedJobs)}`);
  }

  console.log("deployed-job-flags guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

// ---------------------------------------------------------------------------
// Real run
// ---------------------------------------------------------------------------

const dockerfileNames = ["Dockerfile", "Dockerfile.migrate"];
const dockerfileTexts = dockerfileNames
  .map((name) => join(repo, "backend", name))
  .filter((path) => existsSync(path))
  .map((path) => readFileSync(path, "utf8"));

const buildable = buildableBinaries(dockerfileTexts);

const tfFiles = ["infra/envs/dev/cloud_run_jobs.tf", "infra/envs/stg/cloud_run_jobs.tf"].filter((rel) =>
  existsSync(resolve(repo, rel)),
);

const allFindings = [];
const definedFlagsByBinary = new Map();
const cmdDirExistsByBinary = new Map();

for (const rel of tfFiles) {
  const tfText = readFileSync(resolve(repo, rel), "utf8");
  const jobs = extractJobs(tfText);
  for (const job of jobs) {
    if (!cmdDirExistsByBinary.has(job.binary)) {
      const cmdDir = resolve(repo, "backend/cmd", job.binary);
      cmdDirExistsByBinary.set(job.binary, existsSync(cmdDir));
    }
    if (cmdDirExistsByBinary.get(job.binary) && !definedFlagsByBinary.has(job.binary)) {
      const cmdDir = resolve(repo, "backend/cmd", job.binary);
      const goFiles = readdirSync(cmdDir).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"));
      const combined = goFiles.map((f) => readFileSync(join(cmdDir, f), "utf8")).join("\n");
      definedFlagsByBinary.set(job.binary, definedFlagsFromGoSource(combined));
    }
    const findings = findingsForJob(
      job,
      buildable,
      definedFlagsByBinary,
      cmdDirExistsByBinary.get(job.binary),
    );
    for (const finding of findings) {
      allFindings.push(`${rel}: job "${job.binary}": ${finding.replace(`binary "${job.binary}": `, "")}`);
    }
  }
}

if (allFindings.length) {
  console.error("deployed-job-flags guard failed:");
  for (const finding of allFindings) console.error(`- ${finding}`);
  process.exit(1);
}
console.log(`deployed-job-flags guard: all deployed Cloud Run jobs resolve to a built binary with defined flags (${tfFiles.length} tf files checked)`);
