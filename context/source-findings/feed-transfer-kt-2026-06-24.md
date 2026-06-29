# Feed Transfer KT Findings - 2026-06-24

Status: sanitized source finding for Feed Direction implementation reference.

Source reviewed: local meeting-notes DOCX titled `Feed transfer KT - 2026_06_24
15_00 IST - Notes by Gemini.docx`. The transcript is noisy and appears to be an
auto-generated Gemini note, so treat it as directional KT evidence. Do not commit
the raw transcript or local file path.

## Findings

- The KT supports a configuration-screen workflow where feed constraints are
  uploaded or entered as reviewed tables, then future animal entries and
  downstream actions honor those constraints automatically.
- The KT should be read together with the workbook/automation finding in
  `feed-direction-workbook-automation-findings.md`: uploaded sheets are source
  inputs for typed import, review, publish, validation, and parity preview. They
  are not the runtime schema and their tab names should not become product
  module names.
- The feed/ration side is described in terms of feed unit vectors, energy
  capacity, feed-type constraints, breed, tag/stage, weight-band examples, and
  special policy groups such as warm-up or pregnant animals.
- Specific KT examples captured from the Gemini notes/transcript:
  - feed unit vectors and energy capacity, with a mentioned `80/20` packing or
    distribution factor;
  - feed type grouping across grains and dry/green leaf feeds;
  - quantity/weight examples around `400-500g` and `600g`;
  - breed/category weight examples: `F1` around `11-15kg`, `F2` around
    `15-20kg`;
  - pregnant-animal priority/scheduling discussion with a `12:30-15:00` window
    and `14:00-15:00` style session language;
  - a validation heuristic described as roughly `90-95%` shed/pack/breed/tag/
    energy matching with about `5%` warm-up allowance;
  - handheld tag readers/hardware as a possible future signal to reduce feed
    loss and improve manager/director reporting.
- The KT reinforces that ration selection is not just a raw age bucket and not
  just a shed lookup. The likely nutrition key is a reviewed combination such as
  breed + tag/stage + weight/ADG or other approved policy dimensions.
- Treat the specific numbers above as directional KT evidence, not published
  protocol values. The transcript is noisy. `80/20` must not be confused with
  the `Feed, Shiftings and Count.docx` default session split of `50/50`, and
  KT scheduling windows must not override the docx Feed Direction clocks unless
  the Feed Director explicitly approves them as policy.
- The KT does not provide authoritative shed-placement truth. It talks about
  shed/pack combination and future counts, but not enough to infer that every
  breed/tag nutrition cohort maps cleanly to a shed or that shed info is present
  in the constraint tables.
- Therefore GoatOS must keep two concepts separate:
  - physical count/projection grain: tenant + park + shed + breed + horizon;
  - ration/constraint cohort key: reviewed nutrition dimensions such as breed,
    tag/stage, weight band, energy/vector policy, warm-up/pregnancy handling, and
    feed-type constraints.
- Feed generation needs an explicit, reviewed resolver from the count projection
  row to the ration/constraint cohort key. If the projection has shed + breed but
  lacks reviewed shed tag/cohort/ration context, generation must fail closed with
  process-exception work. It must not infer the missing shed/tag relationship from
  the KT transcript or from legacy script behavior.

## Implementation Implication

Use this finding to strengthen Feed Direction gates `G2`, `G4`, and `G5`:

- `G2` must expose physical counts/projections at the agreed aggregate grain and
  indicate whether reviewed ration context resolution is present or blocked.
- `G4` must preserve source-backed ration/constraint provenance from uploaded
  tables or solver output through typed import, review, approval, publish, and
  replay metadata.
- `G5` must own the approved nutrition cohort dimensions and the policy for
  resolving a shed + breed count row into one or more ration cohorts.
- `G5` also owns session-slot policy as configurable protocol state. The docx
  default is two slots, but approved admins must be able to add, disable,
  reorder, or reweight slots in a new effective-dated protocol version.
- `G9` must turn KT validation heuristics such as `90-95%` matching and warm-up
  allowance into explicit reviewed thresholds or mark them unapproved. Do not
  let them hide as formula behavior.
- Tag-reader/hardware discussion is future identity/location/device evidence.
  It does not remove the initial aggregate-count requirement; a later device
  path must go through GoatOS device/identity/location confidence gates.
