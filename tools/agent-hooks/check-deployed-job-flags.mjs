#!/usr/bin/env node
// check-deployed-job-flags.mjs — KERN-001 guard.
//
// deploy/runtime/workers.json is the single authoritative manifest of every backend
// runtime process (see its own "_readme" for the full consumer list). This guard makes
// that manifest load-bearing, not just documentation, by proving THREE things stay true
// on every run, fully offline (no `terraform plan`/`apply`, no cloud calls, no `go run`,
// no docker):
//
//   (a) every Cloud Run Job command+args declared in infra/envs/{dev,stg}/cloud_run_jobs.tf
//       MATCHES the manifest entry for that environment/binary exactly (fails on ANY
//       divergence — tf drifted from the manifest, or the manifest drifted from tf);
//   (b) every arg the manifest declares for a binary is a flag that binary's
//       backend/cmd/<name>/*.go flag parser (non-test) actually defines;
//   (c) every binary the manifest references is actually built by backend/Dockerfile*.
//
// This is exactly the class of defect found in KERN-001: obligation-sweeper's deployed
// job passed `-project-calendar` / `-project-vaccination-read-models`, two flags removed
// from backend/cmd/obligation-sweeper/main.go when the Calendar/vaccination screen
// projections were dropped, so the job never ran and calendar reminders + escalations
// (which the same binary is also responsible for, via -sweep-reminders/-sweep-escalations)
// had no runnable owner. Separately, four job blocks (process_integrity_projector,
// vaccination_execution_projector, vaccination_operations_projector,
// vaccination_projection_worker) referenced binaries deleted by an earlier commit
// (cb6fd35e, migration 000187) and were never removed from Terraform.
//
// Deterministic, offline: parses the JSON manifest + Terraform + Dockerfile + Go source
// as text. No `terraform plan`/`apply`, no cloud calls, no `go run`.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { resolve, join } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const manifestPath = resolve(repo, "deploy/runtime/workers.json");

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
// place a literal `command = ["/app/bin/NAME", ...]` is followed (allowing zero or
// more full-line `#`-comments in between, e.g. the KERN-001 explanatory comment sitting
// between obligation-sweeper's command and args blocks) by a literal `args = [...]`.
// Skips `command = each.value.command` (for_each interpolation) since those resolve
// through the kernel_jobs map, which is itself scanned as separate literal blocks in
// the same file.
function extractJobs(tfText) {
  const jobs = [];
  const re = /command\s*=\s*\[\s*"\/app\/bin\/([a-zA-Z0-9_-]+)"[^\]]*\]\s*(?:#[^\n]*\n\s*)*args\s*=\s*\[([^\]]*)\]/g;
  for (const m of tfText.matchAll(re)) {
    jobs.push({ binary: m[1], argsRaw: m[2] });
  }
  return jobs;
}

// Returns the inner text of a `{ ... }` block, brace-matched from the `{` at
// openBraceIdx. Handles nested braces (a Cloud Run service has containers { },
// resources { }, env { } ... inside it).
function braceBlockFrom(text, openBraceIdx) {
  let depth = 0;
  for (let i = openBraceIdx; i < text.length; i++) {
    if (text[i] === "{") depth++;
    else if (text[i] === "}") {
      depth--;
      if (depth === 0) return text.slice(openBraceIdx + 1, i);
    }
  }
  return "";
}

