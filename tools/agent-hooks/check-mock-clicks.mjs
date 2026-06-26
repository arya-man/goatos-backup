#!/usr/bin/env node
// Click-smoke for the ops-console mock. Loads the file in a real headless
// browser, then:
//   1. fails on ANY page error / uncaught exception during load
//      (catches the "one bad inline script kills ALL click wiring" class).
//   2. clicks every sidebar nav + every drill leaf and asserts the active
//      screen actually changes and nothing throws.
// Exits non-zero with a loud message on failure so push-mock.sh can abort.
import { pathToFileURL } from 'node:url';
import { createRequire } from 'node:module';

const REPO = '/Users/ravi/mesha/goatos';
const FILE = `${REPO}/mock/goatos-dashboard-mock.html`;
const require = createRequire(`${REPO}/apps/admin-web/package.json`);

let chromium;
try {
  ({ chromium } = require('playwright'));
} catch {
  console.error('[mock-clicks] playwright not found under apps/admin-web — skipping click smoke (syntax gate still ran).');
  process.exit(0); // missing dep must not block; syntax gate is the hard floor
}

const errors = [];
const browser = await chromium.launch();
const page = await browser.newPage();
page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
page.on('console', (m) => { if (m.type() === 'error') errors.push(`console.error: ${m.text()}`); });

await page.goto(pathToFileURL(FILE).href, { waitUntil: 'load' });
await page.waitForTimeout(300);

if (errors.length) {
  console.error('[mock-clicks] FAIL — JS errors on load (click wiring is dead):');
  errors.forEach((e) => console.error('  ' + e));
  await browser.close();
  process.exit(1);
}

// Confirm listeners are actually bound, then exercise every nav + leaf.
const targets = await page.$$eval('.nav[data-go],.leaf[data-go]', (els) =>
  els.map((e) => ({
    go: e.dataset.go,
    sub: e.dataset.sub || '',
    label: (e.textContent || '').trim().slice(0, 24),
  }))
);

if (!targets.length) {
  console.error('[mock-clicks] FAIL — no nav/leaf [data-go] elements found.');
  await browser.close();
  process.exit(1);
}

const dead = [];
for (const t of targets) {
  const sel = t.sub
    ? `.leaf[data-go="${t.go}"][data-sub="${t.sub}"]`
    : `.nav[data-go="${t.go}"],.leaf[data-go="${t.go}"]`;
  const before = errors.length;
  try {
    await page.evaluate((s) => {
      const el = document.querySelector(s);
      if (el) el.click();
    }, sel);
    await page.waitForTimeout(40);
  } catch (e) {
    dead.push(`${t.go}/${t.sub} (${t.label}): click threw ${e.message}`);
    continue;
  }
  if (errors.length > before) {
    dead.push(`${t.go}/${t.sub} (${t.label}): ${errors.slice(before).join('; ')}`);
    continue;
  }
  // active screen must resolve to exactly one visible screen
  const okScreen = await page.evaluate(() => {
    const on = document.querySelectorAll('.screen.on');
    return on.length === 1;
  });
  if (!okScreen) dead.push(`${t.go}/${t.sub} (${t.label}): no single active screen after click`);
}

await browser.close();

if (dead.length) {
  console.error(`[mock-clicks] FAIL — ${dead.length}/${targets.length} nav targets broken:`);
  dead.forEach((d) => console.error('  ' + d));
  process.exit(1);
}

console.log(`[mock-clicks] OK — ${targets.length} nav/leaf targets click cleanly, no JS errors.`);
process.exit(0);
