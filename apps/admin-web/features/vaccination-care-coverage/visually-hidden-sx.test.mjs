import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

// guard: visually-hidden-sx (FIXJ11, J3B P2-1). In MUI sx `height: 1` is 100% and `margin: -1` a
// -8px spacing step, so a hand-rolled visually-hidden object is a full-cell absolute box: on
// /vaccination/care-coverage it hung 19px under the last row and the table scroller trapped the
// vertical swipe. Use `visuallyHidden` from @mui/utils (1px box). components/minimal is the template.
const root = new URL("../../", import.meta.url).pathname;
function* files(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name === "node_modules" || name === "minimal" || name.startsWith(".")) continue;
      yield* files(path);
    } else if (/\.tsx?$/.test(name)) yield path;
  }
}
test("visually-hidden-sx: no clip-rect hidden style with a bare 1 size in sx (use @mui/utils visuallyHidden)", () => {
  const offenders = [];
  for (const dir of ["features", "components", "app"]) {
    for (const f of files(join(root, dir))) {
      // A clip-rect object whose size is a bare number: in sx that is 100% / a spacing step. A
      // `React.CSSProperties` object (a style prop) or px strings are real 1px boxes.
      for (const m of readFileSync(f, "utf8").matchAll(/(\S[^\n]*)=\s*\{([^{}]*clip:\s*["']rect\(0[^{}]*)\}/g)) {
        if (/CSSProperties/.test(m[1])) continue;
        if (/\b(height|width):\s*1\b(?!\s*px)/.test(m[2])) offenders.push(f.slice(root.length));
      }
    }
  }
  assert.deepEqual(offenders, []);
});
