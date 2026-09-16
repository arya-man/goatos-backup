import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { assertPayload } from './api-latency-assertion.mjs';
test('both current-day Alerts paths require a successful stock read even with no alerts', () => {
  const manifest = JSON.parse(readFileSync(new URL('./hot-paths.alerts.json', import.meta.url)));
  const endpoints = manifest.endpoints.filter(e => e.name.endsWith('_today'));
  assert.equal(endpoints.length, 2);
  for (const endpoint of endpoints) {
    assert.ok(!endpoint.path.includes('business_date='));
    assertPayload(endpoint, { rows: [], rules_run: ['feed_low_stock'] });
    for (const payload of [ {}, {rules_run: []}, {rules_run: ['pen_feed_quantity_change'], degraded: ['feed_low_stock']}, {rules_run: [], skipped: [{key: 'feed_low_stock'}]} ]) {
      assert.throws(() => assertPayload(endpoint, payload), /must contain feed_low_stock/);
    }
  }
});
test('existing array and numeric assertions still reject deficient payloads', () => {
  for (const [assertion,good,bad] of [
    [{type:'array_min',path:'rows',min:1},{rows:[1]},{rows:[]}],
    [{type:'number_min',path:'total',min:1},{total:1},{total:0}],
  ]) {
    assertPayload({name:'test',assertion}, good);
    assert.throws(() => assertPayload({name:'test',assertion}, bad));
  }
});
