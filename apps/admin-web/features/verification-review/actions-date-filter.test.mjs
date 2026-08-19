import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const filterSource = readFileSync(new URL("./actions-date-filter.tsx", import.meta.url), "utf8");
const paramsSource = readFileSync(new URL("./actions-date-params.ts", import.meta.url), "utf8");
const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const shellSource = readFileSync(new URL("../../components/mesha-shell.tsx", import.meta.url), "utf8");

test("the Actions capture-date filter is page-scoped, not a revived top-bar scope control", () => {
  // The shell's as-of picker was removed on purpose; this control must not bring it back by the
  // side door, or every screen that reads as_of (Control Tower, Adherence, Vaccination — which
  // 400s on a past date) inherits a date scope nobody asked it to honour.
  assert.doesNotMatch(shellSource, /<TopBarDatePicker/);
  assert.doesNotMatch(shellSource, /ActionsDateFilter/);
  assert.match(pageSource, /<ActionsDateFilter/);
  assert.match(filterSource, /basePath/);
  // It writes its OWN params, never as_of.
  assert.doesNotMatch(filterSource, /"as_of"/);
});

test("the shared date params live OUTSIDE the client boundary", () => {
  // A constant imported from a "use client" module reaches a server component as a client-reference
  // PROXY, not the string — so `one(sp, DATE_FROM_PARAM)` matched nothing and every selection fell
  // back to today while typecheck, lint and the guards all stayed green. Reproduced live on
  // 2026-08-12 before the constants were moved here.
  assert.doesNotMatch(paramsSource, /^\s*["']use client["']/);
  assert.match(paramsSource, /DATE_FROM_PARAM = "vd_from"/);
  assert.match(paramsSource, /DATE_TO_PARAM = "vd_to"/);
  const importsParams = /from "\.\/actions-date-params"/;
  assert.match(filterSource, importsParams);
  assert.match(pageSource, importsParams);
  // ...and the picker must not re-export them, which would put the proxy back in reach.
  assert.doesNotMatch(filterSource, /export const DATE_(FROM|TO)_PARAM/);
});

// The landing default is a recent WINDOW, not today (maintainer decision 2026-08-17). Proof arrives
// on the day it is captured and is reviewed later, so a queue pinned to today shows an empty board
// on top of a full backlog -- observed on real data: 402 pending weighing proofs across the previous
// twelve days, and a board reading "No actions to review". The backend already said this in
// ports.ListQueueParams.IsVerifierQueueRead ("does NOT clamp to today"); this page was overriding it.
test("the landing default is a recent window, not today alone, and is expressed by absence", () => {
  // parseDateRange falls through to the window when no params are present...
  assert.match(pageSource, /return \{ from: businessDaysBefore\(today, DEFAULT_QUEUE_WINDOW_DAYS\), to: today \};/);
  assert.match(pageSource, /const DEFAULT_QUEUE_WINDOW_DAYS = \d+;/);
  assert.doesNotMatch(
    pageSource,
    /return \{ from: today, to: today \};/,
    "landing on today alone hides the backlog the verifier is meant to be working",
  );
  assert.match(pageSource, /const today = todayIso\(\);/);
  // ...and only a selection equal to that DEFAULT WINDOW clears the params. Selecting today alone
  // must WRITE vd_from/vd_to=today into the URL: absence now means the whole window, so deleting
  // the params on a today-only pick (the pre-fix behavior) silently widened the board back to two
  // weeks and read as the date filter not working at all — the 2026-08-19 defect.
  assert.match(
    filterSource,
    /if \(nextFrom === defaultFrom && nextTo === today\) \{\s*\n\s*next\.delete\(DATE_FROM_PARAM\);\s*\n\s*next\.delete\(DATE_TO_PARAM\);/,
  );
  assert.doesNotMatch(
    filterSource,
    /nextFrom === today && nextTo === today/,
    "a today-only selection must pin the date into the URL, not fall back to the window",
  );
  // The page hands the filter the same window start parseDateRange falls back to, so the two
  // sides cannot disagree about what absence means.
  assert.match(pageSource, /defaultFrom=\{businessDaysBefore\(today, DEFAULT_QUEUE_WINDOW_DAYS\)\}/);
});






test("changing the date drops the keyset cursor, its back-trail and the open row", () => {
  // A cursor is a position in ONE filtered sequence; carrying it into another lands on an
  // unrelated slice of the queue. Same reason the module and status filters reset them.
  assert.match(filterSource, /RESET_ON_FILTER = \["vi_row", "vi_cursor", "vi_trail", "va_status", "va_code"\]/);
  assert.match(filterSource, /for \(const key of RESET_ON_FILTER\) next\.delete\(key\);/);
});

test("the selection stays responsive while the App Router refresh is in flight", () => {
  // Without the optimistic value the label and the highlighted cells snap back to the previous
  // date for the length of the round trip — the stale-select defect that hit Feed Config.
  assert.match(filterSource, /useTransition/);
  assert.match(filterSource, /startTransition\(\(\) => \{/);
  assert.match(filterSource, /const selFrom = live\?\.from \?\? from;/);
  // Derived away when the server props catch up — never cleared by an effect, which would fire a
  // second render pass after the correct value had already painted.
  assert.match(filterSource, /const live = optimistic\?\.overrides === serverSelection \? optimistic : null;/);
});

test("every visible string on the picker is backend-contract copy", () => {
  for (const key of [
    "filter.date",
    "filter.date.today",
    "filter.date.single",
    "filter.date.range",
    "filter.date.aria",
    "filter.date.previous_month",
    "filter.date.next_month",
    "filter.date.range_start_hint",
    "filter.date.range_end_hint",
    "filter.date.range_separator",
  ]) {
    assert.match(pageSource, new RegExp(`copy\\(pageContract, "${key.replace(/\./g, "\\.")}"\\)`), key);
  }
});
