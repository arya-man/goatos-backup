import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import ts from 'typescript';
import {chromium} from '@playwright/test';
const source = readFileSync(new URL('./smoke-visual-live.mjs', import.meta.url), 'utf8');
const ast = ts.createSourceFile('smoke.js', source, ts.ScriptTarget.Latest, true);
const helpers = [];
function visit(node) {
  if (ts.isFunctionDeclaration(node) && ['reachableStackAvatar', 'intentionalAvatarOverlap', 'unclippedAvatarText'].includes(node.name?.text)) helpers.push(node.getText(ast));
  ts.forEachChild(node, visit);
}
visit(ast);
test('Chrome stacked-avatar guard accepts only bounded individually reachable siblings', async () => {
  assert.equal(helpers.length, 3);
  const browser = await chromium.launch({channel: 'chrome'});
  try {
    const page = await browser.newPage();
    await page.setContent('<style>.avs{display:flex}.av{box-sizing:border-box;width:30px;height:30px;flex-shrink:0;margin-left:-8px}.av:first-child{margin-left:0}</style><div class="wb"><div class="avs"><button class="av">A</button><button class="av">B</button></div></div>');
    const check = new Function(`${helpers.join('\n')} const [a,b]=document.querySelectorAll('button');const ar=a.getBoundingClientRect(),br=b.getBoundingClientRect();return intentionalAvatarOverlap(a,b,Math.max(0,Math.min(ar.right,br.right)-Math.max(ar.left,br.left)),Math.max(0,Math.min(ar.bottom,br.bottom)-Math.max(ar.top,br.top)));`);
    assert.equal(await page.evaluate(check), true);
    await page.locator('button').last().evaluate((e) => e.style.marginLeft='-20px');
    assert.equal(await page.evaluate(check), false);
    await page.locator('button').last().evaluate((e) => e.style.marginLeft='-8px');
    await page.locator('.wb').evaluate((e) => e.className='other');
    assert.equal(await page.evaluate(check), false);
    await page.locator('.other').evaluate((e) => e.className='wb');
    await page.evaluate(() => {const cover=document.createElement('div');cover.style.cssText='position:fixed;inset:0;z-index:100';document.body.append(cover);});
    assert.equal(await page.evaluate(check), false);
  } finally {await browser.close();}
});

test('actual Work Board milk tag CSS keeps white label above AA contrast', async () => {
  const css = readFileSync(new URL('../app/mesha-theme.css', import.meta.url), 'utf8');
  const browser = await chromium.launch({channel: 'chrome'});
  const luminance = (color) => color.match(/\d+/g).slice(0, 3).map(Number).map((v) => v / 255).map((v) => v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4).reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
  try {
    const page = await browser.newPage();
    await page.setContent(`<style>${css}</style><div class="wb"><span class="etag e-milk">MILK</span></div>`);
    const colors = await page.locator('.e-milk').evaluate((e) => {const s=getComputedStyle(e);return {fg:s.color,bg:s.backgroundColor};});
    const ratio = (fg, bg) => (Math.max(luminance(fg), luminance(bg)) + 0.05) / (Math.min(luminance(fg), luminance(bg)) + 0.05);
    assert.ok(ratio(colors.fg, 'rgb(62, 142, 147)') < 4.5, 'fixture must reproduce old failure');
    assert.ok(ratio(colors.fg, colors.bg) >= 4.5, JSON.stringify(colors));
    console.log(`milk label contrast before=${ratio(colors.fg, 'rgb(62, 142, 147)').toFixed(2)} after=${ratio(colors.fg, colors.bg).toFixed(2)}`);
  } finally {await browser.close();}
});

test('production mobile avatar halo is not text clipping, but overflowing text still fails', async () => {
  const css = readFileSync(new URL('../app/mesha-theme.css', import.meta.url), 'utf8');
  const browser = await chromium.launch({channel: 'chrome'});
  try {
    const page = await browser.newPage({viewport:{width:390,height:800}});
    await page.setContent(`<style>${css}</style><main class="main"><div class="wb"><div class="avs"><button class="av">AK</button><button class="av">CK</button><button class="more">+4</button></div></div></main>`);
    const check = new Function(`${helpers.join('\n')} const [a,b]=document.querySelectorAll('button');const ar=a.getBoundingClientRect(),br=b.getBoundingClientRect();return {unclipped:unclippedAvatarText(a), width:ar.width,height:ar.height,allowed:intentionalAvatarOverlap(a,b,Math.min(ar.right,br.right)-Math.max(ar.left,br.left),Math.min(ar.bottom,br.bottom)-Math.max(ar.top,br.top))};`);
    assert.deepEqual(await page.evaluate(check), {unclipped:true,width:30,height:40,allowed:true});
    await page.evaluate(() => {
      const stack=document.querySelector('.avs');
      const toolbar=document.createElement('div');toolbar.className='tbar';toolbar.style.width='200px';
      stack.parentElement.insertBefore(toolbar,stack);
      const spacer=document.createElement('span');spacer.style.width='220px';toolbar.append(spacer,stack);
    });
    assert.equal((await page.evaluate(check)).unclipped, true);
    assert.equal(await page.locator('.tbar').evaluate((e) => e.scrollLeft), 0, 'reachability probe restores toolbar');
    await page.locator('button').first().evaluate((e) => e.textContent='TOO-LONG-TO-FIT');
    assert.equal((await page.evaluate(check)).unclipped, false);
  } finally {await browser.close();}
});
