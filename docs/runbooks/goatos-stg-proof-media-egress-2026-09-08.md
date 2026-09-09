# GoatOS STG Proof Media Egress Spike - 2026-09-08

## What Happened

On 2026-09-08, `gs://goatos-stg-media` served about 396 GiB of successful
`ReadObject` bytes and about 4 GiB of cancelled `ReadObject` bytes. Uploads were
only about 7.7 GiB. This was a download/streaming problem, not APK downloads,
Cloud Build tarballs, or users uploading 396 GiB.

The backend request pattern immediately before the spike was:

- `GET /app/pc-care/worklist?category=hoof_trimming&date=2026-09-08&limit=20`
- `GET /app/pc-care/tasks/<task_id>`
- `GET /app/pc-care/tasks/<task_id>/roster?limit=20`
- thousands of `GET /app/proofs/<proof_id>/download` redirects

The repeated proof rows mapped to PC Care hoof-trimming videos such as "Before
trimming", "While trimming", and "After trimming". The largest observed client
patterns were two mobile app installs repeatedly asking for the same proof IDs.

Observed hot clients:

- `157.51.58.239`, `okhttp/5.1.0`, about 10,900 backend proof download
  redirects.
- `106.195.35.121`, `okhttp/5.1.0`, about 8,800 backend proof download
  redirects.
- Device evidence included a Samsung `SM-A176B` and a realme `RMX3940`.

The top repeated proof IDs were PC Care hoof-trimming proof videos assigned to
Dheeraj Singh's completed work. Some individual proof IDs were requested roughly
150-296 times.

Firebase/GA evidence after re-authentication also supported the same path on
2026-09-08 for Android app `sg.mesha.goatos`:

- about 112,500 filtered `pc_care` events;
- about 84,000 `pc_care_slot_proof_preview` events;
- 4 users triggered that preview event;
- roughly 21,000 preview events per active user on average.

This lines up with the backend and GCS evidence: the app was repeatedly entering
proof preview/playback paths and causing repeated `/app/proofs/<proof_id>/download`
redirects.

## Layman Explanation

This was not one phone storing a 396 GiB file. It was many normal-sized proof
videos being streamed again and again. A signed GCS URL is like a temporary
private download link. If the app keeps asking for that link and keeps preparing
video/photo previews, GCS charges every repeated byte it sends, even if the phone
only buffers and throws the bytes away.

Same APK does not mean every operator triggers the same cost. The bad code was
available to everyone, but the expensive path needed the matching workload,
proof density, and screen:

- Dheeraj had PC hoof-trimming tasks with many video proof slots.
- That screen can show many before/during/after proof previews.
- The app was treating refreshed signed URLs like new media and doing hidden
  preview/probe/player work.
- API clients resolving `GET /app/proofs/<proof_id>/download` must receive the
  JSON URL envelope. If that call redirects to GCS, OkHttp/fetch-style clients
  can follow the redirect and accidentally read the media bytes before the UI
  has made any explicit play/open decision.
- Eeswar's deworming path did not show the same evidence pattern. Deworming and
  hoof trimming do not necessarily have the same proof density or preview loop.
- Dinakar showed a separate stuck-loader style symptom, but staging logs did not
  show the same repeated backend download storm for Dinakar's proof IDs.

## Code Guardrails

Proof preview code must never:

- key Compose/player state by a signed URL;
- derive identity by stripping query strings from signed URLs;
- call `URL(...).openStream()` for remote proof preview photos;
- call `MediaMetadataRetriever.setDataSource(url, emptyMap())` on remote proof
  URLs;
- make manual `Range: bytes=...` readability probes;
- prepare or autoplay remote proof video from list/card composition;
- leave buffering players alive when the app backgrounds.

The static guard is `tools/agent-hooks/check-android-proof-media-egress.mjs` and
is wired into `make mobile-guard` and `make mobile-guard-audit`. For whole-tree
reviews, run `node tools/agent-hooks/check-android-proof-media-egress.mjs --all`
and manually inspect adjacent proof/media consumers that share capture or preview
state with the changed feature.

The follow-up adversarial review also closed these adjacent traps:

- `ProofMediaPreview` no longer has a `mediaIdentity = path` default. Every
  caller must pass a stable identity, so future proof cards fail compilation if
  they rely on a temporary signed URL/path identity.
- The egress guard taints simple aliases such as `val proofUri =
  media.signedUrl` and checks those aliases when they reach Coil, Media3,
  `MediaPlayer`, OkHttp, raw URL streams, or metadata probes.
- Direct vaccination leadership video playback now stops when the app receives
  `ON_STOP` or when its card leaves the visible window; a tapped proof video
  must not keep streaming after background, lock screen, task switch, or scroll.
- Vendor voice-note playback is treated as proof media too. The screen now
  sends an app-stop event so the ViewModel releases raw `MediaPlayer` playback
  instead of leaving a signed proof audio URL alive in the background. The
  ViewModel also cancels stale play requests so a URL resolved after background
  cannot start playback.
