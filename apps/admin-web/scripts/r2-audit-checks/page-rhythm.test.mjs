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

// guard: no-blur-scrim — a blurred full-surface layer (backdrop-filter on a scrim / loading veil)
// flickers in the Android WebView (OCI U1-U8, FJ1-P1-4). Legacy CSS blur declarations only shrink.
test("legacy CSS backdrop blur declarations only shrink", () => {
  const css = ["app/mesha-theme.css", "app/frame.css", "app/minimal-theme.css"].map((f) => readFileSync(join(root, f), "utf8")).join("\n");
  const blurs = (css.match(/\{[^{}]*backdrop-filter:\s*[^;{}]*blur\(/g) || []).length;
  assert.ok(blurs <= 2, `backdrop blur declarations in legacy CSS: ${blurs} (max 2, shrink only)`);
  assert.doesNotMatch(css, /\.vr-results-loading\{backdrop-filter/);
});

// guard: header-gap-wrapped (R3OPS-3). The probe also measures a PageHeader wrapped alone in a div
// inside the page column: the first block is the wrapper's next sibling. A 0px gap fails, 24px passes.
test("header-gap probe sees through a header wrapper", async () => {
  const { chromium } = await import("playwright");
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
    const doc = (gap) => `<div class="screen on"><div style="display:flex;flex-direction:column;gap:${gap}px">
      <div><header data-page-header style="height:60px">Title</header></div>
      <div style="height:100px">KPI row</div></div></div>`;
    await page.setContent(doc(0));
    const bad = await page.evaluate(probePageRhythm);
    assert.equal(bad.filter((f) => f.kind === "header-gap").length, 1, JSON.stringify(bad));
    await page.setContent(doc(24));
    assert.deepEqual((await page.evaluate(probePageRhythm)).filter((f) => f.kind === "header-gap"), []);
  } finally {
    await browser.close();
  }
});

// Static twins for the two wrapped-header layouts the stricter probe found (R3OPS-3).
test("/tasks and the SOP builder keep the page gap under the header", () => {
  const tasks = readFileSync(join(root, "features/leadership-tasks/leadership-tasks-page.tsx"), "utf8");
  assert.match(tasks, /<Box sx=\{\{ minWidth: 0, "& > \[data-page-header\]": \{ mb: 3 \} \}\}>/);
  const builder = readFileSync(join(root, "features/sops/sop-builder.tsx"), "utf8");
  // The builder (and every SOP editor) roots on EditorPage, which is the shared PageRoot grid (24px gap).
  assert.match(builder, /return \(\s*<EditorPage>\s*<EditorHeader/);
  const parts = readFileSync(join(root, "features/sops/editor-parts.tsx"), "utf8");
  assert.match(parts, /export function EditorPage\([^)]*\) \{\s*return \(\s*<PageRoot /);
  assert.doesNotMatch(builder, /marginBottom: 12/, "notices take the column gap, not their own margin");
});
