# Goat OS STG Deploy Runbook (Canonical Contract)

This is the short, authoritative contract. Full Cloud Deploy mechanics (release
values, custom target, rollout ordering) live in
[`cloud-deploy-staging.md`](./cloud-deploy-staging.md). Org/project boundary
lives in [`google-cloud-environments.md`](./google-cloud-environments.md).

## Non-Deploy Paths (NEVER valid for STG deploy)

- GitHub Actions (billing-blocked FOREVER — validate only via `make ci-local`)
- main→stg PR as a deployment trigger
- GitHub MCP / any GitHub identity as the deploy mechanism
- Slice or Heva GitHub identity for Goat OS
- force-pushing a `stg` branch and waiting for CI
- deploying from a dirty worktree
- deploying from Heva / Slice / system-gsuite / apps-script / goatos-sheets
  projects

## Valid Deploy Path

STG deploy is **manual Google Cloud Deploy**. Cloud Deploy is the deployment
authority for `goatos-stg`. GitHub Actions / Cloud Build / a local operator may
only build images and create a release; Cloud Run staging services and jobs are
mutated by the Cloud Deploy rollout task only.

Scripts:

```bash
tools/deploy/stg-clouddeploy-release.sh   # build/create the release
tools/deploy/stg-clouddeploy-task.sh      # custom-target rollout task
```

Do not hand-write long `RELEASE_ID` values. Cloud Deploy generates rollout ids
from the release id, target, and attempt suffix, and the final rollout id must
fit Google Cloud's 63-character resource-id limit. Use the release helper's
short default (`r-<12-char-sha>-<HHMMSS>`) unless there is a specific reason to
override it.

Before running any deploy command, verify:

```bash
gcloud auth list
gcloud config get-value account
gcloud config get-value project
git rev-parse HEAD
git rev-parse origin/main
git status --short --branch
```

Expected:

- account: `ravi@mesha.sg`
- org: `vgoats.com`
- project / environment: `goatos-stg` (STG)
- source SHA: latest approved `origin/main`
- working tree: clean

Authorized STG release-builder accounts:

```text
ravi@mesha.sg
manohark@mesha.sg
```

Runtime DB URLs, API/admin secrets, service-account bindings, and Cloud Run env
are injected by the existing `goatos-stg` Cloud Run/Secret Manager configuration.
Normal DB schema changes are applied by the Cloud Deploy migration job; do not
run manual SQL for a normal release.

## GitHub Release Tag

Every successful STG release must create an annotated GitHub tag with Backend,
Frontend/Admin Web, Mobile Android, Infra/Deploy, Docs/Seed/Data, and Other
sections. The normal STG release helper does this automatically after rollout
success and image parity verification:

```bash
tools/deploy/stg-clouddeploy-release.sh
```

Manual repair command:

```bash
make release-tag ENV=stg SHA="$(git rev-parse HEAD)" CLOUD_DEPLOY_RELEASE="<release-id>"
```

Do not call a STG release closed until the tag exists on GitHub.

## Migration Drift Guardrail

Never edit a migration file that STG may already have applied, including the
clean-slate baseline. If STG is missing a schema field or data repair, ship a
new numbered forward migration and deploy from `origin/main`.

Before declaring a migration-backed STG fix complete:

```bash
SELECT version, checksum
FROM public.goatos_schema_migrations
ORDER BY version DESC
LIMIT 5;
```

Then verify the exact table/column/data contract that broke the screen or API.
A successful frontend deploy is not proof that the DB migrated. If Cloud Deploy
reports a failed migrate job, read the migrate execution logs first; do not
refresh the UI repeatedly and do not call it a cache issue.

Break-glass manual SQL is allowed only to recover STG availability after the
same migration has landed on `main`. Record the applied migration row with the
file checksum, verify `/readyz`, and follow up with a normal Cloud Deploy
release from the same or newer commit so Cloud Run job definitions converge.

## Chatbot / CEO AI Verification

After backend/admin-web deploy, verify the leadership assistant using:

`docs/runbooks/stg-chatbot-ai-wiring.md`

Do not assume Vertex/Gemini, Cube, or MCP Toolbox are wired merely because the
backend deployed. Report each as deployed, fallback-only, placeholder, or missing.

## GCS / Android Video Proof Verification

After backend/admin deploy, verify Android proof/video upload wiring using:

