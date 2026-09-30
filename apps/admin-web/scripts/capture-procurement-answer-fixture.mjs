// Isolated Chrome fixture: the actual AnimalPurchaseAnswers component, bundled from source with the
// app's MUI theme (template anatomy: Box/Typography + theme sx), no API or DB.
import assert from 'node:assert/strict';
import {readFileSync, mkdirSync, writeFileSync} from 'node:fs';
import {resolve, dirname} from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {createHash} from 'node:crypto';
import {build} from 'esbuild';
import {chromium} from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
const appRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const sourcePath = resolve(appRoot, 'features/procurement/animal-purchase-sop.tsx');
const source = readFileSync(sourcePath, 'utf8');
assert.match(source, /export function AnimalPurchaseAnswers\(/, 'Actual exported component must exist');
const output = resolve(process.env.GOATOS_FIXTURE_OUTPUT ?? '/tmp/pr273-procurement-answer-fixture');
mkdirSync(output, {recursive: true});

// Bundle the real component with the app theme. Unrelated media / chip dependencies of the module
// (the lightbox's telemetry SDK, the kit Tag) are stubbed so they never enter this fixture.
const entry = `
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import {ThemeProvider} from '@mui/material/styles';
import CssBaseline from '@mui/material/CssBaseline';
import {createTheme} from '@/theme/create-theme';
import {AnimalPurchaseAnswers} from '@/features/procurement/animal-purchase-sop';
export function render(rows, copy) {
  const theme = createTheme();
  return renderToStaticMarkup(
    React.createElement(ThemeProvider, {theme, defaultMode: 'dark'},
      React.createElement(CssBaseline),
      React.createElement(AnimalPurchaseAnswers, {rows, copy})));
}`;
const stubs = {
  name: 'fixture-stubs',
  setup(b) {
    b.onResolve({filter: /^@\//}, (args) => {
      if (args.path === '@/components/ui-primitives') return {path: 'ui-primitives', namespace: 'stub'};
      return undefined;
    });
    b.onResolve({filter: /^\.\/animal-purchase-lightbox$/}, () => ({path: 'lightbox', namespace: 'stub'}));
    b.onResolve({filter: /\.css$/}, () => ({path: 'css', namespace: 'stub'}));
    b.onLoad({filter: /.*/, namespace: 'stub'}, (args) => ({
      contents: args.path === 'css' ? '' : 'export function Tag(){return null} export function AnimalPurchaseLightbox(){return null}',
      loader: 'js',
    }));
  },
};
const bundlePath = resolve(output, 'answers-bundle.mjs');
await build({
  stdin: {contents: entry, resolveDir: appRoot, loader: 'js'},
  bundle: true,
  platform: 'node',
  format: 'esm',
  outfile: bundlePath,
  jsx: 'automatic',
  alias: {'@': appRoot},
  plugins: [stubs],
  logLevel: 'silent',
  banner: {js: "import {createRequire as __cr} from 'module'; const require = __cr(import.meta.url);"},
});
const {render} = await import(pathToFileURL(bundlePath).href);

const rows = [
  {question_id: 'goat_id', section: 'Animal details', question: 'Goat identifier', answer: 'FIXTURE-001', attention: false},
  {question_id: 'breed', section: 'Animal details', question: 'Breed', answer: 'Boer cross', attention: false},
  {question_id: 'health', section: 'Health inspection', question: 'Visible injury or illness?', answer: 'Needs further review', attention: true},
  {question_id: 'walking', section: 'Health inspection', question: 'Walking normally?', answer: 'Yes', attention: false},
  {question_id: 'field_verdict', section: 'Your verdict', question: 'Recommendation', answer: 'Review before purchase', attention: true},
];
const copy = {verdictSection: "Director's verdict", attentionHint: 'Needs attention'};
const markup = render(rows, copy);
assert.doesNotMatch(markup, /class="[^"]*\bap-/, 'No legacy .ap-* class renders');
const html = `<!doctype html><html data-theme="dark"><meta charset="utf-8"><body><main style="max-width:680px;margin:24px auto;padding:24px"><h1>Procurement answer fixture</h1><p>Isolated component proof · synthetic answers · no live procurement data</p>${markup}</main></body></html>`;
writeFileSync(resolve(output, 'fixture.html'), html);
const browser = await chromium.launch(process.env.GOATOS_FIXTURE_BROWSER_CHANNEL ? {channel: process.env.GOATOS_FIXTURE_BROWSER_CHANNEL} : {});
const evidence = {kind: 'isolated_component_fixture_not_live_API', component_source_sha256: createHash('sha256').update(source).digest('hex'), viewports: []};
try {
  for (const [name, width] of [['desktop', 1000], ['mobile', 390]]) {
    const context = await browser.newContext({viewport: {width, height: 700}});
    const page = await context.newPage();
    await page.setContent(html);
    const axe = await new AxeBuilder({page}).include('main').withRules(['aria-prohibited-attr']).analyze();
    assert.deepEqual(axe.violations, [], 'Populated attention answers must have valid ARIA semantics');
    assert.equal(await page.locator('[data-attention-dot]').count(), 2);
    assert.equal(await page.locator('[data-question]:not([data-attention]) [data-attention-dot]').count(), 0);
    const warning = await page.evaluate(() => {
      const probe = document.createElement('span');
      probe.style.color = 'var(--palette-warning-main)';
      document.body.append(probe);
      const color = getComputedStyle(probe).color;
      probe.remove();
      return color;
    });
    assert.notEqual(warning, 'rgb(0, 0, 0)', 'theme warning token resolves');
    const dots = await page.locator('[data-attention-dot]').evaluateAll((nodes) => nodes.map((node) => {const s = getComputedStyle(node);return {width: s.width, height: s.height, color: s.backgroundColor, label: node.getAttribute('aria-label')};}));
    for (const dot of dots) {assert.equal(dot.width, '9px');assert.equal(dot.height, '9px');assert.equal(dot.color, warning);assert.equal(dot.label, 'Needs attention');}
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    const screenshot = resolve(output, `${name}.png`);
    await page.screenshot({path: screenshot, fullPage: true});
    evidence.viewports.push({name, dots, screenshot, axe_violations: axe.violations});
    await context.close();
  }
} finally {await browser.close();}
writeFileSync(resolve(output, 'evidence.json'), JSON.stringify(evidence, null, 2));
console.log(output);
