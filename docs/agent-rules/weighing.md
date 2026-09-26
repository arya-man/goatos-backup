# Weighing Agent Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Weighing Is ISOLATED — No Herd, No Vaccination, No Exceptions (Claude AND Codex)

Weighing owns its own tables and reads NOTHING from another module's schema, in
either direction, on ANY path — writes, reads, reports, read models, exports,
analytics. It is not "free-flow on the write path". It is isolated.

BANNED on every path: `goats`, `goat_identifiers`, `herd_*`, `vaccination_*`,
`sop_*`, `protocol_*`, `obligation_*` — anything describing an ANIMAL or another
module's rules. Weighing knows a scanned string and a weight. It does not know
what animal that is and must never ask.

RECORDED REPORTING EXCEPTIONS (maintainer decisions 2026-08-07 and 2026-08-19). The admin-web Weights
screen reports average weight by BREED, SEX and MANAGEMENT STAGE. Those three
facts live only on the animal, so exactly one file may resolve a scanned tag:
`backend/internal/weighing/adapters/postgres/weight_demographics.go`, allowlisted
BY NAME in `check-weighing-free-flow-guard.mjs` (`HERD_JOIN_EXEMPT_FILES`) and
permitted `goats` + `goat_identifiers` for same-animal reporting only. The same
file may read `goat_shed_partitions` only to label lump-sum Weights read-model
rows by the exact `(shed, partition)` resident cohort (`Godel 2 - Part 1`,
`Castro 1/2/3`, `Gandhi 1/2/3`, legacy `Gandi 1/2/3`). It must not use that
table to gate capture, submit, close, expected animals, or any write path.
Everything else stays banned, on
every path, in every other weighing file — the exemption is file-scoped precisely
so it cannot leak to the write path, which is the 2026-08-04 defect.

What keeps it safe, and what a future change must preserve: it is READ-ONLY; it
is a reporting path with no capture, submit or close behaviour; NO scan is gated
on identity; and a tag that resolves to nothing is COUNTED and reported, never
rejected — free-flow capture is untouched. The same file may return row context
chips such as "F2 / female" or "Anantapur Sheep / male" for the admin-web
Weights table; those chips label the weighed shed row and must not become a
write-path lookup or validation rule. A whole-shed weigh has no tags and is
attributed by the shed's own cohort only when that cohort is homogeneous for the
reported dimension. Mixed whole-shed averages may be labelled with multiple
breed/sex chips, but are never split across breed or sex buckets, because
splitting one shed average across a mix invents a distribution nobody measured.
Widening this exemption — another file, another table, or any write path — is a
MAINTAINER decision, never a developer convenience.

SECOND RECORDED EXCEPTION (maintainer decision 2026-08-24): the LUMP-SUM CENSUS
SNAPSHOT. Operators kept typing wrong lump-sum head counts, so the operator no
longer enters one: `RecordShedObservation` snapshots the bucket's live resident
count from `goats` + `goat_shed_partitions` INSIDE the submit transaction via
exactly one file — `backend/internal/weighing/adapters/postgres/lump_sum_census.go`,
allowlisted BY NAME in `check-weighing-free-flow-guard.mjs` — stores it frozen on
`weighing_shed_observations.animal_count`, and derives the average from it. The
snapshot never changes afterwards: herd moves do not recompute it, replays return
the original, and the verifier's weight correction is WEIGHT ONLY on both grains
(a correction naming a count is refused, `animal_count_not_applicable`; the
verification spec no longer declares a count field). A register-empty bucket
refuses the submit (422 `shed_count_unavailable`) rather than inventing a count.
This is knowingly a WRITE-PATH read and is recorded as such; its boundaries — one
COUNT of the bucket's own (shed, pen), no per-animal identity, individual
free-flow capture untouched — are stated in the guard header and the census file
itself. Canonical prose: `docs/decisions/weighing-lump-sum-census-count.md`.

THIRD RECORDED EXCEPTION (maintainer decision 2026-08-26): the WEIGHTS SEX FILTER. The
admin-web Weights page carries a **Sex** filter in its own filter bar, beside Weighing, and it
governs the WHOLE page — every KPI, the shed table, both leaderboards, the load chart, the
Growth Director widgets and the breed gain card. A page whose cards disagree about which kids
they counted has no true number on it, which is why this is a page filter and not a card
control. A weighing row knows only a scanned string, so exactly one more file may resolve it:
`backend/internal/weighing/adapters/postgres/sex_scope.go`, allowlisted BY NAME in
`check-weighing-free-flow-guard.mjs`.

