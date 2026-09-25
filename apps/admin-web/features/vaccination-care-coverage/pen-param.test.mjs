import { test } from "node:test";
import assert from "node:assert/strict";
import { decodePens, encodePens } from "./pen-param.ts";

test("a pen set round-trips through one URL value, commas inside a value included", () => {
  const pens = ["9c000000-0000-4000-8000-000000004301|part 2", "9c000000-0000-4000-8000-000000004302|a,b"];
  const raw = encodePens(pens);
  assert.equal(raw.split(",").length, 2, "each value's own comma is escaped, so only the separators split");
  assert.deepEqual(decodePens(raw), pens);
  const viaUrl = new URLSearchParams({ cc_pens: raw }).get("cc_pens");
  assert.deepEqual(decodePens(viaUrl), pens, "survives URLSearchParams encoding and decoding");
});

test("[a, b] and [b] are different URLs, which is the defect the single parameter fixes", () => {
  const a = new URLSearchParams({ cc_pens: encodePens(["x|1", "y|2"]) });
  const b = new URLSearchParams({ cc_pens: encodePens(["y|2"]) });
  assert.notDeepEqual(Object.fromEntries(a), Object.fromEntries(b));
  const repeatedA = new URLSearchParams([["cc_pen", "x|1"], ["cc_pen", "y|2"]]);
  const repeatedB = new URLSearchParams([["cc_pen", "y|2"]]);
  assert.deepEqual(Object.fromEntries(repeatedA), Object.fromEntries(repeatedB), "the retired repeated key collapsed to its last value");
});

test("empty and broken values are dropped, never fatal", () => {
  assert.deepEqual(decodePens(undefined), []);
  assert.deepEqual(decodePens(""), []);
  assert.deepEqual(decodePens("%E0%A4%A,x%7C1,"), ["x|1"]);
});
