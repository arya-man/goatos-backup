// The fixture host (`/tasks-preview`) renders against `leadershipTasksFixtureContract` and has no
// backend, so its `copy` map is the WHOLE answer. `copy()` THROWS on a fixed key it cannot
// resolve, which makes a missing key a 500 at render -- caught by neither `tsc` nor the unit
// suite. It happened: the copy contract moved to the backend, `COPY_FALLBACKS["leadership-tasks"]`
// went with it, and the preview died on the first key it asked for (`crumb`).
//
// This resolves every key the feature's own source asks for, through the real `copy()`.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { copy } from "../../lib/admin-ui-contract.ts";
import { leadershipTasksFixtureContract as fixture } from "./fixture-contract.ts";

const here = dirname(fileURLToPath(import.meta.url));

// Every literal `copy(pageContract, "...")` key in the feature. A key built at runtime
// (`sort.${option}`, `feedback.${code}`) is covered separately below, because a template
// literal cannot be read statically.
const keys = new Set();
for (const file of readdirSync(here)) {
  if (!/\.tsx?$/.test(file)) continue;
  const source = readFileSync(join(here, file), "utf8");
  for (const match of source.matchAll(/copy\(\s*pageContract,\s*"([^"]+)"/g)) {
    keys.add(match[1]);
  }
}
assert.ok(keys.size > 20, `expected the feature to read many copy keys, found ${keys.size}`);

for (const key of [...keys].sort()) {
  assert.doesNotThrow(
    () => copy(fixture, key),
    `/tasks-preview cannot render: the fixture contract is missing copy key ${key}`,
  );
  assert.ok(copy(fixture, key).length > 0, `fixture copy key ${key} resolves to an empty string`);
}

// The runtime-built key spaces. `sort.*` is CLOSED (one key per TASK_SORTS member) and each is a
// fixed key that throws when absent; `feedback.*` is OPEN (the backend's error vocabulary) and is
// read with an empty default, so only the codes the page's own writes can return are asserted.
const { TASK_SORTS } = await import("./task-url.ts");
for (const sort of TASK_SORTS) {
  assert.doesNotThrow(
    () => copy(fixture, `sort.${sort}`),
    `the sort control cannot render: the fixture contract is missing copy key sort.${sort}`,
  );
}
for (const code of ["task_raised", "task_updated", "task_edited", "note_added", "version_conflict"]) {
  assert.equal(
    typeof copy(fixture, `feedback.${code}`, ""),
    "string",
    `feedback.${code} must resolve to a sentence`,
  );
  assert.ok(copy(fixture, `feedback.${code}`, "").length > 0, `feedback.${code} is empty`);
}

// The fixture's table mirrors the backend's `table("leadership-task-progress", ...)` column for
// column. `LeadershipTask` has no priority field, so the urgency column is `days_left`; a fixture
// on the retired `priority` key shows the preview a label the real page does not have.
const columnKeys = fixture.tables[0].columns.map((column) => column.key);
assert.deepEqual(
  columnKeys,
  ["task", "assignee", "raised_by", "status", "evidence", "days_left"],
  "fixture columns must mirror the backend contract's column list, in order",
);
