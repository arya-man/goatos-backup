# Gemini Proof Media Verification Registry

Status: proposed production registry for AI-assisted verifier review.

This document defines what Gemini must check when Goat OS sends proof videos or images for verification. Gemini is an assistant to the generic verification module; it does not approve work by itself. The human Verifier role owns approve/reject, and authority roles own follow-up action.

## Coverage Rule

Any action that captures video or image proof must have a registry entry before it is sent to Gemini. The entry must state the feature purpose, required app claim properties, allowed media type, and category-specific verification checks. If a new producer has no entry, route it to human review and add the category first.

Active verifier categories covered now:
- `vaccination_proof` (`vaccination` alias)
- `weighing_proof` (`weighing` alias)
- `birth_evidence`
- `death_evidence`
- `shifting_move` (`shifting` alias)
- `milk_preparation` (`milk_prep` alias)
- `milk_feeding`
- `feed_distribution`
- `feed_packing`
- `feed_transport`
- `health_adults`
- `health_kids`

Note: this registry follows the active verifier queue category keys. Older design notes may refer to shorthand names such as `vaccination`, `weighing`, `shifting`, or `milk_prep`, and may group Milk under Counts; use the active keys above for Gemini requests.

Known future/cross-cutting proof producers covered as placeholders:
- `procurement_load`
- `transit_handoff`
- `arrival_intake`
- `dispatch_exit`
- `attendance_checkin`
- `breeding_pregnancy`
- `abortion_evidence`
- `inventory_stock_proof`

## Required Input Envelope

Every Gemini request should include `verification_item_id`, `category`, media file/reference, `captured_at`, tenant/park/shed/operator context, and the category-specific `app_claim` from the JSON registry. Video/image alone is not enough for claim comparison.

Common verifier item properties expected by the registry:
- `verification_item_id`
- `category`, `vertical`, `module`, `status`, `row_version`
- `captured_at`
- `media[]`
- `source`
- `subject_label`, `subject_note`
- `context_rows`
- `operator_id`, `operator_name`
- `park_id`, `park_label`
- `shed_id`, `shed_label`, `op_location_label`
- `verdict`

Common media properties expected inside `media[]`:
- `proof_id`
- `download_url`
- `label`
- `answer`
- `mime_type`
- `duration_ms`

## Shared Verification Principles

- Verify the expected category and app claim, not a casual video summary.
- Require visible evidence for the critical action or state.
- Use `needs_human_review` when media is dark, shaky, cropped, occluded, ambiguous, or missing metadata.
- Never invent goat identity, shed, weight, vaccine, batch, feed quantity, person identity, count, or action completion.
- AI output is advisory evidence; it must not close a workflow directly.

## Category Instructions

### `vaccination_proof`

Alias: `vaccination`.

Module/page: `Vaccination` / `Vaccination`. Status: `active`. Media: `video, image_optional`.

Purpose: Prove the operator administered the claimed vaccine/medical dose to the right animal.

Required app claim properties:
- `goat_id`: `string|null`
- `rfid`: `string|null`
- `vaccine_name`: `string`
- `dose_or_stage`: `string`
- `route_site_expected`: `string|null`
- `vial_or_batch_required`: `boolean`

Gemini must verify:
- goat visible
- syringe/needle/applicator visible
- restraint adequate
- administration contact/injection moment visible
- clip continuity proves dose not setup only
- route/site plausible when supplied
- vial/batch visible when required

Failure/review reasons include:
- `wrong_category`
- `goat_not_visible`
- `instrument_not_visible`
- `administration_contact_not_visible`
- `setup_only_no_dose`
- `action_hidden`
- `ambiguous_multiple_animals`
- `route_site_contradiction`

### `weighing_proof`

Alias: `weighing`.

Module/page: `Weighing` / `Weighing`. Status: `active`. Media: `video, image_optional`.

Purpose: Prove the measured value is real and matches the operator-entered number.

Required app claim properties:
- `subject_type`: `goat|feed|container|other`
- `subject_id`: `string|null`
- `operator_entered_weight`: `number`
- `unit`: `kg`
- `tolerance_kg`: `number`
- `tare_expected`: `boolean`
- `tare_or_container_weight`: `number|null`