// Extracts { binary, argsRaw }[] for every google_cloud_run_v2_service whose
// container overrides the image ENTRYPOINT with a literal command =
// ["/app/bin/NAME"]. Unlike jobs, a service's command and args sit in the same
// `containers { }` block but are separated by ports/resources/env blocks, so
// this brace-matches the container rather than relying on adjacency. Services
// that run the Dockerfile ENTRYPOINT (no command, e.g. api) are intentionally
// not extracted — their deployment is not command-reconciled here.
function extractServices(tfText) {
  const services = [];
  const re = /resource\s+"google_cloud_run_v2_service"\s+"[a-zA-Z0-9_]+"\s*\{/g;
  let m;
  while ((m = re.exec(tfText))) {
    const body = braceBlockFrom(tfText, tfText.indexOf("{", m.index + m[0].length - 1));
    const cre = /containers\s*\{/g;
    let cm;
    while ((cm = cre.exec(body))) {
      const cbody = braceBlockFrom(body, body.indexOf("{", cm.index + cm[0].length - 1));
      const cmd = cbody.match(/command\s*=\s*\[\s*"\/app\/bin\/([a-zA-Z0-9_-]+)"/);
      if (!cmd) continue;
      const argsM = cbody.match(/args\s*=\s*\[([^\]]*)\]/);
      services.push({ binary: cmd[1], argsRaw: argsM ? argsM[1] : "" });
    }
  }
  return services;
}

// serviceFindings is the pure reconciliation core (no file IO, so it is
// self-testable). tfServicesByEnv maps env -> [{binary, argsRaw}] extracted from
// that env's Terraform service files.
function serviceFindings(tfServicesByEnv, manifestServices) {
  const findings = [];
  const withCommand = (manifestServices || []).filter(
    (s) => !s.uses_dockerfile_default_entrypoint && Array.isArray(s.args),
  );
  const byBinary = new Map(withCommand.map((s) => [s.binary, s]));

  for (const env of Object.keys(tfServicesByEnv)) {
    const tfServices = tfServicesByEnv[env];
    const tfBinaries = new Set(tfServices.map((s) => s.binary));

    // A: every TF command-service is in the manifest, deployed for this env, args match.
    for (const tf of tfServices) {
      const ms = byBinary.get(tf.binary);
      if (!ms) {
        findings.push(
          `infra/envs/${env}: a Cloud Run service runs /app/bin/${tf.binary} but deploy/runtime/workers.json services[] has no command-override entry for it (manifest drift)`,
        );
        continue;
      }
      if (!(ms.deployed_environments || []).includes(env)) {
        findings.push(
          `deploy/runtime/workers.json: service "${ms.name}" is deployed in infra/envs/${env} but its deployed_environments ${JSON.stringify(ms.deployed_environments || [])} omit "${env}"`,
        );
      }
      const tfArgs = argTokensFromArgsRaw(tf.argsRaw).join(" ");
      const manifestArgs = (ms.args || []).join(" ");
      if (tfArgs !== manifestArgs) {
        findings.push(
          `infra/envs/${env}: service "${ms.name}" terraform args [${tfArgs}] do not match deploy/runtime/workers.json args [${manifestArgs}]`,
        );
      }
    }

    // B: every manifest command-service claiming this env actually exists in TF.
    for (const ms of withCommand) {
      if ((ms.deployed_environments || []).includes(env) && !tfBinaries.has(ms.binary)) {
        findings.push(
          `deploy/runtime/workers.json: service "${ms.name}" claims deployed_environments includes "${env}" but no Cloud Run service runs /app/bin/${ms.binary} in infra/envs/${env}`,
        );
      }
    }
  }
  return findings;
}

// reconcileServices does the file IO (extract each env's TF services) and
// delegates to the pure serviceFindings core.
function reconcileServices(repoRoot, manifestServices) {
  const tfServicesByEnv = {};
  for (const env of ["dev", "stg"]) {
    const files = ["cloud_run_worker.tf", "cloud_run_services.tf"]
      .map((f) => resolve(repoRoot, "infra/envs", env, f))
      .filter((p) => existsSync(p));
    tfServicesByEnv[env] = files.flatMap((p) => extractServices(readFileSync(p, "utf8")));
  }
  return serviceFindings(tfServicesByEnv, manifestServices);
}

// Parses a Terraform `args = [...]` raw inner-string, or a JSON string array,
// into a normalized array of flag tokens (e.g. `-timeout=180s`). Order-preserving.
function argTokensFromArgsRaw(argsRaw) {
  const tokens = [];
  for (const m of argsRaw.matchAll(/"([^"]*)"/g)) tokens.push(m[1]);
  return tokens;
}

// Parses a Terraform/JSON arg token list into bare flag names (without leading
// dashes, without `=value`). Ignores non-flag positional strings.
function flagNamesFromArgTokens(tokens) {
  const names = [];
  for (const token of tokens) {
    const m = /^(-{1,2}[^=]+)(?:=.*)?$/.exec(token);
    if (m) names.push(m[1].replace(/^-+/, ""));
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

// Given one manifest entry {binary, args}, the set of buildable binaries, a lookup of
// binary -> defined-flags Set (or undefined if the cmd package could not be read), and
// whether backend/cmd/<binary> exists at all, returns a list of human-readable findings
// (empty = entry is deployable as declared).
function findingsForManifestEntry(entry, buildable, definedFlagsByBinary, cmdDirExists) {
  const findings = [];
  if (!cmdDirExists) {
    findings.push(`binary "${entry.binary}": no backend/cmd/${entry.binary} package exists`);
    return findings; // no point checking flags against a package that isn't there
  }
  if (!buildable.has(entry.binary)) {
    findings.push(`binary "${entry.binary}": not built by any backend/Dockerfile* (would fail to start: no such file or directory)`);
  }
  const defined = definedFlagsByBinary.get(entry.binary);
  if (defined) {
    for (const flagName of flagNamesFromArgTokens(entry.args || [])) {
      if (!defined.has(flagName)) {
        findings.push(`binary "${entry.binary}": manifest passes undefined flag "-${flagName}" (flag provided but not defined)`);
      }
    }
  }
  return findings;
}

// ---------------------------------------------------------------------------
// Self-test
// ---------------------------------------------------------------------------

function selfTest() {
  // 1) A clean manifest entry: binary built, both flags defined -> no findings.
  const goodBuildable = new Set(["obligation-sweeper"]);
  const goodDefined = new Map([["obligation-sweeper", new Set(["timeout", "tenant-id"])]]);
  const goodEntry = { binary: "obligation-sweeper", args: ["-timeout=180s"] };
  const goodFindings = findingsForManifestEntry(goodEntry, goodBuildable, goodDefined, true);
  if (goodFindings.length) {
    throw new Error(`self-test rejected a valid manifest entry: ${goodFindings.join("; ")}`);
  }

  // 2) The actual KERN-001 regression: manifest (or tf) carries a flag the binary no
  //    longer defines.
  const staleEntry = {
    binary: "obligation-sweeper",
    args: ["-timeout=180s", "-project-calendar=false", "-project-vaccination-read-models=true"],
  };
  const staleFindings = findingsForManifestEntry(staleEntry, goodBuildable, goodDefined, true);
  if (staleFindings.length !== 2) {
    throw new Error(`self-test missed the stale-flag regression: got ${JSON.stringify(staleFindings)}`);
  }
  if (!staleFindings.some((f) => f.includes("-project-calendar"))) {
    throw new Error("self-test did not flag -project-calendar");
  }
  if (!staleFindings.some((f) => f.includes("-project-vaccination-read-models"))) {
    throw new Error("self-test did not flag -project-vaccination-read-models");
  }

  // 3) A manifest entry whose binary was deleted from the Dockerfile/cmd tree entirely
  //    (the vaccination-projection-worker class of bug).
  const deletedEntry = { binary: "vaccination-projection-worker", args: ["-timeout=90s"] };
  const deletedFindings = findingsForManifestEntry(deletedEntry, goodBuildable, new Map(), false);
  if (!deletedFindings.length) {
    throw new Error("self-test missed a manifest entry referencing a deleted cmd package");
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
  const parsedTokens = argTokensFromArgsRaw(parsedJobs[0].argsRaw);
  if (parsedTokens.join(",") !== "-timeout=180s,-project-calendar=false") {
    throw new Error(`self-test failed to tokenize tf args: ${JSON.stringify(parsedTokens)}`);
  }

  // 6b) Regression guard: a `#`-comment block (exactly the KERN-001 explanatory comment
  // shape) sitting between `command = [...]` and `args = [...]` must not hide the job from
  // extraction. This is a real bug the original regex had — obligation_sweeper (the job at
  // the center of KERN-001) was silently unextracted because of its own explanatory comment.
  const commentedTfFixture = `
    obligation_sweeper = {
      command             = ["/app/bin/obligation-sweeper"]
      # -project-calendar / -project-vaccination-read-models no longer exist in
      # obligation-sweeper's flag parser -- passing them made every scheduled run fail.
      args     = ["-timeout=60s"]
    }
  `;
  const commentedJobs = extractJobs(commentedTfFixture);
  if (commentedJobs.length !== 1 || commentedJobs[0].binary !== "obligation-sweeper") {
    throw new Error(
      `self-test regression: extractJobs must tolerate comment lines between command and args, got ${JSON.stringify(commentedJobs)}`,
    );
  }

  // 7) tf-vs-manifest drift: same binary, different args -> a diff finding, not silently
  //    accepted as "flags are all still defined".
  const tfEntry = { binary: "obligation-sweeper", argTokens: ["-timeout=60s"] };
  const manifestEntry = { binary: "obligation-sweeper", args: ["-timeout=180s"] };
  const drift = diffTfAgainstManifest(tfEntry, manifestEntry);
  if (!drift) {
    throw new Error("self-test missed tf-vs-manifest arg drift");
  }

  // 8) SERVICE reconciliation (KERN-REV-08).
  const serviceTf = `
resource "google_cloud_run_v2_service" "kernel_worker" {
  name = "goatos-kernel-worker-stg"
  template {
    containers {
      name = "kernel-worker"
      image = local.backend_image
      command = ["/app/bin/kernel-worker"]
      ports { container_port = 8080 }
      resources { limits = { cpu = "1" } }
      env { name = "GOATOS_ENV" value = "stg" }
      args = ["-timeout=0s"]
    }
    containers {
      name = "otel-collector"
      image = var.otel_collector_image
      args = ["--config=/etc/otelcol-contrib/config.yaml"]
    }
  }
}`;
  const extracted = extractServices(serviceTf);
  if (extracted.length !== 1 || extracted[0].binary !== "kernel-worker") {
    throw new Error("self-test: extractServices did not find the command-override service (got " + JSON.stringify(extracted) + ")");
  }
  if (argTokensFromArgsRaw(extracted[0].argsRaw).join(" ") !== "-timeout=0s") {
    throw new Error("self-test: extractServices grabbed the sidecar args instead of the app container's");
  }

  const goodManifest = [{ name: "kernel_worker", binary: "kernel-worker", args: ["-timeout=0s"], deployed_environments: ["dev", "stg"] }];
  const tfBoth = { dev: [{ binary: "kernel-worker", argsRaw: '"-timeout=0s"' }], stg: [{ binary: "kernel-worker", argsRaw: '"-timeout=0s"' }] };
  if (serviceFindings(tfBoth, goodManifest).length !== 0) {
    throw new Error("self-test: a fully-consistent service produced findings");
  }
  // (a) TF service present but manifest marks it undeployed for stg.
  const undeployed = [{ name: "kernel_worker", binary: "kernel-worker", args: ["-timeout=0s"], deployed_environments: ["dev"] }];
  if (!serviceFindings(tfBoth, undeployed).some((f) => f.includes("omit"))) {
    throw new Error("self-test: missed a TF service whose manifest deployed_environments omit the env");
  }
  // (b) TF service present but missing entirely from the manifest.
  if (!serviceFindings(tfBoth, []).some((f) => f.includes("no command-override entry"))) {
    throw new Error("self-test: missed a TF service with no manifest entry");
  }
  // (c) TF service present but differently configured (args) in the manifest.
  const wrongArgs = [{ name: "kernel_worker", binary: "kernel-worker", args: ["-timeout=9m"], deployed_environments: ["dev", "stg"] }];
  if (!serviceFindings(tfBoth, wrongArgs).some((f) => f.includes("do not match"))) {
    throw new Error("self-test: missed a TF-vs-manifest service arg mismatch");
  }
  // (d) manifest claims deployment in an env where no TF service exists.
  if (!serviceFindings({ dev: [], stg: [{ binary: "kernel-worker", argsRaw: '"-timeout=0s"' }] }, goodManifest).some((f) => f.includes("claims deployed_environments includes"))) {
    throw new Error("self-test: missed a manifest service claiming an env with no TF service");
  }

  console.log("deployed-job-flags guard: self-test passed");
}

// Returns a human-readable finding string if the tf job's argTokens differ from the
// manifest entry's args (order-sensitive, exact match required), else null.
function diffTfAgainstManifest(tfEntry, manifestEntry) {
  const tfArgs = tfEntry.argTokens.join(" ");
  const manifestArgs = (manifestEntry.args || []).join(" ");
  if (tfArgs !== manifestArgs) {
    return `binary "${tfEntry.binary}": terraform args [${tfArgs}] do not match deploy/runtime/workers.json args [${manifestArgs}]`;
  }
  return null;
}

function argsKey(tokens) {
  return (tokens || []).join("\u0000");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

// ---------------------------------------------------------------------------
// Real run
// ---------------------------------------------------------------------------

if (!existsSync(manifestPath)) {
  console.error(`deployed-job-flags guard failed: manifest not found at ${manifestPath}`);
  process.exit(1);
}
const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));

const dockerfileNames = ["Dockerfile", "Dockerfile.migrate"];
const dockerfileTexts = dockerfileNames
  .map((name) => join(repo, "backend", name))
  .filter((path) => existsSync(path))
  .map((path) => readFileSync(path, "utf8"));

const buildable = buildableBinaries(dockerfileTexts);

const allFindings = [];
const definedFlagsByBinary = new Map();
const cmdDirExistsByBinary = new Map();

function ensureFlagsLoaded(binary) {
  if (!cmdDirExistsByBinary.has(binary)) {
    const cmdDir = resolve(repo, "backend/cmd", binary);
    cmdDirExistsByBinary.set(binary, existsSync(cmdDir));
  }
  if (cmdDirExistsByBinary.get(binary) && !definedFlagsByBinary.has(binary)) {
    const cmdDir = resolve(repo, "backend/cmd", binary);
    const goFiles = readdirSync(cmdDir).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"));
    const combined = goFiles.map((f) => readFileSync(join(cmdDir, f), "utf8")).join("\n");
    definedFlagsByBinary.set(binary, definedFlagsFromGoSource(combined));
  }
}

// --- Check (b) + (c) for every manifest entry: services[] and every environment's jobs[]. ---
const allManifestEntries = [
  ...(manifest.services || []),
  ...Object.values(manifest.environments || {}).flatMap((env) => env.jobs || []),
];
for (const entry of allManifestEntries) {
  ensureFlagsLoaded(entry.binary);
  const findings = findingsForManifestEntry(
    entry,
    buildable,
    definedFlagsByBinary,
    cmdDirExistsByBinary.get(entry.binary),
  );
  for (const finding of findings) {
    allFindings.push(`deploy/runtime/workers.json: entry "${entry.name}": ${finding.replace(`binary "${entry.binary}": `, "")}`);
  }
}

// --- Check (a): every tf job's command+args matches its manifest entry, in both
// directions (tf job missing from manifest, or manifest job never deployed to tf, are
// both reported — the latter as a lower-severity note unless it's a job kind, since
// services[] is allowed to carry forward-looking entries like kernel_worker that are
// explicitly marked deployed_to_cloud_run: false). ---
for (const [envName, envDef] of Object.entries(manifest.environments || {})) {
  const tfRelPath = envDef.tf_file;
  const tfAbsPath = resolve(repo, tfRelPath);
  if (!existsSync(tfAbsPath)) {
    allFindings.push(`deploy/runtime/workers.json: environment "${envName}": tf_file "${tfRelPath}" does not exist`);
    continue;
  }
  const tfText = readFileSync(tfAbsPath, "utf8");
  const tfJobs = extractJobs(tfText);
  const manifestJobs = envDef.jobs || [];
  const manifestJobsByBinary = new Map();
  for (const job of manifestJobs) {
    if (!manifestJobsByBinary.has(job.binary)) manifestJobsByBinary.set(job.binary, []);
    manifestJobsByBinary.get(job.binary).push(job);
  }
  const seenManifestJobs = new Set();

  for (const tfJob of tfJobs) {
    const tfArgTokens = argTokensFromArgsRaw(tfJob.argsRaw);
    const manifestCandidates = manifestJobsByBinary.get(tfJob.binary) || [];
    if (manifestCandidates.length === 0) {
      allFindings.push(
        `${tfRelPath}: job "${tfJob.binary}" is deployed in Terraform but has no matching entry in deploy/runtime/workers.json environments.${envName}.jobs (manifest drift)`,
      );
      continue;
    }

    const manifestJob =
      manifestCandidates.find((job) => argsKey(job.args || []) === argsKey(tfArgTokens)) ||
      (manifestCandidates.length === 1 ? manifestCandidates[0] : null);
    if (!manifestJob) {
      allFindings.push(
        `${tfRelPath}: job "${tfJob.binary}" args [${tfArgTokens.join(" ")}] do not match any deploy/runtime/workers.json environments.${envName}.jobs entry for that binary`,
      );
      continue;
    }
    seenManifestJobs.add(manifestJob.name);
    const drift = diffTfAgainstManifest({ binary: tfJob.binary, argTokens: tfArgTokens }, manifestJob);
    if (drift) {
      allFindings.push(`${tfRelPath}: ${drift}`);
    }
  }

  for (const manifestJob of manifestJobs) {
    if (!seenManifestJobs.has(manifestJob.name)) {
      allFindings.push(
        `deploy/runtime/workers.json: environments.${envName}.jobs entry "${manifestJob.name}" (binary "${manifestJob.binary}") is not deployed anywhere in ${tfRelPath} (stale manifest entry — remove it or redeploy the job)`,
      );
    }
  }
}

// --- Check (a) for SERVICES: every command-override Cloud Run service is
// reconciled bidirectionally with the manifest (deployed env + args), closing
// the gap where a service could stay "forward-looking / undeployed" in the
// manifest while Terraform actually creates it. ---
for (const finding of reconcileServices(repo, manifest.services)) {
  allFindings.push(finding);
}

if (allFindings.length) {
  console.error("deployed-job-flags guard failed:");
  for (const finding of allFindings) console.error(`- ${finding}`);
  process.exit(1);
}
const envCount = Object.keys(manifest.environments || {}).length;
console.log(
  `deployed-job-flags guard: all deployed Cloud Run jobs match deploy/runtime/workers.json and resolve to a built binary with defined flags (${envCount} environments checked)`,
);
