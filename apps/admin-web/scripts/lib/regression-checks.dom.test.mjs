// Real-DOM tests for the regression checks.
//
// regression-checks.test.mjs only greps the source, so a check could measure the wrong box and
// still pass it. These fixtures render the markup the app actually ships and run the collector
// in a browser, so every case below is pinned in BOTH directions: the shape that must stay
// quiet, and the genuine bug of the same family that must still report.
//
// The four quiet cases are production false positives from the 2026-09-22 sweep
// (weighing / vaccination / feed judging pass).
import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";
import { collectRegressionFindings, findingSentence } from "./regression-checks.mjs";

let browser;
let page;
test.before(async () => {
  browser = await chromium.launch({ headless: true });
  page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
});
test.after(async () => {
  await browser?.close();
});

async function patternsFor(html) {
  await page.setContent(`<!doctype html><html><body style="margin:0;font-family:system-ui"><main>${html}</main></body></html>`);
  const found = await page.evaluate(collectRegressionFindings, { mobile: false });
  return { patterns: found.map((f) => f.pattern), found };
}

// ---------------------------------------------------------------- A-chart-empty-frame
// The chart components mark their inline SVG aria-hidden — the figures are carried by the
// wrapper's role="img"/aria-label. Treating aria-hidden as "not painted" reported every one of
// those charts as an empty frame while the bars were on screen.
const CHART_WITH_BARS = `
  <section class="card wchart" style="width:600px">
    <h2>Feed mix</h2><p>Share of the fed kg over the window</p>
    <div class="svgbars">
      <svg viewBox="0 0 1100 64" width="100%" aria-hidden="true">
        <rect x="120" y="4" width="800" height="20" fill="#7ac143"></rect>
        <rect x="120" y="34" width="500" height="20" fill="#4a9eda"></rect>
      </svg>
    </div>
  </section>`;
const CHART_WITH_NO_MARKS = `
  <section class="card wchart" style="width:600px;height:120px">
    <h2>Feed mix</h2><p>Share of the fed kg over the window</p>
    <div class="svgbars"><svg viewBox="0 0 1100 64" width="100%" aria-hidden="true"></svg></div>
  </section>`;

test("an aria-hidden chart whose bars are painted is not an empty frame", async () => {
  const { patterns } = await patternsFor(CHART_WITH_BARS);
  assert.ok(!patterns.includes("A-chart-empty-frame"), `expected quiet, got ${patterns.join(", ")}`);
});

test("a chart that really draws nothing is still reported as an empty frame", async () => {
  const { patterns } = await patternsFor(CHART_WITH_NO_MARKS);
  assert.ok(patterns.includes("A-chart-empty-frame"));
});

test("a chart hidden by the responsive scale toggle is not counted as painted", async () => {
  const { patterns } = await patternsFor(`
    <section class="card wchart" style="width:600px;height:120px">
      <h2>Feed mix</h2><p>Share</p>
      <div class="svgbars" style="display:none">
        <svg viewBox="0 0 1100 64" width="100%" aria-hidden="true"><rect x="0" y="0" width="800" height="20"></rect></svg>
      </div>
    </section>`);
  // display:none still hides it, so the card has no visible marks and must report.
  assert.ok(patterns.includes("A-chart-empty-frame"));
});

// ---------------------------------------------------------------- text-overlap
// A Range reports where text WOULD run, ignoring the ancestor that clips it, so an ellipsised
// label reported its full untruncated width and "overlapped" the icon after the ellipsis.
const ELLIPSIS_LABEL_BESIDE_ICON = `
  <div class="wgrouped" style="width:420px">
    <ul class="wbars"><li class="wbar" style="display:grid;grid-template-columns:120px 1fr 60px;gap:8px">
      <span class="wbl" style="display:flex;flex-wrap:nowrap;align-items:center;gap:6px;overflow:visible;white-space:nowrap">
        <span class="wbl-text" style="flex:0 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis">Non-elevated pen</span>
        <span class="wgl-hint"><span class="wgl-i" style="display:inline-flex;width:14px;height:14px;flex:0 0 auto">i</span></span>
      </span>
      <span class="wbt"><i style="display:block;height:18px;width:60%;background:#7ac143"></i></span>
      <span class="wbv">167 g</span>
    </li></ul>
  </div>`;
