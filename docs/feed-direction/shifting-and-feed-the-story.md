# When animals move, who feeds them?

*The story of shifting and feed, and what we changed on 2026-08-10.*

This is written to be read start to finish by someone who was not in the room. It explains the
farm situation first, then what we built, then the decisions we deliberately did **not** take and
why. If you only read one section, read [The morning that breaks it](#the-morning-that-breaks-it).

---

## 1. The two clocks

Two things happen on this farm that look unrelated and are not.

**Animals move between sheds.** Someone raises a *shifting* — "move ten animals from Castro 1 to
Godel 2". A park head approves it. An operator walks the animals across and films it. The herd
register updates.

**Feed is packed the day before it is eaten.** Feed for Thursday is directed and packed on
Wednesday. At **07:00** the day's sheet is generated from the head count in each pen and then
**frozen** — from that moment the packers are working to a fixed document. They weigh out bags,
pen by pen, and film the work. At **15:30** the transport leaves and the feed is physically gone.

Each clock is sensible on its own. The trouble is entirely in how they meet.

---

## 2. The morning that breaks it

Here is the sequence that made this change necessary. Every time is a real one from the system.

| Time | What happens |
|------|--------------|
| **07:00 Wed** | Thursday's sheet is issued. Castro 1 holds 40 animals, so it is packed for 40. |
| **09:00 Wed** | The packer fills Castro 1's bags, films the video, submits it. |
| **09:30 Wed** | The verifier watches the video and approves it. Castro 1 is **done**. |
| **10:00 Wed** | Someone raises a low-priority shifting: **ten more animals into Castro 1**. |
| | Low priority raised before 13:30 means the animals walk **tomorrow — Thursday**. |
| **Thursday** | Fifty animals stand in Castro 1. The feed in the shed was packed for forty. |

Ten animals get nothing.

Nobody did anything wrong. The packer packed exactly what the sheet said. The verifier approved a
video that was genuinely correct at the time. The shifting was raised through the proper channel.
The sheet was simply written before the last piece of information existed, and nothing went back to
correct it.

And the reason nothing went back is the part worth stating plainly: **the feed projection was
waiting for a park head's approval.** Until 2026-08-10, a raised-but-unapproved movement counted for
nothing. The animals were coming, the operator knew they were coming, and the feed sheet did not.

---

## 3. What we changed

Two changes, both landing at **14:00** — the `correction_time` both feed workflows already ran on.
We added no new clock and lifted no lock.

### 3.1 A raised movement now counts before anyone approves it

The feed projection used to count only *authorized* movements. It now also counts movements that are
merely **raised**, and only a **rejection** stops the clock.

The trade is explicit, and it is the heart of the decision:

> Over-packing for a movement the park head later turns down costs one bag of feed.
> Under-feeding animals that really arrive costs the animals.

A rejected or cancelled movement drops out immediately, so the exposure is one afternoon of
over-packing in the worst case.

**Which day does a raised movement count toward?** Not the day it was raised — the day the animals
are expected to *walk*. That is the same lead time the operator's own work queue uses:

- raised **before 13:30 IST** → the animals move **tomorrow**
- raised **at or after 13:30** → they move **the day after**

This matters more than it looks. A movement raised at 13:45 does not reach tomorrow's sheet, because
those animals are not there tomorrow. Had we anchored on the raise *day* instead, we would have fed
the destination a full day early — the same over-feeding bug we were trying to fix, moved one day
earlier.

### 3.2 The 14:00 correction takes the video back

Recomputing the sheet is not enough on its own. In the story above, Castro 1 was **already packed and
already approved** by 09:30. A corrected number on a screen nobody is looking at changes nothing in
the shed.

So the 14:00 correction now reaches into the packing work:

- the pen goes back to the packer,
- the verifier's pending item is **withdrawn** from their queue,
- the earlier approval is **stripped** from the row,
- and the card comes back carrying the **new quantities** and a sentence saying why.

**Yes, we take back an already-approved video.** That was a deliberate call. An approved clip proves
the packer packed the *old* quantity — which is now the wrong quantity. It is no more usable than an
unapproved one. Approval is not what makes feed correct; the number is.

---

## 4. Two narrowings we care about

Making an operator re-film work is expensive and slightly insulting if it turns out to be
unnecessary. So the reopen is spent as narrowly as we could make it.

### Only where the number of mouths actually moved

The correction reprints a pen's sheet for several reasons — a renamed ration group, a re-authored
gram rate, a cosmetic label change. None of those mean the packer packed the wrong amount. Only a
change in **head count** does.

So the reopen is driven by a strictly narrower signal than "which sheds did the sheet touch".

### Per pen, never per shed

Castro 1, Castro 2 and Castro 3 are one building and three different sets of animals on three
different rations. If Castro 2 gains ten animals, the packers of Castro 1 and Castro 3 must not be
told to re-film work that never changed.

This is the same distinction that migration `000137` exists for. Collapsing to the shed here would
undo it.

### Experiment pens are exempt entirely

Experiment rations are authored as an **absolute kg total per pen** — "give this pen 12 kg" — not as
grams per animal. A head-count change moves no quantity there at all. Reopening an experiment pen
would discard a perfectly good video for a sheet that did not change by a single gram.

---

## 5. The subtle part: the operator's card looks identical

There is no new status. A reopened pen uses the existing `rework` state, which the app folds into
the operator's ordinary **"pending"** bucket — "needs my action again".

Which means the status chip on a reopened pen is **byte-for-byte identical** to the chip on a pen
nobody has packed yet.

That is fine for the work queue, and it is a real problem for the human. The packer already packed
this pen this morning. If the card comes back looking like a fresh one, they have no way to know that
the numbers on it are different from the numbers they packed to — and no reason to suspect it.

So the card carries a backend-composed sentence:

> **Animals moved in or out of this pen, so the feed quantities changed. Pack the new amounts and
> record a new video.**

It sits above the quantities, because the first thing the packer needs is *the numbers below are not
the numbers you used*. It says the farm thing, not the system thing — it names no table, no job and
no correction window.

The screenshot fixture puts a reopened pen **first, above the fold**, specifically so that deleting
that line would change the image and fail the check. A fixture that would look the same with the
feature removed proves nothing.

---

## 6. What we deliberately did not do

**We did not lift the transport lock.** It was never in the way — transport is 15:30, the correction
is 14:00. Past 15:30 the feed has physically left the store and a correction cannot reach the shed,
so amending a locked sheet stays refused. If a future change needs to "just unlock it", that is a
conversation, not an implementation detail.

**We did not make approval irrelevant everywhere.** Approval still gates whether the operator may
*carry out* the movement. All that changed is that it no longer gates whether the destination gets
*fed*. Those are different questions and we kept them apart.

**We did not touch feed distribution.** Distribution is still proved per shed-session. Packing merged
to one video per pen per day; distribution did not. The two are now deliberately different, and the
code keeps two separate key functions rather than one with a flag, so the wrong grain cannot be
reused by accident.

**We did not invent a new status.** Adding a "reopened" state would have meant every screen,
filter and count learning about it. The sentence carries the meaning; the state machine stays as
small as it was.

---

## 7. Bugs this work turned up

Building the end-to-end proof surfaced three defects that were already in the tree and that no
existing test could see. All three are fixed in the same change.

**1. Every packing verification item reached the verifier with no location on it.**
`CompletePackingResult` had carried `ShedName`/`PartitionLabel` since the packing gate was first
written, and **nothing ever filled them**. The enqueue read them faithfully and composed an empty
subject label. A verifier's queue card named no pen at all. This is the classic
declared-but-never-populated shape: a test that asks "is the field carried?" passes throughout the
entire outage, because the field is there — it is just always empty. Only a test that asserts the
*output string* catches it.

**2. All three feed domain events were undeliverable.**
`feed.packing.completed`, `feed.distribution.completed` and the inert `feed.direction.completed` each
hand-built their envelope and each omitted the same six schema-required fields
(`occurred_at`, `recorded_at`, `actor`, `visibility_scope`, `evidence_refs`), spelling `producer` as
a bare string where the contract wants an object. The failure mode is why it survived: the database
write succeeds, the transaction commits, the operator's screen flips to completed — and the event is
rejected *later*, by the relay, as `invalid_event_envelope`. It never reaches a consumer and nothing
on any screen says so. Three independent hand-built copies is how three copies drifted from one
schema; they now share one builder.

**3. A `uuid = text` comparison and a provenance string in a uuid column** in the new reopen path —
both mine, both caught before they could reach anyone.

Then an independent review of that work found three more, and the first is the most instructive:

**4. The envelope fix above was itself a partial fix.** It restored the five *missing* fields and
left the *values* wrong: `aggregate_type` had no `feed_*_completion` member, `subject_type` was
`shed` where the enum only has `location`, and the system fallback said `actor_type: "system"` where
the enum says `system_rule`. A wrong value fails validation exactly as a missing field does, so all
three events were still undeliverable. The reviewer's real finding was the *absence of a guard* —
nothing validated a produced feed envelope against the schema, which is why it could be wrong twice.
There is now a test that compiles the real schema with the production validator and runs every feed
envelope through it, and the `subject_type` is no longer a per-caller string at all: it is derived
inside the builder, because a mutation test proved one call site could drift back while a
builder-level test kept passing.

**5. Two legacy queued videos, one silently discarded.** The pen-day merge left the phone able to
hold two pre-upgrade packing rows for one pen — Morning and Evening, each with its own video and its
own idempotency key. Both drain to the same pen-day row; the second returned **200 with its video
never recorded**. This is the accepted-and-ignored failure the strict `session_no` rejection exists
to prevent, reappearing one layer above the API. It is now `409 packing_already_recorded`.

**6. The migration had no rolling-deploy window** — and my first repair of *that* was theatre too.
`000148` dropped the session-bearing unique index in the same step that added the pen-day one, so
every still-running old instance would have failed every packing submission with `42P10` until the
rollout finished. I split it into an expand file and a contract file — and shipped both in the same
release. `backend/cmd/migrate` applies every pending migration in one run, so they execute
back-to-back, still before the new binary serves, and the window is exactly as it was. Splitting the
SQL is not the control; shipping in two *releases* is. The contract migration is now absent from this
release entirely, and `make feed-packing-rollout-guard` fails on any post-`000148` migration that
drops the compatibility index, so the rule is enforced rather than narrated.

The general lesson, and the reason the story is worth telling: **the first two bugs were invisible to
every unit test in the repository and visible within seconds of walking the real path — and the
first repair of one of them was still wrong, because a fix without a guard is a guess that happened
to compile.**

---

## 8. Where the rules actually live

| What | Where |
|------|-------|
| The standing rule, always in an agent's context | `AGENTS.md` → "Confirmed AFTERNOON FEED CORRECTION rule" |
| The full technical decision | `docs/decisions/feed-distribution-verification.md` |
| Which feed day a raised movement counts toward | `counts/domain.FeedShiftingRaisedEffectiveBusinessDate` |
| Which pens get reopened | `feeddirection/domain.CellDiff.HeadCountChangedPens` |
| The reopen itself | `feeddirection/app.reopenPackingForCorrection` → `ports.ReopenPackingForFeedChange` |
| The operator's lead time (shared with the work queue) | `counts/domain.ShiftingActionsDueFrom` |

Each of the two narrowings in §4 is pinned by a test that was **mutation-tested** when written:
deleting the experiment branch, or keying the reopen on the shed instead of the pen, each turns one
red. A guard nobody has tried to break is a guard nobody knows works.