Gemini must verify:
- subject on scale/platform
- scale display visible
- numeric reading readable
- reading stable
- visible reading matches app value within tolerance
- no hand/foot/body/rope pressure affecting scale
- tare/gross/net supported when applicable

Failure/review reasons include:
- `wrong_category`
- `subject_not_on_scale`
- `display_not_visible`
- `reading_unreadable`
- `reading_unstable`
- `weight_mismatch`
- `tamper_or_pressure_visible`
- `tare_rule_contradiction`

### `birth_evidence`

Module/page: `Counts` / `Birth`. Status: `active`. Media: `video`.

Purpose: Prove one mother or child birth workflow task was actually performed with live-camera evidence.

Required app claim properties:
- `workflow_id`: `string`
- `subject_type`: `mother|kid`
- `subject_goat_id`: `string`
- `birth_event_id`: `string`
- `action_key`: `string`
- `action_title`: `string`
- `operator_answer`: `string|null`

Gemini must verify:
- mother/kid subject visible
- specific action_title visibly performed
- operator answer not contradicted
- live-camera proof usable
- for tag task RFID/tag visible
- for kid weight scale/reading visible
- for colostrum/milk feeding actual feeding visible

Failure/review reasons include:
- `wrong_category`
- `subject_not_visible`
- `task_action_not_visible`
- `operator_answer_contradicted`
- `tag_or_weight_unreadable`
- `static_or_unrelated_clip`

### `death_evidence`

Module/page: `Counts` / `Death`. Status: `active`. Media: `video, video_bundle`.

Purpose: Prove the ordered death video and post-mortem video are acceptable evidence after admin acceptance.

Required app claim properties:
- `workflow_id`: `string`
- `goat_id`: `string`
- `proof_sequence`: `death_video|post_mortem_video|bundle`
- `expected_video_count`: `2`

Gemini must verify:
- reported animal visible
- death condition supported, not merely resting ambiguously
- post-mortem evidence visible
- both videos present and distinguishable for bundle review
- animal/location context avoids wrong-animal review

Failure/review reasons include:
- `wrong_category`
- `goat_not_visible`
- `death_condition_not_supported`
- `post_mortem_not_visible`
- `ordered_pair_incomplete`
- `wrong_or_ambiguous_animal`
- `clip_too_dark_or_cropped`

### `shifting_move`

Alias: `shifting`.

Module/page: `Counts` / `Shifting`. Status: `active`. Media: `video, image_optional`.

Purpose: Prove directed movement happened between claimed source and destination and the visible count/cohort is plausible.

Required app claim properties:
- `shifting_event_id`: `string`
- `source_location_id`: `string`
- `destination_location_id`: `string`
- `subject_type`: `goat|cohort|shed_group`
- `subject_ids`: `array|null`
- `claimed_count`: `number|null`
- `movement_type`: `normal|quarantine|icu|pregnant|lactating|warmup|other`

Gemini must verify:
- source/destination context visible
- moved animals/cohort visible
- movement/loading/unloading/arrival/final placement visible
- destination supports claim
- visible count plausible when claimed
- high-risk movement handled safely without obvious mixing risk

Failure/review reasons include:
- `wrong_category`
- `no_movement_or_arrival_visible`
- `animals_not_visible`
- `source_destination_context_missing`
- `destination_contradicts_claim`
- `visible_count_contradicts_claim`
- `unsafe_high_risk_handling_visible`

### `milk_preparation`

Alias: `milk_prep`.

Module/page: `Milk` / `Milk Prep`. Status: `active`. Media: `video, image_optional`.

Purpose: Prove milk/colostrum preparation was done for the claimed subject/session with the right material and hygiene.

Required app claim properties:
- `workflow_id`: `string|null`
- `subject_id`: `string|null`
- `session_label`: `string`
- `claimed_volume`: `number|null`
- `unit`: `ml|l|null`
- `prep_material`: `colostrum|milk|ors|other`

Gemini must verify:
- milk/colostrum/ORS material visible
- preparation container visible
- claimed subject/session context supplied
- quantity plausible if visible
- hygiene not obviously unsafe
- not reused/unrelated footage

