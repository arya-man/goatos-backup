# The word on screen is PEN, never SHED

**Maintainer decision, 2026-09-02.** Every user-visible string in Goat OS says **pen**. The
word *shed* does not appear on any screen a person reads.

This is a **vocabulary decision only**. No behaviour, no grain, no schema, no API contract
changes because of it. If a change made in the name of this rule alters what the software
*does*, that change is wrong.

---

## 1. What actually changed, and what did not

| Layer | Says | Why |
|---|---|---|
| Page titles, table titles, column labels, filters, chips, KPIs, empty states, notes, tooltips, drawer copy, error text a person reads, CSV export headers, download filenames | **pen** | It is what the operator calls the place. |
| Column KEYS (`shed`, `shed_tag`), copy KEYS (`kpi.sheds.label`, `section.shed.title`), table/section ids (`shed-weights`, `shed-factors`) | `shed*` | Machine-readable. Every read model already emits them; renaming is a contract change, not a copy change. |
| Route paths (`/vaccination/sheds`, `/weighing/shed-weights`, `/feed-config/shed-factors`) | `shed*` | Bookmarked, linked, and in generated clients. |
| Database — table names, columns, enums, `subject_type='shed'`, `proof_mode='shed_level_video'`, `position_code='shed_manager'` | `shed*` | The stored contract. Grants, duties, joins and history key on these. |
| Go/TS identifiers, struct fields, JSON wire field names (`shed_id`, `shed_name`, `partition_label`) | `shed*` | Same reason. Renaming a wire field breaks every client at once. |

**The rule in one line:** if a human reads it, it says *pen*. If a machine reads it, it stays
as it is.

---

## 2. Where the copy actually lives

Do not go looking for a strings file. Per the golden frontend rule, **the backend owns
labels**, and there are four separate places a visible word can come from. All four were
renamed; a future screen must get all four right, and missing one is how the first attempt at
this shipped with eleven tables still saying "Shed" on pages whose own copy already said pen.

1. **The page-contract copy maps** — `backend/internal/adminui/app/service.go`. The bulk of
   it: `"section.sheds.title": "Pens"`. The KEY on the left keeps `shed`; only the value moves.
2. **Derived column labels** — `humanLabel()` in the same file. Column labels are computed
   from the column KEY, not read from the copy map, so `shed` → `Pen`, `sheds` → `Pens`,
   `shed_tag` → `Pen tag` are named there explicitly. Leaving them to the default
   de-underscoring is what rendered "Shed".
3. **Go-composed sentences in the producing module** — a label built at write time and stored,
   or built at read time in SQL. Four examples worth knowing, because they are easy to miss:
   `weighing/domain.CorrectedSubjectLabel` ("Whole pen"), `counts/app` shifting completion
   ("Pen move"), `verification/adapters/postgres` batch label (`… || ' pens'`),
   `calendar/adapters/postgres` event subtitle (`' pen · '` / `' pens · '`).
4. **Seeded rows in the database** — SOP library documents and their `form_dsl` field labels.
   These need a forward migration; see `000240_sop_library_pen_vocabulary.sql`.

A fifth, smaller one: **position titles** are prettified from `position_code` in
`workforce/app.formatPositionCode`, so `shed_manager` renders "Pen Manager" while the code
itself is untouched.

---

## 3. The three places the two words meant different things

Most of this was a substitution. Three were not, and each is recorded because the honest
answer was not "write pen".

### Feed Config's multiplier is a **FEED factor**, not a pen factor

`feed_shed_factors` is keyed on `shed_id` with **no partition column**, so one row scales every
pen inside that building. Calling it a "Pen factor" would tell the reader that editing one row
moves one pen, which is false — it moves all of them. It is now **"Feed factor"**, and the copy
around it says "per location" rather than naming either noun.

### Feed **transport** is shed-grain on purpose

One trip per physical building, never one per pen — that is a recorded decision with its own
repair migration (`000152`, after `000143` fanned transport out over partitions and produced
three videos of one load). Its SOP copy therefore says **"physical location"**. Writing "pen"
there would state the exact opposite of the rule the sentence exists to explain.

### The explainers that only existed to relate the two words

Sentences like *"An undivided shed is its single pen"* were teaching the storage relationship.
With one word they are circular, so **the clause is dropped** rather than reworded.

**The general form of all three:** when a sentence needs both words because it is about the
difference between a building and a pen inside it, do not substitute. Either name the thing
accurately without either noun ("location", "feed factor"), or delete the clause. Ask the
maintainer if neither works.

---

## 4. The trap that can silently corrupt data

**`pen` already meant PARTITION in the animal bulk importer.**

`identity/app.normalizeHeader` maps `pen`, `pen_label`, `penlabel` → `partition_label`. The CSV
template's header row is built from the option **labels** and the parser matches by header
**name** — so labelling the location column `"Pen"` would have filed a pen name into
`partition_label` on every imported row. No error, no rejection: a wrong location on every
animal.

