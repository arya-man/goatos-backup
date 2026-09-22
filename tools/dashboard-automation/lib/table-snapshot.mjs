// Lane 4 — per-journey table snapshot / restore engine for write-path journeys.
//
// Ravi's rule: a write journey refreshes and restores ONLY the tables it actually writes.
// There is no wholesale nightly refresh of a big table list. Each journey declares
// `writesTables`; this engine snapshots exactly those, and after the journey restores them
// and PROVES the restore by comparing row counts and a content fingerprint against the
// snapshot. A journey that writes a table it did not declare is a failure, not a warning.
//
// The guard is the feature. Four independent checks must all pass before a single row is
// touched, so no combination of a wrong env var, a stale tunnel or a copy-pasted DSN can
// point this lane at production or STG:
//   1. URL shape      — loopback host only; a remote host is refused outright.
//   2. Known-read-only — the STG read-only proxy / any configured production or STG DSN is
//                        refused even though it also listens on 127.0.0.1.
//   3. Naming          — the database must be a disposable automation/preview database
//                        (`dashboard[_-]automation|preview|tmp|throwaway`, the same rule
//                        `run.mjs: assertPostgresIntegrationConfigured` already enforces), or
//                        the explicitly declared OCI clone.
//   4. Live identity   — the server itself is interrogated: a managed Cloud SQL / AlloyDB
//                        instance (production and STG both are) is refused, a read-only
//                        session is refused, a non-loopback server address is refused, and the
//                        database must carry the one-time clone marker schema that only a
//                        writable throwaway clone can ever have been stamped with.
//
// Nothing here ever touches analytics, telemetry or obligations tables: parity is never a
// gate and those tables move continuously, so a journey that names one is refused.

export const DISPOSABLE_DB_NAME_PATTERN = /dashboard[_-]automation|preview|tmp|throwaway/i;
export const CLONE_MARKER_SCHEMA = "goatos_write_journey_clone";
export const SNAPSHOT_SCHEMA = "goatos_write_journey_snapshot";
// Parity is never a gate. Analytics and telemetry tables move continuously and are meaningless to
// a write journey, so this lane never snapshots, restores or asserts on one, in any list.
export const NEVER_TOUCH_TABLE_PATTERN = /(^|_)(analytic|analytics|telemetry)(s?_|s?$)/i;
// Obligations are never REFRESHED from STG (this lane has no STG refresh path at all, by design).
// They may still appear in a journey's `generatedTables`: publishing a plan version generates the
// work it implies, and leaving those generated rows behind would poison the clone. So they are
// snapshotted and restored with the rest of what the journey touched -- and never declared as a
// table the journey writes directly.
export const NEVER_DECLARE_AS_WRITTEN_PATTERN = /(^|_)obligation(s?_|s?$)/i;
// The tables this repo already treats as continuously moving:
// config.json businessDataParity.latestFullParityReceiptExcludedPatterns. A journey may not
// declare one as a table it WRITES -- it would be claiming ownership of rows that move on their
// own -- though it may still list one under generatedTables when its own publish produces them.
// `table-snapshot.test.mjs` fails if this list drifts from config.json.
export const CONTINUOUSLY_MOVING_TABLES = ["audit_log", "domain_event_processed_events", "outbox_messages"];
export const CONTINUOUSLY_MOVING_PREFIXES = ["herd_signal_"];
export const MANAGED_INSTANCE_ROLES = ["cloudsqlsuperuser", "cloudsqladmin", "alloydbsuperuser", "alloydbadmin"];
const LOOPBACK_HOSTS = new Set(["127.0.0.1", "::1", "localhost", "[::1]"]);
const IDENTIFIER = /^[a-z_][a-z0-9_]*$/;

