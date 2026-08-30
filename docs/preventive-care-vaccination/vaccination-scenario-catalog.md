# One Goat — Vaccination Stories + Batching (Code Flow Map)

**Purpose:** Follow **one goat** from registration → due obligation → **batching into a drive** → execution.
Each story = a life path. Each chapter = what happens to **this goat** and **which code runs**.

Includes **planned vs built** so you can see what we aimed for and what the repo actually does today.

---

## Planned vs built (summary)

| Flow we designed | Built in code? | Where |
|------------------|----------------|-------|
| Per-goat due engine (kid / adult path) | **Yes** | `vaccination/app/generation.go`, `schedule_policy.go` |
| Approved kid schedule without mother-vaccination branching | **Yes** | `schedule_policy.go`, `generation.go` |
| Warming / sick / ICU / pregnancy defer | **Yes** | `schedule_policy.go`, `deferredReason` |
| Health recovery reopen + align to drive | **Yes** | `ReopenDeferredObligationByIdempotencyKey`, `recoveryRescheduleDue` |
| Cross-vaccine gap floors | **Yes** | `compatibility.go` (SM-1 + SM-7) |
| Trusted procurement holding-park / completion suppression | **Yes** | `hasTrustedCompletionEvidence`; trust means our park or our procurement holding park under SOP/video/physical validation, not outside-source claims |
| Booster / adult revacc chain | **Yes** | `booster.go` (SM-7) |
| Obligation scoped to **shed** | **Yes** | `generationScope` |
| **SM-4 shed batching** (rule + window + species) | **Yes** | `sweeper.go` → `sweepWindowGroupKey` |
| **Smart drive date** inside window | **Yes** | `pickBestDriveDate` |
| **Park consolidation** (singleton sheds merge) | **Yes** | `park_consolidation.go` |
| **Combo session** (FMD+HS, etc.) | **Partial** | Same visit date aligned; **separate batch per vaccine version** |
| **Combo align** across versions | **Yes** | `combo_align.go`, `AlignComboDrives` |
| Species split in shed (goat vs sheep) | **Yes** | `target_species` in group key + park merge |
| Max 3 vaccines per combo visit | **Yes** | `combo_session.go`, publish validation |
| Deferred goats **not batched** | **Yes** | SQL: `status IN ('scheduled','due','missed')` only |
| Stock FEFO reserve on batch | **Yes** | `finalizePlannedBatches` → `ReserveForBatch` |
| Stock blocked batch | **Yes** | `MarkBatchStockBlocked` |
| SOP task per batch | **Yes** | `CreateTaskForBatch` |
| Drive-time conflict graph / revalidate goat | **No** | Gaps enforced at **generation**, not at batch attach |
| Stock expiry in drive date scoring | **No** (by policy) | FEFO at reserve only |
| One merged multi-vaccine execution batch | **No** | One batch per protocol version per drive |
| Full published matrix in every tenant | **Ops/config** | Presets exist; must publish in `/config` |

---

## Full pipeline for one goat (two stages)

```
STAGE A — Per goat (SM-1 / SM-7)          STAGE B — Batching (SM-4)
────────────────────────────────          ─────────────────────────
goat.created / recheck / completed   →    obligation scheduled/due, batch_id NULL
  → classify path (kid / adult)             → sweeper runs (cron / obligation-sweeper)
  → compute due_at                          → group by shed + rule + window + species
  → defer OR schedule                       → pick drive date
  → scope = shed                            → CreateBatchWithObligations
                                            → finalize: SOP task + stock reserve
                                            → (optional) park merge pass
                                            → AlignComboDrives for combo sessions
                                            → Calendar projection
```

**One goat only enters a drive when:**
- Obligation status is `scheduled`, `due`, or `missed` (not `deferred`)
- `batch_id IS NULL`
- `due_at <= sweeper cutoff`
- Same compatible **park + rule + due window + species/planner group** as other goats in that drive chunk; shed remains roster detail.

---

## Batching stories (one goat in the drive layer)

