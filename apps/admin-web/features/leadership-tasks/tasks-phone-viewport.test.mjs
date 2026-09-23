// /tasks is opened at PHONE width inside the WhatsApp in-app browser, so the phone treatment on
// this page is behaviour, not polish. Every assertion here is a defect that shipped once
// (2026-09-18) and is cheap to reintroduce by editing one declaration.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const css = read("../../app/mesha-theme.css");
const table = read("./leadership-tasks-table.tsx");
const dataTable = read("../../components/data-table.tsx");
const filters = read("./leadership-tasks-filters.tsx");
const editModal = read("./edit-task-modal.tsx");
const newModal = read("./new-task-modal.tsx");
const dialogShell = read("./use-dialog-shell.tsx");

// ---- A hidden column must hide its HEADER with its body.
// `meta.cellClassName` reaches only the `<td>`s. A column dropped at phone width therefore left
// the header row one cell longer than every body row, so the labels after it sat over the wrong
// column. The header cell needs the class too, which is what `meta.headerClassName` is for.
assert.match(
  dataTable,
  /<th\b[\s\S]{0,200}?className=\{meta\?\.headerClassName\}/,
  "components/data-table.tsx must apply meta.headerClassName to the header cell",
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

// ---- ONE breakpoint for the page: the sheet, the tap targets and the dropped column agree.
// Three different widths meant 561-760px got the bottom sheet with ~28px controls.
assert.match(
  css,
  /@media\(max-width:760px\)\{\.lt-page \.lt-days-col\{display:none\}/,
  "the urgency column must drop at the same 760px breakpoint as the filter sheet",
);
assert.match(
  css,
  /@media\(max-width:760px\)\{[\s\S]*?\.lt-page \.lt-fbar\.lt-fsheet-host\{/,
  "the filter sheet must switch at 760px",
);
assert.doesNotMatch(
  css,
  /@media\(max-width:(?:560|600)px\)\{\.lt-page \./,
  "no .lt-page rule may introduce a second phone breakpoint beside 760px",
);

// ---- The status chips are the page's PRIMARY filter affordance and are anchors, which the
// app-wide phone touch block (`.main button,.btn,.btn.sm`) never matched: ~23px tap targets.
//
// These are asserted SEPARATELY and by their real selectors on purpose. The chips were
// `<a class="achip">` when this rule was written and became `.lt-chips.lt-seg > a` in the toolbar
// rebuild, at which point a single combined pattern naming only `.achip` still PASSED -- satisfied
// entirely by the Clear chip -- while guarding nothing for the control it was named after. A
// half-vacuous assertion is worse than a missing one: it reads as coverage. If the markup moves
// again, each of these must be re-pointed at whatever the control actually renders as.
assert.match(
  css,
  /\.lt-page \.lt-fsheet-host \.lt-seg a\{[^}]*min-height:40px/,
  "the status chips (.lt-seg a) need a >=40px tap target at phone width",
);
assert.match(
  css,
  /\.lt-page \.lt-fsheet-host \.achip\.lt-fclear\{[^}]*min-height:40px/,
  "the Clear chip needs a >=40px tap target at phone width",
);
// The active-filter chips and their remove buttons are the third affordance in this family.
assert.match(
  css,
  /\.lt-page \.lt-fsheet-host \.lt-factive \.achip\{[^}]*min-height:40px/,
  "active-filter chips need a >=40px tap target at phone width",
);

// ---- `vh` is the WRONG unit in an in-app webview: WhatsApp's chrome retracts, so a vh box is
// measured against a viewport the reader does not have and the overlay's bottom is unreachable.
// Both declarations must survive: `vh` first as the fallback, then `dvh`.
assert.match(
  css,
  /\.lt-fgroup\.open\{[^}]*max-height:84vh;max-height:84dvh/,
  "the filter sheet must use dvh with a vh fallback",
);
assert.match(
  css,
  /\.lt-modal\{[^}]*max-height:92vh;max-height:92dvh/,
  "the task modal must use dvh with a vh fallback",
);

// ---- The modal header (and its X) must stay reachable on a short phone: the BOX does not scroll,
// its body does.
assert.match(css, /\.lt-modal\{[^}]*overflow:hidden/, "the modal box must not be the scroller");
assert.match(
  css,
  /\.lt-modal \.lt-modal-hd\{[^}]*flex:0 0 auto/,
  "the modal header must not shrink or scroll away",
);
assert.match(
  css,
  /\.lt-modal \.lt-modal-bd\{[^}]*min-height:0;overflow-y:auto/,
  "the modal body must be the scroll container",
);

// ---- Every overlay on this page owes a phone reader a body scroll lock and a focus trap:
// without the lock a drag inside the sheet scrolled the LIST behind it.
assert.match(dialogShell, /document\.body\.style\.overflow = "hidden"/, "scroll lock");
assert.match(dialogShell, /previousOverflow/, "the scroll lock must restore the previous value");
assert.match(dialogShell, /event\.key !== "Tab"/, "focus trap");
for (const [name, source] of [
  ["the filter sheet", filters],
  ["the edit modal", editModal],
  ["the new-task modal", newModal],
]) {
  assert.match(source, /useDialogShell\(\{/, `${name} must use the shared dialog shell`);
}

// ---- The pending filter bar must NOT dim, because at phone width it CONTAINS the fixed sheet.
// The shared `.wfbusy{opacity:.55;pointer-events:none}` is written for a bar holding only its own
// controls. Here `.lt-fgroup.open` -- `position:fixed`, `z-index:151` -- is a CHILD of the bar, so
// every filter change took the whole open sheet to 55% opacity for as long as the transition ran:
// the page read straight through it, and the opacity's stacking context stopped the z-index
// lifting it clear. Measured at 390px on 2026-09-23 (before: opacity .55, a second pick while
// loading silently swallowed by `pointer-events:none`; after: opacity 1, both picks land).
assert.match(
  filters,
  /className=\{`lt-fbar lt-fsheet-host\$\{isPending \? " wfbusy" : ""\}`\}/,
  "the bar carries wfbusy while pending -- the precondition this rule exists for",
);
assert.match(
  filters,
  /className=\{`lt-fbar lt-fsheet-host[\s\S]*?className=\{`lt-fgroup\$\{sheetOpen \? " open" : ""\}`\}/,
  "the fixed filter sheet is a DESCENDANT of the bar, which is why dimming the bar dims the sheet",
);
const busyRule = css.match(/\.lt-page \.lt-fbar\.lt-fsheet-host\.wfbusy\{[^}]*\}/)?.[0] ?? "";
assert.match(busyRule, /opacity:1/, "the pending filter bar must never dim -- it holds the sheet");
assert.match(
  busyRule,
  /pointer-events:auto/,
  "the sheet must stay tappable while loading, or a second pick is swallowed",
);
assert.match(busyRule, /box-shadow:0 0 0 2px var\(--ring\)/, "the ring is the busy signal instead");