const TEXT_PAINTED_OVER_TEXT = `
  <div style="position:relative;width:420px;font-size:14px;height:40px">
    <span style="position:absolute;left:10px;top:12px;white-space:nowrap">331 animals</span>
    <span style="position:absolute;left:66px;top:12px;white-space:nowrap">Coimbatore Castro 1</span>
  </div>`;

test("an ellipsised label does not overlap the icon that sits after the ellipsis", async () => {
  const { patterns } = await patternsFor(ELLIPSIS_LABEL_BESIDE_ICON);
  assert.ok(!patterns.includes("text-overlap"), `expected quiet, got ${patterns.join(", ")}`);
});

test("text genuinely painted over other text is still reported", async () => {
  const { patterns } = await patternsFor(TEXT_PAINTED_OVER_TEXT);
  assert.ok(patterns.includes("text-overlap"));
});

test("an overlap that exists only in the clipped-away part of the text is not reported", async () => {
  const { patterns } = await patternsFor(`
    <div style="position:relative;width:420px;font-size:14px;height:40px">
      <span style="position:absolute;left:10px;top:12px;width:56px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">331 animals and more words</span>
      <span style="position:absolute;left:66px;top:12px;white-space:nowrap">Coimbatore Castro 1</span>
    </div>`);
  assert.ok(!patterns.includes("text-overlap"), `expected quiet, got ${patterns.join(", ")}`);
});

// ---------------------------------------------------------------- C-cell-mid-word-wrap
// Wrapping at a hyphen is ordinary typography; the bug family is a word the browser had to
// break with nowhere legal to break it.
test("a table cell wrapped at a hyphen is not a mid-word break", async () => {
  const { patterns } = await patternsFor(`<table><tr><td style="width:70px">Shed-average plan 2026-09-07</td></tr></table>`);
  assert.ok(!patterns.includes("C-cell-mid-word-wrap"), `expected quiet, got ${patterns.join(", ")}`);
});

test("a word broken with nowhere legal to break is still reported", async () => {
  const { patterns } = await patternsFor(`<table><tr><td style="width:36px;word-break:break-all">Warmup</td></tr></table>`);
  assert.ok(patterns.includes("C-cell-mid-word-wrap"));
});

test("an ISO date in a table is still reported even when the cell wraps at its hyphens", async () => {
  const { patterns } = await patternsFor(`<table><tr><td style="width:70px">Shed-average plan 2026-09-07</td></tr></table>`);
  assert.ok(patterns.includes("J-raw-text"));
});

// ---------------------------------------------------------------- A-chart-label-clipped
// Every bar label in the app is a two-line clamp (-webkit-box + -webkit-line-clamp), which paints
// its own "..." where it cuts. On a wrapping box Chromium reports scrollWidth a pixel or two above
// clientWidth even though each line sits inside the box, so measuring scrollWidth accused labels
// that were fully on screen, ending in a tidy ellipsis: "2px wide text hidden", on Load wise and
// Counts Breakdown, for three of nine identical labels.
const CLAMPED_BAR_LABEL = `
  <div class="gcols" style="width:400px">
    <div class="gcb"><i class="gcbar" style="display:block;height:40px;width:20px;background:#7ac143"></i>
      <span class="gcval">129</span>
      <span class="gclab" style="font-size:11px;line-height:1.2;text-align:center;white-space:normal;overflow-wrap:anywhere;max-width:86px;overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical">129 (CPT Godel 2 - Part 1, CPT Godel 2 - Part 2)</span>
    </div>
  </div>`;
// The same family's genuine bug: one line, clipped hard, with nothing to say there is more.
const HARD_CLIPPED_BAR_LABEL = `
  <div class="gcols" style="width:400px">
    <div class="gcb"><i class="gcbar" style="display:block;height:40px;width:20px;background:#7ac143"></i>
      <span class="gcval">129</span>
      <span class="gclab" style="font-size:11px;display:block;width:86px;white-space:nowrap;overflow:hidden;text-overflow:clip">129 (CPT Godel 2 - Part 1, CPT Godel 2 - Part 2)</span>
    </div>
  </div>`;

