# Procurement journey: delivery plan

Each slice is one PR, landed through `make land-main`, with its own migrations, seeds, tests,
guards and proof. A slice is not done until the proof column is real: a Chrome screenshot at
1440 and 390 after the final edit, a phone run on the Realme/tablet for phone slices, and a
Postgres E2E that drives the production path (opener → hook → next opener), never a seeded
readback. Slices 0 and 1 start only after the conflicts in `docs/decisions/procurement-journey.md`
are answered.

| # | slice | contents | tests and guards | proof |
|---|---|---|---|---|
| 0 | **Decision + seeds** | the decision doc accepted; ten SOP documents seeded (`sopseed/procurement_*.json` + migration); journey profile table + register page (W6); new task types; permissions + role map + `ModulePages` rows + per-person tick levels | `TestMigrationEmbedsTheProcurementJourneySeeds` (byte-for-byte), `TestTaskTypeRegistryCoversEveryHookAndAnswerKind`, `TestEveryNavLeafIsATickablePage`, `TestPageCatalogPermissionsMatchTheNavigationGate`, `make procurement-sop-guard` extended with the new hook rules, `make sop-driven-herd-operations-guard` (no step title in Go) | W6 at both widths; every document opens in List and Flow |
| 1 | **Engine G1 + G2 + G4 + G5** | chaining via `triggers.on_complete`, day/anchor schedules, `{vars}` substitution, owner list, `pick_person`, `owner_from_step`, completion guards, deep-link task types; SOP studio G9 for these | the pinned tests named in `engine-gaps.md`; `followup-model.test.mjs` round-trips every seeded document unchanged; `flow-layout.test.mjs` for the footer and pick icon | Studio at both widths (`responsive:guard` with the five SOP routes added to its catalogue) |
| 2 | **Stage 0–1: Requests + Sourcing** | `procurement_requests`, quotes, fix-vendor (creates the journey + the `animal_purchase_loads` row), W1, P1–P3, pushes `request_raised`, `journey_opened` | `TestRequestRaisedOpensBothTracks`, `TestFixVendorOpensJourneyAndCompletesTheHook` (idempotent replay, same-key different-payload refused), `TestVendorFixedRefusedWithoutShortlist` guard, `make idempotency-writes-guard`, `make domain-event-architecture-guard`, `make notification-specificity-guard`, `make fcm-recipient-routing-guard` | W1 drawer E2E in Chrome (add quote → shortlist → fix → journey link) at both widths; phone P1–P3 on device incl. offline fix |
| 3 | **Stage 2–3: Stock verification + Selection + Load approval** | candidate extensions, stock weights route, removals, re-owned decision (C1), approval write, W3 Overview/Animals/Steps, W4 re-own, P4–P9, pushes | `TestStockWeighingIsFreeFlowAndRefusesDuplicateTempTag`, `TestInspectStepGuardedUntilEveryWeighedAnimalIsInspectedOrRemoved`, `TestHealthDirectorDecidesAndCxoApprovesTheLoad` (operator refused, CEO floor), `TestLoadRejectionReopensSourcing`, `make operational-location-guard` (pen picker from the partition catalog, display composed), `make aggregate-projection-guard` with the funnel's `projection-review:` marker (grain = candidate, each count disjoint) | Chrome W3/W4 both widths; phone P6 with the temporary RFID reader; E2E: 83 weighed → 5 removed → 78 inspected → 2 rejected → 76 accepted → approve → stages 4/5 open |
| 4 | **Engine G3 + G6 + G8** | series from/until step, lateness sweep (`kernelstages`), Work Board source, Alerts rules | `TestSeriesStartsAtDepartureAndEndsAtArrival`, `TestLateStepNotifiesOnce`, workboard source test, `make scale-guard` (keyset sweep) | Work Board lane at both widths |
| 5 | **Stage 4–5: Warm-up + Transport prep** | daily series from profile, anchored prep steps, vehicle table + form, pick transit manager, Tasks › For me card type "Transit", W3 Transport tab, pushes | `TestWarmupSeriesLengthComesFromTheSnapshotNotTheLiveProfile` (publish a new profile mid-journey, series unchanged), `TestPrepStepsAnchorOnPlannedDispatch`, `TestTransitManagerIsAdmittedToTheJourneyWithoutAStandingGrant`, `TestPenVisitTabRendersATransitCard` | phone: AM without procurement perms sees and completes his step; Chrome Transport tab |
| 6 | **Stage 6–7: Loading + Transit** | tagging → goats rows (C2), departure, transit series + stops, losses, P10–P11, `transit_check_missed` | `TestTaggingCreatesGoatsInTheWarmupStageWithOriginOnTheJourney` (origin filter + Load wise read them), `TestDepartureMintsChecksEveryThreeHoursUntilArrival`, `TestLossInTransitClosesTheGoatThroughTheDeathPath`, `make goat-shed-scope-guard`, `make critical-animal-action-availability-guard`, Android `WorkflowOptimisticSequenceTest` for the series | phone P10 with real RFID scans; P11 with a server-time countdown; E2E across departure → 3 checks → one missed push → arrival cancels the rest |
| 7 | **Stage 8–9: Arrival + Payments + landed cost + PC handoff** | arrival reconcile, place in pen (location events), park warm-up series, ledger + milestones, cost lines at placement, `pc_handoff`, journey close → request fulfilled, W3 Money/Timeline, P12–P13 | `TestArrivalReconcileBlocksUntilLoadedEqualsArrivedPlusLosses`, `TestPlaceInPenMovesEveryGoatAndWritesLandedCostOnce` (replay writes nothing twice), `TestMilestoneAmountFollowsTheAgreedValueAtUnlock`, `TestPaymentLedgerCompletesTheMilestoneNeverATap`, `TestJourneyCloseFulfilsTheRequest`, the existing PC handoff → vaccination E2E re-pointed | E2E whole journey on Postgres (one test, `TestKernelStory_ProcurementJourney`, registered on the E2E report site); Chrome Money/Timeline both widths |
| 8 | **Retire** | `/procurement/source-entry` page + routes removed, `procurementDirectorStockOnly` lens deleted (C4), `procurement.animal_purchase_intake` retired into stages 3/8 (C3), docs/README/skills sync, `check-ia-guard` catalogue, leadership-assistant coverage rows | `TestRetiredProcurementDirectorLensIsReproducedByTicks` updated, `make leadership-assistant-coverage-guard`, `make assistant-route-closure-guard`, `tools/dev/audit-person-access.py` sweep | the Procurement Director's live sidebar before/after |
| 9 | **G7 per-step review** (optional, after the farm has run two journeys) | `review: per_step` flag, `procurement_evidence` category + audience row, sampling policy entry | the birth per-step tests re-run against the flag; `TestEveryVerificationModuleHasAnAudienceCatalogRow` | verifier queue with a transit clip |

