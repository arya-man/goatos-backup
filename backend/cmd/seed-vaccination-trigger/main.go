package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	defaultTenantID   = "00000000-0000-4000-8000-000000000001"
	localFarmID       = "00000000-0000-4000-8000-000000003100"
	localParkID       = "00000000-0000-4000-8000-000000003001"
	localShedID       = "00000000-0000-4000-8000-000000003101"
	localItemID       = "00000000-0000-4000-8000-00000000b001"
	localStockID      = "00000000-0000-4000-8000-00000000b002"
	localProtocolID   = "00000000-0000-4000-8000-00000000b010"
	localVersionID    = "00000000-0000-4000-8000-00000000b011"
	localV1ProtocolID = "00000000-0000-4000-8000-00000000b050"
	localV1VersionID  = "00000000-0000-4000-8000-00000000b051"
	localOperatorID   = "00000000-0000-4000-8000-00000000b071"
	localParkHeadID   = "00000000-0000-4000-8000-00000000b072"
	localVerifierID   = "00000000-0000-4000-8000-00000000b073"
	localSOPVersionID = "b0000000-0000-4000-8000-000000000002"
	localStageK0ID    = "00000000-0000-4000-8000-00000000b030"
	localStageK1ID    = "00000000-0000-4000-8000-00000000b031"
	localStageK2ID    = "00000000-0000-4000-8000-00000000b032"
	localStageK3ID    = "00000000-0000-4000-8000-00000000b033"
)

// The seed's protocol rule_dsl / proof_policy literals live here as single-source constants so the
// same JSON that is inserted into the draft protocol_versions row is also fed through
// protocolapp.ValidateRuleDSL + ValidateExecutionContract before publish. This guarantees the seed can
// only publish JSON that satisfies the real publish contract (no drift), and it is published through
// the protocol service so the canonical publish transaction writes the audit log + emits the
// protocol.version.published outbox message that vaccination generation consumes — rather than a raw
// SQL status flip that bypasses validation, audit and outbox.
const (
	retiredRuleDSL     = `{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial","course_type":"booster","inventory_item_id":"00000000-0000-4000-8000-00000000b001","manufacturer":"Mesha matrix dev baseline","disease":"Enterotoxaemia + Tetanus","compatibility_group":"ET+TT"},"eligibility":{"stage":"K2","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","quarantine","icu"]},"schedule":[{"dose_code":"ET_TT_4W","trigger_type":"birth_age","offset_days":28,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review","min_gap_days":0,"repeat":"none","catch_up":"immediate","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}}]}`
	retiredProofPolicy = `{"required_proofs":["shed","vial_lot","administration"],"seed":"vaccination-matrix-dev-baseline"}`

	v1RuleDSL     = `{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial","course_type":"booster","inventory_item_id":"00000000-0000-4000-8000-00000000b001","manufacturer":"Mesha matrix dev baseline","disease":"Enterotoxaemia + Tetanus","compatibility_group":"ET+TT"},"eligibility":{"stage":"K2","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","quarantine","icu"]},"missed_dose_policy":"pc_approval","schedule":[{"dose_code":"ET_TT_4W","sequence":1,"trigger_type":"birth_age","offset_days":28,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review","min_gap_days":0,"repeat":"none","catch_up":"immediate","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}},{"dose_code":"ET_TT_7W","sequence":2,"trigger_type":"after_previous_completion","offset_days":21,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review","min_gap_days":21,"repeat":"none","catch_up":"pc_approval","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}}]}`
	v1ProofPolicy = `{"required_proofs":["shed","vial_lot","administration"],"seed":"vaccination-v1-matrix-proof-baseline"}`
)

// seedProtocolVersion describes one draft protocol version the seed inserts and then publishes through
// the protocol service. finalStatus is the state the version should end in ("published" or "retired");
// a retired fixture is published (so audit + outbox happen) and then retired.
type seedProtocolVersion struct {
	protocolID   string
	versionID    string
	versionLabel string
	ruleDSL      string
	proofPolicy  string
	finalStatus  string
}

func seedProtocolVersions() []seedProtocolVersion {
	return []seedProtocolVersion{
		{protocolID: localProtocolID, versionID: localVersionID, versionLabel: "Retired matrix dev baseline", ruleDSL: retiredRuleDSL, proofPolicy: retiredProofPolicy, finalStatus: "retired"},
		{protocolID: localV1ProtocolID, versionID: localV1VersionID, versionLabel: "V1 matrix proof baseline", ruleDSL: v1RuleDSL, proofPolicy: v1ProofPolicy, finalStatus: "published"},
	}
}

