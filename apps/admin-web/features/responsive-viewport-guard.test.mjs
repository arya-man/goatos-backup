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

const vaccinationDropdownBody = pageHtml(`
<main class="vplan">
  <section class="head"><div class="head-top"><div><div class="eyebrow">Editing V10</div><h1>Company vaccination plan</h1><p class="sub">Dropdown viewport guard.</p></div><div class="hactions"><button class="btn">Open the draft</button></div></div></section>
  <section class="panes">
    <aside class="leftcol"><div class="vp-sheet vp-nvf"><div class="field"><span>Vaccine</span><select><option selected>Blue Tongue emergency booster with a very long display value</option></select></div><div class="field"><span>First dose</span><select><option selected>Give from date of birth plus anchor/base campaign date</option></select></div></div></aside>
    <section class="card"><div class="card-b"><div class="dose"><div class="dose-h"><span class="lbl">Give it when the animal is <span class="vp-dur"><button class="f">4 weeks</button></span> old</span></div></div><div class="seg"><button class="segb" aria-pressed="true">Routine vaccination drive</button><button class="segb">Emergency catch-up campaign</button></div><div class="vp-seg"><button class="is-on">Date of birth</button><button>Manual campaign</button><button>After previous completion</button></div></div></section>
  </section>
</main>`);

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

// The duration editor itself is the template popover (CustomPopover in duration-field.tsx): MUI clamps
// its paper to the viewport (marginThreshold + maxWidth calc(100% - 32px)), so it is not in this static fixture.
test("vaccination dropdown values and duration popover stay inside laptop and mobile viewports", async () => {
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
    await withPage(viewport, async (page) => {
      await page.setContent(vaccinationDropdownBody, { waitUntil: "load" });
      await assertViewportFit(page, `vaccination dropdown ${viewport.width}px`, [
        ".vplan .vp-dur",
        ".vplan .field select",
        ".vplan .seg",
        ".vplan .vp-seg",
      ]);
    });
  }
});

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
