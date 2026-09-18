# Combined design decisions — Claude and Mesha Studio

Read-only source comparison on 15 September 2026. Claude files were not changed. This report compares inspected behavior, not just visual claims. Existing screenshot references favor the production Mesha shell; no new Claude browser interaction was performed during this review.

Claude source root: inspected Claude mock workspace snapshot.
Our source root: the local prototype workspace.

## Adopt from Claude

| Decision | Why | Evidence | Concrete combined change |
|---|---|---|---|
| Owned here / Read from other modules | Makes edit authority and reuse understandable without opening a sharing matrix | Claude app.js:366-380 renders owned and read-only inherited tables; owner links return to source Config | Add the same two sections to every module's Business rules; source references remain read-only |
| Scope, used-by and effective metadata | Gives CEO enough context to judge a policy change | Claude app.js:263-279 and :332-334; effectiveValue at :151 resolves park overrides | Show accurate company scope, actual consumers and actual local publication time; add park overrides only when implemented, never imply them |
| Production shell density and distinct icons | Closer to supplied screenshots than our repeated glyphs and settings-only navigation | Claude app.js:199-240 reproduces park selector, module groups and leaf navigation | Retain our role preview and local-prototype identity while using distinct familiar module icons and production-like grouping |
| Contextual insertion and selectable validation findings | Reduces graph repair effort | Claude flow.js:281-303 includes edge-local plus controls and selection; :400 links validation entries to nodes | Prefer insert-before/after a selected step and clickable error links over adding disconnected steps blindly |
| Form/list and flow views of one definition | Makes long SOPs approachable while retaining branching clarity | Claude flow.js:215-239 switches List/Flow with one graph state | Future combined editor should retain one underlying definition and support both views, rather than fork saved content |

## Keep from our implementation

| Decision | Why | Evidence |
|---|---|---|
| Standalone four-level Items Config | A dataset list alone does not fulfill Vertical → Category → Subcategory → Item management | items.js:2-18 provides stable category/item IDs, owner hierarchy, edits, sharing and linked usages |
| Actual persisted draft and immutable compiled publications | Visible version numbers must correspond to distinct content | app.js persists local state; studio-v2.js compileWorkflow/confirmPublish stores cloned nodes/items/catalogues/config; read-only preview uses published definition |
| Per-item source availability and owner references | Shared dropdowns must change when source items change | availableItems and nodeCatalogue; compiled catalogue snapshots; validation of archived/unshared items |
| Read-only role demonstrations | User specifically requires directors/operators to see published configuration/SOP behavior | SOP list/editor and operator preview separate editable draft from published snapshots |
| Explicit > versus >= and margin formula | Exactly-at-threshold behavior cannot be guessed | Sales editor and formula disclose comparison and subtractive allowance; Weighing reads same published Sales state |
| Local-only language and reference-only Health examples | Mock publication is not live app deployment or clinical validation | studio-v2.js definition/publish copy; health-course.js raw source dosage and unspecified-unit handling |
| Diagnosis engine separated from ordered course and generic workflow | Parallel diagnoses must not collapse into a single decision-tree leaf | V3 diagnosis workspace remains separate; generic workflow supports Q&A/actions; course retains day/session ordering |

## Reject or correct before borrowing

1. **Claimed immutable publication without a frozen graph.** Claude flow.js:439-445 claims version pinning, but publication only changes `G.sop.published` and `G.sop.draft`. Its Save draft handler (:217) calls `touch`, a toast and repaint. No localStorage/sessionStorage persistence was found in app.js/flow.js. Keep our real clone/persistence model; never inherit the stronger claim from their copy.
2. **Hardcoded impact statistics presented as consequences.** Claude app.js:65 includes `101 → 112`; :68 promises recomputation of a fixed farm value. The editor at :300-328 displays this static `blast` text. Use actual dependent-screen/SOP counts and clearly labeled examples, not invented before/after metrics.
3. **Ambiguous or changed eligibility semantics.** Claude sale-ready row (:65) states at-or-above 35, while the user's example was greater-than 35. Keep an explicit comparator and equality explanation.
4. **Effective-date appearance without scheduling.** Claude `effective` field is assigned TODAY after Apply (:325). That supports an applied-date label, not future effective-date scheduling/history. Our combined UI should label this accurately.
5. **Implementation internals in normal authoring flow.** Claude exposes storage tables, constants, proposed architecture and governance locks beside ordinary settings. Keep provenance available in details; normal forms should explain ownership and business effect plainly.
6. **Automatic expansion of medical authority.** Do not copy source treatment rules or emergency claims into editable global settings without the current source's actual authorization model. Retain source-exact examples and distinguish UI simulation from clinical execution.

## Immediate priority order

1. Add Owned here / Read from other modules and accurate scope, consumers and publication metadata.
2. Finish generic shared dropdown source controls across modules with one Items Config source.
3. Keep Health Diagnosis, ordered Treatment courses and generic SOP flow distinct but cross-linked.
4. Adopt contextual error navigation and insertion next; preserve working graph validation, published snapshots and role boundaries.
5. Final judges must verify the combined build in Chrome at desktop and 390px after all edits. This comparison is a design decision receipt, not a final combined-build signoff.

## Implemented in this combined revision
- Module Business rules now explicitly identify their owner and company scope, followed by a read-only external-source table showing real available counts and source revisions. Weighing includes the actual published Sales rule and owner navigation.
- Generic shared sources support item collections and record lists, owner/type filters, per-module availability, archive impact review and pinned workflow snapshots.
- Health has separate Diagnosis, Treatment courses and generic SOP authoring. Visual AND/OR clauses edit review drafts; recorded scenario results remain source-pinned.
- Park overrides, future effective scheduling, general Flow/List parity and edge-local insertion remain future enhancements; they are not claimed implemented in this revision.