Failure/review reasons include:
- `wrong_category`
- `prep_material_not_visible`
- `container_not_visible`
- `quantity_contradicts_claim`
- `unsafe_hygiene_visible`
- `unrelated_clip`

### `milk_feeding`

Module/page: `Milk` / `Milk Feeding`. Status: `active`. Media: `video`.

Purpose: Prove the claimed kid/subject actually received milk/colostrum/ORS feeding.

Required app claim properties:
- `workflow_id`: `string|null`
- `subject_id`: `string`
- `session_label`: `string`
- `claimed_volume`: `number|null`
- `unit`: `ml|l|null`

Gemini must verify:
- kid/subject visible
- feeding vessel/udder/bottle visible
- actual feeding/suckling/contact visible
- session timing/subject context supports claim
- volume plausible if visible

Failure/review reasons include:
- `wrong_category`
- `subject_not_visible`
- `feeding_not_visible`
- `vessel_or_udder_not_visible`
- `wrong_or_ambiguous_subject`
- `volume_contradicts_claim`

### `feed_distribution`

Module/page: `Feed` / `Feed Distribution`. Status: `active`. Media: `video, image`.

Purpose: Prove feed was distributed to the claimed shed/pen/trough and not merely shown elsewhere.

Required app claim properties:
- `target_location_id`: `string`
- `feed_type`: `string|null`
- `claimed_quantity`: `number|null`
- `unit`: `kg|bags|null`
- `session`: `morning|afternoon|other`

Gemini must verify:
- shed/pen/trough context visible
- feed visible in trough/container
- operator distribution or completed distribution visible
- animals/context match target
- quantity plausible if claimed
- trough not empty when claim says fed

Failure/review reasons include:
- `wrong_category`
- `no_feed_visible`
- `target_context_missing`
- `distribution_not_visible`
- `empty_trough_after_claim`
- `quantity_contradicts_claim`

### `feed_packing`

Module/page: `Feed` / `Feed Packing`. Status: `active`. Media: `video, image`.

Purpose: Prove feed was packed/prepared for the claimed direction before transport/distribution.

Required app claim properties:
- `feed_type`: `string|null`
- `claimed_quantity`: `number|null`
- `unit`: `kg|bags|null`
- `batch_or_session`: `string|null`

Gemini must verify:
- feed bags/container/material visible
- packing action or final packed state visible
- quantity/count plausible
- label/batch/session visible when required
- not an unrelated trough/distribution-only clip

Failure/review reasons include:
- `wrong_category`
- `feed_material_not_visible`
- `packing_not_visible`
- `quantity_contradicts_claim`
- `batch_or_label_missing_when_required`

### `feed_transport`

Module/page: `Feed` / `Feed Transport`. Status: `active`. Media: `video, image`.

Purpose: Prove packed feed moved from packing point to target location or handoff point.

Required app claim properties:
- `source_location_id`: `string|null`
- `destination_location_id`: `string`
- `claimed_quantity`: `number|null`
- `unit`: `kg|bags|null`

Gemini must verify:
- packed feed visible
- transport/loading/unloading/handoff visible
- destination or vehicle/cart context supports claim
- quantity plausible
- not only feed already in trough without transport proof

Failure/review reasons include:
- `wrong_category`
- `packed_feed_not_visible`
- `transport_or_handoff_not_visible`
- `destination_context_missing`
- `quantity_contradicts_claim`

### `health_adults`

Module/page: `Health` / `Adults`. Status: `active`. Media: `video, image`.

Purpose: Prove adult health diagnosis/treatment/follow-up evidence for the claimed animal/action.

Required app claim properties:
- `goat_id`: `string|null`
- `rfid`: `string|null`
- `health_action`: `diagnosis|treatment|follow_up|medicine|quarantine|icu|closeout|other`
- `operator_answer`: `string|null`
- `medicine_or_symptom`: `string|null`

Gemini must verify:
- adult animal visible
- claimed symptom/treatment/follow-up action visible
- medicine/instrument visible when treatment claimed
- operator answer not contradicted
- condition severity/lameness/wound/sign visible when diagnosis claimed
- route/site plausible when supplied

Failure/review reasons include:
- `wrong_category`
- `animal_not_visible`
- `claimed_health_action_not_visible`
- `medicine_or_instrument_missing`
- `operator_answer_contradicted`
- `wrong_or_ambiguous_animal`

