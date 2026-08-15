# Feed Proof Collaboration Model

**Status:** Canonical product contract. UI, backend, mobile, and every E2E test must
conform to this document. If code disagrees with this document, the code is wrong.

## What the product is

Goat OS runs daily farm operations for goat parks. Feeding is executed per
**shed-session** (a shed/partition × session number × date, e.g. "Castro 1 ·
Morning · 2026-08-15"). A feeding is only counted as done when it is **proven**:
three media proofs captured on the ground, then verified later by a verifier.

The three proof slots per shed-session:

1. **Feed weight photo** — weighed feed visible before distribution.
2. **Feed distribution video** — feed actually being given out.
3. **Water distribution video** — water being given out.

All proofs are compressed and overlay-burned (timestamp, operator name, task
context, location) on-device before upload. A raw, overlay-free file reaching the
server is always a defect, never an acceptable fallback outcome.

## The collaboration contract

- **Operators are peers.** The people feeding a shed are all `operator` role.
  There is no hierarchy among them and no peer review. Review happens only later,
  by the verifier role, in the verification queue.
- **A shed-session is ONE shared workspace.** Every operator assigned to that
  park sees the same session state under their own login. It is not "my session"
  or "his account" — it is the shed's session.
- **Free slot assignment.** Work split is decided by the people on the ground,
  not the app. One operator may fill all three slots; three operators may take
  one each; any 2/1 split is equally valid. The app must never force, assume, or
  restrict who captures which proof.
- **Attribution is per-proof.** Each captured proof records who captured it
  ("Captured by Amit Kumar · 2:46 pm"). Attribution answers "who did this task",
  it does not gate who may do the next one.
- **Visibility is immediate and mutual.** When one phone uploads a proof, the
  other phones working the same session show it (thumbnail + attribution) on
  entering the screen, on refresh, and via periodic sync — without app restart.
- **Submit is session-level.** When all three slots are filled — by any mix of
  contributors — any operator on the session may submit. Submit locks the
  session (`pending_verification`) on ALL phones: read-only, no re-capture, no
  camera reachable. Duplicate submits are idempotent.
- **Offline-first.** A capture is durable on-device (Room) before any network
  call. Back/re-enter, process death, and connectivity loss must never lose a
  captured proof.

## What this means for testing

Testing this feature means testing the contract, not one scripted path:

- Multiple real devices, each logged in as a DIFFERENT operator, working the
  SAME shed-session concurrently.
- Vary the split across sheds: 3-way split (one slot each), single operator
  fills all three, 2/1 split. All must work identically.
- Assert cross-phone reflection after EVERY upload, in both directions — every
  phone is an uploader AND a viewer.
- Assert submit lock propagates to all phones.
- Server truth over screen truth: completion row with all three proof refs,
  artifacts compressed + overlay-burned (`upload_original=false`), verified by
  pulling the stored file.

## What this feature is NOT

- Not a review flow between operators.
- Not a fixed role-to-slot mapping (never "the photo person" / "the water person").
- Not per-operator sessions that merge later — there is exactly one session row
  per shed-session, filled collaboratively.
