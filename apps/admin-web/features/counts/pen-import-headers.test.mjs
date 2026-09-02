import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const actionSource = readFileSync(new URL("./herd-actions.ts", import.meta.url), "utf8");
const adminServiceSource = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

// The dashboard calls the operational location a PEN (maintainer decision 2026-09-02). The
// pen-register import template's header row is built from the option LABELS, and the parser
// matches by header NAME -- so renaming a label is a parser change, and forgetting the parser
// half would leave the downloaded template unimportable by the screen that produced it.
test("the pen-register template's own headers are the ones the parser accepts", () => {
  assert.match(adminServiceSource, /option\("shed_code", "Pen code"/);
  assert.match(adminServiceSource, /option\("shed_name", "Pen name"/);
  assert.match(actionSource, /case "pen":\s*\n\s*case "pen_name":/);
  assert.match(actionSource, /case "pen_code":/);
});

// A sheet saved before the rename still imports. This is the half that must never regress:
// the old labels are aliases, not replaced names.
test("the old shed headers still import", () => {
  assert.match(actionSource, /case "shed":\s*\n\s*case "shed_name":\s*\n\s*case "name":\s*\n\s*return "shed_name";/);
  assert.match(actionSource, /case "shed_code":\s*\n\s*case "location_code":\s*\n\s*return "shed_code";/);
});

// The herd (animal) template is a DIFFERENT importer with a different collision: "pen" already
// means PARTITION there, so its location column is labelled "Pen name" rather than "Pen". Naming
// it "Pen" would file a pen name into partition_label on every row, silently and with no error.
test("the herd template avoids the pen/partition header collision", () => {
  assert.match(adminServiceSource, /option\("shed", "Pen name"/);
  assert.match(adminServiceSource, /option\("partition_label", "Partition"/);
});
