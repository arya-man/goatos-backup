import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// /calendar month grid (salvage/ffix-a, 2026-09-27): the page shell's legacy table rules
// (`.wrap/.screen/.main th|td`: grey fill, 48-57px heights, 16-24px padding, sticky thead) also
// matched FullCalendar's raw cells, drawing a tall grey weekday band and insetting the weekend
// shading; the month grid sat in a fixed-height scroller that clipped the last week row; week /
// agenda titles were US-style dates.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("guard: calendar-legacy-table-reset -- FullCalendar cells outrank the shell's legacy th/td rules", () => {
  // The reset lives in the app adapter layered on the verbatim template CalendarRoot.
  const styles = read("../../components/app/calendar/calendar-root.tsx");
  assert.match(styles, /styled\(TemplateCalendarRoot\)/);
  assert.match(styles, /"& \.fc\.fc th, & \.fc\.fc td": \{\s*padding: 0,\s*height: "auto",/);
  assert.match(styles, /background: "transparent",\s*position: "static",/);
  assert.match(styles, /\.\.\.legacyTableReset,\s*\.\.\.tableHeadStyles,/);
});

test("guard: calendar-month-natural-height -- md+ month grid is height auto, titles are DD/MM/YYYY", () => {
  const view = read("./calendar-full-view.tsx");
  assert.match(view, /height=\{mdUp \? "auto" : undefined\}/);
  assert.match(view, /minHeight: \{ xs: "70vh", md: 0 \}/);
  assert.doesNotMatch(view, /setTitle\(api\.view\.title\)|setTitle\(arg\.view\.title\)/);
  // `views` must be module-level: a fresh object each render loops FullCalendar's datesSet.
  assert.match(view, /^const VIEW_DATE_FORMATS = \{/m);
});
