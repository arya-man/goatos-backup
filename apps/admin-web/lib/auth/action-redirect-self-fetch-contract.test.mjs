import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

// When a Server Action calls redirect(), Next self-fetches the redirect target (from
// __NEXT_PRIVATE_ORIGIN, or the public host when unset) to stream the destination page back in the
// action response. Node's fetch drops the Cookie header when it follows a cross-origin redirect, so
// any hop that re-routes the self-fetch (LB http->https 301, canonical-host 308) strips the session
// cookie and the auth middleware bounces the action to /login — the 2026-08-18 STG incident where
// every verifier approve and approvals decision flashed a logout/login. Two halves keep that closed:
// the runtime must self-fetch the container directly, and the middleware must serve loopback-host
// requests in place instead of re-canonicalizing them back out through the load balancer.

const here = new URL(".", import.meta.url).pathname;
const proxySource = readFileSync(join(here, "../../proxy.ts"), "utf8");
const stgServicesSource = readFileSync(
  join(here, "../../../../infra/envs/stg/cloud_run_services.tf"),
  "utf8",
);

test("canonical-host middleware serves loopback self-fetch requests in place", () => {
  assert.match(proxySource, /isLoopbackHost\(requestHost\)/);
  // The exemption must live inside canonicalHostRedirect, before the redirect is composed.
  const fn = proxySource.slice(
    proxySource.indexOf("function canonicalHostRedirect"),
  );
  const body = fn.slice(0, fn.indexOf("function ", 10));
  const loopbackCheck = body.indexOf("isLoopbackHost(requestHost)");
  const redirectCompose = body.indexOf(
    "const redirectUrl = request.nextUrl.clone()",
  );
  assert.notEqual(loopbackCheck, -1);
  assert.notEqual(redirectCompose, -1);
  assert.ok(loopbackCheck < redirectCompose);
  // Loopback shapes the container can self-address as.
  assert.match(proxySource, /127\.0\.0\.1/);
  assert.match(proxySource, /localhost/);
});

test("STG admin-web self-fetches its own container, not the public load balancer", () => {
  assert.match(stgServicesSource, /__NEXT_PRIVATE_ORIGIN/);
  assert.match(stgServicesSource, /http:\/\/127\.0\.0\.1:8080/);
  // The env must sit on the admin-web service resource, not merely anywhere in the file.
  const adminWeb = stgServicesSource.slice(
    stgServicesSource.indexOf(
      'resource "google_cloud_run_v2_service" "admin_web"',
    ),
  );
  const nextResource = adminWeb.indexOf('resource "', 10);
  const adminWebBlock =
    nextResource === -1 ? adminWeb : adminWeb.slice(0, nextResource);
  assert.match(adminWebBlock, /__NEXT_PRIVATE_ORIGIN/);
});
