import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./work-board-board.tsx", import.meta.url), "utf8");

test("work-board filter changes clear per-lane per-park cursors", () => {
  assert.match(source, /function setParam\(params: URLSearchParams, pageContract: AdminUiPageContract/);
  assert.match(source, /laneCursorParams\(laneKeys\)/);
  assert.match(source, /laneParkResetParams\(laneKeys, parkKeys\)/);
  assert.match(source, /setParam\(p, pageContract, PARAM_OWNER, id\)/);
  assert.match(source, /setParam\(p, pageContract, PARAM_MODULE/);
});
