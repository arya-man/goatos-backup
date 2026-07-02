# Goats And Parks Source Findings

Date reviewed: 2026-06-30

Source reviewed: `wiki/Goats and Parks.docx`
Tracked source extract: `context/source-findings/goats-and-parks-source-extract.md`

Status: sanitized source finding and base livestock/park rule for GoatOS. Do not
commit the raw DOCX, screenshots, local paths, or private media. If a later
source owner changes these values, create a reviewed source-backed version rather
than overriding them in frontend code.

## Base Source Rule

`Goats and Parks.docx` is the base source for goat and park semantics across
GoatOS. Any current or future feature that touches goat identity, park/shed
scope, shed tags, lifecycle/stage, breed labels, sex/reproductive state,
fattening, pregnancy, milking, warm-up, feed safety, weighing, handling,
medicine administration, park roles, or feed session execution must use this
finding as the starting rule.

Feature-specific docs may add stricter source-backed rules for their slice. For
example, `Feed, Shiftings and Count.docx` v1.1 controls Feed Direction timing,
Diff, bridge, projection, and ration behavior. Those slice docs must not
silently redefine the base goat/stage/shed-tag meanings from this file. When
there is conflict, record it as a reviewed source conflict and get owner
approval before building runtime behavior.

## Core Terms

| Term | Meaning |
| --- | --- |
| Kid | Baby goat. Male and female newborns are both called kids. |
| Adult female | Fully grown female goat. |
| Buck | Adult male goat, kept separately and used for breeding. |
| Fattening kid | Weaned young goat raised for meat on a high-nutrition diet. |

## Breed And Species Labels

| Label | Source description |
| --- | --- |
| Malai | Large, muscular, typically white, broad frame. |
| Beetal | Tall and lean, long drooping ears, reddish-brown or black with white patches. |
| Sojat | Medium to large frame, mostly white, Roman nose. |
| Osmanabadi | Medium-sized, mostly black or brown, hardy. |
| Boer | Large heavy-set goat, white body with red/brown head. |
| Anantapur Sheep | Sheep label present in the source material. |

GoatOS implication: store breed/species as reviewed reference data with aliases.
Do not hardcode dropdown strings or treat dirty source spellings as clean truth.

## Lifecycle And Stage Facts

| Stage | Source facts | GoatOS implication |
| --- | --- | --- |
| Birth / kidding | Adult females usually deliver one to three kids; twins are common. Active labour is commonly 30-60 minutes. | Birth workflows need timely exception handling when labour is prolonged or kid proof is missing. |
| Colostrum, first 48 hours | Newborn kids must receive colostrum in the first few hours and through the first 48 hours; this window cannot be recovered. | Birth/health SOPs should treat missed colostrum as urgent, time-sensitive work. |
| Milk feeding | After colostrum, kids remain on milk until weaning, typically 60-90 days. | Milk-fed stages must not be treated like normal packed-feed cohorts without explicit policy. |
| Weaning | Transition from milk to solid feed is gradual; abrupt weaning creates stress and digestive problems. | Stage changes and feed config must support gradual transitions. |
| Fattening | Weaned meat kids receive high-energy feed for defined weight gain. Operators should not add/change feed without instruction. | Fattening ration policy is governed config, not operator improvisation. |
| Puberty and breeding | Female goats reach sexual maturity around 10 months; pregnancy is about 150 days. | Breeding, pregnancy, and feed-risk logic need age/status evidence. |
| Productive life | Does are generally fertile/productive until around eight years; bucks are assessed/rotated based on performance. | Long-lived goat passport/history is required; do not infer productivity from current shed only. |

## Reproduction Parameters

| Topic | Source facts |
| --- | --- |
| Estrus / heat | Female goats are receptive for about 12-48 hours, 24 hours on average. Goats cycle about every 21 days; sheep about every 17 days. |
| Synchronization | Progesterone sponges may be used for about 14 days. |
| Seasonality | Some breeds are short-day breeders and come into estrus when day length shortens. |
| Breeding modes | Natural mating and artificial insemination are both described. |
| Natural ratio | About one buck per five females, with at least two days rest before the next breeding session. |
| Artificial insemination | Fresh diluted semen can be used up to three days after collection. |
| Pregnancy confirmation | Pregnancy confirmation requires ultrasound; scanning can start about 45 days after breeding. |
| Gestation | About 150 days. |
| Anestrus | Non-seasonal post-delivery anestrus is expected to resolve around 60 days after delivery; seasonal anestrus resolves in season. |