`docs/runbooks/stg-gcs-video-upload-wiring.md`

Do not assume Android video upload works just because backend deployed.
Report GCS as PASS, FAIL, PARTIAL, or NOT WIRED.

## BUG-041 Policy

BUG-041 is NOT a deployment blocker unless explicitly stated. If BUG-041 remains:

- deploy may proceed
- validation must report the impact
- do not claim vaccination E2E is fully clean
- do not patch BUG-041 inside the deploy task

## STG Seed Closeout Timeouts

STG seed closeout runs over Cloud SQL and may need a larger per-query timeout
than local development. The normal runtime defaults (`GOATOS_PG_QUERY_TIMEOUT`
is `15s` on the API service and `30s` on the worker) are not enough for STG
Cloud SQL seed closeout.

For seed/closeout jobs only, set:

```bash
GOATOS_PG_QUERY_TIMEOUT=60s
```

This is allowed for destructive/admin seed closeout because it generates drive
assignments, memberships, projections, and proof tables over Cloud SQL. Do not
change normal API/runtime query timeouts just to make seed closeout pass. If
closeout needs this timeout, report the slow step and keep it scoped to the
seed command.

## STG Operator Login Credentials

STG Android operator/director test passwords follow:

```text
<FirstName>@2026
```

Canonical credentials and backend binding requirements live in:
`docs/runbooks/stg-operator-login-credentials.md`

Do not invent random passwords during deploy, seed, or Android release tasks.

## STG Vaccination Seed Closeout Contract

> **STG vaccination seed is not complete when commands finish. It is complete
> only when the closeout gate passes.**

Do not promote STG on a "seed commands ran" basis. STG seed is complete only
after a full DB reseed + expected-schedule gate + integrity zeros, all validated
from the DB (not logs). Required checks — membership integrity, lifecycle
mutations (add/death/exit/shed-move/partition-move/missed-cancel-reap/date-
override/operator-cap), read surfaces (Calendar/CT/AC/WF/PA/Vaccination
L1/L2/L3), and CPT seed/catch-up ET+TT-only behavior — plus the failure rule and
proof artifacts live in:

`docs/runbooks/staging-vaccination-clean-slate.md` → "STG Vaccination Seed
Closeout Contract".

Any closeout failure blocks STG promotion and must be logged in
`review_bugs_ledgers.md` before a retry, which is a full reseed, not a patch.

## STG Login Seed Contract

After reseed, verify login readiness using:

`docs/runbooks/stg-login-seed-contract.md`

The STG seed recipes run `make seed-stg-postflight` after credentials and DB
grants. That postflight is the executable check for GCS proof storage, FCM
notification config, CEO AI/chatbot runtime config/secrets, and materialized
login grants. A seed is not complete if this target fails.

Field Android users are email/password. Leadership users are Google SSO **and**
Firebase email/password.

### Canonical STG Personnel Rule (10 people total)

10 STG people total: **5 Mesha leadership (Google SSO and Firebase
email/password, `ceo_internal`, NO vaccination capacity)** + **4 field users
(Firebase email/password, password `<FirstName>@2026`)** + **1 verifier
(Firebase email/password, password `Jyothi@2026`, NO vaccination capacity)**.

| Person | Auth | Role | Adds vaccination capacity? |
|---|---|---|---|
| Amit Kumar | Firebase `Amit@2026` | operator | **yes** |
| Darshan Talwar | Firebase `Darshan@2026` | operator, default vaccination operator | **yes** |
| Sagar Mahoor | Firebase `Sagar@2026` | operator, fallback vaccination operator | **yes** |
| Chandrakant | Firebase `Chandrakant@2026` | **director** | **no** |
| Jyothi | Firebase `Jyothi@2026` | verifier | **no** |
| 5 Mesha leadership | Google SSO or Firebase `<FirstName>@2026` | `ceo_internal` | **no** |

ONLY Amit + Darshan + Sagar count toward vaccination operator animal capacity.
Firebase allowlist alone / Firebase user existing is NOT enough: backend grant
AND `/app/bootstrap` context must pass for all field and leadership users.

> **STG seed is FAIL** unless Amit, Darshan, and Sagar appear as HRMS/vaccination
> operators with capacity, Chandrakant appears as director, Jyothi has verifier
> login/grant readiness, and the 5 Mesha leadership users are `ceo_internal`
> with both Google SSO and Firebase password login available.
