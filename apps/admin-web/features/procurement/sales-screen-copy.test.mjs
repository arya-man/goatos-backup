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
  assert.match(read("./valuation-section.tsx"), /fmtDateTime\(v\.updated_at\)/);
});

test("a load that has sold nothing shows no sold value, never ₹0", () => {
  const section = read("./loadwise-section.tsx");
  assert.match(section, /load\.sold === 0 \? copy\(pageContract, "value\.not_sold_yet"\) : inrCompact\(load\.sold_value\)/);
  assert.match(section, /load\.sold === 0 \? \(\s*<span className="muted">\{copy\(pageContract, "value\.not_sold_yet"\)\}<\/span>/);
});
