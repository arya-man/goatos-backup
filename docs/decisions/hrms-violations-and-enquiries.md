# HRMS violations, enquiries and the clock-in check

Maintainer decisions, 2026-09-30. Migrations 000471 and 000472.

## Violations

- A violation is recorded against **one person**. It names a **violation type** from the published
  HRMS SOP (`hrms.violations`, edited on People / HRMS > HRMS SOP) and may carry a **fine in
  rupees**.
- **A violation type carries no money.** The maintainer's words: "never map money to mistake,
  both are separate". Nothing about the type fills in a fine. The fine is typed each time, and a
  blank fine means no fine ("No fine"). A document that gives a type a fine is refused at save.
- Fines are **recorded only**. Nothing deducts them from pay.
- A violation recorded by hand, or from an enquiry, is **final when recorded**. A mistaken one is
  **withdrawn** with a reason and is never deleted.
- **Who records:**
  - HR and the CEO/CXO record on the web, for every park.
  - A **park head** records on the phone (Tasks › For me › Violations), and only against people
    whose home park they head. Their list shows the month's violations for their parks, newest
    first.

## Enquiries

- An approved **animal death** opens an enquiry for that park. This applies to every death; the
  park head decides whether anyone is penalised.
- The enquiry is filled by the **park head on the phone** or by **HR on the web**.
- It asks the questions of the HRMS SOP version it was opened on (pinned). It is due after that
  version's deadline (48 hours seeded).
- Submitting records a violation for each person named, in the same transaction. Naming nobody is
  a valid report.

## The clock-in check (automatic violations)

- **Who is checked.** Anyone **mapped to a shift** (Timetable) whose home park has a **start
  time** for that shift. A shift with no time set is never checked. Neither is anyone on the day
  their shift was set: nobody is late for a shift they were given at noon.
- **Late.** Clocking in more than the **grace** after the shift's start raises one **"Late
  clock-in"** violation. The grace is 15 minutes, authored on the HRMS SOP's Clock-in check card.
- **Did not clock in.** No clock-in at all by the **shift's end** raises one separate **"Did not
  clock in"** violation. A shift with no end time counts to midnight; one ending before it starts
  runs past midnight.
- **Leave.** A day covered by leave the person **applied for** (pending or approved) is never
  checked. If a pending leave is later rejected, the next run raises the violation, as long as the
  day is still inside the checked window (today and yesterday).
- **They wait for HR.** An automatic violation is **Waiting for HR** until HR decides it on
  People / HRMS > Violations:
  - **Keep:** it counts from then, with a fine HR types (blank = no fine) and an optional note.
  - **Close:** HR gives a reason. It never counts, and the reason stays on it.
- **When it runs.** The `hrms-attendance` kernel stage runs **every 5 minutes** (operational
  lane). It covers **today and yesterday** (IST) with one bounded read and one set-based insert.
  The natural key (person, day, kind) means a re-run, an overlapping tick or a closed violation
  never raises the same one twice.
- **What it records.** Each violation carries a backend-composed fact, for example "Clocked in
  9:10 am · General shift starts 8:30 am · 40 min late".
- **Which types.** The SOP names the types the check raises. HR may rename them (the key stays),
  pick other types, or choose **Off** for either half.

## Days the check never checks

These are the maintainer's decisions of 2026-09-30 (migration 000473).

- **Weekly off.** Each person has a **fixed weekly off**: one or more weekdays, ticked beside
  their shift on the Timetable. HR must set a shift first, because the weekly off belongs to it.
  Changing someone's weekly off does not count as a new shift.
- **Holidays.** HR enters a date on the Timetable's **Holidays** card, for **every park** or for
  **one park**. There are none today because the farm runs every day, so an empty card is normal.
- **No app login.** A person without a login cannot clock in, so they are never checked.
- **Before the start date.** The SOP's `starts_on` is the first day checked. It is set to the day
  after go-live, so switching the check on does not raise a flood of old days. HR can change it
  on the Clock-in check card.
- **Closes itself.** A waiting violation whose day later becomes leave the person applied for, a
  holiday, or their weekly off **closes itself**. It then reads "Closed", with the reason, e.g.
  "Closed by itself: this day is the weekly off". A violation HR already **kept** is never
  touched.

**Which clock-in counts for a shift.** It is the first clock-in **inside the shift's window**:
from 6 hours before the start up to the end. It is matched by time, not by the day the punch was
dated, so a night shift's 00:30 punch is late for the shift that began the evening before. A
clock-in only after the shift ended counts as **no clock-in**.

## Who can act on themselves

The maintainer's answer was "allow it as today". Chandrakant is HR, a park head and a director
at once. He can:
- sign both lines of a Channapatna leave;
- decide his own leave;
- keep or close his own automatic violation;
- record a violation against himself.

Nothing blocks any of this.

## Totals: "how much each person has"

- The Violations page switches between **Month**, **Year** (the year of the chosen month) and
  **All time**. The list, the tiles and the per-person table all follow the switch.
- **Per person:**
  - **Violations** and **Fines**: recorded ones only.
  - **Waiting**: still waiting for HR.
  - **Closed**: closed by HR.
  - **Leave**: days approved, plus days applied for, clipped to the period.
  - Someone with leave and no violation is listed too.
- Violations and leave are **pre-aggregated separately** and joined on the person, so one never
  multiplies the other. A test pins this.
- The page tiles (Violations, Fines, People) **always** count **recorded** violations, whatever
  status tab is open; the tab narrows only the list. **Waiting for HR** is its own tile.
- Hand-recorded violations and enquiry penalties can **never** use the clock-in check's own types.
  Those types are left out of both forms' lists, and the server refuses them.

## Not done / next

- No push notification to HR when a violation starts waiting. HR sees it on the page.
- The check never looks further back than yesterday. A leave rejected days later does not
  retroactively raise a violation, and if the worker is down for more than a day, those days are
  never checked. Nothing alerts on that yet.
