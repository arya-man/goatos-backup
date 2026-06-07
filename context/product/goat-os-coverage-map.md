# Goat OS Coverage Map

Status: frozen migration checklist.

This maps the archived planning docs into the current authoritative docs.

Purpose: make sure nothing discussed in `goatos-categories.md` and related
planning files is lost while keeping the active docs clean.

Do not treat this as living architecture. It is a baseline proving scope was
not lost during consolidation. Removing rows or narrowing scope needs an
explicit review note or ADR.

## Source Docs Now Archived

```text
docs/archive/planning-history/goatos-categories.md
docs/archive/planning-history/goatos-current-system-assessment.md
docs/archive/planning-history/goatos-dashboard-visibility.md
docs/archive/planning-history/goatos-frontend-gap-analysis.md
docs/archive/planning-history/goatos-infra-and-context-direction.md
docs/archive/planning-history/goatos-pluggable-architecture.md
docs/archive/planning-history/existing-repos-inventory.md
docs/archive/planning-history/existing-repos-deep-audit.md
```

## What Is Covered Where

| Old topic | Current home |
| --- | --- |
| Goat passport, identity, old tags, RFID, duplicates | `context/product/goat-os-feature-phases.md` Phase 1; `context/architecture/final-architecture.md` |
| Ledger/event envelope/timeline | `context/architecture/final-architecture.md`; `context/execution/next-contracts.md`; `.agents/skills/goatos-build/references/contracts-events.md` |
| Locations, farms, parks, sheds, cohorts | Product Phase 1; architecture module rules |
| SOP/config engine | Product Phase 2; `context/forms/final-forms-sop-engine.md` |
| Task engine, carry-forward, assignment | Product Phase 2 and Phase 4; `context/forms/final-forms-sop-engine.md` |
| Video/proof verification, park head + central verifier | Product Phase 2 and Phase 3; architecture verification rules |
| Shared proof/media/verification engine across feed, health, procurement, attendance, death, and dispatch | Product cross-cutting proof section; architecture media/verification rules |
| Workforce ops: operators, teams, park heads, shifts, absence, fallback/backfill, handover, escalation | Product Phase 4; Phase 2 minimal vaccination roster; architecture workforce rules |
| Attendance/check-in proof and operator entry logs | Product Phase 4; legacy `health_manager_attendance.js` discovery |
| Vaccination schedules/campaigns/stock linkage | Product Phase 2; forms/SOP doc |
| Health/treatment/death/withdrawal | Product Phase 3 |
| Growth/weight/ADG/body condition | Product Phase 5 |
| Feed/feed conversion/water/supply | Product Phase 5 |
| Crop/fodder farming, farmer network, sowing, crop tasks, harvest, crop expenditure | Product Phase 5B; legacy `farmer_crops_automation.js` discovery |
| Movement/shifting/K1-K2-K3 stage alerts | Product Phase 5 |
| Breeding/pregnancy/kidding | Product Phase 6 |
| Genetics/pedigree/embryo/semen/inbreeding | Product Phase 6 |
| Procurement/vendor/source/landing cost | Product Phase 7 |
| Holding-farm warmup, transport/transit, arrival gate, discrepancy review, intake proof | Product Phase 7 |
| Inventory/vaccine/feed/medicine/equipment batch and expiry | Product Phase 7 |
| Sales/readiness/allocation/exit | Product Phase 8 |
| Customer/festival promise safety: delivery-date eligibility, no double-booking, uncleared feed-contamination gate, promised-weight risk, trusted weight/price, booking-date price audit, replacement/substitution, continuous open-promise monitoring, evidence trail | Product cross-phase promise safety section; Phase 1 foundation; Phase 3 health/withdrawal/feed-clearance; Phase 5 weight/feed/movement; Phase 8 allocation/replacement/price audit/promise-monitoring sweeper |
| Meat yield and genetics feedback | Product Phase 8 |
| Devices/RFID/scales/cameras/ultrasound/collars | Product Phase 9; frontend/mobile device adapters; analytics telemetry flow |
| Edge agent/device gateway/raw telemetry | `context/architecture/final-architecture.md`; `context/analytics/final-analytics-infra.md`; Product Phase 9 summary |
| AI/R&D FaceID/proof/age/weight/pregnancy/disease/genetics/meat-yield models | Product Phase 9; architecture AI authority boundary |
| Data labeling/model registry/realtime inference/batch scoring | Product Phase 9 summary; architecture AI/R&D rules; future implementation docs |
| CEO/admin/investor dashboards | Product Phase 10; frontend architecture; dashboard visibility rules |
| Analytics/BigQuery/Tinybird/Cube/dbt/Metabase/AI analyst | Product Phase 10; `context/analytics/final-analytics-infra.md` |
| Unit economics, COGS, cost/kg, cost/goat, load/source margin | Product Phase 7 captures cost events; Product Phase 10 defines governed metrics |
| Public website | Out of Goat OS core; frontend architecture says business-context reference only |
| Public web lead capture | Future commerce/public surface only, not Goat OS core build |
| Bookings/orders/occasion rules/customer donor portal | Product Phase 8 at high level; detailed commerce context remains architecture/future scope |
| Ownership/payments/settlements/support | Product Phase 8 high level; detailed commerce context remains future scope |
| Auth/permissions/RBAC/realms | `context/architecture/final-architecture.md`; frontend architecture; agent/context protocol |
| Audit/notifications/Slack outbound/Slack ingest bridge | Architecture; frontend Slack automation section; execution plan migration bridge |
| Context layer/agent layer/skills | `context/agents/ai-agent-context-and-protocols.md`; `.agents/skills/goatos-build/SKILL.md` |
| Dev/stg/prod, IAM, CI/CD, load tests | `context/execution/env-load-test-and-doc-hygiene.md`; `context/execution/two-dev-build-plan.md` |
| Pluggable ports/adapters | `context/architecture/final-architecture.md`; frontend architecture; archived pluggable doc |
| Existing dashboard/mobile/slack repo audit | `context/frontend/final-frontend-mobile-backend-architecture.md`; `.agents/skills/goatos-build/references/existing-repos.md`; archived repo audits |

## Items Intentionally Not First Product Loop

These are not missing; they are later product modules or future depth:

```text
training / worker certification
biosecurity
park infrastructure and maintenance
lactation and kid milk-feeding
sellable surplus production such as milk/manure
commerce support tickets
settlements
customer/donor portal depth
investor ownership portal depth
advanced AI model registry and labeling workflows
public website changes
```

They stay visible in this coverage map so they are not forgotten, but they do
not block the first Goat OS loop.

## First Product Loop Still Stands

```text
goat passport
-> vaccination SOP task
-> Android operator proof
-> verifier approval
-> goat timeline update
-> dashboard compliance
```

This loop proves the core machinery. The same machinery then expands across
health, workforce, growth, feed, breeding, genetics, procurement, sales,
devices, AI/R&D, and analytics.
