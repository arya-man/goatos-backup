-- +goose Up
-- seed-fixture-guard:ignore: per-person HRMS title authored on /people; no vaccination/HRMS seed fixture contract
--
-- WORKFORCE MEMBER TITLE (maintainer request 2026-09-11).
--
-- The Tasks "For" picker used to list assignees by NAME. The maintainer wants it to list
-- them by what they ARE -- CEO, COO, CTO, Preventive Care Director -- and reveal the person
-- underneath once a title is chosen. Nothing stored says that today: the four leadership
-- accounts are all `ceo_internal` / grade `cxo`, and the directors resolve only to a role
-- label. So the title is a per-person HRMS fact, edited on People / HRMS like the rest of
-- a person's profile:
--
--   * one row per person WITH a title; a person without one falls back to their
--     designation's catalog label ("Preventive Care Director", "Park Head"), so the picker
--     never shows a blank.
--   * free text, not a designation code: "Feed & Procurement Director" is one person's
--     title across two roles, and a title change must not move their access.
--
-- Its own table rather than a column on workforce_members: the roster row is seed-contract
-- schema (fixtures, validators, runbooks all describe it), and a display title is not part
-- of that contract. The seven rows below are the maintainer's own list for the current STG
-- leadership, keyed by login email so a re-seeded roster picks them up. A title already set
-- on /people outranks the seed (ON CONFLICT DO NOTHING).
CREATE TABLE IF NOT EXISTS public.workforce_member_titles (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  title               text NOT NULL CHECK (btrim(title) <> '' AND length(title) <= 80),
  updated_by          uuid,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  row_version         integer NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id, workforce_member_id),
  FOREIGN KEY (workforce_member_id)
    REFERENCES public.workforce_members (workforce_member_id) ON DELETE CASCADE
);

COMMENT ON TABLE public.workforce_member_titles IS
  'Per-person business title shown where a person is picked by what they are (the Tasks assignee picker). Edited on People / HRMS; absent means fall back to the designation label.';

INSERT INTO public.workforce_member_titles (tenant_id, workforce_member_id, title)
SELECT m.tenant_id, m.workforce_member_id, v.title
FROM public.workforce_members m
JOIN (VALUES
  ('manju@mesha.sg',                'CEO'),
  ('aryaman@mesha.sg',              'COO'),
  ('ravi@mesha.sg',                 'CTO'),
  ('chandrakanth119527@gmail.com',  'Preventive Care Director'),
  ('babureddy315@gmail.com',        'Breeding & Growth Director'),
  ('avishek@vgoats.com',            'Health Director'),
  ('hemant@vgoats.com',             'Feed & Procurement Director')
) AS v(email, title) ON lower(btrim(m.email)) = v.email
WHERE m.status = 'active'
ON CONFLICT (tenant_id, workforce_member_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.workforce_member_titles;
