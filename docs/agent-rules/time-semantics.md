# Time Semantics (India Business Calendar)

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

- Treat Goat OS time semantics as India-business-calendar semantics. Physical
  storage may use `timestamptz`/absolute instants, but every business meaning
  derived from those instants — scheduling, due/missed buckets, reminder keys,
  reporting groups, audit-log display, and UI labels — must convert to
  `Asia/Kolkata` first. UTC must never define a Goat OS business day.
- **VACCINATION TIME GRAIN IS THE BUSINESS DAY — NEVER HOURS (maintainer rule,
  Claude AND Codex).** A vaccination drive is a business DAY in `Asia/Kolkata`.
  It is not an instant, not a timestamp, and never "now ± N hours". This is a
  business rule about how the farm actually works, not a test-hygiene
  preference: operators work a day, a drive is planned for a day,
  `planned_date` is a `DATE`, and two vaccination facts on the same business day
  are the same day no matter how many hours separate their timestamps.
  - Never place a drive, due value, safe window, or query window with hour or
    minute arithmetic. No `time.Now().Add(-2 * time.Hour)`, no
    `dueAt.Add(-1 * time.Hour)`, no `now±N` clock instants — in production code,
    fixtures, or assertions.
  - Anchor to `biztime.BusinessDayStart(...)` / `biztime.BusinessDate(...)`, or
    to a fixed business date. Compare business DATES, not instants.
  - Never widen an hour tolerance to make a same-day comparison pass. If a
    same-day check fails because two timestamps differ by hours, the DAY is the
    correct unit and whichever side compares instants is the defect.
  - Why this is a hard rule: hour-anchored fixtures shipped a defect class where
    15 calendar tests passed or failed depending on the time of day they ran — a
    batched park drive anchors at 00:00 IST, the fixtures asked for `dueAt - 1h`,
    and that fell outside the window except during a ~1-hour slice of each day.
    Sub-day precision on a vaccination date is always a bug in the making.
  - If a specific case genuinely needs sub-day precision, it needs a recorded
    maintainer decision first. Ambiguity is not approval.
- Pinned-clock tests must derive time-sensitive fixture fields such as
  `valid_from`, `valid_to`, due instants, and recipient eligibility from the
  same pinned anchor. Never mix a pinned application clock with SQL `now()` or
  a second `time.Now()` when the fixture is evaluated against that anchor.
- For dashboards or reports that slice data by month, date, breed, farm, shed,
  load, category, status, gender, operator, source, or similar dimensions, use
  the canonical rule in `docs/decisions/high-scale-dashboard-projections.md`
  before coding.
