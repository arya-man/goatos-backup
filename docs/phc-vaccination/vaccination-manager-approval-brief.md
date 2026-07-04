# PHC Vaccination Scheduling — Manager Approval Brief

**Audience:** PHC / operations leadership  
**Purpose:** Explain how Goat OS handles vaccination **for each animal**, how **shed drives** are formed, and what we are asking you to approve before wider rollout.  
**Status:** For review and sign-off — staging validation recommended after approval.

---

## 1. What we are asking you to approve

We are **not** asking you to approve “vaccinate everyone in the shed on one day.”

We are asking you to approve this operating model:

1. **Each goat or sheep gets its own due doses** based on species, age, how it arrived on the farm, health state, and pregnancy — following the published vaccination rules in Config.
2. **Shed drives are created only from compatible, due work** — goats that are medically allowed and operationally eligible on that day.
3. **Deferrals are visible** — warming, sick, ICU, quarantine, late pregnancy, and missing data show as held work with a reason, not silent skips.

If this matches how PHC expects the farm to run, we can proceed to staging walkthroughs with real shed examples.

---

## 2. How vaccination works in Goat OS (two steps)

```
STEP 1 — Per animal          STEP 2 — Shed drive (batching)
"What does THIS animal       "Which due animals can safely
 need, and when?"             be done together in this shed?"
        │                                │
        └──────── must finish ──────────┘
                  before
```

**Step 1** runs when a goat is registered, when rules are published, when health or shed changes, or when a previous dose is completed.

**Step 2** runs on a schedule (sweeper job): it groups **open, due** obligations into shed or park drives, reserves stock, and opens SOP tasks for operators.

---

## 3. End-to-end flow — from registration to completed dose

This is the normal path **one goat** follows when everything is straightforward.

| When | What the system does | What PHC / ops sees |
|------|----------------------|---------------------|
| **Goat registered** | System classifies the animal (species, born here vs procured, age) and creates first due doses from published rules. | Passport / adherence shows upcoming doses. |
| **Due date approaches** | Obligation becomes due inside its medical window. | Calendar and Action Center show due work. |
| **Sweeper runs** | Due goats in the **same shed**, same vaccine, same window (and same species) are grouped into one **planned drive**. Stock is reserved (FEFO). SOP task is created. | Shed vaccination task appears for execution. |
| **Drive day** | Operator vaccinates goats on the list, records dose and proof. | Execution screen / SOP in progress. |
| **PHC verifies** | Accepted dose closes that obligation. | Verification queue clears the row. |
| **After acceptance** | System schedules the **next dose** in the series (e.g. booster 3 weeks later, or next vaccine in order). | Next due appears on Passport and Calendar. |
| **Cycle repeats** | Next due → sweep → drive → record → verify → next dose. | Continuous schedule through kid series and adult revaccination. |

**Important:** A goat that is **on hold** (sick, warming, ICU, etc.) does **not** appear on a drive until the hold clears and the obligation is reopened.

---

## 4. How we classify each animal (drives which schedule applies)

Before any dose is calculated, the system places the animal on one path:

| Animal situation | Schedule used |
|------------------|---------------|
| **Born on our farm** (kid) | Full kid age schedule from date of birth (week 4 ET+TT, week 7 booster, week 16 PPR, etc. — mother-vaccinated timings). |
| **Brought in young** (≤16 weeks) | Same full kid schedule (not shortened). |
| **Brought in as adult** | Shorter **procurement intake** schedule: ET+TT + PPR at arrival (after warming), pox + booster at 4 weeks, then FMD/HS on repeat cycles. |
| **Sheep** | Sheep vaccine set (includes Blue Tongue; combos differ from goats). |
| **Mother unknown** | Treated as **mother vaccinated** (later kid timings) — per current policy. |

Adults do **not** replay the full kid week-by-week table. Kids do **not** use the adult shortcut unless they are past the kid cutoff and classified as adult procurement.

---

## 5. What happens in each case (manager view)

Below is **how the system behaves** — not medical advice. Medical rules come from the published Config matrix and source SOPs.

### 5.1 Farm-born kid — normal

