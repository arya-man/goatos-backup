#!/usr/bin/env node
// check-secret-accessors.mjs — KERN-REV-04 guard.
//
// terraform validate does NOT catch a `google_service_account.runtime["X"]`
// index where "X" is no longer a key of runtime_service_accounts, nor a
// secret_containers[*].accessors entry naming a retired service account — those
// only fail at `terraform plan` (which needs the GCS backend + cloud auth, so it
// cannot run in ci-local). This static guard closes that gap: for each env it
// asserts that every service-account key referenced by
//   - secret_containers[*].accessors,
//   - local.database_clients, and
//   - any literal google_service_account.runtime["X"] reference in the env's *.tf
// actually exists in runtime_service_accounts. Retiring a per-stage SA (as the
// kernel-worker consolidation did) without repointing these references is the
// class of drift this catches.
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const ENVS = ["dev", "stg"];

// Extract the map body of `name = ...` by brace-matching. `name` must be a whole
// identifier — not a suffix of a longer one — so `runtime_service_accounts` does
// NOT match inside `base_runtime_service_accounts`. Handles three declaration
// shapes: a plain `name = { body }`, and a KERN-01 gated ternary
// `name = cond ? {} : { body }` (the empty `{}` is skipped, the real map body is
// returned). Returns "" if the block is not present or is empty.
function blockBody(text, name) {
  const re = new RegExp(`(?:^|[^A-Za-z0-9_])${name}\\s*=`, "m");
  const m = re.exec(text);
  if (m < 0 || !m) return "";
  // Scan forward from the `=`; brace-match each `{...}` group and return the
  // first NON-EMPTY body. This skips the empty `{}` branch of a gated ternary
  // while stopping before an unrelated later declaration's block.
  let i = m.index + m[0].length;
  // Only look within this declaration: stop at the next top-level `name =` decl
  // is unnecessary because we return on the first non-empty brace group.
  for (; i < text.length; i++) {
    if (text[i] !== "{") continue;
    let depth = 0;
    let j = i;
    for (; j < text.length; j++) {
      const c = text[j];
      if (c === "{") depth++;
      else if (c === "}") {
        depth--;
        if (depth === 0) break;
      }
    }
    const body = text.slice(i + 1, j);
    if (body.trim() !== "") return body;
    i = j; // empty group: skip past it and keep scanning (ternary `{}` branch)
  }
  return "";
}

// The set of defined runtime service-account keys. Supports both the flat form
// (`runtime_service_accounts = { ... }`) and the KERN-01 split form, where the
// map is assembled as `runtime_service_accounts = merge(base_..., legacy_...)`
// and the keys live in `base_runtime_service_accounts` +
// `legacy_stage_runtime_service_accounts`. The legacy stage SAs are gated by
// var.retire_legacy_stage_jobs at plan time, but they are still DEFINED in the
// source text (a conditional value, not a removed key), so references to them
// legitimately resolve in phase 1. Removing a key from BOTH source blocks still
// makes any dangling reference fail this guard.
function runtimeServiceAccountKeys(mainTf) {
  const keys = new Set();
  for (const name of [
    "runtime_service_accounts",
    "base_runtime_service_accounts",
    "legacy_stage_runtime_service_accounts",
  ]) {
    for (const k of topLevelKeys(blockBody(mainTf, name))) keys.add(k);
  }
  return keys;
}