test("a two-line clamped bar label that ends in its own ellipsis is not reported as cut off", async () => {
  const { patterns } = await patternsFor(CLAMPED_BAR_LABEL);
  assert.ok(!patterns.includes("A-chart-label-clipped"), `expected quiet, got ${patterns.join(", ")}`);
  assert.ok(!patterns.includes("text-cut-off"), `expected quiet, got ${patterns.join(", ")}`);
});

test("a bar label clipped flat with no ellipsis is still reported as cut off", async () => {
  const { patterns } = await patternsFor(HARD_CLIPPED_BAR_LABEL);
  assert.ok(patterns.includes("A-chart-label-clipped"), `expected a finding, got ${patterns.join(", ")}`);
});

// ---------------------------------------------------------------- text-overlap under a panel
// The SOP basics row squeezes three bordered panels onto a phone. The middle panel's chip runs
// out of it and slides UNDER the next panel, whose opaque background paints over it: the reader
// sees a cut-off chip next to a clean "Kind", never two words printed on each other.
const CHIP_BURIED_UNDER_NEXT_PANEL = `
  <div style="position:relative;width:360px;height:44px;font-size:13px;background:#0d120d">
    <div style="position:absolute;left:8px;top:6px;width:100px;height:32px;border:1px solid #333"></div>
    <span class="tag t-pur" style="position:absolute;left:16px;top:12px;white-space:nowrap;color:#b79cff">Counts / Herd Operations</span>
    <div style="position:absolute;left:112px;top:6px;z-index:1;width:120px;height:32px;border:1px solid #333;background:#0d120d">
      <span class="muted small" style="position:absolute;left:6px;top:6px">Kind</span>
    </div>
  </div>`;

test("a chip that runs under the next panel is not reported as text printed on text", async () => {
  const { patterns } = await patternsFor(CHIP_BURIED_UNDER_NEXT_PANEL);
  assert.ok(!patterns.includes("text-overlap"), `expected quiet, got ${patterns.join(", ")}`);
});

test("two texts printed on each other with nothing opaque between them still report", async () => {
  const { patterns } = await patternsFor(TEXT_PAINTED_OVER_TEXT);
  assert.ok(patterns.includes("text-overlap"));
});

// ---------------------------------------------------------------- J-raw-text
// An SOP's own code, set in the monospace face the product uses for identifiers, is the value a
// person quotes back to us — the same promise <code> makes. A breeding state printed in the UI
// face is a leak, and stays one.
test("an identifier set in the monospace face is not reported as a leaked code", async () => {
  const { patterns } = await patternsFor(
    `<div class="muted small">SOP code <span class="mono" style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace">counts.herd_operation</span></div>`,
  );
  assert.ok(!patterns.includes("J-raw-text"), `expected quiet, got ${patterns.join(", ")}`);
});

test("the same code in the ordinary interface face is still reported", async () => {
  const { patterns } = await patternsFor(`<div class="v">non_pregnant</div>`);
  assert.ok(patterns.includes("J-raw-text"));
});

test("a raw code in a closed dropdown is reported against the dropdown a person can see", async () => {
  const { found } = await patternsFor(
    `<label>Evidence type <select aria-label="Evidence type"><option>Choose</option><option>source_record</option></select></label>`,
  );
  const leak = found.find((f) => f.pattern === "J-raw-text");
  assert.ok(leak, "a code listed in a dropdown is still a leak");
  assert.match(leak.element, /^select/, `the outlined element must be the dropdown, not the invisible option: ${leak.element}`);
  assert.match(leak.detail, /Evidence type/, `the sentence must say where it is: ${leak.detail}`);
  const marked = await page.evaluate(() => document.querySelector("[data-smoke-issue]")?.tagName ?? null);
  assert.equal(marked, "SELECT", "the red box must land on something on screen");
});

