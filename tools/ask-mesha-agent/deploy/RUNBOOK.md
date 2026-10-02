# Ask Mesha agent on Cloud Run (STG) — one-time setup, deploy, rollback

Service `goatos-ask-mesha-stg` (asia-south1, project `goatos-stg`) runs
`tools/ask-mesha-agent` from image
`asia-south1-docker.pkg.dev/goatos-stg/goatos/ask-mesha-agent:<sha12>`, built and
deployed by the existing `cloudbuild.stg.yaml` path (steps
`build-ask-mesha-agent-image`, `deploy-ask-mesha-agent` →
`tools/ask-mesha-agent/deploy/deploy-stg.sh`). Both are no-ops unless the build
sets `_ASK_MESHA_DEPLOY=true`, so the pipeline change lands before this infra.

Nothing below has been run. Ravi reviews and runs it as `ravi@mesha.sg`.

## 0. Preconditions

```bash
gcloud config get-value account   # ravi@mesha.sg
export PROJECT=goatos-stg REGION=asia-south1
export SA=goatos-ask-mesha-stg@${PROJECT}.iam.gserviceaccount.com
export DEPLOYER=goatos-github-deploy-stg@${PROJECT}.iam.gserviceaccount.com
export ADMIN_WEB_SA=goatos-admin-web-stg@${PROJECT}.iam.gserviceaccount.com  # verified via run services describe
export INSTANCE=goatos-stg-core-db BUCKET=goatos-stg-ask-mesha
```

## 1. Runtime service account (least privilege)

```bash
gcloud iam service-accounts create goatos-ask-mesha-stg --project=$PROJECT \
  --display-name="Ask Mesha agent (STG)"
gcloud projects add-iam-policy-binding $PROJECT \
  --member=serviceAccount:$SA --role=roles/cloudsql.client --condition=None
```

## 2. Uploads bucket (private, uniform access, 180-day lifecycle)

```bash
gcloud storage buckets create gs://$BUCKET --project=$PROJECT --location=$REGION \
  --uniform-bucket-level-access --public-access-prevention
cat > /tmp/ask-mesha-lifecycle.json <<'JSON'
{"rule":[{"action":{"type":"Delete"},"condition":{"age":180}}]}
JSON
gcloud storage buckets update gs://$BUCKET --lifecycle-file=/tmp/ask-mesha-lifecycle.json
gcloud storage buckets add-iam-policy-binding gs://$BUCKET \
  --member=serviceAccount:$SA --role=roles/storage.objectAdmin
```

## 3. Writable chat-storage DB (separate from `mesha_ceo_readonly`)

```bash
gcloud sql databases create ask_mesha --instance=$INSTANCE --project=$PROJECT
ASK_PW="$(openssl rand -base64 32 | tr -d '/+=\n')"
gcloud sql users create ask_mesha --instance=$INSTANCE --project=$PROJECT --password="$ASK_PW"
```

As the instance admin, keep `ask_mesha` confined to its own database:

```sql
-- connected to database ask_mesha
REVOKE ALL ON DATABASE ask_mesha FROM PUBLIC;
GRANT CONNECT, CREATE, TEMP ON DATABASE ask_mesha TO ask_mesha;
-- run the CREATE SCHEMA *as ask_mesha* (goatos_app cannot SET ROLE ask_mesha on PG16):
CREATE SCHEMA IF NOT EXISTS ask_mesha AUTHORIZATION ask_mesha;
-- connected to database goatos (no-op in practice: PUBLIC holds CONNECT on goatos, so ask_mesha can
-- still connect; it has SELECT on 0 tables there, verified 2026-09-24)
REVOKE CONNECT ON DATABASE goatos FROM ask_mesha;
```

## 3b. Model access: Gemini Developer API with an AI Studio key (prepaid)

The agent calls Gemini on the Gemini Developer API (`generativelanguage.googleapis.com`) with an AI Studio API
key, so usage bills the maintainer's PREPAID AI Studio credits. Vertex AI is never used for Gemini (postpay billing);
`test/no-vertex-gemini.test.mjs` fails CI on any Vertex Gemini call.

One-time setup (maintainer): create the key in the prepaid AI Studio project, then store it (value from stdin, never echoed):

