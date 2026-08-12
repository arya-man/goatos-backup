# Gemini Video Verification Rubric

Status: proposed production rubric for AI-assisted verifier review.

This document defines what Gemini must check when Goat OS sends proof videos for
Feed Direction, Weighing, and Vaccination verification. Gemini is an assistant
to the generic verification module; it does not approve work by itself. The
human Verifier role still owns the final approve/reject decision, and authority
roles own follow-up action.

## Source Sample

The rubric is based on a `goatos-stg` GCS review from
`gs://goatos-stg-media` on 2026-08-12. GCS object paths identify Feed Direction
legacy media directly, but the UUID proof stream does not encode the app
category in the object name. For Weighing and Vaccination, category grouping was
done by visual content from the latest visible clusters:

- Vaccination: latest injection/medical-action clips around 13:15-14:22 UTC.
- Weighing: latest scale-display and weighing-platform clips around 05:18-05:20 UTC.
- Feed Direction: latest MP4 clips under `legacy/slack/feed/2026/08/12/`.

## Verification Principles

- Check proof against the expected category and app-recorded claim, not as a
  generic video summary.
- Require visible evidence for the critical action. A video that only shows the
  operator before or after the action is not enough.
- Prefer `needs_human_review` when the clip is dark, shaky, occluded, cropped,
  or missing the app claim needed for comparison.
- Report specific reasons; do not hide uncertainty behind a pass.
- Never infer goat identity, shed, weight, vaccine, batch, or feed quantity if
  it is not visible in video or supplied in metadata.
- Treat the model verdict as advisory. The verifier queue must preserve the raw
  model output, confidence, and evidence timestamps for human review.

## Required Input Envelope

Every Gemini request should include:

- `verification_item_id`
- `category`: `feed_direction`, `weighing`, or `vaccination`
- `video_uri` or uploaded Gemini file reference
- `tenant_id`, `park_id`, `shed_id`, and operator identity if available
- capture timestamp and expected business date
- category-specific app claim:
  - Feed Direction: expected proof stage, feed type, target shed/pen, target
    quantity, and whether this is packing, distribution, water, or leftover
    proof.
  - Weighing: subject type, animal/RFID if available, operator-entered weight,
    unit, and whether tare/container weight is expected.
  - Vaccination: goat/RFID if available, vaccine name, dose/stage, route/site
    expectation if configured, and whether vial/batch proof is required.

## Feed Direction Checks

Feed Direction proof is about whether the feed/water work happened for the
right location and expected stage. The reviewed samples showed troughs, shed
context, goats near feeding areas, and feed/water containers rather than a
single numeric scale claim.

Gemini must check:

- correct category: clip is feed, water, packing, distribution, or leftover
  evidence, not vaccination/weighing/random shed footage.
- location context: shed/pen/trough or animals are visible enough to support the
  claimed target.
- material evidence: feed, water, bag, container, trough, or distribution setup
  is visible as expected for the stage.
- action evidence: operator is visibly placing, showing, or completing the feed
  or water proof when the stage requires action, not only an empty trough.
- quantity plausibility: visible amount roughly matches the app claim when a
  quantity is provided; otherwise mark `quantity_unverifiable`.
- freshness: the clip is not a static, reused, or obviously unrelated scene.
- failure conditions: empty/unclear trough, wrong category, no feed/water
  visible, no shed context, unreadable/cropped proof, or only goats with no
  feed-work evidence.

## Weighing Checks

Weighing proof is about whether the measured value is real and matches the
operator-entered number.

Gemini must check:

- correct category: weighing platform, scale, display, animal, feed/container,
  or weighed item is visible.
- subject evidence: the goat/feed/container being weighed is visible and
  plausibly matches the app claim.
- scale evidence: the scale display is visible and readable enough to extract a
  number.
- stable reading: reading is not mid-change, blurred, blocked, or flickering.
- claim comparison: extracted scale number matches `operator_entered_weight`
  within configured tolerance.
- tamper check: no hand, foot, rope, body pressure, leaning, or partial
  placement is visibly affecting the scale.
- tare/container check: if a bag/container is weighed, the expected tare rule or
  gross/net claim must be supplied; otherwise mark `tare_unverifiable`.
- failure conditions: no visible display, unreadable number, unstable reading,
  subject not on scale, obvious pressure/tampering, or mismatch with app value.

## Vaccination Checks

Vaccination proof is about whether the operator actually administered the
proper vaccine action to the goat.

Gemini must check:

- correct category: syringe/needle/applicator, goat body, and medical action are
  visible.
- goat evidence: goat is visible enough to verify that the action is on an
  animal, not only on a handler's hands or ground.
- restraint evidence: goat is restrained well enough for a real dose.
- instrument evidence: syringe/needle/applicator is visible before or during
  contact.
- administration evidence: contact or injection moment is visible on the goat;
  merely showing a syringe near the goat is not enough.
- dose continuity: clip is not cut before contact or after setup only.
- site plausibility: contact point is plausible for the configured route/site
  when route/site metadata is provided.
- vial/batch evidence: only required when the input says it is required; if not
  visible, report `vial_or_batch_not_visible` without failing solely on that
  field.
- failure conditions: syringe not visible, no contact, action hidden by hand,
  wrong category, clip too dark, ambiguous multiple animals, or no complete
  administration moment.

## Top-10 Sample Observations

### Feed Direction

Sample path set: latest 10 MP4s under
`legacy/slack/feed/2026/08/12/`.

Observed patterns:

- Some clips show empty or filled trough areas with shed context.
- Some clips show goats near troughs or inside the feed area.
- Several clips provide location/context proof but do not by themselves prove a
  numeric feed quantity.
- Gemini should identify stage and visible evidence, then mark quantity claims
  unverifiable unless the app supplies expected quantity and video shows enough
  material evidence.

### Weighing

Sample path set: latest visually identified weighing cluster around
`2026-08-12T05:18Z-05:20Z`.

Observed patterns:

- Several clips show a digital scale display and weighing platform.
- Some clips show animals on a platform; others show feed/container material on
  a scale.
- Display readability varies. Gemini must extract the visible value only when
  readable, and compare it to the app value supplied in metadata.
- If the display is visible but cropped, overexposed, or unstable, the correct
  verdict is `needs_human_review`.

### Vaccination

Sample path set: latest visually identified vaccination cluster around
`2026-08-12T13:15Z-14:22Z`.

Observed patterns:

- Many clips show goat restraint and syringe/injection handling.
- Several clips are dark, close-up, or partially occluded by hands.
- Gemini should focus on the actual administration moment, not only the presence
  of a syringe.
- If the needle/contact point is hidden, report the reason and use
  `needs_human_review` unless the clip clearly proves administration elsewhere.

## Output Contract

Gemini must emit JSON only, matching
`docs/proof/gemini-video-verification-rubrics.json`. Use:

- `pass` only when the critical action and claim comparison are visible enough.
- `fail` when visible evidence contradicts the claim or proves the action did
  not happen properly.
- `needs_human_review` for dark, shaky, incomplete, ambiguous, cropped,
  metadata-missing, or low-confidence clips.

Suggested confidence policy:

- `>= 0.85`: strong model confidence.
- `0.65-0.84`: usable but should be sampled by human QA.
- `< 0.65`: route to human review.

The model output must not close a workflow directly. It should be stored as
AI evidence on the verification item and shown to the human Verifier.