The location column is therefore labelled **"Pen name"** (→ `pen_name`), and bare `pen` keeps
its existing meaning. Both halves are pinned by
`identity/app.TestImportHeaderAliasesSurviveThePenRename`.

**The general rule this establishes: a template header label IS a parser input.** Renaming one
without the parser is a break; renaming the parser without keeping the old name is a break for
every sheet already saved. Add the new name as an **alias** and keep the old one.

---

## 4b. The three shapes a sweep misses, found by rendering the pages

Reading the source found most of this. Rendering the pages found the rest, and all three
misses were the same mistake: deciding whether a string is copy by looking at the string.

1. **Bare lowercase nouns.** `"pager.noun": "shed"`, `"schedule.unit.sheds": "sheds"`,
   `"label.shed_fallback": "shed"` -- eleven of them. A sweep that rewrites values containing
   a space or starting with a capital skips every one, and the pages read
   *"1-25 of 104 sheds"* while the contract scan came back clean.
2. **JSX text nodes.** `<option>All sheds</option>` and a bare `Shed` label line are not
   quoted, so a string-literal sweep cannot see them at all. Live Monitor kept its "Shed"
   filter for exactly this reason.
3. **Farm data.** `procurement_vendor_catalog` holds a vendor category **"Sheds Contractor"**
   -- someone who builds sheds, a real trade. That is the farm's word about the outside
   world, not the product's word for a pen, so it is a maintainer decision and is left as it
   is.

**The rule that follows:** decide by POSITION, not by spelling. The bootstrap guard skips a
named set of machine leaves (`key`, `id`, `href`, `icon`, `data_source`, `param`, `columns`)
and treats everything else as copy, so a copy shape nobody anticipated fails closed instead
of slipping through. The JSX guard scans text between tags for the same reason.

## 5. How this is enforced

| Check | What it catches |
|---|---|
| `adminui/app.TestBootstrapContractSaysPenNeverShed` | Walks the whole served bootstrap JSON and fails on any user-visible "shed". Catches copy wherever it is produced — including derived column labels, which is what the first pass missed. Mutation-tested: reverting one `humanLabel` case turns it red across 16 strings. |
| `adminui/app.TestColumnLabelsSpeakPenWhileTheKeysStayShed` | The keys stay `shed_*` while the labels say pen — both halves, so neither can drift. |
| `identity/app.TestImportHeaderAliasesSurviveThePenRename` | Every legacy import header still resolves to the same field, and the `pen`/`pen_name` collision stays resolved. |
| `workforce/app.TestPositionTitlesSayPenWhileTheCodesStayShed` | Titles say pen, `position_code` does not move. |
| `admin-web features/counts/pen-import-headers.test.mjs` | The downloaded template's own headers are the ones its parser accepts, and old sheets still import. |
| `admin-web features/counts/pen-vocabulary.test.mjs` | JSX TEXT nodes, which no string-literal sweep can see. Mutation-tested. |

`TestBootstrapContractSaysPenNeverShed` carries exactly **two** named exceptions, both dead
copy no screen renders: `modal.rule_editor.default_proof_policy` (a token list no admin-web
code reads) and `vaccination_import_columns` (an option group with no consumer, whose labels
are raw snake_case column names because it describes a CSV contract). They are listed by name
so removing either key fails the test loudly rather than quietly widening the exception.

**Verified on the running stack, 2026-09-02:** the served `/admin-web/bootstrap` contract, all
42 page data-source endpoints it declares, and the rendered HTML of all **40** dashboard routes
-- zero user-visible "shed" on any of them. The only occurrence anywhere is the vendor-category
row named above.

---

## 6. Adding a screen after this decision

1. Write the label as **pen**. If the column key is `shed`-something, `humanLabel()` already
   handles it — do not add a copy-map override just to say pen.
2. If your module composes a sentence in Go or in SQL, say pen there. Grep your own module for
   `\b[Ss]heds?\b` inside string literals before you push.
3. Do not rename a column key, a JSON field, a route, a `position_code`, a `subject_type`, or a
   table to match. The mismatch between the stored word and the shown word is deliberate and
   permanent.
4. If your sentence genuinely needs to distinguish a building from a pen inside it, read
   section 3 and then ask.

---

## 7. What this decision does NOT touch

**The Android app.** It carries roughly 471 of its own hardcoded "shed" strings — string
resources, Paparazzi screenshot fixtures, Room migration test SQL — and needs its own
screenshot-fixture regeneration and on-device verification. Backend-owned copy that Android
reads (task templates, SOP form labels, push notification text) *did* move to pen in this
change, so the two surfaces are currently mixed. Finishing Android is tracked separately.

**Every operational rule.** Weighing is still free-flow scan-and-submit. Feed transport is
still one task per building. Packing is still one bag per pen per session. Lump-sum still
snapshots the register count. Nothing in this document changes what any of them do.
