import assert from "node:assert/strict";
import test from "node:test";
import { createGoatOSClient } from "./index.ts";

test("requestWithResponse exposes ETag metadata and accepts conditional 304", async () => {
  const responses = [
    new Response(JSON.stringify({ value: 1 }), { status: 200, headers: { "Content-Type": "application/json", ETag: 'W/"one"' } }),
    new Response(null, { status: 304, headers: { ETag: 'W/"one"' } }),
  ];
  const client = createGoatOSClient({ baseUrl: "http://api", fetchImpl: async () => responses.shift() });

  const first = await client.requestWithResponse("/bootstrap", {});
  assert.deepEqual(first.data, { value: 1 });
  assert.equal(first.response.headers.get("ETag"), 'W/"one"');

  const second = await client.requestWithResponse("/bootstrap", { headers: { "If-None-Match": 'W/"one"' } });
  assert.equal(second.response.status, 304);
  assert.equal(second.data, null);
});
