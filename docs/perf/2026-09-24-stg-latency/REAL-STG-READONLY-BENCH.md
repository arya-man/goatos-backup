# Real stg read-only bench: OLD (stg a67be34c0781) vs NEW (PR #389 head 1c4fa880f)

Run on 2026-09-24, 15:17-15:25 IST. All numbers are in milliseconds, measured from a laptop client.
Verdicts use the NEW p95: <=100 target, <=300 ok, <=500 max, >500 FAIL.

| endpoint | stg p95 (before) | OLD p50 | OLD p95 | NEW p50 | NEW p95 | change (p95) | budget verdict (NEW p95) | stmts OLD/NEW | DB ms p50 OLD/NEW | cold OLD/NEW |
|---|---:|---:|---:|---:|---:|---:|---|---|---|---|
| `/growth-director/fcr` (HTTP OLD 200, NEW 500) | 23215 | 1157 | 2496 | 272 | 378 | -85% | n/a (errors) | 11/4 | 1231/318 | 2134/831 |
| `/app/notifications` (HTTP OLD 200, NEW 200/500) | 15175 | 2263 | 2503 | 393 | 3506 | +40% | FAIL | 5/4 | 2260/427 | 6841/8449 |
| `/app/weighing/alerts` | 9853 | 205 | 250 | 189 | 233 | -7% | ok | 3/3 | 256/218 | 576/477 |
| `/weighing/shed-weights` | 9214 | 138 | 262 | 75 | 195 | -26% | ok | 3/3 | 200/96 | 958/1114 |
| `/work-board/page` | 7235 | 298 | 497 | 508 | 989 | +99% | FAIL | 21/22 | 1327/1945 | 1071/1164 |
| `/weighing/weight-demographics` | 5240 | 86 | 154 | 67 | 152 | -1% | ok | 3/3 | 109/92 | 632/663 |
| `/counts/mortality` | 4575 | 175 | 324 | 96 | 166 | -49% | ok | 5/5 | 256/134 | 315/188 |
| `/weighing/leadership/growth` | 4228 | 71 | 182 | 77 | 149 | -18% | ok | 3/3 | 99/106 | 852/596 |
| `/counts/breakdown` | 4203 | 216 | 282 | 208 | 240 | -15% | ok | 6/6 | 325/300 | 318/356 |
| `/app/vaccination/execution` | 3595 | 559 | 844 | 275 | 608 | -28% | FAIL | 4/4 | 683/372 | 566/682 |
| `/alerts/rows` | 3231 | 188 | 223 | 244 | 337 | +51% | max | 9/9 | 412/488 | 667/617 |
| `/calendar/vaccination/events` | 3177 | 41 | 53 | 39 | 93 | +75% | target | 2/2 | 64/67 | 838/1308 |
| `/vaccination/command/cohort-matrix` | 3009 | 504 | 949 | 331 | 381 | -60% | max | 4/4 | 569/404 | 1337/485 |
| `/vaccination/command` | 2469 | 930 | 1259 | 976 | 1394 | +11% | FAIL | 10/10 | 2849/2902 | 932/1088 |
| `/vaccination/command/shed-dose-matrix` | 2300 | 476 | 1054 | 417 | 1035 | -2% | FAIL | 3/3 | 502/454 | 625/475 |
| `/vaccination/sheds` | 2250 | 213 | 238 | 258 | 582 | +144% | FAIL | 5/5 | 238/277 | 485/485 |
| `/weighing/weighing-dates` | 2193 | 79 | 325 | 71 | 152 | -53% | ok | 3/3 | 103/98 | 294/490 |
| `/work-board/rows/feed%7Cfeed_activity%7C00000000-0000-400...` | 2095 | 213 | 230 | 217 | 287 | +25% | ok | 6/6 | 203/212 | 375/438 |
| `/control-tower/vaccination` | 2089 | 32 | 62 | 41 | 2713 | +4244% | FAIL | 2/2 | 57/71 | 5552/4936 |
| `/vaccination/command/drives` | 2054 | 35 | 147 | 40 | 50 | -66% | target | 2/2 | 61/69 | 444/401 |
| `/counts/herd-analytics` | 1990 | 85 | 177 | 113 | 167 | -6% | ok | 4/4 | 101/143 | 216/202 |
| `/feed-analytics/directed` | 1973 | 40 | 283 | 41 | 89 | -69% | target | 2/2 | 59/70 | 731/659 |
| `/feed-config/experiment` | 1738 | 150 | 185 | 87 | 169 | -9% | ok | 3/3 | 193/114 | 241/237 |
| `/feed-analytics/shed-feed` | 1678 | 44 | 127 | 39 | 89 | -30% | target | 2/2 | 74/69 | 820/481 |
| `/sales/overview` | 1619 | 249 | 308 | 193 | 289 | -6% | ok | 14/14 | 874/748 | 553/302 |
| `/feed-analytics/stock` | 1587 | 78 | 154 | 67 | 140 | -9% | ok | 3/3 | 112/91 | 1523/621 |
| `/app/leadership-tasks/assignees` | 1320 | 84 | 148 | 98 | 153 | +4% | ok | 3/3 | 119/138 | 183/159 |
| `/feed-direction/distribution/captures` (HTTP OLD 500, NEW 500) | 1313 | 93 | 162 | 63 | 141 | -13% | n/a (errors) | 3/3 | 126/89 | 248/216 |
| `/feed-config/shed-tags` | 1266 | 45 | 107 | 34 | 83 | -23% | target | 2/2 | 74/59 | 245/130 |
| `/admin/pen-routines` | 1224 | 151 | 246 | 101 | 192 | -22% | ok | 4/4 | 182/127 | 356/265 |

