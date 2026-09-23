// Proves the LIVE half of the flicker lane end to end: a real browser, a real CDP
// screencast, real frames, the real detector.
//
// The detector's own tests feed it arrays. This one feeds it a browser, because the
// two ways frames arrive are not alike and the difference has already bitten once:
// a screencast produces a frame only when the compositor produces one, so a page
// sitting still produces NOTHING, and the settled frame before a flicker can be a
// second older than the flicker itself. Nothing but a browser catches that.
//
// The page below is synthetic on purpose. It is not a claim about Goat OS: it is the
// instrument check — a page that definitely flickers, so that "no flicker" from this
// harness means something.
import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { captureFlicker, captureOverlayPaint, writeFlickerEvidence } from "./flicker-capture.mjs";

/** A still page that paints one wrong frame about once a second. */
const FLICKERING_PAGE = `<!doctype html><meta name=viewport content="width=device-width">
<style>
  body{margin:0;background:#0f1411;color:#e8f0eb;font:14px system-ui}
  .bar{position:sticky;top:0;z-index:9;padding:12px;background:rgba(30,60,45,.85);backdrop-filter:blur(8px)}
  .row{padding:11px 12px;border-bottom:1px solid #223}
  #ghost{position:fixed;inset:0;pointer-events:none;opacity:0;
    background:repeating-linear-gradient(0deg,#7ad6a6 0 5px,#0f1411 5px 10px)}
</style>
<div class=bar>Filters</div><div id=list></div><div id=ghost></div>
<script>
  const list = document.getElementById("list");
  for (let i = 0; i < 120; i += 1) {
    const row = document.createElement("div");
    row.className = "row";
    row.textContent = "Task " + i + " - something for a person to read";
    list.appendChild(row);
  }
  // One wrong frame, then straight back, about once a second: the shape the phone
  // recording shows. Nothing else on the page moves.
  const ghost = document.getElementById("ghost");
  let n = 0;
  (function tick() { n += 1; ghost.style.opacity = n % 45 === 0 ? "1" : "0"; requestAnimationFrame(tick); })();
</script>`;

/** A still page that does nothing at all. The negative control. */
const CALM_PAGE = FLICKERING_PAGE.replace('n % 45 === 0 ? "1" : "0"', '"0"');

async function phoneBrowser() {
  const { chromium } = await import("@playwright/test");
  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
  });
  return { browser, context };
}

// The browser binary is not present everywhere this test suite runs. A missing
// browser must read as "not run here", never as a pass.
let available = true;
try {
  const { chromium } = await import("@playwright/test");
  const probe = await chromium.launch();
  await probe.close();
} catch {
  available = false;
}

