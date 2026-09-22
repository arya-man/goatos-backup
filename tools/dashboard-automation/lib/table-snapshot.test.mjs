// node --test tools/dashboard-automation/lib/table-snapshot.test.mjs
//
// The guard is the feature, so it is attacked here rather than asserted. The snapshot/restore
// contract is exercised against a fake server that behaves like Postgres for the statements this
// engine emits, including one that "restores" a row short, so the proof has something to catch.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import {
  CLONE_MARKER_SCHEMA,
  CONTINUOUSLY_MOVING_PREFIXES,
  CONTINUOUSLY_MOVING_TABLES,
  assertDeclaredTables,
  assertLiveWriteTarget,
  assertWriteTarget,
  classifyWriteTarget,
  createTableSnapshotEngine,
  describeTarget,
  insertableColumns,
  psqlRunner
} from "./table-snapshot.mjs";

const OCI_ENV = {
  GOATOS_WRITE_JOURNEY_TARGET: "oci-clone",
  GOATOS_WRITE_JOURNEY_CLONE_URL: "postgres://postgres@127.0.0.1:5432/goatos",
  GOATOS_STG_READONLY_DATABASE_URL: "postgres://reader@127.0.0.1:5455/goatos",
  GOATOS_OCI_READONLY_DATABASE_URL: "postgres://reader@127.0.0.1:5432/goatos"
};

test("the guard refuses production, however it is dressed up", () => {
  const productionShapes = [
    "postgres://app:pw@10.0.0.9:5432/goatos",
    "postgres://app@34.93.1.2:5432/goatos",
    "postgres://app@goatos-stg-core-db.asia-south1.gcp:5432/goatos",
    "postgres://app@dashboard.mesha.sg:5432/goatos_dashboard_automation",
    "postgres://app@api.goatos.mesha.sg:5432/preview",
    "postgresql://app@/cloudsql/goatos-stg:asia-south1:goatos-stg-core-db"
  ];
  for (const url of productionShapes) {
    const verdict = classifyWriteTarget(url, OCI_ENV);
    assert.equal(verdict.allowed, false, `guard allowed ${url}`);
    assert.throws(() => assertWriteTarget(url, OCI_ENV), /refuse/);
  }
});

test("the guard refuses STG even though the STG proxy is on loopback too", () => {
  const verdict = classifyWriteTarget("postgres://writer@127.0.0.1:5455/goatos", OCI_ENV);
  assert.equal(verdict.allowed, false);
  assert.equal(verdict.kind, "known-read-only");
  assert.match(verdict.reason, /production or STG/);
});

test("the guard refuses a loopback database that is neither disposable nor the declared clone", () => {
  for (const url of ["postgres://p@127.0.0.1:5432/goatos_production", "postgres://p@127.0.0.1:5432/goatos", "postgres://p@localhost:6000/postgres"]) {
    const env = url.includes(":5432/goatos") && !url.includes("production") ? {} : OCI_ENV;
    assert.equal(classifyWriteTarget(url, env).allowed, false, `guard allowed ${url}`);
  }
});

test("the guard allows only the declared clone and disposable databases", () => {
  assert.equal(classifyWriteTarget("postgres://postgres@127.0.0.1:5432/goatos", OCI_ENV).kind, "oci-clone");
  for (const name of ["goatos_dashboard_automation", "goatos_preview_7", "goatos_tmp", "throwaway_goatos"]) {
    assert.equal(classifyWriteTarget(`postgres://p@127.0.0.1:5999/${name}`, {}).allowed, true, name);
  }
});

test("a DSN never appears in a refusal; only host, port and database name", () => {
  const url = "postgres://someone:sup3rsecret@10.0.0.9:5432/goatos";
  assert.equal(describeTarget(url), "10.0.0.9:5432/goatos");
  assert.throws(() => assertWriteTarget(url, OCI_ENV), (error) => !error.message.includes("sup3rsecret"));
});