Column notes: `stg p95 (before)` comes from `baseline/stg-api-latency-before.csv`. `change` is NEW p95 against OLD p95.
`stmts` is the median number of Postgres statements per warm request (Simple Query plus extended Execute messages).
`DB ms` is the median server-side DB busy time per warm request, summed across connections, so a handler that fans out in parallel can show more DB time than wall time.
`cold` is the first call after boot plus 30 s idle (the analytics warm-up had already run).

## Method

- **Binaries:** `go build ./cmd/api` from detached worktrees. OLD is `a67be34c0781`, the image tag currently on `goatos-api-stg`. NEW is `origin/perf/stg-burst-and-login` at `1c4fa880f`. They were run one after the other, never at the same time, on 127.0.0.1:18081 and :18082, with identical env.
- **DB access:** the `mesha_ceo_readonly` role, taken from Secret Manager `mesha-ceo-readonly-db-url`, through the Cloud SQL Auth Proxy to `goatos-stg:asia-south1:goatos-stg-core-db`. The DSN sets `options=-c default_transaction_read_only=on`, and a `CREATE TEMP TABLE` probe was rejected. No migrations were run. The API does not migrate at boot: `migrationguard` only reads the version. NEW logged `binaryahead_transient` (DB at 000392, binary at 000401). Neither server log contains a "read-only transaction" error, so neither boot nor the analytics warm-up tried to write.
- **Auth:** local `GOATOS_AUTH_MODE=bearer` (HS256) with a throwaway secret, and tokens minted by `cmd/mint-dev-token` for existing stg users. The main user was `8115af7b-…`, who has the `ceo_internal` grant in tenant `…0001`. No accounts were created and no passwords were used. Other settings: `GOATOS_ENV=local`, `GOATOS_MEDIA_STORAGE=local`, `GOATOS_PG_QUERY_TIMEOUT=15s` (same as stg).
- **Endpoints:** the top 30 paths by stg p95 from the baseline CSV that had a GET in the last 24 h of `goatos-api-stg` request logs. For each path, the most frequent real query string was used, with `cursor` removed from `/vaccination/command/drives`. These baseline rows are POST-only and were skipped: `/feed-direction/packing/complete`, `/feed-direction/distribution/complete`, `/app/workflows/:id/actions/:id/complete`. These had no GET in 24 h and were skipped: `/app/proofs/:id`, `/app/counts/pen-reconciliation/cards/:id/workflow`.
- **Load:** per endpoint, 1 cold call and then 10 warm calls. Only one request was in flight at a time, with a 250 ms pause between calls and 1 s between endpoints. The whole run took about 8 min. Before the run, Cloud SQL showed no rollup storm: CPU was 12-18% and disk read ops were 100-4.5k per minute.
- **Statement count and DB time:** a local Postgres wire relay between the API and the proxy counted Execute and Query messages. It measured busy time as the span from the first frontend message to the backend's ReadyForQuery on each connection. This is exact for sequential requests, apart from small background noise such as pool health checks and LISTEN.

## Caveats

- **RTT:** every statement pays the laptop-to-Mumbai round trip, about 20-40 ms. Both sides are inflated equally, and chatty endpoints (`/work-board/page` at 21 statements, `/sales/overview` at 14, `/vaccination/command` at 10) are inflated the most. On Cloud Run, which sits next to the DB, absolute numbers are much lower. Use the `change` column and the statement count, not the absolute verdicts, to judge the PR.
- **Sample size:** with n=10, p95 is effectively the slowest or second-slowest call, so one stg-side blip decides it. `/control-tower/vaccination` NEW p95 is 2713 ms from a single outlier; its p50 is 41 ms against OLD's 32 ms. `/vaccination/sheds` and `/work-board/page` show similar noise: they run the same statement counts on both sides.
- **Live data:** stg is live and shared, so other users' traffic shifted between the OLD and NEW runs, which were minutes apart.
- **`/growth-director/fcr` (NEW):** returns 500 because NEW reads `management_stage`, which comes from pending migration 000399 and does not exist on stg yet. The NEW numbers are for the error path and are not comparable.
- **`/app/notifications` (NEW):** one 500 (the cold call) came from a statement timeout on the unread count. NEW depends on migration 000396 (`notification_centre_feed_dedupe_index`), which is not applied on stg. Even without that index, NEW's warm p50 is 393 ms against OLD's 2263 ms, and DB time is 427 ms against 2260 ms. Re-bench after 000396 lands.
- **`/feed-direction/distribution/captures`:** returns 500 on both sides with `proof: forbidden`. The local run uses local media storage, which cannot sign the stg GCS proof objects. This is a harness artefact, not a regression.
- **Endpoint coverage:** endpoints that need pending migrations 000393-000401 cannot be fully judged against the un-migrated stg DB.
