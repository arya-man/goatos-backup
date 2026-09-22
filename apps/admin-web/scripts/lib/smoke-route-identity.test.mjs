import test from 'node:test';
import assert from 'node:assert/strict';
import {assertSmokeRouteIdentity, assertAnimalPurchaseHeading} from './smoke-route-identity.mjs';
test('successful HTTP redirect to approvals cannot certify procurement', () => {
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/procurement/animal-purchases', 'http://localhost/approvals'), /redirected/);
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/work-board', 'http://other/work-board'), /redirected/);
  assert.equal(assertSmokeRouteIdentity('http://localhost/work-board?date=2026-08-10', 'http://localhost/work-board?date=2026-08-10'), '/work-board');
});
test('procurement needs its own rendered heading, including healthy empty data', () => {
  assert.equal(assertAnimalPurchaseHeading('Animal purchases'), true);
  for (const heading of ['Approvals', 'Loading', 'Procurement', '']) assert.throws(() => assertAnimalPurchaseHeading(heading), /heading/);
});

test('same-path redirects cannot change requested tab/date; extra defaults are allowed', () => {
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/weighing/analytics?tab=birth', 'http://localhost/weighing/analytics?tab=general'), /query parameter/);
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/work-board?date=2026-08-10', 'http://localhost/work-board'), /query parameter/);
  assert.equal(assertSmokeRouteIdentity('http://localhost/work-board?date=2026-08-10', 'http://localhost/work-board?date=2026-08-10&scope_mode=company'), '/work-board');
});

test('sales root may canonicalize to sold page only', () => {
  assert.equal(assertSmokeRouteIdentity('http://localhost/sales?scope_mode=company', 'http://localhost/sales/sold?scope_mode=company'), '/sales/sold');
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/sales?scope_mode=company', 'http://localhost/sales/loads?scope_mode=company'), /redirected/);
});

test('procurement root may canonicalize to source entry only', () => {
  assert.equal(assertSmokeRouteIdentity('http://localhost/procurement?scope_mode=company', 'http://localhost/procurement/source-entry'), '/procurement/source-entry');
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/procurement?scope_mode=company&status=open', 'http://localhost/procurement/source-entry'), /query parameter/);
  assert.throws(() => assertSmokeRouteIdentity('http://localhost/procurement?scope_mode=company', 'http://localhost/procurement/animal-purchases?scope_mode=company'), /redirected/);
});
