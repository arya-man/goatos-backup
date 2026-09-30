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

// The Download weights drawer carries its own Sex select, and it STARTS ON THE PAGE'S Sex filter
// (maintainer request 2026-09-28, superseding the 2026-09-07 "starts on every kid"): a page on All
// opens the drawer on All, a page on Male opens it on Male, and the reader may still change it.
test("the export drawer owns a Sex select that starts on the page's sex", () => {
  assert.match(drawer, /const \[sex, setSex\] = useState\(initialSex\)/);
  assert.doesNotMatch(drawer, /const \[sex, setSex\] = useState\(""\)/);
  assert.match(drawer, /<TextField\s+select\s+id="wt-export-sex"\s+value=\{sex\}/);
  for (const key of ["export.sex.label", "export.sex.all"]) {
    assert.match(drawer, new RegExp(`copy\\(pageContract, "${key}"\\)`), key);
  }
  // The genders are the farm's own list from Configuration (audit 2026-09-26), not male and female
  // typed into the drawer.
  assert.match(drawer, /weightsSexChoices\(pageContract\)\.map/);
  assert.doesNotMatch(drawer, /<option value="male">/);
  // The choice travels to the backend export exactly as the page filter does.
  assert.match(drawer, /sex: sex \|\| undefined/);
  assert.match(action, /sex: input\.sex/);
});

test("every host hands the drawer the page's resolved Sex filter", () => {
  for (const [file, source] of hosts) {
    const wiring = source.slice(source.indexOf("<WeightsExportControl"));
    const props = wiring.slice(0, wiring.indexOf("/>"));
    // sexFilter is the RESOLVED page value ("" = All), the same one the page's own reads use.
    assert.match(props, /initialSex=\{sexFilter\}/, `${file} must pass the page sex into the drawer`);
    // Origin and the weighing mode still travel from the page.
    assert.match(props, /origin=\{originFilter\}/, file);
  }
});

test("the drawer's Sex copy is backend-owned", () => {
  assert.match(contract, /"export\.sex\.label":\s*"Sex"/);
  assert.match(contract, /"export\.sex\.all":\s*"All"/);
  assert.match(contract, /"export\.hint":\s*"Pick the days, park, pens and sex to include\./);
});

// Every opening starts from the page's period and park AS THEY ARE NOW (maintainer request
// 2026-09-24): useState read them once, so after the reader changed the page's period the drawer
// still offered the old one. Sex is reset to the page's current Sex on each opening too.
test("each opening of the drawer takes the page's current period and park", () => {
  assert.match(drawer, /if \(open !== wasOpen\) \{/);
  assert.match(drawer, /setFrom\(initialFrom\);\s*setTo\(initialTo\);\s*setParkId\(initialParkId\);\s*setSex\(initialSex\);/);
});
