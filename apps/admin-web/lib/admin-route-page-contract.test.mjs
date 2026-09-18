import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));

const productPages = ["app/(admin)/tasks/page.tsx"];

test("production admin product pages require backend page contracts", () => {
  for (const page of productPages) {
    const abs = join(root, page);
    const source = readFileSync(abs, "utf8");
    assert.match(
      source,
      /requireAdminWebPageContract\(/,
      `${relative(root, abs)} must fail closed through the backend page contract`,
    );
  }
});

test("production Tasks page uses live backend data, with fixtures confined to preview", () => {
  const route = readFileSync(join(root, "app/(admin)/tasks/page.tsx"), "utf8");
  assert.match(
    route,
    /listLeadershipTasks\(/,
    "production /tasks must fetch the backend Leadership Tasks page",
  );
  assert.match(
    route,
    /listLeadershipTaskAssignees\(/,
    "production /tasks must fetch the backend Leadership Tasks assignee picker",
  );
  assert.match(
    route,
    /searchParams/,
    "production /tasks must read the selected scope from query params",
  );
  // The route threads the WHOLE list state through, not just the scope: the status filter, the
  // page size and the keyset cursor are URL state now, so pinning the old literal
  // `listLeadershipTasks({ scope, filter: "all", limit: 50 })` would pin the page to the one
  // un-filterable, un-pageable call the rewrite exists to replace. What must stay true is that
  // every one of these comes from the parsed params and nothing is hardcoded.
  assert.match(
    route,
    /parseTasksParams\(/,
    "production /tasks must parse its list state from the URL",
  );
  for (const field of [
    /scope: params\.scope/,
    /filter: params\.filter/,
    /limit: params\.limit/,
    /cursor: params\.cursor/,
    /sort: params\.sort/,
    /q: params\.q/,
  ]) {
    assert.match(route, field, `production /tasks must pass ${field} to the backend`);
  }
  // No offset and no total: OFFSET pagination on this endpoint is blocked by `make scale-guard`.
  assert.doesNotMatch(route, /offset/i, "production /tasks must not paginate by offset");

  const component = readFileSync(
    join(root, "features/leadership-tasks/leadership-tasks-page.tsx"),
    "utf8",
  );
  const newTaskModal = readFileSync(
    join(root, "features/leadership-tasks/new-task-modal.tsx"),
    "utf8",
  );
  // The selected-task rail moved OUT of the page into its own component when the status board
  // landed (2026-09-18): the page has ONE call site for it and the panel owns the whole detail,
  // including the no-selection state. The assertions that describe the rail therefore read the
  // panel; the ones that describe the page's own shell still read the page.
  const detailPanel = readFileSync(
    join(root, "features/leadership-tasks/task-detail-panel.tsx"),
    "utf8",
  );
  assert.match(
    component,
    /page \? rowsFromPage\(page\) : preview \? fixtureTasks : \[\]/,
  );
  assert.doesNotMatch(component, /page \? rowsFromPage\(page\) : fixtureTasks/);
  // The scope tabs are the shared `SegmentedLinks` control now, and their hrefs are built by the
  // feature's own URL helper so the rest of the filter state survives a scope change.
  assert.match(component, /<SegmentedLinks/, "scope tabs must use the shared segmented control");
  assert.match(
    component,
    /\[TASK_PARAM\.scope\]: scope\.key/,
    "scope tabs must navigate to the selected backend scope",
  );
  assert.match(
    component,
    /preview \?/,
    "preview-only activity must stay behind the preview flag",
  );
  // The page renders the rail through ONE call site and passes the selected row (or null) down.
  assert.match(
    component,
    /<TaskDetailPanel\b/,
    "the page must render the selected task through the detail panel component",
  );
  assert.match(
    component,
    /detail=\{selected \?\? null\}/,
    "the panel owns the no-selection state; the page passes the row or null",
  );
  // Production activity is the task's OWN feed and notes (`LeadershipTask.activity` /
  // `notes`), handed verbatim to the tabbed feed -- never invented on the client and never a
  // fixture.
  assert.match(
    detailPanel,
    /activity=\{detail\.activity\}/,
    "production activity must be derived from live task fields",
  );
  assert.match(
    detailPanel,
    /notes=\{detail\.notes\}/,
    "comment text must come from the task's own notes",
  );
  assert.doesNotMatch(
    detailPanel,
    /fixtureTasks/,
    "the detail panel must never render fixture rows",
  );
  // Row selection moved into the contract-driven table component with the column set.
  const tasksTable = readFileSync(
    join(root, "features/leadership-tasks/leadership-tasks-table.tsx"),
    "utf8",
  );
  assert.match(
    tasksTable,
    /tasksHref\(basePath, sp, \{ \[TASK_PARAM\.scope\]: scopeKey, \[TASK_PARAM\.task\]: task\.id \}\)/,
    "web task rows must be selectable (task= added to the current URL, so the view and filters survive the click)",
  );
  assert.match(
    tasksTable,
    /columnsFromContract<TaskRow>\(contract/,
    "the task table's columns must come from the backend table contract",
  );
  assert.match(
    component,
    /action=\{raiseLeadershipTaskAction\}/,
    "production raise form must submit to the backend server action",
  );
  assert.match(
    newTaskModal,
    /name="idempotency_key"/,
    "web create must submit a rendered stable idempotency key",
  );
  assert.match(
    newTaskModal,
    /type="file"[\s\S]*name="attachment_file"[\s\S]*multiple/,
    "web create must provide a real file/audio upload control",
  );
  assert.match(
    detailPanel,
    /\/api\/leadership-tasks\/attachments\//,
    "web monitor must open task-scoped attachments through the admin proxy",
  );

  // THE EDIT COMMAND. POST /app/leadership-tasks/{task_id}/edit existed with no web client at
  // all, which is what "task editing is not working" meant.
  const editModal = readFileSync(
    join(root, "features/leadership-tasks/edit-task-modal.tsx"),
    "utf8",
  );
  assert.match(detailPanel, /action=\{editLeadershipTaskAction\}/, "the detail panel must offer Edit");
  assert.match(detailPanel, /detail\.canEdit \?/, "Edit must be offered only when the row allows it");
  assert.match(editModal, /name="row_version"/, "the edit form must carry the row version fence");
  // Nothing rendered the outcome of a write before this change.
  assert.match(component, /<TaskFeedbackBanner/, "writes must report their outcome on the page");

  const actions = readFileSync(
    join(root, "features/leadership-tasks/actions.ts"),
    "utf8",
  );
  // The feedback params are task_status / task_code: features/vaccination-live-tracker/params.ts
  // already owns the name `lt_status`.
  // Comments stripped first: the prose that NAMES the retired parameter must not read as a use
  // of it.
  assert.doesNotMatch(
    actions.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1"),
    /lt_(?:status|code)/,
    "task feedback params must not collide with the live tracker's",
  );
  assert.match(actions, /editLeadershipTask\(/, "the edit server action must call the backend edit command");
  assert.doesNotMatch(
    actions,
    /randomUUID\(/,
    "server action must not mint a fresh idempotency key on retry",
  );
  assert.match(
    actions,
    /uploadLeadershipTaskAttachment\(/,
    "server action must upload selected file/audio before raising the task",
  );
  assert.match(
    actions,
    /attachmentRefs\(formData\)/,
    "server action must send non-empty attachment refs when provided",
  );
  assert.match(
    actions,
    /existingRefs\.length \+ files\.length > 12/,
    "server action must reject the combined attachment count before uploading",
  );
  assert.doesNotMatch(
    actions,
    /return refs\.slice\(0, 12\)/,
    "existing proof refs must not be truncated before the combined attachment cap check",
  );
  const apiServer = readFileSync(join(root, "lib/api/server.ts"), "utf8");
  assert.match(
    apiServer,
    /file_name\?: string/,
    "raiseLeadershipTask wrapper must use backend attachment file_name, not label",
  );
  assert.match(
    apiServer,
    /proof_type: "attachment"/,
    "leadership task uploads must register as proof attachments even for audio/video/photo bytes",
  );
  assert.match(
    apiServer,
    /headers: idempotencyKey \? \{ "Idempotency-Key": idempotencyKey \}/,
    "proof upload creation must send the stable attachment idempotency key as a header",
  );
});
