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
  assert.match(
    component,
    /page \? rowsFromPage\(page\) : preview \? fixtureTasks : \[\]/,
  );
  assert.doesNotMatch(component, /page \? rowsFromPage\(page\) : fixtureTasks/);
  assert.ok(
    component.includes("href={`/tasks?scope=${scope.key}`}"),
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
    /action=\{raiseLeadershipTaskAction\}/,
    "production raise form must submit to the backend server action",
  );
  assert.match(
    component,
    /name="idempotency_key"/,
    "web create must submit a rendered stable idempotency key",
  );
  assert.match(
    component,
    /name="attachment_proof_id"/,
    "web create must accept uploaded proof attachment refs",
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
    /attachmentRefs\(formData\)/,
    "server action must send non-empty attachment refs when provided",
  );
});
