import assert from "node:assert/strict";
import test from "node:test";
import { sopVersionCaption } from "./sop-derive.ts";

test("a version label that restates the SOP name reads as v{n}", () => {
  assert.equal(sopVersionCaption("Feed purchase form", "Feed purchase form v1", 1), "v1");
});

test("an internal kind token never reaches the card", () => {
  assert.equal(sopVersionCaption("Animal Purchase Inspection", "Animal Purchase Inspection · inspection", 3), "v3");
  assert.equal(sopVersionCaption("Gate check", "Monsoon update · inspection", 2), "Monsoon update");
});

test("an authored label that says something else is kept; no label falls back to the number", () => {
  assert.equal(sopVersionCaption("Deworming", "Monsoon dosing update", 4), "Monsoon dosing update");
  assert.equal(sopVersionCaption("Deworming", null, 2), "v2");
  assert.equal(sopVersionCaption("Deworming", null, null), null);
});
