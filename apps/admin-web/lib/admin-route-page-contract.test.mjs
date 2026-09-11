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
  assert.match(
    route,
    /listLeadershipTasks\(\{ scope, filter: "all", limit: 50 \}\)/,
    "production /tasks must pass the selected scope to the backend",
  );

  const component = readFileSync(
    join(root, "features/leadership-tasks/leadership-tasks-page.tsx"),
    "utf8",
  );
  const newTaskModal = readFileSync(
    join(root, "features/leadership-tasks/new-task-modal.tsx"),
    "utf8",
  );
  assert.match(
    component,
    /page \? rowsFromPage\(page\) : preview \? fixtureTasks : \[\]/,
  );
  assert.doesNotMatch(component, /page \? rowsFromPage\(page\) : fixtureTasks/);
  assert.ok(
    component.includes(
      'href={`${preview ? "/tasks-preview" : "/tasks"}?scope=${scope.key}`}',
    ),
    "scope chips must navigate to the selected backend scope",
  );
  assert.match(
    component,
    /preview \?/,
    "preview-only activity must stay behind the preview flag",
  );
  assert.match(
    component,
    /liveFeedRows\(selected\)/,
    "production activity must be derived from live task fields",
  );
  assert.match(
    component,
    /task=\$\{encodeURIComponent\(task\.id\)\}/,
    "web task rows must be selectable so any task's notes and attachments can be inspected",
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
    component,
    /\/api\/leadership-tasks\/attachments\//,
    "web monitor must open task-scoped attachments through the admin proxy",
  );

  const actions = readFileSync(
    join(root, "features/leadership-tasks/actions.ts"),
    "utf8",
  );
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
