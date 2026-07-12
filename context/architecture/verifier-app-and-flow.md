# Verifier app + video-verification flow (from the wiki handbooks)

Status: reference/future scope for the **Verifier** role's app + flow. Sourced from
the Mesha wiki handbooks (PHC-Director + Health-Director + Slack modules). Companion
to [`verification-module-design.md`](./verification-module-design.md) (the generic
backend) and [`org-role-model.md`](./org-role-model.md) (the truth table). Not built
yet. Names omitted per repo policy.

## The verifier role (wiki-sourced)
The **Video Verification Team** is a standing, daily, independent second check on
every execution SOP across every vertical.
- **PC-Director handbook:** "all execution SOPs are **double-verified** by meeting
  the video verification team **every day**." Keep ≥2-week stock, etc.
- **Health-Director handbook §05 (Video Verif.):** "meet verifier every day ·
  violations → **call + written msg** · **Director penalised if at fault**."
- **PHC-Director EOD — "SOP Video Double Verification":** daily metrics the flow
  produces — `videos reviewed by verifier (count)` · `SOP violations flagged (count)`
  · `violations communicated on call? (Y/N)` · `written follow-up sent? (Y/N)` ·
  `penalties issued (count)`.
- **Slack modules:** every task uploads its form + media in Slack (e.g. "Upload
  Diagnosis Video", Deceased/Post-Mortem Video SOP, vaccination drive videos) — the
  verifier reviews across ALL categories, not vaccination-only.

## The flow
```
operator executes SOP → uploads proof video(s) (per category)
   → Verifier reviews the video (daily, independent)
        → APPROVE                      → SOP passes
        → REJECT + REASON (flag violation)
             → communicated on CALL + WRITTEN follow-up
             → Park Head / Director / CEO ACT → penalty / rework / re-assign
```
Key: the verifier only **approves or rejects with a reason**. Communication,
penalty, and action are the **authority's** job (Head/Director/CEO). "Double
verification" = the verifier is a second, independent pass on top of the operator's
own execution — the anti-fraud/quality gate.

## Verifier APP scope (mobile, standalone)
A verifier who opens the app sees **only** video verification — nothing else:
- **A queue of pending media to verify**, filtered by **category** (vaccine,
  feed-direction, diagnosis, death/post-mortem, breeding, … — the vertical/module
  the item came from). The verifier is assigned one or more categories.
- Per item: **play the video(s)** (+ its context: shed/park/operator/timestamp from
  the capture metadata) → **Approve** or **Reject + mandatory reason**.
- **No capture, no ops, no roster, no config** — the standalone Verifier section
  only. (Capture is the operator app; act/penalise is admin-web authority.)
- Bounded/paginated queue (~20), media via streamed signed URLs (scale rules apply).

## Backend (generic — see verification-module-design.md)
- Producers (vaccination, diagnosis, death, feed, …) emit `verification_item`
  (module-agnostic) with `{vertical, module, category, media[], status, verdict}`.
- Verifier action: `POST /verification/items/{id}/verdict {approved|rejected, reason}`
  gated by the new **`verification.review`** permission (the Verifier role). Reject
  requires a reason.
- After verdict, the authority (Head/Director/CEO) acts; the verdict + reason feed
  the daily "SOP Video Double Verification" metrics (violations flagged, penalties).
- Plug-and-play: a new vertical/module registers its category in the verification
  type registry → its videos appear in the verifier queue automatically.

## Roles (truth table alignment)
- **Capture** = operator/manager (mobile capture app). **Verify** = Verifier
  (`verification.review`, this app). **Act** = Head/Director/CEO (admin-web).
  Separation of duty — nobody captures and verifies the same work.
