# Grafana Access — goatos-stg

> Companion to `INFRA.md` §6 ("Grafana access: Cloud Run IAM, not IAP") and
> §8 (`observability_operator_members` has no default — nobody can invoke
> Grafana until it is supplied). Read that section first for the *why*; this
> doc is the *how*, for both humans and agents (Claude/Codex).

## 1. Where Grafana lives — staging target

Grafana's staging target is the Cloud Run service `goatos-stg-grafana`,
project `goatos-stg`, region `asia-south1`, running `grafana/grafana:11.3.0`
when deployed.

```text
GRAFANA_STG_URL = https://goatos-stg-grafana-awtrpmn4za-el.a.run.app
```

**How it was deployed (NOT terraform):** the committed `infra/envs/stg`
Terraform state is empty — stg was built imperatively — so applying the
observability Terraform would collide with live resources (see the memory note
`goatos-stg-tf-state-empty`). Grafana was therefore stood up **imperatively via
`gcloud run deploy`** to match how the rest of stg was built. The Terraform in
`observability.tf` remains the source-of-truth definition for when stg is
brought under IaC (import-then-apply).

**Access model:** when live-smoked, the Cloud Run service is expected to be
invokable by `allUsers` at the network layer, but **gated by Grafana's own
auth** — anonymous disabled (`GF_AUTH_ANONYMOUS_ENABLED=false`), sign-up off, a
strong admin password in Secret Manager
(`goatos-stg-grafana-admin-password`), and API access requiring a Grafana
service-account token. The deploy smoke must verify unauthenticated API, anon
dashboard search, login page, and SA-token API behavior before anyone claims the
current live endpoint is usable.

- **Browser login:** open the URL, sign in as `admin`. Get the password with
  `gcloud secrets versions access latest --secret=goatos-stg-grafana-admin-password --project=goatos-stg`.
- **Runtime identity:** the service runs as SA `goatos-grafana-stg@goatos-stg.iam.gserviceaccount.com`
  with read-only roles (`monitoring.viewer`, `cloudtrace.user`, `logging.viewer`,
  `bigquery.dataViewer`/`jobUser`, `cloudsql.client`) — datasources authenticate
  via that workload identity, no embedded secrets.

**Hardening follow-up (required before advertising `grafana.mesha.sg` beyond the
current operator circle):** put Grafana behind the existing external HTTPS load
balancer on `grafana.mesha.sg`, with **IAP on the Grafana backend service** so
browser access uses Google SSO before Grafana login. Grant IAP access to the
same four CEO/CXO Google identities verified for admin-web SSO:
`ravi@mesha.sg`, `manohark@mesha.sg`, `manju@mesha.sg`, and
`aryaman@mesha.sg`. The only temporary fallback is **public load balancer +
Grafana login**, but only if anonymous access stays disabled, sign-up stays
disabled, admin password/token handling is reviewed, and the service is
restricted to load-balancer ingress so the raw `*.run.app` URL no longer
bypasses the LB. Until that is done, the `*.run.app` URL remains the
authoritative login-gated endpoint.

## 1c. Proposed `grafana.mesha.sg` load-balancer path

This is a runbook proposal only. Do not run these commands casually: the live
staging load balancer was created manually and is not yet imported into
Terraform, so the clean path is an additive manual change followed by
documentation/import, not a speculative Terraform apply.

Last known LB facts from the 2026-09-15 runbook snapshot; re-verify during the
next live Grafana smoke before treating them as current:

```text
Project:        goatos-stg
Region:         asia-south1
Cloud Run:      goatos-stg-grafana
Current URL:    https://goatos-stg-grafana-awtrpmn4za-el.a.run.app
Current IAM:    allUsers has roles/run.invoker; named operators also present
Current ingress: all
LB IP:          8.233.143.24 (goatos-stg-dashboard-ip)
URL map:        goatos-stg-dashboard-map
HTTPS proxy:    goatos-stg-dashboard-https-proxy
Missing today:  grafana.mesha.sg DNS, managed cert, serverless NEG, backend
```