// Depth-1 keys of a HCL map body (lines like `  key = {`). Within
// runtime_service_accounts the only `= {` at depth 1 are the SA keys.
function topLevelKeys(body) {
  const keys = new Set();
  let depth = 0;
  for (const line of body.split("\n")) {
    const trimmed = line.trim();
    const m = depth === 0 && trimmed.match(/^([a-zA-Z0-9_]+)\s*=\s*\{$/);
    if (m) keys.add(m[1]);
    for (const ch of trimmed) {
      if (ch === "{") depth++;
      else if (ch === "}") depth--;
    }
  }
  return keys;
}

// Every literal "string" inside any `accessors = ...` in the given body, whether
// the value is a bare list (`accessors = [ ... ]`) or a KERN-01 concat
// (`accessors = concat([ ... ], local.legacy_stage_sa_keys)`). For each accessors
// declaration the first bracketed `[ ... ]` list is scanned; non-literal refs
// like `local.legacy_stage_sa_keys` carry no quotes and are ignored (those keys
// are validated at their definition site).
function accessorStrings(body) {
  const out = [];
  for (const m of body.matchAll(/accessors\s*=/g)) {
    const open = body.indexOf("[", m.index);
    if (open < 0) continue;
    let depth = 0;
    let end = open;
    for (; end < body.length; end++) {
      if (body[end] === "[") depth++;
      else if (body[end] === "]") {
        depth--;
        if (depth === 0) break;
      }
    }
    const inner = body.slice(open + 1, end);
    for (const s of inner.matchAll(/"([a-zA-Z0-9_]+)"/g)) out.push(s[1]);
  }
  return out;
}

// Every literal "string" inside a `database_clients = toset( ... )`, whether the
// argument is a bare list (`toset([...])`) or a KERN-01 concat
// (`toset(concat([...], local.legacy_stage_sa_keys))`). Non-literal references
// like `local.legacy_stage_sa_keys` have no quotes and are ignored — those keys
// are validated where they are defined (the legacy_stage_runtime_service_accounts
// block), so the literal list is what this checks.
function databaseClients(text) {
  const decl = text.match(/database_clients\s*=\s*toset\(/);
  if (!decl) return [];
  const open = text.indexOf("(", decl.index);
  let depth = 0;
  let end = open;
  for (; end < text.length; end++) {
    if (text[end] === "(") depth++;
    else if (text[end] === ")") {
      depth--;
      if (depth === 0) break;
    }
  }
  const inner = text.slice(open + 1, end);
  return [...inner.matchAll(/"([a-zA-Z0-9_]+)"/g)].map((x) => x[1]);
}

// violationsForEnv is the pure core (no file IO): given an env name and its
// { filename: content } .tf map, returns the list of accessor/database-client/
// runtime[...] references that do not resolve to a runtime_service_accounts key.
export function violationsForEnv(env, texts) {
  const errors = [];
  const mainTf = texts["main.tf"] || "";
  const saKeys = runtimeServiceAccountKeys(mainTf);
  if (saKeys.size === 0) {
    return [`${env}: could not parse runtime_service_accounts keys`];
  }

  // 1. secret_containers[*].accessors
  const secretBody = blockBody(mainTf, "secret_containers");
  for (const a of accessorStrings(secretBody)) {
    if (!saKeys.has(a)) {
      errors.push(
        `infra/envs/${env}/main.tf: secret_containers accessor "${a}" is not a key of runtime_service_accounts (retired SA reference)`,
      );
    }
  }

  // 2. database_clients
  for (const a of databaseClients(mainTf)) {
    if (!saKeys.has(a)) {
      errors.push(
        `infra/envs/${env}/main.tf: database_clients entry "${a}" is not a key of runtime_service_accounts`,
      );
    }
  }

  // 3. literal google_service_account.runtime["X"] references across all *.tf
  for (const [file, text] of Object.entries(texts)) {
    for (const m of text.matchAll(
      /google_service_account\.runtime\["([a-zA-Z0-9_]+)"\]/g,
    )) {
      if (!saKeys.has(m[1])) {
        errors.push(
          `infra/envs/${env}/${file}: reference google_service_account.runtime["${m[1]}"] has no matching runtime_service_accounts key`,
        );
      }
    }
  }
  return errors;
}

// selfTest exercises the pure core against IN-MEMORY fixtures only — it never
// reads or writes tracked files, so it deterministically proves a retired /
// missing accessor is rejected and a clean config passes.
function selfTest() {
  const goodMain = `
locals {
  runtime_service_accounts = {
    api = {
      account_id = "a"
    }
    kernel_worker = {
      account_id = "k"
    }
  }
  database_clients = toset(["api", "kernel_worker"])
  secret_containers = {
    database_url = {
      secret_id = "s"
      accessors = ["api", "kernel_worker"]
    }
  }
}`;
  if (violationsForEnv("fixture", { "main.tf": goodMain }).length !== 0) {
    throw new Error("self-test: a clean config produced violations");
  }
  // Retired accessor in secret_containers.
  const retiredAccessor = goodMain.replace('accessors = ["api", "kernel_worker"]', 'accessors = ["api", "notification_dispatcher"]');
  if (!violationsForEnv("fixture", { "main.tf": retiredAccessor }).some((e) => e.includes("notification_dispatcher"))) {
    throw new Error("self-test: missed a retired secret accessor");
  }
  // Retired database_clients entry.
  const retiredDbClient = goodMain.replace('toset(["api", "kernel_worker"])', 'toset(["api", "outbox_relay"])');
  if (!violationsForEnv("fixture", { "main.tf": retiredDbClient }).some((e) => e.includes("outbox_relay"))) {
    throw new Error("self-test: missed a retired database_clients entry");
  }
  // Retired literal runtime[...] reference in another file.
  const iam = 'x = google_service_account.runtime["obligation_sweeper"].email';
  if (!violationsForEnv("fixture", { "main.tf": goodMain, "iam.tf": iam }).some((e) => e.includes("obligation_sweeper"))) {
    throw new Error("self-test: missed a retired runtime[...] reference");
  }

  // KERN-01 split form: keys come from base_ + legacy_ maps merged via a gated
  // ternary; a reference to a legacy stage SA must RESOLVE (no violation), and a
  // genuinely-undefined key must still be caught.
  const splitMain = `
locals {
  base_runtime_service_accounts = {
    api = {
      account_id = "a"
    }
    kernel_worker = {
      account_id = "k"
    }
  }
  legacy_stage_runtime_service_accounts = var.retire_legacy_stage_jobs ? {} : {
    outbox_relay = {
      account_id = "o"
    }
    domain_consumer = {
      account_id = "d"
    }
  }
  runtime_service_accounts = merge(
    local.base_runtime_service_accounts,
    local.legacy_stage_runtime_service_accounts,
  )
  database_clients = toset(concat(["api", "kernel_worker"], local.legacy_stage_sa_keys))
  secret_containers = {
    database_url = {
      secret_id = "s"
      accessors = concat(["api", "kernel_worker"], local.legacy_stage_sa_keys)
    }
    slack = {
      secret_id = "w"
      accessors = ["kernel_worker"]
    }
  }
}`;
  const splitPubsub = 'm = google_service_account.runtime["outbox_relay"].email\nn = google_service_account.runtime["domain_consumer"].email';
  const splitViolations = violationsForEnv("fixture", { "main.tf": splitMain, "pubsub.tf": splitPubsub });
  if (splitViolations.length !== 0) {
    throw new Error(`self-test: split-form legacy SA references should resolve, got ${JSON.stringify(splitViolations)}`);
  }
  // An undefined key under the split form must still be flagged.
  const splitBad = splitPubsub + '\np = google_service_account.runtime["never_defined"].email';
  if (!violationsForEnv("fixture", { "main.tf": splitMain, "pubsub.tf": splitBad }).some((e) => e.includes("never_defined"))) {
    throw new Error("self-test: split form missed a genuinely-undefined runtime[...] reference");
  }

  console.error("secret-accessors guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const errors = [];
for (const env of ENVS) {
  const dir = resolve(repo, "infra/envs", env);
  if (!existsSync(dir)) continue;
  const files = readdirSync(dir).filter((f) => f.endsWith(".tf"));
  const texts = Object.fromEntries(
    files.map((f) => [f, readFileSync(resolve(dir, f), "utf8")]),
  );
  errors.push(...violationsForEnv(env, texts));
}

if (errors.length > 0) {
  console.error("secret-accessors guard failed:");
  for (const e of errors) console.error(`- ${e}`);
  process.exit(1);
}
console.error(
  `secret-accessors guard: every accessor / database client / runtime[...] reference resolves to a defined service account (${ENVS.length} environments checked)`,
);
