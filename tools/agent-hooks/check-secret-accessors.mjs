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

// Extract the substring of `name = { ... }` by brace-matching from the opening
// brace. Returns "" if the block is not present.
function blockBody(text, name) {
  const decl = text.indexOf(`${name} = {`);
  if (decl < 0) return "";
  const open = text.indexOf("{", decl);
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    const c = text[i];
    if (c === "{") depth++;
    else if (c === "}") {
      depth--;
      if (depth === 0) return text.slice(open + 1, i);
    }
  }
  return "";
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

// Every "string" inside any `accessors = [ ... ]` in the given body.
function accessorStrings(body) {
  const out = [];
  for (const m of body.matchAll(/accessors\s*=\s*\[([^\]]*)\]/g)) {
    for (const s of m[1].matchAll(/"([a-zA-Z0-9_]+)"/g)) out.push(s[1]);
  }
  return out;
}

// Every "string" inside a `database_clients = toset([ ... ])`.
function databaseClients(text) {
  const m = text.match(/database_clients\s*=\s*toset\(\[([^\]]*)\]\)/);
  if (!m) return [];
  return [...m[1].matchAll(/"([a-zA-Z0-9_]+)"/g)].map((x) => x[1]);
}

const errors = [];

for (const env of ENVS) {
  const dir = resolve(repo, "infra/envs", env);
  if (!existsSync(dir)) continue;
  const files = readdirSync(dir).filter((f) => f.endsWith(".tf"));
  const texts = Object.fromEntries(
    files.map((f) => [f, readFileSync(resolve(dir, f), "utf8")]),
  );
  const mainTf = texts["main.tf"] || "";
  const saKeys = topLevelKeys(blockBody(mainTf, "runtime_service_accounts"));
  if (saKeys.size === 0) {
    errors.push(`${env}: could not parse runtime_service_accounts keys`);
    continue;
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
}

if (errors.length > 0) {
  console.error("secret-accessors guard failed:");
  for (const e of errors) console.error(`- ${e}`);
  process.exit(1);
}
console.error(
  `secret-accessors guard: every accessor / database client / runtime[...] reference resolves to a defined service account (${ENVS.length} environments checked)`,
);
