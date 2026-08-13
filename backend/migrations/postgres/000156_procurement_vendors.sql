-- +goose Up

-- The Procurement VENDOR REGISTER: every counterparty the farm buys from or contracts with.
--
-- Maintainer decision 2026-08-12: the register moves out of the "Vendors DB [Procurement]"
-- Google Sheet and into Postgres, which becomes canonical. The sheet is imported ONCE
-- (fixtures/procurement-vendors-2026-08-12/vendors.json, 307 rows exported 2026-08-12) and is
-- then NOT synced -- no importer job, no write-back, no two-way reconciliation. Anything that
-- still writes to that sheet (the Slack `vendor_start_entry` Apps Script logged on its
-- "Vendor Slack Debug" tab) is writing to a document nothing reads any more; retiring or
-- repointing it is separate, deliberately-scoped work.
--
-- WHY THIS IS NOT `orgs` / `parties`
-- ---------------------------------
-- `orgs` already carries org_type='vendor' (four placeholder rows from the baseline seed), but it
-- models a PARTY -- a legal counterparty with a status -- and holds no contact person, no phone, no
-- city, no commercial terms and no payment instrument. The register is an operational contact book:
-- the field team reads it to find who to call for sheep in Anantapur or transport out of
-- Vijayawada. Widening `orgs` with twenty nullable contact/commercial/banking columns would push
-- procurement's operational shape into the shared party model that identity, ownership and
-- commerce all read. This table owns the register; a later change may back-link a vendor to a
-- party via party_id when a vendor first transacts, which is why that column exists NULLABLE from
-- day one rather than being retro-fitted.
--
-- WHY RECORD TYPE IS FREE OF A CHECK CONSTRAINT
-- --------------------------------------------
-- The register spans 22 live record types in the exported data (Sheep Agent 89, Transport Agent 57,
-- Manure Agent 37, Feed Agent 31 ... down to Vet Doctor 1) against 35 declared in the sheet's
-- Validation tab -- 13 are authored-ahead vocabulary nobody has used yet (Plumber, Welder, Land
-- Agent, Tractor ...). The vocabulary is business-managed and grows without a deploy, so it lives
-- in procurement_vendor_catalog (below) as DATA, not in a CHECK. Per AGENTS.md, business-managed
-- dropdown vocabularies come from Postgres, never from backend-code literals.
--
-- STATUS is the one exception and DOES carry a CHECK: it drives whether a vendor is offered for new
-- business at all, so an unrecognised value is a correctness problem rather than a missing label.
-- 'banned' is declared here though the export contains none -- the sheet's Validation tab offers it,
-- and dropping it on import would silently remove the farm's only way to record "never buy here
-- again".

CREATE TABLE public.procurement_vendors (
    vendor_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    -- Identity. business_name is the only always-present name: 52 of 307 exported rows carry no
    -- contact person, so contact_person_name must stay nullable.
    record_type text NOT NULL,
    business_name text NOT NULL,
    contact_person_name text,
    phone_number text,

    -- Commercial context. Sparse by nature -- breed is populated on 49/307 rows (it is meaningful
    -- only for livestock record types), feed on 40/307, and the rest are filled in as a vendor is
    -- actually negotiated with. NOT NULL on any of these would make the import lie.
    breed text,
    feed text,
    status text NOT NULL,
    filtered_stock integer,
    price_per_goat numeric(12,2),
    ready_to_filtered text,
    eta_after_order_days integer,
    details text,

    -- Location. state is present on every exported row and is how the register is most often
    -- filtered; city is missing on 30 rows.
    state text NOT NULL,
    city text,

    -- Payment instruments. Populated on 9 of 307 rows today. Held behind a SEPARATE permission at
    -- the API layer (permissions.VendorFinanceRead) rather than a separate table: splitting the row
    -- would buy nothing at this size and would make every read a join, while the permission split is
    -- what actually withholds the values from a caller who may not see them.
    bank_name text,
    account_no text,
    ifsc_code text,
    upi_id text,
    pan_number text,

    comments text,

    -- Back-link to the shared party model, populated only once a vendor actually transacts.
    -- Deliberately nullable and unconstrained-by-default: the register is a contact book first.
    party_id uuid,

    -- Import provenance, so a row's origin stays answerable after the sheet is gone. NULL for a
    -- vendor created in admin-web.
    source_row integer,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,

    -- Optimistic concurrency for the edit drawer: an editor reads row_version and sends it back, so
    -- two people editing the same vendor cannot silently overwrite each other.
    row_version bigint DEFAULT 1 NOT NULL,

    CONSTRAINT procurement_vendors_pkey PRIMARY KEY (vendor_id),
    CONSTRAINT procurement_vendors_status_check
        CHECK (status = ANY (ARRAY['active'::text, 'inactive'::text, 'negotiating'::text, 'banned'::text])),
    CONSTRAINT procurement_vendors_business_name_not_blank CHECK (btrim(business_name) <> ''),
    CONSTRAINT procurement_vendors_record_type_not_blank CHECK (btrim(record_type) <> ''),
    CONSTRAINT procurement_vendors_state_not_blank CHECK (btrim(state) <> ''),
    -- Non-negative rather than positive: 0 filtered stock is a real, meaningful reading.
    CONSTRAINT procurement_vendors_filtered_stock_nonneg CHECK (filtered_stock IS NULL OR filtered_stock >= 0),
    CONSTRAINT procurement_vendors_eta_nonneg CHECK (eta_after_order_days IS NULL OR eta_after_order_days >= 0),
    CONSTRAINT procurement_vendors_price_nonneg CHECK (price_per_goat IS NULL OR price_per_goat >= 0),
    CONSTRAINT procurement_vendors_party_fk FOREIGN KEY (party_id) REFERENCES public.parties(party_id)
);