- Shared `ProofMediaPreview` stops and disarms video playback when the proof
  card leaves the visible app window. PC Care, verify, scan/vaccination, feed,
  weighing, counts, pen visits, and leadership attachments all use that shared
  behavior.
- The egress guard now scans all Android runtime source sets (`main`, `stg`,
  `prod`, `release`, `dev`, `debug`) and fails if the diff base is invalid
  instead of silently green-lighting an unscanned diff.
- The guard's self-test covers common bypasses: `setMediaItems`, `addMediaItem`,
  `MediaMetadataRetriever.setDataSource(context, Uri.parse(...))`,
  `rememberAsyncImagePainter`, media-object aliases, request-variable OkHttp
  calls, `MediaPlayer` variable calls, and invalid `stable-local` suppression on
  remote signed URLs.
- Later guard hardening also covers typed/chained signed-URL aliases,
  `rememberSaveable(...)` URL keys, common OkHttp receiver names, and fullscreen
  resume auto-arming. Closing fullscreen may preserve position, but it must not
  silently arm/prepare the inline proof player.

Second adversarial sweep additions:

- `ProofMediaPreview` now rejects blank `mediaIdentity` at runtime instead of
  falling back to `path`. A caller can no longer accidentally make a temporary
  signed URL the stable key.
- Scan/vaccination preview fallbacks now use proof/outbox/server ids first and
  deterministic row identity only as the last resort; they do not fall back to
  the signed preview path.
- Feed distribution, feed packing, feed transport, and feed wastage pass explicit
  feature-slot media identities instead of display copy such as the card title.
- Verify photo preview is still tap-to-load, but a refreshed signed URL resets
  the tap-armed state. The app must not keep a remote photo armed across URL
  rotation and silently reload it.
- Vaccination leadership's legacy `videoUrls` fallback no longer keys player
  state by list index alone; it uses the backend item id plus slot index instead
  of deriving identity from any signed URL.
- `GET /app/proofs/uploads` is metadata-only by default. It no longer signs every
  uploaded proof on ordinary list refresh. Callers must explicitly pass
  `include_download_urls=true` when they are intentionally opening media.
- PC Care, feed teammate proofs, and weighing fasting read-only cards now carry
  authenticated backend proof download endpoints in preview state instead of
  pre-minting signed GCS URLs from their ViewModels on screen open, Room emit,
  poll, or playback-failure recovery.
- Backend hot reads for verification queues, process integrity action/adherence
  rows, vaccination scan roster, and weighing leadership gallery now return
  `/app/proofs/<proof_id>/download` paths instead of signed GCS URLs.
- `GET /app/proofs/uploads?include_download_urls=true` and weighing campaign
  CSV export also return backend proof download paths instead of bulk-signing
  GCS URLs during list/export generation.
- The backend guard is `tools/agent-hooks/check-backend-proof-media-egress.mjs`;
  for whole-tree reviews run `node tools/agent-hooks/check-backend-proof-media-egress.mjs`
  after its self-test, and inspect adjacent list/get/summary/dashboard/export
  paths before allowing new signing helpers.
- The admin-web guard is
  `tools/agent-hooks/check-admin-web-proof-media-egress.mjs`; it blocks server
  actions/detail reads from resolving proof refs into signed URLs and blocks
  client drawers from rendering proof routes in `<img>`, `<video>`, `<audio>`,
  or `<source>` tags. Review/admin UI may show proof metadata and an explicit
  "open media" link, but it must not fetch bytes because a drawer became
  visible.
- The Android guard now scans every runtime source set under `src/<variant>/`,
  requires stronger suppression comments, catches blank/title/path identities,
  catches ViewModel pre-hydration of proof download URLs, catches more
  `URL.openConnection().inputStream` and `setDataSource(...)` variants, and
  keeps a self-test for these bypasses.

Known architecture follow-up: purpose-built review/gallery endpoints now return
authenticated backend proof download paths instead of eager signed GCS URLs in
the checked hot reads. A separate API/CDN design can move the explicit open path
to signed CDN URLs without breaking playback.

## Additional Review-Round Fixes

The adversarial review loop found more places where the first pass was not
strict enough:

- Teammate feed-weight photos recorded on another phone are still inspectable:
  the card no longer downloads the remote bitmap passively, but it offers an
  explicit open action and only fetches the image inside fullscreen.
- Feed proof video playback is suspended whenever feed photo/video capture is
  opening, so an old inline player cannot continue underneath the CameraX
  recorder dialog.
- Vendor voice-note playback now emits bounded analytics for play, stop,
  completion, and failure with vendor/proof attribution.
- Verification approve-time evidence validation no longer calls the resolver
  that mints signed download URLs; it only uses the availability checker that
  confirms stored objects still exist.
- Leadership attachment downloads remain an explicit selected-attachment path,
  but now log actor/task/proof/trace attribution when issuing the URL.
- Android guard self-tests now include helper-parameter taint, typed/chained
  aliases, `rememberSaveable` URL keys, OkHttp direct downloads, fullscreen
  resume auto-arm, and fake `stable-local` laundering of a remote URL.
