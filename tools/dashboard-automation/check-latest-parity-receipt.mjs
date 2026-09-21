#!/usr/bin/env node
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const config = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/config.json"), "utf8"));
const args = parseArgs(process.argv.slice(2));

if (args.selfTest) {
  selfTest();
  process.exit(0);
}

const receiptPath = args.receipt ?? process.env.GOATOS_STG_OCI_PARITY_RECEIPT ?? config.businessDataParity.latestFullParityReceiptPath;
const verdict = validateLatestParityReceipt(readJson(receiptPath), {
  now: new Date(),
  expectedStatus: config.businessDataParity.latestFullParityReceiptStatus,
  expectedIncludedTableCount: config.businessDataParity.latestFullParityReceiptIncludedTableCount,
  requiredExcludedPatterns: config.businessDataParity.latestFullParityReceiptExcludedPatterns,
  maxAgeHours: config.businessDataParity.latestFullParityReceiptMaxAgeHours,
});

if (!verdict.ok) {
  console.error(`latest STG-to-OCI parity receipt invalid: ${redactText(verdict.reason)}`);
  process.exit(1);
}

console.log(`latest STG-to-OCI parity receipt: ${verdict.status} ${verdict.includedTableCount} tables at ${verdict.verifiedAt}`);

export function validateLatestParityReceipt(receipt, options) {
  const status = String(receipt.readback_status ?? receipt.readbackStatus ?? receipt.status ?? receipt.result ?? "");
  if (status !== options.expectedStatus) {
    return { ok: false, reason: `status ${JSON.stringify(status)} is not ${options.expectedStatus}` };
  }

  const includedTableCount = Number(receipt.included_table_count ?? receipt.includedTableCount ?? receipt.tables ?? receipt.tableCount ?? receipt.included_tables_count);
  if (includedTableCount !== options.expectedIncludedTableCount) {
    return { ok: false, reason: `included table count ${includedTableCount} is not ${options.expectedIncludedTableCount}` };
  }

  const verifiedAt = parseDate(receipt.verified_at ?? receipt.verifiedAt ?? receipt.finished_at ?? receipt.finishedAt ?? receipt.generated_at ?? receipt.generatedAt);
  if (!verifiedAt) return { ok: false, reason: "missing verifiedAt/finishedAt timestamp" };
  const ageHours = (options.now.getTime() - verifiedAt.getTime()) / 36e5;
  if (!Number.isFinite(ageHours) || ageHours < -0.25) return { ok: false, reason: `verifiedAt ${verifiedAt.toISOString()} is in the future` };
  if (ageHours > options.maxAgeHours) {
    return { ok: false, reason: `receipt is ${ageHours.toFixed(1)}h old; max is ${options.maxAgeHours}h` };
  }

  if (receipt.stg_read_only !== true && receipt.stgReadOnly !== true) {
    return { ok: false, reason: "receipt does not prove STG stayed read-only" };
  }

  const exclusions = new Set((receipt.exclusions ?? receipt.excludedPatterns ?? receipt.excluded_patterns ?? []).map(String));
  for (const required of options.requiredExcludedPatterns) {
    if (!exclusions.has(required)) return { ok: false, reason: `missing exclusion ${required}` };
  }

  if (receipt.oci_write_mode && !["included-table-repair", "business-table-clone", "delta-only-repair"].includes(String(receipt.oci_write_mode))) {
    return { ok: false, reason: `unexpected OCI write mode ${receipt.oci_write_mode}` };
  }

  return {
    ok: true,
    status,
    includedTableCount,
    verifiedAt: verifiedAt.toISOString(),
  };
}

function readJson(file) {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch (error) {
    throw new Error(`cannot read latest STG-to-OCI parity receipt ${file}: ${error.message}`);
  }
}

function parseDate(value) {
  if (!value) return null;
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--receipt") parsed.receipt = raw[++i];
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  const now = new Date("2026-09-21T17:00:00.000Z");
  const base = {
    status: "READBACK_PASS",
    includedTableCount: 293,
    verifiedAt: "2026-09-21T16:04:00.000Z",
    stgReadOnly: true,
    excludedPatterns: [
      "analytics.*",
      "public.audit_log",
      "public.domain_event_processed_events",
      "public.outbox_messages",
      "public.herd_signal_*",
    ],
  };
  const options = {
    now,
    expectedStatus: "READBACK_PASS",
    expectedIncludedTableCount: 293,
    requiredExcludedPatterns: base.excludedPatterns,
    maxAgeHours: 24,
  };
  if (!validateLatestParityReceipt(base, options).ok) throw new Error("self-test: valid receipt rejected");
  if (validateLatestParityReceipt({ ...base, status: "blocked" }, options).ok) throw new Error("self-test: blocked receipt accepted");
  if (validateLatestParityReceipt({ ...base, includedTableCount: 292 }, options).ok) throw new Error("self-test: wrong table count accepted");
  if (validateLatestParityReceipt({ ...base, verifiedAt: "2026-09-19T16:04:00.000Z" }, options).ok) throw new Error("self-test: stale receipt accepted");
  if (validateLatestParityReceipt({ ...base, stgReadOnly: false }, options).ok) throw new Error("self-test: STG-write receipt accepted");
  const dir = mkdtempSync(path.join(tmpdir(), "latest-parity-receipt-"));
  try {
    const fixture = path.join(dir, "receipt.json");
    writeFileSync(fixture, `${JSON.stringify(base)}\n`);
    const loaded = validateLatestParityReceipt(JSON.parse(readFileSync(fixture, "utf8")), options);
    if (!loaded.ok) throw new Error("self-test: fixture receipt rejected");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("latest STG-to-OCI parity receipt: self-test passed");
}
