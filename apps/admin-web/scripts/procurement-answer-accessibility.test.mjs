import assert from 'node:assert/strict';
import test from 'node:test';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {mkdtemp, readFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';

test('populated actual procurement answers have valid attention ARIA on desktop and mobile', {timeout: 180000}, async () => {
  const output = await mkdtemp(join(tmpdir(), 'procurement-answer-a11y-'));
  try {
    await promisify(execFile)(process.execPath, [fileURLToPath(new URL('./capture-procurement-answer-fixture.mjs', import.meta.url))], {
      env: {...process.env, GOATOS_FIXTURE_OUTPUT: output}, timeout: 120000, maxBuffer: 1024 * 1024,
    });
    const evidence = JSON.parse(await readFile(join(output, 'evidence.json'), 'utf8'));
    assert.deepEqual(evidence.viewports.map(item => item.name), ['desktop', 'mobile']);
    for (const viewport of evidence.viewports) {
      assert.equal(viewport.dots.length, 2);
      assert.deepEqual(viewport.axe_violations, []);
    }
  } finally {
    await rm(output, {recursive: true, force: true});
  }
});