// ---------------------------------------------------------------- C-cell-mid-word-wrap
// Sales Config on a phone squeezes the Load column to about one character: "136 - 16/09/2026"
// comes out stacked as 1 / 3 / 6 and the farm column as C / B / E.
test("a load number stacked one digit per line is reported", async () => {
  const { patterns } = await patternsFor(`
    <table><tr><td style="white-space:nowrap">
      <a class="celllink" style="display:flex;width:12px;white-space:normal;overflow-wrap:anywhere;word-break:break-word"><b>136</b><span class="muted small"> &middot; 16/09/2026</span></a>
    </td></tr></table>`);
  assert.ok(patterns.includes("C-cell-mid-word-wrap"), `expected a finding, got ${patterns.join(", ")}`);
});

test("a temporary animal id wrapped at its hyphens is not a broken word", async () => {
  const { patterns } = await patternsFor(
    `<table><tr><td style="width:120px;overflow-wrap:anywhere">TEMP-CBE-CASTRO1-003<div class="muted">G-003284</div></td></tr></table>`,
  );
  assert.ok(!patterns.includes("C-cell-mid-word-wrap"), `expected quiet, got ${patterns.join(", ")}`);
});

// ---------------------------------------------------------------- evidence + wording
test("the element behind the headline sentence is the one the screenshot centres on", async () => {
  await page.setContent(`<!doctype html><html><body><main>${CHART_WITH_NO_MARKS}${TEXT_PAINTED_OVER_TEXT}</main></body></html>`);
  const found = await page.evaluate(collectRegressionFindings, { mobile: false });
  const firstMarked = await page.evaluate(() => {
    const el = document.querySelector("[data-smoke-issue-first]");
    return el ? el.getAttribute("data-smoke-issue") : null;
  });
  assert.equal(found.length > 0, true);
  assert.equal(firstMarked, found[0].pattern, "the marked element must be the one findings[0] names");
  assert.equal(await page.locator("[data-smoke-issue-first]").count(), 1, "exactly one element carries the headline mark");
});

test("a chart label reads as the text on screen, not as its runs concatenated", async () => {
  const { found } = await patternsFor(`
    <div class="wbars" style="width:300px">
      <div class="wbar" style="display:grid;grid-template-columns:18px 1fr;gap:4px">
        <span class="wbl" style="overflow:hidden"><span class="wbl-text">2026-08-10</span><span class="tag">331 animals</span></span>
        <span class="wbt"><i style="display:block;height:10px;width:50%;background:#7ac143"></i></span>
      </div>
    </div>`);
  const narrow = found.find((f) => f.pattern === "A-chart-label-column-narrow");
  assert.ok(narrow, "the crushed label column must still report");
  assert.ok(!/2026-08-10331/.test(narrow.text), `runs must not be glued together: ${narrow.text}`);
  assert.match(narrow.text, /2026-08-10 331 animals/);
});

test("every sentence names what a person sees and carries no code, tag or measurement", () => {
  for (const pattern of Object.keys(
    // one of each family, with the fields the collector fills in
    {
      "text-overlap": 1, "text-cut-off": 1, "A-chart-label-collapsed": 1, "A-chart-label-clipped": 1,
      "A-chart-label-overlap": 1, "A-chart-label-column-narrow": 1, "A-chart-labels-truncated": 1,
      "A-chart-value-missing": 1, "A-chart-empty-frame": 1, "A-svg-text-clipped": 1,
      "A-svg-text-overlap": 1, "A-svg-text-tiny": 1, "B-container-overflow": 1, "C-cell-mid-word-wrap": 1,
      "C-cell-overpaint": 1, "chip-crushed": 1, "J-raw-text": 1, "D-page-overflow": 1,
    },
  )) {
    const s = findingSentence({ pattern, text: "Weekly growth", peer: "331 animals" });
    assert.ok(s.length > 10, `${pattern}: too short`);
    assert.ok(!s.includes(pattern), `${pattern}: leaks its own code`);
    assert.ok(!/[.#]\w+-\w+|span\.|\bdiv\b|\btd\b|\bsvg\b|getBoundingClientRect|scrollWidth|clientWidth/.test(s), `${pattern}: leaks a selector/tag/property: ${s}`);
    assert.ok(!/\d+px|\(<\d+|\d+\/\d+ labels/.test(s), `${pattern}: leaks a measurement: ${s}`);
  }
});
