# Procurement journey: every view, web and phone

Rules every view obeys: backend owns titles, chips, copy, disabled reasons and the step list; the
client owns layout. Every date renders `DD/MM/YYYY`; every pen renders its full operational name
(`Castro 2`, `Mandela 1 - Part 3`) from `operational_location_display`; the word on screen is pen,
never shed; no raw code (`per_kg_live`, `approval_pending`) reaches a label. Every web page is
proven at laptop 1440 and phone 390; every phone screen is Room-first, refresh-on-open, with the
shared `SyncIconButton`.

## Admin-web

Sidebar group **Procurement** (icon truck) becomes, in order: **Requests**, **Journeys**, Vendors,
Feed Purchases, Animal purchases (renamed **Selection review**), Procurement SOP. Source Entry is
removed when stages 4–8 ship (decision 9). Each leaf is a `ModulePages` row so `/people` can tick
it; a new leaf without a row fails `TestEveryNavLeafIsATickablePage`.

### W1 `/procurement/requests` — Requests

- **Header:** title, "New request" button (CXO only; otherwise disabled with the backend reason).
- **KPI strip (4):** Open requests · Animals asked this month · Animals received this month ·
  Average days ask → arrival (90d).
- **Filter bar:** status chips (Open / Sourcing / Fulfilled / Closed short / All), species, purpose,
  park, needed-by range (`ThemedDatePicker`).
- **Table** (keyset, 25): # · Raised · Species & sex · Purpose · Qty · Weight band · Price band ·
  Park · Needed by · Fulfilled (n of qty, bar) · Status chip · Journeys (count). Row click opens
  the **request drawer** (local overlay, no navigation).
- **Request drawer:** facts; the quote sheet (table: vendor, count, rate, avg kg, available,
  shortlisted tick, notes, voice note play) with "Add quote" inline row; "Fix vendor" button
  (Procurement Director) opening the **fix form** inside the drawer: chosen quote, agreed count +
  buffer, rate, weight band, planned dispatch date, profile durations (prefilled, range-validated),
  milestone list (editable rows: label, %, due after stage), NDA/terms checklist (authored).
  Confirm = one server action that returns the journey row; the drawer flips to "Journey J-27
  opened" with a link. "Close short" (CXO) asks for a reason.
- **New request:** the authored request form rendered by the shared entry-form renderer, in a
  drawer.
