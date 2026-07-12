# Shared resolver for local vaccination proof scripts.
#
# The local DB can contain both the old trigger-seed proof matrix and the newer
# real vaccination matrix. Proof scripts must use whichever vaccination matrix is
# currently published instead of pinning protocol/rule UUIDs that may be retired.

resolve_vaccination_fixture() {
  local tenant="${TENANT:-${GOATOS_TENANT_ID:-}}"
  [ -n "$tenant" ] || fail "resolve vaccination fixture: TENANT/GOATOS_TENANT_ID is required"

  VERSION="$(psqlq "
    SELECT pv.protocol_version_id::text
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = '$tenant'
      AND pd.category = 'vaccination'
      AND pd.code IN ('vaccination.matrix', 'vaccination.matrix.trigger_seed')
      AND pv.status = 'published'
      AND pv.effective_from <= CURRENT_DATE
      AND (pv.effective_to IS NULL OR pv.effective_to >= CURRENT_DATE)
    ORDER BY CASE pd.code WHEN 'vaccination.matrix' THEN 0 ELSE 1 END,
             CASE pv.scope_type WHEN 'tenant' THEN 0 ELSE 1 END,
             pv.effective_from DESC,
             pv.version DESC,
             pv.protocol_version_id
    LIMIT 1")"
  [ -n "$VERSION" ] || fail "no published vaccination matrix version found"

  RULE="$(psqlq "
    SELECT rule_id::text
    FROM protocol_rules
    WHERE tenant_id = '$tenant'
      AND protocol_version_id = '$VERSION'
      AND lower(replace(dose_code, '-', '_')) IN (
        'et_tt_first',
        'et_tt_4w',
        'et_primary_1',
        'et_tt_kid_4w'
      )
    ORDER BY CASE lower(replace(dose_code, '-', '_'))
               WHEN 'et_tt_first' THEN 0
               WHEN 'et_tt_4w' THEN 1
               WHEN 'et_primary_1' THEN 2
               ELSE 3
             END,
             sequence,
             sort_order,
             rule_id
    LIMIT 1")"
  [ -n "$RULE" ] || fail "published vaccination matrix $VERSION has no ET+TT primary rule"

  BOOSTER_RULE="$(psqlq "
    SELECT rule_id::text
    FROM protocol_rules
    WHERE tenant_id = '$tenant'
      AND protocol_version_id = '$VERSION'
      AND lower(replace(dose_code, '-', '_')) IN (
        'et_tt_booster',
        'et_tt_7w',
        'et_booster_1',
        'et_tt_kid_7w'
      )
    ORDER BY CASE lower(replace(dose_code, '-', '_'))
               WHEN 'et_tt_booster' THEN 0
               WHEN 'et_tt_7w' THEN 1
               WHEN 'et_booster_1' THEN 2
               ELSE 3
             END,
             sequence,
             sort_order,
             rule_id
    LIMIT 1")"
  [ -n "$BOOSTER_RULE" ] || fail "published vaccination matrix $VERSION has no ET+TT booster rule"

  BOOSTER_TRIGGER="$(psqlq "
    SELECT trigger_type
    FROM protocol_rules
    WHERE tenant_id = '$tenant'
      AND protocol_version_id = '$VERSION'
      AND rule_id = '$BOOSTER_RULE'")"

  SOPVER="$(psqlq "
    SELECT COALESCE(pv.sop_version_id::text, pr.sop_version_id::text, '')
    FROM protocol_versions pv
    LEFT JOIN protocol_rules pr
      ON pr.tenant_id = pv.tenant_id
     AND pr.protocol_version_id = pv.protocol_version_id
     AND pr.rule_id = '$RULE'
    WHERE pv.tenant_id = '$tenant'
      AND pv.protocol_version_id = '$VERSION'
    LIMIT 1")"
  [ -n "$SOPVER" ] || fail "published vaccination matrix $VERSION has no linked SOP version"

  local stock_pair
  stock_pair="$(psqlq "
    SELECT i.item_id::text || '|' || s.stock_id::text
    FROM inventory_items i
    JOIN inventory_stock s
      ON s.tenant_id = i.tenant_id
     AND s.item_id = i.item_id
    WHERE i.tenant_id = '$tenant'
      AND i.category = 'vaccine'
      AND i.status = 'active'
      AND s.status = 'active'
      AND s.quantity_in_stock > s.quantity_reserved
      AND (
        i.item_code IN ('VAC-ET-TT', 'VAC-ET-TT-PC')
        OR lower(i.name) LIKE '%et%tt%'
      )
    ORDER BY CASE i.item_code
               WHEN 'VAC-ET-TT' THEN 0
               WHEN 'VAC-ET-TT-PC' THEN 1
               ELSE 2
             END,
             s.expiry_date NULLS LAST,
             s.stock_id
    LIMIT 1")"
  [ -n "$stock_pair" ] || fail "no active ET+TT vaccine stock with available quantity"
  ITEM="${stock_pair%%|*}"
  LOT="${stock_pair##*|}"

  MATRIX_SUMMARY="$(psqlq "
    SELECT concat_ws('|',
      pd.code,
      pv.status,
      pv.version_label,
      pv.scope_type,
      COALESCE(pv.scope_id::text, ''),
      COALESCE(jsonb_array_length(pv.rule_dsl->'schedule')::text, '0')
    )
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = '$tenant'
      AND pv.protocol_version_id = '$VERSION'")"
  RULE_SUMMARY="$(psqlq "
    SELECT concat_ws('|', dose_code, trigger_type, offset_days::text, due_window_days::text)
    FROM protocol_rules
    WHERE tenant_id = '$tenant'
      AND protocol_version_id = '$VERSION'
      AND rule_id = '$RULE'")"
  BOOSTER_SUMMARY="$(psqlq "
    SELECT concat_ws('|', dose_code, trigger_type, offset_days::text, due_window_days::text, min_gap_days::text)
    FROM protocol_rules
    WHERE tenant_id = '$tenant'
      AND protocol_version_id = '$VERSION'
      AND rule_id = '$BOOSTER_RULE'")"

  SOP_VERSION="$SOPVER"
  VACCINE_ITEM="$ITEM"
  VACCINE_LOT="$LOT"
  export VERSION RULE BOOSTER_RULE BOOSTER_TRIGGER SOPVER SOP_VERSION ITEM VACCINE_ITEM LOT VACCINE_LOT
  export MATRIX_SUMMARY RULE_SUMMARY BOOSTER_SUMMARY
}

# Persistent-local proof scripts create isolated goats/sheds/batches on purpose. Always put the
# developer database back onto the canonical workbook seed before returning, even when a proof
# fails halfway through. Set GOATOS_KEEP_VACCINATION_PROOF_FIXTURES=1 only for an explicit manual
# forensic session; normal proof runs must never leak their fixtures into Calendar/Vaccination.
install_vaccination_proof_cleanup_trap() {
  restore_canonical_vaccination_seed_after_proof() {
    local proof_status=$?
    trap - EXIT
    if [ "${GOATOS_KEEP_VACCINATION_PROOF_FIXTURES:-0}" != "1" ]; then
      echo "restoring canonical local vaccination seed after proof..." >&2
      (
        cd "$BACKEND"
        GOATOS_ENV=local GOATOS_TENANT_ID="$TENANT" \
          go run ./cmd/seed-vaccination-real -tenant-id "$TENANT" -purge-fixtures=true
      ) >/tmp/goatos-vaccination-proof-restore.log 2>&1 || {
        echo "WARNING canonical vaccination seed restore failed; see /tmp/goatos-vaccination-proof-restore.log" >&2
      }
    fi
    exit "$proof_status"
  }
  trap restore_canonical_vaccination_seed_after_proof EXIT
}
