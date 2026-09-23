import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawer = readFileSync(new URL("./weights-export.tsx", import.meta.url), "utf8");
const action = readFileSync(new URL("./weights-export-action.ts", import.meta.url), "utf8");
const hosts = ["./weights.tsx", "./weights-analytics.tsx"].map((file) => [
  file,
  readFileSync(new URL(file, import.meta.url), "utf8"),
]);
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

// The Download weights drawer carries its OWN Sex select (maintainer, 2026-09-07). It starts on
// every kid and ignores the page's Sex filter: the file is a separate selection, and a reader
// filtered to Male on screen still expects the download to follow what they pick in the drawer.
test("the export drawer owns a Sex select that starts on every kid", () => {
  assert.match(drawer, /const \[sex, setSex\] = useState\(""\)/);
  assert.match(drawer, /<select id="wt-export-sex" value=\{sex\}/);
  for (const key of ["export.sex.label", "export.sex.all", "view.sex.male", "view.sex.female"]) {
    assert.match(drawer, new RegExp(`copy\\(pageContract, "${key}"\\)`), key);
  }
  // The choice travels to the backend export exactly as the page filter used to.
  assert.match(drawer, /sex: sex \|\| undefined/);
  assert.match(action, /sex: input\.sex/);
});

test("the drawer never inherits the page's Sex filter", () => {
  // No `sex` prop on the drawer, and no host passes one.
  assert.doesNotMatch(drawer, /^\s+sex\??: string;/m);
  assert.doesNotMatch(drawer, /^\s+sex,\s*$/m);
  for (const [file, source] of hosts) {
    const wiring = source.slice(source.indexOf("<WeightsExportControl"));
    const props = wiring.slice(0, wiring.indexOf("/>"));
    assert.doesNotMatch(props, /\bsex=/, `${file} passes the page sex into the drawer`);
    // Origin and the weighing mode still travel from the page.
    assert.match(props, /origin=\{originFilter\}/, file);
  }
});

test("the drawer's Sex copy is backend-owned", () => {
  assert.match(contract, /"export\.sex\.label":\s*"Sex"/);
  assert.match(contract, /"export\.sex\.all":\s*"All"/);
  assert.match(contract, /"export\.hint":\s*"Pick the days, park, pens and sex to include\./);
});
