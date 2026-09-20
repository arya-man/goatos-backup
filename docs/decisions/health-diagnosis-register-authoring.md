# The diagnosis register is authored, in Health Config, in one version with the form

Maintainer decision 2026-09-21. SUPERSEDES the "committed YAML register" half of the
2026-08-14 decision recorded in `backend/internal/health/diagnosis/embed.go`, which named
itself the starting point:

> the rule table must ultimately be editable by a vet without an engineer, and the machinery
> for that already exists ... Load takes BYTES rather than a path precisely so that migration
> changes nothing in the engine: swapping these embeds for published rows is a change of
> caller, not of algorithm.

This is that migration, plus the two layers that decision did not cover.

## What changes

A health director (or the CEO) adds a symptom, adds a disease, and maps one to the other,
on `/health/config`, without an engineer and without a deploy. The treatment course for that
disease is authored on the same screen, as it already is today.

Health Config becomes the ONE place the farm's clinical rulebook lives. There is no
`/health/sops` route and no second screen: the diagnosis register is a TAB beside the
treatment protocols, because a disease with no course and a course for no disease are both
incoherent, and putting them on two screens is how they drift apart.

## Why all three layers move together, in ONE version

The engine already evaluates data, not code -- 34 adult rules and 27 per kid class in
`diagnosis/registers/*.yaml`, with no disease named anywhere in `engine.go`. Publishing those
rows alone would have delivered "add a disease over EXISTING symptoms" and nothing more,
because a symptom the form cannot ask is a token no rule can ever match. Three layers stand
between a vet and a new symptom, and only the third is data today:

1. **THE QUESTIONS** are a Go struct (`diagnosis.Findings`, ~45 fields), an OpenAPI schema, a
   Kotlin DTO and 1031 lines of hand-built Compose. Adding one question is a four-file change
   across two languages and a release.
2. **THE MAPPING** from a ticked answer to a rule token is ~150 lines of Go `switch` in
   `buildEvidence` / `deriveTokens` (`if f.Nasal { ev["nasal_discharge"] = true }`). This is
   the seam that makes a new SYMPTOM a code change even though a new DISEASE is not.
3. **THE RULES** are already authored data, version-pinned, loaded through `Load([]byte)`.

So the document carries all three, and they are published as ONE version. They are one
clinical statement: a question whose answer emits a token no rule reads is a question that
does nothing, and a rule clause naming a token no question emits is a disease that can never
be diagnosed. Two independently versioned halves would let the farm publish exactly those two
states and discover them in front of a sick animal. Publish validates BOTH directions and
refuses either.

## The document

One published version per animal class (`adult`, `kid_milk`, `kid_weaning`, `kid_fattening`),
carrying the questions, the token mapping, and the rules:

```yaml
questions:
  - id: nasal
    kind: choice
    title: Nasal discharge
    required: true
    options:
      - { value: "no",  label: "No" }
      - { value: "yes", label: "Yes", emits: [nasal_discharge] }
  - id: temp
    kind: number
    title: Temperature
    unit: degF
    bands:                       # replaces deriveTokens' hardcoded thresholds
      - { gt: 106.0,  emits: [HIGH_FEVER, FEVER] }
      - { gte: 103.5, emits: [FEVER] }
      - { lt: 100.0,  emits: [HYPOTHERMIA] }

rules:
  - id: PNEUMONIA
    pathognomonic: [{ findings: [nasal_discharge, FEVER] }]
    severity_base: 3
    treats: pneumonia            # -> a PUBLISHED health protocol, both age bands
```

`emits:` is the whole mechanism. It turns `buildEvidence` from a switch into a loop over the
published questions, and makes "add a symptom" an authoring act rather than a release.

The rule half is TODAY'S SCHEMA UNCHANGED -- `pathognomonic` / `probable` / `possible`
clauses, `gate_required`, `severity_modifiers`, `explains_findings`, `suppresses`,
`residual`. The four committed YAMLs are seeded verbatim as published v1. Nothing about the
algorithm changes; the caller changes.

## `treats:` replaces a string-munged guess, and that is a fix, not a rename

Today a rule reaches its treatment course through `sop_ref` -> `SOPRefToDiseaseKey()`, which
lowercases, swaps spaces for underscores and consults an alias map. A vet adding `sop_ref:
Pneumonia` would be silently relying on that derivation landing on a `disease_key` that
exists. When it does not, the failure is a course that never opens.

The authored form names the protocol EXPLICITLY, and PUBLISH VALIDATES IT: a rule whose
`treats` names no published protocol in both age bands is REFUSED at publish, not discovered
at diagnosis. A rule with no `treats` is a field action (today's `sop_ref: Field`), which is
legitimate and stays legitimate. `SOPRefToDiseaseKey` survives only to read runs recorded
before this change.

## Validate-or-reject at publish

Following `/health/config`'s existing authoring contract exactly (draft -> publish -> retire,
content-hashed no-op detection, write-log ledger). Publish REFUSES:

- a rule clause naming a token NO question emits (a disease that can never fire);
- a question option emitting a token NO rule reads (a question that does nothing) -- a
  WARNING rather than a refusal only where the token is declared in `vocabulary`, which is
  how a vet stages a symptom before the rule that uses it;
- a `treats` naming a disease that is not in the catalog AT ALL. That is a typo, and it
  leaves a diagnosis that fires and then cannot open a course;
