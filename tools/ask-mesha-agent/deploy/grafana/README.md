# Ask Mesha Grafana dashboard

`ask-mesha.json` charts `ask_mesha.events` (written by `../../events.mjs`): asks, success rate,
failures by class, p50/p90 total + first-token latency, per-tool timing, a per-user table, recent
asks, and spend this month vs the $100 cap. A logs panel shows the raw structured lines from
Cloud Logging (`jsonPayload.component="ask-mesha-agent"`).

It is not in `infra/grafana/dashboards/` yet because `ask_mesha` lives in its own database
(RUNBOOK §3), which none of the provisioned datasources reach. To promote it, add the datasource
below to `infra/grafana/provisioning/datasources/datasources.yaml` (+ env vars in
`infra/envs/stg/observability.tf`), move the JSON there and run `tools/ci/check-grafana-durability.mjs`.

## Import (goatos-stg-grafana)

1. Read-only role for Grafana, as the instance admin, connected to database `ask_mesha`:
   ```sql
   CREATE ROLE grafana_ask_mesha_ro LOGIN PASSWORD '<from Secret Manager>';
   GRANT CONNECT ON DATABASE ask_mesha TO grafana_ask_mesha_ro;
   GRANT USAGE ON SCHEMA ask_mesha TO grafana_ask_mesha_ro;
   GRANT SELECT ON ask_mesha.events TO grafana_ask_mesha_ro;   -- events only, never chats/messages
   ```
2. Grafana → Connections → Data sources → PostgreSQL, **uid `ask-mesha-postgres`**, host = the
   same Cloud SQL socket as "Postgres (analytics rollups)", database `ask_mesha`, that role, TLS off.
3. Dashboards → New → Import → upload `ask-mesha.json` → folder "Goat OS".
