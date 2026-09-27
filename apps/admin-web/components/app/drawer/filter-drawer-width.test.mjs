import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// guard: filter-drawer-width (TR1-#37). A filters drawer is the template's 320px paper (MinimalDrawer
// default); the /counts/herd filters drawer was 360px. A *filter* file never widens its MinimalDrawer.
const features = join(fileURLToPath(new URL(".", import.meta.url)), "..", "..", "..", "features");

// Only the MinimalDrawer's own props count: from `<MinimalDrawer` to the line that closes its opening
// tag (a bare `>` or `/>`). Children such as an Iconify width={18} are not the drawer's width.
function drawerProps(src) {
  const out = [];
  const re = /<MinimalDrawer\b/g;
  let m;
  while ((m = re.exec(src))) {
    const rest = src.slice(m.index);
    const end = rest.search(/\n\s*\/?>\s*(\n|$)/);
    out.push(end === -1 ? rest : rest.slice(0, end));
  }
  return out;
}

test("drawerProps reads only the opening tag", () => {
  const ok = "<MinimalDrawer\n  width={320}\n>\n  <Iconify width={18} />\n</MinimalDrawer>";
  const bad = "<MinimalDrawer\n  width={360}\n>\n</MinimalDrawer>";
  assert.equal(drawerProps(ok).some((p) => /\bwidth=\{(?!320\})/.test(p)), false);
  assert.equal(drawerProps(bad).some((p) => /\bwidth=\{(?!320\})/.test(p)), true);
});

test("filter drawers keep the template 320px width", () => {
  const offenders = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (/filter.*\.tsx$/.test(name)) {
        const src = readFileSync(p, "utf8");
        if (drawerProps(src).some((props) => /\bwidth=\{(?!320\})/.test(props))) offenders.push(name);
      }
    }
  };
  walk(features);
  assert.deepEqual(offenders, []);
});
