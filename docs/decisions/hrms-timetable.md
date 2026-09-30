# HRMS group and the Timetable page

Maintainer request 2026-09-30, with answers given the same day.

## What changed

1. **HRMS is its own sidebar group**, seated directly above Others. It used to be one leaf,
   People / HRMS, under Others.
2. **Each view is its own page.** The /people tab strip is retired. The pages are:
   - People — `/people` (the directory and Add person)
   - Clock in / out — `/people/clock`
   - Timetable — `/people/timetable` (new)
   - Leave — `/leave` (unchanged; moved into the group)
   - Notifications — `/people/notifications`
   - Vaccination operators — `/people/vaccination`

   An old `/people?tab=…` link redirects to its page and keeps its other parameters. The four
   "coming soon" placeholder tabs went away with the strip.
3. **Timetable.** Pick a park and see that park's shifts with their working hours. Below them is
   everyone whose home park it is, with the shift each person works.

## The timetable model (migration 000457)

| Fact | Where | Why |
|---|---|---|
| Which shifts exist (Morning, General, Second) | `workforce_shift_catalog` | One list for the farm. Adding a fourth shift needs a new row, not a deploy. |
| When a shift runs at a park | `workforce_park_shift_timings` (park × shift) | Hours differ by park. Morning starts at 06:00 at CBE and 07:00 at CPT (elephant risk in the early morning). |
| Who works which shift | `workforce_member_shifts` (one row per person, shift code only) | A person's hours come from their home park, so moving park needs no second edit. |

Times are stored as minutes after IST midnight:

- An end of 1440 means midnight: the Second shift runs 15:00–24:00.
- An end earlier than the start crosses midnight.
- An end may never equal the start.
- An end without a start is refused.

The Morning shift was given a start and no end, so its end is left unset. The page shows
"6:00 am – end not set" and offers Change time. It does not invent an end time.

A park added under Configuration appears in the park strip on its own. Every shift reads
"Not set" with a Set time button.

## Who may change it

- HR and the CEO/CXO hold `workforce.timetable.read` and `workforce.timetable.write`, through a
  web-only `timetable` capability module.
- Timetable is its own module, not a level on People, so that HR can edit shifts without being
  given the staff directory and its roster and device authority.
- **Park heads do not edit it yet.** They work from the phone and hold no web login. They get a
  phone editor as a follow-up (maintainer answer 2026-09-30).
- Both edit controls are compiled from the write permission, so a reader sees them disabled with
  a reason. Both PUT routes refuse a reader regardless.

## Rules the code keeps

- **Writes are fenced on `row_version`.**
  - An exact replay of the stored values succeeds and writes nothing.
  - A stale write carrying different values gets 409 `timetable_version_conflict`.
  - Each real change writes one `audit_log` row in the same transaction.
- **The page's counts cover the whole park.** Grain is person, and the buckets are disjoint.
  They cover the same active-member set the people list pages through, so paging never changes
  a count.
- **Every word on the page is backend copy** or a backend-composed field, for example
  `timing_label` ("8:30 am – 6:00 pm").
- A save returns the saved row, and the page puts it in place. There is no page revalidate.

## Follow-ups

- A phone editor for park heads, scoped to their own park.
- The Morning shift's end time, once HR or the maintainer states it. Enter it on the page; no
  code change is needed.