// validateSeedProtocolVersions runs the real publish-time validators against every seed protocol
// version JSON. It is called before any insert so drift between the seed literals and the publish
// contract fails fast (and is exercised by unit tests without a database).
func validateSeedProtocolVersions() error {
	for _, sv := range seedProtocolVersions() {
		if err := protocolapp.ValidateRuleDSL([]byte(sv.ruleDSL)); err != nil {
			return fmt.Errorf("seed-vaccination-trigger: version %s rule_dsl invalid: %w", sv.versionID, err)
		}
		v := protocoldomain.Version{
			Category:     "vaccination",
			SopVersionID: localSOPVersionID,
			ProofPolicy:  []byte(sv.proofPolicy),
			RuleDsl:      []byte(sv.ruleDSL),
		}
		if err := protocolapp.ValidateExecutionContract(v); err != nil {
			return fmt.Errorf("seed-vaccination-trigger: version %s execution contract invalid: %w", sv.versionID, err)
		}
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-vaccination-trigger", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 30*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Fail fast (and without a database) if the seed's rule_dsl / proof_policy literals drift out of
	// sync with the real publish contract.
	if err := validateSeedProtocolVersions(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := execSeedSQL(ctx, pool, seedSQL, *tenantID); err != nil {
		return err
	}

	// Publish each draft protocol version through the protocol service so validation runs and the
	// canonical publish transaction writes the audit log + emits the protocol.version.published outbox
	// message. Derived protocol_rules are regenerated by the publish path — the seed never hand-inserts
	// them. A "retired" fixture is published first (so audit + outbox fire) and then retired.
	protocolService := protocolapp.NewService(protocolpg.NewRepository(pool, pgCfg.QueryTimeout))
	for _, sv := range seedProtocolVersions() {
		isDraft, err := upsertDraftProtocolVersion(ctx, pool, *tenantID, sv)
		if err != nil {
			return fmt.Errorf("seed draft protocol version %s: %w", sv.versionID, err)
		}
		// A re-run (or a DB seeded by the old raw-SQL path) leaves the version already published/
		// retired, not draft. PublishVersion rejects a non-draft version (ErrVersionNotDraft), so
		// only publish when the row is currently a draft; an already-finalized version is a no-op.
		if !isDraft {
			continue
		}
		if err := protocolService.PublishVersion(ctx, *tenantID, sv.versionID, nil, "seed-vaccination-trigger:"+sv.versionID); err != nil {
			return fmt.Errorf("publish protocol version %s through protocol service: %w", sv.versionID, err)
		}
		if sv.finalStatus == "retired" {
			if err := retirePublishedSeedVersion(ctx, pool, *tenantID, sv.versionID); err != nil {
				return fmt.Errorf("retire protocol version %s: %w", sv.versionID, err)
			}
		}
	}

	fmt.Printf("seeded vaccination trigger fixtures tenant=%s farm=%s park=%s shed=%s vaccine_item=%s retired_protocol_version=%s v1_protocol_version=%s\n",
		*tenantID, localFarmID, localParkID, localShedID, localItemID, localVersionID, localV1VersionID)
	return nil
}

// upsertDraftProtocolVersion upserts the draft protocol_versions row for a seed version. The row is
// only ever written/updated while it is still draft (ON CONFLICT ... WHERE status='draft'), so a
// re-run never mutates an already-published or retired version. It reports whether the row is now a
// draft: a fresh insert or a still-draft update affects one row (isDraft=true); a conflict on an
// already-published/retired row affects zero rows (isDraft=false) so the caller skips the publish
// step (which would otherwise fail with ErrVersionNotDraft on re-run). Publishing (with validation,
// audit and outbox) is done separately through the protocol service.
func upsertDraftProtocolVersion(ctx context.Context, execer seedExecutor, tenantID string, sv seedProtocolVersion) (bool, error) {
	tag, err := execer.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy,
  sop_version_id, published_at
) VALUES (
  $2::uuid, $1::uuid, $3::uuid, 'park', $4::uuid, 1,
  $5, 'draft', DATE '2026-01-01', DATE '2028-01-01', $6::jsonb, $7::jsonb,
  $8::uuid, NULL
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET version_label = EXCLUDED.version_label,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    rule_dsl = EXCLUDED.rule_dsl,
    proof_policy = EXCLUDED.proof_policy,
    sop_version_id = EXCLUDED.sop_version_id,
    updated_at = now()
WHERE protocol_versions.status = 'draft'`,
		tenantID, sv.versionID, sv.protocolID, localParkID, sv.versionLabel, sv.ruleDSL, sv.proofPolicy, localSOPVersionID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// retirePublishedSeedVersion transitions a just-published seed version to retired. There is no protocol
// service retire method, so this is a scoped status flip guarded to only ever act on a published row
// (idempotent across re-runs). It does not bypass publish validation/audit/outbox — the publish
// already happened through the service just before this call.
func retirePublishedSeedVersion(ctx context.Context, execer seedExecutor, tenantID, versionID string) error {
	_, err := execer.Exec(ctx, `
UPDATE protocol_versions
SET status = 'retired',
    retired_at = COALESCE(retired_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND protocol_version_id = $2::uuid
  AND status = 'published'`, tenantID, versionID)
	return err
}

type seedExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func execSeedSQL(ctx context.Context, execer seedExecutor, sql string, tenantID string) error {
	for _, statement := range strings.Split(sql, ";\n\n") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := execer.Exec(ctx, statement, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func validateTarget(env, databaseURL string) error {
	return localtarget.ValidateLocalDatabaseTarget("seed-vaccination-trigger", env, databaseURL, "local", "dev", "test")
}

const seedSQL = `
INSERT INTO animal_stage_lookup (
  animal_stage_id, tenant_id, stage_code, name, min_age_days, max_age_days,
  sort_order, status
) VALUES
  ('` + localStageK0ID + `', $1::uuid, 'K0', 'Newborn', 0, 1, 0, 'active'),
  ('` + localStageK1ID + `', $1::uuid, 'K1', 'Milk training', 2, 7, 10, 'active'),
  ('` + localStageK2ID + `', $1::uuid, 'K2', 'Milk drinking', 8, 42, 20, 'active'),
  ('` + localStageK3ID + `', $1::uuid, 'K3', 'Weaned kids', 43, NULL, 30, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE
SET name = EXCLUDED.name,
    min_age_days = EXCLUDED.min_age_days,
    max_age_days = EXCLUDED.max_age_days,
    sort_order = EXCLUDED.sort_order,
    status = 'active',
    updated_at = now();

INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, state_region, timezone, status, updated_at
) VALUES
  ('` + localFarmID + `', $1::uuid, 'farm', 'TRIG_FARM', 'Trigger Gate Farm', NULL,
   'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now()),
  ('` + localShedID + `', $1::uuid, 'shed', 'CBE-TRIG-SHED', 'CBE Trigger Shed', '` + localParkID + `',
   'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now())
ON CONFLICT (location_id) DO UPDATE
SET name = EXCLUDED.name,
    parent_location_id = EXCLUDED.parent_location_id,
    status = 'active',
    updated_at = now();

INSERT INTO location_operational_attributes (
  tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination,
  usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes
) VALUES
  ($1::uuid, '` + localParkID + `', true, true, true, true, false, false, false, 10, 'Local vaccination trigger seed park'),
  ($1::uuid, '` + localShedID + `', true, true, true, true, false, false, false, 20, 'Local vaccination trigger seed shed')
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true,
    usable_for_sop = true,
    is_quarantine = false,
    is_icu = false,
    updated_at = now();

WITH seeded_workforce AS (
  INSERT INTO workforce_members (
    workforce_member_id, tenant_id, display_code, display_name, status,
    primary_role_hint, primary_location_id, metadata, updated_at
  ) VALUES
    ('` + localOperatorID + `', $1::uuid, 'CBE-VACC-OP-01', 'CBE Vaccination Operator', 'active',
     'operator', '` + localParkID + `', '{"seed":"vaccination-trigger","role":"vaccination_operator"}'::jsonb, now()),
    ('` + localParkHeadID + `', $1::uuid, 'CBE-PARK-HEAD-01', 'CBE Park Head', 'active',
     'park_head', '` + localParkID + `', '{"seed":"vaccination-trigger","role":"park_head"}'::jsonb, now()),
    ('` + localVerifierID + `', $1::uuid, 'CBE-VACC-VERIFY-01', 'Video Verification Team', 'active',
     'verifier', NULL, '{"seed":"vaccination-trigger","role":"vaccination_verifier"}'::jsonb, now())
  ON CONFLICT (tenant_id, display_code) DO UPDATE
  SET display_name = EXCLUDED.display_name,
      status = 'active',
      primary_role_hint = EXCLUDED.primary_role_hint,
      primary_location_id = EXCLUDED.primary_location_id,
      metadata = EXCLUDED.metadata,
      updated_at = now(),
      row_version = workforce_members.row_version + 1
  RETURNING workforce_member_id, display_code
),
capability_grants AS (
  SELECT
    sw.workforce_member_id,
    wc.capability_id,
    CASE sw.display_code
      WHEN 'CBE-VACC-OP-01' THEN 'vaccination.execute'
      WHEN 'CBE-VACC-VERIFY-01' THEN 'proof.verify'
      ELSE 'sop.execute'
    END AS capability_code,
    CASE sw.display_code
      WHEN 'CBE-VACC-VERIFY-01' THEN 'tenant'
      ELSE 'park'
    END AS scope_type,
    CASE sw.display_code
      WHEN 'CBE-VACC-VERIFY-01' THEN $1::uuid
      ELSE '` + localParkID + `'::uuid
    END AS scope_id
  FROM seeded_workforce sw
  JOIN workforce_capabilities wc
    ON wc.tenant_id = $1::uuid
   AND wc.capability_code = CASE sw.display_code
      WHEN 'CBE-VACC-OP-01' THEN 'vaccination.execute'
      WHEN 'CBE-VACC-VERIFY-01' THEN 'proof.verify'
      ELSE 'sop.execute'
    END
   AND wc.status = 'active'
)
INSERT INTO workforce_member_capabilities (
  tenant_id, workforce_member_id, capability_id, scope_type, scope_id, status
)
SELECT $1::uuid, workforce_member_id, capability_id, scope_type, scope_id, 'active'
FROM capability_grants
ON CONFLICT DO NOTHING;

	INSERT INTO inventory_items (
	  item_id, tenant_id, item_code, name, category, base_unit, status, context
	) VALUES (
	  '` + localItemID + `', $1::uuid, 'VAC-ET-TT-PC', 'ET+TT Vaccine', 'vaccine', 'dose', 'active',
	  '{"seed":"vaccination-matrix-dev-baseline","reference":"docs/preventive-care-vaccination/vaccination-rules.md"}'::jsonb
	)
	ON CONFLICT (item_id) DO UPDATE
	SET item_code = EXCLUDED.item_code,
	    name = EXCLUDED.name,
	    status = 'active',
	    context = EXCLUDED.context,
	    updated_at = now();

	INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
	VALUES ($1::uuid, '` + localItemID + `', 'Enterotoxaemia + Tetanus', 'Mesha matrix dev baseline', 10, 0, '{"seed":"vaccination-matrix-dev-baseline","reference":"docs/preventive-care-vaccination/vaccination-rules.md"}'::jsonb)
	ON CONFLICT (tenant_id, item_id) DO UPDATE
	SET disease = EXCLUDED.disease,
	    manufacturer = EXCLUDED.manufacturer,
	    context = EXCLUDED.context,
	    updated_at = now();

INSERT INTO inventory_stock (
  stock_id, tenant_id, item_id, location_id, lot_code, expiry_date,
  quantity_in_stock, quantity_reserved, quantity_unit, status
	) VALUES (
	  '` + localStockID + `', $1::uuid, '` + localItemID + `', '` + localParkID + `', 'ETTT-LOT-001',
	  DATE '2027-12-31', 1000, 0, 'dose', 'active'
	)
	ON CONFLICT (stock_id) DO UPDATE
	SET lot_code = EXCLUDED.lot_code,
	    quantity_in_stock = GREATEST(inventory_stock.quantity_in_stock, 1000),
	    status = 'active',
	    updated_at = now();

INSERT INTO protocol_definitions (
	  protocol_id, tenant_id, code, name, category, status
	) VALUES (
	  '` + localProtocolID + `', $1::uuid, 'vaccination.matrix.retired_trigger_seed', 'Retired Vaccination Matrix Trigger Seed', 'vaccination', 'active'
	)
	ON CONFLICT (protocol_id) DO UPDATE
	SET code = EXCLUDED.code,
	    name = EXCLUDED.name,
	    status = 'active',
	    updated_at = now();

-- NOTE: protocol_versions rows, their derived protocol_rules, and the published/retired
-- status transitions for these seed protocols are intentionally NOT in this SQL. They are
-- inserted as drafts and then published through the protocol service (see run()) so that
-- publish-time validation, the audit log, and the protocol.version.published outbox message
-- all happen atomically. Only the protocol_definitions rows (needed as FK parents) live here.

INSERT INTO protocol_definitions (
  protocol_id, tenant_id, code, name, category, status
) VALUES (
  '` + localV1ProtocolID + `', $1::uuid, 'vaccination.matrix.trigger_seed', 'Vaccination Rule Matrix Trigger Seed', 'vaccination', 'active'
)
ON CONFLICT (protocol_id) DO UPDATE
SET code = EXCLUDED.code,
    name = EXCLUDED.name,
    status = 'active',
    updated_at = now();

	`
