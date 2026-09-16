# One-time staging Grafana SQL bootstrap

`python3 tools/deploy/bootstrap-grafana-readonly.py` prints a nonmutating plan.
After local proof and review, the operator may use `--apply` with an existing
administrative `DATABASE_URL` loaded in memory through the staging Cloud SQL
Auth Proxy. Target is database `goatos` in `goatos-stg:asia-south1:goatos-stg-core-db`.
The operator must verify that proxy target before applying. The script requires
CREATEROLE and table grant privileges; `goatos_app` has CREATEROLE in verified STG.

This initializes only `goatos_grafana_ro`, the dedicated password secret, and a
secret-scoped accessor binding for the service's actual runtime account. The
Cloud Deploy service account receives only secret-scoped metadata Viewer for
version/IAM preflight; it receives no password access from this bootstrap. SQL
uses stdin, password generation remains in memory, and subprocess diagnostics
are suppressed because PostgreSQL can echo failing SQL. No secret JSON/key files
are created. The role has no memberships and receives only CONNECT, analytics
USAGE, and SELECT on enumerated **existing** summary tables. Missing future tables
are granted by migrations319/320. No ALL TABLES/default privileges or raw-event
SELECT grants. Initialization aborts if PUBLIC would expose app_events.

## Partial failure

There is no cross-service transaction between SQL and Secret Manager. Existing
role or secret stops initialization before password generation; do not blindly
retry or rotate. Inspect metadata and SQL role/grants first:

- Role and enabled secret version exist: preserve both. Finish only missing
  secret accessor binding for the verified runtime SA, then test a read-only
  connection using the existing secret in memory.
- Role exists but no enabled secret version: the generated password was never
  durably stored. Stop and report the exact phase. Operator-approved recovery
  must verify the role has no owned objects or sessions before explicitly
  resetting this initial incomplete credential; the bootstrap never does it.
- Secret exists without role: stop and inspect provenance; never overwrite it.

Verify summary SELECT succeeds and raw `analytics.app_events` SELECT fails using
the new principal. Cloud Deploy's metadata-only preflight refuses absent/disabled
secret versions or missing explicit runtime accessor before log metrics or GCS
asset mutations. Bootstrap is not part of routine deployment and creates no
second service deployment authority.
