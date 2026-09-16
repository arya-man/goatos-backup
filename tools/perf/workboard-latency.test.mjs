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

function versionedBoardReports() {
  const data = board();
  data.summary.by_module = {weighing: 4};
  const sample = captureSample({ms:100}, data.summary, data.lanes);
  const before = {parks:{CBE:'park'},iterations:1,results:{CBE:{legacy:[sample],page:[structuredClone(sample)]}}};
  const after = structuredClone(before);
  for (const kind of ['legacy','page']) after.results.CBE[kind][0].snapshot.counts.by_module_lane = {weighing: {todo:1,in_progress:1,in_review:1,done:1}};
  return {before,after};
}
test('before/after accepts additive module-lane matrix only with exact existing work and consistent totals', () => {
  const {before,after} = versionedBoardReports();
  assert.equal(compareBoardReports(before,after).passed,true);
  assert.match(parityFailures(before.results.CBE.page[0].snapshot,after.results.CBE.page[0].snapshot).join(),/counts mismatch/);
  assert.equal(before.results.CBE.page[0].snapshot.counts.by_module_lane,undefined);
});
test('additive matrix cannot hide changed counts, rows, cursors or degradation', () => {
  for (const mutate of [
    s=>s.counts.by_module_lane.weighing.todo=2,
    s=>delete s.counts.by_module_lane.weighing.done,
    s=>s.counts.by_module_lane.extra={todo:0,in_progress:0,in_review:0,done:0},
    s=>s.counts.by_module_lane.weighing.todo=-1,
    s=>s.counts.total=5,
    s=>s.row_ids.todo=['lost'],
    s=>s.has_next_cursor.todo=true,
    s=>s.degraded=['vaccination'],
  ]) {
    const {before,after}=versionedBoardReports();mutate(after.results.CBE.page[0].snapshot);
    assert.equal(compareBoardReports(before,after).passed,false);
  }
});
test('both-version matrices must match exactly and removal is never allowed', () => {
  const {after}=versionedBoardReports();const before=structuredClone(after);
  assert.equal(compareBoardReports(before,after).passed,true);
  delete after.results.CBE.page[0].snapshot.counts.by_module_lane;
  assert.equal(compareBoardReports(before,after).passed,false);
});
test('existing matrices cannot change even when every aggregate total stays equal', () => {
  const {after:before}=versionedBoardReports();
  for (const kind of ['legacy','page']) {
    const counts=before.results.CBE[kind][0].snapshot.counts;
    counts.by_module={weighing:2,feed:2};
    counts.by_module_lane={weighing:{todo:1,in_progress:1,in_review:0,done:0},feed:{todo:0,in_progress:0,in_review:1,done:1}};
  }
  const after=structuredClone(before);
  const matrix=after.results.CBE.page[0].snapshot.counts.by_module_lane;
  [matrix.weighing,matrix.feed]=[matrix.feed,matrix.weighing];
  assert.match(compareBoardReports(before,after).failures.join(),/counts mismatch/);
});
