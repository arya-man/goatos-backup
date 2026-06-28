#!/usr/bin/env node
import { strict as assert } from "node:assert";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";

if (!globalThis.crypto) {
  globalThis.crypto = webcrypto;
}

const { csvCell, stableCSVContentHash } = await import("../features/counts/herd-import-utils.ts");

const digest = await stableCSVContentHash("Farm,RFID\nCBE,RF-1\n");
assert.match(digest, /^[a-f0-9]{64}$/);
assert.equal(digest, await stableCSVContentHash("Farm,RFID\nCBE,RF-1\n"));
assert.notEqual(digest, await stableCSVContentHash("Farm,RFID\nCBE,RF-2\n"));

for (const dangerous of ["=1+1", "+SUM(A1:A2)", "-10+20", "@cmd", " =1+1", "\t=1+1", "\r=1+1", "\n=1+1"]) {
  const encoded = csvCell(dangerous);
  assert.ok(encoded.startsWith("'") || encoded.startsWith("\"'"), `${dangerous} should start with an apostrophe, quoted if needed`);
}

assert.equal(csvCell("plain"), "plain");
assert.equal(csvCell("a,b"), "\"a,b\"");
assert.equal(csvCell("\"quoted\""), "\"\"\"quoted\"\"\"");

const herdActions = readFileSync(new URL("../features/counts/herd-actions.ts", import.meta.url), "utf8");
assert.match(herdActions, /const BULK_FILE_SHA256_RE = \/\^\[a-f0-9\]\{64\}\$\/;/);
assert.doesNotMatch(herdActions, /\{8,64\}/, "bulk import server actions must not accept legacy 32-bit hashes");
assert.match(herdActions, /function bulkCommitRowsHash/, "server action must derive commit namespace from normalized rows");
assert.match(herdActions, /commitAdminGoatBulkImport\(\{ rows, file_hash: commitHash \}, `goat-bulk:\$\{commitHash\}`\)/);
assert.match(herdActions, /`shed-bulk:\$\{commitHash\}:row:\$\{rowNumber\}`/);

const herdUI = readFileSync(new URL("../features/counts/herd-actions-ui.tsx", import.meta.url), "utf8");
assert.match(herdUI, /previewHash/, "bulk UI must bind commit to the previewed CSV hash");
assert.match(herdUI, /copy\(pageContract, "error\.preview_stale"\)/, "bulk UI must force re-preview after CSV edits with backend-owned copy");

const adminUiService = readFileSync(new URL("../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
assert.match(adminUiService, /"error\.preview_stale":\s+"CSV changed after preview\. Preview rows again before creating records\."/);

console.log("herd import security guard passed.");
