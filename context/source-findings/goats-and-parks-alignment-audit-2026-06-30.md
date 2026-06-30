# Goats And Parks Alignment Audit

Date: 2026-06-30

Purpose: document the repo-wide revisit requested after adding
`wiki/Goats and Parks.docx` as the base goat/park source. This is an alignment
audit, not a claim that every referenced feature is complete end to end.

## Files And Areas Checked

- Repo guidance: `AGENTS.md`, `SKILLS.md`, `context/README.md`,
  `.agents/skills/goatos-build/references/source-findings.md`.
- Frontend/IA: `context/frontend/current-admin-web-scope.md`,
  `apps/admin-web/AGENTS.md`.
- Kernel: `context/architecture/operational-kernel.md`.
- Product phases/glossary: `context/product/goat-os-feature-phases.md`,
  `context/product/glossary.md`, `docs/phases/README.md`.
- PHC/Vaccination: `docs/phc-vaccination/PRD.md`,
  `docs/phc-vaccination/TRD.md`,
  `context/source-findings/phc-vaccination-roster-stage-proposal.md`.
- Feed Direction and Counts/Shifting:
  `docs/feed-direction/PRD.md`, `docs/feed-direction/TRD.md`,
  `docs/feed-direction/BUILD-TO-DONE-GOAL.md`,
  `docs/feed-direction/DEPENDENCY-CLOSURE-PRD.md`,
  `docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md`,
  `docs/feed-direction/COUNTS-SHIFTING-CLOSURE-PRD.md`,
  `docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md`.
- Procurement/source entry:
  `context/execution/procurement-source-entry-backend-handoff.md`.
- Critical guardrails:
  `docs/features/critical-animal-action-guardrails.md`.
- Source findings:
  `context/source-findings/drive-docs-findings.md`,
  `context/source-findings/feed-transfer-kt-2026-06-24.md`,
  `context/source-findings/feed-direction-workbook-automation-findings.md`,
  `context/source-findings/feed-direction-counting-db-reconstruction.md`.
- Code/doc search for: Goats and Parks, shed tag, RFID, ear tag, warm-up,
  pregnancy, fattening, session, K0/K1/K2/K3, Procurement, Parks, CT, AC,
  Calendar, SOP, feed, and counts.

## Alignment Findings

| Area | Current alignment | Gap / action |
| --- | --- | --- |
| Repo-wide source rule | Previously partial. Existing docs mentioned `Goats and Parks`, but no committed full base-source finding existed. | Closed in this pass by adding `goats-and-parks-source-findings.md` and routing docs to it. |
| Kernel | Aligned. The operational kernel already applies to PHC, feed, procurement, parks, and future modules, and requires obligations/proof/read models for process breaks. | Future implementation must turn missing tag, stage, feed-safety, ICU/quarantine, and warm-up exceptions into kernel work instead of UI-only flags. |
| Admin-web IA | Aligned. Vertical/module/command-lens taxonomy keeps Parks as a scope/context dimension and command lenses as top-level surfaces. | Future Parks-owned modules can be added only as real modules, not nested command-room routes. |
| Control Tower / Action Center / Calendar / Protocol Adherence / Workflows | Aligned as top-level command lenses fed by module projections. | Feed, Counts, Procurement, PHC, and Parks/Sheds projections still need to emit the Goats-and-Parks exception fields before these screens can claim those slices. |
| PHC/Vaccination | Mostly aligned. K0/K1/K2/K3 and K2=42 already come from Goats and Parks/glossary evidence. Vaccination uses tag/RFID and trained execution/proof concepts. | References are updated to the new committed source finding. Production roster expansion remains separate source-backed work. |
| Feed Direction | Mostly aligned. Current docs already cover source priority, configurable sessions, breed/tag/stage ration keys, pregnancy/warm-up risk, underfeed/overfeed/moist-feed safety, and count/projection separation. | Patched the docs to make Goats and Parks the base stage/shed/feed-role source and to prevent KT/workbook examples from replacing source-backed runtime templates. |
| Counts/Shifting | Aligned directionally. Aggregate shed + breed counts stay separate from nutrition cohort resolution, and RFID-to-shed is future scope. | The resolver must use reviewed shed-tag/cohort reference data from Goats and Parks before Feed generation can be safe. |
| Procurement/source entry | Aligned directionally. Source-only identity, purpose-specific warm-up, accepted intake, and arrival proof are modeled separately from clean park truth. | Add destination shed tag/cohort review as part of accepted-intake-to-park truth when procurement/feed work resumes. |
| Parks/Sheds/Locations | Partially aligned. Location and capacity models exist, and product taxonomy treats Parks as physical scope. | Full shed-tag reference data from Goats and Parks is not yet imported/published as governed data. Future Locations/Parks work should add effective-dated shed tag semantics. |
| SOP/workflows | Aligned at engine level. SOP/proof/verification exists as shared engine. | SOP packs must add handling, trained-operator, medicine, weighing, feed-panel cleaning, and unsafe-object rules as category-specific policy, not free text only. |
| Critical animal guardrails | Aligned directionally for ICU/quarantine/high-risk transitions. | References now point to the committed Goats and Parks finding. Future packs should use the full shed-tag list rather than a subset. |
| Analytics/read models | Aligned directionally: analytics docs already name Goats/Parks and RFID/scale/camera/device events as source material. | Add metrics only from canonical events/projections; do not derive product truth from BI shortcuts. |

## Non-Negotiable Runtime Implications

- The base identity rule is tag/RFID-first. Visual descriptors are never goat
  identity.
- Park and shed are physical scope. Shed tag is operational cohort semantics and
  can affect feed, PHC, breeding, birth, procurement, and exception workflows.
- Warm-up, pregnancy, lactation, mother, milking, flushing, breeding, fattening,
  ICU, and quarantine are not labels for display only; they change risk,
  eligibility, feed, proof, and escalation.
- Feed-session defaults are data/config. The source's two serving sessions are
  evidence, not a hardcoded reason to prevent admin-approved session changes.
- Current examples such as Masoor Bhusa, Concentrate, 80/20, 50/50, weight
  thresholds, and KT ratios are evidence/templates only unless an approved
  protocol version publishes them.
- Weight capture and logging are identity-linked operational truth; stale or
  missing weights are data-quality states.
- Trained medical actions need role/skill authority.
- Temporary labour can receive simple ground tasks only; GoatOS must not assign
  high-risk decisions or medical execution just because labour is available.

## Remaining Implementation Work

This audit does not finish the active Feed Direction goal. Remaining work before
Feed Direction can be called done still includes the ordered `G2`-`G17` closure,
real Counts/Shifting projection, reviewed ration/template imports, feed
generation, obligations, proof/rework, read models, frontend surfaces, and local
E2E proof.

Future slices should add governed reference data for the full shed-tag table and
its effective dates, then wire those references into Locations/Parks, Counts,
Feed, PHC, Procurement, Breeding, SOP, and command-lens projections.
