# Preventive Care Vaccination Rule Clarity Log

This log records source/wiki/story conflicts that are being clarified before
implementation. Decided items must be reflected in PRD, TRD, rule matrix,
Config authoring handoff, seed data, and kernel behavior before the slice is
called complete.

## Decided

### 1. Mother-not-vaccinated / unknown-mother branch

Decision: GoatOS ignores this branch. Never ask about, model, seed, import,
expose, or schedule from mother-not-vaccinated / unknown-mother status. Mothers
are kept vaccinated operationally, and every kid uses the approved standard
schedule in [vaccination-rules.md](./vaccination-rules.md).

Why: This was confirmed as the final business rule on 2026-07-03. The private
source/wiki may still contain the early branch, but it is non-executable for
GoatOS.

### 2. Mixed-species drive grouping

Decision: drive planning optimizes for maximum safe doctor coverage at the
**park visit** level, with exact per-shed/tag counts retained for execution,
proof, and audit.

Rules:
- Parks contain sheds/tags; do not treat the whole park as one unsafe animal
  bucket.
- Do not treat "one shed = one tiny drive" as the final operating target.
- Combine compatible due work across sheds/tags in the same park when every
  animal stays inside its safe medical window.
- Kid goat+sheep groups can be combined when due windows, vaccine
  compatibility, max-shots-per-visit, stock, health, quarantine/ICU, and warm-up
  rules are all safe.
- Adult goat and adult sheep work stays species-specific inside the same park
  visit because adult vaccine sets differ: Goat Pox is goat-only; Sheep Pox and
  Blue Tongue are sheep-only; shared vaccines apply only where the active matrix
  allows.

Implementation note: this is a planning/sweeper target contract. It does not
mean doctors physically visit twice. It means the generated park drive plan must
show combined totals plus species-safe execution groups and shed/tag counts.

### 3. One-time batching hold and max shots

Decision: drive planning may delay a due group by up to 7 calendar days to
combine with a compatible same-park drive group, but the delay is one-time per
obligation/dose cycle. GoatOS must not keep rolling the same due item forward
to chase larger future groups.

Rules:
- Medical window beats batching window. If any animal would cross its
  `last_safe_date`, run the micro-drive now or escalate the blocker.
- Default batching policy is `max_batching_hold_days = 7` and
  `max_batching_hold_count = 1`.
- A 3-week booster can be held once to week 4 when it safely overlaps another
  compatible park group. It cannot be moved again to week 5 or week 6.
- A doctor visit can plan at most 2 shots per animal. If 3+ vaccines are due,
  choose the highest-priority compatible pair and schedule the remaining rows on
  the next safe date.
- Same-day compatibility is still governed by vaccine class: one live + one
  killed can run together when no blocker exists; the next live vaccine must be
  at least 4 weeks after the prior live.

Implementation note: persist explicit hold state (`original_due_at`,
`first_batching_hold_until`, `batching_hold_count`,
`last_batching_decision_at`) rather than recomputing from current due dates.

### 4. Procurement holding-park source trust

Decision: trusted vaccination evidence is limited to our supervised lifecycle:
our parks and our procurement holding parks. Procurement holding parks are
places where our team starts the vaccination course while animals are held for
4–5 weeks near the buying region before they enter regular sheds.

Rules:
- Trust only `source_context = our_park` or `procurement_holding_park`.
- A procurement holding-park dose must have governed location/holding-period
  context, SOP/video/physical validation, verifier/validator, and accepted proof.
- These trusted doses suppress duplicate work and advance the schedule after the
  animal enters our regular shed.
- Any vaccination claim outside our parks or procurement holding parks is
  untrusted. It does not suppress work. The animal starts/restarts the GoatOS
  schedule after accepted shed intake and warm-up/health gates.

### 5. Deferred animal recovery and re-entry

Decision: predefined defer states postpone vaccination but do not let animals
disappear from the process. Sick, under-treatment, ICU, quarantine, pregnancy
month 4–5, and post-breeding hold are safety blocks. When the animal becomes
eligible again, the kernel reopens the missed obligation from the
`ready_again_at` date.

Rules:
- If the nearest compatible same-park drive is within 7 calendar days of
  `ready_again_at`, add the animal to that drive.
- If the nearest compatible drive is more than 7 calendar days away, schedule a
  micro-drive inside the 7-day buffer, even for one animal.
- Multiple recovered animals whose 7-day buffers overlap may be batched together.
- Example: a July 10 drive can take animals recovered on July 4 and July 5 when
  safe. A July 12 drive is too late for those recovered animals; they need a
  smaller drive inside their own recovery buffer.

Implementation note: recovery/re-entry is event and bucket driven. Health,
quarantine/ICU exit, pregnancy-month transition, post-delivery, and
post-breeding-hold completion events enqueue affected animal/bucket work. Do not
scan the full million-animal herd.

### 6. Backend-owned config and command pagination

Decision: backend/database is the source of truth for protocol draft versions,
activation state, publish sequencing, and command-screen filtering/pagination.
Frontend may cache responses and hold unsaved form edits only.

Rules:
- Backend allocates vaccination matrix draft/version numbers and retries
  collisions.
- Publish of one scoped `vaccination.matrix` version is atomic; partial row
  activation is invalid.
- Company and park active-version selection lives in backend DB/audit state.
- Control Tower and Protocol Adherence use backend filters/sort/pagination,
  default page size 25, supported page sizes 10/25/50. Do not client-filter a
  hidden 200-row dump.
- Filtered-empty state must say no rows match the current filters; it must not
  show the global "healthy/no risk" copy.

## Implementation Sweep Targets

Before the implementation is called complete, remove/replace the old runtime
behavior still visible in code:

- Procurement holding classification in admin contracts and frontend work-state
  must use the governed 4-5 week procurement holding window, not old
  purpose-specific duration buckets.
- Procurement/vaccination evidence copy, errors, and tests should say
  `trusted procurement holding-park evidence` unless the identifier is a legacy
  table/type name.
- Control Tower, Protocol Adherence, Action Center, and verification queues must
  replace hidden `limit: 200` fetches with backend filters, sort, total count,
  page number, and page size.
- Config publish/draft version allocation must be backend-owned and atomic for
  the whole scoped `vaccination.matrix` version.

## Pending Clarification

The items below are not final until the owner confirms the exact rule.

1. Breeding and milking constraints: enforce one-month hold after breeding date,
   breeding-ready prioritization, and milking-time avoidance with exact animal
   fields and scheduling behavior.
2. Pregnancy precision: derive months 4-5 skip and post-delivery catch-up from
   breeding date / pregnancy month fields, not a vague pregnant flag.
3. Manohar operating story rewrite: align story language to Preventive Care
   (PC), mother-status ignore rule, trusted-source wording, and park-level drive
   grouping.