That file answers "which weighs belong to this sex" ONCE and hands the other reads an OPAQUE
list — tag strings and (location, partition) buckets — so `shed_weights.go`, `growth.go`,
`load_weights.go` and the Growth Director reads still name no herd table and still know nothing
about animals. Letting each of them join `goat_identifiers` instead is exactly the leak the
2026-08-04 defect was about. It is READ-ONLY and REPORTING-ONLY: no capture, submit, close or
verdict path calls it, NO scan is gated on identity, and an empty sex resolves to an empty scope
that every caller reads as "no filter", so the unfiltered page runs the query it ran before this
file existed and reads no goat row at all.

An individual weigh is claimed through the animal its tag resolves to. A WHOLE-SHED weigh has
no tag and is claimed only when its shed's resident cohort is entirely that sex — the
maintainer's own rule is that a lump-sum shed holds one sex — and a shed the register shows as
mixed is claimed by NEITHER side rather than split, because one shed average cannot be divided
between two cohorts. A tag that resolves to nothing is still recorded and still counted in the
unfiltered view; it simply cannot answer a question about sex, so the filtered halves do not add
up to the unfiltered total, and that gap is honest rather than missing data.

FOURTH RECORDED EXCEPTION (maintainer decision 2026-09-01): the WEIGHTS ORIGIN FILTER, FARM BORN
vs PURCHASED. Beside Weighing and Sex, and governing the WHOLE page on the same terms: the farm
both breeds its own kids and buys them in loads, and the two grow differently enough that reading
them together answers nothing. Exactly one more file may resolve identity,
`backend/internal/weighing/adapters/postgres/origin_scope.go`, allowlisted BY NAME in
`check-weighing-free-flow-guard.mjs`.

IT DID NOT NEED AN EXCEPTION AT FIRST, and why it does now is the whole rule. Origin looked like a
fact about a PEN -- the farm buys a load and puts it in a shed -- and weighing already owns that
mapping in `weighing_shed_load_tags` (000131), so the first version read no herd table at all. That
is correct for the seven pens whose every resident came off a load (CBE Castro 1/2/3, CPT Castro
1/2, CPT Godel 2 - Part 1/2). It is WRONG for a MIXED pen: CPT Mandela 1 - Part 1 holds 13 kids of
which only FOUR were bought, and judging a scanned weigh by its pen filed all 12 of that pen's
scanned kids as purchased. The maintainer caught it on the first run.

So the rule is PER ANIMAL where the evidence allows it. A SCANNED weigh carries a tag, so it is
claimed through the animal that tag resolves to, and `procurement_load_goats` -- allowlisted for
THIS FILE ONLY -- is the only table that says which animal came off which load. A WHOLE-SHED weigh
carries no tag, so it is claimed through its pen and ONLY when every live resident agrees: all
bought, or none. A mixed pen is claimed by NEITHER side, because one average weight cannot be
divided between two cohorts and claiming it whole is the same defect one grain up. This is the
identical agree-or-neither shape the Sex filter uses for a shed holding both sexes.

The guard's table allowlist is now keyed PER FILE, precisely so this cannot leak: a procurement
table is legal in `origin_scope.go` and still a finding in `sex_scope.go`, `weight_demographics.go`
and `lump_sum_census.go`, none of which has any business asking where an animal was bought. That
per-file scoping has its own adversarial self-test.

It is READ-ONLY and REPORTING-ONLY: no capture, submit, close or verdict path calls it, NO scan is
gated on origin, and an empty origin resolves to an empty scope every caller reads as "no filter",
so the unfiltered page runs the query it ran before this file existed. A tag that resolves to
NOTHING is claimed by neither side -- it is still recorded and still counted unfiltered, it simply
cannot answer where the animal came from -- so the filtered halves need not add up to the
unfiltered total, and that gap is honest rather than missing data.

