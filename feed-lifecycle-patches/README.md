# Feed Direction issue → amend → lock lifecycle — patches for PR #12 branch

Two commits, rebased onto the current remote head `174f45c7` (PR review round 2),
with migration numbering corrected for the reviewer's renumber.

## Apply

```bash
git checkout feat/counts-breakdown-filters      # at origin head 174f45c7
git am feed-lifecycle-patches/0001-*.patch feed-lifecycle-patches/0002-*.patch
```

(`git apply --check` passes clean, so `git am` will not 3-way.)

## What changed vs the originally-pushed version

- **Migration renumbered** `000007_feed_direction_issues.sql` → **`000013_feed_direction_issues.sql`**
  (remote had renumbered the feed migrations to 000009–000012 and inserted 000003–000008).
  `make validate-migrations` applies 000001→000013 in order, clean.

- **Clock unified.** The reviewer added `now func() time.Time` + `WithNowFunc`; the lifecycle added
  its own `now Clock` + `WithClock`. `Clock` is `type Clock func() time.Time`, so they collapsed to a
  single `now Clock` field. `WithClock` is canonical; `WithNowFunc` kept as an alias (reviewer tests use it).

- **Past-date regeneration guard scoped to the live-compute path.** The reviewer's
  `ErrPastDateRegenerationBlocked` was in `normalizePreviewQuery`/`normalizePackingQuery`, which run
  *before* the serve branch — that would have blocked **serving a frozen historical issued sheet**,
  defeating the lifecycle. It is now gated `if q.Draft && s.isPastBusinessDate(...)`: the serve path
  (Draft=false) reads frozen rows / returns never-issued and never regenerates, so it may serve a past
  feed day; the draft (live-compute) path stays guarded. Reviewer guard tests adapted with `Draft: true`
  to preserve their intent.

## Verified in an isolated worktree (branch `feed-lifecycle-onto-remote`)

```
go build ./...            EXIT 0
go vet ./...              EXIT 0
go test ./internal/feeddirection/... ./internal/adminui/...   ok
GOATOS_RUN_POSTGRES_TESTS=1 go test ./internal/feeddirection/...   ok (11.6s)
make validate-migrations  passed (000001→000013)
scale-guard / aggregate-projection / idempotency-writes / seed-migration /
  india-date / deployed-job-flags   all PASS
```

## Not included

`docs/runbooks/{cbe-data,data,kids-cpt}.txt` — raw source row dumps, intentionally left untracked
(AGENTS.md bans committing raw source dumps).