## Identification Rule

Every animal has a numbered ear tag, and each tag carries an RFID chip linked to
the animal record. Operators must use the tag number when reporting or discussing
an animal. Visual descriptions such as "black goat in pen 3" are not identity.
Missing, damaged, or unreadable tags are immediate exceptions.

GoatOS implication:

- Goat identity, SOP work, vaccination, procurement, feed exceptions, shifting,
  death, and sales must reference reviewed identifiers.
- Missing/damaged/unreadable tags create Action Center/process work instead of
  silent fallback to visual descriptions.
- RFID helps, but old tags and source tags still need scoped review rules from
  existing identity findings.

## Digestive And Feed-Safety Rules

Goats are ruminants with four stomach chambers: rumen, reticulum, omasum, and
abomasum. Normal cud chewing is a health signal; no cud chewing is a red flag.

Feed changes must be gradual. The rumen microbes adapt to the current diet, and
sudden changes, including batch-to-batch changes, can cause digestive upset,
bloat, or death. Bloat can be fatal within hours; a firm swollen left side of
the belly is an urgent supervisor alert.

Goats mouth and swallow unsafe objects. Pens must stay clear of plastic bags,
wrappers, rope ends, wire, loose cloth, and outside feed/treats.

GoatOS implication:

- Feed Direction must treat underfeeding, overfeeding, abrupt ration changes,
  moist/unsafe leftovers, and refusal-to-eat as operational exceptions.
- Pregnant, lactating, warm-up, weaning, and fattening animals cannot be hidden
  behind average shed feed. Destination shed feed must be re-resolved after
  shifting.
- Feed changes and transition policies must be governed config with proof,
  review, and escalation where needed.

## Vital Signs And Body Basics

| Measurement | Normal range |
| --- | --- |
| Body temperature | 101.5 F to 103.5 F |
| Heart rate | 70-80 beats per minute |
| Breathing rate | 15-30 breaths per minute |
| Rumen sounds | 1-2 gurgles per minute on the left flank |

Useful body terms: withers, flank, left flank, udder, hoof, poll, and gums.

Teeth can roughly estimate age:

| Age | Teeth |
| --- | --- |
| Under 1 year | Eight small milk teeth. |
| About 1 year | Center two adult teeth visible. |
| About 2 years | Four adult teeth visible. |
| About 3 years | Six adult teeth visible. |
| About 4 years and older | All eight adult teeth visible, called full mouth. |

## Behavior And Handling

Goats are herd animals. Isolation, loud noises, shouting, sudden movements,
rough handling, irregular feeding times, and careless movement cause stress.
Stress affects health, digestion, and weight gain. Dominant animals eat first
and claim preferred spaces; new groups may head-butt or chase until hierarchy is
settled.

Handling rules:

- Approach from the side, not directly from front or behind.
- Move calmly and let the animal see the handler before touch.
- Guide animals with body position and walls/corners; do not chase.
- Never grab horns.
- Be aware of buck facing and do not turn your back on a dominant buck.
- Support kids with the full body; do not pick up a kid by one leg.

GoatOS implication: SOPs, mobile flows, and work instructions should not
encourage isolated, rushed, or rough handling. High-volume operations still need
calm movement, proof, assignment, and exception handling.

## Medicine And Trained-Operator Boundary

The source describes IM, SQ, IV, oral medicine, stomach tubing, and oral
drenching. Medicine administration is not the responsibility of untrained staff
unless they are specifically trained and instructed.

GoatOS implication:

- SOP permissions and task assignment must distinguish observation/data-entry
  work from trained medical administration.
- Medical actions need role/skill authority, dose/proof capture, and verifier
  review where policy requires it.

## Weighing

| Animal group | Source weighing schedule |
| --- | --- |
| Kids | Every Monday. |
| Adult goats | On the 15th of every month. |

Weight is a key farm data point for fattening progress, condition loss, and
breeding doe health. Accurate weighing and tag-linked logging is non-negotiable.

GoatOS implication: weight capture must be tied to goat identity, park/shed
scope, timestamp, actor, and reviewability. Feed, procurement, breeding,
vaccination, and sales logic should treat stale/missing weight as an explicit
data-quality state, not a guessed value.

## Park Team And Vertical Structure

VGoat uses verticals covering areas such as Birth, Health, Breeding, Fattening,
and Infra. Operators take instructions from the Vertical Head, Park Head, or
Central Team.

