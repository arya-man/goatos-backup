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
assert.match(workbookCSV, /Farm,RFID/);
assert.match(workbookCSV, /CBE,RF-1/);
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
