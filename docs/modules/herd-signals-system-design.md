# Herd Signals — System Design and Scalability

**Status:** Design record, 2026-08-22. Companion to
[`herd-signals.md`](./herd-signals.md), which owns the product/vocabulary
boundary; this document owns the *serving shape*: load model, storage tiering,
read budgets, failure modes, and the scale-out ladder.

> **Built vs designed.** Every section is tagged. `[BUILT]` means the code
> exists at the cited file and the migration is committed. `[DESIGNED]` means
> it is a decision recorded here with **no implementation** — do not cite this
> document as evidence of shipped behaviour. Section 8 is the consolidated
> not-built list.
>
> **Every threshold here is provisional** unless it is a committed constant,
> in which case the constant is cited.
>
> **Language boundary applies in full.** This is BLE radio telemetry. Nothing
> in this module identifies a named behaviour or a clinical state; the
> temperature field is **tag temperature**, measured at the tag housing.
> See `herd-signals.md` Section 3.

---

## 1. Load model

### 1.1 Parameters

The load is a function of four parameters. State them explicitly rather than
quoting a single "packets per day" number as if it were a property of the
hardware.

| Parameter | Symbol | Today (measured) | Release envelope |
|---|---|---|---|
| Tags in service | `T` | 20 bench tags | 5,000 – 50,000 (per the scale-envelope ADR) |
| Advertising interval | `A` | ~12.9 s effective (derived below) | assume **10 s** for headroom |
| Gateways hearing a given tag | `G` | 1 | 1 nominal; 2–3 where coverage overlaps |
| Gateway POST batch period | `B` | n/a | assume 30 s |

`A` is the *effective* interval — the tag's advertising rate as filtered by
what the gateway actually forwarded, not the firmware's nominal rate. It is
the only one of the four that can be measured from the existing capture.

### 1.2 Deriving `A` from the bench capture

```
14,000 packets / (2.5 h × 3600 s) = 1.556 packets/s across the deployment
1.556 packets/s / 20 tags         = 0.0778 packets/s per tag
1 / 0.0778                        = 12.86 s per packet per tag per gateway
```

So the observed effective interval is **~12.9 s**, and the 10 s planning
assumption is conservative by ~29%. Both are carried below.

### 1.3 Packet rate at the release envelope

`packets/s = T × G / A`

| `T` | `A` | `G` | packets/s | packets/day |
|---|---|---|---|---|
| 5,000 | 10 s | 1 | 500 | 43.2 M |
| 50,000 | 12.9 s | 1 | 3,888 | 335.9 M |
| 50,000 | 10 s | 1 | **5,000** | **432.0 M** |
| 50,000 | 10 s | 2 | 10,000 | 864.0 M |

**The independent review's ~430M packet rows/day at 50k tags on a ~10 s
interval reproduces exactly** (`50,000 / 10 × 86,400 = 432,000,000`). It is
correct, and it is a *floor*: it assumes each tag is heard by exactly one
gateway. Overlapping coverage multiplies it linearly, and `G` is the parameter
most likely to be underestimated in a real shed layout.

### 1.4 Bytes per packet row

`herd_signal_packets` (migration `000192`) carries a `raw_adv` text column and
a `raw_payload` jsonb alongside the typed columns.

| Component | Bytes (est.) |
|---|---|
| Tuple header + line pointer + alignment | ~30 |
| `packet_id` uuid + `tenant_id` uuid | 32 |
| `gateway_id`, `source`, `tag_id`, `tag_mac` text | ~45 |
| 4 × timestamptz (`received_at`, `gateway_seen_at`, `created_at`) | ~24 |
| numerics/ints/smallints/bools | ~26 |
| `raw_adv` (a 31-byte advertisement as hex ≈ 62 chars) | ~66 |
| `raw_payload` jsonb (`'{}'` today) | ~12 |
| **Heap subtotal** | **~235, round to 250** |
| 4 btree entries (3 from `000192` + the `000193` dedup unique index), ~48 B each | **~190** |
| **Total per row incl. indexes** | **~440** |