test("the live probe refuses a managed Cloud SQL instance, a read-only session and a remote server", () => {
  // [database, managed roles, marker schemas, transaction_read_only, server address]
  assert.throws(() => assertLiveWriteTarget(["goatos", "1", "1", "off", "127.0.0.1"]), /managed Cloud SQL/);
  assert.throws(() => assertLiveWriteTarget(["goatos", "0", "1", "on", "127.0.0.1"]), /read-only session/);
  assert.throws(() => assertLiveWriteTarget(["goatos", "0", "1", "off", "10.1.2.3"]), /loopback/);
  assert.throws(() => assertLiveWriteTarget(["goatos", "0", "0", "off", "127.0.0.1"]), /clone marker/);
  assert.throws(() => assertLiveWriteTarget([]), /identity probe/);
  assert.deepEqual(assertLiveWriteTarget(["goatos", "0", "1", "off", "127.0.0.1"]).marked, true);
});

test("a journey must declare its tables, and may never name a moving table", () => {
  assert.throws(() => assertDeclaredTables([], "j"), /declares no writesTables/);
  assert.throws(() => assertDeclaredTables(undefined, "j"), /declares no writesTables/);
  assert.throws(() => assertDeclaredTables(["herd_signal_analytics"], "j"), /analytics and telemetry/);
  assert.throws(() => assertDeclaredTables(["client_telemetry_events"], "j"), /analytics and telemetry/);
  assert.throws(() => assertDeclaredTables(["obligation_instances"], "j"), /never written directly/);
  assert.doesNotThrow(() => assertDeclaredTables(["obligation_instances"], "j", { generated: true }));
  assert.throws(() => assertDeclaredTables(["leadership_tasks; drop table goats"], "j"), /unusable table name/);
  assert.throws(() => assertDeclaredTables(["leadership_tasks", "leadership_tasks"], "j"), /twice/);
  assert.deepEqual(assertDeclaredTables(["sales_deals", "sales_deal_lines"], "j"), ["sales_deals", "sales_deal_lines"]);
});

// ---------------------------------------------------------------------------------------------
// A fake server that answers exactly the statements the engine emits.
// ---------------------------------------------------------------------------------------------
function fakeServer({ tables, loseRowsOnRestore = {}, failRestore = false }) {
  const live = new Map(Object.entries(tables).map(([name, rows]) => [name, rows.map((r) => ({ ...r }))]));
  const snaps = new Map();
  const counters = new Map([...live.keys()].map((name) => [name, 0]));
  const fingerprint = (rows) => rows.length === 0
    ? "empty-table"
    : createHash("md5").update(rows.map((r) => createHash("md5").update(JSON.stringify(r)).digest("hex")).sort().join("")).digest("hex");

  const sql = (text) => {
    const t = text.trim();
    if (/^create schema/i.test(t)) return [];
    if (/^comment on schema/i.test(t)) return [];
    if (/^create table (\S+)\.\"(\S+)\" as table public\.(\w+)$/i.test(t)) {
      const [, , snapName, table] = t.match(/^create table (\S+)\.\"(\S+)\" as table public\.(\w+)$/i);
      snaps.set(snapName, live.get(table).map((r) => ({ ...r })));
      return [];
    }
    if (/^drop table if exists/i.test(t)) return [];
    if (/information_schema\.columns/i.test(t)) {
      const table = t.match(/table_name = '(\w+)'/)[1];
      const sample = (live.get(table) ?? [])[0] ?? {};
      const names = Object.keys(sample).length ? Object.keys(sample) : ["id"];
      return names.map((name) => [name, "NEVER", "NO", null]);
    }
    if (/from public\.(\w+) t\)/i.test(t) && /^select count/i.test(t)) {
      const table = t.match(/from public\.(\w+) t\)/i)[1];
      const rows = live.get(table) ?? [];
      return [[String(rows.length), fingerprint(rows)]];
    }
    if (/pg_stat_user_tables/i.test(t)) return [...counters].sort().map(([name, n]) => [name, String(n)]);
    if (/^begin;/i.test(t)) {
      if (failRestore) throw new Error("database statement failed (exit 3)");
      for (const line of t.split("\n")) {
        const del = line.match(/^delete from public\.(\w+);$/i);
        if (del) { counters.set(del[1], (counters.get(del[1]) ?? 0) + live.get(del[1]).length); live.set(del[1], []); }
        const ins = line.match(/^insert into public\.(\w+)(?: \([^)]*\))?(?: overriding system value)? select (?:\*|[^ ]+(?:, [^ ]+)*) from \S+\."(\S+)";$/i);
        if (ins) {
          const restored = snaps.get(ins[2]).map((r) => ({ ...r }));
          const lose = loseRowsOnRestore[ins[1]] ?? 0;
          const kept = lose > 0 ? restored.slice(0, Math.max(0, restored.length - lose)) : restored;
          live.set(ins[1], kept);
          counters.set(ins[1], (counters.get(ins[1]) ?? 0) + kept.length);
        }
      }
      return [];
    }
    throw new Error(`fake server does not know: ${t.slice(0, 60)}`);
  };
  return {
    sql,
    live,
    /** Simulate the journey writing rows, the way a real UI click would. */
    write(table, row) { live.get(table).push(row); counters.set(table, (counters.get(table) ?? 0) + 1); }
  };
}

