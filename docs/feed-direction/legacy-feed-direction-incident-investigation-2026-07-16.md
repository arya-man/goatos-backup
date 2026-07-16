# Legacy Feed Direction Investigation

Status: evidence-based incident report and corrected operating guide.

Incident date: 16 July 2026 (India time, based on the supplied WhatsApp and Apps
Script screenshots).

This document validates the supplied `Feed-Counting-and-Video-Verification-Flow`
guide against the repository's sanitized source findings and the execution-log
screenshots. It deliberately separates facts from hypotheses. The legacy Apps
Script source, live execution history, and live Slack history are not present in
this Git checkout, so this report does not claim to have inspected those systems
directly.

## Executive conclusion

The confirmed failure in the supplied log is Google Apps Script's `UrlFetch`
service quota, not a Slack API rate-limit response.

The log shows:

```text
method: chat.postMessage
error: Exception: Service invoked too many times for one day: urlfetch.
```

That error appears at approximately 09:16:07 and again at 09:16:16. The nearby
debug keys are for `feed_thread_consumption_Session1_2026-07-16_CBE_Yashoda 7`
and `...Yashoda 8`, so the supplied evidence is specifically from the morning
Session 1 consumption-message path. It is not proof that the packing path itself
failed.

The WhatsApp report that Yashoda sheds did not receive messages is therefore
consistent with the failed delivery attempt. The script could not complete the
outbound HTTP call. A sheet flag, if it was written before that call or by a
separate path, must not be treated as proof that Slack received the message.

What is not proven yet:

- which Google account exhausted the quota;
- who created the 07:15 trigger;
- how many UrlFetch calls the function made that day;
- whether duplicate triggers, manual reruns, retries, or another Apps Script
  project consumed the rest of that account's quota;
- whether a Slack message was posted and later deleted.

The earlier claim that a particular person owns or executes the script is not
established by the repository evidence. The script owner, sheet owner, trigger
creator, current viewer, and Slack poster can be different identities.

## Evidence reviewed

### Supplied live evidence

1. WhatsApp reports that feed directions/messages were missing for Yashoda sheds
   while Y6 was present.
2. The experiment workbook screenshot shows CBE Yashoda rows in `Experiment Feed
   Config`, including Yashoda 2 with count 18.
3. The user reports that `Count-DB` shows Yashoda 2 with count 20. The supplied
   evidence does not establish which count is operationally correct.
4. The Apps Script log shows two failed `chat.postMessage` attempts with the
   Google exception `Service invoked too many times for one day: urlfetch`.
5. The Slack screenshots show experiment-related CBE/CPT channel names, but they
   do not provide a complete code-level channel inventory.

### Repository evidence

The sanitized repository findings record that the legacy flow uses:

- Counting DB tabs such as `DB` and `FutureDB`;
- the main Feed Directions Automation workbook, including `Count-DB`, `Feed
  Direction`, supply-planning/validation tabs, and packing/consumption forms;
- the separate Experiment Feed Directions Automation workbook, including
  `Experiment Feed Config`, `Feed Direction`, and `Feed Packing Experiment
  Sheds`;
- Apps Script/Slack automation for generation, packing, consumption,
  transport, proof, alerts, retries, and processed flags.

The repository does not contain the live legacy `slack-automation-scripts`
directory or the current Apps Script project. The source-finding note refers to
historical files that were reviewed during earlier source work, but those files
are not available in this checkout for a fresh line-by-line audit.

## Reconciled operating guide

### Sheets and their roles

| Surface | Role | Important caution |
| --- | --- | --- |
| Counting DB: `Template` | Shed/farm structure and setup | It is not the daily count itself |
| Counting DB: `DB` | Completed-day count record | Historical/official count for that date |
| Counting DB: `FutureDB` | Forward projection | A working forecast used to prepare feed |
| Main Automation: `Count-DB` | Imported eligible normal-shed counts | Normal branch; do not assume it is the experiment source |
| Main Automation: `Feed Direction` | Generated normal directions | Contains sessions, sheds, counts, feed items, quantities, and flags |
| Main Automation: supply/validation tabs | Feed and ration calculations | Formula/validation evidence, not independent source truth |
| Main Automation: `Feed Packing Form` | Packing submission/proof rows | Must retain the Slack reference and media evidence |
| Experiment Automation: `Experiment Feed Config` | Experiment count and feed quantities | The guide identifies this as the experiment quantity input |
| Experiment Automation: `Feed Direction` | Generated experiment directions | Generated from experiment config rows with count greater than zero |
| Experiment Automation: `Feed Packing Experiment Sheds` | Experiment packing submissions | Separate from normal packing |
| Video Verification DB | Review surface for packing/consumption/distribution media | An empty verification view does not prove that the upstream form was empty |

