# CEO EOD Update Runbook

This is the canonical procedure for preparing the daily Mesha engineering
update. It is tool-neutral and must be followed by any coding assistant or
maintainer that prepares the message.

## Non-negotiable rules

1. Send exactly one EOD update per report date. A manually sent message and an
   automated message count the same.
2. Use the latest SENT message in the established CEO Gmail thread as both the
   time baseline and semantic baseline.
3. Include verified work from all contributors across commits, branches,
   worktrees, pull requests, dirty files, recent artifacts, and explicit task
   notes. Do not inspect only `origin/main`.
4. Never describe unmerged work as shipped, released, landed, or live. Use
   `In progress:` or `Under validation:`.
5. Compare every proposed bullet with the previous sent update by product
   outcome, not wording. Omit repeated outcomes unless there is a material new
   capability, measured result, field milestone, decision, or state change.
6. Never invent work to fill a section.
7. Never expose commit hashes, pull-request numbers, merge mechanics,
   timestamps, migration numbers, cache keys, runtime details, agent names, or
   test bookkeeping in the CEO message.
8. **Never send raw Markdown or quoted email history.** A CEO update containing
   `**`, backticks, leading `>`, `On ... wrote:`, `<blockquote>`, `gmail_quote`,
   or an embedded prior EOD body is a hard failure and must not be sent.

## Reporting window and duplicate prevention

1. Determine the target report date in Asia/Kolkata, normally the previous
   calendar day.
2. Atomically acquire `/Users/raviteja/mesha/.eod-mail-lock/<YYYY-MM-DD>` with
   `mkdir`, using the IST report date. If it already exists, do not draft or
   send; report `Already sent` or `Another sender is preparing this date;
   skipped` after reading the exact thread.
3. Read the established Gmail thread directly by thread id and inspect every
   message carrying the SENT label. Gmail search is not authoritative because
   a new message may not yet be indexed.
4. If any sent title already covers the target date, alone or in a date range,
   stop and report `Already sent: <title> at <IST time>; skipped`.
5. Record the latest sent message timestamp as epoch seconds. The evidence
   window starts there and ends at the current run time.
6. Save the latest sent body split into its sections for semantic comparison.
7. Repeat the direct thread/SENT duplicate check immediately before sending.
8. Hold the lock through post-send verification. Retain it after success and
   write the sent message id into it. Recover it only after 60 minutes when no
   sender is active and no sent message covers the date.
9. If no product evidence exists after the baseline, do not send a filler
   message. Report `Nothing new since <IST time>; skipped`.

## Mandatory evidence sweep

Fetch all remotes before inspection, then inspect `goatos`, `mesha-exchange`,
`mesha-ops`, and `goat-sensor-lab`, including every worktree.

For every repository:

1. Run `git fetch --all --prune`.
2. Write the complete non-merge commit list since the baseline to a scratch
   file. Do not truncate it with `head` or a result limit.
3. Filter by author timestamp at or after the baseline so rebased historical
   commits are not treated as new daily activity.
4. Count commits by every relevant contributor identity and group every commit
   by conventional-commit scope or product module.
5. Map each feature, user-visible fix, and performance result to an email
   bullet or to a written internal omission reason: repeated outcome,
   engineering-only work, reverted work, or outside the reporting window.
6. List remote branches updated in the window and map each to a pull request,
   unpushed work, or no-PR work.
7. Run open and recently merged pull-request inventory separately for every
   repository. Record `none` explicitly when a query succeeds with no results.
8. For relevant pull requests, inspect author, update time, draft/merge state,
   changed paths, body, checks, review state, and recent commit headlines.
9. Detect umbrella pull requests and report a product outcome only once when
   child work has been folded into a combined branch.
10. Check `origin/main` reachability for each candidate bullet. State work
    plainly only when it is reachable; otherwise label it in progress.
11. Check every worktree with `git status --porcelain`, including untracked
    files and recently modified product artifacts.
12. Include explicit work notes and screenshots supplied since the baseline,
    while treating uncommitted or running work as in progress.

### No false-empty sections

If a section has a current-window product commit, changed product file, active
pull request, validation result, or explicit maintainer note, it must not say
`None`, `No changes`, or `No new milestone` unless every item has a specific,
evidence-backed omission reason in the coverage table. Explicit notes such as
an in-progress vaccination configuration refinement must appear as an
`In progress:` bullet.

Deduplicate the exact outcome, not the broad module. A new workflow, UI
behavior, release or staging state, validation result, blocker, architecture
decision, or operational scope is a new CEO-relevant delta even when the prior
mail mentioned the same feature family. When two drafts exist, reconcile both
against the complete evidence inventory and preserve every distinct supported
outcome; never select the shorter draft merely because it is shorter.

### Required product coverage

Build an internal coverage table with one row per module: Vaccination, Work
Board, Approvals and Tasks, Health, Feed, Weighing, Counts and Loads, Sales and
Procurement, Configuration registers, SOP Studio and Routines, Ask Mesha,
Admin redesign, Android, Performance, Automation and monitoring, Hardware,
and Exchange.

Each row must identify the contributor, merged or in-progress state, evidence,
and either the mapped email bullet or the reason for omission.

