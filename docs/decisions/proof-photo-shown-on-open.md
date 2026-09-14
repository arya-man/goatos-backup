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

- **Visible photos render; hidden photos do not prefetch.** A photo card that is
  actually composed in the viewport may fetch and show the picture. The app must
  not fetch photos for rows outside the viewport or for a whole queue/gallery in
  advance.
- **One proof image, one device-local fetch.** Android caches shared
  `ProofMediaPreview` photos in memory and app-private disk by the stable proof
  `mediaIdentity`, not by the rotating signed URL. Recomposition, scroll
  away/back, or signed URL renewal must not repeatedly read the same GCS object
  on the same device. A second phone signed in with the same account still has to
  fetch its own device-local copy once.
- **Videos still wait for intent.** A video card can show the video affordance,
  but it must not auto-stream, auto-prepare a player, or probe a remote poster
  just because it is visible. Remote video bytes move only after play/open.
- **Proof-media guards stay active.** The proof-media egress guards
  (`check-android-proof-media-egress.mjs`, `check-admin-web-proof-media-egress.mjs`)
  still block raw remote reads, signed-URL state keys, and remote video poster
  probes. Inline photo loads carry a `proof-media-egress:ignore` marker naming
  the bound and the stable cache key.

## Code

- Web: `apps/admin-web/features/verification-review/verification-review-drawer.tsx`
  — `activeIsPhoto` resolves the media URL as soon as the drawer lands on a
  photo (landed as `90ff9c33e`).
- Android verifier: `VerifyProofPhoto` in
  `feature-verify/.../VerifyDetailScreen.kt` — no `loadPhoto` arm; tap opens
  fullscreen directly.
- Android shared preview: `ProofMediaPreview.inlineRemotePhoto` defaults to
  `false`; list/detail callers opt in only when the proof is the visible item
  being shown, so remote photos load through the proof media HTTP path and cache
  by stable `mediaIdentity`, while videos remain click-to-play.
