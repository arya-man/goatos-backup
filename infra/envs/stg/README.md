# goatos-stg Terraform And No-Deploy Handoff

Status: staging Terraform support is committed as source only. No Terraform
init/apply, image push, migration, Cloud Run deploy, DNS/LB change, or Secret
Manager value write was run when this environment was authored.

## Verified Target

```text
Organization: vgoats.com / organizations/563962826703
Folder:       goat-os / folders/188649904255
Project:      goatos-stg
Project no:   514832198871
Region:       asia-south1
Backend:      gs://goatos-stg-tf-state/terraform/stg
```

The backend bucket is declared in `backend.tf` but still must be bootstrapped
imperatively before a real backend init, using the same locked-down posture as
dev: uniform bucket-level access, public access prevention, and object
versioning. Do not store goat data, images, secrets, app artifacts, or migration
payloads in the state bucket.

## Terraform Coverage

The staging composition models these resource groups:

```text
Artifact Registry: asia-south1-docker.pkg.dev/goatos-stg/goatos
Cloud SQL:         goatos-stg-core-db / database goatos
Cloud SQL policy:  activation_policy = NEVER by default
Secret Manager:   containers only, no secret versions
IAM:              per-runtime service accounts and least-scope bindings
Pub/Sub:          outbox topic, analytics/domain subscriptions, DLQ
Cloud Tasks:      near-term kernel queue
Cloud Run:        goatos-api-stg and goatos-admin-web-stg services
Cloud Run Jobs:   migration, outbox DLQ, and scheduled kernel workers
Scheduler:        job invocations in Asia/Kolkata time
GCS:              goatos-stg-proof-media bucket and signer service account
Monitoring:       Cloud Run errors, outbox dead letters, DLQ backlog, Cloud SQL CPU
```

The API service deliberately has no `allUsers` invoker IAM binding. The
admin-web service has public invoker because the login page is public and
application access remains gated by Firebase/JWKS plus backend RBAC. Do not add
public invoker to `goatos-api-stg` unless the staging service-to-service/auth
decision is explicitly reopened.

## Existing Manual Resources

Some staging resources already exist from manual bring-up, including:

```text
goatos-admin-web-stg Cloud Run service
stg.dashboard.mesha.sg load balancer / serverless NEG / certificate / DNS
Firebase/Auth Platform and Google OAuth setup
staging Secret Manager secrets or secret values created during auth work
```

Before any future `terraform apply`, either import those resources into this
state or remove their Terraform resources from the apply scope. Applying without
that decision can fail on duplicate names or overwrite manual configuration.

## Secret Values

Terraform creates secret containers only. Populate secret versions out of band
after verification of the Mesha/VGoats account/project context. Required
staging secret values include:

```text
goatos-stg-database-url
goatos-stg-auth-issuer
goatos-stg-auth-audience
goatos-stg-auth-jwks-url
goatos-stg-auth-allowed-emails
goatos-stg-auth-session-allowed-tenant-ids
goatos-stg-bulk-import-preview-signing-key
goatos-stg-appcheck-enforce
goatos-stg-appcheck-issuer
goatos-stg-appcheck-audience
goatos-stg-appcheck-jwks-url
goatos-stg-proof-gcs-service-account-json
goatos-stg-firebase-web-config
goatos-stg-admin-web-api-base-url
goatos-stg-admin-web-tenant-id
goatos-stg-google-oauth-web-credential
goatos-stg-notification-slack-webhook-url
goatos-stg-notification-incident-webhook-url
```

The proof-media signer service account is modeled, but its key must not be
committed. If the app keeps using V4 signed URLs from a private key, create the
key only in the verified `goatos-stg` context and load the JSON into
`goatos-stg-proof-gcs-service-account-json`. A future safer improvement is to
replace private-key signing with IAMCredentials `signBlob`.

## Safe Local Checks

Read-only/source-only checks:

```bash
terraform -chdir=infra/envs/stg fmt -check
terraform -chdir=infra/envs/stg init -backend=false
terraform -chdir=infra/envs/stg validate
```

Do not run a real backend init, plan, import, apply, destroy, image push, Cloud
Run deploy, migration job, seed command, or DNS/LB command until the operator
explicitly asks for that cloud mutation and the org/project/repo guardrail has
been re-verified.
