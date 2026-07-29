#!/usr/bin/env node
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

function readJson(input) {
  const path = input instanceof URL ? input : new URL(input, import.meta.url);
  return JSON.parse(readFileSync(path, "utf8"));
}

function byId(rows, key) {
  return new Map((rows ?? []).map((row) => [row[key], row]));
}

function expectAccess(persona, expected) {
  for (const [action, allowed] of Object.entries(expected)) {
    assert.equal(
      persona.expected_access?.[action],
      allowed,
      `${persona.code}: expected_access.${action} must be ${allowed}`,
    );
  }
}

export function validateWeighingFixture(fixture) {
  assert.equal(fixture.campaign?.cadence_type, "weekly_kids", "v1 campaign must be weekly_kids");
  assert.equal(fixture.campaign?.animal_group_filter, "kids_k_f", "v1 campaign must be kids_k_f only");
  assert.equal(fixture.campaign?.operator_code, "amit_operator", "Amit is the only v1 execution operator");

  const personas = byId(fixture.personas, "code");
  expectAccess(personas.get("ravi_ceo"), { create: true, edit: true, publish: true, monitor: true, execute: false });
  expectAccess(personas.get("dinakar_director"), { create: false, edit: false, publish: false, monitor: true, execute: false });
  expectAccess(personas.get("amit_operator"), { create: false, edit: false, publish: false, monitor: false, execute: true });
  expectAccess(personas.get("unauthorized_viewer"), { create: false, edit: false, publish: false, monitor: false, execute: false });
  assert.deepEqual(personas.get("dinakar_director")?.capabilities, ["weighing.monitor"], "Dinakar must be monitor-only");

  const scopes = fixture.selected_scopes ?? [];
  assert.ok(scopes.some((scope) => scope.weighing_category === "individual_animal"), "fixture needs individual-animal scope");
  assert.ok(scopes.some((scope) => scope.weighing_category === "per_shed_partition"), "fixture needs per-shed/partition scope");

  const scopeById = byId(scopes, "campaign_shed_id");
  for (const group of fixture.work_groups ?? []) {
    assert.ok((group.campaign_shed_ids ?? []).length > 0, `${group.work_group_id}: group must preserve selected-scope membership`);
    for (const campaignShedId of group.campaign_shed_ids ?? []) {
      assert.ok(scopeById.has(campaignShedId), `${group.work_group_id}: unknown campaign_shed_id ${campaignShedId}`);
    }
  }
  assert.ok(
    (fixture.work_groups ?? []).some((group) => group.status === "delayed" && group.effective_business_date > fixture.campaign.period_end_date),
    "fixture must include delayed roll-forward beyond the campaign week",
  );

  const animalById = byId(fixture.animals, "animal_id");
  const proofById = byId(fixture.proof_artifacts, "proof_artifact_id");
  const acceptedAnimalObservationIds = new Set();
  let wrongShed = 0;
  let notInCampaign = 0;
  let offPage = 0;
  for (const observation of fixture.observations ?? []) {
    const animal = animalById.get(observation.animal_id);
    assert.ok(animal, `${observation.observation_id}: observation animal must exist`);
    const proof = proofById.get(observation.proof_artifact_id);
    assert.ok(proof, `${observation.observation_id}: proof artifact must exist`);
    assert.equal(proof.subject_scope, "animal", `${observation.observation_id}: individual observation proof must be animal scoped`);
    assert.equal(proof.proof_mode, "per_animal_video", `${observation.observation_id}: individual observation requires per-animal video`);
    assert.ok(observation.weight_kg > 0, `${observation.observation_id}: weight must be positive`);
    assert.ok(observation.idempotency_key?.includes(fixture.campaign.campaign_id.slice(0, 8)), `${observation.observation_id}: idempotency key must include campaign identity`);
    if (observation.location_match_status === "other_shed") {
      wrongShed += 1;
      assert.ok(observation.expected_location_label, "wrong-shed scan must keep expected/original shed");
      assert.ok(observation.actual_location_label, "wrong-shed scan must keep actual/current shed");
    }
    if (observation.location_match_status === "not_in_campaign") notInCampaign += 1;
    if (observation.off_page_scan) offPage += 1;
    if (observation.status === "accepted" && animal.expected_campaign_shed_id) {
      assert.ok(!acceptedAnimalObservationIds.has(observation.animal_id), `${observation.animal_id}: duplicate accepted animal observation`);
      acceptedAnimalObservationIds.add(observation.animal_id);
    }
  }
  assert.ok(wrongShed >= 1, "fixture must cover wrong-shed scan");
  assert.ok(notInCampaign >= 1, "fixture must cover not-in-campaign scan");
  assert.ok(offPage >= 1, "fixture must cover off-page scan");

  const observationById = byId(fixture.observations, "observation_id");
  assert.ok((fixture.duplicate_scans ?? []).length >= 1, "fixture must cover duplicate scan/idempotent replay");
  for (const duplicate of fixture.duplicate_scans ?? []) {
    const original = observationById.get(duplicate.original_observation_id);
    assert.ok(original, `${duplicate.scan_id}: duplicate scan must reference an original observation`);
    assert.equal(duplicate.animal_id, original.animal_id, `${duplicate.scan_id}: duplicate scan animal must match original observation`);
    assert.equal(duplicate.idempotency_key, original.idempotency_key, `${duplicate.scan_id}: duplicate replay must reuse the original idempotency key`);
    assert.equal(duplicate.expected_result, "idempotent_replay", `${duplicate.scan_id}: duplicate scan must be an idempotent replay`);
    assert.equal(duplicate.progress_delta, 0, `${duplicate.scan_id}: duplicate scan must not increment progress`);
    assert.equal(duplicate.must_not_create_second_observation, true, `${duplicate.scan_id}: duplicate scan must not create a second observation`);
  }

  const unavailableTruths = new Set(["icu", "quarantine", "dead", "culled", "sold_transferred", "exited"]);
  assert.ok(
    (fixture.animals ?? []).some((animal) => animal.expected_status === "unavailable" && unavailableTruths.has(animal.current_truth)),
    "fixture must classify unavailable animals from current herd truth",
  );

  for (const shedObservation of fixture.shed_observations ?? []) {
    const scope = scopeById.get(shedObservation.campaign_shed_id);
    assert.equal(scope?.weighing_category, "per_shed_partition", `${shedObservation.shed_observation_id}: shed observation must target per-shed/partition scope`);
    const proof = proofById.get(shedObservation.proof_artifact_id);
    assert.equal(proof?.subject_scope, "shed_partition", `${shedObservation.shed_observation_id}: shed observation proof must be shed/partition scoped`);
    assert.equal(proof?.proof_mode, "shed_partition_video", `${shedObservation.shed_observation_id}: shed observation requires shed/partition video`);
    assert.equal(shedObservation.must_not_create_individual_weights, true, "per-shed observation must not create individual weights");
    assert.equal(shedObservation.must_not_update_latest_trusted_animal_weight, true, "per-shed observation must not update animal latest trusted weight");
  }

  assert.ok(
    (fixture.proof_artifacts ?? []).some((proof) => proof.upload_state === "accepted_after_retry" && proof.retry_attempts >= 1),
    "fixture must cover proof upload retry",
  );
  assert.ok(
    (fixture.proof_artifacts ?? []).some((proof) => proof.upload_state === "uploaded_unsubmitted_removed_before_acceptance" && proof.removal_required_endpoint),
    "fixture must cover uploaded-but-unsubmitted proof removal",
  );

  const mobile = fixture.mobile_contract ?? {};
  assert.equal(mobile.room_first, true, "mobile must be Room-first");
  assert.equal(mobile.outbox_idempotent, true, "mobile must use idempotent outbox");
  assert.equal(mobile.process_death_safe, true, "mobile capture must survive process death");
  assert.ok((mobile.sign_out_wipe_tables ?? []).includes("weighing_outbox"), "sign-out wipe must include weighing outbox");
  assert.deepEqual(mobile.rfid_terminators_swallowed_only_on_routes, ["WeighingScanRoute"], "Enter/Tab swallow must be scoped to Weighing scan route only");
  assert.equal(mobile.off_page_scan_expected?.must_update_scan_feed_without_fetch_all, true, "off-page scan must update feed without fetching all animals");

  const scale = fixture.scale_profile ?? {};
  assert.ok(scale.expected_campaign_animals >= 5000, "scale profile must cover 5k+ animals");
  assert.ok(scale.visible_page_size <= 20, "visible page size must stay phone-sized");
  for (const forbidden of ["load_all_animals_for_progress", "linear_scan_rfid_lookup", "visible_page_totals_as_campaign_totals", "order_by_limit_1_for_membership"]) {
    assert.ok((scale.forbidden_read_shapes ?? []).includes(forbidden), `scale profile must forbid ${forbidden}`);
  }

  const p = fixture.expected_progress ?? {};
  assert.equal(
    p.individual_expected_total,
    p.individual_weighed_expected + p.individual_pending_expected + p.individual_unavailable_expected + p.individual_closed_by_leadership,
    "individual progress buckets must be disjoint and complete",
  );
  assert.equal(
    p.per_shed_partition_selected_total,
    p.per_shed_partition_completed + p.per_shed_partition_pending + p.per_shed_partition_proof_blocked + p.per_shed_partition_closed_by_leadership,
    "per-shed progress buckets must be disjoint and complete",
  );
  assert.equal(p.wrong_shed_expected, wrongShed, "wrong-shed expected count must match observations");
  assert.equal(p.not_in_campaign_scans, notInCampaign, "not-in-campaign count must match observations");

  const steps = fixture.e2e_steps ?? [];
  for (const required of ["CEO creates", "Dinakar", "Amit", "Duplicate", "Availability", "rolls forward"]) {
    assert.ok(steps.some((step) => step.includes(required)), `e2e_steps must cover ${required}`);
  }
}

export function validateWeighingFixtureFile(inputUrl = new URL("../../fixtures/weighing-e2e-2026-07-29/weighing-seed.json", import.meta.url)) {
  validateWeighingFixture(readJson(inputUrl));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  validateWeighingFixtureFile();
  console.log("weighing E2E fixture contract: ok");
}
