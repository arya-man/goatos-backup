# CEO-AI answer-quality eval harness

Golden-question regression for the Mesha leadership assistant. It runs a fixed
set of leadership questions **live** through the assistant endpoint, scores each
answer against an **independent Postgres oracle**, and emits a self-contained
HTML report plus a JSON report.

Full operator guide: [`docs/ceo-ai/eval.md`](../../../docs/ceo-ai/eval.md).

## Why this design

- **Deterministic scoring on a non-deterministic answer.** The LLM answer varies
  run to run, but `scoreQuestion(question, response, oracle)` is a pure function —
  no I/O — so the verdict for a given answer is always the same. The scorer is
  unit-tested in `golden_test.go` with zero external dependencies.
- **Independent oracle.** Ground-truth numbers come from SQL over canonical
  `public.*` tables (`goats`, `locations`, …), never the `ceo_ai.*` views the
  assistant reads. The oracle is a genuine second computation, not a mirror of
  the code under test. A grounded answer must contain the oracle's number.
- **stdlib-only.** The oracle runs through `psql` (libpq) on stdin, so no Go
  Postgres driver is vendored and the module builds/tests with the standard
  library alone.
- **Opt-in only.** The live run needs a real assistant (Vertex) and a database.
  It never runs in the default local/PR gate; CI runs only the cheap structural
  self-test and skips the live run loudly (never a silent pass).

## Layout

| File | Role |
|------|------|
| `types.go` | Contract value types (question, expectations, response, oracle, result, metrics). |
| `golden.go` | Loads + validates the golden set (schema, read-only oracle, tier vocab, floors). |
| `oracle.go` | Runs oracle SQL through `psql`; parses scalar / label\|value rows. |
| `assistant.go` | HTTP client for the assistant endpoint. |
| `score.go` | Pure deterministic scorer + metrics fold (grounding, refusal, tool-selection, latency percentiles). |
| `report.go` | Self-contained HTML (E2E report contract) + JSON emit. |
| `main.go` | CLI: flags, prerequisite gate (loud skip / strict fail), orchestration. |
| `golden/*.json` | The golden question set (>= 40 Qs across every GenAI class + adversarial refusals). |
| `golden_test.go` | Validates the committed golden set + unit-tests the scorer. |

## Commands

```bash
# Structural self-test — no assistant, no DB, no Vertex. This is what CI runs.
make ceo-ai-eval-selftest

# Validate the golden set only.
cd tools/ceo-ai/eval && go run . -validate

# Live answer-quality regression (writes out/ceo-ai-eval-report.{html,json}).
export MESHA_ASSISTANT_URL=...        # server-side assistant endpoint
export MESHA_EVAL_BEARER=...          # leadership-gated session token
export GOATOS_EVAL_DATABASE_URL=...   # read-only DSN for the oracle
export GOATOS_EVAL_TENANT_ID=...      # tenant to score against
make ceo-ai-eval
```

## Adding a golden question

Append an object to the relevant `golden/<domain>.json` array:

```json
{
  "id": "kebab-case-unique",
  "class": "one_of_the_genai_query_classes",
  "question": "Plain-English leadership question.",
  "notes": "Why this matters / what shape the answer must take.",
  "expect": {
    "grounded": true,
    "aggregate_first": true,
    "ist": true,
    "tiers_any_of": ["cube", "api"]
  },
  "oracle": {
    "kind": "scalar_int",
    "sql": "SELECT count(*) FROM goats WHERE tenant_id = :'tenant_id'::uuid AND exited_at IS NULL"
  }
}
```

Rules the validator enforces (so `make ceo-ai-eval-selftest` fails on a mistake):

- ids are unique kebab-case; every question asserts at least one scored property.
- `grounded` / `species_split` / `injection_forbid_leak` require an `oracle`.
- every oracle is a single read-only `SELECT`/`WITH`, tenant-scoped via
  `:'tenant_id'`, with no DML/DDL/multi-statement tokens.
- tiers are from `cube|api|toolbox|sql`; the set keeps >= 40 questions / >= 20 classes.

The tenant is bound at runtime as the psql variable `:'tenant_id'` — write oracle
SQL against canonical tables and reference `:'tenant_id'::uuid` for the scope.
