import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: tasks-loading-mirror (SK1). /tasks loading.tsx and the board's UrlSuspense fallback draw the
// template kanban on the board's own four-lane width (FOUR_LANE_COLUMN_WIDTH, 86vw lanes on a phone),
// never an equal-column grid that stacks lanes on a phone; the toolbar fields and header actions come
// from tasks-layout.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("tasks loading mirrors the page board and toolbar", () => {
  const loading = read("../../app/(admin)/tasks/loading.tsx");
  const page = read("./leadership-tasks-page.tsx");
  const dnd = read("./task-board-dnd.tsx");
  for (const src of [loading, page]) {
    assert.match(src, /<KanbanSkeleton lanes=\{TASK_BOARD_SKELETON_LANES\} laneWidth=\{FOUR_LANE_COLUMN_WIDTH\} \/>/);
    assert.doesNotMatch(src, /layout="grid"/);
  }
  assert.match(dnd, /"--kanban-column-width": FOUR_LANE_COLUMN_WIDTH/);
  assert.match(read("../work-board/work-board-board.tsx"), /"--kanban-column-width": FOUR_LANE_COLUMN_WIDTH/);
  assert.match(loading, /fields=\{TASK_TOOLBAR_FIELDS\} actionWidths=\{\[TASK_DATES_BUTTON_WIDTH\]\}/);
  assert.match(loading, /actionWidths=\{TASK_HEADER_ACTION_WIDTHS\} actionHeights=\{TASK_HEADER_ACTION_HEIGHTS\}/);
});
