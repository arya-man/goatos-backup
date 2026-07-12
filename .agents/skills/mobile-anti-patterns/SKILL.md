---
name: mobile-anti-patterns
description: >-
  Use when writing OR reviewing Android/mobile code under apps/goatos-android/**
  (screens, ViewModels, repositories, Room DAOs, Flows). Enforces Room-SSOT
  offline-first, ~20-row keyset pagination, bounded memory, off-main decode,
  lifecycle-aware collection, and stable list keys. Invoke before touching any
  list/VM/repo/DAO and before pushing. Complements `make mobile-guard` +
  `make android-bounded-memory-guard` (the machine gates) with the how-to-fix.
---

# Mobile anti-patterns (phone-scale + offline-first)

Canonical rulebooks: `docs/decisions/mobile-data-fetch-anti-patterns.md` +
`docs/decisions/android-offline-first.md`.

## Fetch / pagination (machine: `make mobile-guard`)
A phone viewport holds ~7–10 items; pulling 50/200/1000 is the mobile twin of
compute-on-read.
- **Never fetch > ~20 rows/screen.** Every drill level is a keyset page of ~20 with
  infinite scroll (prefetch at item ~17–18). No `limit=1000`.
- **Calendar overview = DOTS ONLY** — per-day markers from a backend marker set;
  never fetch/parse a day's events to draw the grid.
- **A drive is a mix of SHEDS, never grouped by vaccine** (coverage-by-vaccine is a
  metric, not the grouping).
- Genuinely bounded (e.g. fixed 7-cell week loop) → `// mobile-guard:ignore: <reason>`.

## Room SSOT / offline-first (banned: network-only screen reads)
- Every READ screen renders from **Room** (single source of truth); network refresh
  runs in background (stale-while-revalidate). Persist read → repo exposes `Flow` →
  VM observes → refresh-on-open upserts Room → re-emits. **Never a blank/loading wall
  when cache exists.**
- A thin `api.xxx()` pass-through repo with no Room persistence is **BANNED** for
  screen reads. New read models ship with Room entity + DAO + Flow from day one.
- **Pagination binds BOTH layers** — the network fetch AND the observed Room read use
  the same keyset + ~20 page. NEVER `SELECT *` / `observeAll()` / an ever-growing
  accumulated blob; the observed read is a bounded keyset window (Room `PagingSource`
  / Paging 3 + `RemoteMediator`).

## Bounded memory (machine: `make android-bounded-memory-guard`)
The retention twin of over-fetch. An in-heap cache/accumulator with no cap/TTL/
eviction, or a DAO reading a whole table into memory, OOMs low-end phones.
- Use `LruCache` or a Room `JsonBlobCacheDao` (`readCachedJson` TTL +
  `enforceCacheBounds` row/byte cap), or filter the DAO read (`WHERE status IN (...)`
  / `LIMIT` window).

## Off-main + lifecycle (memory/jank)
- Decode/parse off the Main thread: `flowOn(Dispatchers.Default)` in the repo; parse
  each field ONCE (never re-parse inside `.find`/`.filter` → O(n²)).
- Screen VMs expose state via `stateIn(viewModelScope, WhileSubscribed(5_000), initial)`
  — not a forever `collectLatest` (leaks a Room collector when backgrounded).
- Dynamic `LazyColumn` items need a **stable unique key** (not index) + `contentType`.
- Release camera/recorder/BT capture + observers on lifecycle stop.

## Backend contract owns visibility
The mobile UI must NOT gate visibility by role (`role ==`); render the backend-composed
nav/actions/disabled-reasons contract. Machine-blocked by `make mobile-contract-ownership-guard`.

## Test integrity
No committed `@Ignore`/`@Disabled`/commented-`// @Test`; no flaky wall-clock
assertions; no fake-green — run `./gradlew … testStgDebugUnitTest --rerun-tasks`
and `make mobile-guard` for REAL before claiming pass.
