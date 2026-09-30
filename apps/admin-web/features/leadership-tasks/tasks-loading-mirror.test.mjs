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

test("the tasks toolbar reads the same widths as its skeleton (REVIEW-49 O79)", () => {
  const filters = read("./leadership-tasks-filters.tsx");
  assert.match(filters, /minWidth=\{TASK_SCOPE_WIDTH\}/);
  assert.match(filters, /width: \{ xs: 1, md: TASK_SORT_WIDTH \}/);
  assert.match(filters, /minWidth: \{ md: TASK_DATES_BUTTON_WIDTH \}/);
  assert.match(read("../../components/people-dropdown.tsx"), /width: \{ xs: 1, md: PEOPLE_DROPDOWN_WIDTH \}/);
  assert.match(read("./tasks-layout.ts"), /TASK_TOOLBAR_FIELDS[^=]*= \[TASK_SCOPE_WIDTH, PEOPLE_DROPDOWN_WIDTH, PEOPLE_DROPDOWN_WIDTH, TASK_SORT_WIDTH, "search"\]/);
  assert.match(read("../../app/(admin)/tasks/loading.tsx"), /<TabsSkeleton count=\{TASK_STATUS_TAB_COUNT\} counts \/>/);
});

// guard: tasks-loading-mirror (FIXJ5 -> FIXJ6, coordinator decision). The loading twin mirrors the
// NORMAL header: the board / list toggle and the 108px "New task" button. The rare "No one can be
// given a task" note (no assignable people, local data) keeps its own row on the page but is not
// drawn by the skeleton.
test("the tasks header skeleton reserves the toggle and the New task button, not the no-assignees note", () => {
  const layout = read("./tasks-layout.ts");
  assert.match(layout, /TASK_HEADER_ACTION_WIDTHS = \[\{ xs: 101, md: 73 \}, 108\]/);
  assert.match(layout, /TASK_HEADER_ACTION_HEIGHTS = \[\{ xs: 54, md: 40 \}, undefined\]/);
  assert.doesNotMatch(layout, /NOTE_SKELETON_WIDTH/);
  assert.doesNotMatch(read("../../app/(admin)/tasks/loading.tsx"), /NO_ASSIGNEES/);
  const modal = read("./new-task-modal.tsx");
  assert.match(modal, /role="note"[\s\S]*minHeight: TASK_NO_ASSIGNEES_NOTE_HEIGHT, flexBasis: TASK_NO_ASSIGNEES_NOTE_BASIS/);
});