- a duplicate rule id, question id, or option value;
- a number question with a band an EARLIER band already covers entirely. Bands are
  first-match-wins, so authoring them least-severe first ("103.5 and up is a fever",
  then "106 and up is a high fever") leaves the high fever permanently unreachable --
  a silent under-read of the sickest animals. Overlap itself is legitimate and is how
  the seeded registers are written, and a GAP is legitimate too: it is how a normal
  temperature emits nothing at all;
- an unknown key anywhere. `Load()` already rejects unknown fields and must keep doing so: a
  key nothing reads is an accept-and-discard that reads to the next author as honoured.

### A disease whose course nobody has written yet is a WARNING, not a refusal

This is the correction the build forced, and it matters. Nine of the shipped register's
thirty diagnoses point at treatment cards nobody has authored -- `SOPRefToDiseaseKey`'s
own comment says so. Refusing a publish on that basis would make the farm's EXISTING
rulebook unpublishable on day one, which is not a safety gate but a lockout.

So the two cases are separated by what they mean rather than by how they look:

```text
treats names no disease in the catalog     FATAL     a typo; the course can never exist
treats names a disease with no live card   WARNING   the card has not been written yet
```

The warning rides the publish response and is printed by the seed command, because a
publish that ALLOWED something and then said nothing about it is how the nine became
invisible in the first place.

### A check that was tried and removed

"The form never names a disease" is a real rule, and refusing a question whose title
matches a rule id looked like the way to enforce it. It is not: this register
deliberately carries SYMPTOM-LABEL rules -- `RED_URINE`, `WOUNDS`, `LUMPS`, `TICKS`,
`FEVER` -- whose job is to surface a finding no diagnosis accounted for. Their ids ARE
sign names, so the check refused four of the farm's own questions for being named after
the signs they record. A guard that refuses the correct register is worse than no guard.
It stays a review rule.

## Two defects the authoring surfaced, both of them rules that could never fire

Neither was known before the two-direction check was written, and both had been sitting
in committed files:

1. **SKIN** lists hair loss on the body, the NECK and the LEGS as three separate probable
   clauses. The form carried ONE boolean, emitting only `skin_coat:hairloss_body`, so two
   of those clauses were unreachable and hair loss on a goat's neck could not be recorded
   at all.
2. **NEURO** matches pathognomonically on `neuro:seizure` in every kid register, which
   also declares it in vocabulary -- and the form offered no way to tick it. A fitting kid
   could only ever be recorded as something else.

Both are repaired in the seeded form. An observation already taken is unaffected: the old
boolean maps to exactly the answer it used to mean.

## What is NOT yet authored, and why the job is not finished

The mapping layer is data. The DOWNSTREAM CLINICAL PIPELINE is not: emergency detection,
housing, the kid compiler, drug rules and the clinical flags read the typed `Findings`
struct directly (`f.Diarrhea.Set`, `f.notEating()`, `f.Nasal`, `f.LockedJaw`). Those
specific fields therefore remain load-bearing, and a vet who DELETES one of those
questions changes what the rules see while that code keeps reading a zero value.

Adding a question is safe today. Renaming or removing one of the fields that pipeline
reads is not. Closing that gap means porting those paths to read the evidence set instead
of the struct -- a real piece of work on clinical code, and the next slice.

## What stays in code, deliberately

Not everything in the diagnosis path is symptom-to-disease, and authoring the parts that are
not would make them LESS safe, not more:

- **The kid crash and refusal ladders** read HISTORY, not symptoms (`refusals_today`,
  `session`, `cmt_neg_streak`, `nad_prior_7d`, K1-vs-K2 first-miss semantics). They are
  follow-up logic over a context the form does not carry.
- **Cross-form exclusivity and the every-field-compulsory rule.** A blank cannot distinguish
  "nobody looked" from "normal", and the unexplained-findings channel -- the thing that
  catches what the diagnosis failed to account for -- depends on that distinction.
- **Species, sex, stage and class**, which come from the herd register and never from the
  form, so a manager cannot retype them.
- **The ranking pipeline itself**: tier over severity, residual resolution, suppression. The
  register says WHICH rules exist; the engine decides how they compete.

## Every run still pins what produced it

`health_diagnosis_runs.register_version` already exists and already pins the rule table per
run. It now pins an authored version id instead of a filename, so an old proposal stays
interpretable after a vet edits the table -- and `health_cases.health_protocol_version_id`
keeps pinning the treatment separately, so a dosage correction does not re-date a diagnosis
and a rule fix does not rewrite a course being administered. The two pins were designed to
move independently and still do.

## Authority

`health.config.read` / `health.config.write`, unchanged: `ceo_internal` and `health_director`
only. Deliberately NOT `operator` (executes a course, does not author it), NOT `park_head`,
and NOT `verifier` (separation of duty: the verifier must not rewrite the standard the work
is judged against). `pc_director` holds nothing here -- preventive care and health are
separate departments and merging them is prohibited.

This is the same permission that already governs the treatment protocols on this screen, and
that is the point: one rulebook, one authority.

## The proof that the migration changed no clinical behaviour

`diagnosis/testdata/catalog*.json` is a 180-story acceptance catalog pinning today's four
registers story by story. Seeding those YAMLs as published v1 and re-running the SAME
catalogs through the AUTHORED path -- questions, mapping and rules all read from the
published version -- is the regression test. A single story diverging means the seed is not
faithful, and the seed is what every farm starts from.

Mutation test: hand-edit one seeded register row and confirm the catalog goes red.
