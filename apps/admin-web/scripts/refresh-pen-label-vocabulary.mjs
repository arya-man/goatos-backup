#!/usr/bin/env node
// Refreshes scripts/lib/pen-label-vocabulary.json -- the farm's real (shed, partition)
// pairs that lane 1's pen-label checks judge rendered labels against.
//
// WHY A GENERATOR AND NOT A REGEX
// The display helper's own doc comment used "Yashoda" as its example of an UNPARTITIONED
// shed rendering as a bare name. Yashoda has ten pens. A rule written from that comment
// would have called "Yashoda 1" a bug and let a bare "Yashoda" through -- backwards on
// both counts. The farm's data is the only contract that does not rot, so the checker
// reads it and this script is how it gets refreshed.
//
// READ-ONLY. One SELECT, inside a READ ONLY transaction. It never writes.
//
//   node apps/admin-web/scripts/refresh-pen-label-vocabulary.mjs            # from the DB
//   node apps/admin-web/scripts/refresh-pen-label-vocabulary.mjs --from-api # from the API
//   node apps/admin-web/scripts/refresh-pen-label-vocabulary.mjs --check    # CI: is it stale?
//
// DB path  : GOATOS_STG_READONLY_DATABASE_URL (the read-only replica lane 2 already uses).
// API path : GOATOS_API_BASE_URL + GOATOS_BEARER_TOKEN + GOATOS_TENANT_ID, GET /feed-config/pens.
//            The API only serves ACTIVE partitions, so it cannot see retired pens -- prefer
//            the DB path and use --from-api only as a cross-check.
//
// WHAT GOES IN `labels`
// The UNION across parks and across active AND retired partitions:
//   - Shed names repeat across Coimbatore and Channapatna with different label sets
//     (Yashoda is 1-4 in one park, 1-10 in the other). A screen can show either park.
//   - A retired pen still appears on historical screens.
// A label valid anywhere is valid. This checker reports MALFORMED labels; an unexpected
// but well-formed one is not a defect and must never be treated as one.

import { readFileSync, writeFileSync } from "node:fs";

const OUT = new URL("./lib/pen-label-vocabulary.json", import.meta.url);
const args = new Set(process.argv.slice(2));

// locations holds both the partitioned parent sheds and the legacy per-pen shed rows;
// shed_partitions is the catalogue that superseded the latter. Only the join matters here.
const SQL = `
SELECT s.name AS shed, p.partition_label AS label
FROM locations s
JOIN shed_partitions p ON p.tenant_id = s.tenant_id AND p.shed_id = s.location_id
WHERE s.tenant_id = $1 AND s.location_type = 'shed'
ORDER BY s.name, p.normalized_label
`;

function shape(pairs) {
  const byShed = new Map();
  for (const { shed, label } of pairs) {
    const name = String(shed ?? "").replace(/\s+/g, " ").trim();
    const value = String(label ?? "").replace(/\s+/g, " ").trim();
    // 'whole' is a comparison sentinel, never a partition. shed_partitions forbids it by
    // check constraint; drop it defensively so it can never reach the valid set.
    if (!name || !value || value.toLowerCase() === "whole") continue;
    if (!byShed.has(name)) byShed.set(name, new Set());
    byShed.get(name).add(value);
  }
  return [...byShed.entries()]
    .map(([name, labels]) => {
      const sorted = [...labels].sort((a, b) => {
        const na = Number(a.replace(/\D/g, "")), nb = Number(b.replace(/\D/g, ""));
        return na - nb || a.localeCompare(b);
      });
      return {
        name,
        // A shed's convention follows its own stored labels, never a guess from its name.
        convention: sorted.every((l) => /^\d+$/.test(l)) ? "numeric" : "worded",
        labels: sorted,
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

async function fromDatabase() {
  const dsn = process.env.GOATOS_STG_READONLY_DATABASE_URL;
  if (!dsn) throw new Error("GOATOS_STG_READONLY_DATABASE_URL is not set (read-only replica DSN)");
  const tenant = process.env.GOATOS_TENANT_ID;
  if (!tenant) throw new Error("GOATOS_TENANT_ID is not set");
  const { default: pg } = await import("pg");
  const client = new pg.Client({ connectionString: dsn });
  await client.connect();
  try {
    await client.query("BEGIN READ ONLY");
    const { rows } = await client.query(SQL, [tenant]);
    await client.query("ROLLBACK");
    return { pairs: rows, origin: "shed_partitions joined to locations on the read-only replica" };
  } finally {
    await client.end();
  }
}

async function fromApi() {
  const base = process.env.GOATOS_API_BASE_URL;
  const token = process.env.GOATOS_BEARER_TOKEN;
  const tenant = process.env.GOATOS_TENANT_ID;
  if (!base || !token || !tenant) throw new Error("GOATOS_API_BASE_URL, GOATOS_BEARER_TOKEN and GOATOS_TENANT_ID are all required");
  const res = await fetch(`${base.replace(/\/$/, "")}/feed-config/pens?limit=500&offset=0`, {
    headers: { authorization: `Bearer ${token}`, "x-goatos-tenant-id": tenant },
  });
  // /version answers 200 without a token, so never use it to prove auth. This endpoint does.
  if (res.status === 401 || res.status === 403) throw new Error(`not authenticated (${res.status}) -- mint a fresh token`);
  if (!res.ok) throw new Error(`GET /feed-config/pens failed: ${res.status}`);
  const body = await res.json();
  const pairs = (body.items ?? []).map((item) => ({ shed: item.shed_name, label: item.partition_label }));
  return { pairs, origin: "GET /feed-config/pens (ACTIVE partitions only -- retired pens are invisible here)" };
}

const { pairs, origin } = args.has("--from-api") ? await fromApi() : await fromDatabase();
const sheds = shape(pairs);
if (sheds.length === 0) throw new Error("refused to write an empty vocabulary: an empty one silently disables the pen-label checks");

const previous = JSON.parse(readFileSync(OUT, "utf8"));
const next = { ...previous, generatedOn: new Date().toISOString().slice(0, 10), source: { ...previous.source, query: origin }, sheds };
const serialised = JSON.stringify(next, null, 2) + "\n";

if (args.has("--check")) {
  const current = readFileSync(OUT, "utf8");
  const same = JSON.stringify(JSON.parse(current).sheds) === JSON.stringify(sheds);
  if (!same) {
    console.error("pen-label vocabulary is STALE: the farm's pens no longer match pen-label-vocabulary.json.");
    console.error("Re-run: node apps/admin-web/scripts/refresh-pen-label-vocabulary.mjs");
    process.exit(1);
  }
  console.log(`pen-label vocabulary: up to date (${sheds.length} sheds, ${sheds.reduce((a, s) => a + s.labels.length, 0)} pens)`);
  process.exit(0);
}

writeFileSync(OUT, serialised);
console.log(`pen-label vocabulary: wrote ${sheds.length} sheds, ${sheds.reduce((a, s) => a + s.labels.length, 0)} pens, from ${origin}`);