These are **only about SM-4** — assume the goat already has an open obligation from Stage A.

### Batch story A — Normal park drive (enough goats)

**Goat:** Healthy. Due for PPR. Shed has 8 goats due same rule/window.

| Step | What happens to this goat | Code |
|------|---------------------------|------|
| 1 | Obligation `scheduled`, scope = **shed**, no batch | `genOneGoat` → `generationScope` |
| 2 | Sweeper groups compatible rows and keeps their shed detail | `sweepWindowGroupKey` + park drive scope |
| 3 | Drive date picked inside window | `pickBestDriveDate` |
| 4 | Batch created `status=planned`, session e.g. `rule:{ruleID}` or `combo:…` | `CreateBatchWithObligations` |
| 5 | SOP task + stock reserved | `finalizePlannedBatches` |
| 6 | Calendar shows one park drive with shed breakdown | Calendar canonical read |

**Built:** Yes.

---

### Batch story B — Singleton shed (only this goat in shed)

**Goat:** Only one due in Shed X; park consolidation on.

| Step | What happens | Code |
|------|--------------|------|
| 1 | Layer 1 sees 1-2 goats at/below `MinShedDriveTargets` (default 2) | `deferShedGroupToPark` → **skip** shed batch |
| 2 | Goat obligations stay unbatched | `batch_id NULL` |
| 3 | Layer 2 park pass: compatible same-park work exists inside the safe window | `consolidateParkDrives` |
| 4 | If the park candidate maximizes distinct safe animals, even from one shed → **park batch** | `scope_type=park`, session `park-consolidation:…` |
| 5 | If no merge possible → layer 3 fallback | `batchRemainingShedObligations` → **shed micro-drive** for orphan |

**Built:** Yes (integration tests in `park_consolidation_integration_test.go`).

---

### Batch story C — Deferred goat skipped

**Goat:** Due for ET+TT but `warming_hold` or `sick`.

| Step | What happens | Code |
|------|--------------|------|
| 1 | Obligation status **deferred** | SM-1 `genOneGoat` |
| 2 | Sweeper query excludes deferred | `status IN ('scheduled','due','missed')` |
| 3 | **Not in any batch** until reopened | — |
| 4 | After recovery → scheduled → next sweep picks up | Story 2 + Batch story A |

**Built:** Yes.

---

### Batch story D — Combo visit (FMD + HS)

**Goat:** FMD due week 12; HS due same window. **Two protocol versions** (one per vaccine).

| Step | What happens | Code |
|------|--------------|------|
| 1 | Two obligations, two version sweeps | `SweepVersion` per published version |
| 2 | FMD batch session `combo:FMD+HS` | `batchSession` → `vaccineComboSession` |
| 3 | HS batch session `combo:FMD+HS` | same session key |
| 4 | After both sweeps, align planned dates | `AlignComboDrives` within 7-day window |
| 5 | Operator may run **two SOP tasks** (one per version) on **same visit date** | separate batches, aligned date |

**Built:** Partial — same **date**, not one combined batch row.

---

### Batch story E — Sheep in goat shed

**Goat:** Sheep; 3 goats due PPR in mixed shed.

| Step | What happens | Code |
|------|--------------|------|
| 1 | `target_species` from `goats.species` | SQL join in `ListUnbatchedDueForVersion` |
| 2 | Group key includes species | `sweepWindowGroupKey` + `TargetSpecies` |
| 3 | Goats and sheep **never same batch** | separate groups |
| 4 | Park merge also splits by species | `parkID + "|" + TargetSpecies` |

**Built:** Yes.

---

### Batch story F — Stock blocked

**Goat:** In batch; no FEFO stock.

| Step | What happens | Code |
|------|--------------|------|
| 1 | Batch planned, obligations attached | SM-4 |
| 2 | Reserve fails | `reservePlannedBatchStock` error |
| 3 | Batch marked stock blocked | `MarkBatchStockBlocked` |
| 4 | Goat still on batch; drive blocked in UI | execution waits |

**Built:** Yes.

---

### Batch story G — Missed then batched

