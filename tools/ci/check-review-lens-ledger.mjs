#!/usr/bin/env node
// check-review-lens-ledger.mjs — keep the review-lens ledger from rotting.
//
// The ledger (.agents/skills/goatos-code-review/references/review-lens-ledger.md) is the
// always-loaded closed-decisions + review-lens record. This guard fails a push if a
// closed-decision entry is malformed, and WARNS (non-fatal) if it cites a guard id that no
// longer exists in the manifest. It is intentionally lenient on citations so adding a
// well-formed entry never false-fails; structure is what must stay correct.
//
// Usage: node tools/ci/check-review-lens-ledger.mjs [--self-test]

import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const LEDGER = ".agents/skills/goatos-code-review/references/review-lens-ledger.md";
const MANIFEST = "tools/ci/guardrail-manifest.json";
const VALID_STATUS = ["CLOSED", "BANNED", "LOCKED", "OPEN"];

// Pure validator: returns { errors: [], warnings: [] }. Fixture-testable.
export function validate({ ledgerText, manifestIds }) {
  const errors = [];
  const warnings = [];

  if (!ledgerText || !ledgerText.trim()) {
    errors.push("ledger is missing or empty");
    return { errors, warnings };
  }

  const lines = ledgerText.split("\n");
  // Split Part A closed-decision blocks: "### CD-<ID> — <title>" until the next "###" or "---".
  const blocks = [];
  let cur = null;
  for (const line of lines) {
    const m = line.match(/^###\s+(CD-[A-Za-z0-9-]+)\b/);
    if (m) {
      cur = { id: m[1], body: [] };
      blocks.push(cur);
      continue;
    }
    if (/^(###\s|---\s*$|## )/.test(line)) cur = null;
    if (cur) cur.body.push(line);
  }

  if (blocks.length === 0) {
    errors.push("no closed-decision (### CD-...) blocks found in the ledger");
  }

  for (const b of blocks) {
    const text = b.body.join("\n");
    if (!/STATUS:/i.test(text)) errors.push(`${b.id}: missing STATUS:`);
    if (!/INVARIANT:/i.test(text) && !/GAP:/i.test(text))
      errors.push(`${b.id}: missing INVARIANT: (or GAP: for OPEN)`);
    if (!/PROOF:/i.test(text) && !/NEXT:/i.test(text))
      errors.push(`${b.id}: missing PROOF: (or NEXT: for OPEN)`);
    const sm = text.match(/STATUS:\s*\**\s*([A-Z]+)/);
    if (sm && !VALID_STATUS.includes(sm[1]))
      errors.push(`${b.id}: STATUS "${sm[1]}" not one of ${VALID_STATUS.join("|")}`);
  }

  // Citation warn-only: an ENFORCED-BY: line naming a guard id not in the manifest.
  const known = new Set(manifestIds);
  for (const line of lines) {
    const e = line.match(/ENFORCED-BY:\s*`?([a-z0-9][a-z0-9-]+)`?/i);
    if (!e) continue;
    const cited = e[1].toLowerCase();
    if (cited === "manual" || cited.startsWith("manual")) continue;
    const norm = cited.replace(/-guard$/, "");
    const hit = [...known].some((k) => {
      const kn = k.replace(/-guard$/, "");
      return kn === norm || kn.includes(norm) || norm.includes(kn);
    });
    if (!hit) warnings.push(`ENFORCED-BY cites "${cited}" not found in manifest (stale?)`);
  }

  return { errors, warnings };
}

function manifestIdsFrom(manifest) {
  const ids = [];
  for (const g of manifest.guards || []) {
    if (g.id) ids.push(g.id);
    if (g.makeTarget) ids.push(g.makeTarget);
  }
  return ids;
}

function selfTest() {
  const good = [
    "### CD-X — thing",
    "- STATUS: **BANNED**",
    "- INVARIANT: x",
    "- PROOF: commit abc; ENFORCED-BY: `no-mismatch-review-queue`",
    "- ENFORCED-BY: no-mismatch-review-queue",
  ].join("\n");
  const ids = ["no-mismatch-review-queue", "config-validate-or-reject"];
  const cases = [
    { name: "good", text: good, ids, wantErr: 0 },
    { name: "empty", text: "", ids, wantErr: 1 },
    { name: "missing STATUS", text: "### CD-Y — t\n- INVARIANT: x\n- PROOF: y", ids, wantErr: 1 },
    { name: "bad STATUS", text: "### CD-Z — t\n- STATUS: **NOPE**\n- INVARIANT: x\n- PROOF: y", ids, wantErr: 1 },
    { name: "stale citation warns", text: "### CD-W — t\n- STATUS: **CLOSED**\n- INVARIANT: x\n- PROOF: y\n- ENFORCED-BY: no-such-guard", ids, wantErr: 0, wantWarn: 1 },
  ];
  let ok = true;
  for (const c of cases) {
    const r = validate({ ledgerText: c.text, manifestIds: c.ids });
    const errOk = c.wantErr === 0 ? r.errors.length === 0 : r.errors.length >= 1;
    const warnOk = c.wantWarn == null ? true : r.warnings.length >= c.wantWarn;
    if (!errOk || !warnOk) {
      ok = false;
      console.error(`  self-test FAIL [${c.name}]: errors=${r.errors.length} warnings=${r.warnings.length}`);
    }
  }
  if (!ok) {
    console.error("review-lens-ledger guard: self-test FAILED");
    process.exit(1);
  }
  console.log("review-lens-ledger guard: self-test passed");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  const ledgerPath = join(REPO, LEDGER);
  const ledgerText = existsSync(ledgerPath) ? readFileSync(ledgerPath, "utf8") : "";
  const manifest = JSON.parse(readFileSync(join(REPO, MANIFEST), "utf8"));
  const { errors, warnings } = validate({ ledgerText, manifestIds: manifestIdsFrom(manifest) });
  for (const w of warnings) console.warn(`  warn: ${w}`);
  if (errors.length) {
    console.error("review-lens-ledger guard: FAIL");
    for (const e of errors) console.error(`  - ${e}`);
    process.exit(1);
  }
  console.log("review-lens-ledger guard: PASS (ledger is consistent)");
}

main();
