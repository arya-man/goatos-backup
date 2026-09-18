// Regression: the browser-push adapter must send the tenant under the header the backend reads.
//
// browser-push-server.ts is a raw fetch (the generated client has no request methods), so its
// headers are hand-assembled. It used to send `X-Tenant-Id`, which nothing in the backend reads:
// bearer auth resolves the tenant from the token claim or `X-GoatOS-Tenant-ID` only, and a
// Firebase ID token carries no Goat OS tenant claim. Every register/list call therefore came
// back 401 `missing_tenant_context` ("tenant context is required"), which the prompt renders as
// an `error` state -- i.e. the bell kept offering "Enable notifications" to a browser that had
// already granted permission, with that sentence underneath.
//
// The feed and mark-read calls go through `@goatos/api-client`, which sets the constant, so
// this test pins the raw adapter to that same constant rather than a hand-typed twin.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// Relative on purpose: this file is read by the test runner without the workspace link resolved.
import { TENANT_CONTEXT_HEADER } from '../../../../packages/api-client/src/constants.js';

const source = readFileSync(new URL('./browser-push-server.ts', import.meta.url), 'utf8');

test('browser-push adapter sends the tenant under the header the backend reads', () => {
  assert.equal(TENANT_CONTEXT_HEADER, 'X-GoatOS-Tenant-ID');
  assert.match(source, /headers\[TENANT_CONTEXT_HEADER\] = config\.data\.tenantId/);
  assert.doesNotMatch(source, /X-Tenant-Id/i);
});
