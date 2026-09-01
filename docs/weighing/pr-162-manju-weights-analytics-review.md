# PR 162 Weights Analytics Review

Reviewed locally against OCI via `127.0.0.1:15432`, with backend on
`127.0.0.1:18080` and admin-web on `127.0.0.1:3319`.

## Manju Requirement Map

- General: covered.
- Breed-wise: covered. Shows daily gain and average weight per breed.
- Birth-wise: covered. Shows farm-born vs purchased average daily gain per breed.
- Shed-wise: code now targets the requested comparison: elevated shed vs crown/ground shed,
  grouped by breed. It no longer renders the old per-shed leaderboard.
- Weight-wise: covered as an extra useful tab. Bands latest weights and shows growth per band.
- Time-wise: covered. Shows last 12 weeks plus breed-wise weekly trend.

## Fixes Added In This Review

- Added `weighing_category` support to `/weighing/weight-demographics`.
- Enabled the Weighing filter only on tabs where the API can honestly narrow the data:
  Breed-wise, Birth-wise, Shed-wise, and Weight-wise.
- Added backend response field `gain_by_breed_shed_type`.
- Replaced Shed-wise frontend with elevated vs crown/ground grouped bars per breed.
- Fixed an origin-filter leak in whole-shed weight/gain band inputs.
- Fixed OpenAPI/generated TypeScript contract drift for:
  - `gain_by_breed_shed_type`
  - `weighing_category` on weight demographics
  - `weekly_gain` on weighing growth

## Live OCI Evidence

Direct API call for `2026-08-24..2026-08-31`, `sex=male` returned real data:

- resolved scanned animals: 180
- lump-sum animals: 340
- breed gain rows present
- weight band rows present
- `gain_by_breed_shed_type`: empty

The empty Shed-wise result is because OCI currently has no explicit elevated/crown/ground
shed metadata. I checked both data and schema:

- no location or shed profile text matching elevated/crown/ground/raised/floor terms
- no relevant public column like elevated, crown, ground, shed type, housing, or floor
  except unrelated health diagnosis housing columns

So the remaining gap is data classification, not the analytics aggregation or UI.

## Screenshots

- `/tmp/pr162-oci-refresh-general.png`
- `/tmp/pr162-oci-refresh-breed.png`
- `/tmp/pr162-oci-refresh-birth.png`
- `/tmp/pr162-oci-refresh-shed.png`
- `/tmp/pr162-oci-refresh-weight.png`
- `/tmp/pr162-oci-refresh-time.png`

## Checks

- `cd backend && go test ./internal/weighing/...`
- `cd apps/admin-web && npm run typecheck`
- `git diff --check`

