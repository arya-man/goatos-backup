# Vaccination Nuance Rules Source

**Source file:** `/Users/ravi/mesha/wiki/Nuances_Rules.docx`
**Extracted into repo:** 2026-07-01 18:18 local source revision
**Scope:** V1 PHC Vaccination config, generation, Calendar, Action Center,
Protocol Adherence, Workflows, and Goat Passport behavior.

This markdown is the tracked engineering source for the vaccination rule
matrix. The local DOCX remains private/source material; this file must travel
with the codebase because V1 config and kernel behavior depend on it.

## Source Vaccine Classes

### Goats

| Vaccine | Source class |
|---|---|
| ET+TT | bacteria killed |
| PPR | virus live |
| Goat Pox | virus live |
| FMD | virus killed |
| HS | bacteria killed |

### Sheep

| Vaccine | Source class |
|---|---|
| ET+TT | bacteria killed |
| PPR | virus live |
| Blue Tongue | virus killed |
| Sheep Pox | virus live |
| FMD | virus killed |
| HS | bacteria killed |

## Source Schedule Table

The **Mother not vaccinated** column is intentionally ignored for V1 because
Mesha operating policy is to ensure mothers are vaccinated in the first three
months. V1 uses the **Mother vaccinated** column.

| Vaccine | Mother vaccinated schedule used by V1 | Revaccination | Priority |
|---|---:|---:|---:|
| ET+TT | 4 weeks and 7 weeks | 6 months | 1 |
| PPR | 16 weeks | 3 years | 2 |
| Blue Tongue | 16 weeks and 20 weeks | 1 year | - |
| Goat Pox | 16 weeks | 1 year | - |
| Sheep Pox | 12 weeks | 1 year | - |
| FMD | 12 weeks | 9 months | - |
| HS | 12 weeks | 1 year | - |

## Source Dose / Vial Table

| Vaccine | Source course type | Dosage | Vial doses |
|---|---|---:|---:|
| PPR | Single | 1 ml | 100 |
| ET+TT | Booster | 2 ml | 100 |
| Blue Tongue | Booster | 2 ml | 100 |
| Goat Pox | Single | 1 ml | 25 |
| Sheep Pox | Single | 1 ml | 100 |
| FMD | Single | 1 ml | 30 |
| HS | Single | 2 ml | 100 |

## Additional Rules

- After procurement/warm-up, even if the source vaccinated the animal, do not
  give any vaccination for one week.
- Two live vaccines must have a 4-week gap.
- Bacterial + viral vaccines may be combined on the same day.
- Live viral + killed viral vaccines may be combined on the same day.
- Quarantine and ICU animals should not be vaccinated.
- Live followed by killed needs a 2-week gap.
- Killed followed by killed needs a 2-week gap.
- Live followed by live needs a 4-week gap.
- Any kid booster needs a 3-week gap.
- Pregnancy is around 5 months.
- Vaccines are allowed up to pregnancy month 3.
- Vaccines must not be scheduled in pregnancy months 4 and 5.
- After delivery, missed vaccines can be given within 2 weeks.
- Adult procured animals can be vaccinated at source.
- Breeding and fattening animals can be vaccinated at source.
- Kids up to 16 weeks must follow the normal schedule.
- Newly procured animals receive ET+TT + PPR first.
- After 4 weeks, give Goat Pox for goats or Sheep Pox for sheep plus ET+TT
  booster.

## V1 Implementation Contract

V1 must support the source matrix in the Config modal and in the kernel.

The Config modal must author these values per matrix row:

- vaccine code and name;
- immunological type: live, killed, toxoid, or reviewed combo;
- pathogen class: bacterial, viral, mixed, or reviewed unknown;
- source course type: single or booster;
- stage, sex, and breed eligibility;
- source schedule text from the table;
- dose amount and unit;
- vial dose count;
- revaccination interval in days;
- priority where the source provides one;
- source review and approval metadata.

The shared V1 policy must author:

- clinical defer states: sick, under treatment, quarantine, ICU;
- reproductive exclusions, including pregnancy skip policy;
- procurement warm-up hold of 7 days;
- kid normal-schedule cutoff at 16 weeks;
- adult source-vaccination allowed;
- same-day compatibility allowed flags;
- live/killed spacing days;
- kid booster minimum gap of 21 days;
- first and second procurement waves.

The V1 kernel must enforce:

- birth-age source schedule rows from the Mother vaccinated column;
- trusted prior vaccination evidence suppression so pre-existing ET+TT/PPR/FMD
  records do not duplicate work;
- generation of the next missing source-schedule row when prior evidence exists;
- one safe older-animal catch-up/review item instead of flooding all missed old
  doses;
- one-week post-arrival warm-up offset;
- quarantine/ICU/sick/under-treatment visible defers;
- pregnancy exclusion where the animal state says pregnant/lactating; month-4
  and month-5 precision must use pregnancy month fields when present in the
  animal data model;
- Calendar drive aggregation after sweeper batching, with goat-level due rows
  remaining in Passport, Protocol Adherence, and Vaccination detail.

## V1 Source Matrix Presets

These are the V1 authoring presets loaded by the Config modal.

| Species/source row | Vaccine | Type | Pathogen | Course | Source schedule days from DOB | V1 effective days | Revaccination days | Dose | Vial |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| goat | ET+TT | toxoid | bacterial | booster | 28, 49 | 28, 49 | 182 | 2 ml | 100 |
| goat | PPR | live | viral | single | 112 | 112 | 1095 | 1 ml | 100 |
| goat | Goat Pox | live | viral | single | 112 | 140 | 365 | 1 ml | 25 |
| goat | FMD | killed | viral | single | 84 | 84 | 274 | 1 ml | 30 |
| goat | HS | killed | bacterial | single | 84 | 84 | 365 | 2 ml | 100 |
| sheep source | ET+TT | toxoid | bacterial | booster | 28, 49 | 28, 49 | 182 | 2 ml | 100 |
| sheep source | PPR | live | viral | single | 112 | 112 | 1095 | 1 ml | 100 |
| sheep source | Blue Tongue | killed | viral | booster | 112, 140 | 112, 140 | 365 | 2 ml | 100 |
| sheep source | Sheep Pox | live | viral | single | 84 | 84 | 365 | 1 ml | 100 |
| sheep source | FMD | killed | viral | single | 84 | 84 | 274 | 1 ml | 30 |
| sheep source | HS | killed | bacterial | single | 84 | 84 | 365 | 2 ml | 100 |

Goat Pox has a source schedule of 16 weeks, but PPR is also a live viral row at
16 weeks and priority 2. V1 therefore preserves PPR at 112 days and moves Goat
Pox to 140 days when the Nuance matrix is loaded, honoring the source live-live
4-week spacing rule.

The current runtime target type is still named `goat`; sheep rows are retained
in the V1 source matrix so the config source is complete. Runtime species-aware
targeting must not silently pretend sheep are goats; when species fields are
present in animal data, sheep rows must use the same V1 kernel rules with a
species eligibility dimension.
