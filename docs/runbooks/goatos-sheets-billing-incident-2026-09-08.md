# goatos-sheets Billing Incident - 2026-09-08

## Summary

On 2026-09-08, Google Cloud Billing projected September spend for billing
account `01FEDE-96BCB3-76D992` at `INR 45,714.03` pre-tax. August charge-period
usage was `INR 35,882.98` pre-tax, so the projection looked like a sudden
month-on-month jump.

The new spike was not caused by GoatOS user volume or millions of records. The
largest new driver was `goatos-sheets/dashboard` in `us-central1`, which had
been manually changed on 2026-09-03 to keep two 2-vCPU Cloud Run instances warm
with CPU always allocated.

## Account And Scope Verified

Use only the `ravi@mesha.sg` Google identity for this account.

```bash
gcloud auth list --filter=status:ACTIVE --format='value(account)'
gcloud config get-value project
gcloud billing accounts describe 01FEDE-96BCB3-76D992 --format=json
```

Observed during the incident:

```text
account: ravi@mesha.sg
active project: goatos-stg
billing account: 01FEDE-96BCB3-76D992
currency: INR
```

Chrome Billing Console also showed `Account: Ravi Teja (ravi@mesha.sg)`.

## Billing Numbers

August 2026 charge-period usage:

```text
Total:                          INR 35,882.98
goatos-stg / GoatOS:            INR 31,642.69
goatos-sheets:                  INR  3,108.75
goatos-dev:                     INR  1,111.90
Charges not specific to project INR     19.64
```

September 1-7 usage from the Billing report:

```text
Total:               INR 11,419.78
goatos-stg / GoatOS: INR  8,569.43
goatos-sheets:       INR  2,436.58
goatos-dev:          INR    252.59
```

September 1-7 by service:

```text
Cloud Run:      about INR 7.3k-7.4k
Cloud SQL:      about INR 1.8k
Cloud Storage:  about INR 932
Networking:     about INR 400
BigQuery:       about INR 130
Gemini API:     about INR 120
```

The September projection was real in the Console, but it was a forecast based
on the first week of usage and recent trends. It was not proof of a matching
amount already paid.

## Root Cause

The new cost driver was:

```text
Project: goatos-sheets
Service: dashboard
Region: us-central1
Runtime: Firebase App Hosting managed Cloud Run
URL: dashboard--goatos-sheets.us-central1.hosted.app
```

Bad state found on 2026-09-08:

```text
revision: dashboard-hotfix-021259
minScale: 2
maxScale: 100
cpu: 2
memory: 1Gi
cpu-throttling: false
```

Meaning:

```text
2 warm Cloud Run instances x 2 vCPU = 4 vCPU always allocated
plus 2Gi memory always allocated
```

For low request volume, this converts the dashboard from scale-to-zero/request
cost into idle instance-based spend. Around the incident window the dashboard
had only about 2,099 requests across 2026-09-01 through 2026-09-08, so the cost
was not traffic-driven.

## Audit Timeline

Cloud Audit Logs showed manual `gcloud` mutations from `ravi@mesha.sg`, with a
Mac Google Cloud SDK user agent and `from-script/False`.

```text
2026-09-03 20:38 UTC
  command family: gcloud run services update
  created revision: dashboard-00156-k27
  shape: min=1 max=80 cpu=1 memory=512Mi cpu-throttling=true

2026-09-03 20:41 UTC
  command family: gcloud run services update
  created revision: dashboard-00157-6v4
  shape: min=2 max=100 cpu=2 memory=1Gi cpu-throttling=false

2026-09-03 20:43 UTC
  command family: gcloud run services update
  created revision: dashboard-hotfix-021259
  shape: min=2 max=100 cpu=2 memory=1Gi cpu-throttling=false

2026-09-03 20:43 UTC
  command family: gcloud run services update-traffic
  traffic moved to dashboard-hotfix-021259
```

This was not Firebase or Cloud Run autoscaling itself in response to user
traffic. It was a service configuration change.

## Relationship To August Billing Recovery

