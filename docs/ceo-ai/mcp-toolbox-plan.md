# Mesha Leadership Assistant MCP Toolbox Plan

Status: target architecture and implementation plan. This document does not
claim the ADK/Vertex agent runtime, persisted memory, MCP Toolbox service,
`ceo_ai.*` reporting schema, or SQL fallback are already implemented.

Currently implemented on `main`:

- leadership-only dashboard bubble and safe assistant endpoint shell
- read-only responses through existing Mesha APIs for the first demo paths
- server-side role/scope gate
- docs, skills, hooks, and local CI guardrails that force future assistant
  coverage updates

Not implemented yet:

- Google ADK agent service
- Vertex/Gemini production planner behind the Mesha backend
- Cube Core metric service
- persisted memory/session store
- production tool orchestration, retries, and fallback execution
- MCP Toolbox Cloud Run service
- dedicated read-only database role and `ceo_ai.*` reporting schema
- validated safe SQL fallback
- assistant audit/metrics tables

## Goal

The leadership assistant should answer broad Mesha operating questions from real
data without exposing write paths or raw production tables to the model.

The production shape is:

```text
Mesha dashboard bubble
  -> Mesha assistant API
  -> Gemini/Vertex AI plans the answer
  -> Cube for governed metrics when the metric exists
  -> Mesha APIs or MCP Toolbox for operational/detail reads
  -> Cloud SQL Postgres read-only views for fallback
  -> Mesha assistant API formats and audits the answer
```

The leadership role is top-level, but the bot still stays read-only.
Operational writes must continue through normal Mesha APIs, domain events,
idempotency, audit, and approval flows.

## Where Cube Fits

Cube is the governed metric service for official leadership numbers. It is not
Vertex AI and it is not MCP Toolbox.

```text
Vertex/Gemini:
  understands the English question, extracts filters, and chooses the route.

Cube:
  owns approved metric formulas and runs SQL against Postgres or BigQuery.

MCP Toolbox:
  exposes curated database tools and safe fallback reads.

Mesha APIs:
  expose app/workflow-shaped operational reads.
```

Use Cube first for official KPI questions:

- active animals
- vaccination due and overdue
- vaccination compliance
- mortality/deaths
- procurement cost and pipeline metrics
- feed cost/consumption
- operator completion and backlog

If Cube has the metric, the assistant must call Cube instead of asking Gemini to
invent SQL. If Cube does not have the metric, the assistant can use a Mesha read
API, then MCP Toolbox, then validated read-only SQL fallback.

Local testing should run Cube as a separate service:

```text
local dashboard :3300
  -> local assistant API
  -> local Cube Core on a configured local port, for example 127.0.0.1:4000
  -> local Postgres read-only user
```

The `127.0.0.1:4000` value is only the recommended laptop default. The actual
local URL is controlled by `MESHA_CUBE_URL`.

Staging should run Cube as its own Cloud Run service:

```text
Service:          mesha-cube-stg / mesha-cube-prod
Region:           asia-south1
Runtime:          Cloud Run
Ingress:          internal or authenticated service-to-service only
Service account:  mesha-cube-stg / mesha-cube-prod
Cloud SQL role:   Cloud SQL Client
Secret access:    Cube API secret/JWT secret + read-only DB credential only
```

In early staging, Cube should query stg Cloud SQL Postgres through
`mesha_cube_readonly`. As BigQuery/dbt marts land, Cube can point historical
official metrics to BigQuery while operational current-state metrics can remain
on Postgres/read models.

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

MESHA_CUBE_URL=<Cube Cloud Run URL or http://127.0.0.1:4000>
MESHA_CUBE_API_SECRET=<Secret Manager>
MESHA_CUBE_DB_USER=mesha_cube_readonly
MESHA_CUBE_DB_PASSWORD=<Secret Manager>

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

## Reporting Schema Coverage Contract

Expose stable `ceo_ai.*` views for every leadership-relevant data surface in
Mesha. This is not a small fixed analytics pack. It is the contract that makes
the assistant understand the whole product now, and stay updated as the product
grows.

The model should see business-language columns, not raw normalized tables. Every
current and future module must be covered by exactly one of these paths:

- existing Mesha read API mapped into the assistant tool catalog
- named MCP Toolbox business tool over a curated reporting view
- governed read-only SQL fallback over `ceo_ai.*`
- explicit documented exclusion explaining why the feature must not be visible
  to the leadership assistant

No leadership-relevant feature is complete until this assistant coverage is
updated in the same PR. That applies to all developers and all coding agents.

Required coverage areas include every current Mesha operating domain:

