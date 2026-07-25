# STG GCS / Android Video Proof Upload Wiring

STG deploy does not count as Android-video-proof-ready unless this checklist is
verified. A green backend/admin deploy does NOT prove Android proof/video upload
works. Report GCS as **PASS**, **FAIL**, **PARTIAL**, or **NOT WIRED**.

Companion docs:
[`stg-deploy.md`](./stg-deploy.md) (deploy contract),
[`../mobile/proof-capture-sync-and-e2e.md`](../mobile/proof-capture-sync-and-e2e.md)
(on-device capture/Room/outbox contract),
[`google-cloud-environments.md`](./google-cloud-environments.md) (org/project boundary).

## Org / Project Boundary (verify FIRST)

- account: `ravi@mesha.sg`
- org: `vgoats.com`
- project / environment: `goatos-stg` (STG)

The proof bucket must live in `goatos-stg`. **Forbidden** targets for STG proof
storage: any `goatos-prod`, `goatos-dev`, `goatos-sheets`, Heva, or Slice
project/bucket. A bucket in the wrong project is an automatic **FAIL**, even if
uploads succeed.

## Runtime Shape

Android must never write to GCS with its own credentials, and admin-web must
never proxy video bytes.

Required path:

```
Android in-app camera / gallery clip
-> Room outbox (PENDING -> IN_FLIGHT)
-> STG backend proof register + signed-URL issue (/app/... proof endpoints)
-> Android uploads bytes directly to the GCS signed URL
-> backend links the object to the proof/subject row in Postgres
-> Room row marked SYNCED
```

The bytes go Android -> GCS directly via a backend-issued signed URL. The API
issues and links; it never streams the video through itself.

## Required STG Backend Config

Backend service `goatos-api-stg` must have:

- `GOATOS_MEDIA_STORAGE=gcs` (NOT `local` — `local` is rejected outside
  local/test/development)
- `GOATOS_GCS_BUCKET` = the `goatos-stg` proof bucket name
- GCS signing identity, supplied as EITHER:
  - `GOATOS_GCS_CLIENT_EMAIL` + `GOATOS_GCS_PRIVATE_KEY`, OR
  - `GOATOS_GCS_SERVICE_ACCOUNT_JSON` (email + private key parsed from the JSON)
- the signing service account must be able to write objects and mint V4 signed
  URLs for `GOATOS_GCS_BUCKET`

Env sources: `backend/internal/bootstrap/api.go` (storage selector),
`backend/internal/proof/adapters/storage/gcs/storage.go` (GCS adapter, requires
non-empty bucket + client email or it errors at startup).

## Required Google Cloud State

In project `goatos-stg`:

- the proof bucket exists in `goatos-stg`
- the backend runtime service account can `storage.objects.create` and sign V4
  URLs (`iam.serviceAccounts.signBlob` if signing via IAM, or a valid private
  key)
- bucket CORS allows the Android upload origin/method (PUT to the signed URL)
- object lifecycle / retention is intentional (proof videos are evidence — do
  not silently auto-delete)
- bucket is not public; access is only via short-lived signed URLs

## Smoke Tests (after STG deploy)

1. Backend storage mode:
   - startup logs / config show `GOATOS_MEDIA_STORAGE=gcs` and a non-empty
     `GOATOS_GCS_BUCKET` in `goatos-stg`
   - backend did NOT fall back to local storage (a `local`-mode STG backend is
     **NOT WIRED**)

2. Signed-URL issue:
   - an authenticated operator proof-register call returns a signed upload URL
     whose host/bucket is the `goatos-stg` proof bucket

3. Real upload round-trip (device or emulator):
   - capture a clip, let the Room outbox drain, confirm the object appears in the
     `goatos-stg` bucket
   - confirm the proof/subject row in Postgres links to that object key
   - confirm the Room row reaches `SYNCED`

4. DB linkage:
   - the stored object key matches the proof row (per-goat `subject_type=goat` +
     goat UUID, or shed-level `subject_type=shed`) so the video is retrievable
     from the operator/verifier surface

## Verdict Rules

- **PASS** — mode `gcs`, bucket in `goatos-stg`, signed URL issued, real clip
  uploaded, object linked in Postgres, Room row `SYNCED`.
- **PARTIAL** — signed URL issues but the end-to-end clip upload or DB linkage
  was not verified on STG; state exactly which step is unproven.
- **FAIL** — bucket in the wrong project/org, backend fell back to `local`,
  signing identity/permission missing, upload rejected, or object not linked.
- **NOT WIRED** — `GOATOS_MEDIA_STORAGE` is not `gcs`, `GOATOS_GCS_BUCKET` is
  empty, or no signing identity is configured.

## Not Ready Conditions

GCS video proof is NOT ready if any of these are true:

- `GOATOS_MEDIA_STORAGE` is `local` or unset on STG
- `GOATOS_GCS_BUCKET` empty, or points at prod/dev/sheets/Heva/Slice
- no signing identity (`GOATOS_GCS_CLIENT_EMAIL`+`GOATOS_GCS_PRIVATE_KEY` or
  `GOATOS_GCS_SERVICE_ACCOUNT_JSON`)
- service account cannot write objects or mint signed URLs
- signed URL issues but a real clip upload / DB linkage was never observed on STG
- admin-web or Android attempts direct GCS credential access instead of the
  backend signed-URL path
