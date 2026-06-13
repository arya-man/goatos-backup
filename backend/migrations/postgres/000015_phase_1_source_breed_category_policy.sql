-- +goose Up
UPDATE breeds
SET
  species = 'goat',
  status = 'active',
  review_notes = 'Business-confirmed Mesha goat breed/category from source Breed column; do not block passport creation from this label.',
  updated_at = now()
WHERE canonical_name = 'Anantapur Sheep';

INSERT INTO breeds (
  species,
  canonical_name,
  status,
  review_notes,
  created_at,
  updated_at
) VALUES (
  'goat',
  'Anantapur Sheep',
  'active',
  'Business-confirmed Mesha goat breed/category from source Breed column; do not block passport creation from this label.',
  now(),
  now()
)
ON CONFLICT (species, canonical_name) DO UPDATE
SET
  status = EXCLUDED.status,
  review_notes = EXCLUDED.review_notes,
  updated_at = now();

INSERT INTO breed_aliases (
  breed_id,
  alias,
  normalized_alias,
  source_system,
  created_at
)
SELECT
  b.breed_id,
  'Anantapur Sheep',
  'anantapur_sheep',
  'legacy_rfid_db',
  now()
FROM breeds b
WHERE b.species = 'goat'
  AND b.canonical_name = 'Anantapur Sheep'
ON CONFLICT (normalized_alias, source_system) DO UPDATE
SET
  breed_id = EXCLUDED.breed_id,
  alias = EXCLUDED.alias;

-- +goose Down
UPDATE breeds
SET
  species = 'sheep',
  status = 'review',
  review_notes = 'Source label is species/breed reference, not Goat Passport default species.',
  updated_at = now()
WHERE canonical_name = 'Anantapur Sheep';

DELETE FROM breed_aliases
WHERE normalized_alias = 'anantapur_sheep'
  AND source_system = 'legacy_rfid_db';
