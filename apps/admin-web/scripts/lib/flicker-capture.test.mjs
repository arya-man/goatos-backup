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
import { captureFlicker, writeFlickerEvidence } from "./flicker-capture.mjs";

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
    const evidence = writeFlickerEvidence(frames, result, dir, "synthetic");
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
