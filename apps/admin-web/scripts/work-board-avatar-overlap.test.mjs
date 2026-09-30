import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import ts from 'typescript';
import {chromium} from '@playwright/test';
import { legacyCss } from "./lib/legacy-css.mjs";
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

// FIXJ6: the legacy `.wb .etag.e-milk` rule died with mesha-theme.css; the work board renders the
// template kanban item (no `.etag` label), so there is no legacy milk tag left to contrast-check.
test('the work board renders no legacy .etag milk tag', () => {
  const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8');
  for (const f of ['../features/work-board/work-board-board.tsx', '../features/work-board/work-board-parts.tsx', '../features/work-board/work-board-modal.tsx']) {
    assert.doesNotMatch(read(f), /\betag\b|e-milk/, `${f} renders the retired .etag markup`);
  }
});

// REVIEW-18 O23: /work-board no longer renders the legacy `.wb .avs > button.av` stack (its halo CSS
// is gone); card assignees are the template kanban item AvatarGroup (components/app/kanban/item-styles.tsx):
// 24px caption avatars, template -8px overlap, display-only (the card itself is the tap target).
test('work-board assignees are the template kanban AvatarGroup, not the retired .avs stack', () => {
  const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8');
  for (const f of ['../features/work-board/work-board-board.tsx', '../features/work-board/work-board-modal.tsx', '../features/leadership-tasks/task-board-card.tsx']) {
    assert.doesNotMatch(read(f), /className=["'{][^"'}]*\bavs\b|className=["']av["']/, `${f} renders the retired .avs/.av markup`);
  }
  const item = read('../components/app/kanban/item-styles.tsx');
  assert.match(item, /<AvatarGroup[\s\S]*?width: 'calc\(3 \* var\(--spacing\)\)',\s*height: 'calc\(3 \* var\(--spacing\)\)',\s*typography: 'caption'/);
  assert.doesNotMatch(item.slice(item.indexOf('<AvatarGroup')), /<Avatar[^>]*onClick/, 'assignee avatars stay display-only');
});

test('a 24px caption avatar fits one initial, and overflowing text is detectable', async () => {
  const browser = await chromium.launch({channel: 'chrome'});
  try {
    const page = await browser.newPage({viewport:{width:390,height:800}});
    // MUI Avatar + AvatarGroup anatomy at the kanban item size (24px, caption 12px, -8px overlap).
    await page.setContent('<style>.g{display:flex;flex-direction:row-reverse}.a{display:flex;align-items:center;justify-content:center;box-sizing:content-box;width:24px;height:24px;border-radius:50%;overflow:hidden;font:400 12px/1.5 sans-serif;border:2px solid #1c252e;margin-left:-8px}</style><div class="g"><div class="a">C</div><div class="a">A</div></div>');
    const fits = () => page.evaluate(() => [...document.querySelectorAll('.a')].map((e) => { const r = document.createRange(); r.selectNodeContents(e); const t = r.getBoundingClientRect(), b = e.getBoundingClientRect(); return t.width <= b.width - 4 && t.left >= b.left && t.right <= b.right; }));
    assert.deepEqual(await fits(), [true, true]);
    const boxes = await page.evaluate(() => [...document.querySelectorAll('.a')].map((e) => Math.round(e.getBoundingClientRect().width)));
    assert.deepEqual(boxes, [28, 28], '24px avatars + template 2px ring');
    await page.locator('.a').first().evaluate((e) => { e.textContent = 'TOO-LONG'; });
    assert.equal((await fits())[0], false, 'overflowing text is caught');
  } finally {await browser.close();}
});
