# STG Cost Controls

## 2026-09-01 Admin Web Cap

Decision: cap `goatos-admin-web-stg` at `min=1 max=2`.

Evidence checked on 2026-09-01 against Cloud Monitoring for 2026-08-25 through
2026-09-01: admin-web's observed maximum instance count was 2. The previous
deploy restore path used `max=4`, and Terraform declared `max_instance_count=5`,
so future deploys or applies could raise the cap again.

Applied live:

```bash
gcloud run services update goatos-admin-web-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --max-instances=2 \
  --max=2
```

Repo contract:

- `infra/envs/stg/cloud_run_services.tf` keeps admin-web at `min=1 max=2`.
- `tools/deploy/stg-clouddeploy-task.sh` restores admin-web at `min=1 max=2`.
- Slack deploys use `cloudbuild.stg.yaml`, which runs the same deploy scripts from
  the pushed `main` commit. After this is merged to `main`, Slack deploy restores
  admin-web at `max=2` too.

Do not change kernel-worker, API, Cloud SQL, Grafana, or herd-signals bridge as
part of this admin-web cap. Those need separate approval.

## Deploy Spin-Up / Shutdown

Do not solve downtime by bringing the API down before deployment. Spinning the
API up and down around STG deploy is not a zero-downtime strategy; it is planned
downtime for both web and Android because both clients depend on the public API.

For no web/Android downtime, the deploy path must keep the currently serving
API/admin revisions public while preparing the new revisions, run only
backward-compatible migrations, then move traffic after readiness passes.

That is the normal industry pattern: expand first, run old and new code together,
then contract later. Do not remove the current STG quiesce step until the release's
database migrations pass the zero-downtime audit.

Use this before attempting a no-downtime STG deploy:

```bash
make stg-zero-downtime-migration-audit
```

The audit checks touched Postgres migrations for operations that can break old
running revisions, including `DROP TABLE`, `DROP COLUMN`, `DROP CONSTRAINT`,
renames, `DELETE FROM`, `TRUNCATE`, new strict constraints, and unique indexes.
If it fails, keep the downtime/quiesce deploy path for that release or split the
database change into two releases.
