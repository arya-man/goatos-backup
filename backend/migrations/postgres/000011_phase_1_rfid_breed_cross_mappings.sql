-- +goose Up
INSERT INTO breeds (
  species,
  canonical_name,
  status,
  review_notes,
  created_at,
  updated_at
) VALUES
  ('goat', 'Sirohi', 'active', 'Approved Phase 1 RFID source goat breed.', now(), now()),
  ('goat', 'Beetal x Malai', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now()),
  ('goat', 'Beetal x Sojat', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now()),
  ('goat', 'Boer x Beetal', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now()),
  ('goat', 'Boer x Malai', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now()),
  ('goat', 'Boer x Sirohi', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now()),
  ('goat', 'Boer x Sojat', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now()),
  ('goat', 'Malai x Sojat', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', now(), now())
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
  approved_alias.alias,
  approved_alias.normalized_alias,
  'legacy_rfid_db',
  now()
FROM (
  VALUES
    ('Sirohi', 'Sirohi', 'sirohi'),
    ('Beetal x Malai', 'Beetal x Malai', 'beetal_x_malai'),
    ('Beetal x Malai', 'Malai x Beetal', 'malai_x_beetal'),
    ('Beetal x Sojat', 'Beetal x Sojat', 'beetal_x_sojat'),
    ('Boer x Beetal', 'Boer x Beetal', 'boer_x_beetal'),
    ('Boer x Malai', 'Boer x Malai', 'boer_x_malai'),
    ('Boer x Sirohi', 'Boer x Sirohi', 'boer_x_sirohi'),
    ('Boer x Sojat', 'Boer x Sojat', 'boer_x_sojat'),
    ('Malai x Sojat', 'Malai x Sojat', 'malai_x_sojat'),
    ('Malai x Sojat', 'Sojat x Malai', 'sojat_x_malai')
) AS approved_alias(canonical_name, alias, normalized_alias)
JOIN breeds b
  ON b.species = 'goat'
 AND b.canonical_name = approved_alias.canonical_name
ON CONFLICT (normalized_alias, source_system) DO UPDATE
SET
  breed_id = EXCLUDED.breed_id,
  alias = EXCLUDED.alias;

-- +goose Down
DELETE FROM breed_aliases
WHERE source_system = 'legacy_rfid_db'
  AND normalized_alias IN (
    'sirohi',
    'beetal_x_malai',
    'malai_x_beetal',
    'beetal_x_sojat',
    'boer_x_beetal',
    'boer_x_malai',
    'boer_x_sirohi',
    'boer_x_sojat',
    'malai_x_sojat',
    'sojat_x_malai'
  );

DELETE FROM breeds b
WHERE b.species = 'goat'
  AND b.canonical_name IN (
    'Sirohi',
    'Beetal x Malai',
    'Beetal x Sojat',
    'Boer x Beetal',
    'Boer x Malai',
    'Boer x Sirohi',
    'Boer x Sojat',
    'Malai x Sojat'
  )
  AND NOT EXISTS (
    SELECT 1
    FROM goats g
    WHERE g.breed_id = b.breed_id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM breed_aliases ba
    WHERE ba.breed_id = b.breed_id
  );
