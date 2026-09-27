import assert from "node:assert/strict";
import { test } from "node:test";
import { fmtClock, fmtGrams, fmtSplit } from "./feed-config-format.ts";

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

test("feed-config tables keep the sticky first column and scroll-edge fades (data-scroll-x on every scroller)", async () => {
  // REVIEW-10 O16: the template conversion swapped div.feed-scroll for a bare Box, which dropped the
  // frame.css sticky first column and components/app/scroll-edges fades (both key on the marker).
  const { readFileSync } = await import("node:fs");
  const page = readFileSync(new URL("./feed-config.tsx", import.meta.url), "utf8");
  const scrollers = page.match(/<Box\n\s+(?:data-scroll-x=""\n\s+)?sx=\{\{ overflowX: "auto" \}\}/g) ?? [];
  assert.ok(scrollers.length >= 4, "four table scrollers");
  for (const box of scrollers) assert.match(box, /data-scroll-x=""/);
});