test("snapshot captures only the declared tables, and restore puts them back exactly", () => {
  const server = fakeServer({ tables: { leadership_tasks: [{ id: 1, title: "a" }], goats: [{ id: 9 }] } });
  const engine = createTableSnapshotEngine({ sql: server.sql });
  const handle = engine.snapshot(["leadership_tasks"], "create-and-move-a-task");
  assert.deepEqual(handle.tables, ["leadership_tasks"]);
  assert.equal(handle.before[0].rowCount, 1);

  server.write("leadership_tasks", { id: 2, title: "Automation check A1" });
  assert.equal(engine.fingerprint("leadership_tasks").rowCount, 2);

  const restored = engine.restore(handle);
  assert.equal(restored.restored, true);
  const verified = engine.verifyRestore(handle);
  assert.equal(verified.verified, true);
  assert.equal(engine.fingerprint("leadership_tasks").rowCount, 1);
});

test("the restore proof catches a row that was not put back", () => {
  const server = fakeServer({
    tables: { sales_deals: [{ id: 1 }, { id: 2 }, { id: 3 }] },
    loseRowsOnRestore: { sales_deals: 1 }
  });
  const engine = createTableSnapshotEngine({ sql: server.sql });
  const handle = engine.snapshot(["sales_deals"], "record-a-sale");
  server.write("sales_deals", { id: 4 });
  assert.equal(engine.restore(handle).restored, true, "the restore itself reports success");
  const verified = engine.verifyRestore(handle);
  assert.equal(verified.verified, false, "but the proof must not believe it");
  assert.equal(verified.mismatches.length, 1);
  assert.equal(verified.mismatches[0].table, "sales_deals");
  assert.match(verified.mismatches[0].reason, /1 row\(s\) missing/);
});

test("the restore proof catches a row that came back changed", () => {
  const server = fakeServer({ tables: { feed_ration_rates: [{ id: 1, grams: 100 }] } });
  const engine = createTableSnapshotEngine({ sql: server.sql });
  const handle = engine.snapshot(["feed_ration_rates"], "change-a-feed-rate");
  engine.restore(handle);
  // Same row count, different contents: only the fingerprint can see this.
  server.live.get("feed_ration_rates")[0].grams = 250;
  const verified = engine.verifyRestore(handle);
  assert.equal(verified.verified, false);
  assert.match(verified.mismatches[0].reason, /contents changed/);
});

test("a restore that cannot run at all is reported, not swallowed", () => {
  const server = fakeServer({ tables: { sop_versions: [{ id: 1 }] }, failRestore: true });
  const engine = createTableSnapshotEngine({ sql: server.sql });
  const handle = engine.snapshot(["sop_versions"], "publish-an-sop");
  const restored = engine.restore(handle);
  assert.equal(restored.restored, false);
  assert.ok(restored.error);
});

test("a write to a table the journey never declared is detected", () => {
  const server = fakeServer({ tables: { leadership_tasks: [{ id: 1 }], notification_requests: [] } });
  const engine = createTableSnapshotEngine({ sql: server.sql });
  const handle = engine.snapshot(["leadership_tasks"], "create-and-move-a-task");
  assert.deepEqual(engine.undeclaredWrites(handle), []);
  server.write("leadership_tasks", { id: 2 });
  assert.deepEqual(engine.undeclaredWrites(handle), [], "a declared table is not an offender");
  server.write("notification_requests", { id: 7 });
  assert.deepEqual(engine.undeclaredWrites(handle), ["notification_requests"]);
});

