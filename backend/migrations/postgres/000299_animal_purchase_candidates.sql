-- +goose Up
-- ANIMAL PURCHASES (maintainer decision 2026-09-13).
--
-- The procurement director records a purchase LOAD on the phone and then, one at a time, the
-- animals on offer in it -- what each is (species, sex, breed), roughly how old and heavy, how it
-- looks, and a video from the in-app camera. The CEO/CXO watches each video on admin-web and
-- ACCEPTS (we buy it) or REJECTS (we skip it). That decision is the whole stage: an accepted
-- animal is NOT written to goats, gets no RFID and joins no procurement_load. The tables below are
-- a CANDIDATE register, deliberately separate from procurement_loads / procurement_load_goats,
-- whose AddGoatToLoad creates a goats row on insert -- exactly the write the maintainer said not
-- to make yet. Promotion of accepted candidates into the herd is a later stage.
CREATE TABLE public.animal_purchase_loads (
  load_id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          uuid NOT NULL,
  load_ref           text NOT NULL,
  vendor_id          uuid NOT NULL,
  -- Denormalised at write so the list never joins the register and a renamed vendor does not
  -- rewrite history: the load was bought from the vendor as it was named that day.
  vendor_name        text NOT NULL,
  park_id            uuid,
  farm_label         text NOT NULL,
  expected_count     integer NOT NULL DEFAULT 0,
  notes              text NOT NULL DEFAULT '',
  status             text NOT NULL DEFAULT 'open',
  recorded_by        uuid,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  row_version        integer NOT NULL DEFAULT 1,
  idempotency_key    text NOT NULL,
  CONSTRAINT animal_purchase_loads_ref_check CHECK (btrim(load_ref) <> '' AND length(load_ref) <= 32),
  CONSTRAINT animal_purchase_loads_farm_check CHECK (farm_label IN ('CBE', 'CPT')),
  CONSTRAINT animal_purchase_loads_expected_check CHECK (expected_count >= 0),
  CONSTRAINT animal_purchase_loads_status_check CHECK (status IN ('open', 'closed')),
  CONSTRAINT animal_purchase_loads_ref_uq UNIQUE (tenant_id, load_ref),
  CONSTRAINT animal_purchase_loads_idem_uq UNIQUE (tenant_id, idempotency_key)
);

-- Keyset list: newest load first.
CREATE INDEX animal_purchase_loads_list_idx
  ON public.animal_purchase_loads (tenant_id, created_at DESC, load_id DESC);

CREATE TABLE public.animal_purchase_candidates (
  candidate_id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          uuid NOT NULL,
  load_id            uuid NOT NULL,
  -- Ordinal within the load ("Animal 7"), assigned under the load's row lock.
  seq_no             integer NOT NULL,
  species            text NOT NULL,
  sex                text NOT NULL,
  breed              text NOT NULL DEFAULT '',
  age_months         integer,
  weight_kg          numeric(6,2),
  condition          text NOT NULL,
  temp_tag           text NOT NULL DEFAULT '',
  notes              text NOT NULL DEFAULT '',
  -- The in-app-camera video, by proof reference (proof_artifacts.proof_id). Mandatory: a
  -- candidate nobody can look at cannot be decided on.
  video_proof_ref    text NOT NULL,
  decision           text NOT NULL DEFAULT 'pending',
  decided_by         uuid,
  decided_by_name    text NOT NULL DEFAULT '',
  decided_at         timestamptz,
  decision_note      text NOT NULL DEFAULT '',
  recorded_by        uuid,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  row_version        integer NOT NULL DEFAULT 1,
  idempotency_key    text NOT NULL,
  CONSTRAINT animal_purchase_candidates_load_fk
    FOREIGN KEY (load_id) REFERENCES public.animal_purchase_loads (load_id) ON DELETE CASCADE,
  CONSTRAINT animal_purchase_candidates_species_check CHECK (species IN ('goat', 'sheep')),
  CONSTRAINT animal_purchase_candidates_sex_check CHECK (sex IN ('male', 'female')),
  CONSTRAINT animal_purchase_candidates_condition_check CHECK (condition IN ('healthy', 'minor_concern', 'unwell')),
  CONSTRAINT animal_purchase_candidates_age_check CHECK (age_months IS NULL OR (age_months >= 0 AND age_months <= 240)),
  CONSTRAINT animal_purchase_candidates_weight_check CHECK (weight_kg IS NULL OR (weight_kg > 0 AND weight_kg < 500)),
  CONSTRAINT animal_purchase_candidates_decision_check CHECK (decision IN ('pending', 'accepted', 'rejected')),
  CONSTRAINT animal_purchase_candidates_decided_check
    CHECK ((decision = 'pending') = (decided_at IS NULL)),
  CONSTRAINT animal_purchase_candidates_video_check CHECK (btrim(video_proof_ref) <> ''),
  CONSTRAINT animal_purchase_candidates_seq_uq UNIQUE (tenant_id, load_id, seq_no),
  CONSTRAINT animal_purchase_candidates_idem_uq UNIQUE (tenant_id, idempotency_key)
);

-- The load's animal list (phone) and the per-load decision counts.
CREATE INDEX animal_purchase_candidates_load_idx
  ON public.animal_purchase_candidates (tenant_id, load_id, seq_no);
-- The CEO's review queue: what is still undecided, oldest first.
CREATE INDEX animal_purchase_candidates_decision_idx
  ON public.animal_purchase_candidates (tenant_id, decision, created_at, candidate_id);

-- +goose Down
DROP TABLE IF EXISTS public.animal_purchase_candidates;
DROP TABLE IF EXISTS public.animal_purchase_loads;
