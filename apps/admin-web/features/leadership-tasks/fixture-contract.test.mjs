// The fixture host (`/tasks-preview`) renders against `leadershipTasksFixtureContract` and has no
// backend, so its `copy` map is the WHOLE answer. `copy()` THROWS on a fixed key it cannot
// resolve, which makes a missing key a 500 at render -- caught by neither `tsc` nor the unit
// suite. It happened: the copy contract moved to the backend, `COPY_FALLBACKS["leadership-tasks"]`
// went with it, and the preview died on the first key it asked for (`crumb`).
//
// This resolves every key the feature's own source asks for, through the real `copy()`.
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, sep } from "node:path";

import { copy } from "../../lib/admin-ui-contract.ts";
import { leadershipTasksFixtureContract as fixture } from "./fixture-contract.ts";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = join(here, "..", "..");

// Every literal `copy(<contract>, "...")` key the feature RENDERS -- which is not the same as the
// keys written in this directory. A shared component the page renders reads its own keys, and it
// throws from the page's contract just the same: `components/worklist-pager.tsx` reads
// `action.previous`, `action.next`, `pager.page` and `pager.rows`, and because the earlier version
// of this guard only walked `readdirSync(here)`, all four were invisible to it -- the guard passed
// green while /tasks-preview died on `action.previous`. So follow `@/components/...` imports
// transitively out of the feature and scan those too.
//
// A key built at runtime (`sort.${option}`, `feedback.${code}`) is covered separately below,
// because a template literal cannot be read statically.
function resolveModule(specifier) {
  const base = join(webRoot, specifier.replace(/^@\//, ""));
  for (const candidate of [`${base}.tsx`, `${base}.ts`, join(base, "index.tsx"), join(base, "index.ts")]) {
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  }
  return null;
}

const scanned = new Set();
const sources = [];
const queue = [];
for (const file of readdirSync(here)) {
  if (/\.tsx?$/.test(file)) queue.push(join(here, file));
}
// The fixture HOST renders the feature too, and may pull in shared components of its own.
for (const hostDir of [join(webRoot, "app", "tasks-preview")]) {
  if (!existsSync(hostDir)) continue;
  for (const file of readdirSync(hostDir)) {
    if (/\.tsx?$/.test(file)) queue.push(join(hostDir, file));
  }
}

while (queue.length > 0) {
  const file = queue.pop();
  if (scanned.has(file)) continue;
  scanned.add(file);
  const source = readFileSync(file, "utf8");
  sources.push([file, source]);
  // Only shared UI is followed. Following every `@/...` import would drag in unrelated features
  // whose keys this page's contract is not required to satisfy.
  for (const match of source.matchAll(/from\s*"(@\/components\/[^"]+)"/g)) {
    const resolved = resolveModule(match[1]);
    if (resolved) queue.push(resolved);
  }
}

const sharedScanned = [...scanned].filter((file) => file.includes(`${sep}components${sep}`));
assert.ok(
  sharedScanned.some((file) => file.endsWith(`${sep}worklist-pager.tsx`)),
  "the scan must reach components/worklist-pager.tsx -- the page renders it and it reads four copy keys",
);

// Any local name, not just `pageContract`: a shared component names the parameter whatever it
// likes, and the key still has to resolve from this page's contract.
const keys = new Set();
for (const [, source] of sources) {
  for (const match of source.matchAll(/\bcopy\(\s*([A-Za-z_$][\w$]*)\s*,\s*"([^"]+)"\s*\)/g)) {
    keys.add(match[2]);
  }
  // The New task modal reads through a local `text(key, fallback)` (its contract prop is
  // optional), and those keys are contract keys just the same -- pin them too, so a modal label
  // cannot slide back into a hard-coded word that the backend map never carried.
  for (const match of source.matchAll(/\btext\(\s*"([^"]+)"\s*,\s*"[^"]*"\s*\)/g)) {
    keys.add(match[1]);
  }
}
assert.ok(keys.size > 20, `expected the feature to read many copy keys, found ${keys.size}`);
for (const key of ["action.previous", "action.next", "pager.page", "pager.rows"]) {
  assert.ok(keys.has(key), `the shared pager's ${key} must be discovered by the scan`);
}

for (const key of [...keys].sort()) {
  assert.doesNotThrow(
    () => copy(fixture, key),
    `/tasks-preview cannot render: the fixture contract is missing copy key ${key}`,
  );
  assert.ok(copy(fixture, key).length > 0, `fixture copy key ${key} resolves to an empty string`);
}

// The fixture is only half the answer. The LIVE /tasks page takes its copy from the Go map, so a
// key that resolves here and not there is still a blank page for every real user. Assert the
// backend map satisfies the same key set.
//
// `pageCopy(id)` is the shared base map overlaid with `pageSpecificCopy(id)`, so the backend's
// effective map for this route is the union of the two -- which is why keys like `action.previous`
// need no page-specific entry.
const serviceGo = join(webRoot, "..", "..", "backend", "internal", "adminui", "app", "service.go");
const go = readFileSync(serviceGo, "utf8");