`docs/runbooks/stg-cloud-run-billing-recovery.md` is scoped to:

```text
Project: goatos-stg
Region: asia-south1
Services: goatos-api-stg, goatos-admin-web-stg
```

That runbook documents an August recovery pattern for platform `429 Rate
exceeded` on the staging API/admin path after billing suspension. It does not
authorize applying `minScale=2`, `maxScale=100`, `2 CPU`, and
`cpu-throttling=false` to `goatos-sheets/dashboard` in `us-central1`.

Keep these separate:

```text
goatos-stg       = GoatOS staging backend/admin baseline spend.
goatos-sheets    = old sheets dashboard/Firebase App Hosting project.
mesha.sg/app.apk = storage-hosted APK redirect, not the dashboard Cloud Run service.
```

## APK Download Is A Separate Cost

`https://mesha.sg/app.apk` redirects to:

```text
gs://goatos-stg-public-downloads/operator/latest/app.apk
```

Observed on 2026-09-08:

```text
Content-Length: 87,158,533 bytes / 83.12 MiB
Content-Disposition: attachment; filename="Mesha-1.0.11.apk"
Cache-Control: no-cache, max-age=0
Created: 2026-09-07 21:46 UTC
```

Repeated downloads of this object can explain part of Cloud Storage/APAC
download spend. During 2026-09-01 through 2026-09-07 the Billing report showed
around `176 GiB` of APAC download, about `INR 877`. That is real but separate
from the `goatos-sheets/dashboard` Cloud Run CPU spike.

Do not route `/app.apk` through Cloud Run. It should remain a static APK object
or redirect, with its download behavior tracked separately from dashboard
runtime spend.

## Fix Applied On 2026-09-08

The immediate remediation changed only `goatos-sheets/dashboard`.

```bash
gcloud run services update dashboard \
  --project=goatos-sheets \
  --region=us-central1 \
  --min-instances=0 \
  --max-instances=20 \
  --cpu=1 \
  --memory=512Mi \
  --cpu-throttling \
  --quiet
```

Post-change verification:

```text
latestReadyRevisionName: dashboard-hotfix-021259
minScale: absent/0
maxScale: 20
cpu-throttling: true
cpu: 1
memory: 512Mi
service URL: https://dashboard-psnartmcoa-uc.a.run.app
```

`gcloud run services describe` omits the `minScale` annotation when the value is
zero. Treat absent `autoscaling.knative.dev/minScale` as `0`.

Terminal availability check after the change:

```text
GET https://dashboard--goatos-sheets.us-central1.hosted.app/
HTTP 307 to /login?next=%2F
```

That means the service was still responding after the scale/cost fix.

Expected savings from this one change:

```text
INR 15k-18k/month pre-tax
INR 17.7k-21.2k/month including 18% GST
```

The user-visible tradeoff is that the old `goatos-sheets` dashboard can cold
start after idle. GoatOS Android, `stg-api.dashboard.mesha.sg`,
`stg.dashboard.mesha.sg`, Cloud SQL, and `https://mesha.sg/app.apk` are not on
this Cloud Run service and should not be affected by this remediation.

## Guardrails

Before changing Cloud Run min instances, CPU, memory, CPU throttling, or max
instances, identify the exact project/service/region and write down the
expected cost impact.

Never copy the August `goatos-stg` billing-recovery min-instance settings into
`goatos-sheets/dashboard`. They are different systems.

For `goatos-sheets/dashboard`, the expected low-cost shape is:

```text
minScale: 0
maxScale: 20
cpu: 1
memory: 512Mi
cpu-throttling: true
```

If someone believes the dashboard needs warm capacity, first prove the specific
failure mode with request logs, revision conditions, and a business owner sign
off. Prefer `minScale=1` with CPU throttling over `minScale=2`,
`cpu=2`, and CPU always allocated.

Enable Cloud Billing export for this billing account. On 2026-09-08, Billing
Export was disabled for FOCUS, Standard, Detailed, Pricing, and CUD exports,
which prevented SQL-level per-SKU/per-resource reconciliation.
