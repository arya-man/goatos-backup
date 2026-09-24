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

## 3b. Claude access (deploy flag `ASK_MESHA_CLAUDE_AUTH`, Cloud Build `_ASK_MESHA_CLAUDE_AUTH`)

**auto (default):** the service gets both the Vertex env (below) and `ANTHROPIC_API_KEY` from
`goatos-stg-ask-mesha-anthropic-api-key` (§4; the runtime SA needs `secretAccessor` on it). It
probes Vertex (one `rawPredict`, `max_tokens` 1, token from the metadata server) at startup and every
15 min while on the key; once Vertex answers (quota approved) new questions use Vertex with no
redeploy, and it re-checks hourly. A Vertex 429/403/404 during a question marks Vertex down and reruns
that question once on the key (only if nothing was shown yet). Logs: `[provider] switched to vertex`;
every metric/event row carries `provider` (`vertex` | `anthropic`); `/healthz` shows the current one:

```bash
curl -s -H "Authorization: Bearer $(gcloud auth print-identity-token)" "$URL/healthz"
# {"ok":true,"provider":"anthropic","claude":{"mode":"auto","vertex_ok":false,"last_probe_reason":"quota_429",...}}
```

Force one provider (a config change -> new revision of the same image, not a build):

```bash
gcloud run services update goatos-ask-mesha-stg --project=$PROJECT --region=asia-south1 \
  --update-env-vars=ASK_MESHA_CLAUDE_AUTH=vertex   # or api-key; back to auto the same way
```

After Vertex is live and `/healthz` has shown `"provider":"vertex"` for a day, retire the key:
set `ASK_MESHA_CLAUDE_AUTH=vertex` as above, then disable the key
(`gcloud secrets versions disable 1 --secret=goatos-stg-ask-mesha-anthropic-api-key --project=$PROJECT`)
and revoke it in the Anthropic Console. Future builds then need `_ASK_MESHA_CLAUDE_AUTH=vertex`
(deploy preflight refuses auto/api-key without an enabled key version). The $100 monthly cap (§3c) is
one cap summed across both providers.

**vertex:** no key; Claude is billed to the GCP project.

```bash
# Enable Claude Sonnet 5 / Opus 5.5 once in Console → Vertex AI → Model Garden (accept terms), then:
gcloud services enable aiplatform.googleapis.com --project=$PROJECT
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:goatos-ask-mesha-stg@$PROJECT.iam.gserviceaccount.com" --role=roles/aiplatform.user
```

Region: `ASK_MESHA_VERTEX_REGION` defaults to **`global`**. Verified 2026-09-24 against the Vertex publisher-model API:
`claude-sonnet-5` and `claude-opus-5-5` are GA **only on `global`** (404 in `asia-south1` and `us-east5`).
`global` routes inference to available capacity (not pinned to India); chats, files and the database stay in asia-south1.

**Quota (blocking):** a new project has 0 quota for Claude on Vertex. A test call on goatos-stg returned
`429 Quota exceeded for aiplatform.googleapis.com/global_online_prediction_requests_per_base_model (anthropic-claude-sonnet)`.
Console -> IAM & Admin -> Quotas -> filter `global_online_prediction_requests_per_base_model` for base models
`anthropic-claude-sonnet` and `anthropic-claude-opus` -> request e.g. 60 requests/min each, and accept both models'
terms in Model Garden. Verify (expect the text `OK`):

```bash
curl -s -X POST -H "Authorization: Bearer $(gcloud auth print-access-token)" -H "x-goog-user-project: $PROJECT" \
  -H "Content-Type: application/json" \
  "https://aiplatform.googleapis.com/v1/projects/$PROJECT/locations/global/publishers/anthropic/models/claude-sonnet-5:rawPredict" \
  -d '{"anthropic_version":"vertex-2023-10-16","max_tokens":5,"messages":[{"role":"user","content":"Reply OK"}]}'
```

**api-key:** Anthropic Console key (set a monthly spend limit there) → secret `goatos-stg-ask-mesha-anthropic-api-key` (§4).

**oauth:** `claude setup-token` token → secret `goatos-stg-ask-mesha-claude-oauth-token`. Personal
Pro/Max plans are for the subscriber's own use; a service used by several people should use
vertex or api-key instead.

## 3c. Spend cap ($100/month)

The agent enforces the cap itself: once this month's summed answer cost reaches
`ASK_MESHA_MONTHLY_BUDGET_USD` (default 100) new questions get a "budget reached" reply
without calling Claude; `ASK_MESHA_PER_ANSWER_BUDGET_USD` (default 1) aborts a single
runaway answer. GCP budgets only alert (they never stop spend), so add one as a backstop:

```bash
BILLING=$(gcloud billing projects describe $PROJECT --format='value(billingAccountName)' | sed 's#billingAccounts/##')
gcloud billing budgets create --billing-account=$BILLING \
  --display-name="Ask Mesha Claude (Vertex) ~\$100" --budget-amount=8800INR \
  --filter-projects=projects/$PROJECT --filter-services=services/C7E2-9256-1C43 \
  --threshold-rule=percent=0.5 --threshold-rule=percent=0.8 --threshold-rule=percent=1.0
```

(Billing account 01FEDE-96BCB3-76D992 is INR: a USD amount is rejected with INVALID_ARGUMENT, and
`--filter-services` needs the billing service ID (`C7E2-9256-1C43` = Vertex AI), not the API name.
That filter covers all Vertex AI use in the project; Ask Mesha is the only Claude user today.)
With `auto` or `api-key`, also set a $100 monthly limit in the Anthropic Console.

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
# api-key mode only: paste the key into stdin, then Ctrl-D
gcloud secrets create goatos-stg-ask-mesha-anthropic-api-key --project=$PROJECT \
  --replication-policy=automatic --data-file=-
printf 'postgresql://ask_mesha:%s@/ask_mesha?host=/cloudsql/%s:%s:%s' \
  "$ASK_PW" $PROJECT $REGION $INSTANCE | \
  gcloud secrets create goatos-stg-ask-mesha-db-url --project=$PROJECT \
  --replication-policy=automatic --data-file=-
unset ASK_PW
# Only the secrets the chosen auth mode uses (vertex: no Claude secret at all).
SECRETS="goatos-stg-ask-mesha-db-url mesha-ceo-readonly-db-url"
# api-key: SECRETS="$SECRETS goatos-stg-ask-mesha-anthropic-api-key"
# oauth:   SECRETS="$SECRETS goatos-stg-ask-mesha-claude-oauth-token"
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
