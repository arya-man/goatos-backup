import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { FEED_MIX_LABEL_WIDTH, FEED_MIX_MIN_HEIGHT, feedMixChartHeight, feedMixLabelLines } from "./feed-mix-layout.ts";

const FEEDS = [
  "Dry Masoor Bhusa",
  "Mesha Kids Concentrate",
  "Mesha Adult Concentrate",
  "Mesha Kids Sheep Concentrate",
  "Mesha Adult Concentrate Goat",
  "Mesha Kids Goat Concentrate",
  "Concentrate",
  "UHT Milk",
  "Mesha Adult Concentrate Sheep",
  "Baking Soda",
  "Vijay Concentrate",
];

for (const [where, width] of Object.entries(FEED_MIX_LABEL_WIDTH)) {
  test(`${where}: no feed name takes more than two axis lines (PR #294 O1: three-line names printed over each other)`, () => {
    for (const name of FEEDS) assert.ok(feedMixLabelLines(name, width).length <= 2, `${name} -> ${JSON.stringify(feedMixLabelLines(name, width))}`);
  });

  test(`${where}: the Goat / Sheep split feeds still read as different feeds`, () => {
    const shown = FEEDS.map((name) => feedMixLabelLines(name, width).join(" "));
    assert.equal(new Set(shown).size, FEEDS.length, JSON.stringify(shown));
    assert.match(feedMixLabelLines("Mesha Adult Concentrate Goat", width).join(" "), /Goat/);
    assert.match(feedMixLabelLines("Mesha Adult Concentrate Sheep", width).join(" "), /Sheep/);
  });
}

test("an over-long name is cut at its END, never its start", () => {
  const lines = feedMixLabelLines("Supercalifragilistic Concentrate With Extra Minerals Added", 18);
  assert.equal(lines.length, 2);
  assert.ok(lines[0].startsWith("Supercalifragilistic"));
  assert.ok(lines[1].endsWith("…"));
  assert.ok(lines[1].length <= 18);
});

test("the chart grows with the rows so two-line labels never overlap (row pitch >= two lines)", () => {
  const labels = FEEDS.map((name) => feedMixLabelLines(name, FEED_MIX_LABEL_WIDTH.phone));
  const height = feedMixChartHeight(labels);
  assert.ok(height > FEED_MIX_MIN_HEIGHT, `11 two-line feeds need more than the template's ${FEED_MIX_MIN_HEIGHT}px, got ${height}`);
  assert.ok((height - 80) / labels.length >= 2 * 14, "each row must be at least two label lines tall");
  assert.equal(feedMixChartHeight([["A"], ["B"]]), FEED_MIX_MIN_HEIGHT);
});

test("the card sizes its chart from the layout and keeps a value at each bar end", () => {
  const src = readFileSync(new URL("./feed-mix-card.tsx", import.meta.url), "utf8");
  assert.match(src, /feedMixChartHeight\(/);
  assert.match(src, /chartClasses\.root/);
  assert.match(src, /dataLabels: \{\s*enabled: true,\s*textAnchor: "start"/);
  // the full name is never lost: the tooltip titles each bar with it
  assert.match(src, /x: \{ formatter: .*rows\[.*\]\?\.label/);
});
