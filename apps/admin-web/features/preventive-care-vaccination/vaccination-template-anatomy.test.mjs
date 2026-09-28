import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: vaccination-template-anatomy (TR1-#10, TR1-#21). /vaccination was an 11,000px page of
// bespoke heat-maps painted by `.cbm-*` rules with off-palette hexes (#22633c / #d6f1e0 clear cells,
// #352c34 + #e36b6a pending chips, #9b5bb0 rework legend, #bbc3cb / #485561 notes). Every matrix is
// now a template table card: Card + CardHeader (+ info tip), template Label state colours, the
// template Scrollbar and pager (PagedRows, MATRIX_ROWS_PER_PAGE rows). This keeps it that way.

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const view = read("./command-board-view.tsx");
const board = read("./command-board.tsx");
const cards = read("./command-board-cards.tsx");
const skeleton = read("./vaccination-skeletons.tsx");
const css = ["../../app/mesha-theme.css", "../../app/frame.css", "../../app/minimal-theme.css", "../../app/globals.css"].map(read).join("\n");

test("vaccination-template-anatomy: the command board renders no legacy cbm classes or raw controls", () => {
  for (const [name, src] of [["command-board-view.tsx", view], ["command-board.tsx", board], ["command-board-cards.tsx", cards]]) {
    assert.doesNotMatch(src, /className=\{?[`"'][^`"']*\bcbm/, `${name}: no .cbm-* class (legacy heat-map paint)`);
    assert.doesNotMatch(src, /className="(card|hd|bd|twrap|tablewrap)"/, `${name}: no legacy card shell`);
    assert.doesNotMatch(src, /<(button|table|section|ul|li|h3|h4)\b/, `${name}: MUI parts only`);
    assert.doesNotMatch(src, /#[0-9a-fA-F]{3,8}\b(?![-\w])/, `${name}: no colour literal`);
  }
});

test("vaccination-template-anatomy: every matrix is a template table card with Label cells and a pager", () => {
  // Pen × Vaccine, Pending by pen, Vaccine × Pen status, Cohort matrix, Scheduled ahead.
  assert.ok((view.match(/<MatrixCard\b/g) ?? []).length >= 5, "five matrix cards");
  assert.match(cards, /<Card id=\{id\}/);
  assert.match(cards, /<CardHeader title=\{title\}/);
  assert.match(cards, /<PagedRows[\s\S]*?scrollbar[\s\S]*?initialRowsPerPage=\{MATRIX_ROWS_PER_PAGE\}/);
  assert.match(cards, /<Label color=\{color\}>/);
  assert.match(cards, /import \{ Label, type LabelColor \} from "@\/components\/minimal\/label"/);
  // Farm blocks are template Tabs with Label counts, not stacked per-farm tables.
  assert.match(view, /<Tabs[\s\S]*?variant="scrollable"/);
  assert.match(view, /<Tab[\s\S]*?icon=\{\s*<Label/);
  // Status filters are soft template chips, never bordered outline pills.
  assert.doesNotMatch(view, /variant=\{statuses\.has\(key\) \? "soft" : "outlined"\}/);
  // TR2-P2-6: the status filter is a multi Select (guard vaccination-status-select), not chips.
  assert.match(view, /checked=\{statuses\.has\(key\)\}/);
  // The skeleton mirrors the cards: legend rows, one table card per matrix, farm tabs.
  assert.match(skeleton, /<OptionalSkeleton>\s*<TableSkeleton columns=\{CB_MATRIX\.penVaccineColumns\}/);
  assert.match(skeleton, /tabs=\{<TabsSkeleton count=\{CB_MATRIX\.cohortFarmTabs\} counts \/>\}/);
});

test("vaccination-template-anatomy: the legacy cbm rules and their off-palette colours are deleted", () => {
  assert.doesNotMatch(css, /\.cbm[-\w]*/, "no .cbm-* rule left in the legacy stylesheets");
  for (const hex of ["#22633c", "#d6f1e0", "#352c34", "#e36b6a", "#9b5bb0", "#d99cff", "#bbc3cb", "#485561"]) {
    assert.ok(!css.toLowerCase().includes(hex), `${hex} (TR1-#10 off-palette) is gone`);
  }
});