// Go string literals, escapes included, as written in a `"key": "value",` map entry.
const PAIR = /"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)"/g;

function sliceBlock(text, startMarker, endPredicate) {
  const start = text.indexOf(startMarker);
  assert.notEqual(start, -1, `could not find ${startMarker} in service.go -- update this guard`);
  const rest = text.slice(start + startMarker.length);
  const end = endPredicate(rest);
  assert.ok(end > 0, `could not find the end of ${startMarker} in service.go -- update this guard`);
  return rest.slice(0, end);
}

// Base map: everything between the func header and the overlay loop.
const baseBlock = sliceBlock(
  go,
  "func pageCopy(id string) map[string]string {",
  (rest) => rest.indexOf("for key, value := range pageSpecificCopy(id)"),
);
// Page-specific map: the `case "leadership-tasks":` arm, up to the next case arm.
const specificFn = sliceBlock(
  go,
  "func pageSpecificCopy(id string) map[string]string {",
  (rest) => rest.indexOf("\n}\n"),
);
const specificBlock = sliceBlock(
  specificFn,
  '\tcase "leadership-tasks":',
  (rest) => {
    const next = rest.indexOf("\n\tcase ");
    return next === -1 ? rest.length : next;
  },
);

const backendCopy = new Map();
for (const block of [baseBlock, specificBlock]) {
  for (const match of block.matchAll(PAIR)) backendCopy.set(match[1], match[2]);
}
// `page()` sets these two from its arguments rather than from a map literal.
backendCopy.set("page.title", fixture.title);
backendCopy.set("page.subtitle", fixture.subtitle);

assert.ok(
  backendCopy.has("crumb") && backendCopy.has("pager.page"),
  "the service.go parse produced no recognisable copy map -- update this guard",
);

const missingFromBackend = [...keys].sort().filter((key) => {
  if (backendCopy.has(key)) return false;
  // The runtime-built spaces are asserted below; only fixed keys are checked here.
  return true;
});
assert.deepEqual(
  missingFromBackend,
  [],
  `the LIVE /tasks page cannot render: backend/internal/adminui/app/service.go does not serve ${missingFromBackend.join(", ")} for route leadership-tasks. Add the key to pageSpecificCopy("leadership-tasks"), or to the shared base map in pageCopy if other pages read it too.`,
);

// The runtime-built key spaces. `sort.*` is CLOSED (one key per TASK_SORTS member) and each is a
// fixed key that throws when absent; `feedback.*` is OPEN (the backend's error vocabulary) and is
// read with an empty default, so only the codes the page's own writes can return are asserted.
const { TASK_SORTS } = await import("./task-url.ts");
for (const sort of TASK_SORTS) {
  assert.doesNotThrow(
    () => copy(fixture, `sort.${sort}`),
    `the sort control cannot render: the fixture contract is missing copy key sort.${sort}`,
  );
  assert.ok(
    backendCopy.has(`sort.${sort}`),
    `the LIVE /tasks sort control cannot render: service.go does not serve sort.${sort} for leadership-tasks`,
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

// The board's own sentences were read through the 3-arg `copy(contract, key, fallback)` form,
// which never throws -- so the contract could silently NOT serve them and the fallback would
// render instead (pending-work P4). The contract owns copy: each of these must be served by
// service.go, mirrored in the fixture, and the frontend fallback must say the same thing, so the
// screen reads identically before and after the contract arrives and nobody rewords one side.
const BOARD_KEYS = [
  "board.aria",
  "board.on_this_page",
  "board.total_unavailable",
  "board.column_empty",
  "board.drag_hint",
  "board.drag_moving",
];
const fallbacks = new Map();
for (const [, source] of sources) {
  for (const match of source.matchAll(
    /\bcopy\(\s*([A-Za-z_$][\w$]*)\s*,\s*"([^"]+)"\s*,\s*"((?:[^"\\]|\\.)*)"\s*,?\s*\)/g,
  )) {
    fallbacks.set(match[2], match[3]);
  }
}
for (const key of BOARD_KEYS) {
  assert.ok(backendCopy.has(key), `service.go does not serve ${key} for leadership-tasks (P4)`);
  assert.ok(key in fixture.copy, `fixture contract is missing ${key}`);
  assert.equal(fixture.copy[key], backendCopy.get(key), `fixture and service.go disagree on ${key}`);
  assert.ok(fallbacks.has(key), `expected a literal 3-arg copy() fallback for ${key} in the feature source`);
  assert.equal(
    fallbacks.get(key),
    backendCopy.get(key),
    `the frontend fallback for ${key} differs from what service.go serves -- reword both together`,
  );
}