ROAD TO SALE COUNTS WHOLE-SHED PENS TOO (maintainer decision 2026-09-01, same day): the Growth
Director's band board read `weighing_observations` alone and so answered "where does every kid sit"
from scanned tags only -- 501 kids while 555 more sat in nine pens. A pen now contributes ALL its
animals at the pen's average, in the bands AND in moved-up/held/slipped-back, the same trade the
daily-gain headline already takes: a pen creeping 24.9 -> 25.1 kg moves every kid in it, and a pen
average also moves when animals enter or leave. The losing-animals list stays scanned-only, because
it NAMES individual animals and a shed average cannot name one. Wire fields renamed with the
meaning (`identity_count` -> `animal_count`, `BandMovement.pair_identities` -> `pair_animals`),
leaving the tag-matching counts untouched since a pen carries no tag to match. Canonical prose:
`docs/decisions/weights-origin-filter.md`.

THREE ORIGINS, NOT TWO (maintainer decision 2026-09-26, SUPERSEDING the two cohorts above -- the
per-animal and agree-or-neither rules stand). "Farm born" meant "on no load" and so held every
animal bought WITHOUT a recorded load (659 of the live herd that day). The filter is now **Farm
born / Procured (no load) / Procured (load)**, keys `farm_born` / `procured_no_load` /
`procured_load`, with the retired `purchased` still accepted as `procured_load`. Per animal, in
order: on a load -> Procured (load); else `goats.origin_type = 'birth'` -> Farm born; else
`'procured'` -> Procured (no load); else no cohort (never guessed). ONE statement of the rule:
`backend/internal/platform/animalorigin`; every SQL site mirrors `Classify`. The Sales Farm born
and Load wise pages already used this split -- a new origin surface must use it too. Canonical
prose: `docs/decisions/weights-origin-filter.md` -> "Three origins, not two".


ONE DAILY-GAIN NUMBER, AND WHOLE-SHED PENS ARE IN IT (maintainer decision 2026-08-26, same day,
SUPERSEDING the individual-only headline). The farm's daily gain is the ANIMAL-WEIGHTED MEAN over
every kid weighed twice (each kid once, at the median of its own pairs) PLUS every whole-shed pen
weighed twice in the window, each pen contributing its average-weight movement ONCE PER ANIMAL it
holds. `weighing.leadership.growth`'s headline and the Weights page's gain-by-breed/sex/stage
charts now compute the IDENTICAL statistic, so a page filtered to one sex shows the same number in
the headline and in the chart.

It did not, and the maintainer found it: filtered to Male the page read 133 g/day in the headline
above 200 g/day in the chart. Three mismatches at once — MEDIAN vs weighted MEAN, PAIRS vs ANIMALS,
and whole-shed pens counted in one and not the other. Each was individually defensible; together
they left the screen with no true number on it. Most of this farm's kids are weighed by the whole
shed (339 of 791 in the landing window), so the old headline also answered "how fast is the herd
growing" from under half the herd.

The wire field is `average_adg_g_per_day` (was `median_adg_g_per_day`) and `headline_animals` is
its denominator — `pair_count` remains the SCANNED-pair count and is now only the denominator of
the pair statistics. Renaming was part of the fix, not tidying: a field named `median_` returning a
mean is the same trap as the caption that told readers "Daily gain uses only the same animals
weighed twice" while 65% of the number was whole-shed movement. Android reads the same endpoint and
moved in the same change; the two surfaces must never report different herd growth.

KNOWN AND ACCEPTED: a whole-shed average moves when animals ENTER OR LEAVE the pen, not only when
they grow, so this is a coarser measure than a scanned pair. That is the trade taken deliberately
rather than report the herd from a minority of it. The pair-based statistics (positive %, negative
pairs, losing animals) stay individual-only — a shed average has no per-animal sign, and inventing
one would put animals in a losing list nobody weighed.

Pinned by `TestGrowthHeadlineEqualsTheGainChartForTheSameSex`, which filters to one sex so the
chart holds exactly one bucket and the headline must equal it animal for animal; it was
mutation-tested by restoring the old median-of-pairs headline and confirming it goes red.

FIFTH RECORDED EXCEPTION (maintainer decision 2026-09-07): SAME-ANIMAL KEYING, and it is the
NARROWEST of the five -- `backend/internal/weighing/adapters/postgres/identity_scope.go` reads
`goat_identifiers` and NOTHING ELSE, not even `goats`. An animal here can carry TWO RFIDs
(`identifier_type` is `animal_identifier_1` or `animal_identifier_2`) and the operator scans
whichever tag they can read. Every reporting read keyed an animal by the RAW SCANNED STRING, so one
weighed on its primary tag in week 1 and its secondary in week 2 was TWO ANIMALS WITH ONE WEIGH
EACH.

