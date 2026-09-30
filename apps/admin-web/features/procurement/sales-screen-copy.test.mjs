import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { salesErrorText } from "./sales-error.ts";

const read = (file) => readFileSync(new URL(file, import.meta.url), "utf8");
const SALES_FILES = [
  "./sales-sold.tsx", "./sales-farm-value.tsx", "./sales-buyer-analytics.tsx", "./sales-farm-born.tsx",
  "./sales-config.tsx", "./market-analytics.tsx", "./loadwise-section.tsx", "./vendor-board.tsx",
  "./market-config-section.tsx",
];

test("a Sales error alert never shows a raw code or a transport sentence", () => {
  assert.equal(
    salesErrorText({ code: "sales_invalid_farm", message: "Pick CBE, CPT or all farms." }, "Could not load."),
    "Pick CBE, CPT or all farms.",
  );
  // No code: a transport failure ("Backend service returned 500.") is not farm copy.
  assert.equal(salesErrorText({ message: "Backend service returned 500." }, "Could not load."), "Could not load.");
  assert.equal(salesErrorText({ code: "x" }, "Could not load."), "Could not load.");
  for (const file of SALES_FILES) {
    assert.doesNotMatch(read(file), /error\.code \?\? \w+\.error\.kind/, `${file} renders a raw error code`);
  }
});

test("Sales dates render DD/MM/YYYY, never the ISO wire value", () => {
  assert.match(read("./sales-config.tsx"), /dealCell\(humanDate\(deal\.sale_date\)\)/);
  assert.doesNotMatch(read("./sales-config.tsx"), /dealCell\(deal\.sale_date\)/);
  assert.match(read("./sales-record-drawer.tsx"), /cell\(field\("sale_date"\), fmtDate\(deal\.sale_date\)\)/);
  // The backend composes "DD/MM/YYYY HH24:MI" (87f77a0a7); fmtValuationSavedAt shows it as sent
  // (re-parsing it with new Date() would read 05/09 as 9 May).
  assert.match(read("./valuation-section.tsx"), /fmtValuationSavedAt\(v\.updated_at\)/);
});

test("a load that has sold nothing shows no sold value, never ₹0", () => {
  const section = read("./loadwise-section.tsx");
  assert.match(section, /load\.sold === 0 \? copy\(pageContract, "value\.not_sold_yet"\) : inrCompact\(load\.sold_value\)/);
  assert.match(section, /load\.sold === 0 \? \(\s*<Box component="span" sx=\{\{ color: "text\.secondary" \}\}>\{copy\(pageContract, "value\.not_sold_yet"\)\}<\/Box>/);
});

test("a failed Sales read shows farm words and no empty state beneath it (API down, 2026-09-25)", () => {
  const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
  const sop = read("../sops/sop-library.tsx");
  assert.doesNotMatch(sop, /\{error\.code\}/, "the SOP library printed the raw error code");
  assert.doesNotMatch(sop, /\{error\.message\}/, "the SOP library printed the transport sentence");
  for (const file of ["./valuation-section.tsx", "./market-reporters-section.tsx"]) {
    const src = read(file);
    assert.match(src, /salesErrorText\(result\.error, copy\(pageContract, "error\.load"\)\)/, file);
    assert.doesNotMatch(src, /result\.error\.message \|\|/, file);
  }
  const config = read("./sales-config.tsx");
  assert.match(config, /\{!dealsResult\.ok \? null : deals\.length === 0 \?/);
  assert.match(config, /\{!loadwiseResult\.ok \? null : loads\.length === 0 \?/);
  assert.match(read("./market-config-section.tsx"), /\{!configResult\.ok \? null : \(/);
});
