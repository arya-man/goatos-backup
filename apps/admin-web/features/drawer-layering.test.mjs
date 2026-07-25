import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const source = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

test("side drawer backdrop stays below the drawer and does not blur the page", () => {
  assert.match(source, /button\.scrim\{[\s\S]*z-index:210[\s\S]*backdrop-filter:none[\s\S]*\}/);
  assert.match(source, /button\.scrim\.on\{display:block;opacity:1;pointer-events:auto\}/);
  assert.match(source, /aside\.drawer\{[\s\S]*z-index:220[\s\S]*\}/);
});