- Doses are generated in order: ET+TT → booster (3 weeks) → PPR → Goat Pox (goats) or sheep equivalents → FMD + HS when due.
- Live vaccines are spaced (e.g. PPR and pox not pushed together if the 4-week live-live gap is not met).
- FMD and HS can be planned for the **same visit** when both are due (combo alignment).
- When the kid series is complete, **adult repeat doses** (6 months, 9 months, 1 year, 3 years per vaccine) follow from each accepted dose.

### 5.2 Kid or adult — first 7 days after arrival (warming)

- **No vaccination** is scheduled or executed during warming.
- Work appears as **deferred** with reason warming hold.
- After day 7, normal scheduling resumes from the correct path (kid or adult).

### 5.3 Animal is sick, in ICU, or in quarantine

- Open vaccination work is **held** (deferred) — not batched into a drive.
- Calendar and adherence show the hold with reason (sick, ICU, quarantine, etc.).
- When the animal recovers and location/health is updated:
  - Work is **reopened**.
  - If a shed drive is planned within about **7 days**, the goat can align to that drive; otherwise it gets a **small / urgent drive** so it is not left behind.

### 5.4 Pregnancy

- **Months 1–3:** vaccination can proceed if otherwise eligible.
- **Months 4–5:** doses in that window are **held** (late pregnancy).
- **After delivery:** missed doses from the hold can be caught up within the **2-week post-delivery window** configured in rules.

If breeding date is missing, pregnancy month may show as **needs review** until data is complete.

### 5.5 Procured adult — vaccinated at source before arrival

- Trusted vaccination records from procurement can **prevent duplicate** doses already given at source.
- **Warming still applies:** no farm dose in the first 7 days after arrival even if source vaccinated recently.
- Next steps in the intake schedule (e.g. week-4 pox + booster) are generated from what is still missing.

### 5.6 Procured animal — vaccination history unknown or not trusted

- System does **not** assume off-farm doses without trusted evidence.
- Intake schedule applies; PHC may need to review catch-up items flagged for approval.

### 5.7 Late or missed dose

- If a dose passes its due window, behavior depends on rule policy (typically **PHC approval** before catch-up).
- Very old missed primaries do not flood the animal with every past dose at once — one controlled catch-up path is used.

### 5.8 Goat moved to another shed

- Open vaccination work **moves with the goat** to the new shed.
- The next drive is planned under the **new shed** (or park consolidation rules below).

### 5.9 Animal sold, died, or left the herd

- No new doses are generated.
- Open work is canceled as appropriate; completed history remains for audit.

### 5.10 Manual PHC campaign

- Separate from day-to-day scheduling: an approved **campaign** can trigger extra rows for a defined cohort without changing the base matrix for everyone.

---

## 6. How shed drives are formed (batching — what ops will see)

### 6.1 What goes into a drive

A drive includes goats that are:

- **Due** (or scheduled inside the active window),
- **Not on hold** (not deferred),
- In the **same shed** (or merged park drive — see below),
- Needing the **same vaccine dose** (same rule),
- In the **same species** (goats and sheep are **not** mixed in one drive),
- Medically compatible for that visit (cross-vaccine gaps already applied when dues were calculated).

We do **not** batch “everyone in the shed regardless of vaccine or species.”

### 6.2 Small sheds

- If a shed has only **one** goat due and park consolidation is on, the system may **wait** and merge with another small shed in the **same park** (same species) into one park-level drive.
- If no merge is possible, a **small shed drive** still runs so the animal is not skipped.

### 6.3 Combo visits (same day, more than one vaccine)

| Combo (examples) | Manager expectation |
|------------------|---------------------|
| FMD + HS | Same visit date when both are due. |
| PPR + Blue Tongue (sheep) | Same visit when configured. |
| ET+TT + PPR (adult day 0) | Two products, one visit — counts as **two vaccines**, within the max **2 vaccines per combo visit** rule. |

**Note:** Today the system aligns **visit dates** for combo partners; execution may still show **one task per vaccine product** (not one combined task for two different products). Operators treat it as one shed visit.

### 6.4 Stock and tasks