| Role | Source responsibility |
| --- | --- |
| Vertical Head | Leads their vertical, focuses on observation, data entry, and ensuring assigned work is completed correctly. |
| Assistant Manager | Supports ground work such as moving animals, administering injections, and carrying out procedures directly. |
| Breeding | Manages heat cycles, mating, buck pairing, and breeding data. |
| Health | Observes illnesses and follows health SOPs. |
| Birth | Manages delivery and newborn/mother SOPs. |
| Infra | Manages park infrastructure and service/repair work. |
| Feed Packer | Packs tomorrow's feed today. |
| Feed Distributor | Distributes feed into assigned shed panels. |

Daily-wage labourers may be added for non-steep-learning-curve tasks, generally
9 AM to 6 PM with a 1 PM to 2 PM lunch break.

GoatOS implication: workforce assignment must respect role/scope/skill and must
not assign high-risk medical or judgement-heavy work to untrained temporary
labour.

## Park Feed Packing And Distribution

The source describes two serving sessions:

| Time | Session |
| --- | --- |
| 9 AM | Morning session; panels are cleaned before feed is added. |
| 3 PM | Afternoon session; panels are cleaned before feed is added. |

The same source describes current examples of Masoor Bhusa and Concentrate and
states that multiple feeds can be mixed in each session.

It also records a legacy/source operating rhythm:

| Time | Activity |
| --- | --- |
| 8:30 AM | Tomorrow's feed directions are sent and packer starts work. |
| 9 AM | Feed distributors serve feed kept outside sheds and record videos. |
| 2 PM | Updated directions are sent for tomorrow's feeding if shiftings changed the plan. |
| 3 PM | Packed feed is distributed/kept outside sheds for tomorrow. |

Feed Direction note: `Feed, Shiftings and Count.docx` v1.1 is the controlling
source for the current GoatOS Feed Direction timing and Diff model. The Goats
and Parks source still anchors base feed-role, two-session, panel-cleaning, and
shed-tag/lifecycle semantics.

## Park Shed Tags

Each shed has a shed tag representing the kind of animals present in that shed.
The source age column is written with date of birth as "Day 1"; GoatOS should
store both the source display range and a normalized zero-based age-days range
for validation/querying. The purpose text can also define a maximum stay that is
different from the display age range, such as K1's seven-day milk-training stay.

| Shed tag | Kid/adult | Source age range | Normalized age days | Purpose |
| --- | --- | --- | --- | --- |
| K0 - Newborn | Kid | 1-2 days | 0-1 | Newborn kids are kept with their mothers for a maximum of one day. |
| K1 - Milk Training | Kid | 3-9 days | 2-8 | Newborn kids separated from their mother are trained to drink from the milk feeding system, for a maximum of seven days. |
| K2 - Milk Drinking | Kid | 10-77 days | 9-76 | Kids that completed milk training drink milk freely, generally about 42 days or six weeks. |
| K3 - Weaning | Kid | 78-84 days | 77-83 | Kids have milk ration cut and are encouraged to eat more grains during weaning. |
| ICU Milk Kids | Kid | 3-77 days | 2-76 | Milk kids in serious condition such as hypothermia, fever, or other urgent illness. |
| Quarantine Milk Kids | Kid | 3-77 days | 2-76 | Milk kids with viral disease such as ORF; separated to prevent spread. |
| Fattening Male | Kid | 120-240 days | 119-239 | Male kids post-weaning on fattening diet for rapid weight gain. |
| Fattening Female | Kid | 120-240 days | 119-239 | Female kids post-weaning on fattening diet for rapid weight gain. |
| Fattening Male Warmup | Kid | 120-240 days | 119-239 | Purchased male kids on warm-up diet before switching to park diet. |
| Fattening Female Warmup | Kid | 120-240 days | 119-239 | Purchased female kids on warm-up diet before switching to park diet. |
| ICU Fattening Kids | Kid | 120-240 days | 119-239 | Fattening kids in serious condition. |
| Quarantine Fattening Kids | Kid | 120-240 days | 119-239 | Fattening kids with viral disease such as ORF; separated to prevent spread. |
| Warmup Non Pregnant | Adult | 300 days + | 299+ | Purchased non-pregnant females on warm-up diet, generally max 14 days before normal Non Pregnant. |
| Warmup Buck | Adult | 300 days + | 299+ | Purchased adult males on warm-up diet, generally max 14 days before Buck. |
| Warmup Pregnant | Adult | 300 days + | 299+ | Purchased pregnant females on warm-up diet, generally max 14 days before Pregnant. |
| Non Pregnant | Adult | 300 days + | 299+ | Non-pregnant females. |
| Flushing | Adult | 300 days + | 299+ | Non-pregnant females fed extra ration to prepare for breeding. |
| Breeding | Adult | 300 days + | 299+ | Adult females and males used for breeding. |
| Pregnant Early Gestation | Adult | 300 days + | 299+ | Ultrasound-confirmed pregnant females until about three months gestation. |
| Pregnant Late Gestation | Adult | 300 days + | 299+ | Pregnant females at or beyond three months gestation. |
| Mother | Adult | 300 days + | 299+ | Adult females after separation with their kids from K0; not milking. |
| Mother Milking Waiting | Adult | 300 days + | 299+ | Adult females after K0 separation that are milking and waiting for milking warm-up diet. |
| Milking Warmup | Adult | 300 days + | 299+ | Milking adult females transferred to warm-up diet before normal milking. |
| Milking | Adult | 300 days + | 299+ | Milking adult females after completing milking warm-up. |
| ICU Adults | Adult | 300 days + | 299+ | Adults in serious condition such as fever or other urgent illness. |
| Quarantine Adults | Adult | 300 days + | 299+ | Adults with viral disease such as ORF; separated to prevent spread. |
| Buck | Adult | 300 days + | 299+ | Adult males. |

