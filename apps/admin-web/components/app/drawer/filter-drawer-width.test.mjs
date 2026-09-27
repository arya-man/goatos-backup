import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// guard: filter-drawer-width (TR1-#37). A filters drawer is the template's 320px paper (MinimalDrawer
// default); the /counts/herd filters drawer was 360px. A *filter* file never widens its MinimalDrawer.
const features = join(fileURLToPath(new URL(".", import.meta.url)), "..", "..", "..", "features");

test("filter drawers keep the template 320px width", () => {
  const offenders = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (/filter.*\.tsx$/.test(name)) {
        const src = readFileSync(p, "utf8");
        if (/<MinimalDrawer[\s\S]*?width=\{(?!320\})/.test(src)) offenders.push(name);
      }
    }
  };
  walk(features);
  assert.deepEqual(offenders, []);
});