```bash
gcloud secrets create goatos-stg-ask-mesha-gemini-api-key --project=$PROJECT --replication-policy=automatic --data-file=-
gcloud secrets add-iam-policy-binding goatos-stg-ask-mesha-gemini-api-key --project=$PROJECT \
  --member="serviceAccount:goatos-ask-mesha-stg@$PROJECT.iam.gserviceaccount.com" --role=roles/secretmanager.secretAccessor
gcloud secrets add-iam-policy-binding goatos-stg-ask-mesha-gemini-api-key --project=$PROJECT \
  --member="serviceAccount:$DEPLOYER" --role=roles/secretmanager.viewer
```

`deploy-stg.sh` mounts it as `GEMINI_API_KEY` and refuses to deploy when the secret has no enabled version or the runtime
SA cannot read it (it never grants). Without the key the service refuses to start (no fallback of any kind). The same secret
feeds the backend ceo-ai planner (`MESHA_GEMINI_API_KEY` on goatos-api-stg, Terraform `api_gemini_api_key_accessor`).

Models (env, all optional; defaults live in `gemini.mjs`):

| Env | Default | Used for |
|---|---|---|
| `ASK_MESHA_MODEL` | `gemini-3.1-pro-preview` (newest Pro) | every answer |
| `ASK_MESHA_DEEP_MODEL` | = `ASK_MESHA_MODEL` | investigations (`deep:`, screenshots); thinking level high |
| `ASK_MESHA_FAST_MODEL` / `ASK_MESHA_CHECK_MODEL` | `gemini-3.8-flash` (newest Flash) | answer checker, 429 fallback |

Verify a model id before changing it (expect HTTP 200; the key goes in a header, never the URL):

```bash
M=gemini-3.1-pro-preview
curl -s -X POST -H "x-goog-api-key: $GEMINI_API_KEY" -H "Content-Type: application/json" \
  "https://generativelanguage.googleapis.com/v1beta/models/${M}:generateContent" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Reply OK"}]}]}'
curl -s -H "x-goog-api-key: $GEMINI_API_KEY" "https://generativelanguage.googleapis.com/v1beta/models?pageSize=200" | grep '"name"'
```

(zsh: write `${M}` with braces.) Switch model without a build: `gcloud run services update goatos-ask-mesha-stg
--project=$PROJECT --region=asia-south1 --update-env-vars=ASK_MESHA_MODEL=<id>`; for builds set `_ASK_MESHA_MODEL`.
`/healthz` returns `{"ok":true,"provider":"gemini","model":"<id>"}`. Spend and credit balance: AI Studio billing page.

## 3c. Spend cap

Gemini spend is prepaid: the AI Studio credit balance is the hard backstop (calls fail with 429 when it runs out). The GCP budget below only covered Vertex and is kept for history.
 ($100/month)

The agent enforces the cap itself: once this month's summed answer cost reaches
`ASK_MESHA_MONTHLY_BUDGET_USD` (default 100) new questions get a "budget reached" reply
without calling the model; `ASK_MESHA_PER_ANSWER_BUDGET_USD` (default 1) and, for investigations,
`ASK_MESHA_DEEP_ANSWER_BUDGET_USD` (default 5) abort a single runaway answer. GCP budgets only alert (they never stop spend), so add one as a backstop:

```bash
BILLING=$(gcloud billing projects describe $PROJECT --format='value(billingAccountName)' | sed 's#billingAccounts/##')
gcloud billing budgets create --billing-account=$BILLING \
  --display-name="Ask Mesha (legacy Vertex) ~\$100" --budget-amount=8800INR \
  --filter-projects=projects/$PROJECT --filter-services=services/C7E2-9256-1C43 \
  --threshold-rule=percent=0.5 --threshold-rule=percent=0.8 --threshold-rule=percent=1.0
```

(Billing account 01FEDE-96BCB3-76D992 is INR: a USD amount is rejected with INVALID_ARGUMENT, and
`--filter-services` needs the billing service ID (`C7E2-9256-1C43` = Vertex AI), not the API name.
That filter covers all Vertex AI use in the project, not only Ask Mesha.)

## 3d. Harden the read-only DB role (required before enabling)

