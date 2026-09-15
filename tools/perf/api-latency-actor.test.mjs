import assert from "node:assert/strict";
import test from "node:test";
import { readApiActor, assertNoActorHeaderOverrides } from "./api-latency-actor.mjs";
const jwt = (claims) => `header.${Buffer.from(JSON.stringify(claims)).toString("base64url")}.signature`;
const base = { baseUrl: "http://localhost:8000", tenantId: "tenant-a", bearerToken: jwt({ sub: "external-subject", tenant_id: "tenant-a" }) };

test("records server actor rather than JWT subject and forwards exact benchmark credentials", async () => {
  let request;
  const actor = await readApiActor({ ...base, cookie: "session=secret", fetchImpl: async (...args) => {
    request = args;
    return Response.json({ actor_id: "internal-user", operator_profile: { secret: "omitted" } });
  } });
  assert.deepEqual(actor, { user_id: "internal-user", tenant_id: "tenant-a" });
  assert.equal(request[0], "http://localhost:8000/app/me");
  assert.equal(request[1].headers.Authorization, `Bearer ${base.bearerToken}`);
  assert.equal(request[1].headers.Cookie, "session=secret");
  assert.equal(request[1].headers["X-GoatOS-Tenant-ID"], "tenant-a");
  assert.equal(request[1].redirect, "error");
});

test("rejects unauthenticated or missing server identity without leaking response body", async () => {
  for (const response of [new Response("secret", { status: 401 }), Response.json({}), Response.json({ actor_id: " " })]) {
    await assert.rejects(readApiActor({ ...base, fetchImpl: async () => response }), (err) => /actor/.test(err.message) && !err.message.includes("secret"));
  }
});

test("rejects unverifiable cookie-only identities and overriding JWT tenants before requests", async () => {
  for (const bearerToken of ["", "opaque", jwt({ tenant_id: "other" })]) {
    await assert.rejects(readApiActor({ ...base, bearerToken, cookie: "secret", fetchImpl: () => { throw new Error("must not fetch"); } }), /JWT|tenant/);
  }
});

test("supports header tenant fallback when verified token has no tenant claim", async () => {
  assert.deepEqual(await readApiActor({ ...base, bearerToken: jwt({ sub: "firebase-user" }), fetchImpl: async () => Response.json({ actor_id: "internal-user" }) }), { user_id: "internal-user", tenant_id: "tenant-a" });
});

test("rejects case-insensitive manifest authentication overrides", () => {
  for (const name of ["authorization", "Authorization", "COOKIE", "x-goatos-tenant-id", "X-GoatOS-Actor-ID"]) {
    assert.throws(() => assertNoActorHeaderOverrides({ [name]: "other" }), /actor context/);
  }
  assert.doesNotThrow(() => assertNoActorHeaderOverrides({ "Cache-Control": "no-cache" }));
});
