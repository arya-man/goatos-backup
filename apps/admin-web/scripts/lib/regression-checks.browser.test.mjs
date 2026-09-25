// The text-overlap rule, run in a real browser against four tiny pages: two it must flag and two
// it must not. Skips when no browser can be launched (the rule is then only checked statically by
// regression-checks.test.mjs).
import test from "node:test";
import assert from "node:assert/strict";
import { collectRegressionFindings } from "./regression-checks.mjs";

let chromium = null;
try {
  ({ chromium } = await import("playwright"));
} catch {
  chromium = null;
}

async function launch() {
  if (!chromium) return null;
  return chromium.launch({ channel: "chrome" }).catch(() => chromium.launch().catch(() => null));
}

const PAGE = (body) => `<!doctype html><html><body style="margin:0;font:14px sans-serif;background:#111;color:#eee">${body}</body></html>`;

const FIXTURES = [
  {
    name: "an ellipsised card line beside another card is NOT drawn over it",
    flag: false,
    html: `<div style="display:flex;gap:12px;padding:10px">
      <div style="width:160px;overflow:hidden"><span style="display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">Next: Return the animal to its register</span></div>
      <div style="width:160px"><span>59 pens</span></div></div>`,
  },
  {
    name: "text genuinely painted over other text IS flagged",
    flag: true,
    html: `<div style="position:relative;height:40px;padding:10px">
      <span style="position:absolute;left:10px;top:10px;white-space:nowrap">Overlapping words here</span>
      <span style="position:absolute;left:100px;top:12px;white-space:nowrap">Another sentence on top</span></div>`,
  },
  {
    name: "an open opaque menu covering page text is NOT drawn over it",
    flag: false,
    html: `<div style="position:relative;padding:10px">
      <p style="margin:0">Column is automatic, at every level: step, then subtask, then card.</p>
      <div role="menu" style="position:absolute;left:10px;top:4px;background:#222;padding:6px;width:110px"><div>Feed</div><div>Vaccination</div></div></div>`,
  },
  {
    name: "a see-through overlay whose text sits over page text IS flagged",
    flag: true,
    html: `<div style="position:relative;padding:10px">
      <p style="margin:0">Column is automatic, at every level: step, then subtask, then card.</p>
      <div style="position:absolute;left:10px;top:12px;width:max-content;white-space:nowrap"><div>Feed Vaccination Herd</div></div></div>`,
  },
];

test("text-overlap flags painted overlaps and ignores clipped or covered text", async (t) => {
  const browser = await launch();
  if (!browser) {
    t.skip("no browser available");
    return;
  }
  try {
    const page = await browser.newPage({ viewport: { width: 800, height: 400 } });
    const wrong = [];
    for (const fixture of FIXTURES) {
      await page.setContent(PAGE(fixture.html));
      const findings = (await page.evaluate(collectRegressionFindings, { mobile: false })).filter((f) => f.pattern === "text-overlap");
      if ((findings.length > 0) !== fixture.flag) wrong.push(`${fixture.name}: ${JSON.stringify(findings)}`);
    }
    assert.deepEqual(wrong, []);
  } finally {
    await browser.close();
  }
});
