# STG Cloud Run Billing Recovery

Use this when `https://stg.dashboard.mesha.sg` or
`https://stg-api.dashboard.mesha.sg` shows `Rate exceeded.` or Cloud Run request
logs say:

```text
The request was aborted because there was no available instance.
```

This can happen after a Google billing suspension is paid and billing is enabled
again. Do not assume the app code is broken until the Google Cloud project,
billing link, Cloud Run service state, and browser surface have all been checked.

## Scope

Project: `goatos-stg`

Region: `asia-south1`

Primary services:

```text
goatos-admin-web-stg
goatos-api-stg
```

Expected steady-state scaling for the API after the 2026-08-26 billing recovery:

```text
goatos-api-stg min-instances=2 max-instances=2
```

Keep runtime repair separate from a code deploy. If a code deploy is needed,
return to `docs/runbooks/stg-deploy.md` and use Cloud Deploy.

## 1. Verify Identity And Billing

```bash
gcloud auth list
gcloud config get-value account
gcloud config get-value project
gcloud billing projects describe goatos-stg \
  --format='yaml(projectId,billingAccountName,billingEnabled)'
```

Expected:

```text
account: ravi@mesha.sg
project: goatos-stg
billingEnabled: true
```

If `billingEnabled` is false, the problem is still billing/linkage. Fix billing
in Google Cloud Console before touching Cloud Run.

## 2. Prove It Is Platform 429

```bash
curl -sS -D - -o /tmp/goatos-stg-admin.txt https://stg.dashboard.mesha.sg/ \
  | sed -n '1,20p'
sed -n '1,40p' /tmp/goatos-stg-admin.txt

curl -sS -D - -o /tmp/goatos-stg-api.txt https://stg-api.dashboard.mesha.sg/healthz \
  | sed -n '1,20p'
sed -n '1,40p' /tmp/goatos-stg-api.txt
```

Platform failure shape:

```text
HTTP/2 429
server: Google Frontend

Rate exceeded.
```

That response is emitted before Goat OS app code handles the request.

## 3. Read Cloud Run Evidence

```bash
gcloud logging read \
  'resource.type="cloud_run_revision" AND resource.labels.service_name=("goatos-api-stg" OR "goatos-admin-web-stg") AND severity>=WARNING' \
  --project=goatos-stg \
  --freshness=2h \
  --limit=80 \
  --format='table(timestamp,resource.labels.service_name,severity,httpRequest.status,httpRequest.requestUrl,textPayload)'
```

The billing-recovery/capacity symptom is:

```text
status=429
The request was aborted because there was no available instance.
```

## 4. Check Scaling And Revision State

```bash
gcloud run services describe goatos-api-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --format=json \
  | jq -r '{name:.metadata.name,generation:.metadata.generation,observed:.status.observedGeneration,min:.spec.template.metadata.annotations["autoscaling.knative.dev/minScale"],max:.spec.template.metadata.annotations["autoscaling.knative.dev/maxScale"],latestReady:.status.latestReadyRevisionName,latestCreated:.status.latestCreatedRevisionName,conditions:.status.conditions}'

gcloud run services describe goatos-admin-web-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --format=json \
  | jq -r '{name:.metadata.name,generation:.metadata.generation,observed:.status.observedGeneration,min:.spec.template.metadata.annotations["autoscaling.knative.dev/minScale"],max:.spec.template.metadata.annotations["autoscaling.knative.dev/maxScale"],latestReady:.status.latestReadyRevisionName,latestCreated:.status.latestCreatedRevisionName,conditions:.status.conditions}'
```

If a fresh revision says `Provisioning revision instances to receive traffic` or
`WaitingForOperation`, Cloud Run is still recovering capacity. Wait for the
retry interval shown in the revision condition, then re-check.

## 5. Recovery Action

For the 2026-08-26 incident, the API baseline requested by the maintainer is
min/max 2:

```bash
gcloud run services update goatos-api-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --min-instances=2 \
  --max-instances=2
```

If admin-web is also returning platform 429s, keep two warm admin-web instances:

```bash
gcloud run services update goatos-admin-web-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --min-instances=2 \
  --max-instances=2
```

Do not call this fixed while either service has `Ready=Unknown`.

## 6. Required Final Verification

Terminal verification:

```bash
curl -sS -D - -o /tmp/goatos-stg-admin-final.txt https://stg.dashboard.mesha.sg/ \
  | sed -n '1,20p'
sed -n '1,40p' /tmp/goatos-stg-admin-final.txt

curl -sS -D - -o /tmp/goatos-stg-api-final.txt https://stg-api.dashboard.mesha.sg/healthz \
  | sed -n '1,20p'
sed -n '1,80p' /tmp/goatos-stg-api-final.txt
```

Browser verification is mandatory:

1. Open Chrome on `https://stg.dashboard.mesha.sg/`.
2. Hard reload.
3. Confirm the page is not `Rate exceeded.`, not a plain `503 Server Error`, and
   not the admin-web contract fallback.
4. If logged in, confirm the real Mesha Admin shell renders.

Record the exact UTC and IST time, final revision names, min/max values, and
browser result in the handoff.
