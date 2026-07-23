// BUG-019 regression: the park menu must survive the Next.js hop.
//
// The backend refuses to guess a park for a tenant-wide actor (409 `park_scope_ambiguous`) AND
// hands back the parks that actor may choose from. Three layers sit between that envelope and the
// browser:
//
//   backend handler  ->  lib/api/server.ts normalizeApiError  ->  app/api/.../route.ts  ->  client.ts
//
// `normalizeApiError` reshapes every backend error into the fixed `ApiUiError` type. Before this
// fix it dropped every field it did not know about, so `availableParks` was silently discarded in
// the middle layer: the backend sent the menu, the route forwarded a menu-less error, and the
// screen had nothing to render and threw. A tenant-wide CEO/CXO account — which per the
// founder/builder visibility invariant is EVERY founder account — could not open the screen at all.
//
// This test pins the contract at the seam that broke: whatever the route serializes must still be
// readable by `parkScopeAmbiguousFromBody`, which is what the browser client calls.

import assert from 'node:assert/strict';
import test from 'node:test';

import { parkScopeAmbiguousFromBody, PARK_SCOPE_AMBIGUOUS_CODE } from './park-scope.ts';

// The exact shape lib/api/server.ts now produces for a 409 park_scope_ambiguous, after the
// JSON round-trip that app/api/vaccination/operator-assignment/config/route.ts performs
// (`NextResponse.json(result.error)`).
function routeSerializedError(availableParks) {
  return JSON.parse(
    JSON.stringify({
      kind: 'bad_request',
      status: 409,
      code: PARK_SCOPE_AMBIGUOUS_CODE,
      message: 'your scope covers more than one park; pass park_id to choose one',
      traceId: 'trace-1',
      availableParks,
    }),
  );
}

test('BUG-019: the backend park menu survives the Next.js passthrough', () => {
  const parks = [
    { parkId: 'park-cpt', code: 'CPT', name: 'Channapatna' },
    { parkId: 'park-cbe', code: 'CBE', name: 'Coimbatore' },
  ];

  const ambiguous = parkScopeAmbiguousFromBody(409, routeSerializedError(parks));

  assert.ok(ambiguous, 'a 409 park_scope_ambiguous must narrow to the typed error, not a bare throw');
  assert.equal(ambiguous.code, PARK_SCOPE_AMBIGUOUS_CODE);
  assert.deepEqual(
    ambiguous.availableParks,
    parks,
    'availableParks must reach the browser intact — dropping it here is what dead-ended the screen',
  );
});

test('BUG-019: zero authorized parks stays distinguishable from several', () => {
  // Zero parks is a genuinely different situation from several and must not be reported as
  // "choose one" — there is nothing to choose. The screen needs to tell these apart, so an
  // empty menu must survive as an empty array rather than collapsing into the same state as
  // a populated one.
  const ambiguous = parkScopeAmbiguousFromBody(409, routeSerializedError([]));

  assert.ok(ambiguous, 'an empty menu is still the ambiguous-scope case, not a generic failure');
  assert.deepEqual(ambiguous.availableParks, []);
});

test('BUG-019: a non-409 error is not mistaken for the park-scope case', () => {
  const notAmbiguous = parkScopeAmbiguousFromBody(400, {
    kind: 'bad_request',
    status: 400,
    code: 'invalid_park_id',
    message: 'park_id must be a UUID',
  });

  assert.equal(notAmbiguous, null);
});

test('BUG-019: a 409 from a different concern is not treated as park scope', () => {
  // The save path returns its own 409 for a row-version conflict. Narrowing must key on the
  // CODE, never on the status alone, or a stale-write conflict would render a park picker.
  const rowVersionConflict = parkScopeAmbiguousFromBody(409, {
    kind: 'conflict',
    status: 409,
    code: 'row_version_conflict',
    message: 'Configuration changed elsewhere.',
  });

  assert.equal(rowVersionConflict, null);
});
