#!/usr/bin/env node
// check-image-parity.mjs — `make e2e-parity`.
//
// Where tools/agent-hooks/check-deployed-job-flags.mjs proves deploy/runtime/workers.json
// is internally consistent with Terraform + Go source as TEXT, this script proves the
// manifest is true of the REAL BUILT IMAGE (`goatos-backend:e2e`, built by
// `make e2e-image-build` from backend/Dockerfile). This is the check the old `go run`-based
// E2E could never do: `go build ./...` and even the text-based guard cannot see a binary
// that failed to get COPYed into the final image stage, a missing shared library, or an
// image built for the wrong platform — only actually running the image catches those.
//
// For every manifest entry (services[] + every environment's jobs[]):
//   1. binary exists  : `docker run --rm --entrypoint test <image> -x /app/bin/<cmd>`
//   2. flags accepted : `docker run --rm --entrypoint /app/bin/<cmd> <image> <args...>`
//      with NO DATABASE_URL set. backend/internal/platform/postgres.Connect's very first
//      line is `if cfg.DatabaseURL == "" { return errors.New("DATABASE_URL is required") }`
//      — no network call, no retry/backoff — so a binary with a CORRECT flag parse fails
//      near-instantly with that one specific message, decoupled from whatever -timeout the
//      manifest declares. A binary whose flag parse FAILS instead prints Go's own
//      "flag provided but not defined: -X" (or "flag needs an argument", or similar) to
//      stderr before ever reaching Connect. We fail on that signature, not on exit code
//      (both paths exit 1) or on absence of a specific success message.
//
// Additionally cross-checks deploy/e2e/docker-compose.e2e.yml's kernel-worker
// entrypoint/command against the manifest's services["kernel_worker"] entry: the compose
// file's command is a literal copy of the manifest args (not templated from it at compose
// time — Compose has no simple concept of "read a JSON array from disk"), so drift between
// the two is a distinct, explicit failure mode we check for here rather than silently
// discovering it only when the smoke test happens to disagree.
//
// Usage:
//   node tools/e2e/check-image-parity.mjs --self-test
//   node tools/e2e/check-image-parity.mjs [--image goatos-backend:e2e]

import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";

const repo = resolve(import.meta.dirname, "../..");
const manifestPath = resolve(repo, "deploy/runtime/workers.json");
const composePath = resolve(repo, "deploy/e2e/docker-compose.e2e.yml");

const FLAG_PARSE_FAILURE_PATTERNS = [
  /flag provided but not defined/i,
  /flag needs an argument/i,
  /invalid boolean value/i,
  /invalid value .* for flag/i,
];
const BINARY_MISSING_PATTERNS = [/no such file or directory/i, /exec format error/i, /executable file not found/i];

// ---------------------------------------------------------------------------
// Pure logic (unit-tested via --self-test with fixture strings; no docker calls there).
// ---------------------------------------------------------------------------

// Classifies a completed `docker run` attempt for one manifest entry's flag-parity check.
// Returns null (pass) or a finding string.
// Returns the first line of stderrText that matches `pattern`, or the first line overall
// as a fallback (e.g. Docker's own "platform does not match host" warning can legitimately
// be line 0, and should not bury the actual matched diagnostic).
function firstMatchingLine(stderrText, pattern) {
  const lines = stderrText.split("\n");
  const hit = lines.find((line) => pattern.test(line));
  return (hit || lines[0] || "").trim();
}

function classifyFlagRun(entry, stderrText, dockerExitCode, dockerRanAtAll) {
  if (!dockerRanAtAll) {
    return `binary "${entry.binary}": docker failed to run the container at all (see stderr)`;
  }
  for (const pattern of BINARY_MISSING_PATTERNS) {
    if (pattern.test(stderrText)) {
      return `binary "${entry.binary}": missing from image (${firstMatchingLine(stderrText, pattern)})`;
    }
  }
  for (const pattern of FLAG_PARSE_FAILURE_PATTERNS) {
    if (pattern.test(stderrText)) {
      return `binary "${entry.binary}": args ${JSON.stringify(entry.args)} rejected by the flag parser (${firstMatchingLine(stderrText, pattern)})`;
    }
  }
  // Any other outcome (including a clean parse followed by "DATABASE_URL is required",
  // a context-deadline error, or exit 0) means flags parsed fine — pass.
  return null;
}

