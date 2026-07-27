import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));

function source(name) {
  return readFileSync(join(here, name), "utf8");
}

test("weighing UI keeps persona capabilities explicit", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /canCreate: leadership/);
  assert.match(data, /canEdit: leadership/);
  assert.match(data, /canPublish: leadership/);
  assert.match(data, /canExecute: operator/);
  assert.match(data, /reviewOnly: role === "director"/);
  assert.match(page, /Monitor \/ review only/);
  assert.match(page, /cannot create, publish, edit, or execute/);
});

test("weighing UI renders category-aware progress and blocks lumpsum individual truth", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /individual_animal/);
  assert.match(data, /per_shed_partition/);
  assert.match(page, /RFID \+ animal identity \+ weight \+ mandatory per-animal video/);
  assert.match(page, /Selected-scope result \+ scope proof; no individual weight update/);
  assert.match(page, /categoryLabel\[row\.category\]/);
});

test("weighing UI uses product labels instead of backend enum or uuid presentation", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(page, /In progress/);
  assert.match(page, /Needs review/);
  assert.match(page, /Shed total/);
  assert.doesNotMatch(page, /replaceAll\("_", " "\)/);
  assert.match(data, /operatorName: "Assigned operator"/);
  assert.match(data, /parkName: "Selected park"/);
});

test("weighing UI exposes wrong-shed expected-original and actual-current context", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /expectedShed/);
  assert.match(data, /originalPartition/);
  assert.match(data, /actualShed/);
  assert.match(data, /currentPartition/);
  assert.match(page, /Expected \/ original/);
  assert.match(page, /Actual \/ current/);
});
