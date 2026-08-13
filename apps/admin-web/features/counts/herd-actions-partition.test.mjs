import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const actionSource = readFileSync(new URL("./herd-actions.ts", import.meta.url), "utf8");
const uiSource = readFileSync(new URL("./herd-actions-ui.tsx", import.meta.url), "utf8");
const adminServiceSource = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("Admin single-create submits exact shed id without partition label", () => {
  assert.doesNotMatch(uiSource, /name="partition_label"/);
  assert.doesNotMatch(actionSource, /optionalString\(formData, "partition_label"\)/);
  assert.doesNotMatch(actionSource, /partition_label: partitionLabel/);
});

test("Admin bulk template includes the partition column", () => {
  assert.match(adminServiceSource, /option\("partition_label", "Partition"/);
  assert.doesNotMatch(uiSource, /partitionColumn/);
  assert.match(uiSource, /const header = bulkColumns\.join\(","\)/);
});