// Compares the compose file's kernel-worker command text against the manifest's
// kernel_worker service entry args. Returns a finding string, or null if they match.
function diffComposeAgainstManifest(composeText, manifestKernelWorkerArgs) {
  const m = /kernel-worker:[\s\S]*?command:\s*\[([^\]]*)\]/.exec(composeText);
  if (!m) {
    return "deploy/e2e/docker-compose.e2e.yml: no kernel-worker `command:` block found";
  }
  const composeArgs = [...m[1].matchAll(/"([^"]*)"/g)].map((x) => x[1]);
  const manifestArgs = manifestKernelWorkerArgs || [];
  if (composeArgs.join(" ") !== manifestArgs.join(" ")) {
    return `deploy/e2e/docker-compose.e2e.yml kernel-worker command [${composeArgs.join(" ")}] does not match deploy/runtime/workers.json services.kernel_worker.args [${manifestArgs.join(" ")}]`;
  }
  return null;
}

function selfTest() {
  // 1) Clean run: no matching failure signature -> pass.
  const passFinding = classifyFlagRun(
    { binary: "obligation-sweeper", args: ["-timeout=60s"] },
    "DATABASE_URL is required\n",
    1,
    true,
  );
  if (passFinding !== null) throw new Error(`self-test: expected pass, got ${passFinding}`);

  // 2) The KERN-001 regression signature.
  const kernFinding = classifyFlagRun(
    { binary: "obligation-sweeper", args: ["-timeout=60s", "-project-calendar=false"] },
    "flag provided but not defined: -project-calendar\nUsage of obligation-sweeper:\n",
    1,
    true,
  );
  if (!kernFinding) throw new Error("self-test missed the KERN-001 flag-rejection signature");

  // 3) Missing binary signature.
  const missingFinding = classifyFlagRun(
    { binary: "vaccination-projection-worker", args: ["-timeout=90s"] },
    "exec /app/bin/vaccination-projection-worker: no such file or directory\n",
    127,
    true,
  );
  if (!missingFinding) throw new Error("self-test missed the binary-missing signature");

  // 4) docker itself failing to run at all (e.g. image not found) is also a finding.
  const dockerFailFinding = classifyFlagRun({ binary: "api", args: [] }, "", 125, false);
  if (!dockerFailFinding) throw new Error("self-test missed a docker-level run failure");

  // 5) compose/manifest cross-check: matching case passes.
  const okCompose = `
  kernel-worker:
    entrypoint: ["/app/bin/kernel-worker"]
    command: ["-timeout=0s"]
  `;
  if (diffComposeAgainstManifest(okCompose, ["-timeout=0s"]) !== null) {
    throw new Error("self-test: matching compose/manifest kernel-worker args incorrectly flagged as drift");
  }

  // 6) compose/manifest cross-check: drifted case fails.
  const driftedCompose = `
  kernel-worker:
    entrypoint: ["/app/bin/kernel-worker"]
    command: ["-timeout=30s"]
  `;
  const drift = diffComposeAgainstManifest(driftedCompose, ["-timeout=0s"]);
  if (!drift) throw new Error("self-test missed compose/manifest kernel-worker arg drift");

  console.log("e2e image-parity check: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

// ---------------------------------------------------------------------------
// Real run
// ---------------------------------------------------------------------------

const imageArgIdx = process.argv.indexOf("--image");
const image = imageArgIdx !== -1 ? process.argv[imageArgIdx + 1] : process.env.GOATOS_E2E_IMAGE || "goatos-backend:e2e";

if (!existsSync(manifestPath)) {
  console.error(`e2e-parity failed: manifest not found at ${manifestPath}`);
  process.exit(1);
}
const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));

