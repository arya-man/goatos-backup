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
- The KT reinforces that ration selection is not just a raw age bucket and not
  just a shed lookup. The likely nutrition key is a reviewed combination such as
  breed + tag/stage + weight/ADG or other approved policy dimensions.
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