Security boundary for public `grafana.mesha.sg`:

1. Required access model: enable IAP on `goatos-stg-grafana-backend`, grant
   `roles/iap.httpsResourceAccessor` only to `user:ravi@mesha.sg`,
   `user:manohark@mesha.sg`, `user:manju@mesha.sg`, and
   `user:aryaman@mesha.sg`, keep Cloud CDN off for that backend, and keep
   Grafana login enabled as a second layer.
2. Temporary fallback, only while IAP is being wired: keep `allUsers` Cloud Run
   invoker for LB reachability, but verify `GF_AUTH_ANONYMOUS_ENABLED=false`,
   `GF_USERS_ALLOW_SIGN_UP=false`, a strong Secret Manager-backed admin
   password, reviewed Grafana service accounts/tokens, no public dashboard
   snapshots, and no anonymous API access.

Either model must also update Grafana's public root URL before cutover:

```bash
gcloud run services update goatos-stg-grafana \
  --project=goatos-stg \
  --region=asia-south1 \
  --ingress=internal-and-cloud-load-balancing \
  --update-env-vars=GF_SERVER_ROOT_URL=https://grafana.mesha.sg/
```

Add the LB pieces, matching the existing admin-web/API/MCP naming pattern:

```bash
gcloud compute network-endpoint-groups create goatos-stg-grafana-neg \
  --project=goatos-stg \
  --region=asia-south1 \
  --network-endpoint-type=serverless \
  --cloud-run-service=goatos-stg-grafana

gcloud compute backend-services create goatos-stg-grafana-backend \
  --project=goatos-stg \
  --global \
  --load-balancing-scheme=EXTERNAL_MANAGED \
  --protocol=HTTP \
  --timeout=30s

gcloud compute backend-services add-backend goatos-stg-grafana-backend \
  --project=goatos-stg \
  --global \
  --network-endpoint-group=goatos-stg-grafana-neg \
  --network-endpoint-group-region=asia-south1
```

If using IAP, enable it on the new backend before exposing DNS:

```bash
gcloud compute backend-services update goatos-stg-grafana-backend \
  --project=goatos-stg \
  --global \
  --iap=enabled,oauth2-client-id=<IAP_OAUTH_CLIENT_ID>,oauth2-client-secret=<IAP_OAUTH_CLIENT_SECRET>

gcloud iap web add-iam-policy-binding \
  --project=goatos-stg \
  --resource-type=backend-services \
  --service=goatos-stg-grafana-backend \
  --member='user:ravi@mesha.sg' \
  --role='roles/iap.httpsResourceAccessor'
gcloud iap web add-iam-policy-binding \
  --project=goatos-stg \
  --resource-type=backend-services \
  --service=goatos-stg-grafana-backend \
  --member='user:manohark@mesha.sg' \
  --role='roles/iap.httpsResourceAccessor'
gcloud iap web add-iam-policy-binding \
  --project=goatos-stg \
  --resource-type=backend-services \
  --service=goatos-stg-grafana-backend \
  --member='user:manju@mesha.sg' \
  --role='roles/iap.httpsResourceAccessor'
gcloud iap web add-iam-policy-binding \
  --project=goatos-stg \
  --resource-type=backend-services \
  --service=goatos-stg-grafana-backend \
  --member='user:aryaman@mesha.sg' \
  --role='roles/iap.httpsResourceAccessor'
```

Create and attach the Google-managed certificate. It is fine to attach it before
the cert is ACTIVE, but do not rely on the hostname until it is active.

```bash
gcloud compute ssl-certificates create goatos-grafana-cert \
  --project=goatos-stg \
  --global \
  --domains=grafana.mesha.sg

gcloud compute target-https-proxies update goatos-stg-dashboard-https-proxy \
  --project=goatos-stg \
  --ssl-certificates=goatos-stg-dashboard-cert,goatos-stg-api-cert,goatos-mcp-cert,goatos-mcp-cert-20260814,goatos-prod-facing-cert,goatos-grafana-cert
```

