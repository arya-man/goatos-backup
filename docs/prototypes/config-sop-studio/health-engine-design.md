# Health diagnosis workspace

## Source

Read the pinned `vgoats/health-sop` repository at `1db838d7e01642b0b62f478be100270dbe3c2279`: README, ENGINEERING, ALGORITHM, ACCEPTANCE, classes, four registers and four catalogs. Source files are retained unmodified under health-source for reproducibility. `bash check.sh` passed all 260 source reference stories (180 adult, 46 milk, 21 weaning, 13 fattening).

## Model

Diagnosis is separate from generic SOP flow and treatment scheduling. Each animal uses one class pack; applicable clinical rules match in parallel. Tier/confidence is separate from severity. Multiple Problems, covered findings, actions, rechecks, unexplained findings and Director flags remain distinct. The Director confirms or overrides; the mock never opens/closes a real Problem.

## Honest execution boundary

`health-engine-data.js` includes normalized immutable registers and catalog stories plus full recorded oracle Results. Results were obtained from the pinned reference `evaluate` and matcher; partial catalog `expect` assertions were not expanded into invented outputs. The recording includes matcher evidence/severity separately. Pipeline-generated Problems may lack matcher details, and the UI says so.

The browser selects among recorded observations. It does not accept arbitrary observations or claim to execute the complete diagnosis engine. Edited register drafts do not alter recordings. No clinical rule publication is enabled without clinical review and full changed-register acceptance evidence. The local review queue cannot fabricate this evidence or send external requests.

## Interactions

- Select all four class packs and any of the 260 recorded scenarios.
- Inspect concurrent ranked proposals, severity/confidence, matched evidence, housing, unexplained findings, emergencies, covered Problems, rechecks and Director flags.
- Record local Director confirmation or an override with required replacement decision and reason.
- Search exact source rules; edit applicability/gates, confidence clauses, severity and SOP reference into a review draft.
- Queue local acceptance review, inspect missing publication requirements, discard draft.

## Integration

Load health-engine-data.js before health-engine.js, after existing studio and course scripts. health-engine.js wraps render to add Health / Diagnosis. CSS in health-engine.css. State lives under state.healthDiagnosis. All source data remains immutable; only local draft/review/decision records persist.