function dockerRun(args) {
  const result = spawnSync("docker", args, { encoding: "utf8" });
  return {
    ranAtAll: result.error === undefined,
    stdout: result.stdout || "",
    stderr: result.stderr || "",
    code: result.status,
  };
}

const findings = [];
const entries = [
  ...(manifest.services || []),
  ...Object.values(manifest.environments || {}).flatMap((env) => env.jobs || []),
];

// Dedupe by binary: every job/service sharing a binary would otherwise be checked
// redundantly (e.g. obligation_sweeper appears in both dev and stg with different args —
// each distinct args set IS worth checking, so dedupe by (binary, args) not just binary).
const seen = new Set();
for (const entry of entries) {
  const key = `${entry.binary}::${(entry.args || []).join(" ")}`;
  if (seen.has(key)) continue;
  seen.add(key);

  // Entries tagged with a non-default "image" (currently only migrate, which Terraform
  // deploys from a SEPARATE image — local.migration_image / backend/Dockerfile.migrate —
  // never merged into the backend image this script checks) are not expected to exist in
  // `image`. They are still checked end-to-end for real by
  // deploy/e2e/docker-compose.e2e.yml's own service for that image (see `make e2e-smoke`),
  // which is stronger coverage than a static existence/flag check would be anyway.
  if (entry.image && entry.image !== "backend") {
    console.log(`SKIP: "${entry.binary}" belongs to image "${entry.image}" (see deploy/runtime/workers.json.images.${entry.image}), not ${image} — verified instead by its own compose service in make e2e-smoke`);
    continue;
  }

  const existsResult = dockerRun(["run", "--rm", "--entrypoint", "test", image, "-x", `/app/bin/${entry.binary}`]);
  if (!existsResult.ranAtAll) {
    findings.push(`binary "${entry.binary}": docker failed to run at all — is the image "${image}" built? (${existsResult.stderr.trim()})`);
    continue;
  }
  if (existsResult.code !== 0) {
    findings.push(`binary "${entry.binary}": /app/bin/${entry.binary} is not an executable file inside ${image}`);
    continue;
  }
  console.log(`PASS: /app/bin/${entry.binary} exists in ${image}`);

  const flagResult = dockerRun([
    "run",
    "--rm",
    "--entrypoint",
    `/app/bin/${entry.binary}`,
    "-e",
    "DATABASE_URL=",
    image,
    ...(entry.args || []),
  ]);
  const finding = classifyFlagRun(entry, flagResult.stderr, flagResult.code, flagResult.ranAtAll);
  if (finding) {
    findings.push(finding);
  } else {
    console.log(`PASS: /app/bin/${entry.binary} accepts args ${JSON.stringify(entry.args || [])}`);
  }
}

// Compose/manifest kernel-worker cross-check.
if (existsSync(composePath)) {
  const composeText = readFileSync(composePath, "utf8");
  const kernelWorkerEntry = (manifest.services || []).find((s) => s.name === "kernel_worker");
  const composeDrift = diffComposeAgainstManifest(composeText, kernelWorkerEntry ? kernelWorkerEntry.args : []);
  if (composeDrift) {
    findings.push(composeDrift);
  } else {
    console.log("PASS: deploy/e2e/docker-compose.e2e.yml kernel-worker command matches deploy/runtime/workers.json");
  }
} else {
  findings.push(`compose file not found at ${composePath}`);
}

if (findings.length) {
  console.error(`e2e-parity failed against image "${image}":`);
  for (const f of findings) console.error(`- ${f}`);
  process.exit(1);
}
console.log(`e2e-parity: PASS — every deploy/runtime/workers.json entry resolves to a working binary+flags in ${image}`);
