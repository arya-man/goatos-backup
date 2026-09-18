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
  boardColumnsForFilter,
  boardColumnTotal,
  boardColumnHasTotal,
  safeTaskReturnTo,
  resolveTaskView,
  unprefixedTaskParamAliases,
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

test("a board keeps every column under a status filter, and the other columns read 0", () => {
  // B4, second answer (CEO, 2026-09-18): Jira keeps all columns under a status filter and lets
  // the others read 0. The earlier collapse-to-one-column made one status a full-width wall of
  // cards. What must still never happen is a live count above an empty column, so the excluded
  // columns' totals are 0, not the whole-list number.
  const all = ["open", "in_progress", "done", "cancelled"];
  for (const filter of ["all", "open", "in_progress", "done", "overdue"]) {
    assert.deepEqual([...boardColumnsForFilter(filter)], all, filter);
  }
  const totals = new Map([["open", 172], ["in_progress", 119], ["done", 117]]);
  assert.equal(boardColumnTotal("open", "all", totals), 172);
  assert.equal(boardColumnTotal("done", "done", totals), 117);
  assert.equal(boardColumnTotal("open", "done", totals), 0);
  assert.equal(boardColumnTotal("in_progress", "done", totals), 0);
  assert.equal(boardColumnTotal("cancelled", "all", totals), null);
  assert.equal(boardColumnTotal("open", "overdue", totals), null);
});

test("the overdue lens is a filter value, and no column claims a total under it", () => {
  // `overdue` is a lens the endpoint offers (open or in-progress tasks past their deadline),
  // never a status: it round-trips as a filter value, never normalises to `all`, and the board
  // under it draws To do + In progress -- each holding only its late cards -- rather than
  // inventing an "Overdue" column the Work Board has no lane for.
  assert.equal(normalizeTaskFilter("overdue"), "overdue");
  assert.deepEqual([...boardColumnsForFilter("overdue")], ["open", "in_progress", "done", "cancelled"]);
  // No column has a publishable whole-list total under the lens: the response's status counts
  // are the STATUS totals, not the late subset on screen, so the pill must read "—" like the
  // cancelled column's rather than a number the column is visibly not showing.
  for (const column of ["open", "in_progress"]) {
    assert.equal(boardColumnHasTotal(column, "overdue"), false);
    assert.equal(boardColumnHasTotal(column), true);
  }
  assert.equal(boardColumnHasTotal("cancelled", "all"), false);
});

test("`view` is read as an alias of `t_view`, and `t_view` wins when both are present", () => {
  // B5: `?view=list` looked like it selected a view and was silently ignored.
  assert.equal(resolveTaskView(undefined, "list"), "list");
  assert.equal(resolveTaskView(undefined, "board"), "board");
  assert.equal(resolveTaskView("board", "list"), "board");
  assert.equal(resolveTaskView("list", "board"), "list");
  // An empty `t_view=` does not shadow the alias.
  assert.equal(resolveTaskView("", "list"), "list");
  // Another screen's `view` value pasted onto /tasks selects nothing: the default view.
  assert.equal(resolveTaskView(undefined, "schedule"), "board");
  assert.equal(resolveTaskView(undefined, undefined), "board");
  // Because it is honoured, it is NOT reported as ignored.
  assert.deepEqual(unprefixedTaskParamAliases(["view"]), []);
});

test("an unprefixed parameter this page does not read is reported, not honoured", () => {
  assert.deepEqual(unprefixedTaskParamAliases(["scope", "filter", "task"]), []);
  // An alias BESIDE its real parameter is not a trap: the prefixed one wins unambiguously.
  assert.deepEqual(unprefixedTaskParamAliases(["sort", "t_sort"]), []);
  // Several at once, each named with the parameter it meant.
  assert.deepEqual(unprefixedTaskParamAliases(["q", "sort"]), [
    { alias: "q", param: "t_q" },
    { alias: "sort", param: "t_sort" },
  ]);
});

test("the board carries no person filter of its own", () => {
  // B1/B2: the avatar group and its dead `+5` overflow are gone, and the one searchable person
  // filter reuses the @-mention picker's ranking rather than being a second implementation.
  const board = readFileSync(new URL("./leadership-tasks-board.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(board, /ltb-person/);
  assert.doesNotMatch(board, /aria-hidden="true">\+/);
  const picker = readFileSync(new URL("./task-people-filter.tsx", import.meta.url), "utf8");
  assert.match(picker, /filterMentionCandidates/);
  assert.match(picker, /moveMentionHighlight/);
  // Not `@base-ui/react`, which is a dead dependency in this workspace. The prose above names
  // it, so the test looks for an IMPORT of it rather than a mention of it.
  assert.doesNotMatch(picker, /from\s+["']@base-ui/);
});
