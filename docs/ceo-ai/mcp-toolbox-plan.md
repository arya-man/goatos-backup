# Mesha Leadership Assistant MCP Toolbox Plan

Status: production-ready plan and starter config. This document does not deploy
cloud resources by itself.

## Goal

The leadership assistant should answer broad Mesha operating questions from real
data without exposing write paths or raw production tables to the model.

The production shape is:

```text
Mesha dashboard bubble
  -> Mesha assistant API
  -> Gemini/Vertex AI plans the answer
  -> MCP Toolbox exposes approved read tools
  -> Cloud SQL Postgres read-only views
  -> Mesha assistant API formats and audits the answer
```

The leadership role is top-level, but the bot still stays read-only.
Operational writes must continue through normal Mesha APIs, domain events,
idempotency, audit, and approval flows.

## What MCP Toolbox Does Here

MCP Toolbox is the database tool server. It gives the model a controlled list of
tools instead of letting it hold a database password or invent unsafe queries.

Use it for:

- Cross-module leadership questions that do not map cleanly to one dashboard API.
- Fast analytics over curated reporting views.
- A last-resort read-only SQL tool over a locked `ceo_ai` schema.
- Central tool descriptions, parameters, pooling, auth, and observability.

Do not use it to wrap every REST API. REST APIs are still the correct surface for
business commands and app-shaped reads. Wrapping each API as MCP creates duplicate
contracts, duplicate auth rules, noisy tools, and worse model selection. The bot
needs business-capability tools, not one tool per endpoint.

## Current Backend Context

The Mesha backend is Go with `pgx/v5` / `pgxpool` and sqlc-style typed SQL. It is
not GORM. Existing services own business invariants in Go repositories and
Postgres migrations.

The Cloud Run pattern already mounts Cloud SQL at `/cloudsql`, stores
`DATABASE_URL` in Secret Manager, and runs migrations as a separate release job.
The Toolbox service should follow the same region and Cloud SQL connector pattern,
but with its own service account, read-only database user, and config secret.

## Required Cloud Resources

Create one Toolbox service per environment that needs the leadership assistant:

```text
Service:          mesha-mcp-toolbox-stg / mesha-mcp-toolbox-prod
Region:           asia-south1
Runtime:          Cloud Run
Ingress:          internal or authenticated service-to-service only
Service account:  mesha-mcp-toolbox-stg / mesha-mcp-toolbox-prod
Cloud SQL role:   Cloud SQL Client
Secret access:    Toolbox config + read-only DB credential only
```

The dashboard should not call Toolbox directly from the browser. The Mesha
assistant API calls Toolbox server-side after verifying leadership role and tenant
scope.

## Environment Variables

Use new Mesha-prefixed env vars for the assistant/Toolbox integration:

```text
MESHA_AI_PROVIDER=vertex
MESHA_VERTEX_PROJECT=<env project>
MESHA_VERTEX_LOCATION=asia-south1
MESHA_VERTEX_MODEL=gemini-2.5-flash

MESHA_MCP_TOOLBOX_URL=<Cloud Run Toolbox URL>
MESHA_MCP_TOOLSET=mesha_ceo_toolset
MESHA_MCP_DB_USER=mesha_ceo_readonly
MESHA_MCP_DB_PASSWORD=<Secret Manager>
MESHA_MCP_TENANT_ID=<tenant uuid from server-side session>

MESHA_GCP_PROJECT=<env project>
MESHA_GCP_REGION=asia-south1
MESHA_CLOUDSQL_INSTANCE=<Cloud SQL instance name>
MESHA_DATABASE_NAME=<database name>
```

Keep existing legacy-prefixed runtime variables until the app is migrated, but
new leadership assistant config should use Mesha names.

## Database Role

Create a dedicated read-only database role. It should not inherit the app user's
permissions.

```sql
CREATE ROLE mesha_ceo_readonly LOGIN PASSWORD '<generated secret>';
ALTER ROLE mesha_ceo_readonly SET statement_timeout = '8000ms';
ALTER ROLE mesha_ceo_readonly SET idle_in_transaction_session_timeout = '5000ms';
ALTER ROLE mesha_ceo_readonly SET default_transaction_read_only = on;

CREATE SCHEMA IF NOT EXISTS ceo_ai;

REVOKE ALL ON SCHEMA public FROM mesha_ceo_readonly;
GRANT USAGE ON SCHEMA ceo_ai TO mesha_ceo_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA ceo_ai TO mesha_ceo_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA ceo_ai
  GRANT SELECT ON TABLES TO mesha_ceo_readonly;
```

Do not grant `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `CREATE`, `USAGE` on
sequences, or direct access to `public` tables.

## Reporting Schema

Expose stable `ceo_ai.*` views. The model should see business-language columns,
not raw normalized tables.

Recommended initial views:

```text
ceo_ai.animal_current_scope
  tenant_id, animal_id, park_id, park_label, shed_id, shed_label,
  management_stage, lifecycle_status, sex, breed, age_days

ceo_ai.vaccination_shed_status
  tenant_id, park_label, shed_label, animals, due, done, planned_sessions,
  next_due_date, manager_label, backup_label, status

ceo_ai.feed_direction_current
  tenant_id, feed_day, park_label, shed_label, workflow, session_no,
  feed_item_label, quantity_kg, blocked_reason, amended

ceo_ai.counts_movement_daily
  tenant_id, event_date, park_label, shed_label, births, deaths,
  transfers_out, shifts_in, shifts_out, approvals_pending

