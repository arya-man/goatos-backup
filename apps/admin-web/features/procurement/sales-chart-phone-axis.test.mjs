import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sales-chart-phone-axis (FJ3 P1-6). On /sales/sold at 390 the Month by month card's
// eighteen month ticks were rotated -45deg by ApexCharts and the last one ran past the card edge.
// The page passes template chart options (apexcharts `responsive`, deep-merged by useChart) that
// keep the ticks flat, hide overlapping ones and bound the tick count below 600px. The template
// card in components/minimal stays verbatim.
test("Sold month-by-month chart keeps its axis labels inside the card on a phone", () => {
  const src = readFileSync(new URL("./sales-sold-monthly.tsx", import.meta.url), "utf8");
  assert.match(src, /responsive: \[\{ breakpoint: 600, options: \{ xaxis: \{ tickAmount: 6, labels: \{ rotate: 0, rotateAlways: false, hideOverlappingLabels: true/);
  assert.match(src, /options: PHONE_AXIS \}\}/, "the phone axis options reach the template card");
});
