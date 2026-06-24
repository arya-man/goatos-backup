-- +goose Up
-- Upgrade the structural vaccination SOP seed from the old placeholder shape to the canonical
-- schema_version + fields + proof_policy contract. This is structural only: no vaccine schedule
-- values are seeded here.
UPDATE sop_definitions
SET code = 'vaccination.drive',
    name = 'Vaccination Session',
    description = 'Shed or cohort vaccination drive execution with backend-owned proof and verification.',
    status = 'active',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND sop_id = 'b0000000-0000-4000-8000-000000000001'
  AND row_version = 1;

UPDATE sop_versions
SET version_label = 'Vaccination Session v1',
    status = 'published',
    form_dsl = '{
      "schema_version": "goatos.sop-form.v1",
      "sop_code": "vaccination.drive",
      "title": "Vaccination Session",
      "trigger": "protocol_window",
      "repeat_for_each_goat": {"source_field": "goat_ids", "item_key": "goat_id"},
      "fields": [
        {"key": "vaccine_lot_id", "label": "Vaccine batch", "type": "vaccine_batch_picker", "required": true, "option_source": "inventory.vaccine_lots.fefo"},
        {"key": "cold_chain_verified", "label": "Cold chain verified", "type": "boolean", "required": true},
        {"key": "shed_video", "label": "Shed drive proof video", "type": "video_proof", "required": true, "proof_subject": "shed"},
        {"key": "vial_lot_video", "label": "Vial and lot proof video", "type": "video_proof", "required": true, "proof_subject": "vial_lot"},
        {"key": "goat_ids", "label": "Goats vaccinated", "type": "goat_scan", "required": true, "repeat": true},
        {"key": "dose_ml_given", "label": "Dose given", "type": "number", "required": true},
        {"key": "route_site", "label": "Route / site", "type": "select", "required": true, "option_source": "vaccination.route_sites"},
        {"key": "administered_at", "label": "Administered at", "type": "date_time", "required": true},
        {"key": "adverse_reaction", "label": "Adverse reaction observed", "type": "boolean", "required": true},
        {"key": "adverse_reaction_notes", "label": "Adverse reaction notes", "type": "text", "required": false},
        {"key": "administration_video", "label": "Administration proof video", "type": "video_proof", "required": true, "proof_subject": "administration"}
      ],
      "rules": [
        {"type": "block_submission_if", "when": {"field": "cold_chain_verified", "operator": "equals", "value": false}, "message": "Cold chain must be verified before submitting the drive."},
        {"type": "required_if", "field": "adverse_reaction_notes", "when": {"field": "adverse_reaction", "operator": "equals", "value": true}, "message": "Adverse reaction notes are required when a reaction is observed."},
        {"type": "proof_required_if", "field": "shed_video", "when": {"field": "goat_ids", "operator": "not_empty"}, "message": "Shed drive video proof is required."},
        {"type": "proof_required_if", "field": "vial_lot_video", "when": {"field": "vaccine_lot_id", "operator": "not_empty"}, "message": "Vial and lot proof is required."},
        {"type": "proof_required_if", "field": "administration_video", "when": {"field": "goat_ids", "operator": "not_empty"}, "message": "Administration proof is required."}
      ],
      "workflow": {
        "nodes": [
          {"key": "operator_submission", "label": "Operator submission", "type": "operator_execution"},
          {"key": "proof_verification", "label": "Proof verification", "type": "proof_verification"},
          {"key": "accepted", "label": "Accepted", "type": "accepted"},
          {"key": "rework_requested", "label": "Rework requested", "type": "rework"}
        ],
        "edges": [
          {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
          {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
          {"from": "proof_verification", "to": "rework_requested", "condition": "verifier_rejects"}
        ]
      }
    }'::jsonb,
    proof_policy = '{
      "required": true,
      "types": ["video"],
      "minimum_count": 3,
      "subject_scope": "batch",
      "verify_before_apply": true,
      "verify_capability": "proof.verify",
      "expected_subjects": ["shed", "vial_lot", "administration"]
    }'::jsonb,
    compatibility = '{
      "min_app_version": "0.2.0",
      "supported_field_types": ["text", "number", "date_time", "boolean", "select", "goat_scan", "rfid_scan", "vaccine_batch_picker", "photo_proof", "video_proof"],
      "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in", "not_in", "gt", "gte", "lt", "lte"],
      "supported_proof_actions": ["video.capture"]
    }'::jsonb,
    validation_report = '{"valid": true, "errors": [], "warnings": [{"field": "form_dsl", "code": "structural_seed", "message": "Structural vaccination SOP seed; protocol schedule values are configured separately."}]}'::jsonb,
    published_at = COALESCE(published_at, now()),
    retired_at = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND sop_version_id = 'b0000000-0000-4000-8000-000000000002'
  AND row_version = 1;

-- +goose Down
UPDATE sop_versions
SET version_label = 'v1-skeleton',
    status = 'draft',
    form_dsl = '{"steps":[{"key":"shed_video","type":"video","label":"Shed drive video"},{"key":"vial_lot","type":"scan","label":"Vial / lot proof"},{"key":"cold_chain","type":"yesno","label":"Cold chain verified"},{"key":"dose","type":"number","label":"Dose"},{"key":"route_site","type":"text","label":"Route / site"},{"key":"administered_at","type":"datetime","label":"Administered at"},{"key":"adverse_reaction","type":"yesno","label":"Adverse reaction"},{"key":"est_vs_used","type":"number","label":"Estimated vs used quantity"},{"key":"verifier_review","type":"review","label":"Verifier review"}]}'::jsonb,
    proof_policy = '{"required":["shed_video","vial_lot","cold_chain","dose"],"verify_capability":"proof.verify"}'::jsonb,
    compatibility = '{}'::jsonb,
    validation_report = '{}'::jsonb,
    published_at = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND sop_version_id = 'b0000000-0000-4000-8000-000000000002'
  AND row_version = 2;

UPDATE sop_definitions
SET name = 'Vaccination drive',
    description = 'Shed vaccination drive execution + proof skeleton.',
    status = 'draft',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND sop_id = 'b0000000-0000-4000-8000-000000000001'
  AND row_version = 2;
