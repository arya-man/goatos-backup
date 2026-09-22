# Manual DB change audit

Codex, Claude, and humans sometimes change goatos-stg rows directly (psql,
psycopg2, one-off scripts). Migration `000387_manual_db_change_audit.sql` records
every such change in `audit.db_changes`, with who did it, why, and the row's
before/after values.

## What is captured

| Captured | Not captured |
|---|---|
| INSERT / UPDATE / DELETE on every `public` table, including partitions (logged under the partition's own name) | SELECTs (reads) |
| TRUNCATE (table name only, no rows) | DDL (`CREATE` / `ALTER` / `DROP`); schema changes go through migrations and git |
| Old + new row as JSON, `actor`, `reason`, DB user, `application_name`, client IP, `txid`, time | Writes by Goat OS services (API, workers, migrate); see "How service traffic is excluded" |
| Tables added by later migrations; `cmd/migrate` runs `audit.attach_all()` after every deploy | Sessions that deliberately bypass triggers (`session_replication_role = replica`, dropping the trigger) |

## How to make a change (humans, Codex, Claude)

Always wrap the write in a transaction that states who and why:

```sql
BEGIN;
SELECT audit.begin_change('ravi via claude', 'fix wrong vaccine date for G123, reported in #ops');
UPDATE ...;
COMMIT;
```

- `actor`: `<person> via <tool>`, e.g. `ravi via codex`, `ravi via psql`.
- `reason`: the purpose in plain words, plus a ticket, Slack thread, or PR link when one exists.
- `audit.begin_change` refuses an empty actor or reason. The values are
  transaction-local (`set_config(..., true)`), so they never leak into a pooled
  connection. Plain `SET LOCAL app.actor = '...'; SET LOCAL app.reason = '...';` works too.
- A write with no actor or reason is **still recorded**; only the `actor` and
  `reason` columns are empty. Treat an empty-reason row as a process miss.

Set a recognisable client name as well, so `application_name` shows the tool:

```bash
PGAPPNAME=claude psql "$DB_URL" ...
```

```python
psycopg2.connect(..., application_name="codex")
```

Do **not** use an application name starting with `goatos-`. That prefix marks
service traffic, and it switches the audit off for the session.

## Reading the trail

```sql
-- latest manual changes
SELECT changed_at, actor, reason, application_name, table_name, op, old_row, new_row
FROM audit.db_changes ORDER BY changed_at DESC LIMIT 50;

-- one table's history
SELECT * FROM audit.db_changes WHERE table_name = 'goat' ORDER BY changed_at DESC;

-- changes nobody explained
SELECT * FROM audit.db_changes WHERE reason IS NULL ORDER BY changed_at DESC;

-- everything one transaction did
SELECT * FROM audit.db_changes WHERE txid = <txid> ORDER BY id;
```

`audit.db_changes` is append-only: UPDATE, DELETE, and TRUNCATE on it raise an error.

## How service traffic is excluded

Tagging is **opt-in per deployed binary**. Only the deployed Cloud Run services and
scheduled jobs (API, workers, sweepers, migrate) set
`Config.ApplicationName = platformpg.ServiceApplicationName("<binary>")`, which
gives their pool `application_name = 'goatos-<binary>'`. Each audit trigger has
`WHEN (current_setting('application_name') NOT LIKE 'goatos-%')`, so for those
sessions Postgres evaluates that condition in C and never runs the trigger
function. API write latency is unaffected.

Everything else stays untagged and **is audited**: every operator CLI under
`backend/cmd/*` (repairs such as `repair-obligation-duplicates`, recomputes,
backfills, seeds, imports), whether it uses `platformpg.Connect` or its own pool.

`backend/internal/platform/postgres/service_application_name_guard_test.go` pins
the exact list of service-tagged binaries. Tagging any other binary fails the
test. Adding a name there is a deliberate review decision that exempts that
binary from the audit; never do it just to fix a build.

## Operations

- Re-attach triggers by hand (idempotent; returns the count of newly covered tables):
  `SELECT audit.attach_all();`
- Size: rows accumulate only from manual sessions, so growth is small. If a large
  manual backfill bloats the table, archive old rows through a migration rather than
  deleting ad hoc (the append-only guard blocks ad-hoc deletes by design).
