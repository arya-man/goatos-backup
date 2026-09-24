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
