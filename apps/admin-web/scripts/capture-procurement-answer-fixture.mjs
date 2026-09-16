// Isolated Chrome fixture: actual Answer component + production CSS, no API or DB.
import assert from 'node:assert/strict';
import {readFileSync, mkdirSync, writeFileSync} from 'node:fs';
import {resolve, dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import {chromium} from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
const appRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const source = readFileSync(resolve(appRoot, 'features/procurement/animal-purchase-sop.tsx'), 'utf8');
const parsed = ts.createSourceFile('actual.tsx', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const component = parsed.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === 'AnimalPurchaseAnswers');
assert.ok(component, 'Actual exported component must exist');
// Compile the exact function AST; unrelated media dependencies never enter this fixture.
const code = ts.transpileModule(component.getText(parsed).replace(/^export /, ''), {compilerOptions: {jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2022}}).outputText;
const AnimalPurchaseAnswers = vm.runInNewContext(`${code}\nAnimalPurchaseAnswers`, {React});
const rows = [
  {question_id: 'goat_id', section: 'Animal details', question: 'Goat identifier', answer: 'FIXTURE-001', attention: false},
  {question_id: 'breed', section: 'Animal details', question: 'Breed', answer: 'Boer cross', attention: false},
  {question_id: 'health', section: 'Health inspection', question: 'Visible injury or illness?', answer: 'Needs further review', attention: true},
  {question_id: 'walking', section: 'Health inspection', question: 'Walking normally?', answer: 'Yes', attention: false},
  {question_id: 'field_verdict', section: 'Your verdict', question: 'Recommendation', answer: 'Review before purchase', attention: true},
];
const copy = {verdictSection: "Director's verdict", attentionHint: 'Needs attention'};
const markup = renderToStaticMarkup(React.createElement(AnimalPurchaseAnswers, {rows, copy}));
const css = readFileSync(resolve(appRoot, 'app/mesha-theme.css'), 'utf8');
const output = resolve(process.env.GOATOS_FIXTURE_OUTPUT ?? '/tmp/pr273-procurement-answer-fixture');
mkdirSync(output, {recursive: true});
const html = `<!doctype html><meta charset="utf-8"><style>${css}</style><style>body{padding:24px;font-family:var(--f);background:var(--bg);color:var(--ink)}main{max-width:680px;margin:auto;background:var(--panel);padding:24px;border-radius:14px}h1{font-size:20px}p{font-size:13px;color:var(--muted)}</style><main><h1>Procurement answer fixture</h1><p>Isolated component proof · synthetic answers · no live procurement data</p>${markup}</main>`;
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
    assert.equal(await page.locator('.ap-attention-dot').count(), 2);
    assert.equal(await page.locator('.ap-sop-row:not(.attention) .ap-attention-dot').count(), 0);
    const dots = await page.locator('.ap-attention-dot').evaluateAll((nodes) => nodes.map((node) => {const s = getComputedStyle(node);return {width: s.width, height: s.height, color: s.backgroundColor, label: node.getAttribute('aria-label')};}));
    for (const dot of dots) {assert.equal(dot.width, '9px');assert.equal(dot.height, '9px');assert.equal(dot.color, 'rgb(224, 165, 58)');assert.equal(dot.label, 'Needs attention');}
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    const screenshot = resolve(output, `${name}.png`);
    await page.screenshot({path: screenshot, fullPage: true});
    evidence.viewports.push({name, dots, screenshot, axe_violations: axe.violations});
    await context.close();
  }
} finally {await browser.close();}
writeFileSync(resolve(output, 'evidence.json'), JSON.stringify(evidence, null, 2));
console.log(output);
