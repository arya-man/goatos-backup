# Vaccination V1 Clean-Slate Coverage Status

**Path B target correction (2026-07-03):** this is a historical local-demo
coverage artifact. The target architecture now uses mixed-species
`herd_animals` / `animal_id` and Animal Passport language per the PRD/TRD/V1
foundation spec. Historical proof artifact names may still include old goat-only
labels, but product wording below should be read as Animal Passport /
per-animal / `animal.*`.

Date: 2026-07-01
Branch: `vaccination-v1-close`
Checklist source: `docs/preventive-care-vaccination/V1-SOP-BUILDER-BUGS-E2E-CHECKLIST.md`

## Summary

The V1 demo blockers are closed for local-dev proof.

The proof is scoped to the V1 Preventive Care (PC) vaccination demo contract. V1 now captures the
vaccination rule policy in config and enforces the local kernel gates, including
compatibility spacing for the V1 matrix. Route/resource optimization,
business admin setup, and million-animal permutation testing are separate
production programs outside this V1 closure.

Current evidence:

- Full E2E smoke: `VACCINATION-RULES-20260703-V1-MATRIX`
- SOP/Config authoring: `VACCINATION-RULES-20260703-V1-MATRIX`
- Click matrix: `VACCINATION-RULES-20260703-V1-MATRIX`
- Source vaccination rules: `docs/preventive-care-vaccination/vaccination-rules.md`
- Vaccination Rules kernel/config package proof: `go test ./internal/adminui/app ./internal/protocol/app ./internal/vaccination/app -count=1`
- Older-animal anti-flood: `go test ./internal/vaccination/app -run 'TestOlderGoat' -count=1`
- Calendar drive collapse: `go test ./internal/calendar/adapters/postgres -run 'TestCalendarVaccinationProjectionCollapsesBatchedGoatDosesToDrive' -count=1`
- Chain proof: `vaccination-chain-proof stamp=1782915162`
- Trusted-history existing-vaccination proof: `vaccination-trusted-history-proof stamp=1782917268`
- Rework proof: `vaccination-rework-proof stamp=1782899439`
- Visual smoke screenshots: `.codex-goatos-render/admin-web-screenshots/2026-07-01T14-12-57-572Z`
- Rework Animal Passport screenshot: `.codex-goatos-render/admin-web-screenshots/2026-07-01T09-04-55-269Z/desktop-goat-passport-rework-proof.png`

## Coverage Table

