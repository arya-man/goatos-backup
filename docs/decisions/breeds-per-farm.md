# Breeds are per farm, and are added on Configuration

Maintainer decision, 2026-09-25. Supersedes the 2026-09-18 note that made Configuration >
Breeds read-only "until breeds are tenant-scoped".

## What changed

- `breeds` and `breed_aliases` carry `tenant_id` (migration `000433_breeds_are_per_farm`).
  Uniqueness is `(tenant_id, species, canonical_name)`.
- Configuration > Items & settings > Breeds is editable like parks, pens and species: add,
  rename, archive, delete, and a tab in the bulk workbook right after Species.
- Every reader lists or matches breeds for the caller's farm only: web pickers (adminui),
  the phone birth form (`/app/counts/breeds`), Sales products' breeds and sellable species,
  Counts breed aliases, and the census breed correction.
- Register animal (web) picks the breed from a dropdown of the farm's active breeds of the
  chosen species (`herd_breeds` option group; each option's `group` is its species code).

## Why

There was no way to add a breed anywhere. The register was read-only because the table was
product-wide, and Register animal only stored `goats.breed` text, which never reached Sales,
the web pickers or a sale product's breed list.

## How the migration keeps animals safe

- The oldest tenant keeps every existing row and its `breed_id`, so `goats`, `breed_aliases`,
  `shifting_event_impacts`, `count_base_anchors` and `count_projection_snapshot_rows` are
  untouched for it. STG holds one tenant.
- Any other tenant gets its own copies and its own references are repointed to them.
- A breed the herd carries as text but the list lacks is added to that farm's list.
- The Down refuses once more than one tenant owns breeds.

## Rules that follow

- A rename also renames animals that name the breed only as text (`breed_id` NULL), which is
  how Register animal and phone births record it.
- An archived breed is not offered for a new animal; the herd filter keeps it so old animals
  can still be found.
- A new farm starts with an empty breed list and adds its own.

Pinned by `TestBreedsAreEachFarmsOwnList` (real Postgres) and
`TestRegisterAnimalBreedsCarryTheirSpecies`.
