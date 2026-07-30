# Verifier app + video-verification flow (from the wiki handbooks)

Status: implemented contract for the **Verifier** role's app + flow. Sourced from
the Mesha wiki handbooks (PHC-Director + Health-Director + Slack modules). Companion
to [`verification-module-design.md`](./verification-module-design.md) (the generic
backend) and [`org-role-model.md`](./org-role-model.md) (the truth table). Names
omitted per repo policy.

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

## Verifier APP scope (mobile, verifier-only workspace)

Maintainer decision 2026-07-30, superseding both the synthetic standalone
**Verification** drawer module and the Android-only Vaccination/Weighing rewrite:
the backend composes exactly five verifier drawer modules — **Vaccination,
Weighing, Counts, Feed, and Health**. Each module opens the same reusable media
queue and renders backend-defined page tabs across the top:

| drawer module | backend-defined page tabs |
|---|---|
| Vaccination | Vaccination |
| Weighing | Weighing |
| Counts | Birth, Death, Shifting, Milk Prep, Milk Feeding |
| Feed | Feed Distribution, Feed Packing, Feed Transport |
| Health | Adults, Kids |

A verifier still sees **only** video verification — never the corresponding
operator/leadership pages:

- **A date-scoped media queue**, separated by the selected page's disjoint
  backend category. After a first-level page such as Birth, Death, or Shifting
  is selected, the backend supplies a second tab row: **Due / Approved /
  Rejected**. These are disjoint verification-item buckets (`pending`,
  `approved`, `rejected`); Android never recomputes or combines them. Death reads category `death_evidence`; Birth reads
  category `birth_evidence`. Each mother or child enters Birth verification as
  soon as every task in that subject's workflow is complete with video. One item
  at `workflow_id` grain carries only that mother or child's ordered proof bundle;
  siblings never block or share the verdict.
  Other active producers cover vaccination, shifting, milk preparation/feeding,
  feed distribution/packing/transport evidence. Weighing and Health keep their
  declared pages even when no producer has enqueued work yet; an empty page does
  not fabricate evidence.
- Per item: **play the video(s)**. Every task proof renders its backend-authored
  workflow task title immediately above the matching video. Question/value tasks
  also render the recorded operator answer; action-only tasks omit the answer.
  Android never derives either value from category or list position. Context includes shed/park/operator/
  timestamp from the capture metadata → **Approve** or **Reject + mandatory reason**.
- **No capture, no ops, no roster, no config** — the verifier-only workspace
  only. Tabs render backend-owned category queues; an empty tab does not fabricate
  verification work.
- Bounded/paginated queue (~20), media via streamed signed URLs (scale rules apply).
- The calendar selects an Asia/Kolkata `business_date` and may move backward to
  historical dates. Approved and rejected rows retain their resolved proof
  media, open in the same detail screen, and are view-only because their verdict
  is already terminal. The bell is backed by an indexed backend `EXISTS` query:
  it indicates whether the selected page/location has any pending item captured
  before today's business-day start and toggles that bounded missed-only queue.

## Backend (generic — see verification-module-design.md)
- Producers (vaccination, diagnosis, death, feed, …) emit `verification_item`
  (module-agnostic) with `{vertical, module, category, media[], status, verdict}`;
  resolved task media includes `media[].label` from canonical task/action truth.
- Verifier action: `POST /verification/items/{id}/verdict {approved|rejected, reason}`
  gated by the new **`verification.review`** permission (the Verifier role). Reject
  requires a reason.
- After every goat verdict in a drive is approved, the authority
  (Park Head/Director/CEO/CxO) atomically closes the submission through
  `POST /verification/submissions/{submission_id}/close`; the verdict + reason feed
  the daily "SOP Video Double Verification" metrics (violations flagged, penalties).
- Plug-and-play: a vertical/module registers its category plus backend navigation
  metadata (`navigation_module`, page key/label/order) in the verification type
  registry → its videos appear under the correct drawer module and top tab.

Operational read contract: row grain is one `verification_item`; page and status
buckets are disjoint; `filter_options.has_missed` is a whole-filter boolean, not
derived from the current page. Lists remain keyset-paginated by
`(captured_at,item_id)`. Date predicates use bare `captured_at` with inclusive
UTC start/exclusive UTC end bounds derived from the India business date, matching
`verification_items_queue_idx`. This is a read-only extension of the existing
verification state/evidence contract, so no domain event or leadership-assistant
write/read mapping changes.

## Roles (truth table alignment)
- **Capture** = ground operator only (mobile capture app). **Verify** = Verifier
  (`verification.review`, this app). **Act** = scoped Park Head/Director or
  CEO/CxO (`verification.act`, leadership app).
  Separation of duty — nobody captures and verifies the same work.