THE NUMBER WAS NOT WRONG, IT WAS ABSENT, and that is the part worth carrying forward: it produced NO
ADG at all -- gone from the headline, the gain-by-breed/sex/stage charts, the band board's
moved-up/held/slipped-back and the losing-animals list -- while counting TWICE in the denominators
those same averages divide. A wrong number gets questioned; a missing one reads as an animal nobody
weighed twice. The farm cannot scan its way out of it: both tags are on the animal, and only the
register knows they are one.

The file answers "which of these strings are the same animal" ONCE and hands `growth.go`,
`shed_weights.go`, `load_weights.go`, `weight_demographics.go` and the Growth Director reads an
OPAQUE tag -> canonical-tag map, so those files still name no herd table -- the same shape
`sex_scope.go` established, for the same reason.

READ-ONLY and REPORTING-ONLY. **THE ONE WEIGHING BUSINESS RULE -- no scanning an animal twice in the
same bucket before submit -- STILL COMPARES RAW STRINGS AND IS DELIBERATELY UNTOUCHED**; making it
identity-aware would gate a scan on the herd register, which is banned outright. A SINGLE-TAG animal
is NEVER remapped (the map holds only animals with two or more active permanent RFIDs; a
`temporary_tag` is not a second RFID slot), an UNRESOLVABLE tag keeps its own string and still pairs
with itself, and a farm with no double-tagged animal runs the query it ran before this file existed,
key for key. The canonical key is one of the animal's OWN tags -- its `animal_identifier_1` -- never
a goat_id, because `animal_key` is rendered verbatim to a reader as `ScannedIdentifier` in the
losing-animals list.

THE COST IS REAL AND ACCEPTED: weighing now depends on herd identity being right, the exact
dependency `growth.go` refused in 2026-08-04. If the register wrongly attaches animal B's tag to
animal A their weights MERGE and the gap reports as growth that never happened. Two narrowings hold
it down and both are pinned: only `status = 'active'` identifiers are read (`disputed`, `duplicate`,
`invalid` are the register saying do not trust this row, and are exactly the rows that would fuse
two animals), and a tag is remapped only when the SAME goat carries another permanent RFID. A
genuine RE-TAG still splits history, and that stays honest.

SCOPE IS PAGE-WIDE ON PURPOSE: fixing only the ADG read would leave a headline counting an animal
once beside a shed table counting it twice, which is the cross-surface disagreement about a business
number this file bans. Canonical prose: `docs/decisions/weighing-same-animal-keying.md`. Pinned by
`identity_scope_integration_test.go` (five tests, each mutation-tested), and by a guard self-test
proving `goats` is STILL a finding inside that same exempt file.

ALLOWED besides `weighing_*`: proof / idempotency / audit / outbox plumbing, and
exactly four ORG tables — `locations`, `workforce_members`, `user_scope_grants`,
`shed_partitions` (a task belongs to a park, a person, and a physical partition).
Adding to that list is a MAINTAINER decision, never a developer convenience.

CRITICAL DISTINCTION (maintainer decision 2026-08-06; reporting exception clarified 2026-08-19): `shed_partitions` is an
ORG-scoped CATALOG of partitions that exist, keyed by (tenant_id, shed_id,
normalized_label), with NO per-animal data. It is allowed. `goat_shed_partitions`
is a PER-GOAT table (PK tenant_id, goat_id) that reveals which animal sits where.
It is strictly BANNED except for the single reporting file named above, where it
may be used only to label lump-sum composition at the selected operational
location grain. This distinction is enforced by the weighing isolation guard
(`check-weighing-free-flow-guard.mjs` mode 16): reading one maintains isolation,
reading the other breaks it unless the read stays inside that file-scoped
reporting exception.

Also banned, because they are invented rules on a path that has none: any
weighing CADENCE ("weekly", "monthly on the 15th", a minimum interval between
weighs), any "overdue"/"missed weigh"/"expected next weigh" concept, and any
roster/ownership/clinical gate before accepting a scan.