| Checklist area | V1 demo status | Evidence | Separate scope |
| --- | --- | --- | --- |
| Users and role setup | Closed for local demo | `seed-dev-grant` in `VACCINATION-RULES-20260703-V1-MATRIX`; UI runs as Superadmin / CEO / COO. | Business-facing user-management setup UI is not claimed. |
| Park and shed setup | Closed for local demo | `seed-vaccination-trigger` plus proof-created local sheds feed generated work. | Full admin UI for park/shed creation is not claimed. |
| Animal setup | Closed for local demo | `vaccination-chain-proof.log` creates an animal, emits `animal.created`, generates obligations; visual smoke captures Animal Passport. | Full browser validation matrix for every animal field remains production-hardening. |
| Animal entry paths | Closed for V1 demo | Manual animal API path and procurement accepted-intake matrix proof pass in `VACCINATION-RULES-20260703-V1-MATRIX`; Herd Register visual route is captured. | Full browser CSV/import negative matrix is production hardening. |
| SOP builder lifecycle | Closed for V1 demo | `VACCINATION-RULES-20260703-V1-MATRIX` creates, validates, dry-runs, publishes, reopens, edits, and republishes. | Broader multi-domain SOP rollout is outside vaccination V1. |
| SOP field types | Closed for V1 demo | Authoring smoke covers text, number, yes/no, select, multiselect, animal scan / Animal ID, shed picker, vaccine batch, medicine picker, photo proof, video proof. | Additional usability polish can continue after demo. |
| Conditional rules | Closed for V1 demo | Authoring smoke saves `require_if`, `require_proof`, and `block_if_empty` rules and republishes. | Complex cross-step rule authoring beyond V1 smoke remains hardening. |
| Config/matrix setup | Closed for V1 demo | `VACCINATION-RULES-20260703-V1-MATRIX` loads the Vaccination Rules matrix, previews impact, saves drafts, publishes ET+TT, PPR, Goat Pox, FMD, and HS as separate protocol versions, and reopens the drawer with version/audit metadata. `AUTHORING-2026-07-01T18-35-03-998Z` proves the updated `/config?new_rule=1` authoring page with breadcrumb, page region, preview, save, publish, and reopen drawer. The authoring flow keeps **Add matrix row** blank, exposes **Copy selected row** for intentional copies, and asserts those paths in the live authoring smoke. | Additional matrix rows are data entry, not a V1 code blocker. Larger roster-completion, route/resource optimization, and broader negative browser permutations remain outside the V1 demo closeout. |
| Vaccination Rules dependency | Closed for V1 demo | `vaccination-rules.md` preserves the DOCX evidence; Config captures vaccine type/pathogen class, schedule, dose, vial, revaccination, same-day/gap policy, procurement warm-up, adult prior-vaccination policy, pregnancy policy, and defer states. `go test ./internal/adminui/app ./internal/protocol/app ./internal/vaccination/app -count=1` passes. | Route/resource optimization is separate scope. |
| Goats and Parks source dependency | Closed for V1 demo docs | `context/source-findings/goats-and-parks-source-extract.md` is the tracked Markdown extract of `wiki/Goats and Parks.docx`; `context/source-findings/goats-and-parks-source-findings.md` remains the source-findings summary. | Whenever the DOCX changes, refresh the Markdown extract in git before changing shed/tag/cohort logic. |
| Generation and backfill | Closed for V1 demo | `vaccination-chain-proof stamp=1782915162` proves existing-animal backfill, `animal.created`, generation, sweeper, proof, booster, and replay idempotency. `vaccination-trusted-history-proof stamp=1782917268` proves an animal seeded with accepted/verified existing ET+TT 4-week history suppresses that old dose and creates the next ET+TT 7-week obligation. | Large mixed-state permutation testing is separate production scope. |
| Older-animal unknown-history anti-flood | Closed for V1 demo | `TestOlderGoatUnknownHistoryCreatesOnlyOneHistoricalCatchUp`, `TestOlderGoatUnknownHistoryCreatesOnlyOnePCReviewCatchUp`, `TestOlderGoatTrustedFirstDoseAllowsNextMissingDose`, and `vaccination-trusted-history-proof stamp=1782917268` pass. | Large mixed-state permutation testing remains production hardening. |
| Calendar drive aggregation | Closed for V1 demo | `TestCalendarVaccinationProjectionCollapsesBatchedGoatDosesToDrive` and `vaccination-trusted-history-proof stamp=1782917268` prove Calendar uses a shed-drive batch row after batching, while the animal-level next obligation remains in AC/WF/Passport and the trusted old 4-week row does not surface as new Calendar work. | Route/resource optimization remains separate scope. |
| Execution and proof accepted path | Closed | Full smoke proves proof upload -> verify accept -> completion -> booster. | None for V1 demo. |
| Rejected/rework path | Closed for data-plane demo | `vaccination-rework-proof stamp=1782899439` proves reject -> rework -> resubmit -> accept, with rejected and accepted Passport history. | Full browser field-worker rework walkthrough is separate hardening. |
| Operational surfaces | Closed for current V1 surfaces | Full smoke and visual smoke cover Vaccination, Action Center, Protocol Adherence, Workflows, Calendar, Control Tower, Config, SOP Library, Herd Register, Audit, DLQ, Animal Passport. | Future/unbuilt modules remain out of scope. |
| Interlinked navigation/clicks | Closed for current V1 surfaces | `CONFIG-PAGE-V2-20260701-R2` passes shell/sidebar, Vaccination, Action Center, Protocol Adherence, Workflows, Config page authoring, and SOP Library controls/drawers/interlinks. | Narrow/mobile click matrix can be expanded later. |
| Animal Passport vaccination polish | Closed for V1 demo | Passport API exposes real `workflow_row_id`; UI shows open due rows/history and Workflow/Action Center links. Screenshot: `desktop-goat-passport-rework-proof.png`. | Broader health/passport presentation polish remains product polish. |
| Visual QA | Closed for current V1 demo | `CONFIG-PAGE-V2-20260701` runs the full visual route smoke with screenshots under `.codex-goatos-render/admin-web-screenshots/2026-07-01T18-35-52-753Z`; Config authoring desktop/mobile proof is under `.codex-goatos-render/config-page/`. | Pixel baseline comparison is not part of this run. |

## Safe Demo Claim

Safe:

> V1 Preventive Care (PC) vaccination is demo-ready locally: SOP authoring, multi-row
> breed/stage/sex/class-aware config authoring, vaccination rule policy capture,
> schedule/dose/vial/revaccination rows, V1 compatibility spacing,
> local defer/procurement/pregnancy kernel gates, due generation,
> older-animal unknown-history anti-flood, shed drive grouping, Calendar
> drive-first aggregation after batching, proof,
> verification, rework, booster scheduling, replay idempotency, current UI
> interlinks, and Animal Passport history are proven.

Separate production scope:

> Route/resource optimization, business user/shed admin setup UI, and
> million-animal permutation testing are not part of this V1 demo handover. The
> V1 vaccination rule matrix and compatibility spacing are part of this handover.
