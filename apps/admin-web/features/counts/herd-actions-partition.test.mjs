import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const actionSource = readFileSync(new URL("./herd-actions.ts", import.meta.url), "utf8");
const uiSource = readFileSync(new URL("./herd-actions-ui.tsx", import.meta.url), "utf8");

test("Admin single-create submits the operator partition label", () => {
  assert.match(uiSource, /name="partition_label"/);
  assert.match(actionSource, /optionalString\(formData, "partition_label"\)/);
  assert.match(actionSource, /partition_label: partitionLabel/);
});

test("Admin bulk template includes the partition column", () => {
  assert.match(uiSource, /const partitionColumn = copy\(pageContract, "field\.partition_label", "Partition"\)/);
  assert.match(uiSource, /\[\.\.\.configuredBulkColumns, partitionColumn\]/);
  assert.match(uiSource, /const header = bulkColumns\.join\(","\)/);
});
