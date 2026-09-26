import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import plugin, { probePageRhythm } from "./page-rhythm.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..", "..");

test("rhythm plugin is a P0 r2 audit check", () => {
  assert.equal(plugin.name, "rhythm");
  assert.equal(plugin.p0, true);
  assert.equal(typeof plugin.run, "function");
  assert.equal(typeof probePageRhythm, "function");
});

// Static twin of the header-gap probe: a page component that renders the PageHeader must not
// return it inside a bare fragment (the page gap comes from the `screen on` grid root).
test("pages rendering PageHeader do not return a bare fragment root", () => {
  const offenders = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, name.name);
      if (name.isDirectory()) walk(p);
      else if (name.name.endsWith(".tsx")) {
        const src = readFileSync(p, "utf8");
        if (!/<PageHeader\b/.test(src)) continue;
        // `return (\n    <>\n      <PageHeader` : the header is the first child of a fragment.
        if (/return \(\s*<>\s*<PageHeader\b/.test(src)) offenders.push(p.slice(root.length + 1));
      }
    }
  };
  walk(join(root, "features"));
  assert.deepEqual(offenders, []);
});

// Static twin of raw-code-label: a workflow chain node's state is a backend code.
test("workflow chain node state is humanized", () => {
  const src = readFileSync(join(root, "features/process-integrity/workflow-drilldown.tsx"), "utf8");
  assert.doesNotMatch(src, />\{node\.state\}</);
});

// guard: phone-fab-clearance — at phone width Ask Mesha is a floating bubble over the page; the
// shell content keeps bottom room for it so the last row/pager scrolls clear (FJ1-P1-2).
test("shell content keeps phone bottom clearance for the Ask Mesha bubble", () => {
  const src = readFileSync(join(root, "components/mesha-shell.tsx"), "utf8");
  assert.match(src, /@media \(max-width:620px\)": \{ "--layout-dashboard-content-pb"/);
  const panel = readFileSync(join(root, "features/ceo-ai/ceo-ai-panel.tsx"), "utf8");
  assert.match(panel, /matchMedia\("\(max-width:620px\)"\)/, "bubble breakpoint and clearance breakpoint must match");
});

// guard: no-blur-scrim — a blurred full-surface layer (backdrop-filter on a scrim / loading veil)
// flickers in the Android WebView (OCI U1-U8, FJ1-P1-4). Legacy CSS blur declarations only shrink.
test("legacy CSS backdrop blur declarations only shrink", () => {
  const css = ["app/mesha-theme.css", "app/frame.css", "app/minimal-theme.css"].map((f) => readFileSync(join(root, f), "utf8")).join("\n");
  const blurs = (css.match(/\{[^{}]*backdrop-filter:\s*[^;{}]*blur\(/g) || []).length;
  assert.ok(blurs <= 2, `backdrop blur declarations in legacy CSS: ${blurs} (max 2, shrink only)`);
  assert.doesNotMatch(css, /\.vr-results-loading\{backdrop-filter/);
});
