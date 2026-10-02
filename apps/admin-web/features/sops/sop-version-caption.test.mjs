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

import { sopCardCaption } from "./sop-derive.ts";

// PR #294 S4: on /procurement/sops and /sales/sops the sub-line "v1" sat right above the
// "Published · v1" Label. A published card whose caption is only the number shows no sub-line.
test("a published card does not repeat its version number under the title", () => {
  assert.equal(sopCardCaption("Feed purchase form", "Feed purchase form v1", 1, "active"), null);
  assert.equal(sopCardCaption("Sales deal", null, 7, "active"), null);
  // an authored label still says something the Label does not
  assert.equal(sopCardCaption("Deworming", "Monsoon dosing update", 4, "active"), "Monsoon dosing update");
  // a draft card has no "Published · vN" Label, so its number stays
  assert.equal(sopCardCaption("Sales deal", null, 2, "draft"), "v2");
});
