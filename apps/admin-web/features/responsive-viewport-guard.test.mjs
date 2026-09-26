import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import test from "node:test";

const require = createRequire(new URL("../package.json", import.meta.url));
const { chromium } = require("playwright");

const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

function pageHtml(body) {
  return `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <style>${css}</style>
  <style>
    body{margin:0;background:var(--bg);color:var(--ink);font-family:Inter,system-ui,sans-serif}
    .proof-main{padding:10px}
    .proof-static-fs{position:static!important;display:block!important;inset:auto!important;min-height:auto!important;margin-top:12px!important}
  </style>
</head>
<body>${body}</body>
</html>`;
}

async function withPage(viewport, run) {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport });
    await run(page);
  } finally {
    await browser.close();
  }
}

async function assertViewportFit(page, label, selectors) {
  const metrics = await page.evaluate((checkedSelectors) => {
    const viewportWidth = window.innerWidth;
    const boxes = checkedSelectors.flatMap((selector) =>
      Array.from(document.querySelectorAll(selector)).map((element) => {
        const rect = element.getBoundingClientRect();
        return {
          selector,
          left: Math.floor(rect.left),
          right: Math.ceil(rect.right),
          width: Math.ceil(rect.width),
        };
      }),
    );
    return {
      viewportWidth,
      documentWidth: document.documentElement.scrollWidth,
      bodyWidth: document.body.scrollWidth,
      boxes,
    };
  }, selectors);

  assert.equal(metrics.documentWidth, metrics.viewportWidth, `${label}: document should not horizontally scroll`);
  assert.ok(metrics.bodyWidth <= metrics.viewportWidth, `${label}: body should not be wider than viewport`);
  assert.ok(metrics.boxes.length > 0, `${label}: expected checked controls to render`);
  for (const box of metrics.boxes) {
    assert.ok(box.left >= 0, `${label}: ${box.selector} bleeds left`);
    assert.ok(box.right <= metrics.viewportWidth, `${label}: ${box.selector} bleeds right`);
  }
}

const herdDropdownBody = pageHtml(`
<main class="herd-signals-page lt-page proof-main">
  <section class="lt-fbar">
    <label class="fsel search has"><span>Search</span><input value="A0002A F0:C9:90:A0:00:2A long value" /></label>
    <label class="fsel"><span>Pen</span><select><option selected>All pens including quarantine and nursery overflow</option></select></label>
    <label class="fsel"><span>Movement</span><select><option selected>No movement, quiet, missing signal, and movement spike</option></select></label>
    <label class="fsel"><span>Pattern</span><select><option selected>Any pattern with very long selected label</option></select></label>
  </section>
  <section class="fs proof-static-fs"><div class="fsbd"><div class="daterow"><span class="rowlabel">Range</span><div class="rangepick"><button>1h</button><button>6h</button><button>12h</button><button class="on">24h</button><button>3d</button><button>7d</button><button>30d</button></div><input type="datetime-local" value="2026-09-11T20:24" /><input type="datetime-local" value="2026-09-12T20:24" /></div></div></section>
</main>`);

// The vaccination plan editor fixture moved to features/vaccination-plan/responsive-css.test.mjs
// (vaccination-plan-template): its controls are MUI parts now, not the .vplan markup this fixture drew.
test("herd signal filters and range controls stay inside laptop and mobile viewports", async () => {
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
    await withPage(viewport, async (page) => {
      await page.setContent(herdDropdownBody, { waitUntil: "load" });
      await assertViewportFit(page, `herd signals filters ${viewport.width}px`, [
        ".herd-signals-page .lt-fbar",
        ".herd-signals-page .fsel",
        ".herd-signals-page .fsel select",
        ".herd-signals-page .daterow",
        ".herd-signals-page .rangepick",
        ".herd-signals-page input[type=datetime-local]",
      ]);
    });
  }
});
