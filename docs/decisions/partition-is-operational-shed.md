# Partition Is The Operational Shed

Status: mandatory guardrail  
Owner: Goat OS operator surfaces  
Applies to: Android, admin-web, backend read models, OpenAPI contracts, seeds, CI, agent skills

## Rule

Animals live in sheds. If a place has parts, each part is the shed for operator
work. The common/base name such as `Godel 1`, `Mandela 2`, `Gandhi`, or
`Old Yashoda` is grouping metadata only when it has parts.

Do not describe product behavior as "parent shed plus partition". That wording
is wrong. In product language, `Godel 1 - Part 1` is the shed.
`partition_label` is only a legacy database/API column name used to store the
actual shed name while the operational-location migration is in progress.

Every operator-facing work surface must use the resolved actual shed as the
operational identity:

```text
operational shed key   = stable actual-shed identity
operational shed label = actual shed label, for example Godel 1 - Part 1
```

Legacy database/API fields may still carry `shed_id`, `physical_shed`, and
`partition_label` separately while the migration to exact operational locations
is in progress. Product code must resolve those legacy fields into the actual
shed before rendering, grouping, routing, caching, or generating tasks. Do not
treat the base/common name as the work identity.

Only show the plain base/common shed name when there are no parts.

Allowed:

```text
Godel 1 - Part 1
Godel 1 - Part 2
Mandela 2 - Part 7
Old Yashoda - Part 4
Gandhi - Part 1
```

Not allowed as operator work identities when partitions exist:

```text
Godel 1
Mandela 2
Old Yashoda
Gandhi
Godel/Mandela/Old Yashoda group
```

## Vaccination

Vaccination drives can schedule multiple vaccine obligations for the same
animal. That does not change the UI grain.

For one partition with 42 animals and two vaccines:

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
animal in that partition. The scan screen list, circular progress, and submit
gate are animal-grain, not dose-grain.

## Weighing

Weighing buckets, new-task dropdowns, campaign shed cards, leadership video
lists, and proof/close/reopen routes must also use the actual shed identity. A
bucket or dropdown option named only `Godel 1` is valid only when there are no
parts for that bucket.

When a campaign targets `Godel 1 - Part 1`, `Godel 1 - Part 2`, `Godel 1 - Part
3`, and `Godel 1 - Part 4`, it must render four separate options/cards. Do not
collapse them into `Godel 1`.

## Backend Read Models

Backend read models must expose enough fields for clients to resolve the actual
shed identity without guessing:

```text
shedId
physicalShed or shedName
partition or partitionLabel
stable row/task/bucket id at actual-shed grain
```

If a task/bucket is generated from a higher-level drive or batch, the generated
operator work item still has to be unique at actual-shed grain. The drive can be
the umbrella; the operator task/card/dropdown option cannot be the umbrella.

## Client Contract

Clients must not group cards, list rows, dropdown options, Compose keys, route
identity, or cache scopes by the base/common shed when a partition exists.

Required examples:

```text
card key      = actual-shed identity + task/bucket identity
route params  = actual-shed identity + task/bucket identity
cache scope   = actual-shed identity + task/bucket identity
display label = actual shed label, for example Gandhi - Part 1
```

Forbidden examples:

```text
key = base/common shed id
key = taskId
key = campaignShedId
label = base/common shed name
groupBy(base/common shed)
groupBy(taskId)
groupBy(batchId)
```

Those are valid only inside code that has first proved the work item has no
parts.

## CI And Review

Any change touching these surfaces must run the partition identity guard:

```bash
make operational-partition-identity-guard
```

Run the affected product tests too:

```bash
./gradlew :app:compileStgDebugKotlin --no-daemon
./gradlew :app:testStgDebugUnitTest --tests '*ShedsExecutionIdentityTest' --no-daemon
```

Review checklist:

- Does every repeated operator card include partition in its stable key?
- Does every visible shed label include partition when present?
- Does every dropdown option include partition when present?
- Does every backend row expose partition metadata rather than making clients parse it?
- Does every count match the operational grain: animals for animal work, doses only for vaccine carry?
- Does one goat proof/scan satisfy all same-animal vaccination obligations?