test("the clone marker schema name is the one the stamp creates", () => {
  const calls = [];
  const engine = createTableSnapshotEngine({ sql: (text) => { calls.push(text); return []; } });
  engine.stampClone();
  assert.ok(calls.some((text) => text.includes(`create schema if not exists ${CLONE_MARKER_SCHEMA}`)));
});

test("a stale stats read is never mistaken for 'nothing was written'", () => {
  // pg_stat counters are not transactional. The first read here is the pre-journey snapshot the
  // server would legitimately still be serving; only the second read has the write in it.
  const reads = [
    [["leadership_tasks", "0"], ["notification_requests", "0"]],
    [["leadership_tasks", "0"], ["notification_requests", "4"]],
    [["leadership_tasks", "0"], ["notification_requests", "4"]]
  ];
  let i = 0;
  const engine = createTableSnapshotEngine({
    sql: (text) => {
      if (/pg_stat_user_tables/.test(text)) return reads[Math.min(i++, reads.length - 1)];
      if (/^create schema/i.test(text)) return [];
      if (/as table public/i.test(text)) return [];
      if (/^select count/i.test(text)) return [["1", "f"]];
      throw new Error(`unexpected: ${text.slice(0, 40)}`);
    }
  });
  const counters = engine.writeCounters();
  assert.equal(counters.get("notification_requests"), 4, "the engine must wait for two agreeing reads, not trust the first");
});

test("the counter read asks the server to flush its stats and drops the reader's cached snapshot", () => {
  const seen = [];
  const engine = createTableSnapshotEngine({
    sql: (text) => { seen.push(text); return /pg_stat_user_tables/.test(text) ? [["goats", "0"]] : []; }
  });
  engine.writeCounters();
  const statsCalls = seen.filter((text) => /pg_stat_user_tables/.test(text));
  assert.ok(statsCalls.every((text) => text.includes("pg_stat_force_next_flush")), "must force this backend's pending stats out");
  assert.ok(statsCalls.every((text) => text.includes("pg_stat_clear_snapshot")), "must drop the reader's cached stats snapshot");
});

test("a server too old for pg_stat_force_next_flush still gets a cleared snapshot", () => {
  const seen = [];
  const engine = createTableSnapshotEngine({
    sql: (text) => {
      if (text.includes("pg_stat_force_next_flush")) throw new Error("database statement failed (exit 3)");
      seen.push(text);
      return /pg_stat_user_tables/.test(text) ? [["goats", "0"]] : [];
    }
  });
  assert.equal(engine.writeCounters().get("goats"), 0);
  assert.ok(seen.some((text) => text.includes("pg_stat_clear_snapshot")));
});

test("verifyRestore refuses to say 'verified' when an undeclared table was written", () => {
  const server = fakeServer({ tables: { leadership_tasks: [{ id: 1 }], notification_requests: [] } });
  const engine = createTableSnapshotEngine({ sql: server.sql });
  const handle = engine.snapshot(["leadership_tasks"], "create-and-move-a-task");
  server.write("notification_requests", { id: 7 });
  engine.restore(handle);
  const verified = engine.verifyRestore(handle);
  assert.equal(verified.verified, false, "the declared tables came back, but the clone is still dirty");
  assert.deepEqual(verified.undeclaredTables, ["notification_requests"]);
  assert.match(verified.mismatches.at(-1).reason, /changed data it did not declare/);
});

test("a restore failure carries the server's own words for the receipt, but never the statement", () => {
  const runner = psqlRunner("postgres://p@127.0.0.1:5999/goatos_tmp", {
    spawnSync: () => ({ status: 3, stdout: "", stderr: 'ERROR:  update or delete on table "sales_deals" violates foreign key constraint\nSTATEMENT:  delete from public.sales_deals;' }),
    redact: (value) => value
  });
  assert.throws(() => runner("delete from public.sales_deals;"), (error) => {
    assert.match(error.message, /violates foreign key constraint/, "an operator needs the server's reason");
    assert.ok(error.serverDetail, "the detail belongs on the error so the receipt can carry it");
    return true;
  });
});