`mesha_ceo_readonly` must not be able to reach other roles/hosts. The `dblink` extension is
installed and executable by PUBLIC; revoke it (as the DB owner / cloudsqlsuperuser):

```sql
REVOKE EXECUTE ON FUNCTION dblink(text), dblink(text,boolean), dblink(text,text), dblink(text,text,boolean),
  dblink_exec(text), dblink_exec(text,boolean), dblink_exec(text,text), dblink_exec(text,text,boolean),
  dblink_connect(text), dblink_connect(text,text) FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
-- verify: should all be false
SELECT has_function_privilege('mesha_ceo_readonly', 'dblink_exec(text,text)', 'EXECUTE'),
       has_schema_privilege('mesha_ceo_readonly', 'public', 'CREATE');
```

Check what else uses dblink/public CREATE first (`\df dblink*`, app roles) so the revoke doesn't
break another service. After deploy, confirm in the container that the agent's `Read /proc/1/environ`
and `Read <state>/.pgenv` are denied.

## 3e. `mesha_ceo_readonly` SELECT grants (NOT in any migration yet)

Granted by hand on goatos-stg 2026-09-24; a fresh/restored DB will not have them and `run_sql`
will fail with `permission denied`. Run as the owner of those tables (the migration owner),
inside the repo's audited change wrapper:

```sql
-- connected to database goatos, as the table owner
BEGIN;
SELECT audit.begin_change('ravi via claude', 'ask-mesha: mesha_ceo_readonly read grants');
DO $$
DECLARE s text;
BEGIN
  FOREACH s IN ARRAY ARRAY['public','analytics','audit','ceo_ai','forensic_repair'] LOOP
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO mesha_ceo_readonly', s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO mesha_ceo_readonly', s);
    EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO mesha_ceo_readonly', s);
    -- future tables created by THIS role (run once per role that creates tables)
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT ON TABLES TO mesha_ceo_readonly', s);
  END LOOP;
END $$;
COMMIT;
-- verify: 0 rows = every table readable, and no write privilege anywhere
SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE c.relkind IN ('r','p','v','m') AND n.nspname IN ('public','analytics','audit','ceo_ai','forensic_repair')
   AND NOT has_table_privilege('mesha_ceo_readonly', c.oid, 'SELECT');
SELECT count(*) FROM information_schema.role_table_grants
 WHERE grantee = 'mesha_ceo_readonly' AND privilege_type IN ('INSERT','UPDATE','DELETE','TRUNCATE');  -- 0
```

Proposal: move these into a migration (idempotent GRANTs are safe to replay) so every env and
restore gets them and CI can assert the no-write invariant. Keep the role's *password* out of
migrations. Single-tenant only: before a 2nd tenant, replace this blanket grant with the
per-tenant `ceo_readers` + RLS plan in `docs/agent-rules/ask-mesha.md` ("Multi-tenant isolation").

**Chat privacy (required, right after the grant):** each CEO may read only their own chats. The read-everything
role must NOT see any assistant chat history:

```sql
REVOKE SELECT ON public.ceo_ai_conversations, public.ceo_ai_messages, public.ceo_ai_assistant_audit,
  public.ceo_ai_response_cache, public.ceo_ai_rate_limit FROM mesha_ceo_readonly;
-- never GRANT the ask_mesha schema (chat store) to mesha_ceo_readonly
-- verify: every row must be false
SELECT t, has_table_privilege('mesha_ceo_readonly', t, 'SELECT')
  FROM unnest(ARRAY['public.ceo_ai_conversations','public.ceo_ai_messages','public.ceo_ai_assistant_audit',
                    'public.ceo_ai_response_cache','public.ceo_ai_rate_limit']) AS t;
```
**Order matters:** re-running the §3e grant block (`GRANT SELECT ON ALL TABLES`) re-grants these
tables, so always re-run this REVOKE + verify right after it. The `ALTER DEFAULT PRIVILEGES` in §3e
also makes every *future* table readable, including any new assistant/chat/credential table: a
migration that adds one must `REVOKE SELECT ... FROM mesha_ceo_readonly` in the same migration.
`audit.db_changes` stores old/new rows of manual changes; never hand-edit chat tables under
`audit.begin_change` or the rows become readable there.
(Applied on goatos-stg 2026-09-24 via audit.begin_change.) Per-user isolation of the agent's own chats is
enforced by the service (email + tenant ownership on every chat/file route).

