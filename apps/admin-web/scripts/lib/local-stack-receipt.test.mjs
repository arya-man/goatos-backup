import test from 'node:test';
import assert from 'node:assert/strict';
import { validateLocalStackReceipt, validateBuildProvenance } from './local-stack-receipt.mjs';
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
  const proof = {git_sha: 'sha', build_id: 'build'};
  assert.equal(validateBuildProvenance(proof, 'build', 'sha'), proof);
  assert.throws(() => validateBuildProvenance(proof, 'new-build', 'sha'), /does not match/);
  assert.throws(() => validateBuildProvenance(proof, 'build', 'new-sha'), /does not match/);
  assert.throws(() => validateBuildProvenance(null, 'build', 'sha'), /does not match/);
});