Add the host rule and path matcher to the existing URL map. Use an exported YAML
edit or an equivalent reviewed command sequence; do not disturb the existing
`api-host`, `mcp-host`, or default admin-web routing.

```yaml
hostRules:
- hosts:
  - grafana.mesha.sg
  pathMatcher: grafana-host
pathMatchers:
- name: grafana-host
  defaultService: https://www.googleapis.com/compute/v1/projects/goatos-stg/global/backendServices/goatos-stg-grafana-backend
```

Then add Cloudflare DNS as **DNS-only**:

```text
Zone:   mesha.sg
Record: A grafana -> 8.233.143.24
Proxy:  DNS only
TTL:    Auto
```

Verification:

```bash
dig +short grafana.mesha.sg A
gcloud compute ssl-certificates describe goatos-grafana-cert \
  --project=goatos-stg \
  --global \
  --format='value(managed.status,managed.domainStatus)'
gcloud compute url-maps describe goatos-stg-dashboard-map \
  --project=goatos-stg \
  --format='yaml(hostRules,pathMatchers)'
curl -sS -o /dev/null -w '%{http_code}\n' https://grafana.mesha.sg/login
curl -sS -o /dev/null -w '%{http_code}\n' https://grafana.mesha.sg/api/search
```

Expected security result:

- IAP model: unauthenticated browser reaches the IAP challenge, authorized
  Google identity reaches Grafana login, and unauthenticated Grafana API access
  does not return data.
- Grafana-login fallback: `/login` returns 200, `/api/search` returns 401
  without a Grafana token, and raw `*.run.app` access is blocked by the new
  Cloud Run ingress setting.

**Runbook target:** datasources = Google Cloud Monitoring (default; also serves
Google Managed Prometheus metrics via `prometheus.googleapis.com/*`) and
BigQuery. The dashboard set is API/RED, Database, Kernel pipeline, Frontend
RUM, Mobile, and SLO/burn. Treat dashboard import, datasource health, and
non-empty panels as current only after the live Grafana smoke runs against that
deployment. Panels are **empty until the backend telemetry rollout** (api +
kernel jobs must run with `GOATOS_OBS_SINK=otlp` + the collector sidecar) and
the `analytics.*` rollup runs. Follow-ups: Cloud Trace / Cloud Logging /
Postgres datasources, the GMP query-frontend sidecar, and GCS-volume-based
provisioning (so datasources+dashboards self-restore on redeploy).

## 1b. Viewing the dashboards (first-time gotcha)

**Grafana's Home page shows a "Welcome to Grafana" tutorial panel by default, NOT your
dashboards** — new users think "nothing is here." The 6 Goat OS dashboards live under the
left-nav **Dashboards** menu (`/dashboards`). During live smoke, set the **org default
home dashboard** to "Goat OS — API / RED"; only claim Home shows current live
data after `/api/ds/query` returns representative non-empty frames in that environment:

```bash
# (admin auth) point Home at a dashboard by uid
curl -u admin:$PW -X PUT "$GRAFANA_STG_URL/api/org/preferences" \
  -H 'Content-Type: application/json' \
  -d '{"homeDashboardUID":"goatos-stg-api-red","timezone":"browser"}'
```

Do the same for prod after import. Dashboard uids: `goatos-stg-api-red`, `goatos-stg-db`,
`goatos-stg-kernel-pipeline`, `goatos-stg-frontend-rum`, `goatos-stg-mobile`,
`goatos-stg-slo-burn`.

**Second gotcha — the time range.** Panels default to "Last 6 hours"; if telemetry only
started minutes ago they look empty/sparse. Set the top-right range to **Last 15–30 minutes**
to see recent data. "No data" on a panel usually means either the time range is too wide OR
that metric isn't emitted yet (e.g. all `kernel_*` panels until the jobs rollout — see
LESSONS #22), not that the dashboard is broken.

## 2. Headless service-account token flow (recommended)

This is the path both humans running one-off queries and Claude/Codex should
use — no browser session required. It uses your own `gcloud` user or a
service account's identity to authenticate to Cloud Run, then mints a
Grafana-native API token so subsequent calls don't need a fresh Google ID
token every time.