- **Phone width:** KPIs 2×2, table collapses to cards (#, species/qty, needed by, status), drawer
  full-screen.

### W2 `/procurement/journeys` — Journeys (the board)

- **Header:** title; view toggle **Board | List** (client state, URL replaced locally).
- **KPI strip:** In sourcing · At vendor (verifying + selecting + approval pending) · Warming up ·
  In transit · Arrived this month · Outstanding to vendors (₹).
- **Board view:** one column per status group (Sourced → At vendor → Warm-up → Ready → Transit →
  Arrived), each card: J-no, vendor, species × accepted/agreed, next due step with owner and
  due (red when late), park, money chip (paid/agreed). Columns scroll inside their own
  `overflow-x: auto` rail on phone.
- **List view:** table: J-no · Request # · Vendor · Species · Agreed → Accepted → Loaded → Arrived
  (four small numbers) · Stage · Next step · Due · Riding AM · Transit manager · Paid / Agreed · Park.
- Row/card click navigates to W3 (it is a real detail page, not a drawer: it has tabs).

### W3 `/procurement/journeys/[journey_id]` — Journey detail

- **Header:** "J-27 · Kumar Farms · 72 sheep · CBE", status chip, stage progress rail (ten
  stages, done/current/pending, the current one's next step under it).
- **Tabs:** Overview · Animals · Steps · Money · Transport · Timeline.
- **Overview:** the **funnel** (asked 75 → brought 83 → removed on sight 4 → removed at weighing 5 →
  rejected by health 2 → accepted 72 → loaded 72 → arrived 71 → lost 1) as a horizontal bar
  funnel with counts, each bar labelled; profile snapshot card (durations, medicines, feed); dates
  card (fixed, approved, dispatch planned, departed, arrived; countdown to dispatch); people card
  (director, manager, health director, riding AM, transit manager, park head); vendor card.
- **Animals:** table (keyset): temp tag · verified weight · inspection verdict · health decision ·
  RFID · outcome (accepted / removed at … / lost in …) · pen. Filter chips by outcome. Row click
  opens the existing candidate drawer (answers + media lightbox).
- **Steps:** every stage as a collapsible section listing its steps (title, owner label, due,
  status chip, proof thumbnails that open on click, answer). Read-only on web except the office's
  own steps (**load approval**, route plan, pick the riding AM and the transit manager, review transit checks, payment milestones), which act in
  place via the step's own control. The load approval control opens the **approval drawer**: the
  funnel again, money (accepted × rate × avg kg = agreed value; estimated landed from profile),
  pen picker (partition catalog of the destination park), "Approve" / "Reject with reason" (two
  buttons where the action is, never `window.confirm`).
- **Money:** agreed value, milestones table (label, %, amount, due, status, paid on), ledger
  table with "Record payment" (amount, date, mode, reference, receipt upload), outstanding.
  Landed-cost lines (read-only here; edited where they are today).
- **Transport:** vehicle card (transporter, number, driver, charge), truck checklist with photos,
  route plan text, transit checks timeline (each check: time, video, condition chip), stops.
- **Timeline:** every stage completion, decision, push and payment in order with who and when.
- **Phone width:** tabs scroll horizontally in their own rail; the funnel stacks vertically;
  tables become cards.

### W4 `/procurement/animal-purchases` → **Selection review** (existing page, re-owned)

Offered to the Health Director (and CEO). Adds a journey column and a "J-27" filter; the decision
form is unchanged. The header tiles gain "awaiting your decision".

### W5 `/procurement/sops` (existing)

Lists the ten journey documents beside the inspection, supplier, feed and toxin documents. Each
opens the List | Flow editor with the G9 additions. The Flow view of a stage ends in a locked
"then opens: Warm-up at source, Transport preparation" footer.

### W6 Configuration › Items & settings › **Procurement journey profiles**

Register table: purpose · label · source warm-up · park warm-up · journey feed days · transit
check · milestones (summary "10/20/60/10") · version · status. Row drawer edits every key in
`configuration.md` with the register pickers (vaccine, medicine, feed item, stage by sex) and the
milestone rows; Save draft / Publish; a published row is read-only with "Edit as new draft".

### W7 Work Board — Procurement lane (G8) and Alerts

One row per live journey; the Alerts page gains the three lateness rules (transit check missed,
milestone overdue, transport prep late) under the HRMS-gated rule config the Alerts page already
has.

## Phone (Android, module `vendors` → renamed **Procurement**)

Bottom bar (module registry, backend-composed): **Requests** · **Journeys** · Vendors · Feed
Purchases. Selection review stays on the web (it is a desk screen). The riding AM, who has no
procurement permission, sees the journey under **Tasks › For me** (the generic "work the system
owes this person" tab, decision 2026-09-14) as a card type "Transit · J-27", and that card opens the
same Journey steps screen limited to their steps. The transit manager sees their monitoring steps
there too when they hold no procurement module, and under Journeys when they do.

### P1 Requests list

Cards: "#12 · 75 male sheep · 15–17 kg · ₹380–400/kg" / "CBE · needed by 08/10/2026" / status
chip / fulfilled bar. "+" in the list header (CXO only). Filter bar (status / purpose) in the
shared worklist filter bar. Tap → P2.

### P2 Request detail

Facts card; **Quote sheet** list (vendor, count, rate, avg kg, available, shortlisted tick) with
"Add quote" (form: vendor search of the register with "new vendor" inline, count, rate + unit,
avg kg, available from, warm-up at vendor yes/no, notes, voice note record); **Steps** card (the
request's two tracks, the shared `PurchaseStepsCard`); "Fix vendor" (Procurement Director) → P3.

### P3 Fix vendor

One scrolling form, same fields as the web fix form, profile values prefilled, milestones as
editable rows, the NDA/terms authored checklist (yes/no + attach). Confirm. Offline: queued on the
outbox as `procurement.fix_vendor`; the card shows "Waiting for network" until the journey id
returns.

### P4 Journeys list

Cards: "J-27 · Kumar Farms" / "72 of 75 · sheep · CBE" / stage chip / next step + due (red when
late) / money chip. Filter bar: stage, park, mine. Tap → P5.

### P5 Journey detail

Header (J-no, vendor, stage rail compressed to a progress line with the stage name), funnel as a
compact row of numbers, then the **Steps** card: stages as sections, the current stage expanded,
each step the shared workflow step row (owner label, due, blocked note, branch note, proof
counters). Tabs under the header: Steps · Animals · Money · Transport (horizontally scrollable
chips, not a second nav level). Deep-link steps open P6–P11.

### P6 Stock weighing (free-flow)

Top: "Weighed 41 · removed 5". Row entry: temp tag field (keyboard or temporary RFID reader
scan), weight kg, optional photo; "Add" appends to a list below (newest on top, keyset 20 with
infinite scroll). A duplicate temp tag in this journey is refused in place ("Already weighed").
"Done weighing" completes the step (guarded on ≥ 1 row). Nothing here reads `goats`.

### P7 Mark removed

Pick list of this journey's live candidates (temp tag, weight) with a reason chip per picked
animal (register-driven) and a note; confirm. Used at weighing, warm-up, loading, transit, arrival
with the stage stamped by the step that opened it.

### P8 Inspect animals (existing inspection flow)

The existing `AnimalPurchaseAnimalCreateScreen`, opened per weighed candidate from a list that
shows "inspected ✓ / not yet / removed"; the weight field is prefilled from P6 and locked.

### P9 Load approval (phone, Procurement Director / CXO)

The funnel as stacked rows, money card, pen picker (partition catalog), Approve / Reject with
reason. Same write as the web drawer.

### P10 Tagging (first warm-up day, at the vendor)

List of accepted candidates (temp tag, weight, sex); tap a row → scan RFID (the existing
scanner surface) → row shows the RFID; "Confirm tagging" when every accepted row has one (or the
missing ones are marked removed via P7). Confirm writes the goats rows server-side in one
transaction and completes the step.

### P11 Riding AM screen (with the load)

Reached from Tasks › For me or P5. Header: "J-27 · departed 05:40 · 6h 20m on the road". Next
check card at the top with a countdown from server time and a "Record check" button (video +
condition chips); list of done checks with their condition; stops list; "Animal down?" shortcut
to P7 (reason in_transit); "Reached the park" (the arrival step: video). The transit manager's
name and number sit under the header as selectable text. Every due time is the server's; the
screen never computes a gate from the phone clock (the toxin rule).

### P14 Transit manager screen (at the park or main office)

The same journey from the office side, on the phone and on W3's Transport tab. Header: "J-27 ·
AM: Suresh · last check 08:40 · next due 11:40". A review card for each check as it lands (the
AM's video, the condition chip, "All fine / Called the AM / Escalated"); an overdue card the
moment a check passes its grace ("Check 3 overdue by 12 min — contact the AM", with the AM's
number); a distress card when the AM reports it (continue / stop and rest / divert to a vet); the
handover confirmation after the Park Head records arrival. The transit manager records nothing
about the animals; every fact comes from the AM.

### P12 Arrival (Park Head)

The arrival stage's steps on P5: unload video, arrived count (number), reconcile note if the
numbers disagree ("Loaded 72, arrived 71 — record the missing animal" → P7), place in pen
(photo), condition question, park warm-up day cards, PC handoff (engine).

### P13 Money (phone)

Milestones with status, ledger list, "Record payment" (amount, date, mode, reference, receipt
photo) for `procurement.journey.pay` holders.

## Pushes the screens answer to

| key | to | opens |
|---|---|---|
| `procurement.request_raised` | Procurement Director + Manager | P2 |
| `procurement.journey_opened` | CXO | W3 / P5 |
| `procurement.load_approval_pending` | Procurement Director + CXO | P9 |
| `procurement.load_approved` | Park Head (destination), Procurement Manager | P5 |
| `procurement.dispatch_tomorrow` | riding AM, transit manager, Park Head | P11 / P14 |
| `procurement.transit_check_missed` | transit manager; Procurement Director if the chase step is itself late | P14 |
| `procurement.transit_distress` | transit manager | P14 |
| `procurement.arrived` | Park Head, Health Director | P12 / W4 |
| `procurement.milestone_due` / `_overdue` | Procurement Director | P13 |
| `procurement.journey_closed` | CXO | W3 |

Every message carries the vendor, the journey number, the species and count, the park and the
date in IST (the meaningful-notification rule); each is a catalog row switchable per designation.