The local source findings also report `CPT Validation`, `CBE Validation`, hidden
validation copies, `Validation-BW`, `Feed-Energy-Protein`, supply-planning
tabs, and `Feed Consumption & Wastage` as legacy calculation/proof surfaces.
They contain formula-driven rules, feed vectors, processed fields, and
reconciliation signals. Broken references and inconsistent labels were observed
in historical workbook evidence, so those tabs must be checked rather than
treated as immutable business rules.

### Expected daily timeline

The supplied guide and repository findings describe an approximate India-time
sequence, not a guaranteed exact scheduler timestamp:

```text
~14:00       Counting DB is prepared for the next cycle
~23:30       DB receives the completed-day count
~00:30       FutureDB projection/night check is updated
~01:00       FutureDB is rolled forward for tomorrow
~03:00       Experiment Feed Direction is generated from Experiment Feed Config
~07:15       Experiment packing or morning consumption automation runs, depending on function
~07:30       Main normal-shed packing run is expected
~09:00       Normal Session 1 serving/consumption timing in the operating guide
~14:00       Changed directions / Diff path may run after movement information
~15:00       Session 2 serving/transport timing in the operating guide
```

The screenshots establish a 07:15 operational expectation and a 09:16 failure,
but they do not establish the exact trigger configuration. Apps Script time
triggers can run within a time window rather than at an exact minute, so the
actual trigger configuration must be read from Apps Script.

### Normal branch

The normal branch reads the forward projection, copies eligible rows into
`Count-DB`, skips excluded baby tags such as K0/K1, reads the farm template,
applies approved feed rules, and writes one or more Feed Direction rows by farm,
shed, breed/age context, and session. The template controls the feed set and
session shape. Exact ration quantities are legacy implementation behavior and
must not be copied into GoatOS as unreviewed global constants.

### Experiment branch

The experiment branch reads `Experiment Feed Config`, generates experiment
directions for rows with a positive count, and posts to experiment-specific
operational channels/forms. Experiment sheds are intentionally excluded from
the normal feed branch. The supplied screenshots visibly include CBE/CPT
experiment packing/distribution/wastage naming patterns, but the repository does
not contain a complete live channel map. Channel membership or code inventory
must therefore be checked in the actual Slack workspace and Apps Script project.
The operating guide says CBE and CPT use their farm channels for normal work and
experiment work uses separate experiment channels; the exact live mapping still
needs to be exported from the script rather than guessed from a sidebar.

### Proof and verification

The expected proof chain is:

```text
direction row
  -> Slack packing/consumption instruction
  -> staff action and video/form submission
  -> Automation packing or consumption form
  -> Video Verification import/view
  -> media review: Pending, Verified, or Rejected
```

Rejected proof needs a reason and rework path. A packing shortfall or missing
delivery proof must return the logical task to a resend/rework state. It must not
be hidden by a stale processed checkbox.

### Triage map

| Observation | First check |
| --- | --- |
| Shed absent from direction | FutureDB, Count-DB, then Feed Direction for that date |
| Direction exists but no Slack message | Apps Script execution, trigger identity, UrlFetch error, then exact Slack search |
| Slack message exists but no form/video | Form response, media link, and Video Verification import |
| Verification view is empty | Upstream packing/consumption form and import mapping |
| Duplicate or doubled feed | Duplicate generation run, Diff regeneration, duplicate trigger, and idempotency state |
| Experiment count differs from Count-DB | Do not merge silently; obtain the source-owner decision for that date |
| `Processed` is checked with no Slack timestamp | Treat as an audit/control defect and resend only after confirming no delivery |

Other legacy symptoms have straightforward first checks: a sudden FutureDB total
drop points to an incomplete projection; a missing CPT/CBE shed points to the
projection/import/direction chain; and an empty Video Verification view points
first to the upstream packing form or its import mapping. These symptoms are
diagnostic starting points, not proof of a single root cause.

## Corrected flow

### Counts and directions

`DB` is the historical/official count for a completed day. `FutureDB` is a
forward projection used to prepare feed. The main automation copies eligible
projected rows into `Count-DB`, applies template and ration rules, and writes
the main `Feed Direction`.

Experiment sheds are a separate branch. The guide and repository findings say
that experiment quantities come from `Experiment Feed Config`, not from the
main `Count-DB`/`FutureDB` calculation. The main Feed Direction may show an
experiment marker or zero normal feed so the normal branch does not generate a
second ration for those sheds.

This separation explains why the Yashoda 2 count mismatch matters, but it does
not explain the UrlFetch exception. They are two separate defects:

| Problem | Confirmed meaning | Operational risk |
| --- | --- | --- |
| `20` in Count-DB versus `18` in Experiment Feed Config | The two source surfaces disagree; the authoritative value is unresolved | Wrong experiment quantity or an inconsistent direction |
| `Service invoked too many times for one day: urlfetch` | The executing Apps Script identity could not make another UrlFetch call | Slack messages may not be delivered |

Do not fix the count by copying 20 into the experiment sheet, or 18 into
Count-DB, until the Feed Director confirms which source represents the actual
animal count for that date. The mismatch should be recorded as a data exception
and resolved once, with an audit note.

### Slack delivery

The intended sequence should be:

```text
read eligible direction row
  -> build message
  -> call Slack chat.postMessage through UrlFetchApp
  -> verify HTTP/Slack response and capture channel + message timestamp
  -> save the message link/ts and delivery time
  -> only then mark the row as sent/processed
```

The last two steps are the important control. `Packing Processed`, `Consumption
Processed`, or a similar checkbox is a local workflow state, not delivery proof.
The proof is the successful Slack response plus the saved message timestamp or
permalink. A failed UrlFetch call must leave the row in `Failed`/`Pending` with
the error, not in `Processed`.

No person should have to click an acceptance checkbox merely because a Slack
message was supposed to be sent. A person may review or approve the feed
direction according to business policy, but the automation itself must not
claim delivery until Slack confirms it.

### Consumption versus packing

The supplied 09:15–09:16 log is keyed to
`feed_thread_consumption_Session1_...`. Therefore:

- it directly supports a failure in morning Session 1 consumption posting;
- it does not, by itself, prove that experiment packing messages failed;
- the packing path needs its own execution rows and Slack search by date/channel;
- a report saying “feed directions missing” must identify whether it means
  packing, consumption, distribution, or a video-verification row.

This distinction was missing from the shorter guide and is now explicit.

## Why it happened suddenly

The daily schedule alone does not guarantee that the daily run stays below the
quota. Apps Script service quotas are per user and are cumulative over a
rolling 24-hour period from the first request, not necessarily reset at local
midnight. The current Google documentation lists URL Fetch calls as 20,000 per
day for consumer accounts and 100,000 per day for Google Workspace accounts;
Google can change these limits.

Therefore, “we send 50+ messages twice” is not enough by itself to prove quota
exhaustion. At roughly 100 outbound calls, it is far below those documented
limits. The real fetch count may be much higher if one shed message causes
additional calls for channel lookup, thread lookup, file/media handling, retries,
or updates. Other scripts running as the same Google identity can also consume
that identity's Apps Script service quota.

The most likely explanations, in descending order, are:

1. The trigger account had already spent most of its rolling UrlFetch quota on
   other executions or other scripts.
2. A duplicate 07:15 trigger, manual run, retry, or loop caused the same rows to
   be processed more than once.
3. The function performs several UrlFetch calls per shed/message, so the visible
   message count understates the fetch count.
4. A separate quota or runtime issue exists in addition to UrlFetch.

These are hypotheses until the execution history and trigger list are checked.
The screenshot proves the quota error; it does not identify which of the four
caused the extra usage.

This is also not the normal shape of a Slack API rate-limit error. Slack's
documented response is normally a Slack error such as `ratelimited`/HTTP 429,
with a retry interval. The supplied error is thrown by Google Apps Script and
names `urlfetch` directly. Manohar's statement that the “Slack API limit” was
exhausted is therefore not supported by this screenshot; it may be a shorthand
for the integration failing while trying to call Slack.

## Exact verification procedure

The following checks are read-only and should be done before changing any sheet
flags or manually resending all sheds.

1. Open the bound Feed Directions Apps Script project from the sheet. In
   **Executions**, filter 16 July around 07:15–09:20 and record the function
   name, start time, duration, success/failure, and full exception for every run.
2. Search the execution log for the affected date and the exact keys for the
   affected sheds. Count how many `chat.postMessage` attempts were made and how
   many additional UrlFetch operations the function performs per row.
3. Open Apps Script **My Triggers** while signed in as each possible operator,
   especially the account that created the project and the account that may have
   configured the 07:15 job. Record trigger function, schedule, creation
   timing, and whether more than one trigger calls the same function.
4. Check the Apps Script project sharing panel and the bound sheet's Drive
   ownership separately. Do not infer trigger ownership from sheet edit access.
5. Search Slack by exact channel, date, shed, and message text. For every row,
   classify it as `posted`, `not found`, `posted then deleted`, or `unknown`.
6. Compare the result with the sheet's stored Slack timestamp/link. A checked
   processed cell with no successful timestamp is an audit failure, not delivery
   evidence.