```bash
# (a) Make sure you're pointed at the right project — never assume.
gcloud config set project goatos-stg

# (b) Mint a Cloud Run invoker identity token for your account.
#     Requires roles/run.invoker on goatos-stg-grafana (see INFRA.md §7/§8 —
#     granted via observability_operator_members).
export ID_TOKEN="$(gcloud auth print-identity-token)"

# (c) Pull the Grafana admin password from Secret Manager (read access
#     required; this is the GF_SECURITY_ADMIN_PASSWORD value Terraform wired
#     into the Grafana container — see observability.tf).
export GRAFANA_ADMIN_PASSWORD="$(gcloud secrets versions access latest \
  --secret=goatos-stg-grafana-admin-password --project=goatos-stg)"

# (d) Create a service account inside Grafana itself (one-time, or reuse an
#     existing one) via the Grafana HTTP API. The Cloud Run IAM identity
#     token goes in Authorization: Bearer; Grafana's own admin credentials go
#     in HTTP basic auth — Cloud Run IAM gates the request in, Grafana's own
#     auth gates the action once inside.
curl -sS -X POST "$GRAFANA_STG_URL/api/serviceaccounts" \
  -H "Authorization: Bearer $ID_TOKEN" \
  -u "admin:$GRAFANA_ADMIN_PASSWORD" \
  -H "Content-Type: application/json" \
  -d '{"name": "claude-codex-agent", "role": "Viewer"}'
# → returns {"id": <SA_ID>, ...}; use "role": "Editor" only if the task needs
#   to create/modify dashboards, not just query them.

# (e) Mint a token for that service account.
curl -sS -X POST "$GRAFANA_STG_URL/api/serviceaccounts/<SA_ID>/tokens" \
  -H "Authorization: Bearer $ID_TOKEN" \
  -u "admin:$GRAFANA_ADMIN_PASSWORD" \
  -H "Content-Type: application/json" \
  -d '{"name": "claude-codex-agent-token"}'
# → returns {"key": "<GRAFANA_STG_TOKEN>", ...} — copy this once, it is not
#   retrievable again. This is the token used for all future queries,
#   independent of the short-lived ID_TOKEN from step (b).
```

Once minted, every subsequent Grafana API/dashboard/datasource query still
goes through the Cloud Run IAM boundary, so both headers are required on
every call:

```bash
curl -sS "$GRAFANA_STG_URL/api/dashboards/uid/<dashboard-uid>" \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "Authorization: Bearer $GRAFANA_STG_TOKEN"
```

(Most HTTP clients only send one `Authorization` header — in practice the
Grafana MCP server or your script issues the Cloud Run identity token as a
short-lived wrapper/proxy layer, e.g. via `gcloud run services proxy`, and
uses the Grafana token for the actual Grafana auth. If you're scripting raw
`curl` against the public Cloud Run URL directly, mint a fresh `ID_TOKEN`
per call since these expire in ~1 hour, and pass it as the bearer token while
using Grafana's own `Authorization: Bearer $GRAFANA_STG_TOKEN` for the
Grafana-level auth — check whichever client/library you use for its
supported dual-auth pattern.)

## 3. Local storage — never commit tokens

Store both values in your shell profile, local only, never in git:

```bash
# ~/.zshrc (local only — never commit)
export GRAFANA_STG_URL="https://goatos-stg-grafana-<hash>-el.a.run.app"
export GRAFANA_STG_TOKEN="glsa_xxxxxxxxxxxxxxxxxxxx"
```

Rules:

- **NEVER commit `GRAFANA_STG_TOKEN` or `GRAFANA_STG_URL`'s hash-bearing
  hostname into any file in this repo** — no doc, no `.env` checked into git,
  no script with the value inlined.
- Secrets live in exactly two places: `~/.zshrc` (local, per-developer) or GCP
  Secret Manager (`goatos-stg-grafana-admin-password`,
  `goatos-stg-grafana-postgres-datasource-password`) for anything runtime
  services need. Never in git, never in chat transcripts committed as docs.
