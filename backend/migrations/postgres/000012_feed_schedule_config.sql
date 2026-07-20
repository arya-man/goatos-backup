-- +goose Up
-- Feed schedule configuration: the CLOCK that drives daily feed dispatch, plus the write ledger
-- that makes the authored feed config editable from the app under the mandatory idempotency
-- contract.
--
-- WHY A FORWARD MIGRATION (not an edit to 000003):
-- the migrator is forward-only. 000003 has already been applied and checksum-tracked on dev, so
-- editing it would leave those databases silently missing everything below. Same rule that
-- produced 000002 and 000003.
--
-- ---------------------------------------------------------------------------
-- 1. feed_schedule_config -- WHEN the day's feed direction happens
-- ---------------------------------------------------------------------------
-- 000003 answered "how much"; this answers "when". One row per (tenant, park, workflow) carries
-- three clock times:
--
--   direction_time   -- when the day's packing direction is ISSUED to the shed.
--   correction_time  -- when emergency-shifting corrections are BATCHED and reissued.
--   transport_time   -- the deadline after which a correction can no longer reach the shed.
--
-- WHY THIS IS PER-WORKFLOW AND NOT ONE GLOBAL SETTING
-- ---------------------------------------------------
-- The 'normal' and 'experiment' workflows genuinely run on different clocks: normal packing is
-- directed first thing in the morning (07:00), while experiment sheds are hand-entered and are
-- directed in the afternoon (14:00). A single tenant- or park-global direction_time would have to
-- pick one of the two, and whichever it picked would issue the other workflow's direction at the
-- wrong hour every single day. So `workflow` is part of the natural key, not a filter column.
--
-- WHY CORRECTIONS ARE BATCHED AT A FIXED TIME, NOT FIRED ON APPROVAL
-- ------------------------------------------------------------------
-- MAINTAINER DECISION: an emergency shifting approved during the day does NOT immediately reissue
-- that shed's direction. Corrections accumulate and are reissued ONCE, at correction_time (14:00).
--
-- The alternative -- fire-on-approval -- was rejected because it races: three approvals in one
-- morning produce three amended directions for the same shed, arriving in whatever order the
-- consumer happens to process them, and the shed staff cannot tell which sheet is current. One
-- amended direction per shed per day is unambiguous by construction. correction_time is therefore
-- a business decision stored as config, not a scheduling implementation detail.
--
-- transport_time is the point past which a correction is pointless: the feed has left. It is
-- NULLABLE because a park that has not yet declared its transport cutoff has no honest value for
-- it, and inventing one would make a late correction look deliverable. NULL means "no declared
-- cutoff", which the read path must treat as "unknown", never as "no deadline".
--
-- TIME SEMANTICS -- these are INDIA BUSINESS CALENDAR (Asia/Kolkata) LOCAL times
-- ------------------------------------------------------------------------------
-- Per AGENTS.md, every business meaning derived from an instant converts to Asia/Kolkata first;
-- UTC never defines a Goat OS business day. These columns are `time` (time WITHOUT time zone) and
-- carry NO offset on purpose. 07:00 means seven in the morning at the park, on whatever date the
-- reader is scheduling, forever -- it is a recurring wall-clock rule, not an instant.
--
-- Storing `timetz` or a UTC-shifted 01:30 would be actively wrong here: it would bind a recurring
-- business rule to a fixed offset, and it would make the stored value unreadable to the operator
-- who authored "7 AM". The consumer combines (business date, this local time, Asia/Kolkata) to get
-- the instant. Do not add a UTC offset column.
CREATE TABLE feed_schedule_config (
    feed_schedule_config_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id                 uuid NOT NULL,
    workflow                text NOT NULL,
    direction_time          time NOT NULL,
    correction_time         time NOT NULL,
    transport_time          time,
    valid_from              date NOT NULL DEFAULT CURRENT_DATE,
    valid_to                date,
    created_by              uuid,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_schedule_config_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_schedule_config_workflow_check CHECK (workflow = ANY (ARRAY['normal'::text, 'experiment'::text])),
    -- A correction amends a direction that was already issued, so it cannot precede it. Equality is
    -- legal and is the live experiment case (direction 14:00, correction 14:00): the first
    -- direction of the day already carries that day's approved corrections.
    CONSTRAINT feed_schedule_config_correction_order_check CHECK (correction_time >= direction_time),
    -- Transport is the deadline a correction races; a cutoff before the correction batch would make
    -- every correction dead on arrival.
    CONSTRAINT feed_schedule_config_transport_order_check CHECK (transport_time IS NULL OR transport_time >= correction_time),
    CONSTRAINT feed_schedule_config_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- Natural key including valid_from: one authored clock per (park, workflow) per effective date.
CREATE UNIQUE INDEX feed_schedule_config_natural_key_uidx
    ON feed_schedule_config (tenant_id, park_id, workflow, valid_from);

-- At most ONE open (current) clock per (park, workflow). Two open rows would make "when do we issue
-- today's direction" ambiguous, and the dispatcher would pick one arbitrarily.
CREATE UNIQUE INDEX feed_schedule_config_open_row_uidx
    ON feed_schedule_config (tenant_id, park_id, workflow)
    WHERE valid_to IS NULL;

-- Hot read: the dispatcher resolves one (park, workflow) clock per tick. Covering, so the current
-- row is answered from the index.
CREATE INDEX feed_schedule_config_current_lookup_idx
    ON feed_schedule_config (tenant_id, park_id, workflow)
    INCLUDE (direction_time, correction_time, transport_time)
    WHERE valid_to IS NULL;

-- As-of read: the same lookup for a historical business date (audit / back-dated recompute).
CREATE INDEX feed_schedule_config_asof_lookup_idx
    ON feed_schedule_config (tenant_id, park_id, workflow, valid_from DESC);

COMMENT ON TABLE feed_schedule_config IS
  'Per (tenant, park, workflow) feed dispatch clock: when the day''s direction is issued, when emergency-shifting corrections are batched and reissued, and the transport cutoff after which a correction cannot land. Per-workflow because normal (07:00) and experiment (14:00) genuinely differ. Effective-dated.';
COMMENT ON COLUMN feed_schedule_config.workflow IS
  'Which dispatch workflow this clock governs: ''normal'' (grid-computed sheds) or ''experiment'' (hand-entered sheds). Part of the natural key -- the two run on different cutoffs.';
COMMENT ON COLUMN feed_schedule_config.direction_time IS
  'LOCAL Asia/Kolkata wall-clock time the day''s packing direction is issued. No UTC offset is stored: this is a recurring business-calendar rule, not an instant. Combine with the business date in Asia/Kolkata to get the instant.';
COMMENT ON COLUMN feed_schedule_config.correction_time IS
  'LOCAL Asia/Kolkata wall-clock time at which approved emergency-shifting corrections are BATCHED and the amended direction is reissued. Maintainer decision: batched at a fixed time rather than fired on approval, so a shed receives at most one amended direction per day instead of several racing ones.';
COMMENT ON COLUMN feed_schedule_config.transport_time IS
  'LOCAL Asia/Kolkata wall-clock deadline after which a correction can no longer reach the shed. NULLABLE: NULL means the park has not declared a cutoff and must read as UNKNOWN, never as "no deadline".';
COMMENT ON COLUMN feed_schedule_config.valid_to IS
  'NULL = currently in force. A clock change closes this row and inserts a new one; it does not UPDATE the times in place.';


-- ---------------------------------------------------------------------------
-- 2. feed_config_write_log -- the idempotency + audit ledger for authored edits
-- ---------------------------------------------------------------------------
-- 000003's config tables are now EDITABLE from the app (/feed-config/* write routes), and
-- AGENTS.md makes idempotency a mandatory write-path contract: every mutating endpoint persists a
-- client idempotency key AND a request fingerprint in the SAME transaction as its side effects,
-- returns the original result for an exact replay without rerunning them, and rejects a
-- same-key/different-payload replay.
--
-- WHY A LEDGER TABLE RATHER THAN idempotency_key COLUMNS ON EACH CONFIG TABLE
-- ---------------------------------------------------------------------------
-- counts carries idempotency_key/request_fingerprint on the written row itself (see
-- count_projection_exception_resolutions), which works because one write produces exactly one new
-- row. An effective-dated config edit does not: superseding a rate CLOSES an existing row and
-- INSERTS a new one, so the write's identity spans two rows and belongs to neither. Hanging the
-- key off the new row would also lose the key entirely for the "unchanged" outcome, which writes
-- no row at all and must still replay identically.
--
-- So the ledger records the WRITE, not the row: one entry per accepted authored edit, carrying the
-- key, the fingerprint, what the edit did, and which rows it produced/closed. That makes replay a
-- single indexed lookup and gives the config surface an audit trail of who changed what and when,
-- which the config tables' own created_by cannot express for a supersede.
--
-- The ledger row is written INSIDE the same transaction as the close/insert. There is no
-- "best-effort afterwards" path: an entry that exists is proof the side effects committed.
CREATE TABLE feed_config_write_log (
    feed_config_write_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants (tenant_id),
    -- Which authored surface was edited. Part of the audit record, NOT of the idempotency key:
    -- uniqueness is (tenant, key) alone, so one client key can never mean two different writes.
    write_kind           text NOT NULL,
    idempotency_key      text NOT NULL,
    request_fingerprint  text NOT NULL,
    -- What the edit actually did, using the same vocabulary as seed-feed-ration's reconciliation:
    --   inserted   -- no open row existed; a first authored value was created.
    --   superseded -- an open row authored on an EARLIER day was closed and a new one opened.
    --   corrected  -- an open row authored TODAY was corrected in place (a same-day re-author
    --                 cannot be given a window without violating valid_to > valid_from).
    --   unchanged  -- the authored value already matched; no row was written.
    outcome              text NOT NULL,
    -- The row now in force after this write. NULL only for 'unchanged' writes that matched a row
    -- the caller did not address by id.
    result_row_id        uuid,
    -- The row this write closed, for 'superseded'. NULL otherwise -- this is what makes the
    -- effective-dated history walkable from the ledger.
    superseded_row_id    uuid,
    -- The business date (Asia/Kolkata) the edit took effect on -- the new row's valid_from and the
    -- closed row's valid_to. Stored so the ledger is readable without joining the config tables.
    effective_from       date NOT NULL,
    actor_ref            text NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_config_write_log_kind_check CHECK (write_kind = ANY (ARRAY['ration_rate'::text, 'shed_factor'::text, 'schedule_config'::text])),
    CONSTRAINT feed_config_write_log_outcome_check CHECK (outcome = ANY (ARRAY['inserted'::text, 'superseded'::text, 'corrected'::text, 'unchanged'::text])),
    CONSTRAINT feed_config_write_log_idem_check CHECK ((btrim(idempotency_key) <> ''::text) AND (btrim(request_fingerprint) <> ''::text)),
    CONSTRAINT feed_config_write_log_actor_check CHECK (btrim(actor_ref) <> ''::text),
    -- A supersede must name BOTH ends of the window it created, or the history it claims to
    -- preserve cannot be reconstructed.
    CONSTRAINT feed_config_write_log_supersede_shape_check CHECK (
        (outcome <> 'superseded'::text) OR (result_row_id IS NOT NULL AND superseded_row_id IS NOT NULL)
    ),
    CONSTRAINT feed_config_write_log_insert_shape_check CHECK (
        (outcome NOT IN ('inserted'::text, 'corrected'::text)) OR result_row_id IS NOT NULL
    )
);

-- THE idempotency index. Its violation is how a concurrent duplicate is detected: the second
-- transaction's INSERT fails on this constraint, re-reads the committed entry, and returns the
-- original result instead of applying the edit twice.
CREATE UNIQUE INDEX feed_config_write_log_idempotency_uidx
    ON feed_config_write_log (tenant_id, idempotency_key);

-- Audit read: "what was edited on this surface recently", newest first.
CREATE INDEX feed_config_write_log_kind_recent_idx
    ON feed_config_write_log (tenant_id, write_kind, created_at DESC);

COMMENT ON TABLE feed_config_write_log IS
  'Idempotency + audit ledger for authored feed-config edits. One entry per accepted write, written in the SAME transaction as the effective-dated close/insert it describes. Exists as a ledger rather than as key columns on each config table because a supersede spans two rows and an unchanged write spans none.';
COMMENT ON COLUMN feed_config_write_log.request_fingerprint IS
  'Stable hash of the canonical client request. An exact replay hashes identically and returns the original result; a same-key/different-payload replay hashes differently and is rejected as a conflict.';
COMMENT ON COLUMN feed_config_write_log.outcome IS
  'inserted | superseded | corrected | unchanged -- the same three-way reconciliation seed-feed-ration performs, so the UI and the seed cannot disagree about what an edit means.';


-- +goose Down
-- Reversible: drop the two tables this migration added, in reverse dependency order. Nothing
-- outside this migration references either, and feed_config_norm (owned by 000003) is untouched.
DROP TABLE IF EXISTS feed_config_write_log;
DROP TABLE IF EXISTS feed_schedule_config;
