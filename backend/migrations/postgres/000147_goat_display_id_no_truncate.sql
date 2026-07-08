-- +goose Up
-- next_goat_display_id() used lpad(nextval::text, 6), which TRUNCATES once the
-- sequence passes 999,999: lpad('1000000', 6) = '100000', so G-1000000 collided
-- with G-100000 and INSERTs failed at 1M+ goats (goats_display_id_unique),
-- breaking the million-animal mandate.
--
-- Fix: zero-pad to a MINIMUM of 6 digits via lpad to GREATEST(6, len), so values
-- up to 999,999 stay 6 digits (G-000001 .. G-999999) and larger values grow with
-- no truncation (G-1000000, G-1000001, ...). Matches the schema's own
-- goats_display_id_format_check (^G-[0-9]{6,}$). (to_char(...,'FM000000') is NOT
-- usable here: Postgres returns '######' when the value overflows the mask width.)
-- nextval is read once inside the subquery to avoid a double increment.
CREATE OR REPLACE FUNCTION next_goat_display_id()
RETURNS text
LANGUAGE sql
AS $$
  SELECT 'G-' || lpad(v::text, GREATEST(6, length(v::text)), '0')
  FROM (SELECT nextval('goat_display_id_seq') AS v) s;
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION next_goat_display_id()
RETURNS text
LANGUAGE sql
AS $$
  SELECT 'G-' || lpad(nextval('goat_display_id_seq')::text, 6, '0');
$$;