## 3f. Events table + Grafana

`ASK_MESHA_DB_MIGRATE=1` (set by `deploy-stg.sh`) applies `sql/001_init.sql` (store) and
`sql/002_events.sql` (`ask_mesha.events`) at start, as the `ask_mesha` user. Grafana read role and
datasource: `deploy/grafana/README.md` (events-only SELECT, uid `ask-mesha-postgres`).

## 4. Secrets (values piped from stdin, never echoed)

```bash
printf 'postgresql://ask_mesha:%s@/ask_mesha?host=/cloudsql/%s:%s:%s' \
  "$ASK_PW" $PROJECT $REGION $INSTANCE | \
  gcloud secrets create goatos-stg-ask-mesha-db-url --project=$PROJECT \
  --replication-policy=automatic --data-file=-
unset ASK_PW
# Secrets the service mounts (the Gemini key secret is created by the maintainer, §3b).
SECRETS="goatos-stg-ask-mesha-db-url mesha-ceo-readonly-db-url goatos-stg-ask-mesha-gemini-api-key"
for s in $SECRETS; do
  gcloud secrets add-iam-policy-binding $s --project=$PROJECT \
    --member=serviceAccount:$SA --role=roles/secretmanager.secretAccessor
  # deploy-stg.sh preflight describes every secret it mounts
  gcloud secrets add-iam-policy-binding $s --project=$PROJECT \
    --member=serviceAccount:$DEPLOYER --role=roles/secretmanager.viewer
done
```

`mesha-ceo-readonly-db-url` already exists; `deploy/entrypoint.sh` turns it
(URL or `host=... user=...` DSN) into `$ASK_MESHA_STATE_DIR/.pgenv` at startup.

## 5. Deployer + invoker permissions

```bash
# Cloud Build deployer may deploy the service and act as its runtime SA.
gcloud iam service-accounts add-iam-policy-binding $SA --project=$PROJECT \
  --member=serviceAccount:$DEPLOYER --role=roles/iam.serviceAccountUser
# (deployer already holds roles/run.developer|admin for the existing services;
#  confirm: gcloud projects get-iam-policy $PROJECT --flatten=bindings --filter="bindings.members:$DEPLOYER")
# The preflight reads SA/bucket metadata (secret viewer grants are in §4):
gcloud storage buckets add-iam-policy-binding gs://$BUCKET \
  --member=serviceAccount:$DEPLOYER --role=roles/storage.legacyBucketReader
```

After the first deploy creates the service:

```bash
gcloud run services add-iam-policy-binding goatos-ask-mesha-stg --project=$PROJECT --region=$REGION \
  --member=serviceAccount:$ADMIN_WEB_SA --role=roles/run.invoker
```

## 6. Deploy

Order from zero: §0 → §1 → §2 → §3 → §3b → §3c → §3d → §3e → §4 → §5 (deployer part) → first
run below → §5 invoker grant → verify → second run → verify in admin-web → §3f Grafana.

Trigger the normal `goatos-stg-deploy-main` build (launcher / Slack) with the
extra substitutions:

1. First run: `_ASK_MESHA_DEPLOY=true` (service only). Grant `run.invoker` (§5).
2. Second run: `_ASK_MESHA_DEPLOY=true,_ASK_MESHA_WIRE_ADMIN_WEB=true` — sets
   `CEO_AI_AGENT_URL` and `CEO_AI_AGENT_AUDIENCE` (both = service URL) on
   `goatos-admin-web-stg` with `--update-env-vars` and shifts traffic to that
   revision. Cloud Deploy releases also use `--update-env-vars`, so the wiring
   survives later deploys. (If Terraform `infra/envs/stg` is applied to
   `admin_web`, add the two env vars there too or it will drop them.)

### 6b. Hosted MCP (`ask_goatos` on https://mcp.mesha.sg/mcp)