- If a token leaks (committed, pasted, logged), rotate it immediately — see
  `RUNBOOK.md` → "Rotate the Grafana token".

## 4. MCP wiring (Claude / Codex)

The Grafana MCP server (whichever distribution you use — the pattern is
env-var-based configuration, not a specific path) needs exactly two
environment variables:

```text
GRAFANA_URL   = $GRAFANA_STG_URL
GRAFANA_API_KEY (or GRAFANA_TOKEN, depending on the MCP server's exact var
                 name) = $GRAFANA_STG_TOKEN
```

Point your MCP server config at these two env vars (sourced from `~/.zshrc`,
never hardcoded into the MCP config file itself) so Claude/Codex can query
dashboards, datasources, and alerts through the MCP tool surface instead of
raw `curl`. Do not invent or assume a specific config file path here — follow
whatever your installed Grafana MCP server's own setup docs specify for where
it reads its env vars from (typically the same mechanism other MCP servers in
this workspace use: an env block in the MCP server's registration, pointing
at already-exported shell variables).

Because Grafana sits behind Cloud Run IAM, an MCP server that only speaks
plain Grafana API-key auth may not be able to reach `$GRAFANA_STG_URL`
directly unless something in front of it also attaches the Cloud Run identity
token (e.g. running the MCP server behind `gcloud run services proxy`
locally, or an IAM-aware wrapper). If your MCP server has no such wrapper,
fall back to the `gcloud run services proxy` local tunnel (§6) and point
`GRAFANA_URL` at `http://localhost:8080` instead.

## 5. GitHub-secret promotion trigger (decision — do this only when triggered)

**Today: no Grafana token exists in GitHub Actions secrets.** Nothing in CI
calls Grafana — dashboards are file-provisioned via Terraform/GCS
(`infra/grafana/dashboards/*.json`), and the deploy identity used by GitHub
Actions is Workload Identity Federation (WIF), not a Grafana token. Do not add
one speculatively.

**Add it when, and only when**, a workflow starts calling the Grafana HTTP
API for one of:

- API-based dashboard sync (instead of the current file-provisioning path),
- deploy annotations (marking a deploy event on Grafana panels), or
- synthetic/SLO checks that query Grafana from CI.

At that point, reserve these exact names, scoped to a GitHub Actions
**environment** (not repo-wide secrets), so `stg` and future `prod` never
share a token:

```text
GRAFANA_STG_TOKEN   — GitHub Actions environment secret, "stg" environment
GRAFANA_PROD_TOKEN  — GitHub Actions environment secret, "prod" environment
```

Reserving the names now (in this doc) means whoever adds that workflow later
doesn't have to invent naming — they just add the secret under the right
environment and reference `secrets.GRAFANA_STG_TOKEN` /
`secrets.GRAFANA_PROD_TOKEN` in the new job.

## 6. Browser fallback

If you need to view dashboards interactively, or the headless flow above
isn't available, use the human browser fallback — this requires the real
Chrome session signed in as `ravi@mesha.sg` (or another
`observability_operator_members` principal), since Cloud Run IAM checks the
Google identity behind the request:

```bash
gcloud run services proxy goatos-stg-grafana \
  --project=goatos-stg --region=asia-south1
# then open http://localhost:8080 in a browser signed into an authorized
# Google account — the proxy attaches your ID token to every request.
```

From inside that browser session you can also mint a Grafana API token via
**Administration → Service accounts** in the Grafana UI itself, as an
alternative to the `curl` flow in §2 — functionally identical, just
click-driven instead of scripted.

## 7. Summary — which path to use

| You are | Use |
|---|---|
| A human running one-off PromQL/dashboard queries | §2 headless flow, or §6 browser fallback if you want to click around |
| Claude/Codex answering an observability question | §2 headless flow + §4 MCP wiring |
| A CI workflow (future) | §5 — GitHub environment secret, only once a workflow actually calls Grafana |
| Anyone who just leaked a token | Rotate immediately — see `RUNBOOK.md` |
