# The 17:30 feed-proof-times Slack report

Maintainer decision, 2026-09-05.

## What it is

Every feed day, at **17:30 IST**, Goat OS posts one message per park into the farm's Slack
channel (`C0BV1GXCX8B`) showing, for every pen on that day's feed sheet, **when each of its
three captures reached the backend**:

| capture | what it is | media |
| --- | --- | --- |
| feed weight | the feed on the scale, before it is given out | photo |
| feed distribution | the feed being given to the animals | video |
| water | the water being given | video |

They are the three refs on one `feed_distribution_completions` row, which is already at the
grain the report is at: **one pen, one session, one feed day**.

## The five decisions

**1. One row per PEN, with each session side by side.** Distribution runs twice a day (~09:00
and ~15:00) and each session has its own three captures, so the underlying facts are
pen-SESSIONS. They were first rendered one per row; seen on real data that is a 122-line wall
per park with the pen name repeated throughout. The table now gives each pen one row, with a
`MORNING` and an `EVENING` column group — 61 rows instead of 122 — and a pen with any gap
turns red while the `--` cells still say exactly which session and which capture is missing.

The session columns are read off the sheet, not assumed to be two: a park that adds a third
feeding gets a third column rather than silently losing it.

**2. The time is the UPLOAD time.** `proof_artifacts.uploaded_at`, stamped when the object
lands and the upload is marked completed. A phone that captured at 09:10 and only found
signal at 11:40 reports **11:40**.

This was chosen knowingly. It is the instant the farm can *prove*; a client-claimed capture
time is a number the phone supplies and nothing verifies. The consequence to carry: a late
upload reads as late work, and the honest reading of this report is **"when the evidence
reached us"**, never "when the animals ate". An upload still in flight is deliberately NOT
reported as done — `upload_state='completed'` is the moment the artifact exists.

**3. Latest upload wins.** A verifier rejection sends a pen back for a re-shoot and the
replacement capture becomes the row's ref. The report follows the ref, so it always shows the
capture that currently *stands* as the proof, never a superseded one.

**4. Missing is RED, via a ```diff block.** Slack has no inline text colour — a message can
bold, italicise or code-span, but it cannot colour a word. A fenced block tagged `diff` is
syntax-highlighted, and in that grammar a line beginning with `-` is a deletion and renders
red. So a pen-session missing a capture is written as a `-` line and arrives red; a complete
one is written with a leading space and stays plain. The monospace font of the block is also
what keeps the columns aligned, which Block Kit cannot promise.

Every complete row MUST start with a space and every incomplete row MUST start with `-`.
Nothing else may begin a line: `+` renders green and `#` renders as a comment, either of which
would silently say something the farm did not mean.

**5. One message per park.** CBE and CPT are run by different people, the table is long enough
with both sessions of every pen, and one park's sheet arriving late must not hold the other's
report.

## The expected set is the SHEET, not the completions

Reading `feed_distribution_completions` alone would make a pen nobody fed simply *absent*, and
a report that goes quiet exactly when work was skipped is the opposite of what 17:30 is for.
The rows come from the **live `feed_direction_issues` sheet** for the day (states `issued`,
`amended`, `locked`), so every planned pen-session appears, with `--` where a capture never
arrived. A park with no live sheet is told so in words rather than shown an empty table.

## No private scheduler

The task-kernel lock forbids a module keeping its own scheduler, so there is no cron
expression anywhere in this feature. `kernelstages.FeedProofTimesStage` rides the shared
**operational cadence** (5 minutes) and two properties do the rest:

- **17:30 is a GATE**, not a schedule: the notifier declines to write before the cutoff. The
  post therefore lands on the first tick at or after 17:30 (in practice 17:30–17:35), and a
  worker that was down at 17:30 posts as soon as it returns rather than skipping the day. A
  late post is worth having; a missing one is not.
- **Once per day** comes from the BUSINESS DATE in the idempotency key
  (`feed.proof_times:<business date>:<park id>`), exactly as `FeedLowStockNotifier` and
  `LoadAgeNotifier` do it. The first tick after the cutoff writes; every later tick that day
  writes nothing. That survives a worker restart, a redeploy at 18:00, and the HA pair ticking
  together — no state of its own, and redelivery is safe.

## Slack addressing

An **incoming webhook URL is bound to one channel by Slack** and cannot be retargeted by its
payload, so addressing a second channel means holding a second URL.

