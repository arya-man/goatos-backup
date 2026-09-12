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

test("work-board opened subtasks preserve the selected owner filter", () => {
  const pageSource = readFileSync(new URL("./work-board-page.tsx", import.meta.url), "utf8");
  const modalSource = readFileSync(new URL("./work-board-modal.tsx", import.meta.url), "utf8");
  const subtasksSource = readFileSync(new URL("./work-board-subtasks.tsx", import.meta.url), "utf8");
  const actionsSource = readFileSync(new URL("./actions.ts", import.meta.url), "utf8");
  const apiSource = readFileSync(new URL("../../lib/api/work-board-server.ts", import.meta.url), "utf8");

  assert.match(pageSource, /selectedOwner=\{owner\}/);
  assert.match(modalSource, /selectedOwner\?: string/);
  assert.match(modalSource, /key=\{`\$\{row\.row_key\}:\$\{selectedOwner \?\? ""\}`\}/);
  assert.match(modalSource, /<WorkBoardSubtasks[^>]+selectedOwner=\{selectedOwner\}/);
  assert.match(subtasksSource, /owner: selectedOwner/);
  assert.match(actionsSource, /owner\?: string/);
  assert.match(actionsSource, /owner: input\.owner/);
  assert.match(apiSource, /owner\?: string/);
  assert.match(apiSource, /owner: scope\.owner/);
});