The index overhead is ~43% of the row cost. That is not a tuning detail at
this volume: the four indexes on an insert-only table are, together, nearly as
expensive as the data.

### 1.5 Storage growth

At **50k tags / 10 s / G=1 → 432 M rows/day**:

| Horizon | Packet rows | Bytes (heap + index @440 B) |
|---|---|---|
| Day | 432 M | **~190 GB** |
| Week | 3.02 B | **~1.33 TB** |
| Month (30 d) | 13.0 B | **~5.7 TB** |

At 5,000 tags the same arithmetic gives 43.2 M rows/day, **~19 GB/day**,
~570 GB/month — an order of magnitude smaller but still not a table anyone
keeps forever without a retention decision.

### 1.6 Activity-window rate

Three tiers are written on every ingest
(`repository.go:84`, `activityWindowTiers = []int{60, 300, 3600}`). Row count
is driven by *time*, not by packet rate — one row per tag per bucket per tier:

| Tier | Buckets/tag/day | Rows/day @50k tags |
|---|---|---|
| 60 s | 1,440 | 72.0 M |
| 300 s | 288 | 14.4 M |
| 3600 s | 24 | 1.2 M |
| **Total** | 1,752 | **87.6 M** |

**The review's ~90 M activity-window rows/day across three tiers is correct**
(87.6 M exactly). Estimated row cost is ~128 B heap + ~140 B across the PK and
two secondary indexes ≈ **270 B**, so ~**23.7 GB/day** if every tier is
retained without limit.

### 1.7 Write amplification and WAL

Per ingest batch the transaction does (`repository.go:97`):

1. one gateway upsert;
2. `N` packet inserts, sent as one `pgx.Batch`;
3. `3 × (distinct tag,bucket)` activity-window upserts — one pass per tier;
4. one `tag_latest` upsert per distinct tag.

Batching is what keeps step 3 sane. With `B = 30 s`, a batch carries ~3
packets per tag but collapses to **one** 60 s-tier bucket per tag, so window
upserts are `T × 3 tiers / B` = **5,000/s at 50k tags**, not per-packet. Even
so, at 50k tags the steady write shape is roughly **1,700 packet inserts/s +
5,000 window upserts/s + 1,700 `tag_latest` upserts/s**.

Window upserts are `ON CONFLICT DO UPDATE` — every one produces a dead tuple.
That is **~432 M dead tuples/day at the 60 s tier alone**, against 72 M live
rows. This, not the packet table, is the autovacuum pressure point: packets
are insert-only (freeze/visibility-map work only), activity windows are
update-heavy and will bloat without aggressive per-table autovacuum settings.

WAL should be planned at **200–400 GB/day** at 50k tags — heap + index dirtying
plus full-page writes after each checkpoint. Backup, replication lag, and
storage IOPS budgets all derive from that number, not from the row count.

---

## 2. Ingest path `[BUILT]`

`POST /herd-signals/packets` — `adapters/http/handler.go:57`, service at
`app/service.go:40`, repository at `adapters/postgres/repository.go:97`.

| Property | Where | Behaviour |
|---|---|---|
| Auth | `handler.go:63-72` | tenant + actor must both be present in request context; 401 otherwise. There is no anonymous gateway path. |
| Strict decode | `handler.go:76-78` | `DisallowUnknownFields` — an unrecognised field fails the batch rather than being silently dropped. |
| Per-packet timestamp tolerance | `service.go:56-60` | a packet with an unparseable `seen_at` is logged and skipped; the rest of the batch proceeds. |
| Empty batch | `service.go:82` | rejected with an error, not accepted as a no-op. |
| Single transaction | `repository.go:102` | gateway upsert, packet inserts, all three window tiers, and every `tag_latest` update commit or roll back together. |
| Dedup | migration `000193` | unique index on `(tenant_id, tag_id, received_at, motion_count) NULLS NOT DISTINCT`; inserts use `ON CONFLICT DO NOTHING`. |
| Replay accounting | `repository.go:151-170` | only rows whose insert reported `RowsAffected > 0` enter `newPackets`; rollup and `tag_latest` operate on that subset. |
| Snapshot monotonicity | `repository.go:222` | `tag_latest` advances only when the incoming `received_at` is strictly newer than the stored `last_seen_at`. |