Third, optional run: `_ASK_MESHA_DEPLOY=true,_ASK_MESHA_WIRE_MCP=true` (can be combined with
`_ASK_MESHA_WIRE_ADMIN_WEB=true`). `deploy-stg.sh` then:

1. grants `roles/run.invoker` on `goatos-ask-mesha-stg` to the MCP runtime SA
   `goatos-mcp-stg@goatos-stg.iam.gserviceaccount.com` (the deployer needs
   `run.services.setIamPolicy` on the agent service, i.e. `roles/run.admin`; `run.developer` is not enough);
2. updates `goatos-mcp-stg` with `--timeout=300` and `--update-env-vars`
   `MESHA_MCP_AGENT_URL`, `MESHA_MCP_AGENT_AUDIENCE` (both = agent URL) and `MESHA_MCP_AGENT_TIMEOUT=240s`.

The MCP then calls `${MESHA_MCP_AGENT_URL}/ceo-ai/ask` with `stream:false`, a metadata-server ID token
in `X-Serverless-Authorization`, the CEO's own bearer in `Authorization` (the agent re-checks it against
the STG API) and `X-Mesha-Client: mcp` (events carry `source:"mcp"`). Unset vars = legacy `/ceo-ai/ask`.

`goatos-mcp-stg` is Terraform-owned (`infra/envs/stg/cloud_run_services.tf`, which now also sets
`timeout = "300s"`). A later `terraform apply` of that service drops the two env vars; ask_goatos then
falls back to the legacy path (safe). Re-run this wiring after such an apply.

Verify: in Claude (connector `mcp.mesha.sg`) ask "why did ADG drop last week in Castro?"; expect an
analyst answer within a few minutes ending `Source: Ask Mesha agent` + `Conversation: <id>`, and an
`ask_completed` row with `row->>'source' = 'mcp'` in `ask_mesha.events`.

Rollback: `gcloud run services update goatos-mcp-stg --project=$PROJECT --region=$REGION
--remove-env-vars=MESHA_MCP_AGENT_URL,MESHA_MCP_AGENT_AUDIENCE,MESHA_MCP_AGENT_TIMEOUT --quiet`.

### Verify (read-only)

```bash
gcloud run services describe goatos-ask-mesha-stg --project=$PROJECT --region=$REGION \
  --format='value(status.url,metadata.labels.commit_sha,status.latestReadyRevisionName)'
gcloud run services logs read goatos-ask-mesha-stg --project=$PROJECT --region=$REGION --limit=50
#   expect: "wrote N PG settings for user mesha_ceo_readonly", no migrate / pg errors
URL=$(gcloud run services describe goatos-ask-mesha-stg --project=$PROJECT --region=$REGION --format='value(status.url)')
curl -s -o /dev/null -w '%{http_code}\n' "$URL/ceo-ai/starters"   # 403 (IAM), never 200
```

```sql
-- database ask_mesha
SELECT to_regclass('ask_mesha.events');                    -- not null
SELECT event_name, count(*) FROM ask_mesha.events WHERE ts > now() - interval '1 hour' GROUP BY 1;
```

After wiring admin-web: open Ask Mesha in admin-web STG, ask a quick question (streams, answer
arrives, `ask_completed` row) and a deep one (Opus, up to ~2 min, stream keeps alive via 10s pings,
cost ≤ $5 in the event row). Confirm a SQL write (`DELETE ...`) is refused by the DB.

## Rollback

Fastest (admin-web back to the backend `/ceo-ai/*`):

```bash
gcloud run services update goatos-admin-web-stg --project=$PROJECT --region=$REGION \
  --remove-env-vars=CEO_AI_AGENT_URL,CEO_AI_AGENT_AUDIENCE --no-traffic --quiet
REV=$(gcloud run services describe goatos-admin-web-stg --project=$PROJECT --region=$REGION \
  --format='value(status.latestCreatedRevisionName)')
gcloud run services update-traffic goatos-admin-web-stg --project=$PROJECT --region=$REGION \
  --to-revisions=$REV=100 --quiet
```

Bad agent image only: `gcloud run services update-traffic goatos-ask-mesha-stg
--to-revisions=<previous-rev>=100`. Stop spend: set `--min-instances=0` or
delete the service; the bucket and `ask_mesha` DB keep chat history.
