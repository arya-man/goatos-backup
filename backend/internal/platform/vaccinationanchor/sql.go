// Package vaccinationanchor holds the ONE SQL definition of which vaccination anchor events count as
// a goat's previous administration ("anchor_admins"). Generation (the schedule that proposes a
// date) and persistence (the guard that proves it) both build their query from it, so the two can
// never disagree about which anchor chains a follow-up dose.
package vaccinationanchor

import "strings"

// anchorAdministrationsSelect is the anchor_admins SELECT. Placeholders:
//
//	{{TENANT}}     tenant uuid bind
//	{{GOATS}}      goat uuid[] bind
//	{{AS_OF_DATE}} the business (Asia/Kolkata) date the history is read as of: anchors strictly
//	               before it count, and the rule is resolved through the protocol version that is
//	               published and effective on it for the goat's tenant/park scope.
//
// Output columns: goat_id text, administered_at timestamptz, vaccine_code, vaccine_type,
// pathogen_class, dose_code, sequence, protocol_version_id, protocol_id, source.
const anchorAdministrationsSelect = `
  SELECT DISTINCT ON (g.goat_id, vae.vaccination_anchor_event_id)
         g.goat_id::text AS goat_id,
         vae.anchor_date::timestamptz AS administered_at,
         vae.vaccine_code::text AS vaccine_code,
         COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'type', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'type', ''), '')::text AS vaccine_type,
         COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'pathogen_class', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'pathogen_class', ''), '')::text AS pathogen_class,
         pr.dose_code::text AS dose_code,
         pr.sequence::int AS sequence,
         pv.protocol_version_id::text AS protocol_version_id,
         pv.protocol_id::text AS protocol_id,
         'anchor_event'::text AS source
  FROM goats g
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
   AND gsp.shed_id = g.shed_id
  JOIN vaccination_anchor_events vae
    ON vae.tenant_id = g.tenant_id
   AND vae.canceled_at IS NULL
   AND vae.chain_future_from_anchor
   AND vae.protocol_version_id IS NOT NULL
   AND vae.dose_code IS NOT NULL
   AND vae.source_system <> 'vaccination_plan_publish'
   AND vae.anchor_date < {{AS_OF_DATE}}
   AND (
     vae.scope_type = 'tenant'
     OR (vae.scope_type = 'animal_set' AND vae.scope_payload ? 'animal_ids' AND (vae.scope_payload -> 'animal_ids') ? g.goat_id::text)
     OR (vae.scope_type = 'park' AND COALESCE(vae.scope_payload ->> 'park_id', '') = g.park_id::text)
     OR (vae.scope_type = 'shed' AND COALESCE(vae.scope_payload ->> 'shed_id', '') = g.shed_id::text)
     OR (vae.scope_type = 'partition' AND COALESCE(vae.scope_payload ->> 'shed_id', '') = g.shed_id::text AND COALESCE(vae.scope_payload ->> 'partition_label', '') = COALESCE(gsp.partition_label, 'whole'))
   )
  JOIN protocol_versions anchor_pv
    ON anchor_pv.tenant_id = g.tenant_id
   AND anchor_pv.protocol_version_id = vae.protocol_version_id
  JOIN protocol_rules anchor_pr
    ON anchor_pr.tenant_id = anchor_pv.tenant_id
   AND anchor_pr.protocol_version_id = anchor_pv.protocol_version_id
   AND lower(btrim(anchor_pr.dose_code)) = lower(btrim(vae.dose_code))
   AND lower(btrim(COALESCE(NULLIF(anchor_pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(anchor_pv.rule_dsl -> 'vaccine' ->> 'code', '')))) = lower(btrim(vae.vaccine_code))
  JOIN protocol_rule_lineage anchor_lineage
    ON anchor_lineage.tenant_id = anchor_pr.tenant_id
   AND anchor_lineage.protocol_version_id = anchor_pr.protocol_version_id
   AND anchor_lineage.rule_id = anchor_pr.rule_id
  JOIN protocol_versions pv
    ON pv.tenant_id = g.tenant_id
   AND pv.protocol_id = anchor_pv.protocol_id
   AND pv.status = 'published'
   AND pv.effective_from <= {{AS_OF_DATE}}
   AND (pv.effective_to IS NULL OR pv.effective_to > {{AS_OF_DATE}})
   AND (pv.scope_type = 'tenant' OR (pv.scope_type = 'park' AND pv.scope_id = g.park_id))
  JOIN protocol_rule_lineage current_lineage
    ON current_lineage.tenant_id = pv.tenant_id
   AND current_lineage.protocol_version_id = pv.protocol_version_id
   AND current_lineage.identity_key = anchor_lineage.identity_key
  JOIN protocol_rules pr
    ON pr.tenant_id = current_lineage.tenant_id
   AND pr.protocol_version_id = current_lineage.protocol_version_id
   AND pr.rule_id = current_lineage.rule_id
   AND lower(btrim(pr.dose_code)) = lower(btrim(vae.dose_code))
   AND lower(btrim(COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', '')))) = lower(btrim(vae.vaccine_code))
  WHERE g.tenant_id = {{TENANT}}
    AND g.goat_id = ANY({{GOATS}})
    AND (
      NOT vae.enforce_age_eligibility
      OR pr.trigger_type <> 'birth_age'
      OR (g.dob IS NOT NULL AND g.dob + pr.offset_days <= vae.anchor_date)
    )
  ORDER BY g.goat_id, vae.vaccination_anchor_event_id,
           CASE WHEN pv.scope_type = 'park' THEN 0 ELSE 1 END,
           pv.effective_from DESC,
           pv.protocol_version_id`

// AnchorAdministrationsSQL renders the anchor_admins SELECT with the given SQL expressions.
func AnchorAdministrationsSQL(tenant, goats, asOfDate string) string {
	return strings.NewReplacer("{{TENANT}}", tenant, "{{GOATS}}", goats, "{{AS_OF_DATE}}", asOfDate).Replace(anchorAdministrationsSelect)
}
