// guard: mask-icon-not-img. The template course widget icons (lib/minimal-icons COURSE_WIDGET_ICONS)
// are MASK svgs: CourseWidgetSummary paints them through SvgColor with a tone gradient. Rendered as a
// plain <img> they come out black (/vaccination/plan KPI cards, 390 dark side-by-side).
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const IMG_MASK = /component="img"[^>]*src=\{[^}]*\b(icon|COURSE_WIDGET_ICONS)\b/;

test("course widget mask icons are never a plain img", () => {
  const root = new URL("../../", import.meta.url).pathname;
  const hits = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const abs = join(dir, name);
      if (statSync(abs).isDirectory()) walk(abs);
      else if (name.endsWith(".tsx")) {
        const text = readFileSync(abs, "utf8");
        if (text.includes("COURSE_WIDGET_ICONS") && IMG_MASK.test(text)) hits.push(abs.slice(root.length));
      }
    }
  };
  walk(join(root, "features"));
  assert.deepEqual(hits, []);
  assert.match('<Box component="img" alt="" src={kpi.icon} />', IMG_MASK, "self-test");
  const plan = readFileSync(new URL("./plan-console.tsx", import.meta.url), "utf8");
  assert.match(plan, /<SvgColor src=\{kpi\.icon\}/);
});
