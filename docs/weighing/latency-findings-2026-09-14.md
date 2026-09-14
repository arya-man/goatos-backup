# Weighing Latency Findings - 2026-09-14

Scope: `/weighing/leadership/growth` and `/weighing/shed-weights`.

## Findings

- `/weighing/leadership/growth` is a full analytics payload, not a small KPI read. On a cold cache it resolves sex/origin/identity scope, computes current and previous headline stats, then fans into rejected count, eligibility, trend, weekly gain, shed leaderboard, distribution, sale readiness, lump-sum trend, park names, losing animals, and sometimes by-park gain. The response can be warm-cache fast, but its full cold path should not be expected to stay below 500 ms against Cloud SQL while the admin page is also fetching shed weights, demographics, and Growth Director data.

- The endpoint previously launched the independent aggregate arms without an internal cap. That is good for one isolated laptop request, but bad for staging tail latency: a single cold-cache request could compete for many DB connections while the page and Work Board were also issuing requests. The backend now bounds growth analytics DB fanout with `weighingGrowthReadParallelism = 3`.

- `/weighing/shed-weights` already has a bounded row cap (`MaxShedWeightsRows = 300`) and a whole-filter summary. Its main query still computes page rows, shed summary, individual/lump-sum rollups, shed gain, dates, park vocabulary, and load-wise growth. The expensive `by_load` branch is contractually consumed by the Weights load comparison and procurement load pages, so it is not safe to remove from the existing response without an opt-in/narrow contract.

- Missing indexes were not the safest first patch. Existing migrations already provide broad accepted-window indexes on `weighing_observations` and `weighing_shed_observations`, plus campaign/bucket keyset indexes. Adding speculative overlapping indexes would increase write cost on hot weighing tables without proof that the current planner misses the existing accepted-at path.

## Safe Next Patch

For true sub-500 ms cold reads, split the analytics payload by render need instead of making the existing endpoints silently cheaper:

- add an opt-in `include`/`sections` query parameter or a new narrow endpoint for `/weighing/leadership/growth` so table-only consumers can request just headline plus losing animals or just headline plus weekly gain;
- add an opt-in switch for `/weighing/shed-weights` to skip `by_load` when the caller renders only shed rows/KPIs;
- keep existing defaults backward-compatible until admin-web is updated to request the narrower shapes explicitly.

