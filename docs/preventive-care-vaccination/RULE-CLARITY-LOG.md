# Preventive Care Vaccination Rule Clarity Log

This log records source/wiki/story conflicts that are being clarified before
implementation. Decided items must be reflected in PRD, TRD, rule matrix,
Config authoring handoff, seed data, and kernel behavior before the slice is
called complete.

## Decided

### 1. Mother-not-vaccinated / unknown-mother branch

Decision: GoatOS ignores this branch. Never ask about, model, seed, import,
expose, or schedule from mother-not-vaccinated / unknown-mother status. Mothers
are kept vaccinated operationally, and every kid uses the approved standard
schedule in [vaccination-rules.md](./vaccination-rules.md).

Why: This was confirmed as the final business rule on 2026-07-03. The private
source/wiki may still contain the early branch, but it is non-executable for
GoatOS.

### 2. Mixed-species drive grouping

Decision: drive planning optimizes for maximum safe doctor coverage at the
**park visit** level, with exact per-shed/tag counts retained for execution,
proof, and audit.

Rules:
- Parks contain sheds/tags; do not treat the whole park as one unsafe animal
  bucket.
- Do not treat "one shed = one tiny drive" as the final operating target.
- Combine compatible due work across sheds/tags in the same park when every
  animal stays inside its safe medical window.
- Kid goat+sheep groups can be combined when due windows, vaccine
  compatibility, max-shots-per-visit, stock, health, quarantine/ICU, and warm-up
  rules are all safe.
- Adult goat and adult sheep work stays species-specific inside the same park
  visit because adult vaccine sets differ: Goat Pox is goat-only; Sheep Pox and
  Blue Tongue are sheep-only; shared vaccines apply only where the active matrix
  allows.

Implementation note: this is a planning/sweeper target contract. It does not
mean doctors physically visit twice. It means the generated park drive plan must
show combined totals plus species-safe execution groups and shed/tag counts.

## Pending Clarification

The items below are not final until the owner confirms the exact rule.

1. Trusted source vaccination validation: trust only our parks or procurement
   holding parks under SOP/video/physical validation; decide exact DB/API proof
   fields and rejection behavior.
2. Procurement holding duration and geography: source mentions Punjab/Rajasthan
   and screenshots mention 4-5 weeks while repo docs mention 3-4 weeks; choose
   the governed duration and seed/source-holding model.
3. Breeding and milking constraints: enforce one-month hold after breeding date,
   breeding-ready prioritization, and milking-time avoidance with exact animal
   fields and scheduling behavior.
4. Pregnancy precision: derive months 4-5 skip and post-delivery catch-up from
   breeding date / pregnancy month fields, not a vague pregnant flag.
5. Manohar operating story rewrite: align story language to Preventive Care
   (PC), mother-status ignore rule, trusted-source wording, and park-level drive
   grouping.
6. Control Tower / Protocol Adherence filtered-empty and backend-filter issues.
7. Config draft version allocation: backend should own version allocation/retry,
   not frontend process memory.
