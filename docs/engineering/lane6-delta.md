# Lane 6 — did the farm change legally overnight?

Lane 2 asks *"is the farm's data right at this instant"*. This lane asks the harder question:
**did the farm's data change LEGALLY since yesterday.** It is read-only, so it can run against the
real data; it reaches every animal, pen, load and day rather than only the screens someone
scripted; and transitions are where the bugs actually live.

Before this, day-over-day did not exist. Lane 2 has 60 point-in-time checks, only 4 of which
reference anything temporal, and there was **no snapshot store at all**.

## How it works

```
capture-delta-snapshot.mjs   once a business day  →  one reading, stored on local disk
check-delta.mjs              two readings in      →  was the change between them legal?
prove-delta-checks.mjs       no database at all   →  does each check actually discriminate?
```

A **reading** is 17 bounded sections: standing sections describing a state (how many living
animals stand in each pen, how far each vaccination round has got, which jobs are open on which
version of a written procedure) and movement sections bounded by the *previous* reading's day
(animals that left, sales tagged, pen moves raised, loads arrived, care handed in). Every section
is one capped `SELECT`, so a daily reading reads a day of movement and never a history.

Readings are stored **outside the product's database** — `~/.cache/goatos-delta-snapshots` by
default, `GOATOS_DELTA_SNAPSHOT_DIR` to move it. Nothing in this lane writes to stg.

## The eleven transition checks

Each is derived from a rule already written in `AGENTS.md` or a decision doc, and each names that
rule so a reader can go and disagree with the rule rather than with the check.

| Check | The rule it comes from |
|---|---|
| Work left open against an animal that left | vaccination obligations belong to live animals |
| A pen sold out of was never re-fed | `SaleFeedReductionDay` — the park's own correction clock, never a constant |
| A pen sold out of is still fed for the animals that left | the same rule, read on the number |
| A pen move raised late is treated as tomorrow's work | `ShiftingActionsDueFrom`, 13:30 IST cutoff |
| A weighing handed in reached nobody to review it | a submitted weighing raises a verification item |
| A weighing closed while a video was still waiting | the close gate is unconditional |
| A feed load that arrived owes a strip test and has none | every load reached owes an aflatoxin round |
| A pen worked yesterday is owed a visit and has none | pen visit tasks, the day after |
| A vaccination round went backwards | field work done cannot become undone |
| An open job was moved onto a newly published procedure | publishing changes the NEXT workflow opened |
| The herd total moved without anything leaving or arriving | animals leave by a recorded exit |

## The four outcomes, and why the fourth is the point

```
did not change illegally   the check ran and compared real things
changed illegally          the check ran and found a breach
nothing to compare         the check ran and nothing of its kind had moved
not checked                the check could not run, and the reason says why
```

**Two absent readings agree with each other, and that agreement is how a false pass is born.** So:

- A section that could not be read yields **no rows and a reason**, never an empty list.
- A section that hit its row cap is a **partial** reading and is refused for the same reason — a
  partial compared with a partial is two things agreeing about a part of the farm.
- `examined` counts what was **read and compared**. A check that examined nothing reports
  "nothing to compare"; it never borrows the word "pass" from a check that compared something.
- Checks are **one-sided where the rule is one-sided**. A pen move planned further out than the
  cutoff requires is ordinary planning, not an offence. A herd total that *rose* is births and
  purchases, which this reading does not carry, so it is never an accusation.

## Consistency is not correctness when the two things are not the same thing

Every reading records what it was taken against: the schema version, the store it came from, the
farms in scope, and an optional app version. `comparability()` **refuses** to compare two readings
across a migration, a redeploy, a different store, a different set of farms, the same day, the
wrong way round, or a movement window that does not line up. Across any of those the difference
between the two is a change in the *software*, and reporting it as the farm changing illegally is
a false accusation of the people who run it. A refused comparison renders **no verdict on any
check** — every one reads "not checked" with the refusal as its reason.

## Safety

This is the lane that could repeat the 2026-09-23 outage, so it reuses lane 2's machinery rather
than rewriting it: the same **shared** run lock (a second sweep refuses, it does not queue), the
same SELECT-only guard, the same explicit read-only transaction that is rolled back, the same
15-second statement timeout, the same structural row cap applied by the runner rather than trusted
from the check's own text, and 150ms between reads. One connection, one read at a time, 17 reads
in a run. The so-called read-only replica **is the primary**, which is why none of this is
optional.

`check-delta.mjs` and `prove-delta-checks.mjs` touch no database at all.

## Proof

- `node tools/dashboard-automation/prove-delta-checks.mjs` — **11 of 11** checks stay quiet on a
  legal overnight change *they actually examined*, fire on the illegal one, and for all **22**
  (check, reading) pairs say "not checked" rather than "fine" when that reading is missing, and
  again when it is only half taken.
- `node --test "tools/dashboard-automation/*.test.mjs"` — comparability refusals, the Asia/Kolkata
  business day, the correction-clock boundary, the 13:30 boundary, and the no-false-positive cases.
- Proven end to end against a local throwaway Postgres carrying the **real** migrated schema: all
  17 readings execute, and the herd-total check fired on ten animals that stopped being alive with
  nothing recorded as leaving, then went quiet when five left and were recorded as leaving. The
  throwaway database and its cluster were dropped afterwards.

The prover was itself mutation-tested: breaking a check so it cannot fire, breaking another so it
fires on correct data, and making a missing reading read as an empty one each turn it red.

## What is not derivable yet

- **Who owes a pen visit.** `pen_visit_tasks` no longer carries a named person — any of the park's
  named visitors may go — so the check judges that a visit is *owed at all*, in a park that has
  somebody to owe it to. It cannot judge that the right person got it.
- **Pen grain for feed.** The feed sheet is written per shed, so the sale-to-feed checks compare at
  shed grain and a sale out of one partition of a shared shed is judged against the whole shed.
- **Births and purchases.** No reading carries them, which is why the herd-total check is one-sided.

## Running it

```bash
export GOATOS_STG_READONLY_DATABASE_URL=...        # read-only; the store is separate
node tools/dashboard-automation/capture-delta-snapshot.mjs      # once a day
node tools/dashboard-automation/check-delta.mjs                 # compares the two most recent days
```

Neither is on a timer. Per §1 of the handover, the first live run is one supervised cycle.
