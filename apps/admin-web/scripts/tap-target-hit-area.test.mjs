// The sub-40px tap-target check measures the box a FINGER lands on, not the bare element rect.
//
// It posted "Buttons too small to tap — Search by title, or type a task number" about the tasks
// search field. That field is an 18px-tall <input> inside a 44px padded, bordered `.lt-fsearch`
// box, and the 44px box is what a person sees and taps: measured on production /tasks at 390px,
// a real touch tap at the box's top+8px, bottom-3px and centre each landed in the input. The
// check was reading the inner rect and calling a normal-sized box a failure.
//
// These tests run the real helper from the real smoke script against the real theme CSS, and
// pin both halves of the contract: the padded field passes, and things that are genuinely too
// small to tap still fail.
import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import ts from 'typescript';
import {chromium} from '@playwright/test';

const source = readFileSync(new URL('./smoke-visual-live.mjs', import.meta.url), 'utf8');
const ast = ts.createSourceFile('smoke.js', source, ts.ScriptTarget.Latest, true);
const HELPERS = ['tapSurface', 'hasOwnWords'];
const helpers = [];
let selectorSrc = null;
(function visit(node) {
  if (ts.isFunctionDeclaration(node) && HELPERS.includes(node.name?.text)) helpers.push(node.getText(ast));
  if (ts.isVariableDeclaration(node) && node.name.getText(ast) === 'interactiveSelector') selectorSrc = `const ${node.getText(ast)};`;
  ts.forEachChild(node, visit);
})(ast);
const helperSrc = helpers.join('\n');

const css = readFileSync(new URL('../app/mesha-theme.css', import.meta.url), 'utf8');
const MIN = 40;

// Measure a selector the way the check does: its own rect, and the tap surface the check uses.
const measure = (selector) => new Function(`
  ${selectorSrc}
  ${helperSrc}
  const element = document.querySelector(${JSON.stringify(selector)});
  if (!element) throw new Error("fixture is missing " + ${JSON.stringify(selector)});
  const own = element.getBoundingClientRect();
  const hit = tapSurface(element, ${MIN});
  return {
    own: [Math.round(own.width), Math.round(own.height)],
    hit: [Math.round(hit.width), Math.round(hit.height)],
    flagged: hit.width < ${MIN} || hit.height < ${MIN},
  };
`);

async function onPage(html, run) {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({viewport: {width: 390, height: 844}});
    await page.setContent(`<style>${css}</style>${html}`);
    await run(page);
  } finally {
    await browser.close();
  }
}

test('the smoke script still exposes the helper these tests pin', () => {
  assert.equal(helpers.length, HELPERS.length, `smoke-visual-live.mjs must still declare ${HELPERS.join(', ')} as named functions`);
  assert.ok(selectorSrc, 'interactiveSelector must stay a named const in smoke-visual-live.mjs');
});

test('the tasks search field is measured by its padded box, not its 18px inner input', async () => {
  // The real markup: features/leadership-tasks/leadership-tasks-filters.tsx.
  await onPage(`
    <div class="lt-page"><div class="lt-fbar lt-fsheet-host" role="group" aria-label="Filter tasks">
      <span class="lt-fsearch"><svg class="ic" style="width:15px" aria-hidden="true"></svg>
        <input type="search" aria-label="Search by title, or type a task number" value="pen">
        <button type="button" class="lt-qclr" aria-label="Clear">x</button>
      </span>
    </div></div>`, async (page) => {
    const result = await page.evaluate(measure('.lt-fsearch input'));
    assert.ok(result.own[1] < MIN, `fixture must reproduce the old failure: inner input was ${result.own.join('x')}`);
    assert.ok(result.hit[1] >= MIN, `the padded box a finger lands on must be measured, got ${result.hit.join('x')}`);
    assert.ok(result.hit[0] >= MIN, `the padded box must be wide enough too, got ${result.hit.join('x')}`);
    assert.equal(result.flagged, false, 'a normal-sized search box must not be posted to Slack as too small to tap');
    console.log(`tasks search: own=${result.own.join('x')} hit=${result.hit.join('x')}`);
  });
});