7. Reconcile Yashoda 2's count with the Feed Director before regenerating or
   resending its direction. Preserve the original values and record the reason
   for the correction.

Installable Apps Script triggers always execute as the account that created the
trigger, even when another editor opens the sheet or manually interacts with
it. An account cannot see another account's installed triggers in **My
Triggers**. That is why the trigger creator, rather than the current viewer,
must confirm the execution identity and quota context.

## Immediate operational response

For this incident:

- stop repeated manual runs until the execution identity and failure rows are
  known;
- do not mark failed rows as processed;
- capture the failed shed/date list from the execution log;
- after the quota window recovers, resend only rows with no confirmed Slack
  timestamp;
- verify the actual Slack message in the relevant channel before closing the
  incident;
- separately resolve the Yashoda 2 count mismatch.

If operations cannot wait for the quota window, use an approved manual Slack
message as a temporary continuity measure and record it as a manual bridge. Do
not silently alter processed flags to make the sheet look complete.

## Required engineering fix

The legacy automation should be changed or replaced so that:

- each logical message has an idempotency key such as date + farm + shed +
  session + message type;
- duplicate triggers and concurrent manual runs are blocked with a lock;
- the script keeps a durable attempt record with start time, executing identity,
  fetch count, Slack response, message timestamp, and error;
- only a verified Slack `ok` response sets `Processed`;
- UrlFetch quota errors are classified as retry-later, not as successful or
  retry-immediately;
- retries are bounded and scheduled after the quota window, with an alert;
- the script reports a delivery summary: attempted, posted, failed, skipped,
  and unresolved;
- the automation exposes the trigger function and owner/creator in its runbook;
- the long-term path moves outbound delivery to a monitored service with its own
  identity and durable queue rather than relying on mutable sheet checkboxes.

Using `fetchAll` or a webhook may reduce runtime or simplify transport, but it
does not by itself solve a daily UrlFetch quota problem: those are still outbound
requests and still need idempotency, accounting, and failure proof.

## Validation of the supplied guide

The supplied guide is directionally correct about the business split:

- main normal sheds and experiment sheds are separate branches;
- Experiment Feed Config is the experiment quantity input;
- Feed Direction and packing/consumption forms are intermediate operational
  surfaces;
- Slack posting and video verification are separate stages;
- processed flags and sheet links are legacy implementation signals.

The following corrections are required and are incorporated in this report:

| Original implication | Correction |
| --- | --- |
| A processed tick means Slack delivery | It is only a local state; require a successful Slack response and stored timestamp/link |
| A missing Slack message is automatically a generation problem | First distinguish source row missing, message not attempted, UrlFetch failure, Slack rejection, and deletion |
| The 09:15 log proves packing failure | The keys shown are Session 1 consumption keys; packing needs separate evidence |
| 50+ messages explain the daily quota failure | Not proven; documented quotas are much higher, and per-row fetch multiplication/other executions must be measured |
| The sheet owner or current editor is the quota user | Installable triggers run as their creator; confirm the creator in My Triggers |
| Count-DB and Experiment Feed Config can be merged casually | They currently represent different branches; resolve the Yashoda 2 mismatch with an owner decision and audit note |

## Evidence gaps to close

This report should be updated after the following artifacts are obtained:

- Apps Script project export or read-only source snapshot;
- execution details for the full 16 July morning run;
- My Triggers screenshots/export from each possible trigger creator;
- exact Slack channel/message search results for each affected shed;
- the source row and date that establish whether Yashoda 2 is 18 or 20;
- confirmation of whether the script was manually run or retried before 09:16;
- the script's exact success/processed-state write order.

Until those artifacts are available, the defensible incident statement is:

> On 16 July, the morning Session 1 consumption automation attempted to call
> Slack but Apps Script rejected UrlFetch because the executing account's daily
> UrlFetch quota was exhausted. The exact source of the accumulated usage and
> whether any prior messages were posted require Apps Script execution and
> trigger-owner evidence. The Yashoda 2 count disagreement is a separate
> unresolved source-data defect.

## References

- Repository source finding: `context/source-findings/feed-direction-workbook-automation-findings.md`
- Repository source finding: `context/source-findings/drive-docs-findings.md`
- Supplied operating guide: `Feed-Counting-and-Video-Verification-Flow.docx`
- Google Apps Script quotas: <https://developers.google.com/apps-script/guides/services/quotas>
- Google Apps Script installable triggers: <https://developers.google.com/apps-script/guides/triggers/installable>
- Slack `chat.postMessage`: <https://api.slack.com/methods/chat.postMessage>
- Slack rate limits: <https://api.slack.com/apis/rate-limits>
