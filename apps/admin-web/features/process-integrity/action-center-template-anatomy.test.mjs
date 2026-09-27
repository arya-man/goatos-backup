// TR1-#26 / TR1-#29 guards (template review round 1).
//   action-center-kpi-selected: the selected quick tile is the template's selected-card ring
//     (checkout/payment: 0 0 0 2px text.primary), never a status-coloured outline (it read as a bright
//     blue focus box on the "All" tile).
//   action-center-card-anatomy: the status board is the template kanban (components/app/kanban
//     columns + item anatomy); a card carries at most two Labels and every other fact as a caption
//     line; the legacy .taskboard / .tcol / .task CSS stays deleted.
//   adherence-ledger-readable: the /protocol-adherence ledger fits the 1440 content column (Evidence
//     on screen), scrolls inside the template Scrollbar below it, and its cells wrap instead of cutting
//     text with an ellipsis ("Start SOP - overdu…").
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const page = read("./action-center.tsx");
const parts = read("./action-center-board-parts.tsx");
const board = read("./work-board.tsx");
const adherence = read("./protocol-adherence.tsx");
const theme = read("../../app/mesha-theme.css");

test("action-center-kpi-selected: selected tile uses the template text.primary ring, not a coloured outline", () => {
  assert.match(parts, /^"use client";/);
  assert.match(parts, /boxShadow: `0 0 0 2px \$\{theme\.vars\.palette\.text\.primary\}`/);
  assert.match(parts, /on \? SELECTED_TILE_SX : false/);
  for (const src of [page, parts]) {
    assert.doesNotMatch(src, /outline(Color)?\s*:/, "no outline on the quick tiles");
  }
  assert.match(page, /<ActionCenterQuickTile /);
});

test("action-center-card-anatomy: template kanban columns and item anatomy, at most two Labels", () => {
  assert.match(board, /import \{ KanbanBoard, KanbanColumn \} from "@\/components\/app\/kanban";/);
  assert.match(board, /import \{ ItemContent, ItemInfo, ItemName, ItemStatus/);
  assert.match(board, /export const MAX_CARD_LABELS = 2;/);
  assert.match(board, /\]\.slice\(0, MAX_CARD_LABELS\);/);
  const labelTags = board.match(/<Label\b/g) ?? [];
  assert.equal(labelTags.length, 1, "one Label element, mapped over the capped list");
  assert.doesNotMatch(board, /<Tag\b/, "no legacy Tag chips on a card");
  assert.doesNotMatch(board, /className=["`{][^"`}]*\b(taskboard|tcol|tcards|task|task-ac|sla)\b/, "no legacy board classes");
  assert.match(board, /<KanbanBoard data-ac-board/);
  assert.match(parts, /<KanbanItemRoot data-filter-row data-ac-card/);
  assert.doesNotMatch(board, /label\.empty_placeholder/, "empty columns carry no placeholder box");
  assert.doesNotMatch(theme, /^\s*(@media[^{]*\{\s*)?\.(taskboard|tcol|tcolh|tcards|task-ac|task|sla)[{ :.]/m, "legacy board CSS stays deleted");
});

test("adherence-ledger-readable: ledger fits 1440, scrolls below it, and wraps instead of clipping", () => {
  const widths = adherence.match(/const LEDGER_WIDTHS = \[([^\]]+)\]/);
  assert.ok(widths, "LEDGER_WIDTHS declared");
  const sum = widths[1].split(",").reduce((acc, n) => acc + Number(n.trim()), 0);
  assert.ok(sum <= 1060, `ledger min width ${sum} must fit the 1440 content column (1060)`);
  assert.match(adherence, /<Scrollbar>\s*<Table sx=\{\{ minWidth: LEDGER_MIN_WIDTH \}\}/);
  assert.doesNotMatch(adherence, /tableLayout/, "no fixed layout squeezing columns");
  assert.doesNotMatch(adherence, /ClipText/, "no ellipsis cells in the ledger");
  assert.match(adherence, /\{row\.next_action\} →/);
});