Why this is here and not only in the ledger: on 2026-08-04 an ADG read model
shipped a `LEFT JOIN goat_identifiers` to resolve a scanned tag to a goat_id.
The rule already existed in
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md` (A-6, B-4,
C-3) and in `docs/features/weighing/TRD.md`, and a dedicated guard with 15
failure modes was already wired into `ci-local` — but every mode was scoped to
the WRITE path, the guard only ran at CI time, and the constraint was never
passed into the subagent brief that proposed the join. Three ways to miss one
rule. It is now: (1) stated here, in always-loaded context; (2) enforced on
READ paths too by `check-weighing-free-flow-guard.mjs` (mode 16,
`weighing-reads-non-weighing-table`); (3) run on EVERY weighing file edit by
`tools/agent-hooks/check-weighing-isolation-on-edit.sh`, wired into PostToolUse
for BOTH `.claude/settings.json` and `.codex/hooks.json`.

If you delegate weighing work to a subagent, the isolation rule goes in the
brief. An agent that was never told the boundary will propose crossing it, and
it will sound reasonable.

Isolation is not permission to create a private coordination island. Weighing
continues to commit its own domain state, audit, idempotency, proof, and outbox
without an inbound kernel dependency. The shared task kernel, outside the
Weighing package, consumes those durable events outward-only to materialize
owner/clock, hierarchy, contact-waterfall, proof, and sign-off coordination.
That consumer must be receipt-backed, idempotent, version-fenced, bounded,
observable, replayable, and reconciled against Weighing source rows. This does
not widen the Weighing table allowlist, and generic task state must never gate
scan, submit, verdict, reopen, or close.

## WEIGHING IS SCAN-AND-SUBMIT. Nothing else. (Claude AND Codex, every session)

Maintainer statement, 2026-08-03. Sessions keep re-deriving weighing rules that do
not exist, and the maintainer keeps re-explaining them. This is the WHOLE feature:

```
CEO assigns sheds to an operator or a director (the Growth Director executes too)
individual  → scan RFID, enter weight, record video — per animal
lump-sum    → total weight, video(s) — per shed (head count is snapshotted
              server-side from the herd register at submit; maintainer decision
              2026-08-24, frozen forever, verifier edits weight only)
submit
```

**The ONLY business rule: an animal cannot be scanned twice in the same bucket before
submit.**

WEIGHING SOP, THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16,
`docs/decisions/weighing-sop.md` -> "The weigh captures are authored"): WHAT the operator
captures beside the scan and the weight is the published weighing SOP's call, in TWO SEPARATE
sections authored independently and never merged -- PER ANIMAL (named video / photo / either
slots, at least one compulsory, plus per-animal questions) and WHOLE PEN (counted slots min..max,
at most 10 captures per pen, plus per-pen questions). The seeded document is today's behaviour
(one compulsory "Weighing video" per animal; 1..5 per pen) and the 000315-embedded seed file
never changes; an older app's single video / flat list is accepted on the seeded slot and the
rest reads "Not captured (older app)" to the verifier. The verifier item says which kind it is
("Weighed as: Per animal / Whole pen", answers grouped per section). The ONE business rule
above still compares RAW scanned strings and is untouched; the grain stays one item per animal
/ per pen (ledger B-5). Guard: `make weighing-sop-guard` rules 6-8.

There is **NO** shed↔RFID validation (a scanned tag is stored verbatim and is never
checked against a shed), **NO** roster / expected animal count / denominator /
progress percentage, **NO** herd, goat, clinical or lifecycle lookup, **NO** vaccine,
protocol or obligation rules, and **NO** "shed is empty" concept — free-flow means the
system cannot know what is in a shed and must not try to.

**Do not invent problems that cannot exist in this model.** Two were raised and killed
on 2026-08-03: an "empty shed outcome" (impossible — nothing knows a shed is empty),
and the per-animal verifier queue called a grain bug (one video per animal means one
review per animal; the grain follows the EVIDENCE — ban B-5).

Legitimate weighing work is **plumbing, never rules**: do writes reach the server, is
evidence reviewable, are failures visible, do screens show honest numbers.

Machine-enforced by `make weighing-free-flow-guard` (in `make guardrails` and
`make ci-local`), which blocks a herd/goat/vaccination join on the write path, a
roster gate, a clinical-state read, and an expected-animal denominator in weighing UI.
Canonical prose: `docs/features/weighing/TRD.md` → "What weighing IS"; bans and their
history: `context/repo-audits/weighing-implementation-do-not-reopen-ledger.md`.
