# External MCP staging deploy scaffold

This scaffold is for the external leadership-assistant MCP facade in
`goatos-stg`, `asia-south1`. It exposes a small MCP-compatible HTTP surface and
proxies tool calls to the existing Goat OS API `/ceo-ai/ask` path, so the API
keeps ownership of auth, tenant scope, audit, Cube, Toolbox, SQL guard, and
read-model policy.

It follows the existing Goat OS staging patterns:

- Cloud Run service is Terraform-owned.
- Runtime identity is a dedicated service account.
- Secret values live in Secret Manager; source only declares names and access.
- The service is built from the existing backend image and runs `/app/bin/mcp`.
- It does not mount Cloud SQL or read database secrets directly; it calls the
  API, and the API keeps the existing Cloud SQL/Secret Manager surface.

## Target

```text
Project:          goatos-stg
Region:           asia-south1
Service:          goatos-mcp-stg
Artifact image:   asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:<tag>
Service account:  goatos-mcp-stg@goatos-stg.iam.gserviceaccount.com
Ingress:          public Cloud Run invoker, application-gated by bearer token
Secret:            goatos-stg-auth-allowed-emails -> MESHA_MCP_ALLOWED_EMAILS
Upstream:          GOATOS API /ceo-ai/ask
```

## Normal release path

The regular staging release remains the Slack button in `#goatos-stg-deploy`,
backed by Cloud Build trigger `goatos-stg-deploy-main` and Cloud Deploy. That
release already builds and pushes the backend image used by `goatos-mcp-stg`.

Before any direct cloud command, verify:

```bash
gcloud config get-value account
gcloud config get-value project
```

Expected project is `goatos-stg`; expected region is `asia-south1`; expected
repo is `vgoats/goatos`.

## Terraform boundary

Terraform owns creation of:

- `google_cloud_run_v2_service.mcp`
- `google_cloud_run_v2_service_iam_member.mcp_public_invoker`
- runtime service account key `mcp`
- Secret Manager accessor for `goatos-stg-auth-allowed-emails`

Use Terraform only to create or change the service shape. Use Cloud Deploy for
normal image promotion.

Plan from a verified `goatos-stg` context:

```bash
terraform -chdir=infra/envs/stg plan
```

## Smoke check

After rollout, verify the Cloud Run revision and health endpoint:

```bash
gcloud run services describe goatos-mcp-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --format='value(status.url,status.latestReadyRevisionName)'
```

Then smoke `/readyz`, followed by an MCP `tools/list` request. Real
`tools/call` requests must include the caller bearer token and
`X-GoatOS-Tenant-ID`; the MCP service verifies the token email against the
leadership allowlist before it reaches the upstream API.