### 2.1 Why `(tenant_id, tag_id, received_at, motion_count)` is the natural key

`motion_count` is cumulative on the tag. Two rows from the same tag at the same
`received_at` carrying the same `motion_count` are, physically, one reading
observed twice — there is no state the tag could have been in that produces two
distinct such packets. The key needs no vendor sequence number and no
server-assigned id, so it survives a gateway that retries a batch verbatim.

Empirically it holds: the maintainer verified the tuple is **perfectly unique
across all 13,751 real captured rows**. That is a strong but not unbounded
result — it says nothing about a future firmware that resets `motion_count`
mid-second, and nothing about two gateways forwarding the same advertisement
(see 2.2).

### 2.2 The NULL-`motion_count` case — closed, with a caveat

A battery- or temperature-only advertisement can carry a NULL `motion_count`.
Under standard SQL semantics every NULL is distinct from every other NULL, so
such packets would never conflict and a retried batch would double-insert them
— the exact failure the index exists to prevent. Migration `000193` closes this
with `NULLS NOT DISTINCT`, so **the hole is closed in the committed
migration**, not outstanding.

The caveat that *is* outstanding: `gateway_id` is not part of the key. Two
gateways that both hear the same advertisement and both forward it produce one
stored row, and whichever arrives second is silently dropped. That is
deliberate for dedup, but it means **`herd_signal_packets` cannot answer
"which gateways heard this tag"** — per-gateway RSSI comparison, the natural
basis for proximity localisation, is not derivable from the stored data.
Recorded here as a known limitation, not a defect.

### 2.3 Out-of-order safety

Two independent mechanisms:

- **Within a batch:** window rollup tracks `firstMotionAt`/`lastMotionAt` and
  compares packets by `received_at`, not by iteration order
  (`repository.go:1009-1051`). An earlier fix (`M4`) corrected a condition that
  had made "last" mean "whichever the Go map iterated last".
- **Across batches:** `tag_latest` will not move backwards
  (`repository.go:222`), and `IngestPackets` returns `latestUpdated` counting
  only snapshots that actually advanced.

**Known gap `[DESIGNED]`:** the window upsert sets `motion_delta = $7` on
conflict (`repository.go:1105`), i.e. this batch's delta replaces the stored
one rather than being recomputed as `stored_first → new_last`. For in-order
arrival within a bucket the two agree. For a *late* packet landing in an
already-written bucket they do not. The correct form recomputes from
`LEAST(stored_first, new_first)` to `GREATEST(...)` by timestamp; that is not
implemented.

---

## 3. Storage tiering

| Tier | Table | Grain | Written by | Read by | Retention decision |
|---|---|---|---|---|---|
| Raw | `herd_signal_packets` | one advertisement | ingest tx | **nothing on any request path today** | `[BUILT]` 14 days, partition-dropped (000200/000201) |
| 60 s windows | `herd_signal_activity_windows` (`bucket_seconds=60`) | tag × minute | ingest rollup | timeline for ranges ≤ 1 h | `[DESIGNED]` **24–48 h** |
| 300 s windows | same table | tag × 5 min | ingest rollup | timeline ≤ 24 h; p75 baseline (`repository.go:841`) | `[DESIGNED]` 30 days |
| 3600 s windows | same table | tag × hour | ingest rollup | timeline > 24 h | `[DESIGNED]` 13 months |
| Snapshot | `herd_signal_tag_latest` | one row per tag | ingest tx | live list, summary, insights | permanent (50k rows, ~15 MB) |

Two facts drive the retention shape:

1. **No read path reads `herd_signal_packets`.** Grep the repository: every
   query on the request path targets `herd_signal_tag_latest`,
   `herd_signal_activity_windows`, or `herd_signal_gateways`. Raw packets exist
   for forensics and for re-derivation if the advertisement decoding rules
   change. A table nothing reads, growing at 190 GB/day, is the single largest
   cost in the module and the easiest to bound.
