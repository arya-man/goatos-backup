import assert from "node:assert/strict";
import { test } from "node:test";
import { fmtClock, fmtExperimentArm, fmtGrams, fmtSplit } from "./feed-config-format.ts";

test("authored grams read without trailing zeros, with Indian grouping", () => {
  assert.equal(fmtGrams("1200.000"), "1,200");
  assert.equal(fmtGrams("12.500"), "12.5");
  assert.equal(fmtGrams("0.000"), "0");
  assert.equal(fmtGrams(""), "");
  assert.equal(fmtGrams("abc"), "abc");
});

test("a session split reads as a share of the day", () => {
  assert.equal(fmtSplit("0.5000"), "50%");
  assert.equal(fmtSplit("0.3333"), "33.33%");
});

test("clock times drop their seconds", () => {
  assert.equal(fmtClock("14:00:00"), "14:00");
  assert.equal(fmtClock("09:30"), "09:30");
});

test("an editor opens on the plain stored number, with no padding and no thousands separator", async () => {
  const { fmtInputNumber } = await import("./feed-config-format.ts");
  assert.equal(fmtInputNumber("1500.000"), "1500");
  assert.equal(fmtInputNumber("12.500"), "12.5");
  assert.equal(fmtInputNumber("0.000"), "0");
  assert.equal(fmtInputNumber(""), "");
  assert.equal(fmtInputNumber(undefined), "");
  assert.equal(fmtInputNumber("abc"), "abc");
});

test("feed-config tables scroll in the template Scrollbar (TR1-#20)", async () => {
  // Every section table sits in the template Scrollbar (the ration grid in DataTable's own), never a
  // bare overflow Box / legacy .feed-scroll div.
  const { readFileSync } = await import("node:fs");
  const page = readFileSync(new URL("./feed-config.tsx", import.meta.url), "utf8");
  assert.ok((page.match(/<Scrollbar\b/g) ?? []).length >= 4, "four table scrollers");
  assert.doesNotMatch(page, /<Box\b[^>]*overflowX: "auto"/, "no bare overflow Box around a table");
  assert.doesNotMatch(page, /feed-scroll/, "no legacy .feed-scroll wrapper");
});

test("an experiment arm reads in farm words: DD/MM/YYYY, pen, sex and age spelt out (D4)", () => {
  assert.equal(fmtExperimentArm("Sheep M NEW"), "Sheep Male New");
  assert.equal(fmtExperimentArm("B+S Goat F OLD"), "B+S Goat Female Old");
  assert.equal(fmtExperimentArm("Shed-average plan 2026-09-07"), "Pen-average plan 07/09/2026");
  assert.equal(
    fmtExperimentArm("Mixed (9 Goat F, 1 Sheep F, 5 Goat M) NEW - warmup 20:80"),
    "Mixed (9 Goat Female, 1 Sheep Female, 5 Goat Male) New - warmup 20:80",
  );
  assert.equal(fmtExperimentArm(""), "");
});
