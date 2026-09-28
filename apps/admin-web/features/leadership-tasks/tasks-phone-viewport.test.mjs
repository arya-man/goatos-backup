// /tasks is opened at PHONE width inside the WhatsApp in-app browser, so the phone treatment on
// this page is behaviour, not polish. Every assertion here is a defect that shipped once
// (2026-09-18) and is cheap to reintroduce by editing one declaration.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const table = read("./leadership-tasks-table.tsx");
const dataTable = read("../../components/data-table.tsx");
const filters = read("./leadership-tasks-filters.tsx");
const editModal = read("./edit-task-modal.tsx");
const newModal = read("./new-task-modal.tsx");

// ---- A hidden column must hide its HEADER with its body.
// `meta.cellClassName` reaches only the `<td>`s. A column dropped at phone width therefore left
// the header row one cell longer than every body row, so the labels after it sat over the wrong
// column. The header cell needs the class too, which is what `meta.headerClassName` is for.
// The header row is the template TableHeadCustom: each head cell carries `className`.
assert.match(
  dataTable,
  /className: meta\?\.headerClassName/,
  "components/data-table.tsx must apply meta.headerClassName to the header cell",
);
assert.match(
  readFileSync(new URL("../../components/app/table/table-head-custom.tsx", import.meta.url), "utf8"),
  /className=\{headCell\.className\}/,
  "TableHeadCustom must put the head cell className on the <th>",
);
const urgencyMetas = table.match(/meta: \{ cellClassName: "lt-days-col"[^}]*\}/g) ?? [];
assert.equal(urgencyMetas.length, 3, "all three spellings of the urgency column carry the class");
for (const meta of urgencyMetas) {
  assert.match(
    meta,
    /headerClassName: "lt-days-col"/,
    "the urgency column must hide its header with its body at phone width",
  );
}

// ---- ONE phone breakpoint for the page (theme `md`): the urgency column drops, its clock
// reappears under the title, and the toolbar stacks -- all on the same width, so no band of
// widths gets the stacked toolbar with a desktop table.
assert.match(
  table,
  /\[theme\.breakpoints\.down\("md"\)\]: \{ "& \.lt-days-col": \{ display: "none" \} \}/,
  "the urgency column (header AND body cells) drops below md",
);
assert.match(
  table,
  /className="lt-clock-inline" sx=\{\{ display: \{ xs: "block", md: "none" \}/,
  "the same clock shows under the title exactly where the column is dropped",
);
assert.match(
  filters,
  /flexDirection: \{ xs: "column", md: "row" \}/,
  "the template toolbar stacks its controls at phone width (sections/user/user-table-toolbar.tsx)",
);
// No page-owned phone sheet any more: the toolbar is in flow, and every popup it opens is a
// portalled MUI surface (Select menu, CustomPopover) that owns its own focus and scroll lock.
assert.doesNotMatch(filters, /lt-fgroup|lt-fsheet|position: "fixed"/, "no hand-made fixed filter sheet");
assert.match(filters, /<CustomPopover/, "the dates disclosure is the template menu popover (portal)");

// ---- The status tabs scroll sideways inside their own strip at phone width (template Tabs
// `variant="scrollable"`), never widening the page.
assert.match(filters, /<Tabs[\s\S]*?variant="scrollable"/, "the status tabs scroll inside their strip");

// ---- The task modals are the template MUI Dialog with scroll="paper": the dialog is sized to
// the visual viewport by MUI, the header (and its X) stays put and only DialogContent scrolls, so
// a short phone can always reach Close. They portal above the page and the Ask Mesha button.
for (const [name, source] of [
  ["the edit modal", editModal],
  ["the new-task modal", newModal],
]) {
  assert.match(source, /<Dialog[\s\S]*?scroll="paper"/, `${name} must be an MUI Dialog that scrolls its content`);
  assert.match(source, /<DialogTitle[\s\S]*?<DialogContent/, `${name} keeps its header outside the scroller`);
}

// ---- Every overlay on this page owes a phone reader a body scroll lock and a focus trap:
// without the lock a drag inside the sheet scrolled the LIST behind it.
// The two modals get the lock and the trap from MUI Dialog (Modal), and Back closes them.
for (const [name, source] of [
  ["the edit modal", editModal],
  ["the new-task modal", newModal],
]) {
  assert.match(source, /useBackCloses\(open, closeModal\)/, `${name} must close on browser Back`);
}


// ---- guard: tasks-phone-stacked-row (FJ1-P1-11). Below sm the list is ONE column: the people,
// status and evidence columns carry `lt-wide-col` on header AND body and are hidden, and the task
// cell repeats the assignee + status, so nothing is clipped at the card edge mid-word.
{
  const wide = table.match(/cellClassName: "[^"]*lt-wide-col[^"]*", headerClassName: "[^"]*lt-wide-col[^"]*"/g) ?? [];
  assert.equal(wide.length, 4, "assignee, raised_by, status and evidence all carry lt-wide-col on header and cell");
  assert.match(table, /down\("sm"\)\]: \{ "& \.lt-wide-col": \{ display: "none" \}/, "lt-wide-col hides below sm");
  assert.match(table, /className="lt-phone-meta"[\s\S]{0,200}display: \{ xs: "flex", sm: "none" \}[\s\S]{0,600}task\.assignee[\s\S]{0,400}task\.statusLabel/, "the task cell stacks assignee + status on a phone");
  assert.match(table, /\{task\.assignee \|\| task\.isAssignee \? \(/, "an unassigned phone row renders no empty assignee span (no leading gap)");
}

// ---- guard: tasks-phone-search-row. At phone width the keyword field owns its row; "Dates" wraps.
{
  const filters = readFileSync(new URL("./leadership-tasks-filters.tsx", import.meta.url), "utf8");
  assert.match(filters, /flexWrap: \{ xs: "wrap", sm: "nowrap" \}[\s\S]{0,1200}<TextField\s+fullWidth\s+type="search"/, "search row wraps at xs with a full-width field");
  assert.doesNotMatch(filters, /maxWidth: \{ xs: "50%"/, "the Dates button no longer takes half the phone row");
}

// ---- guard: tasks-phone-filters-fold (TR2-P2-5). At 390 the scope, two people filters and sort were
// four stacked full-width selects above search + Dates, pushing the board below the fold. Below md
// they fold behind one Filters button into the template drawer; the skeleton folds the same way.
import { test as foldTest } from "node:test";
import { readFileSync as readFold } from "node:fs";
foldTest("guard: tasks-phone-filters-fold - phone filters fold into a drawer", () => {
  const src = readFold(new URL("./leadership-tasks-filters.tsx", import.meta.url), "utf8");
  const assert = { match: (s, re, m) => { if (!re.test(s)) throw new Error(m ?? String(re)); } };
  assert.match(src, /<Box sx=\{\{ display: \{ xs: "none", md: "contents" \} \}\}>\{filterControls\}<\/Box>/);
  assert.match(src, /<MinimalDrawer open=\{phoneFiltersOpen\}[\s\S]{0,300}\{filterControls\}/);
  assert.match(src, /sx=\{\{ display: \{ xs: "inline-flex", md: "none" \}, flexShrink: 0 \}\}/);
  assert.match(readFold(new URL("../../app/(admin)/tasks/loading.tsx", import.meta.url), "utf8"), /<FilterCardSkeleton inCard fold fields=\{TASK_TOOLBAR_FIELDS\}/);
});