test("the live capture sees a page that flickers", { skip: available ? false : "no browser on this machine" }, async () => {
  const { browser, context } = await phoneBrowser();
  const dir = mkdtempSync(path.join(tmpdir(), "flicker-capture-"));
  try {
    const page = await context.newPage();
    await page.setContent(FLICKERING_PAGE);
    await page.waitForTimeout(300);
    const { frames, result } = await captureFlicker(page, {
      // Sit still and watch. Scrolling is exercised by the real journey; here the
      // page must be still, so that what is detected is the flicker and not motion.
      drive: async (p, mark) => { await mark("look at the page", () => p.waitForTimeout(5_000)); },
    });

    assert.ok(frames.length > 10, `the screencast must deliver frames, got ${frames.length}`);
    assert.equal(result.flicker, true, `the capture must see a page that definitely flickers: ${result.reason}`);
    assert.ok(result.events.length >= 3, `it recurs, got ${result.events.length}`);
    for (const event of result.events) {
      assert.ok(event.abruptness >= 0.4, "each one jumps rather than eases");
    }

    // And it produces something a person can look at, which is not a still.
    const evidence = await writeFlickerEvidence(frames, result, dir, "synthetic");
    assert.ok(evidence.gif || evidence.filmstrip || evidence.frames.length >= 3,
      "a flicker finding must carry moving evidence or a run of frames, never one still");
    if (evidence.gif) assert.match(evidence.gif, /\.gif$/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
    await context.close();
    await browser.close();
  }
});

test("the live capture does not invent flicker on a calm page", { skip: available ? false : "no browser on this machine" }, async () => {
  const { browser, context } = await phoneBrowser();
  try {
    const page = await context.newPage();
    await page.setContent(CALM_PAGE);
    await page.waitForTimeout(300);
    const { result } = await captureFlicker(page, {
      drive: async (p, mark) => { await mark("look at the page", () => p.waitForTimeout(3_000)); },
    });
    assert.equal(result.flicker, false, `a calm page must stay quiet: ${JSON.stringify(result.events)}`);
  } finally {
    await context.close();
    await browser.close();
  }
});

test("scrolling a calm page is not reported as flicker", { skip: available ? false : "no browser on this machine" }, async () => {
  const { browser, context } = await phoneBrowser();
  try {
    const page = await context.newPage();
    await page.setContent(CALM_PAGE);
    await page.waitForTimeout(300);
    const { result } = await captureFlicker(page, {
      drive: async (p, mark) => {
        await mark("scroll down the page", async () => {
          for (let i = 0; i < 8; i += 1) { await p.mouse.wheel(0, 240); await p.waitForTimeout(80); }
        });
        await mark("scroll back up", async () => {
          for (let i = 0; i < 8; i += 1) { await p.mouse.wheel(0, -240); await p.waitForTimeout(80); }
        });
      },
    });
    assert.equal(result.flicker, false, `scrolling is movement, not flicker: ${JSON.stringify(result.events)}`);
  } finally {
    await context.close();
    await browser.close();
  }
});

// ---------------------------------------------------------------------------
// The overlay-paint defect class: an opaque panel showing the page behind it
// ---------------------------------------------------------------------------
// This is the bug Ravi actually filmed, reduced to its essentials: a panel the
// stylesheet declares solid, over a half-transparent dimmer, over a page — and for a
// fifth of a second after a filter changes, the panel's own background is not painted
// and the page is legible through the dimmer.
//
// The negative controls matter as much as the positive one. A panel closing, a panel
// whose contents change, and a panel that is translucent by design must all stay
// silent, or the check is noise.
function overlayPage({ flashOnChange = true, closeOnChange = false, panelAlpha = 1 } = {}) {
  return `<!doctype html><meta name=viewport content="width=device-width">
<style>
  body{margin:0;background:#0f1411;color:#e8f0eb;font:14px system-ui}
  .row{padding:11px 12px;border-bottom:1px solid #223}
  .scrim{position:fixed;inset:0;z-index:150;background:rgba(6,12,9,.42)}
  .panel{position:fixed;left:0;right:0;bottom:0;height:520px;z-index:151;padding:14px;
    background:rgba(22,31,26,${panelAlpha})}
  .panel.ghost{background:transparent}
  .panel.shut{display:none}
  .opt{display:block;padding:12px 6px;border-bottom:1px solid #2a3a32}
</style>
<div id=list></div>
<div class=scrim></div>
<div class=panel id=panel><h3>Filters</h3>
  <label class=opt><input type=checkbox> All</label>
  <label class=opt><input type=checkbox> Dinakar</label>
  <label class=opt><input type=checkbox> Manju</label>
  <label class=opt><input type=checkbox> Manohark</label>
  <label class=opt><input type=checkbox> Aryaman</label>
</div>
<script>
  const list = document.getElementById("list");
  for (let i = 0; i < 120; i += 1) {
    const row = document.createElement("div");
    row.className = "row";
    row.textContent = "Task " + i + " - shift decking sheets on to the platform";
    list.appendChild(row);
  }
  const panel = document.getElementById("panel");
  panel.addEventListener("change", () => {
    if (${String(closeOnChange)}) { panel.classList.add("shut"); return; }
    if (!${String(flashOnChange)}) return;
    // The defect: the panel's own background stops being painted for ~0.2s.
    panel.classList.add("ghost");
    setTimeout(() => panel.classList.remove("ghost"), 200);
  });
</script>`;
}

async function runOverlay(html, options = {}) {
  const { browser, context } = await phoneBrowser();
  try {
    const page = await context.newPage();
    await page.setContent(html);
    await page.waitForTimeout(300);
    return await captureOverlayPaint(page, {
      // The panel is already open in this fixture, so there is nothing to open.
      open: async () => true,
      interact: async (p, mark) => {
        const boxes = p.locator(".panel input[type=checkbox]");
        const n = await boxes.count();
        for (let i = 1; i < Math.min(n, 5); i += 1) {
          await mark("change the filters", async () => {
            await boxes.nth(i).click({ timeout: 3_000 }).catch(() => {});
            await p.waitForTimeout(700);
          });
        }
        return 4;
      },
      ...options,
    });
  } finally {
    await context.close();
    await browser.close();
  }
}

test("finds a solid panel that shows the page through it when a filter changes",
  { skip: available ? false : "no browser on this machine" }, async () => {
    const result = await runOverlay(overlayPage());
    assert.ok(result.overlays.length > 0, "the solid panel must be found on the page");
    const panel = result.findings.find((f) => f.showedThrough);
    assert.ok(panel, `the defect must be found: ${JSON.stringify(result.findings.map((f) => f.reason))}`);
    assert.ok(panel.events.length >= 2, `once per change, got ${panel.events.length}`);
    for (const event of panel.events) {
      assert.ok(event.seconds <= 1.5, `a flash, not a state: ${event.seconds}s`);
      assert.ok(event.abruptness >= 0.4, "it appeared between one frame and the next");
      assert.ok(event.peak >= 0.02, `a real part of the panel changed, got ${event.peak}`);
    }
    assert.equal(panel.overlay.label, "Filters", "the finding must name the panel a person sees");
  });

test("a solid panel that stays solid is not reported",
  { skip: available ? false : "no browser on this machine" }, async () => {
    const result = await runOverlay(overlayPage({ flashOnChange: false }));
    assert.ok(!result.findings.some((f) => f.showedThrough),
      `a panel that behaves must stay quiet: ${JSON.stringify(result.findings.map((f) => f.events))}`);
  });

test("a panel that closes on a change is not a panel that flickered",
  { skip: available ? false : "no browser on this machine" }, async () => {
    const result = await runOverlay(overlayPage({ closeOnChange: true }));
    assert.ok(!result.findings.some((f) => f.showedThrough), "closing is what a panel is for");
  });

test("a panel that is see-through by design is never judged at all",
  { skip: available ? false : "no browser on this machine" }, async () => {
    // The whole basis of this check is that the browser says the element is opaque.
    // Something translucent must not even be collected, let alone reported.
    const result = await runOverlay(overlayPage({ panelAlpha: 0.6 }));
    assert.ok(!result.overlays.some((o) => o.label === "Filters"),
      "a translucent panel is not an opaque overlay and must not be picked up");
  });
