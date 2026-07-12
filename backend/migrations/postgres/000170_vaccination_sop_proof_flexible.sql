-- +goose Up
-- Confirmed maintainer rule for the vaccination SOP video proof (docs/mobile/proof-capture-sync-and-e2e.md
-- §2): all three named proof videos stay MANDATORY (shed_video, vial_lot_video, administration_video),
-- and the operator may additionally attach up to two ad-hoc extra videos (e.g. a second shed/batch in the
-- same drive) beyond the three named subjects, capped at 5 videos total per submission. Each ad-hoc extra
-- video carries an operator-entered caption (required whenever that extra video slot is used) so a
-- reviewer knows what it shows without relying on the fixed proof_subject vocabulary. Every field also
-- gets a plain operator-facing `description` (form DSL field-level property) so the mobile Submit form
-- shows what each field is for, per the admin-authored-content contract in AGENTS.md ("Frontend must not
-- ship local defaults ... backend contract owns labels/copy"). This is a data-only update to the already
-- published canonical vaccination.drive SOP version seeded in 000081; schema_version, sop_code, trigger,
-- repeat_for_each_goat, and workflow are unchanged.
UPDATE sop_versions
SET form_dsl = '{
      "schema_version": "goatos.sop-form.v1",
      "sop_code": "vaccination.drive",
      "title": "Vaccination Session",
      "trigger": "protocol_window",
      "repeat_for_each_goat": {"source_field": "goat_ids", "item_key": "goat_id"},
      "fields": [
        {"key": "vaccine_lot_id", "label": "Vaccine batch", "type": "vaccine_batch_picker", "required": true, "option_source": "inventory.vaccine_lots.fefo", "description": "Select the vaccine batch/lot being used for this drive, sourced from FEFO inventory."},
        {"key": "cold_chain_verified", "label": "Cold chain verified", "type": "boolean", "required": true, "description": "Confirm the vaccine was kept within the cold chain temperature range up to the point of use."},
        {"key": "shed_video", "label": "Shed drive proof video", "type": "video_proof", "required": true, "proof_subject": "shed", "description": "Video of the shed/drive — which shed and group is being vaccinated."},
        {"key": "vial_lot_video", "label": "Vial and lot proof video", "type": "video_proof", "required": true, "proof_subject": "vial_lot", "description": "Video of the vaccine vial + lot/batch number, for traceability."},
        {"key": "goat_ids", "label": "Goats vaccinated", "type": "goat_scan", "required": true, "repeat": true, "description": "Scan each goat''s RFID tag as it is vaccinated."},
        {"key": "dose_ml_given", "label": "Dose given", "type": "number", "required": true, "description": "Dose administered, in millilitres, per the protocol."},
        {"key": "route_site", "label": "Route / site", "type": "select", "required": true, "option_source": "vaccination.route_sites", "description": "Route and body site used for administration."},
        {"key": "administered_at", "label": "Administered at", "type": "date_time", "required": true, "description": "Date and time the dose was administered."},
        {"key": "adverse_reaction", "label": "Adverse reaction observed", "type": "boolean", "required": true, "description": "Whether an adverse reaction was observed after administration."},
        {"key": "adverse_reaction_notes", "label": "Adverse reaction notes", "type": "text", "required": false, "description": "Details of the observed adverse reaction, if any."},
        {"key": "administration_video", "label": "Administration proof video", "type": "video_proof", "required": true, "proof_subject": "administration", "description": "Video of the actual injection — proof the dose was given, not just logged."},
        {"key": "extra_video_1", "label": "Additional video 1 (optional)", "type": "video_proof", "required": false, "proof_subject": "additional", "description": "Optional extra video, e.g. a second shed or batch covered in this drive. Add a caption below if you attach this."},
        {"key": "extra_video_1_caption", "label": "Additional video 1 caption", "type": "text", "required": false, "description": "Caption describing what additional video 1 shows."},
        {"key": "extra_video_2", "label": "Additional video 2 (optional)", "type": "video_proof", "required": false, "proof_subject": "additional", "description": "Optional second extra video, e.g. a third shed or batch covered in this drive. Add a caption below if you attach this."},
        {"key": "extra_video_2_caption", "label": "Additional video 2 caption", "type": "text", "required": false, "description": "Caption describing what additional video 2 shows."}
      ],
      "rules": [
        {"type": "block_submission_if", "when": {"field": "cold_chain_verified", "operator": "equals", "value": false}, "message": "Cold chain must be verified before submitting the drive."},
        {"type": "required_if", "field": "adverse_reaction_notes", "when": {"field": "adverse_reaction", "operator": "equals", "value": true}, "message": "Adverse reaction notes are required when a reaction is observed."},
        {"type": "proof_required_if", "field": "shed_video", "when": {"field": "goat_ids", "operator": "not_empty"}, "message": "Shed drive video proof is required."},
        {"type": "proof_required_if", "field": "vial_lot_video", "when": {"field": "vaccine_lot_id", "operator": "not_empty"}, "message": "Vial and lot proof is required."},
        {"type": "proof_required_if", "field": "administration_video", "when": {"field": "goat_ids", "operator": "not_empty"}, "message": "Administration proof is required."},
        {"type": "required_if", "field": "extra_video_1_caption", "when": {"field": "extra_video_1", "operator": "not_empty"}, "message": "Caption is required when additional video 1 is attached."},
        {"type": "required_if", "field": "extra_video_2_caption", "when": {"field": "extra_video_2", "operator": "not_empty"}, "message": "Caption is required when additional video 2 is attached."}
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
    -- minimum_count stays 3 (the three named videos are still all mandatory); maximum_count is new and
    -- caps total attached videos (3 named + up to 2 ad-hoc extras) at 5. expected_subjects and
    -- subject_scope are unchanged from 000081. ad_hoc_extra_videos is descriptive metadata (not read by
    -- any backend validator today) documenting the extra-video allowance for the mobile client, which
    -- reads proof_policy to build the capture cap per docs/mobile/proof-capture-sync-and-e2e.md.
    proof_policy = '{
      "required": true,
      "types": ["video"],
      "minimum_count": 3,
      "maximum_count": 5,
      "subject_scope": "batch",
      "verify_before_apply": true,
      "verify_capability": "proof.verify",
      "expected_subjects": ["shed", "vial_lot", "administration"],
      "retention_policy": "operational_90d",
      "ad_hoc_extra_videos": {
        "allowed": true,
        "max_count": 2,
        "field_keys": ["extra_video_1", "extra_video_2"],
        "requires_caption": true
      }
    }'::jsonb,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND sop_version_id = 'b0000000-0000-4000-8000-000000000002'
  AND row_version = 2;

-- +goose Down
-- Restore the exact 000081 published state: all 3 named videos required, minimum_count 3, no
-- maximum_count, no field descriptions, no ad-hoc extra-video slots. This undoes only 000170's change to
-- the sop_versions row -- it does not replay 000081's own Down (which reverts to the pre-canonical draft
-- skeleton); that stays 000081's responsibility.
UPDATE sop_versions
SET form_dsl = '{
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
      "expected_subjects": ["shed", "vial_lot", "administration"],
      "retention_policy": "operational_90d"
    }'::jsonb,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND sop_version_id = 'b0000000-0000-4000-8000-000000000002'
  AND row_version = 3;
