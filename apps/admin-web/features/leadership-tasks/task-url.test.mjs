import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import {
  DEFAULT_TASK_SORT,
  normalizeTaskDateRange,
  normalizeTaskFilter,
  normalizeTaskQuery,
  normalizeTaskScope,
  normalizeTaskSort,
  safeTaskReturnTo,
  TASK_QUERY_MAX,
  taskUuidFilter,
} from "./task-url.ts";

test("a date range is only applied when BOTH ends are present and in order", () => {
  // The list endpoint answers 400 invalid_date_range for half a span, so half a span must never
  // be sent -- the bar shows a note instead.
  assert.deepEqual(normalizeTaskDateRange(undefined, undefined), { incomplete: false });
  assert.deepEqual(normalizeTaskDateRange("2026-09-01", "2026-09-30"), {
    from: "2026-09-01",
    to: "2026-09-30",
    incomplete: false,
  });
  assert.equal(normalizeTaskDateRange("2026-09-01", "").incomplete, true);
  assert.equal(normalizeTaskDateRange("", "2026-09-30").incomplete, true);
  // from > to is the other 400 on the same code.
  assert.equal(normalizeTaskDateRange("2026-09-30", "2026-09-01").incomplete, true);
  // A half-typed span keeps showing what the reader typed.
  assert.equal(normalizeTaskDateRange("2026-09-01", "").from, "2026-09-01");
  // Datetimes are accepted as well as dates.
  assert.equal(
    normalizeTaskDateRange("2026-09-01T00:00:00+05:30", "2026-09-30T23:59:00+05:30").incomplete,
    false,
  );
  assert.equal(normalizeTaskDateRange("01/09/2026", "30/09/2026").incomplete, true);
});

test("return_to accepts /tasks and its query only -- never the fixture host", () => {
  assert.equal(safeTaskReturnTo("/tasks?scope=team_progress"), "/tasks?scope=team_progress");
  assert.equal(safeTaskReturnTo("/tasks"), "/tasks");
  // The defect: startsWith("/tasks") accepted the fixture host as the landing page of a live write.
  assert.equal(safeTaskReturnTo("/tasks-preview"), "/tasks?scope=assigned_by_me");
  assert.equal(safeTaskReturnTo("/tasks-preview?scope=team_progress"), "/tasks?scope=assigned_by_me");
  assert.equal(safeTaskReturnTo("/tasks/17"), "/tasks?scope=assigned_by_me");
  assert.equal(safeTaskReturnTo("https://evil.example/tasks"), "/tasks?scope=assigned_by_me");
  assert.equal(safeTaskReturnTo("//evil.example/tasks"), "/tasks?scope=assigned_by_me");
  assert.equal(safeTaskReturnTo(undefined), "/tasks?scope=assigned_by_me");
});

test("free text is trimmed and capped at the contract's 120 characters", () => {
  assert.equal(normalizeTaskQuery("  fence  "), "fence");
  assert.equal(normalizeTaskQuery("   "), undefined);
  assert.equal(normalizeTaskQuery(undefined), undefined);
  assert.equal(normalizeTaskQuery("x".repeat(200))?.length, TASK_QUERY_MAX);
});

test("sort, scope and status filter are closed enums with a default", () => {
  assert.equal(normalizeTaskSort(undefined), DEFAULT_TASK_SORT);
  assert.equal(normalizeTaskSort("deadline_asc"), "deadline_asc");
  // An unknown sort is a 400 invalid_sort; it degrades to the default rather than being sent.
  assert.equal(normalizeTaskSort("title_asc"), DEFAULT_TASK_SORT);
  assert.equal(normalizeTaskScope("assigned_to_me"), "assigned_to_me");
  assert.equal(normalizeTaskScope("nonsense"), "team_progress");
  assert.equal(normalizeTaskFilter("in_progress"), "in_progress");
  assert.equal(normalizeTaskFilter("cancelled"), "all");
});

test("a malformed uuid filter is dropped rather than sent as a 400", () => {
  assert.equal(taskUuidFilter("11111111-1111-4111-8111-111111111111"), "11111111-1111-4111-8111-111111111111");
  assert.equal(taskUuidFilter(" 11111111-1111-4111-8111-111111111111 "), "11111111-1111-4111-8111-111111111111");
  assert.equal(taskUuidFilter("not-a-uuid"), undefined);
  assert.equal(taskUuidFilter(undefined), undefined);
});

test("the page keeps OFFSET pagination and the top-bar scope keys out of the feature", () => {
  // make scale-guard blocks offset pagination on this endpoint, and check-ia-guard.mjs forbids a
  // page re-reading the shell's scope. Both are asserted on source because both are about what
  // the feature must NOT contain. Comments are stripped first, the same way the guard script
  // does it -- prose that NAMES the rule must not read as a breach of it.
  const stripComments = (text) =>
    text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
  for (const file of [
    "./params.ts",
    "./leadership-tasks-page.tsx",
    "./leadership-tasks-filters.tsx",
    "../../app/(admin)/tasks/page.tsx",
  ]) {
    const source = stripComments(readFileSync(new URL(file, import.meta.url), "utf8"));
    assert.doesNotMatch(source, /total_count/, file);
    assert.doesNotMatch(
      source,
      /["'`](?:park|as_of|range|scope_mode|date_from|date_to)["'`]/,
      file,
    );
  }
  // No offset reaches the URL or the request. `WorklistPager`'s props are offset-shaped, so the
  // page derives a DISPLAY position from the cursor stack for it; that name may appear there and
  // nowhere else.
  for (const file of ["./params.ts", "../../app/(admin)/tasks/page.tsx"]) {
    const source = stripComments(readFileSync(new URL(file, import.meta.url), "utf8"));
    assert.doesNotMatch(source, /offset/i, file);
  }
});
