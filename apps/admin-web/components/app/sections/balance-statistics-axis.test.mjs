// guard: balance-statistics-axis (REVIEW-23 O28). The derived BankingBalanceStatistics prints its
// y-axis ticks through formatSeriesValue (series unit, en-IN grouping), like its tooltip; plain Apex
// numbers ("1500") lose the g / kg / ₹ every weighing chart needs.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const src = readFileSync(new URL("./overview/banking/banking-balance-statistics.tsx", import.meta.url), "utf8");

test("y-axis and tooltip both format with the series unit", () => {
  assert.match(src, /yaxis:\s*\{\s*labels:\s*\{\s*formatter:\s*\(value: number\) => formatSeriesValue\(value, currentSeries\?\.unit \?\? '', 0\)/);
  assert.match(src, /tooltip:[\s\S]*formatSeriesValue\(value, currentSeries\?\.unit/);
});

test("formatSeriesValue groups en-IN and places units", async () => {
  const body = src.slice(src.indexOf("export function formatSeriesValue"), src.indexOf("export function BankingBalanceStatistics"));
  const js = body.replace(/export function formatSeriesValue\(value: number \| null \| undefined, unit = '', digits = 0\)/, "function formatSeriesValue(value, unit = '', digits = 0)");
  const fmt = new Function(`${js}; return formatSeriesValue;`)();
  assert.equal(fmt(150000, "g", 0), "1,50,000 g");
  assert.equal(fmt(1234, "₹·", 0), "₹1,234");
  assert.equal(fmt(null, "kg", 0), "–");
});