**Goat:** Window passed; sweeper marked **missed**.

| Step | What happens | Code |
|------|--------------|------|
| 1 | `MarkMissed` after grace | `sweeper.MarkMissed` |
| 2 | Status `missed` still unbatched | SQL includes `missed` in sweep |
| 3 | Can still batch for catch-up drive | SM-4 (if Preventive Care unblocks / regen) |

**Built:** Yes.

---

### Batch story H — Shifted shed before sweep

**Goat:** Due in Shed A; moved to Shed B.

| Step | What happens | Code |
|------|--------------|------|
| 1 | `goat.location.changed` | obligation shift handler |
| 2 | Open obligation **rescoped** to Shed B | `rescoped` event |
| 3 | Next sweep batches under Shed B | SM-4 uses new scope |

**Built:** Yes (`shift.go`).

---

## Life stories (Stage A + B woven in)

### Story 1 — Farm-born kid, normal (born in our shed)

**Animal:** Born here. DOB known. Approved kid schedule applies. Healthy. Shed has enough animals.

| Ch | Life event | Stage A (due) | Stage B (batch) |
|----|------------|---------------|-----------------|
| 1 | Registered | `goat.created` → SM-1 | — |
| 2 | Kid path | `schedulePathForGoat` → kid | — |
| 3 | ET+TT week 4 | obligation **scheduled**, shed scope | — |
| 4 | Due week 4 | status **due** | **Batch story A** — park drive group with shed/tag breakdown |
| 5 | Vaccinated | verify accept → SM-7 booster | batch **completed** |
| 6 | PPR → Goat Pox | cross-gap may delay pox | separate drives per vaccine version |
| 7 | FMD + HS | two obligations | **Batch story D** combo align |
| 8 | Adult revacc | SM-7 repeat | SM-4 each cycle |

---

### Story 2 — Sick between doses

| Ch | Life event | Stage A | Stage B |
|----|------------|---------|---------|
| 1 | Sick | `deferred` `"sick"` | **Batch story C** — skipped |
| 2 | Recovers | reopen + recovery align | next sweep → **A** or micro-drive **B** |

---

### Story 3 — ICU shed move

| Ch | Life event | Stage A | Stage B |
|----|------------|---------|---------|
| 1 | ICU location | `location_icu` defer | **C** |
| 2 | Leave ICU | location recheck reopen | **A** / **B** |

---

### Story 4 — No DOB

| Ch | Life event | Stage A | Stage B |
|----|------------|---------|---------|
| 1 | No DOB | `missing_dob` deferred | **C** — never batched |
| 2 | DOB fixed | real schedule | then **A** |

---

### Story 5 — Pregnant months 4–5

| Ch | Life event | Stage A | Stage B |
|----|------------|---------|---------|
| 1 | Late pregnancy | `late_pregnancy_hold` | **C** |
| 2 | After kidding | catch-up window | **A** for reopened doses |

---

### Story 6 — Procured young kid

| Ch | Life event | Stage A | Stage B |
|----|------------|---------|---------|
| 1 | Arrival | warming `warming_hold` | **C** |
| 2 | Day 8+ | kid path obligations | **A** |
| 3 | Source ET+TT trusted | suppressed duplicate | only next dose batches |

---

### Story 7 — Procured adult

| Ch | Life event | Stage A | Stage B |
|----|------------|---------|---------|
| 1 | Warming then adult path | `post_arrival` rules | **C** then **A** |
| 2 | Day 0 ET+TT+PPR | two versions or rows | combo session align **D** |
| 3 | Week 4 pox + booster | | **A** or **D** |

---

### Story 8 — Missing entry date

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | `missing_entry_date` | **C** |
| 2 | Entry fixed → schedule | **A** |

---

### Story 9 — Vaccinated at source

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | Trusted procurement holding-park evidence suppresses day-0 | no duplicate batch |
| 2 | Week-4 wave due | **A** |

---

### Story 10 — Sheep kid

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | Sheep matrix rows | — |
| 2 | PPR + Blue Tongue combo | **D** + **E** species split |