test('the surface never grows into a NEIGHBOURING control - the Clear button keeps its own pixels', async () => {
  await onPage(`
    <div class="lt-page"><div class="lt-fbar lt-fsheet-host">
      <span class="lt-fsearch"><svg class="ic" style="width:15px" aria-hidden="true"></svg>
        <input type="search" aria-label="Search by title, or type a task number" value="pen">
        <button type="button" class="lt-qclr" aria-label="Clear" style="width:40px;height:40px">x</button>
      </span>
    </div></div>`, async (page) => {
    const layout = await page.evaluate(() => {
      const input = document.querySelector('.lt-fsearch input').getBoundingClientRect();
      const clear = document.querySelector('.lt-qclr').getBoundingClientRect();
      const wrap = document.querySelector('.lt-fsearch').getBoundingClientRect();
      return {inputRight: Math.round(input.right), clearLeft: Math.round(clear.left), wrapWidth: Math.round(wrap.width)};
    });
    assert.ok(layout.clearLeft >= layout.inputRight - 1, 'fixture must place Clear to the right of the input');
    const result = await page.evaluate(measure('.lt-fsearch input'));
    assert.ok(result.hit[0] < layout.wrapWidth,
      `the surface must stop at the Clear button, got ${result.hit[0]} of the ${layout.wrapWidth}px wrapper`);
    assert.equal(result.flagged, false, 'clipping at the neighbour must not turn the field back into a false alarm');
    console.log(`clear-button clipping: hit width ${result.hit[0]} < wrapper ${layout.wrapWidth}`);
  });
});

test('a genuinely small link inside a card that carries its own words still fails', async () => {
  // Real shape, production /alerts at 390px: a 37x20 "Retry" link inside a 336x69 `.alert` card
  // that also reads "Alerts could not be loaded. Try again." Tapping that sentence does nothing,
  // so the card is not the link's tap target and the link is a real failure.
  await onPage(`
    <div class="lt-page"><section class="card"><div class="alert" style="display:flex;gap:8px;align-items:center;padding:13px 16px;border:1px solid #f0635f;background:rgba(240,99,95,0.16);width:336px">
      Alerts could not be loaded. Try again.
      <a href="#" style="display:block">Retry</a>
    </div></section></div>`, async (page) => {
    const result = await page.evaluate(measure('.alert a'));
    assert.ok(result.own[0] < MIN || result.own[1] < MIN, `fixture must be genuinely small, got ${result.own.join('x')}`);
    assert.deepEqual(result.hit, result.own, 'a card that speaks for itself is not the link tap target');
    assert.equal(result.flagged, true, 'a genuinely too-small link must still be reported');
    console.log(`alerts Retry: own=${result.own.join('x')} hit=${result.hit.join('x')} flagged=${result.flagged}`);
  });
});

test('a bare small button with no control box around it still fails', async () => {
  await onPage('<div class="lt-page"><div><button style="width:20px;height:20px;border:0;padding:0">x</button></div></div>', async (page) => {
    const result = await page.evaluate(measure('button'));
    assert.deepEqual(result.hit, [20, 20], 'nothing around it can be claimed as finger area');
    assert.equal(result.flagged, true, 'a 20px button is exactly what this check exists to catch');
    console.log(`bare button: own=${result.own.join('x')} hit=${result.hit.join('x')} flagged=${result.flagged}`);
  });
});

test('a small icon button cannot claim a wide empty toolbar as its tap area', async () => {
  await onPage(`
    <div class="lt-page"><div style="width:340px;height:60px;border:1px solid #333;background:#161f1a;display:flex;align-items:center">
      <button aria-label="Close" style="width:20px;height:20px;border:0;padding:0;background:transparent"></button>
    </div></div>`, async (page) => {
    const result = await page.evaluate(measure('button'));
    assert.equal(result.flagged, true, 'a 20px button in a 340px bar is still a 20px button');
    console.log(`icon in wide bar: own=${result.own.join('x')} hit=${result.hit.join('x')} flagged=${result.flagged}`);
  });
});

test('the walk stops as soon as the surface is big enough, it does not keep climbing', async () => {
  // A 30px control in a 60px control box, itself inside a 300px panel that would also qualify.
  // The answer must be the 60px box: the measurement stops being useful once it clears the bar,
  // and a check that kept climbing would end up calling every target "big enough".
  await onPage(`
    <div class="lt-page"><div style="width:300px;height:300px;border:1px solid #333;background:#161f1a">
      <span style="display:inline-flex;align-items:center;justify-content:center;width:60px;height:60px;border:1px solid #333;background:#0e1512">
        <button aria-label="Tiny" style="width:30px;height:30px;border:0;padding:0;background:transparent"></button>
      </span>
    </div></div>`, async (page) => {
    const result = await page.evaluate(measure('button'));
    assert.deepEqual(result.hit, [60, 60], 'the control box is the answer, not the panel around it');
    assert.equal(result.flagged, false);
    console.log(`bounded growth: own=${result.own.join('x')} hit=${result.hit.join('x')} (panel is 300x300)`);
  });
});