-- The register's natural key, and PHONE IS PART OF IT ON PURPOSE.
--
-- The obvious key -- (business_name, record_type, state) -- was checked against the export and is
-- WRONG: it collides on five row pairs, and four of those five are legitimate. One business
-- routinely has several people worth calling, each a row of their own:
--
--   Iffco Tokio General Insurance / TN / Coimbatore   -> Manoj 9354471192, Mr Kanna 9003386052
--   New Olog Logistics Private Ltd / MP / Bhopal      -> Namrata 8925868290, Roshan 8318937370
--   Irshad / MP / Bhopal                              -> two numbers for the same named agent
--   Suresh / AP / Anantapur                           -> 9959555636 and 8374730252
--
-- Adding city does not separate them (all four pairs share a city); adding the phone does. The
-- fifth pair -- 'Ajay Tomar', Transport Agent, MP, phone 6261962172 on both -- is the genuine
-- double-entry this constraint exists to stop, and the importer collapses it to the richer row.
--
-- So the key reads "the same person at the same business in the same state", which is what a
-- duplicate actually is here. The phone is compared as DIGITS ONLY because the sheet stores it
-- inconsistently ('97550 44183' carries a space, others do not), and a formatting difference must
-- not be allowed to defeat the constraint.
--
-- City is deliberately excluded: it is corrected often enough in this data ('Nellor' for Nellore,
-- 'Bellari' for Ballari) that a typo would defeat the key. record_type IS included, because one
-- business legitimately appears twice when it both agents goats and runs transport.
--
-- coalesce(...,'') rather than a bare NULL: Postgres treats NULLs as distinct in a unique index, so
-- without it any number of phone-less rows for one business would slip through. Exactly one
-- exported row has no phone, so the collapse to '' costs nothing today and closes the hole.
CREATE UNIQUE INDEX procurement_vendors_natural_uq
    ON public.procurement_vendors (
        tenant_id,
        lower(btrim(business_name)),
        record_type,
        state,
        coalesce(regexp_replace(phone_number, '\D', '', 'g'), '')
    );

-- Keyset pagination order for the vendor table: (business_name, vendor_id). vendor_id is the
-- tiebreaker so a page boundary cannot repeat or skip a row when two vendors share a name.
CREATE INDEX procurement_vendors_keyset_idx
    ON public.procurement_vendors (tenant_id, business_name, vendor_id);

-- Facet filters. Each is a low-cardinality equality predicate the list screen offers.
CREATE INDEX procurement_vendors_record_type_idx ON public.procurement_vendors (tenant_id, record_type, business_name, vendor_id);
CREATE INDEX procurement_vendors_status_idx ON public.procurement_vendors (tenant_id, status, business_name, vendor_id);
CREATE INDEX procurement_vendors_state_idx ON public.procurement_vendors (tenant_id, state, business_name, vendor_id);

