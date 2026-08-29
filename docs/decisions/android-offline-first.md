# ADR: Android read screens are offline-first (Room is the UI's single source of truth)

Status: ACCEPTED — hard rule for every agent (Claude + Codex) and human working on
`apps/goatos-android`.

References (authoritative): Android Architecture — Data layer
(https://developer.android.com/topic/architecture/data-layer) and Offline-first
(https://developer.android.com/topic/architecture/data-layer/offline-first).

## Rule (non-negotiable)

Backend is the source of truth for DATA. On-device, **Room is the single source of
truth for what the UI renders.** Every backend READ response is persisted to Room
first; screens observe Room; the network refresh runs in the background and updates
Room, which re-emits to the UI (stale-while-revalidate). A screen must NEVER show a
blank/loading wall on re-entry when cached data exists.

Concretely, every read model MUST:
1. **Persist to Room** — a Room entity + DAO per read model (typed columns or a
   contract-versioned JSON blob for large/rollup payloads); upserts are transactional.
2. **Expose an observable** — the repository returns `Flow<T>` (or
   `Flow<Resource<T>>`) sourced from the DAO. The ViewModel `collect`s it; it never
   calls a one-shot `suspend api.xxx()` and drops the result on the floor.
3. **Refresh in the background** — on screen open / pull-to-refresh, kick a network
   fetch that upserts Room on success; the DAO Flow re-emits and the UI updates in
   place. Network failure keeps the last cached data visible.
4. **Show a sync/stale indicator** — a small "syncing…"/"updated Xm ago"/"offline"
   affordance, NOT an empty state, while a refresh is in flight over cached data.
5. **Be tenant-scoped and bounded** at 1–5M-animal scale (indexed lookups, paginated
   / keyset where lists can grow; never cache an unbounded full scan).

Do NOT call the app "offline-first" until the READ models are cached too. Bootstrap
(`BootstrapCache`) and the write **outbox** already follow this; the read screens must
match.

## Banned anti-pattern

A **network-only read repository** — a repo method that is just
`api.xxx(...)` returned straight to the ViewModel with no Room persistence — is
banned for any screen-facing read. (As of this ADR the offenders to migrate are
`CalendarRepository`, `ControlTowerRepository`, `ExecutionRepository`,
`AdherenceRepository`, and `VaccinationInsightsRepository` — thin pass-throughs that
caused the "loads every time / empty state" bug.)

## Pattern to follow (NetworkBoundResource / SSOT)

```
fun observeX(scope): Flow<Resource<X>> = flow {
    emitAll(dao.observeX(scope).map { cached ->            // 1) cached-first (may be empty on cold start)
        Resource.fromCache(cached)
    })
}
// separately, on open/refresh:
suspend fun refreshX(scope) = runCatching { api.getX(scope) }
    .onSuccess { dao.upsert(it.toEntity()) }              // 3) Room re-emits -> UI updates
    // onFailure: keep cache; surface stale/offline in the indicator
```

The ViewModel exposes `state = repo.observeX(scope)` (cached immediately) + a
`isRefreshing`/`lastSyncedAt` signal; the screen renders cache + the sync indicator.

## Enforcement

- Code review (`/code-review`) and every agent must reject a new/edited screen-read
  path that is network-only. New read models ship with their Room entity + DAO + Flow
  from day one.
- Applies to all current and future read screens (Calendar, Overview/Control-Tower,
  Sheds/Execution, Adherence, Insights, and anything added later).

## Outbox write lifecycle

Every `OutboxOpType` must declare four separate decisions in the production-owned
`OutboxLifecyclePolicy.kt`: immediate UI/overlay, success reconciliation,
terminal-failure repair, and process-death recovery. Different operations may use
different mechanisms; an upload supporting a parent submission does not need the
same UI as an optimistic Room write. A wildcard/default policy is forbidden because
it would let a newly added operation compile without lifecycle review.

The declaration is architecture metadata, not proof that the mechanism is wired.
Each operation still needs production-path tests for its declared behavior. Kotlin's
exhaustive `when`, `OutboxLifecyclePolicyTest`, and
`check-android-outbox-lifecycle-policy.mjs` jointly prevent an operation from being
added without a declaration. The static guard runs, with adversarial self-tests, from
`make mobile-guard` and ordinary local CI.
