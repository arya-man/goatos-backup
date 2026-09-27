import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: work-board-loading-mirror (SK1). /work-board loading.tsx and the board fallback draw the page:
// header with its template mb, the bare toolbar row (people dropdown, module select, search, day
// stepper filling a phone row), the template kanban on FOUR_LANE_COLUMN_WIDTH.
// guard: work-board-sr-only-contained. The board is `position: relative`: an empty lane's absolutely
// placed sr-only row escaped the board scroller, widened the document to 1110px at 390 and the phone
// webview shrank the whole page to fit.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("work-board loading mirrors the page", () => {
  const loading = read("../../app/(admin)/work-board/loading.tsx");
  const board = read("./work-board-board.tsx");
  for (const src of [loading, board]) assert.match(src, /<KanbanSkeleton lanes=\{WB_SKELETON_LANES\} laneWidth=\{FOUR_LANE_COLUMN_WIDTH\} \/>/);
  assert.match(loading, /searchSmall fields=\{WB_TOOLBAR_FIELDS\} actionWidths=\{\[WB_DAY_STEPPER_WIDTH\]\}/);
  assert.match(board, /width: \{ xs: 1, md: WB_MODULE_SELECT_WIDTH \}/);
  assert.match(read("./work-board-layout.ts"), /WB_TOOLBAR_FIELDS[^=]*= \[PEOPLE_DROPDOWN_WIDTH, WB_MODULE_SELECT_WIDTH, "search"\]/);
  assert.match(read("./work-board-page.tsx"), /<Box sx=\{\{ mb: WB_HEADER_MB \}\}>/);
});

test("the board contains its lanes' absolutely placed rows", () => {
  const board = read("./work-board-board.tsx");
  const sx = board.slice(board.indexOf("const BOARD_SX = {"), board.indexOf("} as const;", board.indexOf("const BOARD_SX = {")));
  assert.match(sx, /position: "relative"/);
});
