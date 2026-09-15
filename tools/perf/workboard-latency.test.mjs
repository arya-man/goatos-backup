import assert from 'node:assert/strict';
import test from 'node:test';
import { LANES, compareBoardReports, captureSample, actorFingerprint, boardSnapshot, parityFailures, scenarioFailures, statistics } from './workboard-latency.mjs';
const jwt = (sub, exp = 1) => `header.${Buffer.from(JSON.stringify({ sub, iss: 'test', exp })).toString('base64url')}.signature`;
function board() {
  return { summary: { total: 4, by_lane: Object.fromEntries(LANES.map((lane) => [lane, 1])) }, lanes: Object.fromEntries(LANES.map((lane) => [lane, { rows: [{ row_key: `${lane}-one`, lane }] }])) };
}
test('actor comparison rejects changed subject/tenant without depending on token renewal', () => {
  assert.equal(actorFingerprint(jwt('one'), 'tenant'), actorFingerprint(jwt('one', 2), 'tenant'));
  const before = { actor_fingerprint: actorFingerprint(jwt('one'), 'tenant') };
  assert.deepEqual(scenarioFailures(before, before), []);
  assert.match(scenarioFailures(before, { actor_fingerprint: actorFingerprint(jwt('two'), 'tenant') }).join(), /actor_fingerprint/);
  assert.notEqual(actorFingerprint(jwt('one'), 'tenant'), actorFingerprint(jwt('one'), 'other'));
});
test('equal counts with changed row identities fail parity', () => {
  const data = board();
  const before = boardSnapshot(data.summary, data.lanes);
  data.lanes.todo.rows[0].row_key = 'different';
  assert.match(parityFailures(before, boardSnapshot(data.summary, data.lanes)).join(), /row_ids/);
});
test('summary mismatch and missing rows fail rather than produce false speedup', () => {
  const data = board();
  data.lanes.todo.rows = [];
  assert.throws(() => boardSnapshot(data.summary, data.lanes), /no first-page rows/);
  data.summary.total = 0;
  assert.throws(() => boardSnapshot(data.summary, data.lanes), /nonzero/);
});
test('degraded unknown modules fail even with healthy remaining rows', () => {
  const data = board();
  data.lanes.done.degraded = ['future_module'];
  assert.throws(() => boardSnapshot(data.summary, data.lanes), /future_module/);
});
test('duplicate keys, wrong lanes and total drift are invalid', () => {
  const data = board();
  data.lanes.todo.rows.push(data.lanes.todo.rows[0]);
  assert.throws(() => boardSnapshot(data.summary, data.lanes), /Duplicate/);
  data.lanes.todo.rows.pop();
  data.lanes.todo.rows[0].lane = 'done';
  assert.throws(() => boardSnapshot(data.summary, data.lanes), /Invalid identity/);
  data.lanes.todo.rows[0].lane = 'todo';
  data.summary.total = 99;
  assert.throws(() => boardSnapshot(data.summary, data.lanes), /disagree/);
});
test('tail percentile includes the slowest request in twenty samples', () => {
  const samples = Array.from({ length: 20 }, (_, i) => ({ ms: i === 19 ? 900 : 100, response_bytes: 42 }));
  assert.equal(statistics(samples).p99_ms, 900);
  assert.equal(statistics(samples).p95_ms, 100);
});

test('degraded samples retain measured timing and identities without becoming valid proof', () => {
  const data = board();
  data.lanes.todo.degraded = ['weighing'];
  const sample = captureSample({ ms: 870, response_bytes: 1024 }, data.summary, data.lanes);
  assert.equal(sample.valid, false);
  assert.match(sample.error, /weighing/);
  assert.equal(sample.ms, 870);
  assert.equal(sample.response_bytes, 1024);
  assert.deepEqual(sample.invalid_response.lanes.todo.row_ids, ['todo-one']);
  assert.deepEqual(sample.invalid_response.lanes.todo.degraded, ['weighing']);
  assert.equal(statistics([sample]).p99_ms, 870);
  assert.equal(statistics([sample].filter((item) => item.valid)).p99_ms, null);
});

test('before/after certification rejects lost rows and distinguishes invalid baseline', () => {
  const data = board();
  const sample = captureSample({ms: 100}, data.summary, data.lanes);
  const before = {parks: {CBE: 'park'}, iterations: 1, results: {CBE: {legacy: [sample], page: [sample]}}};
  const after = structuredClone(before);
  assert.equal(compareBoardReports(before, after).passed, true);
  after.results.CBE.page[0].snapshot.row_ids.todo = ['other'];
  assert.equal(compareBoardReports(before, after).passed, false);
  before.results.CBE.page[0].valid = false;
  assert.match(compareBoardReports(before, after).incomparable.join(), /row parity unproven/);
});
