# Round 4 persistence compatibility review

This is a design review against recorded architecture and the parent's refreshed real-staging read-only evidence. No DB write or new migration is approved or performed. Parent reports refresh2026-09-16T12:53UTC on real Cloud SQL, latest migration317, inventory7 vaccine/dose records, feed17 records/4active and zero admin UI config entries. Those are attributed observations, not queries independently repeated by this judge.

Independently rechecked source snapshot397114d1d06baddb50dffc7d2c2f9df1d0497b7b baseline schema at lines1980–1997 and3161–3178. `admin_ui_config_entries` permits only presentation value kinds text/label/title/copy/tone/disabled_reason. `inventory_items` has tenant/item UUID, code, name, constrained category, base unit, status, context and row_version. Neither supports the mock's complete generic model directly.

## Compatible persistence direction

| Data authored in central interface | Existing versus proposed persistence |
|---|---|
| Existing vaccine/dose inventory | Reference existing inventory item IDs and preserve stock/protocol consumers; do not reseed duplicates from mock names. |
| A new physical Needle | Could be an inventory consumable if inventory behavior is intended. Arbitrary taxonomy and module applicability still need separately designed metadata/contracts; inventory category enum is not an arbitrary category tree. |
| Feed catalogue | Preserve feed_item_catalog identity, specialized nullable nutrient properties, active/retired state and existing ration references. Do not duplicate it into inventory merely for a central page. |
| Arbitrary category/subcategory/module links | Proposed tenant-scoped registry/relations. Stable hierarchy IDs, acyclic parent validation, grant union semantics and retirement behavior need schema/API design. Category links govern availability, not authorization. |
| Feed quantity/time, clinical protocol or weighing SOP policy | Use authoritative existing domain writer, dimensions/effective dates, validation, revision, audit and publish lifecycle. A generic scalar table cannot replace them. |
| New reporting weight/valuation settings | Proposed persisted business configuration and consumers; inspected source hardcodes them. Separate reporting from valuation; never write derived revenue/average price as policy. |
| SOP comparisons using a shared configuration | Proposed stable reference format plus typed resolved snapshot compatible with existing versioned form contracts. Publishing must validate grants/unit/type, record revision/value, and retain task-pinned historical definitions. |
| Transit dependencies | Proposed event/subject/target-SOP mapping over existing durable event/workflow services. Names alone cannot identify a published SOP version or a real producer event. |
| UI titles/copy | Existing admin_ui_config_entries may fit its documented presentation kinds; zero current entries is not evidence that it is a general-purpose configuration store. |

## Gates for an implementation proposal

- Preserve tenant isolation, source IDs and domain ownership; category/subcategory foreign keys and uniqueness need explicit constraints. Define migration/backfill and retirement without breaking historical references.
- Inventory choice availability remains subject to task scope, stock, expiry, FEFO and backend disabled reasons. A category link must never bypass those checks or actor grants.
- Keep effective-dated ration/schedule dimensions and null-versus-zero; a descriptive free-text scope is not a persistence resolver.
- Domain writes must bump relevant contract/config family revisions and invalidate ETags/caches. Current family revision numbers are freshness markers, not business configuration values to copy.
- Phone compatibility must preserve published/pinned SOPs, typed DTO/form support, durable outbox idempotency and conflict behavior. New generic reference syntax cannot be claimed supported merely because the local mock compiles it.
- Document read-your-writes/consumer refresh and list batching so new central lookup does not recreate contract load failures or route fanout. The existing `backend_down`, board and weights failure strings need real route/mobile evidence before production certification.
- No clinical proof cap or policy can be generalized across features from a convenient scalar sample. Existing immutable/locked policy and feature-specific validation still apply.

The central interface is compatible as an authoring shell over authoritative domain stores plus deliberately new registry relationships. It is incompatible as a bulk dump of browser state into UI configuration or a replacement of all domain stores with untyped item JSON. The detailed parent data plan will be checked separately once written.

## Detailed plan review

Reviewed the now-written `staging-data-plan.md`. **Compatible as a bounded proposal**, with no persistence blocker in its stated direction: it preserves inventory category constraints and no-stock-on-create, feed identity/properties, clinical lifecycle, existing SOP storage/engine hooks, typed proposed policy ownership, module applicability versus permission, immutable runs and revisions/ETags. It explicitly refuses to insert sample IDs/string modules/descriptive scopes directly into staging. No prose correction was necessary.

The plan's fresh observation that12 active SOP definitions carry category_key/subcategory/triggers does not establish a reusable item hierarchy/module-grant schema: those fields describe SOP definitions. Implementation still requires the plan's new taxonomy/reference contract. This distinction reconciles the fresh schema evidence with the earlier bounded search of backend/internal for subcategory text. The plan remains a proposal, not an executable migration or proof that new consumers are deployed.
