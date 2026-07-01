# Vaccination V1 Clean-Slate Coverage Status

Date: 2026-07-01
Branch: `vaccination-v1-close`
Checklist source: `docs/phc-vaccination/V1-SOP-BUILDER-BUGS-E2E-CHECKLIST.md`

## Summary

The V1 demo blockers are closed for local-dev proof.

The proof is scoped to the V1 PHC vaccination demo contract. V1 now captures the
source nuance policy in config and enforces the local kernel gates, including
source compatibility spacing for the V1 matrix. Route/resource optimization,
business admin setup, and million-goat permutation testing are separate
production programs outside this V1 closure.

Current evidence:

- Full E2E smoke: `NUANCE-RULES-20260701-V1-MATRIX-R2`
- SOP/Config authoring: `NUANCE-RULES-20260701-V1-MATRIX-R2`
- Click matrix: `NUANCE-RULES-20260701-V1-MATRIX-R2`
- Source nuance rules: `docs/phc-vaccination/source-nuances-rules.md`
- Nuance kernel/config package proof: `go test ./internal/adminui/app ./internal/protocol/app ./internal/vaccination/app -count=1`
- Older-goat anti-flood: `go test ./internal/vaccination/app -run 'TestOlderGoat' -count=1`
- Calendar drive collapse: `go test ./internal/calendar/adapters/postgres -run 'TestCalendarVaccinationProjectionCollapsesBatchedGoatDosesToDrive' -count=1`
- Chain proof: `vaccination-chain-proof stamp=1782915162`
- Trusted-history existing-vaccination proof: `vaccination-trusted-history-proof stamp=1782917268`
- Rework proof: `vaccination-rework-proof stamp=1782899439`
- Visual smoke screenshots: `.codex-goatos-render/admin-web-screenshots/2026-07-01T14-12-57-572Z`
- Rework Goat Passport screenshot: `.codex-goatos-render/admin-web-screenshots/2026-07-01T09-04-55-269Z/desktop-goat-passport-rework-proof.png`

## Coverage Table