---

### Story 11 — Late / missed dose

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | `catch_up_preventive_care_approval` defer | **C** until Preventive Care path |
| 2 | Missed materialized | `MarkMissed` | **G** |

---

### Story 12 — Cross-vaccine gap (PPR then Pox too soon)

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | Pox due pushed +28d from PPR | later `due_at` |
| 2 | | pox drive weeks later **A** |

---

### Story 13 — Shed shift

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | Recheck may cancel if ineligible | — |
| 2 | Obligation rescoped | **H** |

---

### Story 14 — Sold / died

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | No new generation | open/deferred **canceled** |
| 2 | | removed from future sweeps |

---

### Story 15 — Manual Preventive Care campaign

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | `manual_campaign` rows only | campaign obligations |
| 2 | | **A** when due |

---

### Story 16 — New protocol published

| Ch | Stage A | Stage B |
|----|---------|---------|
| 1 | Cohort backfill | new obligations |
| 2 | Old version canceled | old batches unaffected (completed history) |

---

### Story 17 — One dose end-to-end (full chain)

| # | Step | Code |
|---|------|------|
| 1 | SM-1 creates obligation | `genOneGoat` |
| 2 | Sweeper batches | `SweepVersion` → **Batch A** |
| 3 | SOP task created | `finalizePlannedBatches` |
| 4 | Stock reserved | FEFO |
| 5 | Operator records dose | completion API |
| 6 | Preventive Care verifies accept | SM-5 |
| 7 | `vaccination.completed` event | outbox |
| 8 | SM-7 next dose | `ScheduleNextDose` |
| 9 | Calendar updated | `RefreshVaccinationProjection` |

---

## Master checklist (one goat — generation + batching)

```
STAGE A — Generation
  [ ] goat.created (SM-1)
  [ ] kid path / adult procurement path
  [ ] approved kid schedule only; no mother-vaccination category
  [ ] warming_hold / sick / ICU / pregnancy defer
  [ ] missing_dob / missing_entry_date
  [ ] trusted history suppress
  [ ] cross-vaccine gap floor
  [ ] health recovery reopen + align
  [ ] vaccination.completed → SM-7

STAGE B — Batching (SM-4)
  [ ] deferred NOT in sweep query
  [ ] shed batch (≥ MinShedDriveTargets)
  [ ] singleton deferred to park merge
  [ ] park consolidation batch
  [ ] orphan singleton fallback shed batch
  [ ] species split in group key
  [ ] pickBestDriveDate
  [ ] combo session on batch
  [ ] AlignComboDrives same visit date
  [ ] finalize SOP task
  [ ] stock reserve / stock blocked
  [ ] shift rescope before sweep
  [ ] MarkMissed then batch
  [ ] calendar projection after sweep

NOT BUILT / PARTIAL
  [ ] drive-time per-goat revalidation
  [ ] conflict graph at batch time
  [ ] single multi-vaccine merged batch
  [ ] stock expiry in drive scoring
```

---

## Story index

| Situation | Stories |
|-----------|---------|
| Normal kid life + drives | **1**, **17**, Batch **A** |
| Sick / ICU | **2**, **3**, Batch **C** |
| Missing data | **4**, **8** |
| Pregnancy | **5** |
| Procurement | **6**, **7**, **9** |
| Sheep | **10**, Batch **E** |
| Late / missed | **11**, Batch **G** |
| Gap between vaccines | **12** |
| Shed move | **13**, Batch **H** |
| Exit herd | **14** |
| Campaign / publish | **15**, **16** |
| Only one goat in shed | Batch **B** |
| FMD+HS same visit | Batch **D** |
| No stock | Batch **F** |

---

*Code map: `vaccination/app/generation.go` (SM-1), `booster.go` (SM-7), `schedule_policy.go`, `compatibility.go`, `obligation/app/sweeper.go` (SM-4), `park_consolidation.go`, `drive_planner.go`, `combo_align.go`, `cmd/obligation-sweeper/main.go`.*
