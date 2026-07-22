# Leadership Assistant ("CEO AI") — Secrets & Config Runbook

**Status:** local-runnable today; staging read-only DB roles are a **pending
deploy step** (secrets seeded with clearly-marked placeholders). This runbook is
the single source of truth for where every leadership-assistant credential and
config value lives, how to seed/rotate it, and how any developer runs the stack
from a clean clone.

## Source-of-truth model

| Layer | Holds | Never holds |
| --- | --- | --- |
| **Google Secret Manager** (`goatos-stg`, org `vgoats.com`) | all credential VALUES (readonly DB URLs, Cube API secret) + the toolset config entry | — |
| **GitHub Actions secrets** (`vgoats/goatos`) | the same secrets CI needs for eval/integration jobs | — |
| **`.env.ceo-ai.local`** (gitignored) | a local materialization for dev only | is never committed |
| **`.env.ceo-ai.local.example`** (committed) | placeholders + where the real value comes from | any real value |
| **Plain config** (docs/env) | Vertex project/location/model, Cube URL, Toolbox URL | credentials |

Rule: **credentials MUST come from Secret Manager; low-sensitivity config may be
plain env.** No secret VALUE is ever committed — only names + retrieval steps.

## Secret Manager entries (project `goatos-stg`)

Labelled `app=ceo-ai`. List names (values are never printed) with:

```bash
gcloud secrets list --project=goatos-stg --filter='labels.app=ceo-ai' \
  --format='table(name,labels.kind)'
```

| Secret name | Kind | Value today | Consumers |
| --- | --- | --- | --- |
| `mesha-ceo-readonly-db-url` | credential | **PLACEHOLDER** (stg role pending) | backend SQL fallback, MCP Toolbox |
| `mesha-cube-readonly-db-url` | credential | **PLACEHOLDER** (stg role pending) | Cube Core |
| `mesha-cube-api-secret` | credential | real (random 256-bit, generated) | backend (signs Cube JWTs), Cube Core |
| `mesha-mcp-toolset` | config | `mesha_ceo_toolset` | MCP Toolbox, backend |

### Accessor grants (least privilege)

`roles/secretmanager.secretAccessor` is granted per secret to the exact service
account that needs it:

- **`goatos-api-stg`** (backend) — all four entries (it routes Cube/Toolbox/SQL).
- **`mesha-cube-stg`** (Cube Cloud Run) — `mesha-cube-readonly-db-url`,
  `mesha-cube-api-secret`. *Grant is applied once this SA exists (pending deploy).*
- **`mesha-mcp-toolbox-stg`** (Toolbox Cloud Run) — `mesha-ceo-readonly-db-url`,
  `mesha-mcp-toolset`. *Pending deploy.*

Re-running `tools/dev/setup-ceo-ai-secrets.sh` re-applies grants and skips SAs
that do not exist yet with a printed notice.

### Pending: real staging readonly credentials

The live Cloud SQL roles `mesha_ceo_readonly` / `mesha_cube_readonly` and their
staging DSNs are **not yet provisioned**. The two `*-db-url` secrets hold a
placeholder so wiring, IAM, and CI are provable now without fabricating a working
staging credential. When the staging roles are created, write the real values
(a new secret version) WITHOUT committing them:

```bash
MESHA_CEO_READONLY_DB_URL='postgres://mesha_ceo_readonly:...@/goatos?host=/cloudsql/...' \
MESHA_CUBE_READONLY_DB_URL='postgres://mesha_cube_readonly:...@/goatos?host=/cloudsql/...' \
  tools/dev/setup-ceo-ai-secrets.sh
```

## GitHub Actions secrets (`vgoats/goatos`)

Set for CI eval/integration jobs (names only):

- `MESHA_CUBE_API_SECRET`
- `MESHA_MCP_TOOLSET`
- `MESHA_CEO_READONLY_DB_URL` (placeholder until stg role deploys)
- `MESHA_CUBE_READONLY_DB_URL` (placeholder until stg role deploys)

These are written with the Mesha PAT (`$MESHA_GITHUB_PAT`, account `ravimesha`) —
never a Heva/Slice `gh` account. Note: remote GitHub Actions is billing-blocked;
`make ci-local` is the authoritative gate. These CI secrets are staged for when
Actions is restored / for hosted eval jobs.

## Scripts

| Script | Purpose |
| --- | --- |
| `tools/dev/setup-ceo-ai-secrets.sh` | Idempotently create/label the Secret Manager entries, seed placeholders, generate the Cube API secret, and apply least-privilege IAM. Refuses to run unless account=`ravi@mesha.sg`, project=`goatos-stg`, org=`vgoats.com`. |
| `tools/dev/fetch-ceo-ai-secrets.sh` | Local dev: pull the config from Secret Manager into a gitignored `.env.ceo-ai.local`. Keeps a working local DSN over a remote placeholder unless `--force-remote`. |
| `tools/dev/setup-ceo-ai-local-role.sh` | Purely-local alternative: mint throwaway `mesha_ceo_readonly` / `mesha_cube_readonly` roles against the local Postgres and write their DSNs to `.env.ceo-ai.local`. No Secret Manager access needed. |

### Which do I run?

- **Local stack, no staging access:** `setup-ceo-ai-local-role.sh` (mints local
  readonly roles + local DSNs) then supply `MESHA_CUBE_API_SECRET` locally.
- **Local stack mirroring staging config:** `fetch-ceo-ai-secrets.sh` (pulls the
  Cube API secret + toolset; DB URLs stay placeholder until the stg role exists,
  so combine with the local-role script for working DSNs).

## Rotation

- **Cube API secret:** `tools/dev/setup-ceo-ai-secrets.sh --rotate-cube-api-secret`
  (writes a new Secret Manager version), then re-mirror to GitHub and redeploy
  Cube + backend so both sign/verify with the new secret.
- **Readonly DB creds:** rotate the Cloud SQL role password, then write the new
  DSN as a new secret version via the env-var form above.

## Guardrails

- No secret value is ever printed by these scripts (values pass via stdin /
  `--data-file=-`, never argv).
- `.env.ceo-ai.local` is gitignored (`.gitignore`); `.env.ceo-ai.local.example`
  is force-tracked as a committed template with placeholders only.
- Org guard in `setup-ceo-ai-secrets.sh` hard-stops on the wrong account/project/org.

## Related

- `docs/runbooks/cube-local.md` — run Cube locally (`tools/dev/run-cube-local.sh`).
- `docs/runbooks/mcp-toolbox-local.md` — run MCP Toolbox locally.
- `docs/runbooks/google-cloud-environments.md` — gcloud/Cloud SQL context.
- `docs/ceo-ai/` — assistant purpose, routing, and tool contracts.