| Checklist area | V1 demo status | Evidence | Separate scope |
| --- | --- | --- | --- |
| Users and role setup | Closed for local demo | `seed-dev-grant` in `NUANCE-RULES-20260701-V1-MATRIX-R2`; UI runs as Superadmin / CEO / COO. | Business-facing user-management setup UI is not claimed. |
| Park and shed setup | Closed for local demo | `seed-vaccination-trigger` plus proof-created local sheds feed generated work. | Full admin UI for park/shed creation is not claimed. |
| Goat setup | Closed for local demo | `vaccination-chain-proof.log` creates goat, emits `goat.created`, generates obligations; visual smoke captures Goat Passport. | Full browser validation matrix for every goat field remains production-hardening. |
| Goat entry paths | Closed for V1 demo | Manual goat API path and procurement accepted-intake matrix proof pass in `NUANCE-RULES-20260701-V1-MATRIX-R2`; Herd Register visual route is captured. | Full browser CSV/import negative matrix is production hardening. |
| SOP builder lifecycle | Closed for V1 demo | `NUANCE-RULES-20260701-V1-MATRIX-R2` creates, validates, dry-runs, publishes, reopens, edits, and republishes. | Broader multi-domain SOP rollout is outside vaccination V1. |
| SOP field types | Closed for V1 demo | Authoring smoke covers text, number, yes/no, select, multiselect, goat scan, shed picker, vaccine batch, medicine picker, photo proof, video proof. | Additional usability polish can continue after demo. |
| Conditional rules | Closed for V1 demo | Authoring smoke saves `require_if`, `require_proof`, and `block_if_empty` rules and republishes. | Complex cross-step rule authoring beyond V1 smoke remains hardening. |
| Config/matrix setup | Closed for V1 demo | `NUANCE-RULES-20260701-V1-MATRIX-R2` loads the Nuance Rules matrix, previews impact, saves drafts, publishes ET+TT, PPR, Goat Pox, FMD, and HS as separate protocol versions, and reopens the drawer with source metadata. | Additional source rows are data entry, not a V1 code blocker. Matrix UX hardening remains: `Source schedule` must be explicitly read-only/derived or editable through a clear control, and `Add matrix row` must not silently prefill source-preset data unless the user chose a copy action. |
| Nuance Rules source dependency | Closed for V1 demo | `source-nuances-rules.md` preserves the DOCX source; Config captures vaccine type/pathogen class, source schedule, dose, vial, revaccination, same-day/gap policy, procurement warm-up, adult/source-vaccination policy, pregnancy policy, and defer states. `go test ./internal/adminui/app ./internal/protocol/app ./internal/vaccination/app -count=1` passes. | Route/resource optimization is separate scope. |
| Generation and backfill | Closed for V1 demo | `vaccination-chain-proof stamp=1782915162` proves existing-goat backfill, `goat.created`, generation, sweeper, proof, booster, and replay idempotency. `vaccination-trusted-history-proof stamp=1782917268` proves a goat seeded with accepted/verified existing ET+TT 4-week history suppresses that old dose and creates the next ET+TT 7-week obligation. | Large mixed-state permutation testing is separate production scope. |
| Older-goat unknown-history anti-flood | Closed for V1 demo | `TestOlderGoatUnknownHistoryCreatesOnlyOneHistoricalCatchUp`, `TestOlderGoatUnknownHistoryCreatesOnlyOnePHCReviewCatchUp`, `TestOlderGoatTrustedFirstDoseAllowsNextMissingDose`, and `vaccination-trusted-history-proof stamp=1782917268` pass. | Large mixed-state permutation testing remains production hardening. |
| Calendar drive aggregation | Closed for V1 demo | `TestCalendarVaccinationProjectionCollapsesBatchedGoatDosesToDrive` and `vaccination-trusted-history-proof stamp=1782917268` prove Calendar uses a shed-drive batch row after batching, while the goat-level next obligation remains in AC/WF/Passport and the trusted old 4-week row does not surface as new Calendar work. | Route/resource optimization remains separate scope. |
| Execution and proof accepted path | Closed | Full smoke proves proof upload -> verify accept -> completion -> booster. | None for V1 demo. |
| Rejected/rework path | Closed for data-plane demo | `vaccination-rework-proof stamp=1782899439` proves reject -> rework -> resubmit -> accept, with rejected and accepted Passport history. | Full browser field-worker rework walkthrough is separate hardening. |
| Operational surfaces | Closed for current V1 surfaces | Full smoke and visual smoke cover Vaccination, Action Center, Protocol Adherence, Workflows, Calendar, Control Tower, Config, SOP Library, Herd Register, Audit, DLQ, Goat Passport. | Future/unbuilt modules remain out of scope. |
| Interlinked navigation/clicks | Closed for current V1 surfaces | `NUANCE-RULES-20260701-V1-MATRIX-R2` passes shell/sidebar, Vaccination, Action Center, Protocol Adherence, Workflows, Config, and SOP Library controls/drawers/interlinks. | Narrow/mobile click matrix can be expanded later. |
| Goat Passport vaccination polish | Closed for V1 demo | Passport API exposes real `workflow_row_id`; UI shows open due rows/history and Workflow/Action Center links. Screenshot: `desktop-goat-passport-rework-proof.png`. | Broader health/passport presentation polish remains product polish. |
| Visual QA | Closed for current V1 demo | `smoke:visual:live` passes desktop and narrow active routes; final screenshots under `2026-07-01T14-12-57-572Z`. | Pixel baseline comparison is not part of this run. |

## Safe Demo Claim

Safe:

> V1 PHC vaccination is demo-ready locally: SOP authoring, multi-row
> breed/stage/sex/class-aware config authoring, source nuance policy capture,
> source schedule/dose/vial/revaccination rows, V1 compatibility spacing,
> local defer/procurement/pregnancy kernel gates, due generation,
> older-goat unknown-history anti-flood, shed drive grouping, Calendar
> drive-first aggregation after batching, proof,
> verification, rework, booster scheduling, replay idempotency, current UI
> interlinks, and Goat Passport history are proven.

Separate production scope:

> Route/resource optimization, business user/shed admin setup UI, and
> million-goat permutation testing are not part of this V1 demo handover. The
> V1 vaccination rule matrix and compatibility spacing are part of this handover.
