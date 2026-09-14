# A proof PHOTO is on screen the moment its card opens — never behind a tap

Maintainer decision 2026-09-14. Applies to BOTH surfaces: the admin-web
verification drawer (`/verify`) and the Android app (verifier detail, and every
record screen that renders `ProofMediaPreview` with a photo).

## The rule

When a person opens an item whose proof is a photo (the feed weight reading,
the water photo, an animal purchase capture), the picture itself is rendered on
first paint. There is no blank tile, no "Open photo" / "Tap to open photo"
control, and no click that the picture waits for. A tap on the picture ENLARGES
it; it never REVEALS it.

Videos are unchanged: a video still waits for play (a click on the web, the play
control on the phone) unless the queue thumbnail carried the play intent.

## What was retired, and why it existed

Both surfaces used to render a photo proof as a **tap-armed** tile: a signed
proof URL is a paid object read, so the photo was fetched only after an explicit
click. That guardrail was written for LISTS — a queue scrolling past dozens of
proofs must not fetch dozens of images nobody opened — and it was applied to the
one-item DETAIL as well.

On the detail it is wrong. The verifier had opened exactly one item and was
looking at a blank card with an eye-off icon, on the phone and in the web
drawer alike. A blank where the evidence should be reads as a missing or broken
proof, and the extra click was pure friction on the one screen whose whole job
is to show that evidence. The maintainer saw it on both surfaces and retired it
on both the same day.

## The boundary that stays

The paid-read concern is real and is kept where it applies:

- **Lists never auto-fetch.** A scrolling queue or gallery passes
  `inlineRemotePhoto = false` (Android) or resolves nothing until a row is opened
  (web). The proof-media egress guards
  (`check-android-proof-media-egress.mjs`, `check-admin-web-proof-media-egress.mjs`)
  still enforce that; the detail-screen loads carry a `proof-media-egress:ignore`
  marker naming the bound — one photo, for the one item the person opened.
- **One read, not two.** Android keys the Coil memory/disk cache by the stable
  proof id, so enlarging the same photo does not fetch it again.

## Code

- Web: `apps/admin-web/features/verification-review/verification-review-drawer.tsx`
  — `activeIsPhoto` resolves the media URL as soon as the drawer lands on a
  photo (landed as `90ff9c33e`).
- Android verifier: `VerifyProofPhoto` in
  `feature-verify/.../VerifyDetailScreen.kt` — no `loadPhoto` arm; tap opens
  fullscreen directly.
- Android shared preview: `ProofMediaPreview.inlineRemotePhoto` defaults to
  `true`; the "Tap to open photo" placeholder is reachable only when a caller
  opts out for a list.
