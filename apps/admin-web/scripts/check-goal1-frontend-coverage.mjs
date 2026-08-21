#!/usr/bin/env node
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";

function read(rel) {
  return readFileSync(new URL(`../${rel}`, import.meta.url), "utf8");
}

function functionBody(source, name) {
  const marker = `function ${name}`;
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, `${name} must exist`);
  const signatureClose = source.indexOf(") {", start);
  assert.notEqual(signatureClose, -1, `${name} must use a standard function body`);
  const open = signatureClose + 2;
  assert.notEqual(open, -1, `${name} must have a body`);
  let depth = 0;
  for (let i = open; i < source.length; i += 1) {
    const ch = source[i];
    if (ch === "{") depth += 1;
    if (ch === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(open, i + 1);
    }
  }
  throw new Error(`${name} body did not close`);
}

// The Config screen's assertions used to live here. They were removed with the
// screen itself: /config authored protocol rules for a category set that turned
// out to be vaccination-only, and Preventive Care / Vaccination plan replaced it.
// Nothing here asserts the plan console yet -- its behaviour is covered end to
// end by tools/e2e/suite.mjs, which drives the real screen rather than matching
// source text.

const herdUI = read("features/counts/herd-actions-ui.tsx");
const failedRows = functionBody(herdUI, "downloadFailedRows");
assert.match(failedRows, /parseCSVRecords\(csv,\s*\{\s*unterminatedQuoteMessage: parseErrorMessage\s*\}\)/, "downloadFailedRows must use the shared quote-aware parser");
assert.match(failedRows, /failedImportRows\(rows\)/, "downloadFailedRows must derive failed rows from shared failure semantics");
assert.match(failedRows, /parsed\[row\.row_number - 1\]/, "downloadFailedRows must preserve source row alignment");
assert.match(failedRows, /failureNotes\(row\)/, "downloadFailedRows must append backend failure notes");

for (const drawerName of ["BulkImportDrawer", "ShedImportDrawer"]) {
  const body = functionBody(herdUI, drawerName);
  assert.match(body, /accept=\{sheetImportAccept\}/, `${drawerName} file input must accept CSV and XLSX`);
  assert.match(body, /isSpreadsheetFile\(file\.name, file\.type\)/, `${drawerName} must detect spreadsheet uploads`);
  assert.match(
    body,
    /spreadsheetArrayBufferToCSV\(\s*await file\.arrayBuffer\(\),\s*copy\(pageContract, "error\.xlsx_empty"\),\s*copy\(pageContract, "error\.xlsx_parse_failed"\),?\s*\)/s,
    `${drawerName} must convert XLSX before preview with backend-owned parse copy`,
  );
  assert.match(body, /event\.target\.value = ""/, `${drawerName} must allow re-uploading the same file after parse errors`);
  assert.match(body, /const hash = await stableCSVContentHash\(csv\)/, `${drawerName} must bind preview and commit to CSV content hash`);
  assert.match(body, /hash !== previewHash/, `${drawerName} must reject stale commit after CSV edits`);
}

const adminUiService = read("../../backend/internal/adminui/app/service.go");
assert.match(adminUiService, /"error\.xlsx_empty"/, "backend-owned UI copy must include XLSX empty-workbook error");
assert.match(adminUiService, /"error\.xlsx_parse_failed"/, "backend-owned UI copy must include XLSX parse fallback");
const matrixStates = adminUiService.match(/ID: "matrix_states",[\s\S]*?\n\t\t\t},/);
assert.ok(matrixStates, "vaccination page contract must expose matrix_states options");
assert.match(matrixStates[0], /option\("missed"/, "matrix_states must label first-class missed work state");

const executionBoard = read("features/vaccination-execution/execution-board.tsx");
assert.match(executionBoard, /row\.blockerReason/, "Vaccination execution rows must display backend blocker reasons");
assert.match(executionBoard, /row\.nextAction/, "Vaccination execution rows must display backend next action");
assert.match(executionBoard, /scopeHref\("\/action-center", scope, \{\}, \{ state: row\.workState \}\)/, "Blocked shed drawer must route owner action to Action Center");
assert.match(executionBoard, /copy\(pageContract, "action\.open_action_center"\)/, "Shed drawer must expose the Action Center owner-action path");

const actionCenter = read("features/process-integrity/action-center.tsx");
assert.match(actionCenter, /const blocker = row\.blocker_reason/, "Action Center drawer must consume backend blocker reason");
assert.match(actionCenter, /<span>\{blocker\}<\/span>/, "Action Center drawer must render blocker text visibly");
assert.match(actionCenter, /<div className="v">\{row\.next_action\}<\/div>/, "Action Center drawer must render backend next_action visibly");

console.log("goal1 frontend coverage guard passed.");
