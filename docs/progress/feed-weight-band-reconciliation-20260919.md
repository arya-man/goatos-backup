# Feed Weight Band Reconciliation - 2026-09-19

## Scope

Fix the ADG Analytics Weight-wise `Feed by weight band` count mismatch against the General tab for
the same filters. The visible failing case was live `goatos-stg`, all parks, period
`2026-08-03` through `2026-09-15`, Weighing = All, Sex = Male, Origin = All.

## Finding

- General tab read from `/weighing/shed-weights`: `515` animals weighed.
- General split: `183` individual + `332` lump-sum.
- Feed by weight band reconciliation carried the same source split: `183` individual + `332`
  lump-sum.
- Feed by weight band rows are a narrower latest-feed-plan view. They showed `462` matched animals
  on `On farm`; `Include exited` showed `506` after row de-dupe.
- The backend reconciliation still carried the General-tab denominator, but the UI tile did not
  show that denominator beside the row-based count.

## Root Cause

The `Animals weighed` tile was row-based, not the General-tab denominator. It deduped the currently
matched feed-band rows by pen x band x source, so it counted animals attached to the latest feed
rollups/bands under the card's display rules. The card also carried the General-tab reconciliation
split (`183` individual + `332` lump-sum), but that total was not visible beside the row-based tile.

A backend hypothesis was checked and rejected: removing the per-animal suppression behind `pen_avg`
did not change the live `goatos-stg` counts for this case. The remaining mismatch is semantic:
General counts all weighed animals in the period; Feed by weight band rows count matched feed-plan
evidence.

## Fix

- Renamed the row-based tile from `Animals weighed` to `Matched animals`.
- Added a subline with the General-tab denominator, for example `515 total weighed in period`.
- Updated `docs/weighing/feed-direction-by-weight-band.md` to document the distinction.

## Proof

- Live `goatos-stg` read-only check through Cloud SQL Auth Proxy confirmed the failing numbers:
  General `515`, feed reconciliation `183 + 332`, row-based matched `462`, Include exited `506`.
- `cd backend && go test ./internal/adminui/app ./internal/growthdirector/adapters/postgres -count=1`
- `cd apps/admin-web && npm run check:ui-contract`

## Pending

- Push branch and open PR.