- Backend guard self-tests now include helper indirection where a read-like
  API calls another function that signs proof media, including helpers split
  across sibling files in the same package.
- The guardrail-registration guard now rejects fake Makefile/CI wiring through
  comments or `echo`; a required guard must be wired to a real standard CI
  step.
- The Android egress guard now blocks `SubcomposeAsyncImage` and
  `URI.create(...).toURL()` download variants, not only direct `AsyncImage` and
  `URL(...)` calls.
- Leadership-task attachment downloads emit the same `proof_download_url_issued`
  event name that the Slack/GCP responder query watches, with leadership
  surface/task/proof/client fields.
- Vendor voice-note URL resolution cannot leave the card stuck in loading if
  the app backgrounds before the URL call returns.
- Admin toxin review no longer resolves signed proof URLs during drawer detail
  load. Done steps and strip-photo evidence carry authenticated backend proof
  routes, and the strip photo is an explicit "open media" link instead of an
  `<img src=...>` that fetches bytes just because the drawer is visible.
- Leadership-task attachment downloads now pass the selected proof artifact
  through the service boundary and log storage provider, object key, MIME, size,
  content hash, scope, subject, proof type, and uploaded-by metadata on the
  `proof_download_url_issued` event.
- The Android guard now catches import aliases for `MediaItem`, `Request`,
  `URL`, `URI`, `AsyncImage`, and `SubcomposeAsyncImage`, plus extension-helper
  wrappers such as `String.openProofBytes()` that hide raw remote proof IO.
- The backend guard now catches method-value signer laundering such as
  `sign := s.proofMedia.DownloadURL` in read-like functions or same-package
  helpers, not only direct `DownloadURL(...)` calls.

## Alerting

Create Slack channel:

```text
#goatos-stg-cost-alerts
channel_id: C0C1HLFEYAU
invite_link: https://join.slack.com/share/enQtMTIwNTE3MDEwMDY1NDQtYTMxZmVjMGUxMDcyNmI3NjU1M2VjOTVmNjZhN2FjN2RjNDNjMjYyYWMwMDc5ZjBmY2Q2MzUxOWQzMzZjMzRjYw
```

Recommended Cloud Monitoring alerts for `goatos-stg-media`:

- Warning: `ReadObject OK` egress > 5 GiB in 1 hour.
- Critical: `ReadObject OK` egress > 15 GiB in 1 hour.
- Critical: `ReadObject OK` egress > 40 GiB in 24 hours.
- Warning: `ReadObject CANCELLED` egress > 512 MiB in 1 hour.
- Critical: `ReadObject CANCELLED` egress > 2 GiB in 1 hour.
- Warning: `ReadObject` request count > 1,000 in 10 minutes.
- Critical: `ReadObject` request count > 3,000 in 10 minutes.

MQL for the one-hour warning:

```text
fetch gcs_bucket
| metric 'storage.googleapis.com/network/sent_bytes_count'
| filter resource.project_id == 'goatos-stg'
| filter resource.bucket_name == 'goatos-stg-media'
| filter metric.method == 'ReadObject'
| filter metric.response_code == 'OK'
| align delta(1h)
| every 1m
| group_by [], [egress_bytes: sum(value.sent_bytes_count)]
| condition egress_bytes > 5368709120
```

The backend now logs `proof_download_redirect` with proof, actor, device, app
version, scope, MIME, content hash, and uploaded-by metadata before redirecting
to the signed URL. Use this log event for the attribution layer:

```text
resource.type="cloud_run_revision"
AND resource.labels.project_id="goatos-stg"
AND jsonPayload.event=("proof_download_url_issued" OR "proof_download_redirect")
```

API-client URL resolution should use:

```text
Accept: application/json
GET /app/proofs/<proof_id>/download
```

The backend answers JSON by default and only redirects browser-style
`Accept: application/json` callers receive the JSON envelope. Media/browser
clients that do not ask for JSON are redirected on explicit open/play so
ExoPlayer and browser anchors can stream. JSON responses emit
`proof_download_url_issued`; redirect responses emit `proof_download_redirect`.
Treat any ViewModel/list/client path that calls this endpoint before explicit
open/play/share as a review blocker, because redirect following can become paid
GCS egress.

Slack alert text should include the bucket, read GiB, cancelled GiB, redirect
count, top users/devices/screens/proofs, and this runbook path.

Slack delivery status:

- The Slack channel exists: `#goatos-stg-cost-alerts`.
- Terraform now defines the Cloud Monitoring webhook channel, the Pub/Sub
  budget push path, and the GoatOS `cost-alert-bridge` Cloud Run service.
- This means Slack/Cloud Monitoring alerting is configured in code, not proven
  live in GCP yet.
- Those resources are not verified live until the channel-specific Slack
  incoming webhook or bot token is stored in Secret Manager and Terraform/deploy
  is applied. The Slack channel ID by itself is not a Cloud Monitoring
  destination.
