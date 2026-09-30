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
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const page = read("./action-center.tsx");
const parts = read("./action-center-board-parts.tsx");
const board = read("./work-board.tsx");
const adherence = read("./protocol-adherence.tsx");
const theme = legacyCss("mesha-theme");

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

// REVIEW-35 guards.
//   action-center-card-selector: the Filters / My tasks panel hides board cards through the SAME
//     selector the card shell carries (the panel still queried the deleted `.taskboard .task`, so
//     search and owner filters hid nothing).
//   action-center-loading-mirrors-page: the route loading.tsx paints the page's own board skeleton
//     (quick tiles, toolbar, kanban lanes, pager), never the retired grid lanes (TR1-#1 jump).
//   adherence-tabs-scroll-buttons: UrlTabs keeps its opt-in template scroll arrows (phones
//     included). The adherence strip itself no longer needs them: TR2-P1-7 cut it to All + 4
//     states with the rest in a select (guard adherence-tabs-fit, protocol-adherence-tabs.test.mjs).
const filters = read("./action-center-filters.tsx");
const skeletons = read("./action-center-skeletons.tsx");
const loading = read("../../app/(admin)/action-center/loading.tsx");
const urlTabs = read("../../components/app/url-tabs.tsx");

test("action-center-card-selector: filters hide cards through the shared card selector", () => {
  assert.match(parts, /export const ACTION_CENTER_CARD_SELECTOR = "\[data-ac-board\] \[data-ac-card\]";/);
  assert.match(filters, /import \{ ACTION_CENTER_CARD_SELECTOR \} from "\.\/action-center-board-parts";/);
  const uses = filters.match(/querySelectorAll<HTMLElement>\(ACTION_CENTER_CARD_SELECTOR\)/g) ?? [];
  assert.equal(uses.length, 2, "apply and clear both use the shared selector");
  assert.doesNotMatch(filters, /taskboard|\.task\b/, "no retired board selector");
  for (const rel of ["../../scripts/smoke-action-center-local-drawer-live.mjs", "../../scripts/smoke-vaccination-click-matrix-live.mjs", "../../scripts/smoke-visual-live.mjs"]) {
    assert.doesNotMatch(read(rel), /\.taskboard/, `${rel} must not query the retired board`);
  }
});

test("action-center-loading-mirrors-page: loading.tsx renders the page's own board skeleton", () => {
  assert.match(loading, /import \{ VIEW_SKELETON \} from "@\/features\/process-integrity\/action-center-skeletons";/);
  assert.match(loading, /\{VIEW_SKELETON\[""\]\}/);
  assert.doesNotMatch(loading, /layout="grid"|KanbanSkeleton/);
  assert.match(page, /import \{ BOARD_LANES_SKELETON, VIEW_SKELETON \} from "\.\/action-center-skeletons";/);
  assert.doesNotMatch(page, /const (BOARD_LANES_SKELETON|VIEW_SKELETON) =/, "one definition, shared");
  assert.doesNotMatch(skeletons, /layout="grid"/, "lanes are the kanban layout the board renders");
});

// TR3-P0-5: the board toolbar is a CARD on the page (severity chips, search, ⓘ, My tasks, Filters); its
// twin is the same card with the page's five chips at the 44px phone tap height, not a bare chip row.
test("action-center-loading-mirrors-page: the board toolbar twin is the page's toolbar card", () => {
  assert.match(page, /<Card>\s*<Box id="acToggles"/);
  assert.match(skeletons, /<ToolbarSkeleton card left=\{<ChipRowSkeleton count=\{AC_TOOLBAR\.chipWidths\.length\}[^>]*height=\{\{ xs: 44, sm: 32 \}\}/);
  assert.match(skeletons, /chipWidths: \[93, 67, 63, 62, 44\]/);
  const chips = page.match(/SEVERITY_ORDER\.map\(\(s2\)/g) ?? [];
  assert.equal(chips.length, 1, "All severity + the SEVERITY_ORDER chips");
});

test("adherence-tabs-scroll-buttons: UrlTabs keeps the opt-in template scroll arrows", () => {
  assert.match(urlTabs, /scrollButtons=\{scrollButtons\}/);
  assert.match(urlTabs, /allowScrollButtonsMobile=\{scrollButtons === "auto"\}/);
  assert.match(urlTabs, /"& \.MuiTabs-scrollButtons": \{ width: TAP_MIN\b/);
});

// TR2-P2-7: the My tasks / Filters dialog had a stray lone "i" and a Clear all + Clear local
// outlined pair. One Clear all (URL facets + local text) and Done.
test("guard: action-center-dialog-actions - one Clear all, no lone info glyph", () => {
  assert.doesNotMatch(filters, /InfoHint|filter\.clear_local/);
  assert.equal((filters.match(/filter\.clear_all/g) ?? []).length, 1);
  assert.match(filters, /clearLocalFilters\(\);\s*setOpen\(false\);/);
});