function parse(databaseUrl) {
  try {
    const url = new URL(String(databaseUrl));
    if (!/^postgres(ql)?:$/.test(url.protocol)) return null;
    return {
      host: url.hostname.toLowerCase(),
      port: url.port || "5432",
      database: decodeURIComponent(url.pathname.replace(/^\//, ""))
    };
  } catch {
    return null;
  }
}

/** Never put a DSN in a message: only ever the parts that cannot be a credential. */
export function describeTarget(databaseUrl) {
  const parsed = parse(databaseUrl);
  if (!parsed) return "<unparseable database URL>";
  return `${parsed.host}:${parsed.port}/${parsed.database}`;
}

/**
 * Pure URL-shape classification. `env` supplies the DSNs this lane must refuse even when they
 * are loopback (the STG read-only proxy is on 127.0.0.1:5455 on the OCI box).
 * @returns {{allowed: boolean, kind: string, reason: string｜null, target: string}}
 */
export function classifyWriteTarget(databaseUrl, env = process.env) {
  const target = describeTarget(databaseUrl);
  const parsed = parse(databaseUrl);
  if (!parsed) return { allowed: false, kind: "unparseable", reason: "not a postgres:// URL", target };
  if (!LOOPBACK_HOSTS.has(parsed.host)) {
    return { allowed: false, kind: "remote", reason: `refusing a non-loopback database host (${parsed.host})`, target };
  }
  for (const [name, value] of Object.entries(env ?? {})) {
    if (!/DATABASE_URL$/.test(name) || !value) continue;
    // Only DSNs this box declares as STG or production. A read-only role on the clone itself
    // points at the same host:port as the clone and must not lock the clone out.
    if (!/STG|STAGING|PROD|PRODUCTION/i.test(name)) continue;
    const other = parse(value);
    if (other && other.host === parsed.host && other.port === parsed.port) {
      return { allowed: false, kind: "known-read-only", reason: `refusing the database this box declares as production or STG (${name})`, target };
    }
  }
  if (DISPOSABLE_DB_NAME_PATTERN.test(parsed.database)) {
    return { allowed: true, kind: "disposable-preview", reason: null, target };
  }
  const declared = parse(env?.GOATOS_WRITE_JOURNEY_CLONE_URL ?? "");
  if (env?.GOATOS_WRITE_JOURNEY_TARGET === "oci-clone" && declared
    && declared.host === parsed.host && declared.port === parsed.port && declared.database === parsed.database) {
    return { allowed: true, kind: "oci-clone", reason: null, target };
  }
  return {
    allowed: false,
    kind: "unknown",
    reason: "database is neither a disposable automation/preview database nor the declared OCI clone",
    target
  };
}

export function assertWriteTarget(databaseUrl, env = process.env) {
  const verdict = classifyWriteTarget(databaseUrl, env);
  if (!verdict.allowed) throw new Error(`write journeys refuse ${verdict.target}: ${verdict.reason}`);
  return verdict;
}

/** The catalogue contract: a journey must declare its tables, and may never name a moving table. */
export function assertDeclaredTables(tables, journeyName = "journey", { generated = false } = {}) {
  if (!Array.isArray(tables) || tables.length === 0) {
    throw new Error(`${journeyName} declares no writesTables; a write journey that does not say what it writes cannot run`);
  }
  const seen = new Set();
  for (const table of tables) {
    if (typeof table !== "string" || !IDENTIFIER.test(table)) {
      throw new Error(`${journeyName} declares an unusable table name ${JSON.stringify(table)}`);
    }
    if (NEVER_TOUCH_TABLE_PATTERN.test(table)) {
      throw new Error(`${journeyName} declares ${table}; analytics and telemetry tables are never touched by this lane`);
    }
    if (!generated && NEVER_DECLARE_AS_WRITTEN_PATTERN.test(table)) {
      throw new Error(`${journeyName} declares ${table} as a table it writes; obligations are only ever restored as rows a journey's own publish generated, never written directly and never refreshed from STG`);
    }
    if (!generated && (CONTINUOUSLY_MOVING_TABLES.includes(table) || CONTINUOUSLY_MOVING_PREFIXES.some((prefix) => table.startsWith(prefix)))) {
      throw new Error(`${journeyName} declares ${table} as a table it writes; this repo already treats it as continuously moving, so a journey can never own its rows`);
    }
    if (seen.has(table)) throw new Error(`${journeyName} declares ${table} twice`);
    seen.add(table);
  }
  return [...seen];
}

// `insert into t select * from snap` is wrong on any real schema: a GENERATED column refuses a
// non-DEFAULT value, and an identity column declared ALWAYS needs OVERRIDING SYSTEM VALUE. This
// was not theory -- the first live run against the OCI clone failed exactly here on
// feed_ration_rates.ration_group_key, and the restore proof is what caught it.
export function columnsSql(table) {
  return `select column_name, is_generated, is_identity, identity_generation
from information_schema.columns
where table_schema = 'public' and table_name = '${table}'
order by ordinal_position`;
}

/** @returns {{ insertable: string[], overriding: boolean }} */
export function insertableColumns(rows) {
  const insertable = [];
  let overriding = false;
  for (const [name, isGenerated, isIdentity, identityGeneration] of rows ?? []) {
    if (String(isGenerated).toUpperCase() !== "NEVER") continue;
    if (String(isIdentity).toUpperCase() === "YES" && String(identityGeneration).toUpperCase() === "ALWAYS") overriding = true;
    insertable.push(name);
  }
  return { insertable, overriding };
}

export function fingerprintSql(table) {
  return `select count(*)::text, coalesce(md5(string_agg(h, '' order by h)), 'empty-table')
from (select md5(t.*::text) as h from public.${table} t) s`;
}

// pg_stat_* counters are NOT transactional: a backend accumulates them locally and flushes on a
// timer, and a reading session serves a cached snapshot until it is cleared. Reading them straight
// after a journey can legitimately return the pre-journey numbers, which would make an undeclared
// write invisible. So: force this backend's pending stats out, drop the reader's cached snapshot,
// and (in `writeCounters`) only believe a reading that two consecutive polls agree on.
export function writeCounterSql() {
  return `select pg_stat_force_next_flush();
select pg_stat_clear_snapshot();
select relname, (coalesce(n_tup_ins,0) + coalesce(n_tup_upd,0) + coalesce(n_tup_del,0))::text
from pg_stat_user_tables where schemaname = 'public' order by relname`;
}

// Servers before PostgreSQL 15 have no pg_stat_force_next_flush(); fall back to clearing the
// reader's snapshot only, and let the poll-until-stable loop do the rest.
export function legacyWriteCounterSql() {
  return `select pg_stat_clear_snapshot();
select relname, (coalesce(n_tup_ins,0) + coalesce(n_tup_upd,0) + coalesce(n_tup_del,0))::text
from pg_stat_user_tables where schemaname = 'public' order by relname`;
}

export function liveIdentitySql() {
  return `select current_database(),
  (select count(*)::text from pg_roles where rolname in (${MANAGED_INSTANCE_ROLES.map((r) => `'${r}'`).join(", ")})),
  (select count(*)::text from pg_namespace where nspname = '${CLONE_MARKER_SCHEMA}'),
  current_setting('transaction_read_only'),
  coalesce(host(inet_server_addr()), 'unix-socket')`;
}

/**
 * Interrogates the server itself. Production and STG are Cloud SQL instances and always carry a
 * managed superuser role; the OCI clone is a plain container Postgres and never does.
 */
export function assertLiveWriteTarget(identityRow, { requireMarker = true } = {}) {
  const [database, managedRoles, markerSchemas, readOnly, serverAddress] = identityRow ?? [];
  if (!database) throw new Error("write journeys refuse a database that did not answer the identity probe");
  if (Number(managedRoles ?? 0) > 0) {
    throw new Error("write journeys refuse a managed Cloud SQL/AlloyDB instance; production and STG are read-only for this automation");
  }
  if (String(readOnly) === "on") {
    throw new Error("write journeys refuse a read-only session; this is a replica or a read-only proxy, not the writable clone");
  }
  if (!["127.0.0.1", "::1", "unix-socket"].includes(String(serverAddress))) {
    throw new Error(`write journeys refuse a server reachable at ${serverAddress}; only a loopback clone may be written`);
  }
  if (requireMarker && Number(markerSchemas ?? 0) === 0) {
    throw new Error(`write journeys refuse a database with no clone marker; stamp the throwaway clone once with --stamp-clone before running`);
  }
  return { database, serverAddress, marked: Number(markerSchemas ?? 0) > 0 };
}

/** Every entry point re-validates its handle: a hand-rolled or reused handle must not reach SQL. */
function assertHandle(handle, schema) {
  if (!handle || typeof handle !== "object") throw new Error("snapshot handle is missing");
  if (typeof handle.id !== "string" || !/^[a-z0-9_]{3,64}$/.test(handle.id)) throw new Error("snapshot handle has an unusable id");
  if (handle.schema !== schema) throw new Error("snapshot handle belongs to a different snapshot schema");
  assertDeclaredTables(handle.tables, `snapshot ${handle.id}`, { generated: true });
  return handle;
}

function assertTableName(table) {
  if (typeof table !== "string" || !IDENTIFIER.test(table)) throw new Error(`unusable table name ${JSON.stringify(table)}`);
  return table;
}

function handleIdFor(journeyName) {
  const slug = String(journeyName).toLowerCase().replaceAll(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "").slice(0, 24);
  const salt = Math.random().toString(36).slice(2, 8);
  return `${slug || "journey"}_${salt}`;
}

/**
 * @param {{ sql: (text: string) => string[][], requireMarker?: boolean }} deps
 *   `sql` runs one statement batch and returns tab-split rows. Injectable so the engine is unit
 *   tested against an in-memory fake and run for real through psql.
 */
export function createTableSnapshotEngine({ sql, requireMarker = true, schema = SNAPSHOT_SCHEMA }) {
  if (typeof sql !== "function") throw new Error("createTableSnapshotEngine needs a sql runner");

  function snapshotTableName(handleId, table) {
    return `${schema}."${handleId}__${table}"`;
  }

  return {
    schema,

    /** Proves the connected server is the writable clone (or a disposable preview database). */
    assertLive() {
      return assertLiveWriteTarget(sql(liveIdentitySql())[0], { requireMarker });
    },

    stampClone() {
      sql(`create schema if not exists ${CLONE_MARKER_SCHEMA}`);
      sql(`comment on schema ${CLONE_MARKER_SCHEMA} is 'Goat OS dashboard automation lane 4: this database is a disposable writable clone. Never stamp production or STG.'`);
      return { stamped: true };
    },

    /**
     * Two consecutive agreeing reads, with the stats flushed and the reader's snapshot cleared in
     * between. A stale first read can never be mistaken for "nothing was written".
     */
    writeCounters({ attempts = 4 } = {}) {
      const read = () => {
        let rows;
        try {
          rows = sql(writeCounterSql());
        } catch {
          rows = sql(legacyWriteCounterSql());
        }
        // The two `select pg_stat_*()` statements answer one blank-ish row each; keep the pairs.
        return new Map(rows.filter((row) => row.length === 2 && row[1] !== "").map(([table, count]) => [table, Number(count)]));
      };
      let previous = read();
      for (let i = 1; i < attempts; i += 1) {
        const next = read();
        if (next.size === previous.size && [...next].every(([table, count]) => previous.get(table) === count)) return next;
        previous = next;
      }
      return previous;
    },

    fingerprint(table) {
      const [count, digest] = sql(fingerprintSql(assertTableName(table)))[0] ?? [];
      return { table, rowCount: Number(count ?? -1), fingerprint: String(digest ?? "") };
    },

    /** Snapshot exactly the declared tables, and nothing else. */
    snapshot(tables, journeyName = "journey") {
      // The snapshot list is writesTables + generatedTables; the stricter writesTables-only rule is
      // enforced by the catalogue validator before a journey is ever selected to run.
      const declared = assertDeclaredTables(tables, journeyName, { generated: true });
      const id = handleIdFor(journeyName);
      sql(`create schema if not exists ${schema}`);
      const captured = [];
      const columns = {};
      for (const table of declared) {
        sql(`create table ${snapshotTableName(id, table)} as table public.${table}`);
        columns[table] = insertableColumns(sql(columnsSql(table)));
        if (columns[table].insertable.length === 0) {
          throw new Error(`${journeyName}: ${table} has no columns this engine could put back, so it must not be snapshotted`);
        }
        captured.push(this.fingerprint(table));
      }
      return {
        id,
        journey: journeyName,
        schema,
        tables: declared,
        columns,
        capturedAt: new Date().toISOString(),
        before: captured,
        writeBaseline: [...this.writeCounters()]
      };
    },

    /**
     * Tables written since the snapshot that the journey never declared. `pg_stat_user_tables`
     * counts every insert/update/delete the server actually performed, so a journey cannot hide a
     * side effect behind an API call.
     */
    undeclaredWrites(handle) {
      assertHandle(handle, schema);
      const baseline = new Map(handle.writeBaseline ?? []);
      const now = this.writeCounters();
      const declared = new Set(handle.tables);
      const offenders = [];
      for (const [table, count] of now) {
        if (declared.has(table)) continue;
        if (count > (baseline.get(table) ?? 0)) offenders.push(table);
      }
      return offenders.sort();
    },

    /**
     * Put every declared table back exactly as it was. Foreign keys between the declared tables
     * are handled by deleting all of them before inserting any of them, inside one transaction.
     */
    restore(handle) {
      assertHandle(handle, schema);
      const deletes = [...handle.tables].reverse().map((t) => `delete from public.${t};`);
      const inserts = handle.tables.map((t) => {
        const { insertable, overriding } = handle.columns?.[t] ?? { insertable: null, overriding: false };
        if (!insertable?.length) return `insert into public.${t} select * from ${snapshotTableName(handle.id, t)};`;
        const list = insertable.map((c) => `"${c}"`).join(", ");
        return `insert into public.${t} (${list})${overriding ? " overriding system value" : ""} select ${list} from ${snapshotTableName(handle.id, t)};`;
      });
      const body = [...deletes, ...inserts].join("\n");
      const attempts = [
        { strategy: "replica-role", text: `begin;\nset local session_replication_role = replica;\n${body}\ncommit;` },
        { strategy: "deferred-constraints", text: `begin;\nset constraints all deferred;\n${body}\ncommit;` }
      ];
      let lastError = null;
      for (const attempt of attempts) {
        try {
          sql(attempt.text);
          return { restored: true, strategy: attempt.strategy, restoredAt: new Date().toISOString() };
        } catch (error) {
          lastError = error;
        }
      }
      return { restored: false, strategy: null, restoredAt: new Date().toISOString(), error: String(lastError?.message ?? lastError) };
    },

    /**
     * The restore is not believed until it is proved: same row count AND same content
     * fingerprint as the snapshot, per table. A single row that was not put back, or put back
     * with a changed column, fails this.
     */
    verifyRestore(handle) {
      assertHandle(handle, schema);
      // The module promises that a journey which wrote something it did not declare is a failure.
      // Enforce it here rather than trusting the caller: rows in an undeclared table were never
      // snapshotted, so they were never put back either.
      const undeclared = this.undeclaredWrites(handle);
      const tables = [];
      for (const before of handle.before) {
        let after;
        try {
          after = this.fingerprint(before.table);
        } catch (error) {
          tables.push({ ...before, actualRowCount: null, actualFingerprint: null, ok: false, reason: `could not be read back: ${String(error?.message ?? error)}` });
          continue;
        }
        const countOk = after.rowCount === before.rowCount;
        const contentOk = after.fingerprint === before.fingerprint;
        tables.push({
          table: before.table,
          rowCount: before.rowCount,
          actualRowCount: after.rowCount,
          fingerprint: before.fingerprint,
          actualFingerprint: after.fingerprint,
          ok: countOk && contentOk,
          reason: countOk && contentOk ? null
            : !countOk ? `${before.rowCount - after.rowCount} row(s) missing after the restore`
              : "rows are back but their contents changed"
        });
      }
      const mismatches = tables.filter((row) => !row.ok);
      if (undeclared.length) {
        mismatches.push({
          table: undeclared.join(", "),
          undeclared: true,
          ok: false,
          reason: "the run changed data it did not declare, and that data was not put back"
        });
      }
      return { verified: mismatches.length === 0, tables, mismatches, undeclaredTables: undeclared };
    },

    cleanup(handle) {
      assertHandle(handle, schema);
      for (const table of handle.tables) {
        try {
          sql(`drop table if exists ${snapshotTableName(handle.id, table)}`);
        } catch {
          // A snapshot table left behind is recoverable; never let cleanup mask a real result.
        }
      }
    }
  };
}

/** Default runner: psql, one statement batch per call, ON_ERROR_STOP so a failure is never silent. */
export function psqlRunner(databaseUrl, { spawnSync, psqlBin = "psql", cwd, redact = (value) => value } = {}) {
  if (typeof spawnSync !== "function") throw new Error("psqlRunner needs node:child_process spawnSync");
  return (text) => {
    const child = spawnSync(psqlBin, [databaseUrl, "-v", "ON_ERROR_STOP=1", "-X", "-q", "-At", "-F", "\t"], {
      cwd,
      encoding: "utf8",
      input: `${text}\n`
    });
    if (child.status !== 0) {
      // Never echo the STATEMENT back -- that is what would put table names into a log. The
      // server's own error is what an operator needs when a restore fails, so keep it, redacted.
      const detail = redact(String(child.stderr ?? "").trim().split("\n").filter(Boolean).slice(-3).join("; ")).slice(0, 400);
      const error = new Error(`database statement failed (exit ${child.status})${detail ? `: ${detail}` : ""}`);
      error.serverDetail = detail;
      throw error;
    }
    return String(child.stdout ?? "")
      .split("\n")
      .filter((line) => line !== "" && !["SET", "BEGIN", "COMMIT", "ROLLBACK"].includes(line.trim()))
      .map((line) => line.split("\t"));
  };
}
