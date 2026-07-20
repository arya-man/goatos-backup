-- +goose Up
-- Feed-direction ISSUES: the FROZEN, issued feed sheet and its lifecycle.
--
-- WHY A FORWARD MIGRATION (not an edit to 000003-000006):
-- the migrator is forward-only. 000003-000006 have already been applied and checksum-tracked on
-- dev/stg, so editing any of them would leave those databases silently missing everything below.
-- Same rule that produced 000002 through 000006.
--
-- ---------------------------------------------------------------------------
-- WHY THIS EXISTS -- Feed Direction stops being a live calculator
-- ---------------------------------------------------------------------------
-- Until now /feed-direction/preview LIVE-COMPUTED a sheet for ANY target_date on every request:
-- the projection is "live herd + approved-but-unexecuted shiftings" and nothing is dated into the
-- future, so asking for tomorrow, next week, or three months out all returned the same confident
-- number. Three defects followed: (1) tomorrow's sheet was viewable now as if it had been issued;
-- (2) a PAST sheet could not be retrieved, because nothing was ever stored; (3) the 14:00
-- correction the business runs had nothing to correct, because no issued artifact existed.
--
-- Feed for day D is produced on D-1, per (park, workflow), on the clock already stored in
-- feed_schedule_config (000004): direction_time ISSUES the sheet (generate once, freeze immutably),
-- correction_time AMENDS it (recompute, diff, persist the affected sheds only), transport_time
-- LOCKS it (no further change; later changes roll to the next feed day). This migration is the
-- durable record those three transitions write. The issue IS the operational-kernel
-- expected-process record: what sheet was promised, whether it was amended, and the evidence.
--
-- ---------------------------------------------------------------------------
-- 1. feed_direction_issues -- the issued sheet HEADER, one live row per (tenant, park, day, workflow)
-- ---------------------------------------------------------------------------
-- Per-workflow because 000004's dispatch clock is per-workflow: normal packing is directed at
-- 07:00 and the hand-entered experiment sheds at 14:00, so a park-day's full sheet is composed of
-- up to TWO issues that can be issued at different instants. The read path unions them.
--
-- The row is MUTATED IN PLACE across its lifecycle (issued -> amended -> locked): issued_at,
-- amended_at and locked_at accumulate on the one row, and amendment_count counts the corrections.
-- There is never more than one live issue per (tenant, park, feed_day, workflow) -- the partial
-- unique index below is that guarantee.
--
-- IDEMPOTENCY (AGENTS.md mandatory write-path contract). The worker re-runs the same issue/amend/
-- lock safely: idempotency_key is the stable operation identity ("issue:{tenant}:{park}:{day}:
-- {workflow}") and request_fingerprint == generation_input_fingerprint is the hash of the herd +
-- config the sheet was generated from. An EXACT re-issue (same key, same fingerprint) is a no-op
-- replay that returns the original and runs no side effects. A re-issue with a DIFFERENT fingerprint
-- is a legitimate fresh issue ONLY while state='issued' and nothing downstream has consumed the
-- sheet -- the service replaces the frozen rows in place. Once state is 'amended' or 'locked' a
-- plain re-issue with changed inputs is REJECTED: a correction or the transport lock has already
-- acted on the issued document, and changing it now must go through Amend, not a silent re-Issue.
CREATE TABLE feed_direction_issues (
    feed_direction_issue_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id                     uuid NOT NULL,
    feed_day                    date NOT NULL,
    workflow                    text NOT NULL,
    state                       text NOT NULL DEFAULT 'issued',
    -- Lifecycle instants. Business meaning is Asia/Kolkata (AGENTS.md); the service stamps them
    -- from a business-calendar clock, never from SQL now(). issued_at is always present; the other
    -- two fill in as the sheet is amended and then locked.
    issued_at                   timestamptz NOT NULL,
    amended_at                  timestamptz,
    locked_at                   timestamptz,
    -- Hash of the herd + config the sheet was generated from. A re-issue with an identical
    -- fingerprint is a no-op; an amend compares this against a fresh recompute to tell whether
    -- anything actually changed before writing a single row.
    generation_input_fingerprint text NOT NULL,
    -- The idempotency envelope. request_fingerprint == generation_input_fingerprint by construction
    -- (the request that produces an issue IS its generation inputs); it is stored under the contract
    -- name too so the replay/conflict logic reads the same as every other module's write path.
    idempotency_key             text NOT NULL,
    request_fingerprint         text NOT NULL,
    -- Provenance, mirroring the counts projection snapshots' source_contract/version columns.
    source_contract             text NOT NULL,
    source_contract_version     text NOT NULL,
    amendment_count             integer NOT NULL DEFAULT 0,
    generated_by                text NOT NULL,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_direction_issues_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_direction_issues_workflow_check CHECK (workflow = ANY (ARRAY['normal'::text, 'experiment'::text])),
    CONSTRAINT feed_direction_issues_state_check CHECK (state = ANY (ARRAY['issued'::text, 'amended'::text, 'locked'::text])),
    -- The lifecycle instants must exist for the state that implies them: an 'amended' sheet has an
    -- amended_at, a 'locked' sheet has a locked_at. A sheet locked without an intervening amend has
    -- a null amended_at, which is correct.
    CONSTRAINT feed_direction_issues_amended_shape_check CHECK (state <> 'amended' OR amended_at IS NOT NULL),
    CONSTRAINT feed_direction_issues_locked_shape_check CHECK (state <> 'locked' OR locked_at IS NOT NULL),
    CONSTRAINT feed_direction_issues_amendment_count_check CHECK (amendment_count >= 0),
    CONSTRAINT feed_direction_issues_fingerprint_check CHECK (btrim(generation_input_fingerprint) <> ''),
    CONSTRAINT feed_direction_issues_idem_check CHECK ((btrim(idempotency_key) <> ''::text) AND (btrim(request_fingerprint) <> ''::text)),
    CONSTRAINT feed_direction_issues_source_check CHECK ((btrim(source_contract) <> ''::text) AND (btrim(source_contract_version) <> ''::text)),
    CONSTRAINT feed_direction_issues_generated_by_check CHECK (btrim(generated_by) <> ''::text)
);

-- ONE LIVE ISSUE per (tenant, park, feed_day, workflow). Partial on the live state set so that a
-- future archival/superseded state can be added without colliding with this guarantee; today every
-- legal state is live, so this is effectively the natural key.
CREATE UNIQUE INDEX feed_direction_issues_live_uidx
    ON feed_direction_issues (tenant_id, park_id, feed_day, workflow)
    WHERE state IN ('issued', 'amended', 'locked');

-- THE idempotency index. Its violation is how a concurrent duplicate is detected: the second
-- transaction's INSERT fails here, re-reads the committed row, and returns the original result
-- instead of issuing twice.
CREATE UNIQUE INDEX feed_direction_issues_idempotency_uidx
    ON feed_direction_issues (tenant_id, idempotency_key);

-- Serving read: resolve every workflow's issue for one park + feed day in one indexed lookup. The
-- read path unions the (at most two) rows and reports their aggregated lifecycle.
CREATE INDEX feed_direction_issues_serve_idx
    ON feed_direction_issues (tenant_id, park_id, feed_day);

COMMENT ON TABLE feed_direction_issues IS
  'The issued feed-direction sheet header, one live row per (tenant, park, feed_day, workflow), mutated in place across issued -> amended -> locked. The durable operational-kernel record of what feed sheet was promised for a day and how it changed. Per-workflow because the 000004 dispatch clock is per-workflow.';
COMMENT ON COLUMN feed_direction_issues.state IS
  'issued (frozen on generation) | amended (a correction batch changed some sheds) | locked (transport cutoff passed; no further change). Mutated in place; the *_at columns accumulate.';
COMMENT ON COLUMN feed_direction_issues.generation_input_fingerprint IS
  'Hash of the herd + config the sheet was generated from. Equal fingerprint => a re-issue is a no-op and an amend has nothing to change. Stored so change detection never re-reads the whole sheet.';


-- ---------------------------------------------------------------------------
-- 2. feed_direction_issue_rows -- the FROZEN sheet, the WHOLE generated scope
-- ---------------------------------------------------------------------------
-- The complete generated scope for the issue, NOT a page: past reads and pagination both serve
-- STORED rows, so the whole park-day must be here. One row per generated CELL -- (grain, session,
-- feed_item) -- which is the same grain the live DirectionRow.Items carries, denormalized flat so
-- the read path can reconstruct the exact DirectionRows and re-run the tested whole-scope summary.
--
-- BLOCKED-VS-ZERO IS PRESERVED STRUCTURALLY, EXACTLY AS THE GENERATOR ENCODES IT. quantity_kg is
-- NULL if and only if the cell is blocked (no authored ration); a stored 0.000 is an AUTHORED zero
-- (milk-fed kids), a real feeding instruction. The two are opposite states with opposite
-- consequences and the CHECK below makes them un-collapsible at the schema level: a blocked cell
-- MUST carry a reason code and no quantity, a resolved cell MUST carry a quantity and no reason.
--
-- THE GRAIN DISCRIMINATOR IS IN THE NATURAL KEY. A shed can hold several ration grains (two breeds
-- sharing a ration group, e.g. Beetal + Sojat), so (shed_id, session_no, feed_item) alone is NOT
-- unique within an issue. shed_tag_key and breed_key -- generated by the same feed_config_norm()
-- the generator groups grains by -- complete the key so multi-grain sheds never collide.
CREATE TABLE feed_direction_issue_rows (
    feed_direction_issue_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   uuid NOT NULL REFERENCES tenants (tenant_id),
    feed_direction_issue_id     uuid NOT NULL REFERENCES feed_direction_issues (feed_direction_issue_id) ON DELETE CASCADE,
    park_id                     uuid NOT NULL,
    park_label                  text NOT NULL,
    shed_id                     uuid NOT NULL,
    shed_label                  text NOT NULL,
    -- The authored tag the animals resolved onto (raw source text on a blocked row). Its norm key
    -- matches the grain group key the generator used, which is what makes the natural key exact.
    shed_tag                    text NOT NULL DEFAULT '',
    shed_tag_key                text GENERATED ALWAYS AS (feed_config_norm(shed_tag)) STORED,
    breed                       text NOT NULL DEFAULT '',
    breed_key                   text GENERATED ALWAYS AS (feed_config_norm(breed)) STORED,
    ration_group                text NOT NULL DEFAULT '',
    experiment_arm              text NOT NULL DEFAULT '',
    session_no                  integer NOT NULL,
    session_label               text NOT NULL DEFAULT '',
    head_count                  bigint NOT NULL,
    -- True for the experiment workflow, whose authored kg is already a shed total: head_count must
    -- never be multiplied into the quantity. Carried through so no reader or rollup ever does.
    head_count_informational    boolean NOT NULL,
    workflow                    text NOT NULL,
    feed_item_label             text NOT NULL,
    feed_item_key               text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    -- NULL IFF BLOCKED. A stored 0.000 is an authored zero, not a gap. See the CHECK below.
    quantity_kg                 numeric(12, 3),
    -- The authored rate / multiplier this quantity came from, echoed for the operator. NULL on a
    -- blocked cell and on every experiment cell (which authors absolute kg with no per-head rate).
    grams_per_head              numeric(12, 3),
    shed_factor                 numeric(8, 4),
    -- Set IFF blocked, mirroring domain.BlockedReason: a machine-stable code plus the operator
    -- sentence that names the exact missing coordinate.
    blocked_reason_code         text,
    blocked_reason_detail       text,
    -- The (grain, session) session total, summing RESOLVED cells only -- denormalized onto every
    -- cell of the grain so a row reconstructs without a second pass. Partial when the row is blocked.
    session_total_kg            numeric(12, 3) NOT NULL,
    overdue_pending             boolean NOT NULL,
    -- Generation order, so the frozen sheet reconstructs byte-for-byte: row_seq orders the grain
    -- rows, item_seq orders the cells within a row.
    row_seq                     integer NOT NULL,
    item_seq                    integer NOT NULL,
    -- An amendment marks ONLY the cells it changed, so the UI can show exactly what moved.
    amended                     boolean NOT NULL DEFAULT false,
    amended_at                  timestamptz,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_direction_issue_rows_workflow_check CHECK (workflow = ANY (ARRAY['normal'::text, 'experiment'::text])),
    -- BLOCKED-VS-ZERO, enforced structurally: blocked iff no quantity and a reason present; resolved
    -- iff a quantity is present and no reason. A stored 0.000 is therefore necessarily resolved.
    CONSTRAINT feed_direction_issue_rows_blocked_shape_check CHECK (
        (quantity_kg IS NULL AND blocked_reason_code IS NOT NULL)
        OR (quantity_kg IS NOT NULL AND blocked_reason_code IS NULL)
    ),
    CONSTRAINT feed_direction_issue_rows_seq_check CHECK (row_seq >= 0 AND item_seq >= 0),
    CONSTRAINT feed_direction_issue_rows_amended_shape_check CHECK (amended = false OR amended_at IS NOT NULL)
);

-- Natural key: the grain cell inside an issue. shed_tag_key + breed_key are the grain discriminator
-- that keeps a multi-grain shed's identically-named feed items from colliding.
CREATE UNIQUE INDEX feed_direction_issue_rows_natural_key_uidx
    ON feed_direction_issue_rows (tenant_id, feed_direction_issue_id, shed_id, session_no, shed_tag_key, breed_key, feed_item_key);

-- Serving read: load one issue's whole scope in generation order for reconstruction, and narrow to
-- a shed for the shed filter. Ordered so the frozen sheet comes back exactly as it was written.
CREATE INDEX feed_direction_issue_rows_serve_idx
    ON feed_direction_issue_rows (tenant_id, feed_direction_issue_id, shed_id, row_seq, item_seq);

COMMENT ON TABLE feed_direction_issue_rows IS
  'The FROZEN feed-direction sheet: the whole generated scope for an issue (not a page), one row per (grain, session, feed_item) cell. Read paths serve these stored rows; the whole-scope summary is recomputed over them so it stays invariant to page size. Blocked-vs-zero is preserved: quantity_kg NULL iff blocked.';
COMMENT ON COLUMN feed_direction_issue_rows.quantity_kg IS
  'The frozen packable quantity in kg. NULL IFF blocked (no authored ration -- surfaced as a gap); a stored 0.000 is an AUTHORED zero and a real feeding instruction. The blocked_shape CHECK makes the two un-collapsible.';
COMMENT ON COLUMN feed_direction_issue_rows.amended IS
  'True on a cell an amendment changed. An amend marks only the rows it moved, so the UI can show what a correction did without diffing the whole sheet.';


-- +goose Down
-- Reversible: drop the child rows then the header. Nothing outside this migration references either,
-- and feed_config_norm (owned by 000003) is untouched.
DROP TABLE IF EXISTS feed_direction_issue_rows;
DROP TABLE IF EXISTS feed_direction_issues;
