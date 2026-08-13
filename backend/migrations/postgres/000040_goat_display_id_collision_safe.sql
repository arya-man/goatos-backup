-- +goose Up
-- Canonical goat inserts use next_goat_display_id(), while reviewed imports may carry explicit
-- G- display IDs. An import can therefore move the occupied high-water mark ahead of the sequence.
-- Resynchronize once, then keep allocation collision-safe by skipping any explicit ID already in
-- goats. The UNIQUE(display_id) constraint remains the final concurrency guard.
--
-- Initial-seed coupling: this changes only canonical display-ID allocation. Fresh seeds and source
-- fixtures keep their existing explicit IDs; no derived/read-model seed or source shape changes.
SELECT setval(
  'public.goat_display_id_seq',
  GREATEST(
    (SELECT last_value FROM public.goat_display_id_seq),
    COALESCE((
      SELECT max(substring(display_id FROM 3)::bigint)
      FROM public.goats
      WHERE display_id ~ '^G-[0-9]+$'
    ), 0)
  ),
  true
);

CREATE OR REPLACE FUNCTION public.next_goat_display_id()
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
  candidate text;
  sequence_value bigint;
BEGIN
  LOOP
    sequence_value := nextval('public.goat_display_id_seq');
    candidate := 'G-' || lpad(
      sequence_value::text,
      GREATEST(6, length(sequence_value::text)),
      '0'
    );

    EXIT WHEN NOT EXISTS (
      SELECT 1
      FROM public.goats g
      WHERE g.display_id = candidate
    );
  END LOOP;

  RETURN candidate;
END;
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION public.next_goat_display_id()
RETURNS text
LANGUAGE sql
AS $$
  SELECT 'G-' || lpad(v::text, GREATEST(6, length(v::text)), '0')
  FROM (SELECT nextval('public.goat_display_id_seq') AS v) s;
$$;
