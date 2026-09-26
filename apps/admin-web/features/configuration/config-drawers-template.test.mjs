import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Configuration drawers (salvage/ffix-c, 2026-09-27): the sheet / workbook drawers showed the
// browser's bare "Choose file" input and a legacy `.btn` beside it, and the row drawer's fields sat
// in legacy `.fld` wrappers with a hand-typed "*" next to MUI's own required asterisk ("Name * *").
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("guard: config-drawer-upload-template -- file pickers are the template Upload area, actions kit Buttons", () => {
  for (const file of ["./sheet-drawer.tsx", "./workbook-drawer.tsx"]) {
    const src = read(file);
    assert.match(src, /import \{ UploadFile \} from "@\/components\/minimal\/upload";/, file);
    assert.doesNotMatch(src, /type="file"/, `${file} renders a bare file input`);
    assert.doesNotMatch(src, /className="btn sm b"[^>]*data-testid="(sheet|workbook)-upload"/, file);
  }
  const upload = read("../../components/minimal/upload/upload-file.tsx");
  assert.match(upload, /type="file"/);
  assert.match(upload, /data-testid=\{testId\}/);
});

test("guard: config-row-drawer-required -- the label is the column label; MUI adds the one asterisk", () => {
  const src = read("./row-drawer.tsx");
  assert.doesNotMatch(src, /column\.required \? " \*" : ""/);
  assert.doesNotMatch(src, /className="fld"/);
});