ceo_ai.procurement_pipeline
  tenant_id, source_label, batch_label, current_stage, animals,
  vaccination_pending, rejected, entered_at

ceo_ai.ops_exception_queue
  tenant_id, area, severity, status, park_label, shed_label, title,
  opened_at, owner_label, source_id

ceo_ai.sop_execution_status
  tenant_id, area, park_label, shed_label, task_label, status,
  due_at, completed_at, verifier_label

ceo_ai.inventory_stock_position
  tenant_id, item_label, category, stock_on_hand, unit, park_label,
  reorder_flag, last_reconciled_at
```

These views can be ordinary views first. Convert hot summaries to materialized
views or projection tables only after query-plan checks show a real need.

## Toolsets

Use `docs/ceo-ai/mcp-toolbox-tools.yaml` as the starter Toolbox config.

Initial toolset:

```text
mesha_ceo_toolset
  mesha_count_by_scope
  mesha_vaccination_due_summary
  mesha_feed_direction_summary
  mesha_ops_exceptions
  mesha_readonly_sql
```

Tool rules:

- Prefer specific tools first.
- Return aggregates by default.
- Do not list individual animals unless the user explicitly asks and an approved
  detail tool exists.
- Every tool must require `tenant_id` from server-side context.
- Limit rows at the SQL level.
- Add one new business tool per recurring leadership question cluster, not per API.

## Read-Only SQL Fallback

The fallback exists so leadership can ask questions not yet covered by a specific
tool. It must be guarded twice:

1. Model prompt: generate only one `SELECT` over `ceo_ai.*` with `LIMIT <= 100`.
2. Server validator: parse and reject anything outside the policy before calling
   Toolbox.

Reject SQL containing:

```text
; -- /* */ INSERT UPDATE DELETE UPSERT MERGE COPY ALTER CREATE DROP TRUNCATE
GRANT REVOKE ANALYZE VACUUM CALL DO EXECUTE SET RESET
```

Also reject:

- more than one statement
- references outside `ceo_ai`
- missing `tenant_id` filter unless the view function applies it internally
- missing `LIMIT`
- `LIMIT > 100`
- volatile functions

The safer version is a Postgres function like:

```sql
CREATE OR REPLACE FUNCTION ceo_ai.run_readonly_sql(sql text, tenant uuid)
RETURNS SETOF jsonb
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ceo_ai, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Implement through the Mesha backend SQL validator first';
END;
$$;
```

Do not implement dynamic SQL in the database until the backend validator and
audit logging are landed.

## Vertex AI Role

Vertex AI/Gemini is the reasoning layer:

- classify intent
- choose a Toolbox tool
- fill safe parameters
- optionally draft a read-only SQL query over `ceo_ai` views
- summarize the rows in business language

It should not receive database credentials, Cloud SQL IAM, or direct table access.
It does not execute SQL by itself. Mesha executes tools server-side and records
the audit trail.

## API Boundary

Keep one Mesha assistant endpoint in the app backend:

```text
POST /api/ceo-ai/ask
```

Server responsibilities:

- verify authenticated CEO/CXO role
- bind tenant/scope from session, not from user text
- call Vertex AI for plan/tool arguments
- call MCP Toolbox with the configured `mesha_ceo_toolset`
- validate read-only SQL fallback before execution
- summarize rows and include provenance
- write an audit event with question, tool, row count, latency, and status

The browser should only know about the Mesha assistant endpoint.

## Internal Tracking

This is not a user-facing feature. The chat UI should not expose an agent
trace, chain of thought, tool timeline, or debug transcript. Internal tracking
exists only for the small Mesha admin/engineering group to debug wrong answers,
latency, cost, tool failures, retry behavior, and permission issues.

Track internally:

```text
assistant_requests_total{tool,status}
assistant_request_latency_ms{tool}
assistant_tool_rows_returned{tool}
assistant_sql_rejected_total{reason}
assistant_vertex_failover_total
assistant_toolbox_errors_total{tool}
```

Log:

- actor id and role
- tenant id
- user question hash plus redacted question text
- chosen tool
- generated SQL hash when fallback is used
- row count
- source views
- latency
- error/rejection reason

## Rollout

1. Add `ceo_ai` schema, views, grants, and read-only role migration.
2. Add Secret Manager entries for Toolbox config and DB credential.
3. Deploy Toolbox Cloud Run with Cloud SQL connector and strict
   `allowed-hosts` / `allowed-origins`.
4. Add Mesha assistant backend client for Toolbox toolset loading/invocation.
5. Gate the dashboard bubble to CEO/CXO only.
6. Enable Vertex AI planner.
7. Add audit logging and metrics.
8. Run stg smoke questions:
   - "How many animals are in Castro 1?"
   - "Which sheds are overdue for vaccination today?"
   - "Show current feed blocked gaps."
   - "What approvals are pending?"
   - "What changed in counts yesterday?"
9. Promote only after SQL fallback rejection tests and role permission tests pass.

## References

- MCP Toolbox source config supports environment-variable replacement for
  secrets and `cloud-sql-postgres` sources.
- MCP Toolbox tools support parameterized `postgres-sql` statements and
  authorization hooks.
- MCP Toolbox toolsets let one app load only the tools it needs.
- MCP Toolbox Cloud Run deployment supports config mounting from Secret Manager
  and production `allowed-hosts` / `allowed-origins` hardening.