- When a drive is planned, the system **reserves vaccine stock** (FEFO) for the batch.
- If stock is insufficient, the drive can show as **stock blocked** until resolved.
- An **SOP vaccination task** is created for the shed (or park) scope.

### 6.5 Calendar and escalations

- After sweeping, vaccination work is projected to **Calendar**.
- Reminders and escalations can fire for overdue drives per configured SLA.

---

## 7. Same-day rules the system enforces (summary)

| Rule | System behavior |
|------|-----------------|
| Warming (7 days after arrival) | Hold all vaccination |
| ICU / quarantine | Hold; no drive |
| Sick / under treatment | Hold; no drive |
| Pregnancy months 4–5 | Hold; catch up after delivery window |
| Live → live gap | At least 4 weeks between live vaccines (dues adjusted) |
| Live → killed / killed → killed | At least 2 weeks where applicable |
| ET+TT kid booster | 3 weeks after primary |
| Max vaccines per combo visit | **2** — not 3 on one visit |
| Mixed goat + sheep in shed | **Separate drives** by species |

---

## 8. What the system does **not** do today (set expectations)

Please confirm these are acceptable for this release:

| Item | Current state |
|------|----------------|
| Blind “whole shed vaccination day” | **Not supported** — by design |
| Mother-not-vaccinated early kid schedule | **Not supported** — we assume mother vaccinated when unknown |
| One single task merging two different vaccine products | **Partial** — same visit date, separate product tasks |
| Re-check every goat’s health at the moment of batching | **Not supported** — holds must be updated in identity/health before sweep |
| Pick drive date based on vial expiry | **Not supported** — expiry handled at stock reserve (FEFO) |
| Full vaccine matrix live in every environment | **Requires Config publish** — presets exist; ops must publish rules |

---

## 9. What we need from you to approve

Please confirm **yes / no / comment** on each:

| # | Question | Default in system |
|---|----------|-------------------|
| 1 | Per-animal scheduling before shed drives | Yes — core model |
| 2 | Mother unknown → use mother-vaccinated (later) kid timings | Yes |
| 3 | Max **2** vaccines on one combo visit | Yes |
| 4 | Goats and sheep in same shed → **separate drives** | Yes |
| 5 | Warming 7 days — no vaccination | Yes |
| 6 | Sick / ICU / quarantine — hold, visible in adherence | Yes |
| 7 | Pregnancy months 4–5 hold + 2-week post-delivery catch-up | Yes |
| 8 | Procured adult intake schedule (not full kid table) | Yes |
| 9 | Trusted source doses suppress duplicates; warming still applies | Yes |
| 10 | Small sheds may merge at park level before a lone goat is driven | Yes |
| 11 | Acceptable that combo = same **date**, separate product tasks | Needs confirmation |
| 12 | Acceptable that drive date does **not** use expiry scoring | Needs confirmation |

**Sign-off:**

| Role | Name | Date | Approved (Y/N) | Comments |
|------|------|------|----------------|----------|
| PHC / Veterinary lead | | | | |
| Operations lead | | | | |
| Product / COO | | | | |

---

## 10. Recommended validation after approval (staging)

We suggest walking through **one real example** for each row — not code testing:

1. Farm-born kid — full series through first FMD+HS combo visit.  
2. Procured adult — warming, then day-0 and week-4 waves.  
3. One goat sick between doses — hold, recover, join next drive.  
4. One singleton shed — park merge or small drive.  
5. Mixed goat + sheep shed — confirm two separate drives.  
6. One late dose — PHC approval / catch-up visible.

---

## 11. One-page flow (for sharing)

```
Register goat
    → System assigns schedule path (kid / adult / sheep)
    → Creates due doses (or holds if warming / sick / missing DOB)

Due date reached
    → Sweeper groups eligible goats in shed (same vaccine, species, window)
    → Plans drive date, reserves stock, opens SOP task

Drive executed
    → Operator records dose + proof

PHC verifies
    → Dose accepted → next dose scheduled automatically
    → Cycle continues until series + adult revaccination
```

---

*Supporting detail for engineering and test authoring: `vaccination-scenario-catalog.md` (internal).  
Medical source matrix: `docs/preventive-care-vaccination/source-nuances-rules.md`.*