-- SEARCH.
--
-- The search box must match a fragment anywhere in the name, contact or phone -- an operator types
-- "goat" and expects "Bhopal Goat And Agro", and types the last four digits of a number they have
-- on their phone. That is an infix LIKE, which is non-SARGable against an ordinary btree and is
-- exactly the anti-pattern make scale-guard blocks. AGENTS.md names the approved fix: a normalized
-- column plus a pg_trgm GIN index.
--
-- search_text is GENERATED, not maintained by application code, so it cannot drift from the columns
-- it summarises and cannot be forgotten on an UPDATE path added later.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE public.procurement_vendors
    ADD COLUMN search_text text GENERATED ALWAYS AS (
        lower(
            coalesce(business_name, '') || ' ' ||
            coalesce(contact_person_name, '') || ' ' ||
            coalesce(phone_number, '') || ' ' ||
            coalesce(city, '') || ' ' ||
            coalesce(record_type, '')
        )
    ) STORED;

CREATE INDEX procurement_vendors_search_trgm_idx
    ON public.procurement_vendors USING gin (search_text public.gin_trgm_ops);


-- The business-managed dropdown vocabularies behind the register's selects, imported from the
-- sheet's "Validation" tab and thereafter edited as data.
--
-- The imported vocabulary is the UNION of what Validation declared and what the DB rows actually
-- use: seven cities appear on real vendor rows but were never added to the sheet's list (BV Halli,
-- HD Kote, Hassan, Kalyandurg, Kochin, Malvali, Penukonda). Importing Validation alone would have
-- left those vendors holding a city their own edit form could not re-select, so the export unions
-- them in and this table receives the union.
CREATE TABLE public.procurement_vendor_catalog (
    tenant_id uuid NOT NULL,
    -- 'record_type' | 'breed' | 'state' | 'city' | 'status' | 'feed'
    kind text NOT NULL,
    value text NOT NULL,
    label text NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    -- A retired vocabulary entry stops being OFFERED for new rows but keeps rendering on the
    -- vendors that already carry it. Deleting the row instead would blank an existing vendor's
    -- field in the UI.
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT procurement_vendor_catalog_pkey PRIMARY KEY (tenant_id, kind, value),
    CONSTRAINT procurement_vendor_catalog_kind_check
        CHECK (kind = ANY (ARRAY['record_type'::text, 'breed'::text, 'state'::text, 'city'::text, 'status'::text, 'feed'::text])),
    CONSTRAINT procurement_vendor_catalog_value_not_blank CHECK (btrim(value) <> ''),
    CONSTRAINT procurement_vendor_catalog_label_not_blank CHECK (btrim(label) <> '')
);

CREATE INDEX procurement_vendor_catalog_kind_idx
    ON public.procurement_vendor_catalog (tenant_id, kind, is_active, sort_order, value);


-- `procurement_manager` -- the job role that runs the vendor register.
--
-- Maintainer decision 2026-08-12: vendor access is "leadership + a procurement role". Unlike
-- counts_approver (000108), this IS a job rather than a per-person authority: running the
-- procurement desk is somebody's role, and a future holder of that desk SHOULD inherit the
-- register. So it is granted per role, and the named individual is attached by granting them this
-- role -- not by adding vendor.* to an unrelated director job, which would widen it to every holder
-- of that job.
--
-- vertical_code is 'procurement', which already exists in org_verticals (baseline, sort_order 1).
-- That makes is_legacy = false legal here, unlike growth_director/feed_director/counts_approver
-- which had no matching vertical and had to be declared legacy to satisfy
-- org_role_catalog_vertical_or_legacy_check.
--
-- Without this row every grant INSERT fails: user_scope_grants_role_fk and
-- auth_pending_email_grants_role_fk are both FKs to org_role_catalog(role_key).
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES ('procurement_manager', 'manager', 'procurement', false, 'Procurement Manager', now())
ON CONFLICT (role_key) DO UPDATE
SET label = EXCLUDED.label,
    vertical_code = EXCLUDED.vertical_code;


-- +goose Down

DELETE FROM public.user_scope_grants WHERE role = 'procurement_manager';
DELETE FROM public.auth_pending_email_grants WHERE role = 'procurement_manager';
DELETE FROM public.org_role_catalog WHERE role_key = 'procurement_manager';

DROP TABLE IF EXISTS public.procurement_vendor_catalog;
DROP TABLE IF EXISTS public.procurement_vendors;
-- pg_trgm is deliberately NOT dropped: it is a database-wide extension and another feature may
-- have started depending on it since this migration ran.