```text
ceo_ai.animal_current_scope
  tenant_id, animal_id, park_id, park_label, shed_id, shed_label,
  species, management_stage, lifecycle_status, sex, breed, age_days

ceo_ai.shed_capacity_current
  tenant_id, park_label, shed_label, animals, capacity, variance,
  status, owner_label, backup_label

ceo_ai.vaccination_shed_status
  tenant_id, park_label, shed_label, animals, due, done, planned_sessions,
  next_due_date, manager_label, backup_label, status

ceo_ai.vaccination_dose_pickup
  tenant_id, business_date, park_label, shed_label, vaccine_label,
  doses_to_pick, animals_due, animals_overdue, owner_label, backup_label,
  next_action

ceo_ai.feed_direction_current
  tenant_id, feed_day, park_label, shed_label, workflow, session_no,
  feed_item_label, quantity_kg, blocked_reason, amended

ceo_ai.counts_movement_daily
  tenant_id, event_date, park_label, shed_label, births, deaths,
  transfers_out, shifts_in, shifts_out, approvals_pending

ceo_ai.procurement_pipeline
  tenant_id, source_label, batch_label, current_stage, animals,
  vaccination_pending, rejected, entered_at

ceo_ai.source_entry_health_status
  tenant_id, load_label, source_label, animals_expected, animals_received,
  animals_accepted, animals_rejected, health_blockers, evidence_status

ceo_ai.ops_exception_queue
  tenant_id, area, severity, status, park_label, shed_label, title,
  opened_at, owner_label, source_id

ceo_ai.sop_execution_status
  tenant_id, area, park_label, shed_label, task_label, status,
  due_at, completed_at, verifier_label

ceo_ai.verification_queue_status
  tenant_id, area, park_label, shed_label, pending, rejected, accepted,
  oldest_pending_at, owner_label

ceo_ai.inventory_stock_position
  tenant_id, item_label, category, stock_on_hand, unit, park_label,
  reorder_flag, last_reconciled_at

ceo_ai.workforce_coverage_status
  tenant_id, park_label, role_label, owner_label, backup_label,
  coverage_status, active_work_count, overdue_work_count

ceo_ai.action_center_current
  tenant_id, area, severity, park_label, shed_label, title, owner_label,
  backup_label, due_at, status

ceo_ai.audit_activity_summary
  tenant_id, business_date, area, actor_label, action_label, result,
  count, last_activity_at
```

These views can be ordinary views first. Convert hot summaries to materialized
views or projection tables only after query-plan checks show a real need.

## Toolsets

Use `docs/ceo-ai/mcp-toolbox-tools.yaml` as the starter Toolbox config.

Minimum always-on toolset:

```text
mesha_ceo_toolset
  mesha_count_by_scope
  mesha_capacity_summary
  mesha_vaccination_due_summary
  mesha_vaccination_dose_pickup
  mesha_feed_direction_summary
  mesha_shifting_summary
  mesha_procurement_summary
  mesha_ops_exceptions
  mesha_workforce_coverage
  mesha_verification_queue
  mesha_inventory_stock
  mesha_action_center
  mesha_audit_summary
  mesha_readonly_sql
```

Tool rules:

- Prefer specific tools first.
- Return aggregates by default.
- Do not list individual animals unless the user explicitly asks and an approved
  detail tool exists.
- Every tool must require `tenant_id` from server-side context.
- Limit rows at the SQL level.
- Add one new business tool per recurring leadership question cluster.
- Future modules must add their assistant tool mapping before the feature is
  considered done.

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

1. Add `ceo_ai` schema, grants, read-only role migration, and coverage views for
   all current leadership-relevant modules.
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
9. Add a CI guard that fails when a new read API, reporting table, module route,
   or operational feature is added without assistant coverage or an explicit
   exclusion.
10. Promote only after SQL fallback rejection tests, role permission tests, and
    coverage guard tests pass.

## Future-Work Enforcement

Every future feature PR must answer this checklist:

- What leadership question should the assistant answer for this feature?
- Which Mesha read API, MCP tool, or `ceo_ai.*` view exposes it?
- What words should the assistant use in business language?
- What internal/backend fields must be hidden from the answer?
- What tenant, park, shed, date, role, and row-limit guards apply?
- What source/freshness metadata should the answer carry internally?

If the answer is "this feature is not for leadership," the PR must document that
exclusion. Silence is not allowed. This rule belongs in agent instructions,
Claude/Codex skill context, PR checks, and local CI so future work keeps the
assistant current automatically.

### Explicit exclusion: role navigation visuals

Android role chrome, Paparazzi screenshots, GitHub Pages navigation graphs, and
UI-only display-label cleanup do not add a leadership assistant read API, MCP
Toolbox tool, SQL fallback surface, or `ceo_ai.*` coverage view. They are still
guarded by the visual regression suite and UI label guard, but assistant
coverage is unchanged unless the underlying backend module, reporting contract,
or leadership question changes.

## References

- MCP Toolbox source config supports environment-variable replacement for
  secrets and `cloud-sql-postgres` sources.
- MCP Toolbox tools support parameterized `postgres-sql` statements and
  authorization hooks.
- MCP Toolbox toolsets let one app load only the tools it needs.
- MCP Toolbox Cloud Run deployment supports config mounting from Secret Manager
  and production `allowed-hosts` / `allowed-origins` hardening.
