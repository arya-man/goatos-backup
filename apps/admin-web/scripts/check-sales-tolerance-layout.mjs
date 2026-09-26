import assert from 'node:assert/strict';
import { chromium } from '@playwright/test';

// Exercise the actual route at the laptop interval where KPIs remain three columns.
const base = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? 'http://127.0.0.1:3300').replace(/\/$/, '');
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
if (!bearerToken) {
  console.error('Missing required live-smoke env: GOATOS_BEARER_TOKEN');
  process.exit(2);
}

const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const context = await browser.newContext();
  const cookieUrl = new URL(base);
  await context.addCookies([
    {
      name: 'goatos_firebase_id_token',
      value: bearerToken,
      domain: cookieUrl.hostname,
      path: '/',
      httpOnly: true,
      sameSite: 'Lax',
      expires: Math.floor(Date.now() / 1000) + 3600,
    },
  ]);
  const page = await context.newPage();
  for (const width of [390, 1081, 1280, 1366, 1440, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${base}/sales/farm-value`, { waitUntil: 'networkidle' });
    const checkErrors = async () => assert.doesNotMatch(await page.locator('body').innerText(), /backend_down|Admin-web contract unavailable|The board could not be loaded|Weights could not be loaded/);
    await checkErrors();
    const footer = page.locator('.sales-ready-tolerance');
    await footer.waitFor();
    const bounds = await footer.evaluate(el => {
      const card = (el.closest('.kit-kpi, .MuiCard-root, .kpi, .card') ?? el.parentElement).getBoundingClientRect();
      return [...el.querySelectorAll('label,strong,input,output,button')].map(child => {
        const box = child.getBoundingClientRect();
        return { tag: child.tagName, inside: box.left >= card.left && box.right <= card.right && box.top >= card.top && box.bottom <= card.bottom };
      });
    });
    assert.ok(bounds.every(x => x.inside), `${width}px: clipped control ${JSON.stringify(bounds)}`);
    const button = footer.getByRole('button');
    assert.equal(await button.isDisabled(), true);
    const slider = footer.getByRole('slider');
    await slider.focus();
    await slider.press('ArrowRight');
    assert.equal(await button.isEnabled(), true);
    await button.click();
    await page.waitForURL(/sale_ready_tolerance_g=50/);
    await page.waitForFunction(() => document.querySelector('.sales-ready-tolerance button')?.disabled === true);
    assert.equal(new URL(page.url()).pathname, '/sales/farm-value');
    assert.equal(await page.locator('#sale-ready-tolerance').inputValue(), '50');
    await checkErrors();
    if (process.env.GOATOS_SCREENSHOT_DIR) await page.screenshot({ path: `${process.env.GOATOS_SCREENSHOT_DIR}/farm-value-${width}.png`, fullPage: true });
    console.log(`PASS ${width}px: controls contained; Apply stays on Farm value and reads back 50 g`);
  }
} finally {
  await browser.close();
}
