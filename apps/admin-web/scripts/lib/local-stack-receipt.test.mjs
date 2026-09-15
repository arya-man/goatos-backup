import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import assert from 'node:assert/strict';
import { validateLocalStackReceipt, validateBuildProvenance, sourceStatus, cleanBuildSource } from './local-stack-receipt.mjs';
const receipt = {schema_version: 1, pid: 123, process_identity: 'started now', created_at: new Date().toISOString(), git_sha: 'sha', api_base_url: 'http://127.0.0.1:18174', admin_web_base_url: 'http://127.0.0.1:13473'};
test('controlled launch receipt matches exact API, admin origin and build', () => {
  assert.equal(validateLocalStackReceipt(receipt, receipt, () => 'started now'), receipt);
  for (const key of ['git_sha', 'api_base_url', 'admin_web_base_url']) {
    assert.throws(() => validateLocalStackReceipt(receipt, {...receipt, [key]: 'other'}, () => 'started now'), /mismatch/);
  }
});
test('dead/reused PID and missing launch receipt cannot certify browser build', () => {
  assert.throws(() => validateLocalStackReceipt(null, receipt), /Missing/);
  assert.throws(() => validateLocalStackReceipt(receipt, receipt, () => {throw new Error('dead');}), /dead/);
  assert.throws(() => validateLocalStackReceipt(receipt, receipt, () => 'reused'), /reused/);
});

test('production build provenance rejects stale build ID or checkout', () => {
  const proof = {git_sha: 'sha', build_id: 'build', clean_source: true, source_status_start: [], source_status_end: []};
  assert.equal(validateBuildProvenance(proof, 'build', 'sha'), proof);
  assert.throws(() => validateBuildProvenance(proof, 'new-build', 'sha'), /does not match/);
  assert.throws(() => validateBuildProvenance(proof, 'build', 'new-sha'), /does not match/);
  assert.throws(() => validateBuildProvenance(null, 'build', 'sha'), /does not match/);
});

test('dirty source before or after compilation cannot certify clean HEAD', () => {
  for (const [before, after] of [[[' M app/page.tsx'], []], [[], ['?? app/new.tsx']], [null, []]]) {
    assert.equal(cleanBuildSource(before, after), false);
    assert.throws(() => validateBuildProvenance({git_sha:'sha',build_id:'build',clean_source:true,source_status_start:before,source_status_end:after}, 'build', 'sha'), /dirty/);
  }
});

test('source status excludes ignored proof/build files but catches untracked source', () => {
  const dir = mkdtempSync(join(tmpdir(), 'goatos-provenance-'));
  const git = (...args) => execFileSync('git', ['-C', dir, ...args], {stdio:'ignore'});
  try {
    git('init');
    writeFileSync(join(dir,'.gitignore'), '.next/\n.codex-goatos-render/\n');
    git('add','.gitignore');
    git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-m','fixture');
    for (const name of ['.next','.codex-goatos-render']) {mkdirSync(join(dir,name));writeFileSync(join(dir,name,'proof.json'),'{}');}
    assert.deepEqual(sourceStatus(dir), []);
    writeFileSync(join(dir,'new-source.ts'), 'export {};');
    assert.equal(sourceStatus(dir).length, 1);
  } finally {rmSync(dir,{recursive:true,force:true});}
});
