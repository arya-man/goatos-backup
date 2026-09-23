// The Feed follow-up table's "Directions compared" column renders its two feed days as
// DD/MM/YYYY, like every other date on the page (maintainer lock 2026-09-10).
//
// They were the one place on the page that did not: `before_day` / `after_day` arrive as the
// wire's ISO business day (`feed_day::text` -> `2026-09-21`) and were substituted into the
// backend's "{before} → {after}" copy verbatim, so the column read `2026-09-21 → 2026-09-23`
// beside the event-date cell one column to its left already reading `21/09/2026`.
//
// date-format-guard cannot see this shape: all of its patterns require the date field to sit in
// a JSX TEXT node (`>{row.some_date}<`), and a field injected through `.replace()` into a copy
// string never does. Hence this test — the guard stays green either way.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const page = readFileSync(join(here, "feed-follow-up.tsx"), "utf8");

test("both compared feed days go through fmtDate", () => {
  for (const field of ["before_day", "after_day"]) {
    const placeholder = field === "before_day" ? "before" : "after";
    assert.match(
      page,
      new RegExp(String.raw`\.replace\("\{${placeholder}\}", fmtDate\(check\.${field}\)\)`),
      `check.${field} must be formatted, not substituted raw`,
    );
  }
});

test("no date field is substituted into copy unformatted anywhere on this page", () => {
  // A BARE argument only: `.replace("{x}", check.some_day)`. An argument carrying a call --
  // fmtDate(...) -- is the fixed shape, so the capture stops at the first "(" on purpose.
  const raw = [...page.matchAll(/\.replace\("\{[a-z_]+\}",\s*([A-Za-z_$][\w$.]*)\s*\)/g)]
    .map((m) => m[1].trim())
    .filter((arg) => /\.(?:\w*_date|\w*_day|\w*_at)$/.test(arg));
  assert.deepEqual(raw, [], `these date fields reach the screen unformatted: ${raw.join(", ")}`);
});

test("the event date keeps the same helper, so one row cannot mix two date shapes", () => {
  assert.match(page, /\{fmtDate\(check\.event_date\)\}/);
});
