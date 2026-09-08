import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));

const productPages = [
  "app/(admin)/tasks/page.tsx",
];

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
  assert.match(route, /listLeadershipTasks\(/, "production /tasks must fetch the backend Leadership Tasks page");

  const component = readFileSync(join(root, "features/leadership-tasks/leadership-tasks-page.tsx"), "utf8");
  assert.match(component, /page \? rowsFromPage\(page\) : preview \? fixtureTasks : \[\]/);
  assert.doesNotMatch(component, /page \? rowsFromPage\(page\) : fixtureTasks/);
});
