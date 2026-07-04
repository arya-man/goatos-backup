#!/usr/bin/env node
import { strict as assert } from "node:assert";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";

if (!globalThis.crypto) {
  globalThis.crypto = webcrypto;
}

const {
  csvCell,
  isSpreadsheetFile,
  parseCSVRecords,
  sheetImportAccept,
  spreadsheetArrayBufferToCSV,
  stableCSVContentHash,
} = await import("../features/counts/herd-import-utils.ts");

const digest = await stableCSVContentHash("Farm,Animal ID 1\nCBE,AID-1\n");
assert.match(digest, /^[a-f0-9]{64}$/);
assert.equal(digest, await stableCSVContentHash("Farm,Animal ID 1\nCBE,AID-1\n"));
assert.notEqual(digest, await stableCSVContentHash("Farm,Animal ID 1\nCBE,AID-2\n"));

for (const dangerous of ["=1+1", "+SUM(A1:A2)", "-10+20", "@cmd", " =1+1", "\t=1+1", "\r=1+1", "\n=1+1"]) {
  const encoded = csvCell(dangerous);
  assert.ok(encoded.startsWith("'") || encoded.startsWith("\"'"), `${dangerous} should start with an apostrophe, quoted if needed`);
}

assert.equal(csvCell("plain"), "plain");
assert.equal(csvCell("a,b"), "\"a,b\"");
assert.equal(csvCell("\"quoted\""), "\"\"\"quoted\"\"\"");

const rawRecords = parseCSVRecords("A,B\n  keep  ,\" spaced,cell \"\n");
assert.deepEqual(rawRecords[1], ["  keep  ", " spaced,cell "], "failed-row export parser must preserve original cell text");
const trimmedRecords = parseCSVRecords("A,B\n  trim  ,\" keep inner \"\n", { trimCells: true });
assert.deepEqual(trimmedRecords[1], ["trim", "keep inner"], "preview parser trim must be explicit");
assert.throws(() => parseCSVRecords("\"unterminated", { unterminatedQuoteMessage: "bad csv" }), /bad csv/);
assert.equal(isSpreadsheetFile("goats.xlsx"), true, "xlsx uploads must be accepted for conversion");
assert.match(sheetImportAccept, /\.xlsx/, "file input accept list must include xlsx");
const sampleWorkbookBase64 =
  "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";
const workbookBytes = Buffer.from(sampleWorkbookBase64, "base64");
const workbookArray = workbookBytes.buffer.slice(workbookBytes.byteOffset, workbookBytes.byteOffset + workbookBytes.byteLength);
const workbookCSV = await spreadsheetArrayBufferToCSV(workbookArray, "empty workbook", "parse failed");
assert.match(workbookCSV, /Farm,Animal ID 1/);
assert.match(workbookCSV, /CBE,AID-1/);
await assert.rejects(
  () => spreadsheetArrayBufferToCSV(new TextEncoder().encode("not an xlsx").buffer, "empty workbook", "parse failed"),
  /parse failed/,
);

const herdActions = readFileSync(new URL("../features/counts/herd-actions.ts", import.meta.url), "utf8");
assert.match(herdActions, /const BULK_FILE_SHA256_RE = \/\^\[a-f0-9\]\{64\}\$\/;/);
assert.doesNotMatch(herdActions, /\{8,64\}/, "bulk import server actions must not accept legacy 32-bit hashes");
assert.match(herdActions, /function bulkCommitRowsHash/, "server action must derive commit namespace from normalized rows");
assert.match(herdActions, /preview_token: stablePreviewToken/, "goat commit must send the backend preview token");
assert.match(herdActions, /file_hash: stableFileHash/, "goat commit must send the previewed CSV hash, not a rows-only hash");
assert.doesNotMatch(herdActions, /file_hash: commitHash/, "goat commit must not replace the previewed CSV hash with the row hash");
assert.match(herdActions, /`shed-bulk:\$\{commitHash\}:row:\$\{rowNumber\}`/);

const herdUI = readFileSync(new URL("../features/counts/herd-actions-ui.tsx", import.meta.url), "utf8");
assert.match(herdUI, /previewHash/, "bulk UI must bind commit to the previewed CSV hash");
assert.match(herdUI, /preview\.preview_token/, "bulk UI must pass the backend preview token into commit");
assert.match(herdUI, /copy\(pageContract, "error\.preview_stale"\)/, "bulk UI must force re-preview after CSV edits with backend-owned copy");
assert.doesNotMatch(herdUI, /decision === "failed"/, "bulk import decision state must not include impossible failed option");

const adminUiService = readFileSync(new URL("../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
assert.match(adminUiService, /"error\.preview_stale":\s+"CSV changed after preview\. Preview rows again before creating records\."/);
const herdDecisionGroup = adminUiService.match(/ID: "herd_bulk_decisions",[\s\S]*?\n\t\t},/);
assert.ok(herdDecisionGroup, "herd bulk decision option group must exist");
assert.doesNotMatch(herdDecisionGroup[0], /option\("failed"/, "admin UI contract must not expose impossible failed import decision");

console.log("herd import security guard passed.");