test("a generated column is left out of the restore, and an ALWAYS identity gets its override", () => {
  // The first live run against the OCI clone failed here: `insert into t select * from snap`
  // cannot write feed_ration_rates.ration_group_key, a generated column.
  const { insertable, overriding } = insertableColumns([
    ["ration_rate_id", "NEVER", "YES", "ALWAYS"],
    ["ration_group_label", "NEVER", "NO", null],
    ["ration_group_key", "ALWAYS", "NO", null],
    ["grams_per_head", "NEVER", "NO", null]
  ]);
  assert.deepEqual(insertable, ["ration_rate_id", "ration_group_label", "grams_per_head"]);
  assert.equal(overriding, true);

  const statements = [];
  const engine = createTableSnapshotEngine({
    sql: (text) => {
      statements.push(text);
      if (/information_schema\.columns/.test(text)) {
        return [["id", "NEVER", "NO", null], ["label", "NEVER", "NO", null], ["label_key", "ALWAYS", "NO", null]];
      }
      if (/pg_stat_user_tables/.test(text)) return [["feed_ration_rates", "0"]];
      if (/^select count/i.test(text)) return [["1", "f"]];
      return [];
    }
  });
  const handle = engine.snapshot(["feed_ration_rates"], "change-a-feed-rate");
  assert.deepEqual(handle.columns.feed_ration_rates.insertable, ["id", "label"]);
  engine.restore(handle);
  const insert = statements.find((text) => text.includes("insert into public.feed_ration_rates"));
  assert.match(insert, /insert into public\.feed_ration_rates \("id", "label"\) select "id", "label" from/);
  assert.ok(!insert.includes("label_key"), "a generated column must never be written back");
});

test("a table this engine could not put back is refused at snapshot time, not discovered at restore time", () => {
  const engine = createTableSnapshotEngine({
    sql: (text) => {
      if (/information_schema\.columns/.test(text)) return [["computed", "ALWAYS", "NO", null]];
      if (/pg_stat_user_tables/.test(text)) return [["t", "0"]];
      if (/^select count/i.test(text)) return [["1", "f"]];
      return [];
    }
  });
  assert.throws(() => engine.snapshot(["sales_deal_lines"], "record-a-sale"), /no columns this engine could put back/);
});

test("the continuously-moving table list stays in step with config.json", () => {
  // config.json businessDataParity.latestFullParityReceiptExcludedPatterns is the repo's own
  // statement about which tables move on their own. If it changes, this list must change too.
  const config = JSON.parse(readFileSync(new URL("../config.json", import.meta.url), "utf8"));
  const excluded = config.businessDataParity.latestFullParityReceiptExcludedPatterns ?? [];
  const named = excluded.filter((p) => p.startsWith("public.") && !p.includes("*")).map((p) => p.replace("public.", ""));
  const prefixes = excluded.filter((p) => p.startsWith("public.") && p.endsWith("*")).map((p) => p.replace("public.", "").replace("*", ""));
  assert.deepEqual([...CONTINUOUSLY_MOVING_TABLES].sort(), [...named].sort());
  assert.deepEqual([...CONTINUOUSLY_MOVING_PREFIXES].sort(), [...prefixes].sort());
});

test("a continuously-moving table cannot be declared as a table a journey writes", () => {
  for (const table of [...CONTINUOUSLY_MOVING_TABLES, "herd_signal_packets_p2026_08_30"]) {
    assert.throws(() => assertDeclaredTables([table], "j"), /continuously moving|analytics and telemetry/, table);
    // It may still be restored as a row the journey's own publish generated.
    assert.doesNotThrow(() => assertDeclaredTables([table], "j", { generated: true }), table);
  }
});

test("a hand-rolled or foreign snapshot handle never reaches SQL", () => {
  const engine = createTableSnapshotEngine({ sql: () => { throw new Error("SQL must not have been reached"); } });
  for (const bad of [
    undefined,
    { id: 'x"; drop table goats; --', schema: "goatos_write_journey_snapshot", tables: ["goats"] },
    { id: "ok_handle", schema: "somewhere_else", tables: ["goats"] },
    { id: "ok_handle", schema: "goatos_write_journey_snapshot", tables: ["goats; drop table x"] },
    { id: "ok_handle", schema: "goatos_write_journey_snapshot", tables: [] }
  ]) {
    assert.throws(() => engine.restore(bad), /handle|declares/);
    assert.throws(() => engine.cleanup(bad), /handle|declares/);
    assert.throws(() => engine.undeclaredWrites(bad), /handle|declares/);
  }
  assert.throws(() => engine.fingerprint("goats; drop table x"), /unusable table name/);
});