### `health_kids`

Module/page: `Health` / `Kids`. Status: `active`. Media: `video, image`.

Purpose: Prove kid health diagnosis/treatment/follow-up evidence for the claimed animal/action.

Required app claim properties:
- `goat_id`: `string|null`
- `rfid`: `string|null`
- `health_action`: `diagnosis|treatment|follow_up|medicine|quarantine|icu|closeout|other`
- `operator_answer`: `string|null`
- `medicine_or_symptom`: `string|null`

Gemini must verify:
- kid visible
- claimed symptom/treatment/follow-up action visible
- medicine/instrument visible when treatment claimed
- operator answer not contradicted
- condition/feeding/standing/alertness visible when relevant
- safe handling visible

Failure/review reasons include:
- `wrong_category`
- `kid_not_visible`
- `claimed_health_action_not_visible`
- `medicine_or_instrument_missing`
- `operator_answer_contradicted`
- `unsafe_handling_visible`

### `procurement_load`

Module/page: `Procurement` / `Load/Source`. Status: `future_known`. Media: `video, image`.

Purpose: Prove source loading, animal identity/count, health/source evidence, or holding-farm vaccination evidence for procurement.

Required app claim properties:
- `load_id`: `string`
- `source_party`: `string|null`
- `claimed_count`: `number|null`
- `proof_stage`: `source_entry|health_check|hf_vaccination|loading|other`

Gemini must verify:
- animals/load context visible
- claimed stage visible
- count plausible
- source/truck/location context supports claim
- health/vaccination proof visible when claimed

Failure/review reasons include:
- `wrong_category`
- `load_context_missing`
- `animals_not_visible`
- `stage_action_not_visible`
- `count_contradicts_claim`

### `transit_handoff`

Module/page: `Procurement` / `Transit`. Status: `future_known`. Media: `video, image`.

Purpose: Prove transit handoff, truck loading/unloading, or custody transfer evidence.

Required app claim properties:
- `load_id`: `string`
- `from_location_id`: `string|null`
- `to_location_id`: `string|null`
- `claimed_count`: `number|null`

Gemini must verify:
- vehicle/load visible
- handoff/loading/unloading visible
- animals or sealed cargo/feed/material visible as claimed
- location/custody context supports claim
- count plausible

Failure/review reasons include:
- `wrong_category`
- `vehicle_or_load_missing`
- `handoff_not_visible`
- `location_context_missing`
- `count_contradicts_claim`

### `arrival_intake`

Module/page: `Procurement` / `Arrival`. Status: `future_known`. Media: `video, image`.

Purpose: Prove arrival intake, accepted/rejected animals, discrepancy, health, or ownership evidence.

Required app claim properties:
- `load_id`: `string`
- `arrival_location_id`: `string`
- `claimed_count`: `number|null`
- `review_stage`: `arrival|intake|rejection|discrepancy|health|ownership`

Gemini must verify:
- arrival location/context visible
- animals visible
- review stage visible
- accepted/rejected/discrepancy evidence supports claim
- count plausible

Failure/review reasons include:
- `wrong_category`
- `arrival_context_missing`
- `animals_not_visible`
- `review_stage_not_visible`
- `count_contradicts_claim`

### `dispatch_exit`

Module/page: `Sales/Dispatch` / `Dispatch/Exit`. Status: `future_known`. Media: `video, image`.

Purpose: Prove sale/dispatch/exit loading, animal identity, readiness, and handoff evidence.

Required app claim properties:
- `dispatch_id`: `string|null`
- `goat_ids`: `array|null`
- `claimed_count`: `number|null`
- `destination`: `string|null`

Gemini must verify:
- animals visible
- dispatch/loading/handoff visible
- vehicle/destination context supports claim
- identity tags visible when required
- count plausible
- animal not visibly unfit/dead when claimed dispatch-ready

Failure/review reasons include:
- `wrong_category`
- `animals_not_visible`
- `dispatch_not_visible`
- `identity_unreadable_when_required`
- `count_contradicts_claim`
- `visible_unfit_animal`

### `attendance_checkin`

Module/page: `Workforce` / `Attendance`. Status: `future_known`. Media: `image, video_optional`.

