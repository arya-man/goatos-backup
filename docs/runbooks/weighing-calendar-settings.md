# Change weighing calendar dates directly in PostgreSQL

**For Claude, Codex, or an operator asked to change weighing dates:** use the tenant row in `public.weighing_calendar_config`. This is database-only configuration. Do not build a settings UI, edit frontend constants, publish a SOP, or deploy code to change these values.

The initial application deployment installs the table and reader. Subsequent changes are ordinary SQL transactions. They control the existing calendar on ADG Analytics, Weights, and Download:

| Column | Initial value | Meaning |
| --- | --- | --- |
| `earliest_date` | `2026-07-05` | First selectable calendar date |
| `default_from_mode` | `fixed_date` | `fixed_date` or `rolling_weeks`; legacy `rolling_days` also supported |
| `default_from_date` | `2026-08-03` | Landing start when mode is fixed |
| `default_from_weeks` | `NULL` | Whole weeks before the current IST date, when mode is rolling weeks |
| `default_from_days` | `NULL` | Legacy inclusive-day count; leave NULL for fixed/week modes |

## Connect and confirm the tenant

Use the database explicitly requested by the user. For `goatos-stg`, use Cloud SQL `goatos-stg:asia-south1:goatos-stg-core-db` through the approved Secret Manager/Auth Proxy path; do not substitute OCI. Local OCI testing uses its existing private environment and tunnel. Never print or commit the connection string or credentials.

Open `psql` with the securely loaded connection string and fail on SQL errors:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1
```

Confirm the target database and tenant before a write:

```sql
SELECT current_database(), inet_server_addr(), inet_server_port();
SELECT tenant_id, name FROM public.tenants ORDER BY name;

-- Replace this placeholder with the confirmed tenant UUID.
\set tenant_id 'CONFIRMED-TENANT-UUID'

SELECT * FROM public.weighing_calendar_config
WHERE tenant_id = :'tenant_id'::uuid;
```

Record the returned row before changing it. The tenant row is the entire scope; never omit the tenant predicate. The migration seeds existing tenants. If a newly created tenant has no row, first create its default row:

```sql
INSERT INTO public.weighing_calendar_config (tenant_id)
VALUES (:'tenant_id'::uuid)
ON CONFLICT (tenant_id) DO NOTHING;
```

## Set July 5 minimum and fixed August 3 default

```sql
BEGIN;
SELECT * FROM public.weighing_calendar_config
WHERE tenant_id = :'tenant_id'::uuid FOR UPDATE;

UPDATE public.weighing_calendar_config
SET earliest_date = DATE '2026-07-05',
    default_from_mode = 'fixed_date',
    default_from_date = DATE '2026-08-03',
    default_from_weeks = NULL,
    default_from_days = NULL
WHERE tenant_id = :'tenant_id'::uuid
RETURNING *;
-- Verify UPDATE 1 and the returned dates. Otherwise ROLLBACK.
COMMIT;
```

To change only the earliest selectable date, update only `earliest_date` with the same tenant predicate and transaction. In fixed mode the minimum must not be later than the fixed default start.

## Switch to six weeks before today, or any configured N

This changes the default start and leaves the earliest selectable day unchanged:

```sql
\set weeks 6
BEGIN;
SELECT * FROM public.weighing_calendar_config
WHERE tenant_id = :'tenant_id'::uuid FOR UPDATE;

UPDATE public.weighing_calendar_config
SET default_from_mode = 'rolling_weeks',
    default_from_weeks = :weeks,
    default_from_date = NULL,
    default_from_days = NULL
WHERE tenant_id = :'tenant_id'::uuid
RETURNING *;
-- Verify UPDATE 1 and the returned week count. Otherwise ROLLBACK.
COMMIT;
```

Change `\set weeks 6` to a whole number from **1 to 520** for N weeks. The DB rejects invalid modes, missing required mode values, out-of-range counts, and invalid fixed-date boundaries. PostgreSQL date columns reject invalid calendar dates. To return to a fixed date, use the previous transaction with the desired `default_from_date`.

## Verify the effective default and refresh

```sql
WITH current_day AS (
  SELECT (CURRENT_TIMESTAMP AT TIME ZONE 'Asia/Kolkata')::date AS today
)
SELECT c.tenant_id, c.earliest_date, c.default_from_mode,
       c.default_from_date, c.default_from_weeks, c.default_from_days,
       d.today AS current_ist_day,
       LEAST(d.today, GREATEST(c.earliest_date,
         CASE c.default_from_mode
           WHEN 'fixed_date' THEN c.default_from_date
           WHEN 'rolling_weeks' THEN d.today - c.default_from_weeks * 7
           WHEN 'rolling_days' THEN d.today - (c.default_from_days - 1)
         END)) AS effective_default_start,
       c.row_version, c.updated_at
FROM public.weighing_calendar_config c CROSS JOIN current_day d
WHERE c.tenant_id = :'tenant_id'::uuid;

SELECT family_key, revision, changed_at
FROM public.admin_ui_config_family_revisions
WHERE tenant_id = :'tenant_id'::uuid AND family_key = 'weighing-calendar';
```

A database trigger maintains row metadata and bumps the admin UI configuration revision even for a plain SQL UPDATE. The existing bootstrap reader consumes the row. The frontend bootstrap cache can retain the previous configuration for up to **60 seconds**; then reload the page. No server restart or deployment is required.

Verify `/weighing/analytics?scope_mode=company` or `/weighing/weights?scope_mode=company` without `wt_from`/`wt_to`: explicit date selections take precedence over defaults. Check the earliest calendar day and the default start. The same minimum applies to Download. Report the actual DB readback and rendered dates; do not claim success from UPDATE alone.

## Date semantics

- `rolling_weeks`: current **Asia/Kolkata date minus N × 7 days**, clamped to `earliest_date`. September 16, 2026 minus six weeks is **August 5**; minus two weeks is **September 2**.
- Fixed mode does not move each day.
- Both report endpoints are inclusive. Weeks specify a date offset, not an inclusive date count.
- The default end remains the latest weighing date in the selected scope. Missing/future/older-than-start latest dates fall back to today so the range remains valid. This change does not configure the end date.
- Legacy `rolling_days` retains its inclusive count: today minus `(days - 1)`.
- Calendar settings are independent of `sop_versions`. Do not modify or publish a weighing SOP for this request; its historical `weights_pages` metadata is not the calendar authority.

## Rollback a configuration change

Use the same transaction pattern to restore the **previously recorded row values**, including mode and NULL values for unused fields. Do not delete the row as a rollback: that would invoke application defaults rather than the recorded prior configuration. Confirm the revision, wait for the bounded cache expiry, and read the page again.