### Product boundaries

- **Goat OS:** operational product, admin web, Android, backend, analytics,
  workflows, assistant features, automation, performance, and reliability.
- **Hardware:** BLE tags, collars, gateways, sensors, device ingestion, and
  realtime movement from physical devices.
- **Exchange:** strategy, research, ownership and investment models, CEO and LP
  workflows, investor artifacts, prototypes, visual review, and all work in
  the standalone Exchange repository.

The Exchange source is the standalone `mesha-exchange` repository. Goat OS may
contain only a pointer and must not be used as proof that Exchange had no work.
Inspect Exchange commits, branches, worktrees, pull requests, dirty files,
`apps/web`, system design, UI specification, build notes, and prototypes.

For Hardware, compare with the previous Hardware bullets before drafting. Do
not repeat an already reported BLE, movement, risk, freshness, battery, or
insight outcome merely because its implementation was tuned again. Report only
a new milestone; otherwise state that there was no new hardware milestone.

## Goat OS classification

- **Implementation:** product code, bug fixes, performance work, features, UI
  changes, design-system work, and refactors. This is the lowest priority and
  must not dominate the update.
- **Configuration:** only user-facing farm or business setup that users can
  author or change, such as parks, species, genders, breeds, pen types, feeding
  sessions, feed catalogues and rates, diseases, medicines, SOP rules,
  registers, roles, permissions, operator assignments, and shifts.
- **Testing:** QA, browser and phone E2E, visual regression, realistic-data
  checks, staging or production verification, scale and performance proof,
  review findings, CI/CD validation, monitoring, dashboards, alert evidence,
  and regression guards.

Engineering configuration is not Configuration. Runtime settings, deployment,
CI/CD, monitoring, browser automation, and alerting belong in Testing. UI and
design work belongs in Implementation. Do not force equal bullet counts.

Configuration bullets must name the registers changed and the screens or apps
that now consume them. Do not compress distinct register outcomes into one
vague line.

## Required message structure

```text
EOD Update | <date or date range>

Goat OS

33% - implementation (bugfixing / optimization / new feature is the last priority)
<verified outcome bullets>

33% - configuration
<verified outcome bullets>

33% - testing
<verified outcome bullets>

Hardware
<verified outcome bullets>

Exchange
<verified outcome bullets>
```

The three `33%` labels must appear exactly as written. Do not add `Done`,
`In Progress`, `Next`, or another section. Put status inside each bullet. Keep
the language concise, outcome-level, and honest about state. Weekend work may
be included in substance, but do not put Sunday in the title unless explicitly
requested.

## Independent judge gate

After the first draft, run an independent fresh review with the prior sent
message, evidence inventory, coverage table, and proposed draft visible
together. Reject the draft unless all gates pass:

1. The baseline is the actual latest SENT timestamp.
2. All contributors were checked across commits, branches, worktrees, pull
   requests, dirty files, and task notes.
3. Every bullet has evidence inside the reporting window.
4. Every unmerged item is explicitly marked in progress.
5. Every Goat OS bullet is classified correctly.
6. Hardware and Exchange were checked in their actual repositories and notes.
7. No bullet semantically repeats a prior outcome without a material delta.
8. The message is concise, CEO-facing, and free of engineering mechanics.
9. The coverage table exists and maps every commit group to a bullet or a
   written omission reason.
10. Merged versus in-progress state was verified with `origin/main`
    reachability.
11. The final message passes the presentation gate below.
12. No section claims `None`, `No changes`, or `No new milestone` while its
    coverage row contains current-window product activity or an explicit
    maintainer note without a specific justified omission.

Keep an internal judge receipt listing removed repeats, relabeled in-progress
items, classification decisions, and presentation checks. Do not include that
receipt in the CEO email.

## Gmail presentation and send gate

The email must be a clean HTML reply in the established thread. Plain text and
Markdown are forbidden.

1. Build a `text/html` body containing only the new EOD update.
2. Use HTML section labels (`<strong>` or headings), paragraphs, and
   `<ul><li>` lists. Escape all dynamic text before inserting it into HTML.
3. Do not call the direct send action with reply context. That path can append
   the entire historical conversation.
4. Create a reply draft using the latest message as reply context so Gmail
   preserves the correct thread headers.
5. Immediately replace the draft MIME payload with the clean `text/html` body.
   Draft replacement must contain only the new update and no quoted history.
6. Read the updated draft and reject it if it contains `On ... wrote:`,
   `<blockquote>`, `gmail_quote`, leading `>`, raw `**`, backticks, Markdown
   headings, or any earlier EOD title/body.
7. Re-run the duplicate-date check.
8. Send the cleaned draft.
9. Read the SENT message back and verify all of the following:
   - it remains in the established Gmail thread;
   - its MIME payload contains `text/html`;
   - the new EOD title and body appear exactly once;
   - no earlier EOD body or quoted history is embedded;
   - no raw Markdown markers are visible.
10. If any post-send check fails, report the exact failure. Do not send a second
    correction for the same date unless explicitly instructed.

The presentation gate is absolute: **never send a CEO EOD update with raw
Markdown or quoted history.**
