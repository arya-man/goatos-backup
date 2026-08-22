# SOP Library Seed Documents

Migration `000186_sop_library_milk_and_weighing.sql` seeds library documents for
existing Milk and Weighing workflows. This is not a clean-slate replay step and
does not create executable `sop_tasks`; it inserts idempotent `sop_definitions`
and `sop_versions` rows for every tenant, matching the earlier module SOP
library seed pattern.

Seed/closeout implication: run normal migrations. No separate `seed-*` command is
required for these documents.
