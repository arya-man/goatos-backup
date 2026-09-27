// guard: leave-toolbar-outside-panel, routines-toolbar-outside-panel (R3OPS-3).
// On /leave and /routines the filter toolbar sat INSIDE the URL-keyed panel: every filter change
// swapped the control the reader had just used to a skeleton, and an empty result removed the
// filters that could widen it. The toolbar row now renders before the card's UrlSuspense; only the
// table + footer are keyed. The dense switch crosses the boundary through a per-card context.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

for (const [name, page, chrome, row, table, scope] of [
  ["leave", "./leave-page.tsx", "./leave-toolbar.tsx", "LeaveToolbarRow", "LeaveTableChrome", "LeaveDenseScope"],
  ["routines", "../pen-routines/routines-page.tsx", "../pen-routines/routines-chrome.tsx", "RoutinesToolbarRow", "RoutinesTableChrome", "RoutinesDenseScope"],
]) {
  test(`${name}: toolbar row renders outside the keyed panel`, () => {
    const src = read(page);
    const rows = [...src.matchAll(new RegExp(`<${row}\\b`, "g"))].map((m) => m.index);
    assert.equal(rows.length, 2, `${name} renders one ${row} per table card`);
    for (const at of rows) {
      const next = src.indexOf("<UrlSuspense", at);
      const nextTable = src.indexOf(`<${table}`, at);
      assert.ok(next > at && next < nextTable, `${row} comes before the card's UrlSuspense`);
      const open = src.lastIndexOf("<UrlSuspense", at);
      const close = src.lastIndexOf("</UrlSuspense>", at);
      assert.ok(open === -1 || close > open, `${row} is not inside an open UrlSuspense`);
      assert.ok(src.lastIndexOf(`<${scope}>`, at) > src.lastIndexOf(`</${scope}>`, at), `${row} sits inside its card's ${scope}`);
    }
    for (const m of src.matchAll(/toolbar=\{\{/g)) {
      assert.ok(src.lastIndexOf(`<${row}`, m.index) > src.lastIndexOf(`<${table}`, m.index), `${table} no longer takes the toolbar`);
    }
    assert.doesNotMatch(src, /fallback=\{<TableSkeleton[^}]*toolbar=/, "the panel skeleton no longer draws a second toolbar");
  });

  test(`${name}: dense switch shared through a card context`, () => {
    const src = read(chrome);
    assert.match(src, new RegExp(`export function ${scope}\\(`));
    assert.match(src, new RegExp(`export function ${row}\\([\\s\\S]*?useContext\\(DenseContext\\)`));
    assert.match(src, new RegExp(`export function ${table}\\([\\s\\S]*?useContext\\(DenseContext\\)`));
  });
}

// guard: leave-chrome-serialisable. The server page handed `copyFor: (key) => copy(...)` to the client
// toolbar and pager; React refuses functions across the server/client boundary, so /leave threw a
// Server Components error the moment the chrome rendered. Copy crosses as a resolved labels map.
test("leave: the client chrome receives copy as a labels map, never a function", () => {
  const src = read("./leave-page.tsx");
  const chrome = read("./leave-toolbar.tsx");
  assert.doesNotMatch(src, /copyFor:\s*\(/, "no function prop into the client chrome");
  assert.match(src, /labels: chromeLabels,/);
  assert.match(src, /const LEAVE_CHROME_COPY_KEYS = \[/);
  for (const key of chrome.matchAll(/copyFor\("([^"]+)"\)/g)) assert.ok(src.includes(`"${key[1]}"`), `LEAVE_CHROME_COPY_KEYS lists ${key[1]}`);
  assert.doesNotMatch(chrome, /copyFor: \(key: string\) => string;/, "the chrome props declare no function");
});

// guard: list-toolbar-kebab (TR1-#23). The template list toolbar ends in one ⋮ popover: Export is a
// menu item, the Dense switch is the table footer's (no "Columns" text button that toggled it), and
// the "shown / total" count shows only beside active filter chips (never a stray "0 / 0").
test("leave and routines toolbars: one ⋮ menu, no stray count", () => {
  for (const file of ["../leave/leave-toolbar.tsx", "../pen-routines/routines-chrome.tsx"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    assert.doesNotMatch(src, /<Button[^>]*startIcon=\{<(Columns3|Download)\b/, `${file}: Columns / Export text buttons`);
    assert.match(src, /<RowMenu[\s\S]{0,200}onSelect: exportCsv/, `${file}: Export in the ⋮ menu`);
    assert.match(src, /\{chips\.length \? <Box component="span"[^>]*>\{shown\} \/ \{total\}<\/Box> : null\}/, `${file}: count only with chips`);
  }
});
