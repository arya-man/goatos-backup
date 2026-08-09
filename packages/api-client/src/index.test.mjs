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

test("an array query value is sent as a REPEATED parameter, never comma-joined", async () => {
  // The failure this guards is silent: `String(["Hybrid","COFS"])` is `"Hybrid,COFS"`, which a
  // backend reading r.URL.Query()["feed_item"] receives as ONE item literally named "Hybrid,COFS".
  // It matches nothing, returns an empty page, and looks exactly like "nothing is configured".
  let seen;
  const client = createGoatOSClient({
    baseUrl: "http://api",
    fetchImpl: async (url) => {
      seen = new URL(url);
      return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
    },
  });

  await client.request("/feed-config/ration-rates", {
    query: { park_id: "p", feed_item: ["Hybrid", "COFS", "Dry Maize"], grams_op: "gt", grams_value: "0" },
  });

  assert.deepEqual(seen.searchParams.getAll("feed_item"), ["Hybrid", "COFS", "Dry Maize"]);
  assert.equal(seen.searchParams.get("park_id"), "p");
  assert.equal(seen.searchParams.get("grams_op"), "gt");
});

test("an empty array query value sends no parameter at all", async () => {
  // Distinct from "no filter" only if nothing is emitted: an empty `feed_item=` would be read as a
  // filter for an item whose name is the empty string.
  let seen;
  const client = createGoatOSClient({
    baseUrl: "http://api",
    fetchImpl: async (url) => {
      seen = new URL(url);
      return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
    },
  });

  await client.request("/feed-config/ration-rates", { query: { park_id: "p", feed_item: [] } });

  assert.equal(seen.searchParams.has("feed_item"), false);
});