- `GOATOS_SLACK_CHANNEL_WEBHOOKS` — JSON object mapping channel id to webhook URL, e.g.
  `{"C0BV1GXCX8B":"https://hooks.slack.com/services/..."}`.
- `GOATOS_FEED_PROOF_TIMES_SLACK_CHANNEL` — the channel this report posts to. **Unset disables
  the stage**: an environment with no channel wired posts nothing rather than queueing rows no
  gateway can deliver.
- `GOATOS_SLACK_WEBHOOK_URL` is unchanged and remains the **incident** channel.

A Slack request addresses its destination the way every channel does: `recipient_ref` carries
the delivery address (the channel id, as an FCM token is for a push). An **unmapped channel id
FAILS** with `ErrChannelNotConfigured` rather than falling back to the incident webhook —
misdelivering a farm report into the incident channel would look like a successful send while
nobody who needed it saw it. The fallback applies only when a request names no channel at all,
which is what the incident path does, deliberately.

## The closed enum this uncovered

`notification_requests.notification_type` is a CHECK-constrained enum, last written in the
baseline with nine vaccination-cadence values. Three notifiers had already shipped values
outside it — `obligation_missed`, `feed_low_stock`, `procurement_load_overdue` — so their
INSERT could only have been failing `23514` on every tick.

That failure is quiet in the way that matters: the notifier returns an error into a cadence
log, nothing is queued, and the alert never arrives — which is indistinguishable from "there
was nothing to alert about". **A daily alert that has never once fired looks identical to a
farm with no low stock and no overdue load.**

Migration `000259_notification_type_daily_alerts.sql` widens the enum for all four together.
Fixing only the new one would have left the constraint still rejecting three shipped
notifiers — not a smaller change, the same change with three known defects left in.

`TestEveryNotificationTypeIsAllowedByTheCheckConstraint` compares the two sides — every
`NotificationType*` constant in `notificationbridge` against the enum in the last migration
that writes the constraint — and is what found `obligation_missed`, which two passes by eye
had missed. It is mutation-tested: dropping a value from the migration turns it red.

## Where it lives

| piece | file |
| --- | --- |
| report shape + Slack rendering | `backend/internal/feeddirection/domain/feed_proof_times.go` |
| the read | `backend/internal/feeddirection/adapters/postgres/feed_proof_times.go` |
| the notifier + 17:30 gate | `backend/internal/notificationbridge/feed_proof_times_notify.go` |
| the stage | `backend/internal/kernelstages/feed_proof_times.go` |
| per-channel Slack routing | `backend/internal/notification/adapters/gateway/gateway.go` |
| the enum widening | `backend/migrations/postgres/000259_notification_type_daily_alerts.sql` |

## Pinned by

- `TestSlackBodyMarksAPenMissingACaptureAsARedDiffLine` — mutation-tested: removing the `-`
  marker turns it red. The fixture carries two pens, the minimum that can prove the marker
  discriminates rather than being printed on every line.
- `TestSlackBodyPutsBothSessionsOnOnePenRow` — the pen is named ONCE; two rows per pen is what
  this layout replaced.
- `TestSlackBodyRendersAThirdSessionWhenTheParkRunsOne`.
- `TestSlackBodyDoesNotRepeatTheTitle` — the gateway posts `Title + "\n" + Body`, so a body
  carrying its own heading would render it twice in the channel.
- `TestAnUnmappedSlackChannelFailsInsteadOfMisdelivering` — mutation-tested: making it fall
  back to the incident webhook turns it red.
- `TestNothingIsPostedBeforeTheCutoff`, `TestOneMessagePerParkIsPostedAtTheCutoff`,
  `TestTheEventKeyIsStableAcrossTicksOfTheSameDay`.
- `TestFeedProofTimesReadsOneRowPerPenSessionAndTheUploadInstants` — the GRAIN proof. The sheet
  is one row per feed ITEM, so a pen fed three items has three rows for one pen-session; the
  fixture gives every pen-session three items precisely so a lost `GROUP BY` cannot pass.
- `TestFeedProofTimesMatchesThePenAcrossLabelCasing` — the pen join is `partition_key` on both
  sides (the identical generated column, migrations 000135 and 000137). Matching on the raw
  label would let "Part 3" and "part 3" read as different pens and file a fed pen as missing.
- `TestKernelWorkerSchedulesTheFeedProofTimesPost` — a notifier that is never scheduled posts
  nothing, and quiet is indistinguishable from "no sheet today".