GoatOS implication:

- These tags are base reference data for shed/cohort semantics.
- Every clean current herd animal must belong to one current shed/tag at a time,
  and that tag must carry the source age range, normalized age-day policy, and
  purpose needed by vaccination, feed, counts, procurement, and shifting.
- The same physical shed/tag can contain multiple species and breeds. Legacy
  count sheets show goat breeds and Anantapur Sheep co-located in the same
  shed/tag group, so species is an animal fact and shed tag is a shared
  operational cohort fact.
- Do not infer that every tag is valid for every species. Kid tags can carry
  mixed goat/sheep animals where the park data says so. Sheep mothers can have
  normal mother/lactation biological state for K0 and vaccination, but the
  commercial milking workflow tags (`Mother Milking Waiting`, `Milking Warmup`,
  `Milking`) are goat/doe operational tags unless a future approved sheep dairy
  policy adds sheep-specific equivalents.
- ICU and quarantine are risk/isolation states that require critical-action
  policy, not generic feed or vaccination default behavior.
- Warm-up, pregnancy, lactation, flushing, breeding, mother, and fattening are
  nutrition and workflow-critical states.
- Feed, Counts/Shifting, Procurement, Preventive Care (PC), Breeding, and command-lens read
  models must preserve the distinction between physical shed placement and the
  nutrition/health/reproductive cohort implied by the shed tag.

## Cross-Slice Build Rules

- Operational kernel: every source-backed exception should become canonical
  state, audit/outbox, obligation/work, proof/verification, and read-model
  visibility where the process demands it.
- SOP/workflows: medical, handling, weighing, feeding, and shifting SOPs need
  role/skill gates and proof where policy requires it.
- Control Tower, Action Center, Calendar, Protocol Adherence, and Workflows:
  surface missing tags, overdue stage transitions, under/overfeeding risk,
  missed weight capture, unsafe feed leftovers, quarantine/ICU state, and
  unreviewed movement/feed conflicts as process gaps, not hidden local UI state.
- Preventive Care (PC) / Vaccination: tag/RFID identity, K0/K1/K2/K3 stage facts, trained medical
  administration, park/shed scope, and calm handling remain mandatory context.
- Feed Direction: session slots are governed config; the two-source-session
  default and panel-cleaning fact are evidence, while detailed Feed Direction
  timing/Diff/ration comes from the Feed source docs.
- Counts/Shifting: physical aggregate counts and movement ledgers must resolve
  into these shed-tag/cohort meanings before Feed can generate safe quantities.
- Procurement/source entry: source-only identifiers, purpose-specific warm-up,
  accepted-intake, arrival health/weight, and destination shed tag must be
  reviewed before goats become clean park truth.
- Parks/Sheds/Locations: parks and sheds are physical scope; shed tags are
  operational cohort semantics attached to shed state over time.

## Open Extraction Notes

The DOCX includes park layout and vertical sections that may contain visual
content not fully represented by text extraction. Do not invent layout or
vertical details from blank extraction. If those details become implementation
inputs, extract the visuals or get owner review and record a new finding.
