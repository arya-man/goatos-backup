# Exact Physical Shed Is Atomic

Status: mandatory guardrail  
Owner: Goat OS operator surfaces  
Applies to: Android, admin-web, backend read models, OpenAPI contracts, seeds, CI, agent skills

## Rule

Animals live in exact physical sheds. If farm operations names a shed `Castro 2`,
`Gandhi 1`, or `Godel 1 - Part 3`, that full stored name is the shed for
operator work. A common/base name such as `Godel 1`, `Mandela 2`, `Gandhi`, or
`Old Yashoda` is grouping metadata only when it has exact child sheds.

Do not describe product behavior as "parent shed plus partition". That wording
is wrong. `partition_label` is legacy database/API compatibility metadata. It
must not be appended to an exact shed name in live code, and product code must
not require it to identify a live animal/work location.

Every operator-facing work surface must use the resolved exact shed as the
operational identity:

```text
operational shed key   = stable exact-shed identity
operational shed label = stored exact shed label, for example Godel 1 - Part 1
```

Legacy database/API fields may still carry `shed_group_id`, `physical_shed`, or
`partition_label` while old rows are cleaned. Product code must resolve those
fields to the exact shed before rendering, grouping, routing, caching, or
generating tasks. Do not treat the base/common name as the work identity.

Allowed:

```text
Castro 1
Castro 2
Gandhi 1
Godel 1 - Part 1
Godel 1 - Part 2
Mandela 2 Part 7
Old Yashoda Part 4
```

Not allowed as operator work identities:

```text
Godel 1
Mandela 2
Old Yashoda
Gandhi
Godel/Mandela/Old Yashoda group
Castro 1 1
Castro - 1
Gandhi 1 - Part 1
exact shed name + partition label
```

## Vaccination

Vaccination drives can schedule multiple vaccine obligations for the same
animal. That does not change the UI grain.

For one exact shed with 42 animals and two vaccines:

```text
Backend obligations:
  Blue Tongue: 42
  Sheep Pox:   42

Operator card:
  Targeted animals: 42
  Open/done animals: 0/42, 42/42, etc.
  Chips:
    Blue Tongue x/42
    Sheep Pox   x/42
```

One animal scan/proof satisfies all vaccine obligations scheduled for that
animal in that exact shed. The scan screen list, circular progress, and submit
gate are animal-grain, not dose-grain.

## Weighing

Weighing buckets, new-task dropdowns, campaign shed cards, leadership video
lists, and proof/close/reopen routes must also use the exact shed identity. A
bucket or dropdown option named only `Godel 1` is valid only when `Godel 1` is
itself the exact shed.

When a campaign targets `Godel 1 - Part 1`, `Godel 1 - Part 2`, `Godel 1 - Part
3`, and `Godel 1 - Part 4`, it must render four separate options/cards. Do not
collapse them into `Godel 1`, and do not rebuild those labels from `Godel 1` plus
legacy partition labels.

## Backend Read Models

Backend read models must expose enough fields for clients to use the exact shed
identity without guessing:

```text
shedId
physicalShed or shedName = exact shed name
stable row/task/bucket id at exact-shed grain
```

If a task/bucket is generated from a higher-level drive or batch, the generated
operator work item still has to be unique at exact-shed grain. The drive can be
the umbrella; the operator task/card/dropdown option cannot be the umbrella.

## Client Contract

Clients must not group cards, list rows, dropdown options, Compose keys, route
identity, or cache scopes by the base/common shed when an exact shed exists.

Required examples:

```text
card key      = exact-shed identity + task/bucket identity
route params  = exact-shed identity + task/bucket identity
cache scope   = exact-shed identity + task/bucket identity
display label = stored exact shed label, for example Gandhi 1
```

Forbidden examples:

```text
key = base/common shed id
key = taskId
key = campaignShedId
label = base/common shed name
label = exact shed name + partition label
groupBy(base/common shed)
groupBy(taskId)
groupBy(batchId)
```

Those are valid only inside code that has first proved the row is an aggregate,
not an operator work item.

## CI And Review

Any change touching these surfaces must run the exact-shed identity guard:

```bash
make operational-partition-identity-guard
```

Run the affected product tests too:

```bash
./gradlew :app:compileStgDebugKotlin --no-daemon
./gradlew :app:testStgDebugUnitTest --tests '*ShedsExecutionIdentityTest' --no-daemon
```

Review checklist:

- Does every repeated operator card include the exact shed id in its stable key?
- Does every visible shed label render the stored exact shed label?
- Does every dropdown option identify the exact shed, including zero-animal sheds?
- Does every backend row expose exact shed id/name/display without requiring `partition_label`?
- Does every count match the operational grain: animals for animal work, doses only for vaccine carry?
- Does one goat proof/scan satisfy all same-animal vaccination obligations?