## Cross-cutting, every slice

- **Root-cause rule:** every bug found during a slice gets a failing production-path test first.
- **Copy firewall:** no `per_kg_live`, `approval_pending`, `J-` composed on the client; the
  `TestBootstrapContractSaysPenNeverShed` walk covers every new contract; `make ui-vaccine-labels-guard`
  (the warm-up vaccine step title comes from the register label).
- **Dates:** `fmtDate` / `GoatOsDates` / `biztime.FarmDate` only; `make date-format-guard`.
- **Telemetry:** every new screen and route wired (`make telemetry-guard`), events named in
  `AnalyticsEvents`; the journey funnel (request → fixed → approved → departed → arrived) is a
  tracked journey.
- **Leadership assistant:** every new table and route gets a `ceo_ai.*` view or a coverage-matrix
  row in the same slice ("how many animals are in transit right now" is the obvious question).
- **Postgres tests** run on the OCI RAM Postgres (`:5442`) per the laptop rule; no Docker.
- **Phone proof** on the visible profile only; the throwaway DB on `15544` with the API on
  `8081`; the target-chain check before every run.

## Open questions (beyond the decision doc's C1–C9)

1. Does a journey ever split across two trucks? (Proposed v1: no; a second truck is a second
   journey on the same request.)
2. Who may record a payment on the phone: the director only, or also the manager? (Proposed:
   director; the manager sees the money tab read-only.)
3. Should the stock weighing accept the Weighing module's RFID reader hardware path? (Proposed:
   yes, the scanner surface is shared UI; the table is procurement's.)
4. Sheep vs goat warm-up stages: the register has `Fattening Male Warmup` etc. for kids; is there
   an adult sheep warm-up stage, or does the profile map to `Warmup Buck`?
5. Does the request need CXO approval of the fixed vendor before stock verification starts, or is
   the Procurement Director's fix final (the whiteboard says final call)? Proposed: final; the CXO
   is pushed.