Purpose: Prove operator attendance/check-in/location/shift evidence.

Required app claim properties:
- `workforce_member_id`: `string`
- `shift_id`: `string|null`
- `location_id`: `string|null`
- `captured_at`: `string`

Gemini must verify:
- person/selfie or required attendance evidence visible
- location/shift context supports claim when visible
- timestamp metadata supplied
- not a reused/static unrelated image
- face/person match must be human-reviewed unless a separate identity model is approved

Failure/review reasons include:
- `wrong_category`
- `person_not_visible`
- `location_context_missing_when_required`
- `reused_or_unrelated_media`
- `timestamp_missing`

### `breeding_pregnancy`

Module/page: `Breeding` / `Breeding/Pregnancy`. Status: `future_known`. Media: `video, image`.

Purpose: Prove breeding, heat, pregnancy, ultrasound, kidding-prep, or reproductive treatment evidence.

Required app claim properties:
- `goat_id`: `string|null`
- `action`: `heat|breeding|pregnancy_check|ultrasound|kidding_prep|reproductive_treatment|other`
- `operator_answer`: `string|null`

Gemini must verify:
- animal visible
- claimed reproductive action/evidence visible
- instrument/screen visible for ultrasound or treatment
- operator answer not contradicted
- identity/context sufficient

Failure/review reasons include:
- `wrong_category`
- `animal_not_visible`
- `claimed_reproductive_action_not_visible`
- `instrument_or_screen_missing`
- `operator_answer_contradicted`

### `abortion_evidence`

Module/page: `Counts/Breeding` / `Abortion`. Status: `future_known`. Media: `video, image`.

Purpose: Prove abortion/miscarriage evidence and mother condition for review.

Required app claim properties:
- `mother_goat_id`: `string|null`
- `event_id`: `string|null`
- `operator_answer`: `string|null`

Gemini must verify:
- mother visible when required
- abortion/event evidence visible enough for human review
- location/context supports claim
- operator answer not contradicted
- clip is not generic birth/death footage

Failure/review reasons include:
- `wrong_category`
- `mother_not_visible`
- `event_evidence_not_visible`
- `operator_answer_contradicted`
- `ambiguous_birth_death_or_abortion`

### `inventory_stock_proof`

Module/page: `Inventory` / `Stock`. Status: `future_known`. Media: `image, video_optional`.

Purpose: Prove feed/medicine/vaccine/equipment stock, batch, expiry, or cold-chain evidence.

Required app claim properties:
- `item_type`: `feed|medicine|vaccine|equipment|other`
- `batch_or_lot`: `string|null`
- `expiry_date`: `string|null`
- `quantity`: `number|null`
- `unit`: `string|null`

Gemini must verify:
- item visible
- label/batch/lot visible when required
- expiry visible when required
- quantity/count plausible
- cold-chain/container visible when required

Failure/review reasons include:
- `wrong_category`
- `item_not_visible`
- `batch_or_lot_missing`
- `expiry_missing_when_required`
- `quantity_contradicts_claim`
- `cold_chain_not_visible_when_required`

## Vaccination Instruction Snapshot

Purpose: Prove the operator administered the claimed vaccine/medical dose to the right animal.

Properties Gemini needs:
- `goat_id`: `string|null`
- `rfid`: `string|null`
- `vaccine_name`: `string`
- `dose_or_stage`: `string`
- `route_site_expected`: `string|null`
- `vial_or_batch_required`: `boolean`

Checks written for vaccination:
- goat visible
- syringe/needle/applicator visible
- restraint adequate
- administration contact/injection moment visible
- clip continuity proves dose not setup only
- route/site plausible when supplied
- vial/batch visible when required

It should fail or route to human review for:
- `wrong_category`
- `goat_not_visible`
- `instrument_not_visible`
- `administration_contact_not_visible`
- `setup_only_no_dose`
- `action_hidden`
- `ambiguous_multiple_animals`
- `route_site_contradiction`

## Output Contract

Gemini must emit JSON only, matching `docs/proof/gemini-video-verification-rubrics.json`. Use `pass`, `fail`, or `needs_human_review`, with confidence, purpose checked, observations, claim comparison, issues, and evidence timestamps.