2. **The 60 s tier is only reachable for ranges ≤ 1 h**
   (`domain.SelectBucketTier`, `motion.go:199`) unless a caller explicitly pins
   `bucket_seconds=60` — and `MaxTimelineBuckets = 2000` (`motion.go:225`)
   caps that at a ~33 h span anyway. So **nothing the code can serve needs 60 s
   buckets older than ~48 h.** Keeping them longer buys nothing and costs
   ~19 GB/day.

### 3.1 Partitioning `[BUILT]`

`herd_signal_packets` is now RANGE-partitioned daily on `received_at`
(migration `000200_herd_signal_packets_partition.sql`). The existing captured
rows were copied into the new partitioned table and the old heap kept as
`herd_signal_packets_pre_partition_000200` for one retention cycle as a
rollback/audit safety net (not dropped by the migration). `herd_signal_activity_windows`
remains unpartitioned — its own volume (Section 1.6, ~24 GB/day at full
tiers, well below packets) does not currently justify the conversion cost;
revisit if/when a tier's own row count crosses the same order-of-magnitude
threshold that justified this migration for packets.

The dedup unique index (`herd_signal_packets_dedup_uidx`, originally 000193,
redefined by 000196 to key on `device_seen_at`) had to be widened to include
`received_at` because PostgreSQL requires every unique index on a partitioned
table to include the partition key. This is a deliberate, documented
correctness trade — not a silent constraint change — recorded in full in
000200's migration header: it narrows dedup so a retry whose new
server-stamped `received_at` lands in a *different* daily partition than the
original attempt is no longer guaranteed to be deduplicated by this index
alone. Accepted because nothing on any read path reads `herd_signal_packets`
(this section's own opening claim), so a rare duplicate raw row is a
forensics-only artifact, not a product-visible bug. The PRIMARY KEY changed
from `(packet_id)` to `(packet_id, received_at)` for the same structural
reason.

Below, superseded by the above (kept for the historical decision record):

**Decision: range-partition on time, and do it at introduction.**

| Table | Partition key | Granularity |
|---|---|---|
| `herd_signal_packets` | `received_at` | daily |
| `herd_signal_activity_windows` | `bucket_start` | daily for the 60 s tier; monthly is adequate if the tiers are split into separate tables |

The reason to decide now rather than later is mechanical, not aesthetic:
retention on a partitioned table is `DROP TABLE` on a partition — instant, no
dead tuples, no index churn. Retention on an unpartitioned table is
`DELETE ... WHERE received_at < …`, which at 432 M rows/day means deleting
432 M rows to reclaim a day, generating an equal volume of dead tuples and WAL,
and leaving index bloat that only `VACUUM FULL` (an exclusive lock) reclaims.
And **converting a multi-billion-row table to partitioned after the fact
requires copying every row**. The cheap moment is while the table is empty.

This aligns with, rather than contradicts, the scale-envelope ADR's removal of
partitioning from `goat_identity_events` / `audit_log` /
`obligation_status_events`: that removal was justified by those tables' *actual
measured* volumes at 5k–50k animals. `herd_signal_packets` is three to four
orders of magnitude larger per day than any of them. The ADR's ladder ends with
"add partitioning… for the measured hotspot"; the arithmetic in Section 1 is
that measurement, taken before the table is filled rather than after.

Note that the `000193` dedup unique index must include the partition key to
remain enforceable on a partitioned table — `received_at` is already in it, so
the key is partition-compatible as written. This is a fortunate accident worth
preserving.

### 3.2 Retention `[BUILT — raw packets only]`

Migration `000201_herd_signal_packets_partition_maintenance.sql` adds two
functions, hardcoded to target `herd_signal_packets` only (never a
parameterized table name — a typo or careless future caller cannot point
this at `herd_signal_activity_windows` or `herd_signal_tag_latest`, which
retain aggregates on their own longer, separately-designed schedule):

- `herd_signal_packets_ensure_future_partitions(days_ahead int default 14)`
  — idempotent; creates any missing daily partition through `today + days_ahead`.
- `herd_signal_packets_prune_expired_partitions(retention_days int default 14)`
  — `DROP TABLE` on every daily partition entirely older than the cutoff. A
  catalog-only operation independent of partition row count: no dead tuples,
  no index bloat, no `VACUUM FULL`.

Default retention is **14 days** (the top of the 7–14 day range this section
originally proposed) — see the reasoning in 000201's migration header:
retention cost here is symmetric (storage only, since nothing reads this
table) while the forensics/re-derivation value of the window is asymmetric
and irreversible once a partition is dropped, so the default favors the
longer end.

`backend/cmd/herd-signals-partition-maintenance` (`make
herd-signals-partition-maintenance`) is the documented command that calls
both functions against `DATABASE_URL`, intended to run once daily.
**Scheduling it (cron / Cloud Run job / etc.) in any environment is an infra
step outside this migration's scope and is NOT yet wired up** — running the
command against the target database is currently a manual/operator action
until that scheduling exists.

Activity-window per-tier retention (60 s / 300 s / 3600 s: 48 h / 30 days /
13 months) remains `[DESIGNED — NOT BUILT]`: `herd_signal_activity_windows`
is not partitioned (see 3.1) and has no TTL/pruning job of its own yet.

---

## 4. Read paths and their budgets

The repo's API latency policy (`tools/perf/api-latency-policy.mjs`) is
**p90 ≤ 300 ms, p95/p99 ≤ 500 ms**, and AGENTS.md is explicit that a
seconds-class operator read is a bug regardless of a green `ci-local`.

| Endpoint | Cadence | Query shape | Index | Bounded? | Budget risk at 50k |
|---|---|---|---|---|---|
| `GET /herd-signals/live` (page) | 5 s | keyset `(last_seen_at, tag_id) < (…)`, `LIMIT ≤ 201` | `herd_signal_tag_latest_last_seen_idx` | yes, `limit ≤ 200` (`handler.go:117`) | low |
| `GET /herd-signals/live` (summary) | same request | 10 × `count(*) FILTER` over the **whole filter** | none usable — it is a full scan of `tag_latest` | 50k rows | **high — see 4.1** |
| `GET /herd-signals/tags/{id}/timeline` | 60 s | range scan on one tag, one tier | `herd_signal_activity_windows_tag_idx` | yes, `MaxTimelineBuckets = 2000` | low |
| `GET /herd-signals/gateways` | 15 s | full read of `herd_signal_gateways` + one batched shed-location lookup | `tenant` | gateway count (tens) | low |
| `GET /herd-signals/insights` | on load | 12 cards from ~9 queries, 4 of them correlated joins | mixed | 50k `tag_latest` × domain tables | **medium–high — see 4.3** |

The per-page enrichment is already batched correctly
(`service.go:enrichTagsBatch`): one `ResolveTagsBatch`, one `GetGoatsByIDs`,
one `GetShedLocations`, one `GetBaselineDeltas` for the whole page — no N+1
fan-out, per the AGENTS.md rule.

### 4.1 The live summary is the hot path, and it must not need the animal join

`ListTagsLatest` (`repository.go:509`) builds one filter (`herdSignalsLiveFilter`)
and reuses it for both the page and the summary, so the two cannot drift — this
satisfies operational read model contract rule 3 (summary is a whole-filter
aggregate, never summed from the page). Correct, and worth keeping.

But `computeSummary` is passed `tagLocationJoin` **unconditionally**
(`repository.go:576`), so every summary — including the default, unfiltered one
— performs a `LEFT JOIN LATERAL` into `goat_identifiers` plus two more joins,
once per tag, for all 50,000 tags, on every 5-second poll from every viewer.

**Requirement `[DESIGNED]`: when no `park_id`, `shed_id`, or `q` filter is set,
the summary must be computable without the animal join at all.** Two reasons,
and they are independent:

- **Performance.** Ten `count(*) FILTER` aggregates over a 50k-row table with
  no join is a single sequential pass — sub-100 ms and trivially cacheable.
  With the LATERAL join it is 50,000 index probes into `goat_identifiers` per
  request, and this is the most frequently issued query in the module.
- **Correctness under the unmapped-tag invariant.** Every counted column
  (`mapping_state`, `movement_state`, `signal_state`, `battery_state`, the
  sensor bits) lives on `tag_latest`. None of them needs the join. The join is
  present solely to make `park_id`/`shed_id`/`q` filterable. Carrying it when
  those filters are absent is pure cost on the exact path that must work when
  nothing is mapped.

`park_id`/`shed_id`/`q` still require the join, and that is fine: those are
operator-initiated, lower-cadence, and narrow the result.

### 4.2 Timeline

Well-bounded by construction. `SelectBucketTier` picks 60/300/3600 s from the
requested span; an explicitly pinned `bucket_seconds` must be one of the three
stored tiers or the request is rejected rather than silently approximated
(`service.go:155-160`); and `MaxTimelineBuckets = 2000` rejects any
range/tier combination that would exceed it. Densification to fill gaps happens
in Go over at most 2,000 entries — bounded memory, no unbounded scan.

### 4.3 Insights

Twelve cards, of which four are correlated joins against
`vaccination_completions`, `health_cases`, `feed_direction_completions`, and
`weighing_observations`, each joined through `goat_identifiers` to
`herd_signal_tag_latest` (`repository.go:910-985`). At 50k tags these are the
heaviest queries in the module, and the endpoint has **no cache**.

`[DESIGNED]` A **30–60 s TTL cache keyed on `tenant_id`** is the right first
mechanism here, not a projection table: the cards are tenant-global (no
per-viewer parameters), the freshness requirement is loose (they describe
30-minute and 24-hour windows), and `N` viewers currently multiply an identical
computation `N` times. A cache collapses that to one computation per TTL
regardless of viewer count — a strictly larger win than optimising the query,
and reversible.

---

## 5. Polling and fan-out

Cadences are specified in `herd-signals.md` Section 7 and restated here for the
cost arithmetic: live 5 s default (5/15/60 s selectable), timeline 60 s,
gateways 15 s, polling paused while the tab is hidden, stale banner at 30 s,
manual refresh always bypasses both.

Per active viewer, per second:

```
live      1/5  = 0.200 req/s   → 2 queries each (page + summary)
gateways  1/15 = 0.067 req/s
timeline  1/60 = 0.017 req/s   (only when a tag drawer is open)
```

| Concurrent viewers | live req/s | summary scans/s | rows scanned/s for summary alone @50k tags |
|---|---|---|---|
| 5 | 1.0 | 1.0 | 50 K |
| 20 | 4.0 | 4.0 | 200 K |
| 50 | 10.0 | 10.0 | **500 K** |

That last row is the case to design against, and it is *entirely* summary cost:
the paginated page itself is a keyset read of ≤ 200 rows and does not scale
with viewer count in any interesting way. Two mitigations, in order:

1. drop the animal join from the unfiltered summary (Section 4.1) — makes each
   scan roughly an order of magnitude cheaper;
2. `[DESIGNED]` a short TTL cache (5–10 s, i.e. at most one poll interval) on
   the *unfiltered* summary only. Filtered summaries stay uncached: they are
   rare, parameterised, and narrow.

Pause-on-hidden-tab is load-bearing at these numbers, not a nicety: without it,
every tab any operator ever left open contributes its full 0.2 req/s forever.

---

## 6. The unmapped-tag invariant

Fully specified in `herd-signals.md` → "Unmapped tags are the NORMAL state, not
a degraded one". Not restated here. The architectural consequences that belong
in *this* document:

- **The unfiltered path must not touch the animal tables at all.** Section 4.1
  is the same requirement arrived at from the performance side. The hot path
  and the staging-correctness path are the same path, which is a useful
  property: making it fast and making it correct are one change, and a
  regression in either shows up in the other.
- **`herd_signal_tag_latest` deliberately stores no `park_id`/`shed_id`**
  (`repository.go:437`), because an animal's shed changes and a cached copy
  would go stale silently. That choice is what forces the LATERAL join onto the
  filtered path, and it is the right trade *today*. Section 7 names the
  measurement that would reverse it.
- **An inner join anywhere on the tag→animal path empties the screen on
  staging, silently.** No error, no zero-with-a-reason — an empty list that
  looks like "no tags". Every join on that path is `LEFT JOIN` in the committed
  code (`tagLocationJoin`, `repository.go:439-449`); a regression test that
  ingests a packet for a tag with no `goat_identifiers` match and asserts the
  row, the summary count, and the timeline is the guard against it.

---

## 7. Failure modes and backpressure

| Mode | What happens today `[BUILT]` | Gap `[DESIGNED]` |
|---|---|---|
| **Gateway offline** | `gatewayStatus` (`service.go:570`) computes `online`/`offline` from `last_seen_at` freshness against `StalePacketMinutes = 30`, ignoring the stored status string. Tags fall to `pattern_state = missing` after 30 min. No data is lost; the window rows for that period simply do not exist, and the timeline renders them as `is_gap = true` — distinguishable from "packets arrived, zero movement". | No alert on a gateway that stops reporting; an operator must be looking at the gateways view. |
| **Clock skew (gateway vs server)** | `received_at` is taken verbatim from the gateway's `seen_at` (`service.go:56`). Nothing validates it against server time. | **Real incident:** the bench capture's timestamps were Asia/Kolkata wall clock and were once stored as UTC, placing every packet **5.5 hours in the future**. Consequences are systemic, not cosmetic: `tag_latest` monotonicity locks to a future `last_seen_at` and refuses correctly-stamped packets; buckets land in future partitions; "stale" and "missing" invert. **Required:** reject or clamp a packet whose `received_at` is more than a small skew window (provisional: 5 min future, 24 h past) from server time, and record the rejection rather than dropping it silently. Not implemented. |
| **Duplicate / replayed batch** | Fully handled. `000193` + `ON CONFLICT DO NOTHING` dedup the rows; `newPackets` ensures rollup and `tag_latest` skip them; an all-replay batch commits as a no-op returning `stored=0, latestUpdated=0` (`repository.go:172-179`). | The response does not distinguish "0 stored because replay" from "0 stored because rejected" beyond the counts. |
| **Gateway flooding the endpoint** | Nothing. No per-gateway rate limit, no batch-size cap, no request-body size cap beyond the platform default. A misconfigured or compromised gateway can issue arbitrarily large batches, each inside one transaction. | **Required:** cap packets per batch (provisional: 5,000), and rate-limit per `(tenant_id, gateway_id)`. Not implemented. |
| **Partial batch failure** | Two different behaviours by failure class, both deliberate: a packet with an unparseable timestamp is *skipped* and the batch proceeds (`service.go:56-60`); any DB error aborts the whole transaction, so nothing partial is ever committed. | The skipped-packet count is not returned; `Accepted` counts the request payload and `Stored` the inserts, so the difference conflates "skipped bad timestamp" with "deduped replay". |
| **Ingest slower than arrival** | No queue. The gateway POST is synchronous against Postgres; backpressure is the request timing out, and the gateway's own retry is what recovers — which the dedup key makes safe. | At 50k tags this becomes the binding constraint before any read path does. See Section 7.1. |

### 7.1 Where ingest breaks first `[DESIGNED]`

The synchronous-write-to-Postgres design is right at the current scale and is
squarely inside the ADR's "canonical indexed SQL, one worker, no projections"
envelope. It stops being right somewhere between 5k and 50k tags, and the
constraint is write throughput, not query latency: ~1,700 packet inserts/s plus
~5,000 window upserts/s plus the WAL volume in Section 1.7. When it does, the
ordered response is **retention first, partitioning second, and only then**
decoupling ingest from rollup (persist packets synchronously; roll windows up
in a worker stage). Do not reach for a queue before the two cheap mechanisms.

---

## 8. Scale-out ladder

Following the scale-envelope ADR's ordering — repair plans and indexes, reduce
duplicate work, tune batches, adjust cadence, then and only then add a
projection, a stage, a worker, a partition. Each step below names its
**trigger metric**, not a feeling.

| # | Step | Trigger metric | Status |
|---|---|---|---|
| 1 | Drop the animal join from the unfiltered live summary | `GET /herd-signals/live` p95 > 300 ms, **or** tag count > 10,000 (whichever first) | `[DESIGNED]` |
| 2 | Retention job for `herd_signal_packets` | table > 500 GB, **or** age of oldest row > 14 days | `[DESIGNED]` |
| 3 | Range-partition packets + activity windows on time | **do at introduction** — the trigger is "before the table has data", because the retrofit cost is a full table copy | `[DESIGNED]` |
| 4 | 30–60 s TTL cache on insights | insights p95 > 400 ms, **or** > 10 concurrent dashboard viewers | `[DESIGNED]` |
| 5 | Per-table autovacuum tuning on `herd_signal_activity_windows` | `n_dead_tup / n_live_tup` > 0.2 sustained, or table bloat > 30% | `[DESIGNED]` |
| 6 | Per-gateway rate limit + batch-size cap | any single gateway > 10% of total ingest, or any batch > 5,000 packets | `[DESIGNED]` |
| 7 | Denormalise `park_id`/`shed_id` onto `tag_latest`, refreshed from animal-move events | filtered live p95 > 500 ms **after** step 1 — and only with an event-driven refresher, since the reason it is absent today is staleness, not cost | `[DESIGNED]` |
| 8 | Decouple window rollup from the ingest transaction into a worker stage | ingest p95 > half the gateway batch period (i.e. > 15 s at `B=30 s`), or sustained DB write saturation attributable to rollup | `[DESIGNED]` |
| 9 | A `herd_signal_live_projection` table | only after 1, 4, 5, and 7 are in place and the live read still misses its target — per the ADR, a projection is the tool, not the default | `[DESIGNED]` |

Adequate at 50k tags **as designed** (steps 1–5 applied): the live list, the
timeline, and the gateways view. **Breaks first without them:** storage, then
the live summary under viewer fan-out, then insights, then ingest write
throughput.

---

## 9. What is not built yet

Consolidated, so nobody cites this document as evidence of shipped behaviour.

| Item | Status | Where it would go |
|---|---|---|
| Partitioning of `herd_signal_packets` / `herd_signal_activity_windows` | **NOT BUILT** — the migrations create ordinary tables | new migration |
| Any retention/TTL/pruning of any tier | **NOT BUILT** — no job, no maintainer, no `DELETE` anywhere | kernel worker stage |
| Gateway per-tag counters | **NOT BUILT** — `TagsSeenRecently`, `WeakTags`, `UnmappedTags` are **hardcoded `0`** with a `TODO` (`service.go:293-295`). The gateways view renders zeros that read as measurements. Either compute the per-gateway `tag_latest` aggregate or omit the fields. | `repository.go` + `service.go` |
| Insights caching | **NOT BUILT** — no cache of any kind in the module | `app/service.go` |
| Join-free unfiltered summary | **NOT BUILT** — `computeSummary` always receives `tagLocationJoin` | `repository.go:576` |
| Clock-skew rejection/clamping | **NOT BUILT** — `received_at` is trusted verbatim | `app/service.go` |
| Per-gateway rate limiting / batch-size cap | **NOT BUILT** | handler/middleware |
| Cross-batch `motion_delta` recomputation for late packets | **NOT BUILT** — conflict path overwrites with the current batch's delta | `repository.go:1105` |
| Per-gateway RSSI retention (dedup drops the second gateway's copy of an advertisement) | **NOT BUILT and not currently derivable** | key/schema change |
| Autovacuum tuning for the window table | **NOT BUILT** — defaults only | migration / DB config |

Several of the above were in flight in parallel worktrees at the time of
writing; re-read the cited file before treating any row here as current.

---

## 10. Open questions

- Real `A` and `G` in a deployed shed. Every number in Section 1 is a function
  of them, and only a real gateway deployment settles them.
- Whether `raw_adv` and `raw_payload` both need to persist. Dropping one, or
  compressing `raw_adv` to `bytea`, removes ~25% of the heap cost of the
  largest table in the system.
- Whether the 60 s tier earns its ~19 GB/day given that no default read path
  selects it (`SelectBucketTier` returns 60 only for spans ≤ 1 h).
- Whether packets should be written to object storage rather than Postgres,
  given that nothing on the request path reads them.
