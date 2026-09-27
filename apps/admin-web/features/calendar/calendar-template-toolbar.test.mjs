import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// TR1-#25 (/calendar vs the template calendar view): the heading carried a Week/History segmented
// toggle, owner filter chips sat above the card, a workstream tab row sat inside the card above the
// toolbar, "Today" was an outlined button and the template's filter icon was missing.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("guard: calendar-template-toolbar -- solid error Today + filter icon with the reset dot", () => {
  const toolbar = read("../../components/app/calendar/calendar-toolbar.tsx");
  const today = toolbar.slice(toolbar.indexOf("const renderTodayAndFilters"), toolbar.indexOf("const renderLoading"));
  assert.match(today, /size="small"\s*color="error"\s*variant="contained"/, "Today is the template's contained error button");
  assert.doesNotMatch(today, /variant="outlined"/);
  assert.match(today, /icon="ic:round-filter-list"/, "template filter icon");
  assert.match(today, /<Badge color="error" variant="dot" invisible=\{!canReset\}>/);
  // No dead control: the filter button exists only when the page wires a drawer.
  assert.match(today, /\{onOpenFilters \? \(/);
});

test("guard: calendar-template-toolbar -- no heading toggle, no chip row, no tab row in the card", () => {
  const page = read("./calendar.tsx");
  const view = read("./calendar-full-view.tsx");
  assert.doesNotMatch(page, /\bButtonGroup\b|\bTemplateTabs\b|\bFilterChip\b|\bLinkButton\b/, "window / owner / workstream live in the filters drawer");
  assert.doesNotMatch(page, /<PageHeader[^>]*actions=/s, "the template's Add event has no Mesha action, so the heading has none");
  assert.match(page, /<LinkFiltersResult totalResults=/, "applied filters show as the template CalendarFiltersResult chips");
  assert.match(page, /filters=\{filters\}/);
  assert.doesNotMatch(view, /\{header\}/, "nothing renders between the Card and CalendarRoot");
  assert.match(view, /<CalendarFilters open=\{filtersOpen\}/);
  const drawer = read("./calendar-filters.tsx");
  assert.match(drawer, /<MinimalDrawer[\s\S]*width=\{320\}[\s\S]*canReset=\{model\.canReset\}/, "template 320px filters drawer with the reset dot");
  assert.match(drawer, /minHeight: "var\(--tap-min\)"/, "44px choices in the webview");
});
