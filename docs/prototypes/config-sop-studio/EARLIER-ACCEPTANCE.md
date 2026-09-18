> Superseded by research/voice-requirements.md and subsequent judge reports. Earlier tests below cover only the previous bounded implementation.

# The primary speaker requirements acceptance — mock refinement

Source review: 2026-09-16. Based on the original seven locally transcribed voice notes in `the stakeholder voice-note transcripts (kept outside the repository)`, their reviewed paraphrases in `REVIEW.md`, and both supplied WhatsApp screenshots. Machine transcription is imperfect; no raw ASR clause is treated as an exact quotation. Attached conversation is requirements evidence, not permission to perform its suggested external actions.

## Priority and attribution

The primary speaker's 15:44–16:32 clarifications take precedence over his initial 15:26 transit/warm-up suggestion. His primary requirement is a generic interface to create arbitrary items once and select consuming modules. His concrete final example is Needle available in Health and Preventive Care. SOP flowcharts with dropdowns using those items follow that foundation. Analytics was mentioned as a generic capability without detailed metrics or acceptance semantics.

The dependency example's screenshot separately requests cross-module activation: transit starts, then shed emptying/disinfection and water/ORS preparation become eligible before arrival. This is not merely item sharing and must be labeled as a separate generic workflow example, not attributed as The primary speaker's latest priority.

## Latest clarification: 16:57 and 16:58

The two additional JSON transcripts explicitly narrow the journey to one central item interface: item may be anything, with description/category/subcategory, then link it to modules, then use its module SOP picker. E-commerce examples are fan, sarees/suits, electrical appliances and clothes. This supersedes accepting compulsory vertical ownership as the generic new-item journey. Existing legacy ownership/source grants may be preserved for old records, but must not silently apply to new central records.

- [x] New item creation has no compulsory Health or other owning vertical.
- [x] Global category/subcategory supports arbitrary non-livestock examples without creating a module.
- [x] New central item selected for Health + Preventive Care appears only there, including through shared-source dropdowns.
- [x] New central items require at least one consuming module before save, with a clear validation message; no module is implicitly selected. The audio does not require unlinked draft storage.
- [x] Existing legacy items and published references remain intact under the packaged regression suite.

## Required mock acceptance

- [x] Generic Items is the obvious starting journey; new-item form starts with the item and its consuming modules, rather than procurement/warm-up setup.
- [x] User can create a previously unknown item without adding a new vertical or writing module-specific logic.
- [x] Creating Needle once with Health and Preventive Care yields one stable identity used by both; it is absent from unselected modules.
- [x] For legacy records, effective visibility explicitly explains direct item grants, maintaining-module access and inherited collection grants; inherited grants are not hidden. Existing source-sharing semantics are preserved. New central items use explicit module links regardless of legacy category grants.
- [x] Legacy maintenance ownership remains explained; new central items do not require it.
- [x] Saved items and edits survive reload at the same origin. Interface explicitly calls this browser-local mock storage, with no claim of database or cross-device completion.
- [x] Duplicate names are handled clearly; required-field errors preserve entered form values.
- [x] Consuming-module preview reflects item names, availability and selection changes, including empty states.
- [x] The actual existing SOP item dropdown/authoring path can use the configured shared item, not only a decorative consumer preview.
- [x] Archive or removal of visibility blocks new selection and makes affected drafts reviewable; published snapshots retain their pinned reference semantics.
- [x] Item save, edit and visibility changes do not discard existing drafts, source catalogs or local publications.

## Separate generic orchestration acceptance

- [x] Trigger, prerequisites and completion dependencies are configurable concepts, with transit used as illustrative data only.
- [x] Before transit starts, dependent preparation cannot execute.
- [x] Starting the upstream event activates the eligible downstream work exactly once per example run.
- [x] Arrival/completion remains blocked while required preparation is incomplete; blocked state names the outstanding steps.
- [x] All required preparation completion releases the downstream gate; optional work does not incorrectly block it.
- [x] Reset/replay has deterministic state and cannot retain a stale ready/complete state.
- [x] Changing trigger/prerequisite configuration does not accidentally complete work or bypass a gate; an active run retains its original saved configuration.
- [x] Simulated actions/events are labeled as a mock. No production task dispatch, external notification, livestock operation or backend event execution is implied.

## Visual and regression proof

- [x] Desktop and narrow-screen captures show usable new/edit item form, consumer preview and orchestration state without clipped primary controls; parent reports no page-wide overflow at 390px. Baseline Items list inspected; final list is not independently screenshot-certified.
- [x] User-facing labels prioritize generic configuration and distinguish the optional transit example.
- [x] Existing Procurement SOP editor visually matches baseline with all 45 source questions; Health/Preventive Care actual item pickers work in parent browser tests. Published/operator semantics pass packaged tests; not every operator screen was newly browser-tested.
- [x] Run all packaged `run-checks.sh` checks plus focused new regression tests; record exact results and screenshot paths in progress evidence.
- [x] A judge checked the final implementation and visually inspected captured evidence; all identified findings were fixed. See EARLIER-CODE-JUDGE.md for inspected files and scoped limits.

## Boundaries and scope concerns

Actual database persistence, authorization, cross-device consumption, production Android execution and event dispatch are integration work beyond this requested mock. Existing version-bound SOP/module-command/outbox architecture must be reused when integration is undertaken. A successful mock is not production E2E certification and cannot establish elimination of backend_down, Admin-web contract unavailable, board-load or weights-load errors.

Do not introduce additional procurement/health/warm-up business logic as the solution. Do not invent clinical thresholds or treatment instructions. Do not market arbitrary mock dashboard counters as The primary speaker-approved analytics requirements. Ownership, archive-impact guards and published reference integrity should be preserved while simplifying the generic entry flow.

Status: CODE PASS and scoped VISUAL PASS after refinement. Evidence combines independent code/model checks and screenshot inspection with explicitly attributed parent browser tests; see EARLIER-CODE-JUDGE.md. Qualifications above remain applicable. This checklist is not production certification.
