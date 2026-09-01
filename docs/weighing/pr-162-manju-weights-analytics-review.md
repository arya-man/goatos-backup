# PR 162 Weights Analytics Review

Reviewed locally against OCI via `127.0.0.1:15432`, with backend on
`127.0.0.1:18080` and admin-web on `127.0.0.1:3319`.

## Manju Requirement Map

- General: covered.
- Breed-wise: covered. Shows daily gain and average weight per breed.
- Birth-wise: covered. Shows farm-born vs purchased average daily gain per breed.
- Shed-wise: code now targets the requested comparison: elevated shed vs ground shed,
  grouped by breed. It no longer renders the old per-shed leaderboard. Partitions are
  classified from their parent shed name when needed.
- Weight-wise: covered as an extra useful tab. Bands latest weights and shows growth per band.
- Time-wise: covered. Shows last 12 weeks plus breed-wise weekly trend.

## Fixes Added In This Review

- Added `weighing_category` support to `/weighing/weight-demographics`.
- Enabled the Weighing filter only on tabs where the API can honestly narrow the data:
  Breed-wise, Birth-wise, Shed-wise, and Weight-wise.
- Added backend response field `gain_by_breed_shed_type`.
- Replaced Shed-wise frontend with elevated vs ground grouped bars per breed.
- Added the review-note shed classification until this becomes first-class shed metadata:
  - Ground sheds: Gandhi, Castro, Ho Chi Minh, Old Yashoda / Yashoda Old.
  - Elevated sheds: Mandela, Godel, Sumathi, Yashoda, New Yashoda / Yashoda New.
- Fixed an origin-filter leak in whole-shed weight/gain band inputs.
- Added the same Download drawer to `/weighing/analytics` as `/weighing/weights`.
- Updated the CSV export path so the file follows the active page filters for sex,
  origin, and weighing mode, instead of exporting a broader population than the screen.
- Set the shared Weights / Weights analytics default period to start from 2026-08-03,
  the first dense/proper goatos-stg weighing history, through the latest weighing date.
- Fixed OpenAPI/generated TypeScript contract drift for:
  - `gain_by_breed_shed_type`
  - `weighing_category` on weight demographics
  - `sex`, `origin`, and `weighing_category` on the CSV export
  - `weekly_gain` on weighing growth

## Live OCI Evidence

Direct API call for `2026-08-24..2026-08-31`, `sex=male` returned real data:

- resolved scanned animals: 180
- lump-sum animals: 340
- breed gain rows present
- weight band rows present
- `gain_by_breed_shed_type`: empty

The initial empty Shed-wise result happened because OCI had no explicit elevated/ground
shed metadata. I checked both data and schema:

- no location or shed profile text matching elevated/ground/raised/floor terms
- no relevant public column like elevated, crown, ground, shed type, housing, or floor
  except unrelated health diagnosis housing columns

The current implementation therefore uses the review-note names above as a fallback, while
still preferring explicit metadata when it exists. Do not remove that fallback unless the
same mapping is migrated into DB-backed shed metadata.

## Screenshots

- `/tmp/pr162-oci-refresh-general.png`
- `/tmp/pr162-oci-refresh-breed.png`
- `/tmp/pr162-oci-refresh-birth.png`
- `/tmp/pr162-oci-refresh-shed.png`
- `/tmp/pr162-oci-shed-ground-after-mapping.png`
- `/tmp/pr162-analytics-download-final.png`
- `/tmp/pr162-analytics-default-aug3-final.png`
- `/tmp/pr162-oci-refresh-weight.png`
- `/tmp/pr162-oci-refresh-time.png`

## Checks

- `cd backend && go test ./internal/weighing/...`
- `cd backend && go test -count=1 ./internal/weighing/...`
- `cd apps/admin-web && npm run typecheck`
- `git diff --check`
